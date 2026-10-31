package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

func decisionFile(step string) string {
	return "decision-" + step + ".json"
}

type inbox struct {
	mu      sync.Mutex
	waiting map[string]chan decision
	held    []decision
}

func newInbox(held ...decision) *inbox {
	return &inbox{waiting: map[string]chan decision{}, held: held}
}

func (in *inbox) post(d decision) {
	in.mu.Lock()
	defer in.mu.Unlock()
	to := d.Step
	if _, waits := in.waiting[to]; !waits && to == "" && len(in.waiting) == 1 {
		for path := range in.waiting {
			to = path
		}
	}
	if answers, waits := in.waiting[to]; waits {
		delete(in.waiting, to)
		answers <- d
		return
	}
	in.held = append(in.held, d)
}

func (in *inbox) await(step string) (<-chan decision, func()) {
	in.mu.Lock()
	defer in.mu.Unlock()
	answers := make(chan decision, 1)
	if i := slices.IndexFunc(in.held, func(d decision) bool { return d.Step == step || d.Step == "" }); i >= 0 {
		answers <- in.held[i]
		in.held = slices.Delete(in.held, i, i+1)
		return answers, func() {}
	}
	in.waiting[step] = answers
	return answers, func() {
		in.mu.Lock()
		defer in.mu.Unlock()
		if in.waiting[step] == answers {
			delete(in.waiting, step)
		}
	}
}

type waiter struct {
	*StepRun

	item string
}

func waitingSteps(steps []*StepRun) []waiter {
	return waitingIn(steps, "")
}

func waitingIn(steps []*StepRun, item string) []waiter {
	var found []waiter
	for _, step := range steps {
		if step.Status == "waiting" {
			found = append(found, waiter{step, item})
		}
		inner := item
		if step.Kind == "item" {
			inner = step.Title
		}
		found = append(found, waitingIn(step.Children, inner)...)
	}
	return found
}

func (run *Run) refreshStatus() {
	run.Status = "running"
	if len(waitingSteps(run.Steps)) > 0 {
		run.Status = "waiting"
	}
}

func waitingList(waiting []waiter) string {
	lines := make([]string, len(waiting))
	for i, w := range waiting {
		lines[i] = fmt.Sprintf("- %s: %s", w.Path, w.Title)
		if w.item != "" {
			lines[i] += " (" + w.item + ")"
		}
	}
	return strings.Join(lines, "\n")
}

func waitingStep(run *Run, path string) (*StepRun, error) {
	waiting := waitingSteps(run.Steps)
	switch {
	case run.Status != "waiting" || len(waiting) == 0:
		return nil, errors.New("the run is not waiting for you")
	case path != "":
		if i := slices.IndexFunc(waiting, func(w waiter) bool { return w.Path == path }); i >= 0 {
			return waiting[i].StepRun, nil
		}
		return nil, fmt.Errorf("%s is not waiting for you; waiting now:\n%s", path, waitingList(waiting))
	case len(waiting) > 1:
		return nil, fmt.Errorf("%d steps wait for you; name one with -step:\n%s", len(waiting), waitingList(waiting))
	}
	return waiting[0].StepRun, nil
}

func (c *Core) Decide(id string, d decision) error {
	run := c.store.load(id)
	if run == nil {
		return fmt.Errorf("no run %s", id)
	}
	step, err := waitingStep(run, d.Step)
	if err != nil {
		return err
	}
	if err = d.check(step); err != nil {
		return err
	}
	d.Step = step.Path
	if c.store.hasSignal(id, decisionFile(step.Path)) {
		return fmt.Errorf("a decision for %s is already being applied", step.Path)
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return c.store.signal(id, decisionFile(step.Path), raw)
}
