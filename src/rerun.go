package main

import (
	"cmp"
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
	if !fromScratch && run.Status == "done" && !deferredIn(run.Steps) {
		return "", fmt.Errorf("run %s is done; nothing to resume", id)
	}
	c.store.sweepRun(id)
	if fromScratch {
		fresh, err := c.runner.start(lib, chain, run.Inputs)
		if err != nil {
			return "", err
		}
		return fresh.ID, nil
	}
	return id, c.runner.resume(lib, chain, id, "", false)
}

func (c *Core) cleanUp(run *Run) error {
	lib, chain, err := c.chainOf(run)
	if err != nil {
		return err
	}
	c.store.sweepRun(run.ID)
	return c.runner.resume(lib, chain, run.ID, "", true)
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
	return c.runner.resume(lib, chain, id, path, false)
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

func (r *Runner) resume(lib *Library, chain *Chain, id, from string, cleanup bool) error {
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
		run.RerunFrom, run.Cleanup = "", cleanup
		var target *StepRun
		switch {
		case cleanup:
		case from != "":
			target = rewindFrom(lib, run.Steps, from)
		default:
			target = rewind(lib, chain, run.Steps)
		}
		if target != nil {
			run.RerunFrom = target.Title
		}
		if !cleanup {
			run.Reason = ""
		}
		run.Status, run.EndedAt, run.PID, run.ResumedAt = "running", nil, 0, &now
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

func unfinished(sr *StepRun) bool {
	return sr.Status != "done" && sr.Status != "skipped" || slices.ContainsFunc(sr.Children, unfinished)
}

func rewind(lib *Library, chain *Chain, steps []*StepRun) *StepRun {
	i := slices.IndexFunc(steps, unfinished)
	if i < 0 {
		return nil
	}
	if back := retryFrom(chain, steps[i], i); back >= 0 {
		restart(steps, back, again, back-1)
		return steps[back]
	}
	return retryAt(lib, steps, i)
}

func retryAt(lib *Library, steps []*StepRun, i int) *StepRun {
	var target *StepRun
	restart(steps, i, func(sr *StepRun) *StepRun {
		fresh, inner := retry(lib, sr)
		target = inner
		return fresh
	}, i)
	return target
}

func retryFrom(chain *Chain, sr *StepRun, i int) int {
	if chain == nil || sr.Status != "failed" || i >= len(chain.Steps) {
		return -1
	}
	return chain.stepIndex(cmp.Or(chain.Steps[i].Repeat, chain.Steps[i].RetryFrom))
}

func rewindFrom(lib *Library, steps []*StepRun, path string) *StepRun {
	for i, sr := range steps {
		switch {
		case sr.Path == path:
			restart(steps, i, again, i)
			return steps[i]
		case !strings.HasPrefix(path, sr.Path+".") && unfinished(sr):
			return retryAt(lib, steps, i)
		case strings.HasPrefix(path, sr.Path+"."):
			target := rewindFrom(lib, sr.Children, path)
			if target != nil {
				sr.Status, sr.Error, sr.Ended, sr.EndedAt = "pending", "", "", nil
				restart(steps, i, func(sr *StepRun) *StepRun { return sr }, i)
			}
			return target
		}
	}
	return nil
}

func restart(steps []*StepRun, from int, first func(*StepRun) *StepRun, keepTo int) {
	old := slices.Clone(steps)
	steps[from] = first(steps[from])
	for j := from + 1; j < len(steps); j++ {
		steps[j] = again(steps[j])
	}
	for r := from + 1; r < len(steps); r++ {
		if old[r].Kind != "repeat" || slices.IndexFunc(old, func(sr *StepRun) bool { return sr.ID == old[r].Uses }) > keepTo {
			continue
		}
		steps[r].Repeats = old[r].Repeats
		for j := from; j <= r; j++ {
			steps[j].Rounds = old[j].Rounds
		}
	}
}

func retry(lib *Library, sr *StepRun) (*StepRun, *StepRun) {
	if sr.Kind != "chain" && sr.Kind != "item" {
		fresh := again(sr)
		fresh.Retry = sr.Kind == "agent" && sr.Status == "failed"
		return fresh, fresh
	}
	sr.Status, sr.Error, sr.Ended, sr.EndedAt = "pending", "", "", nil
	target := sr
	if sr.Kind == "item" || len(sr.Children) == 0 || sr.Children[0].Kind != "item" {
		if inner := rewind(lib, lib.Chains[sr.Uses], sr.Children); inner != nil {
			target = inner
		}
		return sr, target
	}
	for i, group := range sr.Children {
		if !unfinished(group) {
			continue
		}
		fresh, inner := retry(lib, group)
		sr.Children[i] = fresh
		if target == sr {
			target = inner
		}
	}
	return sr, target
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
