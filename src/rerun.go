package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func (c *Core) Rerun(id string, fromScratch bool) (string, error) {
	run := c.store.load(id)
	if run == nil {
		return "", fmt.Errorf("no run %s", id)
	}
	if run.going() {
		return "", errors.New("the run is still going")
	}
	lib, chain, err := c.chainOf(run)
	if err != nil {
		return "", err
	}
	c.store.sweepRun(id)
	if fromScratch {
		fresh, err := c.runner.start(lib, chain, run.Inputs)
		if err != nil {
			return "", err
		}
		return fresh.ID, nil
	}
	return id, c.runner.resume(lib, chain, id, "")
}

func (c *Core) RerunFrom(id, path string) error {
	run := c.store.load(id)
	if run == nil {
		return fmt.Errorf("no run %s", id)
	}
	step := findStep(run.Steps, func(s *StepRun) bool { return s.Path == path })
	switch {
	case run.going():
		return errors.New("stop the run first")
	case step == nil:
		return fmt.Errorf("the run has no step %s", path)
	case step.Status == "pending" || step.Status == "skipped":
		return fmt.Errorf("%s did not run", step.Title)
	}
	lib, chain, err := c.chainOf(run)
	if err != nil {
		return err
	}
	c.store.sweepRun(id)
	return c.runner.resume(lib, chain, id, path)
}

func (c *Core) chainOf(run *Run) (*Library, *Chain, error) {
	lib, err := c.Library(run.Project)
	if err != nil {
		return nil, nil, err
	}
	chain := lib.Chains[run.Chain]
	if chain == nil {
		return nil, nil, fmt.Errorf("no chain named %s", run.Chain)
	}
	return lib, chain, nil
}

func (c *Core) DeleteRun(id string) error {
	run := c.store.load(id)
	if run == nil {
		return fmt.Errorf("no run %s", id)
	}
	if run.going() {
		return errors.New("stop the run before deleting it")
	}
	c.store.sweepRun(id)
	return c.store.remove(id)
}

func (r *Runner) resume(lib *Library, chain *Chain, id, from string) error {
	if len(chain.Problems) > 0 {
		return fmt.Errorf("fix the chain first: %s", strings.Join(chain.Problems, "; "))
	}
	run := &Run{}
	if err := readJSON(filepath.Join(r.store.runDir(id), "run.json"), run); err != nil {
		return err
	}
	if !fits(lib, chain, run.Steps) {
		return errors.New("the chain's steps changed since this run; rerun it from scratch")
	}
	if err := r.store.clearSignals(id); err != nil {
		return err
	}
	now := time.Now()
	r.store.update(run, func() {
		run.RerunFrom = ""
		if target := rewindFrom(run.Steps, from); target != nil {
			run.RerunFrom = target.Title
		} else {
			rewind(run.Steps)
		}
		run.Status, run.Reason, run.EndedAt, run.PID, run.ResumedAt = "running", "", nil, 0, &now
	})
	if err := r.spawn(run); err != nil {
		r.abandon(run, "its process could not start: "+err.Error())
		return err
	}
	return nil
}

func fits(lib *Library, chain *Chain, steps []*StepRun) bool {
	if chain == nil || len(steps) != len(chain.Steps) {
		return false
	}
	for i, step := range chain.Steps {
		sr := steps[i]
		if sr.ID != step.ID || sr.Kind != step.Kind() || sr.Uses != step.Uses() {
			return false
		}
		if step.Chain == "" || len(sr.Children) == 0 {
			continue
		}
		sub := lib.Chains[step.Chain]
		if sr.Children[0].Kind != "item" {
			if !fits(lib, sub, sr.Children) {
				return false
			}
			continue
		}
		for _, group := range sr.Children {
			if !fits(lib, sub, group.Children) {
				return false
			}
		}
	}
	return true
}

func rewind(steps []*StepRun) {
	for i, sr := range steps {
		if sr.Status == "done" || sr.Status == "skipped" {
			continue
		}
		steps[i] = retry(sr)
		for j := i + 1; j < len(steps); j++ {
			steps[j] = again(steps[j])
		}
		return
	}
}

func rewindFrom(steps []*StepRun, path string) *StepRun {
	for i, sr := range steps {
		var target *StepRun
		switch {
		case sr.Path == path:
			steps[i] = again(sr)
			target = steps[i]
		case strings.HasPrefix(path, sr.Path+"."):
			if target = rewindFrom(sr.Children, path); target == nil {
				return nil
			}
			sr.Status, sr.Error, sr.Ended, sr.EndedAt = "pending", "", "", nil
		default:
			continue
		}
		for j := i + 1; j < len(steps); j++ {
			steps[j] = again(steps[j])
		}
		return target
	}
	return nil
}

func retry(sr *StepRun) *StepRun {
	if sr.Kind != "chain" && sr.Kind != "item" {
		return again(sr)
	}
	sr.Status, sr.Error, sr.Ended, sr.EndedAt = "pending", "", "", nil
	if sr.Kind == "item" || len(sr.Children) == 0 || sr.Children[0].Kind != "item" {
		rewind(sr.Children)
		return sr
	}
	for i, group := range sr.Children {
		if group.Status != "done" {
			sr.Children[i] = retry(group)
		}
	}
	return sr
}

func restore(lib *Library, sc *scope, step Step, sr *StepRun) {
	if step.Kind() != "chain" {
		sc.outputs[step.ID] = sr.Output
		if sr.Session != "" {
			sc.keepSession(step, agentSession{id: sr.Session, runner: sr.Runner, title: step.Title})
		}
		return
	}
	if key, _, _ := listInput(step, sc); key != "" {
		sc.outputs[step.ID] = map[string]any{"summary": sr.Headline}
		return
	}
	sub := lib.Chains[step.Chain]
	child := sc.child(map[string]string{})
	for i, c := range sr.Children {
		if c.Status == "done" {
			restore(lib, child, sub.Steps[i], c)
		}
	}
	sc.adopt(child)
	sc.outputs[step.ID] = child.flatten()
}

func sameItems(groups []*StepRun, items []any, sub *Chain) bool {
	if len(groups) != len(items) {
		return false
	}
	for i, group := range groups {
		if group.Kind != "item" || group.Title != text(items[i]) || len(group.Children) != len(sub.Steps) {
			return false
		}
	}
	return true
}
