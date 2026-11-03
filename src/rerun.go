package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
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
		if sr.Status != "done" && sr.Status != "skipped" {
			restart(steps, i, retry)
			return
		}
	}
}

func rewindFrom(steps []*StepRun, path string) *StepRun {
	for i, sr := range steps {
		switch {
		case sr.Path == path:
			restart(steps, i, again)
			return steps[i]
		case !strings.HasPrefix(path, sr.Path+".") && sr.Status != "done" && sr.Status != "skipped":
			restart(steps, i, retry)
			return steps[i]
		case strings.HasPrefix(path, sr.Path+"."):
			target := rewindFrom(sr.Children, path)
			if target != nil {
				sr.Status, sr.Error, sr.Ended, sr.EndedAt = "pending", "", "", nil
				restart(steps, i, func(sr *StepRun) *StepRun { return sr })
			}
			return target
		}
	}
	return nil
}

func restart(steps []*StepRun, from int, first func(*StepRun) *StepRun) {
	old := slices.Clone(steps)
	steps[from] = first(steps[from])
	for j := from + 1; j < len(steps); j++ {
		steps[j] = again(steps[j])
	}
	for r := from + 1; r < len(steps); r++ {
		if old[r].Kind != "repeat" || slices.IndexFunc(old, func(sr *StepRun) bool { return sr.ID == old[r].Uses }) > from {
			continue
		}
		steps[r].Repeats = old[r].Repeats
		for j := from; j <= r; j++ {
			steps[j].Rounds = old[j].Rounds
		}
	}
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

func restore(sc *scope, step Step, sr *StepRun) {
	if sr.Session != "" {
		sc.keepSession(step, agentSession{id: sr.Session, runner: sr.Runner, title: step.Title})
	}
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
