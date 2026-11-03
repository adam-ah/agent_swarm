package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type runEnd struct {
	ended  bool
	status string
	reason string
}

func (e *runEnd) finish(status, reason string) {
	if !e.ended {
		e.status, e.reason = status, reason
	}
	e.ended = true
}

type pass struct {
	lib      *Library
	run      *Run
	chain    *Chain
	steps    []*StepRun
	sc       *scope
	end      *runEnd
	feedback *string
}

func expand(lib *Library, chain *Chain, prefix string) []*StepRun {
	out := make([]*StepRun, 0, len(chain.Steps))
	for _, step := range chain.Steps {
		sr := &StepRun{ID: step.ID, Path: prefix + step.ID, Title: step.Title, Kind: step.Kind(), Uses: step.Uses(), Always: step.Always, Status: "pending"}
		if i := chain.stepIndex(step.Revise); step.Revise != "" && i >= 0 {
			sr.Revise = chain.Steps[i].Title
		}
		if step.Chain != "" {
			sr.Children = expand(lib, lib.Chains[step.Chain], sr.Path+".")
		}
		out = append(out, sr)
	}
	return out
}

func (r *Runner) execute(ctx context.Context, lib *Library, chain *Chain, run *Run, sc *scope) {
	end := &runEnd{status: "done"}
	r.runSteps(ctx, pass{lib: lib, run: run, chain: chain, steps: run.Steps, sc: sc, end: end, feedback: &run.Feedback})
	r.ending.Lock()
	defer r.ending.Unlock()
	r.noteStopped(run, "when the run ended", r.endRun(run))
	finished := time.Now()
	r.store.update(run, func() {
		run.EndedAt = &finished
		run.Status = end.status
		run.Reason = end.reason
	})
}

func (r *Runner) runSteps(ctx context.Context, p pass) {
	for _, v := range p.chain.Vars {
		p.sc.values[v.Name] = p.sc.fill(v.Value, false)
	}
	if *p.feedback != "" {
		p.sc.values["feedback"] = *p.feedback
	}
	if p.sc.parent == nil {
		r.store.update(p.run, func() { r.refreshSubject(p.chain, p.run, p.sc) })
	}
	for i := 0; i < len(p.chain.Steps); i++ {
		i = r.runOne(ctx, p, i)
	}
}

func (r *Runner) runOne(runCtx context.Context, p pass, i int) int {
	step, sr := p.chain.Steps[i], p.steps[i]
	if r.mocks != nil {
		r.mocks.order = append(r.mocks.order, sr)
	}
	defer func() { r.noteStopped(p.run, "after "+step.Title, r.release(sr.Path)) }()
	if runCtx.Err() != nil {
		p.end.finish("stopped", "You stopped the run before: "+step.Title)
	}
	if sr.Status == "done" {
		restore(p.lib, p.sc, step, sr)
		return i
	}
	if r.skip(runCtx, p, step, sr) {
		return i
	}
	if step.Kind() == "repeat" {
		return r.loop(runCtx, p, step, sr, i)
	}
	ctx := runCtx
	if p.end.ended {
		ctx = context.WithoutCancel(runCtx)
	}
	now := time.Now()
	r.store.update(p.run, func() {
		sr.Status, sr.StartedAt = "running", &now
		if step.Kind() == "tool" || step.Kind() == "agent" {
			sr.Log = sr.logName()
		}
	})
	var out map[string]any
	var err error
	if step.Kind() == "chain" {
		out, err = r.runChain(ctx, p, sr, step)
	} else {
		out, err = r.runStep(ctx, p.lib, p.chain, p.run, sr, step, p.sc)
	}
	p.sc.outputs[step.ID] = out
	r.record(runCtx, p, step, sr, out, err)
	if err == nil && !p.end.ended && out["decision"] == "revise" {
		return r.sendBack(p, step, i, text(out["note"]))
	}
	return i
}

func (r *Runner) skip(ctx context.Context, p pass, step Step, sr *StepRun) bool {
	if p.end.ended && !step.Always {
		delete(p.sc.outputs, step.ID)
		r.store.update(p.run, func() { skipAll(sr) })
		return true
	}
	if step.When != "" && !p.sc.holds(step.When) {
		delete(p.sc.outputs, step.ID)
		r.store.update(p.run, func() {
			skipAll(sr)
			sr.Headline = "Skipped: " + unmet(step.When)
			p.endedEarly(ctx, step, sr)
		})
		return true
	}
	return false
}

