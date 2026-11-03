package main

import (
	"context"
	"fmt"
	"slices"
	"time"
)

const defaultRounds = 3

func (s Step) rounds() int {
	if s.Rounds > 0 {
		return s.Rounds
	}
	return defaultRounds
}

func (r *Runner) loop(ctx context.Context, p pass, step Step, sr *StepRun, i int) int {
	target := p.chain.stepIndex(step.Repeat)
	back := p.chain.Steps[target].Title
	round, rounds := sr.Repeats+1, step.rounds()
	now := time.Now()
	if round < rounds {
		r.store.update(p.run, func() {
			sr.Status, sr.StartedAt, sr.EndedAt = "done", &now, &now
			sr.Headline = fmt.Sprintf("Sent the run back to %s for round %d of %d", back, round+1, rounds)
			for j := target; j <= i; j++ {
				p.steps[j] = again(p.steps[j])
				p.steps[j].Rounds = fmt.Sprintf("%d of %d", round+1, rounds)
			}
			p.steps[i].Repeats = round
			p.steps[target].Loop = fmt.Sprintf("Round %d of %d: %s sent the run back here", round+1, rounds, step.Title)
		})
		return target - 1
	}
	r.store.update(p.run, func() {
		sr.StartedAt, sr.EndedAt = &now, &now
		if step.Fail == "out_of_rounds" {
			p.fail(ctx, step, sr, fmt.Errorf("all %d rounds ran and it still wanted to go back to %s", rounds, back))
			return
		}
		sr.Status, sr.Headline = "done", fmt.Sprintf("All %d rounds ran, so the run went on", rounds)
	})
	return i
}

func restartLoops(steps []*StepRun, landing, from int) {
	for r := from + 1; r < len(steps); r++ {
		sr := steps[r]
		if sr.Kind != "repeat" || slices.IndexFunc(steps, func(s *StepRun) bool { return s.ID == sr.Uses }) <= landing {
			continue
		}
		sr.Repeats = 0
		for j := from + 1; j <= r; j++ {
			steps[j].Rounds = ""
		}
	}
}

func crossingLoops(chain *Chain) []string {
	var problems []string
	for j, outer := range chain.Steps {
		start := chain.stepIndex(outer.Repeat)
		for i := max(start, 0); outer.Repeat != "" && i < j; i++ {
			if inner := chain.Steps[i]; inner.Repeat != "" && chain.stepIndex(inner.Repeat) < start {
				problems = append(problems, fmt.Sprintf("step %q: its loop from %s crosses the loop of step %q; nest loops or keep them apart", outer.ID, outer.Repeat, inner.ID))
			}
		}
	}
	return problems
}

func (p pass) endedEarly(ctx context.Context, step Step, sr *StepRun) {
	if round := sr.Repeats + 1; step.Fail == "ended_early" && round < step.rounds() {
		p.fail(ctx, step, sr, fmt.Errorf("the loop ended after round %d of %d, because %s", round, step.rounds(), unmet(step.When)))
	}
}

func loopProblems(step Step, where string, seen map[string]bool) []string {
	if step.Repeat == "" {
		if step.Rounds != 0 || step.Fail != "" {
			return []string{where + ": rounds and fail only apply to repeat steps"}
		}
		return nil
	}
	var problems []string
	if !seen[step.Repeat] {
		problems = append(problems, fmt.Sprintf("%s: repeat must name an earlier step in this chain, not %q", where, step.Repeat))
	}
	if step.Rounds < 0 || step.Rounds == 1 {
		problems = append(problems, fmt.Sprintf("%s: rounds is the most times the loop runs, 2 or more, not %d", where, step.Rounds))
	}
	if step.Fail != "" && step.Fail != "out_of_rounds" && step.Fail != "ended_early" {
		problems = append(problems, fmt.Sprintf("%s: fail is out_of_rounds or ended_early, not %q", where, step.Fail))
	}
	if len(step.With) > 0 || step.StopIf != "" || step.Stop != "" || step.Always {
		problems = append(problems, where+": a repeat step takes only when, rounds and fail")
	}
	return problems
}