func (r *Runner) record(ctx context.Context, p pass, step Step, sr *StepRun, out map[string]any, err error) {
	local := stepScope(step, p.sc, out)
	finished := time.Now()
	r.store.update(p.run, func() {
		sr.EndedAt = &finished
		if step.Kind() != "chain" {
			sr.Output = out
		}
		if err != nil {
			p.fail(ctx, step, sr, err)
			return
		}
		p.settle(step, sr, out, local)
		if p.sc.parent == nil {
			r.refreshSubject(p.chain, p.run, p.sc)
		}
	})
}

func (p pass) fail(ctx context.Context, step Step, sr *StepRun, err error) {
	sr.Status, sr.Error = "failed", err.Error()
	if ctx.Err() != nil {
		p.end.finish("stopped", "You stopped the run during: "+step.Title)
		return
	}
	p.end.finish("failed", step.Title+" failed: "+err.Error())
}

func (p pass) settle(step Step, sr *StepRun, out map[string]any, local *scope) {
	sr.Status = "done"
	for _, child := range sr.Children {
		if child.Kind != "item" && (child.Status == "stopped" || child.Status == "rejected") {
			sr.Status = child.Status
		}
	}
	if step.Kind() == "user" && out["decision"] == "reject" {
		reason := "You rejected: " + step.Title
		if note := text(out["note"]); note != "" {
			reason += " (" + note + ")"
		}
		sr.Status = "rejected"
		p.end.finish("rejected", reason)
	}
	if step.StopIf != "" && !p.end.ended && local.holds(step.StopIf) {
		reason := local.fill(step.Stop, false)
		if reason == "" {
			reason = "Stopped after: " + step.Title
		}
		sr.Status, sr.Ended = "stopped", reason
		p.end.finish("stopped", reason)
	}
}

func (r *Runner) sendBack(p pass, step Step, i int, note string) int {
	target := p.chain.stepIndex(step.Revise)
	earlier, _ := p.sc.lookup("feedback")
	feedback := strings.TrimSpace(earlier + "\n\n" + note)
	r.store.update(p.run, func() {
		for j := target; j <= i; j++ {
			p.steps[j] = again(p.steps[j])
		}
		p.steps[target].SentBack = note
		*p.feedback = feedback
	})
	p.sc.values["feedback"] = feedback
	return target - 1
}

func skipAll(sr *StepRun) {
	sr.Status = "skipped"
	for _, child := range sr.Children {
		skipAll(child)
	}
}

func (r *Runner) runChain(ctx context.Context, p pass, sr *StepRun, step Step) (map[string]any, error) {
	sub := p.lib.Chains[step.Chain]
	values := map[string]string{}
	for _, in := range sub.Inputs {
		if _, known := p.sc.lookup(in.Name); !known && p.lib.Settings.inputValue(in) != "" {
			values[in.Name] = p.sc.fill(p.lib.Settings.inputValue(in), false)
		}
	}
	for k, v := range step.With {
		values[k] = p.sc.fill(v, false)
	}
	key, items, err := listInput(step, p.sc)
	if err != nil {
		return nil, err
	}
	if key != "" {
		return r.runEach(ctx, p, sr, step, values, key, items)
	}
	if len(sr.Children) != len(sub.Steps) || len(sr.Children) > 0 && sr.Children[0].Kind == "item" {
		r.store.update(p.run, func() { sr.Children = expand(p.lib, sub, sr.Path+".") })
	}
	child := p.sc.child(values)
	r.runSteps(ctx, pass{lib: p.lib, run: p.run, chain: sub, steps: sr.Children, sc: child, end: p.end, feedback: &sr.Feedback})

	out := child.flatten()
	p.sc.adopt(child)
	var failed []string
	for _, c := range sr.Children {
		if c.Status == "failed" {
			failed = append(failed, c.Title)
		}
	}
	r.store.update(p.run, func() { sr.Cost = costOf(sr.Children) })
	if len(failed) > 0 {
		return out, fmt.Errorf("%s failed", strings.Join(failed, ", "))
	}
	return out, nil
}

func costOf(steps []*StepRun) float64 {
	total := 0.0
	for _, s := range steps {
		total += s.Cost
	}
	return total
}

func (r *Runner) refreshSubject(chain *Chain, run *Run, sc *scope) {
	if v, ok := sc.complete(chain.Subject); ok && chain.Subject != "" {
		run.Subject = v
	}
	if v, ok := sc.complete(chain.Link); ok && chain.Link != "" {
		run.Link = v
	}
}
