package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

type Runner struct {
	store *Store
	swarm string
	addr  string
}

func (r *Runner) logFile(run *Run, sr *StepRun, ext string) string {
	return filepath.Join(r.store.logDir(run), sr.Log+ext)
}

func (r *Runner) environment(run *Run) []string {
	return append(os.Environ(),
		"SWARM_ADDR="+r.addr,
		"SWARM_PROJECT="+run.Project,
		"PATH="+filepath.Dir(r.swarm)+string(os.PathListSeparator)+os.Getenv("PATH"))
}

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

func (r *Runner) start(lib *Library, chain *Chain, inputs map[string]string) (*Run, error) {
	if len(chain.Problems) > 0 {
		return nil, fmt.Errorf("fix the chain first: %s", strings.Join(chain.Problems, "; "))
	}
	if len(chain.Steps) == 0 {
		return nil, fmt.Errorf("%s has no steps yet", chain.Name)
	}
	values := map[string]string{"project": lib.Project, "home": lib.Home, "swarm": r.swarm, "memory": readMemory(lib.Project), "feedback": ""}
	for _, in := range chain.Inputs {
		v := strings.TrimSpace(inputs[in.Name])
		if v == "" {
			v = in.Default
		}
		if v == "" && !in.Optional {
			return nil, fmt.Errorf("%s is required", in.Label)
		}
		values[in.Name] = v
	}
	run := &Run{
		ID:        r.store.newID(),
		Chain:     chain.Name,
		Title:     chain.Title,
		Project:   lib.Project,
		Inputs:    map[string]string{},
		Status:    "running",
		StartedAt: time.Now(),
		decisions: make(chan decision, 1),
	}
	for _, in := range chain.Inputs {
		run.Inputs[in.Name] = values[in.Name]
	}
	values["run_id"] = run.ID
	values["run_folder"] = r.store.logDir(run)
	values["guide"] = guideText()
	values["catalog"] = lib.catalog()
	run.Steps = expand(lib, chain, "")
	ctx, cancel := context.WithCancel(context.Background())
	run.cancel = cancel
	r.store.put(run)
	go r.execute(ctx, lib, chain, run, newScope(values))
	return run, nil
}

func expand(lib *Library, chain *Chain, prefix string) []*StepRun {
	var out []*StepRun
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
	r.runSteps(ctx, lib, run, chain, run.Steps, sc, end)
	finished := time.Now()
	r.store.update(run, func() {
		run.EndedAt = &finished
		run.Status = end.status
		run.Reason = end.reason
	})
}

func (r *Runner) runSteps(ctx context.Context, lib *Library, run *Run, chain *Chain, steps []*StepRun, sc *scope, end *runEnd) {
	for _, v := range chain.Vars {
		sc.values[v.Name] = sc.fill(v.Value, false)
	}
	for i := 0; i < len(chain.Steps); i++ {
		step, sr := chain.Steps[i], steps[i]
		if ctx.Err() != nil {
			end.finish("stopped", "You stopped the run before: "+step.Title)
		}
		if end.ended && !step.Always {
			r.store.update(run, func() { skipAll(sr) })
			continue
		}
		if step.When != "" && !sc.holds(step.When) {
			r.store.update(run, func() { skipAll(sr); sr.Headline = "Skipped: " + step.When + " is not true" })
			continue
		}
		stepCtx := ctx
		if end.ended {
			stepCtx = context.Background()
		}
		now := time.Now()
		r.store.update(run, func() {
			sr.Status = "running"
			sr.StartedAt = &now
			if step.Kind() == "tool" || step.Kind() == "agent" {
				sr.Log = sr.logName()
			}
		})

		var out map[string]string
		var err error
		if step.Kind() == "chain" {
			out, err = r.runChain(stepCtx, lib, run, sr, step, sc, end)
		} else {
			out, err = r.runStep(stepCtx, lib, run, sr, step, sc)
		}
		sc.outputs[step.ID] = toAny(out)
		local := stepScope(step, sc, out)
		finished := time.Now()
		r.store.update(run, func() {
			sr.EndedAt = &finished
			if step.Kind() != "chain" {
				sr.Output = toAny(out)
			}
			if sr.Artifacts == nil {
				sr.Artifacts = r.artifacts(run.Project, local, append(lib.Tools[step.Tool].Show, step.Show...))
			}
			if step.Headline != "" {
				sr.Headline = local.fill(step.Headline, false)
			}
			if err != nil {
				sr.Status = "failed"
				sr.Error = err.Error()
				if ctx.Err() != nil {
					end.finish("stopped", "You stopped the run during: "+step.Title)
				} else {
					end.finish("failed", step.Title+" failed: "+err.Error())
				}
				return
			}
			sr.Status = "done"
			for _, child := range sr.Children {
				if child.Status == "stopped" || child.Status == "rejected" {
					sr.Status = child.Status
				}
			}
			if step.Kind() == "approval" && out["decision"] == "reject" {
				reason := "You rejected: " + step.Title
				if out["note"] != "" {
					reason += " (" + out["note"] + ")"
				}
				sr.Status = "rejected"
				end.finish("rejected", reason)
			}
			if step.StopIf != "" && !end.ended && local.holds(step.StopIf) {
				reason := local.fill(step.Stop, false)
				if reason == "" {
					reason = "Stopped after: " + step.Title
				}
				sr.Status, sr.Ended = "stopped", reason
				end.finish("stopped", reason)
			}
			if sc.parent == nil {
				r.refreshSubject(chain, run, sc)
			}
		})
		if err == nil && !end.ended && out["decision"] == "revise" {
			target := chain.stepIndex(step.Revise)
			r.store.update(run, func() {
				for j := target; j <= i; j++ {
					steps[j] = again(steps[j])
				}
			})
			earlier, _ := sc.lookup("feedback")
			sc.values["feedback"] = strings.TrimSpace(earlier + "\n\n" + out["note"])
			i = target - 1
		}
	}
}

func skipAll(sr *StepRun) {
	sr.Status = "skipped"
	for _, child := range sr.Children {
		skipAll(child)
	}
}

func (r *Runner) runChain(ctx context.Context, lib *Library, run *Run, sr *StepRun, step Step, sc *scope, end *runEnd) (map[string]string, error) {
	values := map[string]string{}
	for k, v := range step.With {
		values[k] = sc.fill(v, false)
	}
	child := sc.child(values)
	r.runSteps(ctx, lib, run, lib.Chains[step.Chain], sr.Children, child, end)

	out := map[string]string{}
	for k, v := range child.flatten() {
		out[k] = text(v)
	}
	for id, fields := range child.outputs {
		if _, taken := sc.outputs[id]; !taken {
			sc.outputs[id] = fields
		}
	}
	var failed []string
	cost := 0.0
	for _, c := range sr.Children {
		cost += c.Cost
		if c.Status == "failed" {
			failed = append(failed, c.Title)
		}
	}
	r.store.update(run, func() { sr.Cost = cost })
	if len(failed) > 0 {
		return out, fmt.Errorf("%s failed", strings.Join(failed, ", "))
	}
	return out, nil
}

func (r *Runner) refreshSubject(chain *Chain, run *Run, sc *scope) {
	if v, ok := sc.complete(chain.Subject); ok && chain.Subject != "" {
		run.Subject = v
	}
	if v, ok := sc.complete(chain.Link); ok && chain.Link != "" {
		run.Link = v
	}
}

func (r *Runner) runStep(ctx context.Context, lib *Library, run *Run, sr *StepRun, step Step, sc *scope) (map[string]string, error) {
	switch step.Kind() {
	case "agent":
		return r.runAgent(ctx, lib, run, sr, step, sc)
	case "approval":
		return r.runApproval(ctx, run, sr, step, sc)
	default:
		return r.runTool(ctx, lib, run, sr, step, sc)
	}
}

func (r *Runner) runTool(ctx context.Context, lib *Library, run *Run, sr *StepRun, step Step, sc *scope) (map[string]string, error) {
	command := stepScope(step, sc, nil).fill(lib.Tools[step.Tool].Run, true)
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = run.Project
	cmd.Env = r.environment(run)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	call := startCall(r.logFile(run, sr, ".call.json"), cmd, "", false)
	err := runGroup(ctx, cmd)
	call.finish(cmd, err, stdout.String(), stderr.String())

	out := map[string]string{"output": strings.TrimSpace(stdout.String())}
	var fields map[string]any
	if json.Unmarshal(stdout.Bytes(), &fields) == nil {
		for k, v := range fields {
			out[k] = text(v)
		}
	}
	if err != nil {
		reason := lastLines(stderr.String(), 3)
		if reason == "" {
			reason = lastLines(stdout.String(), 3)
		}
		if reason == "" {
			reason = err.Error() + "; it printed nothing"
		}
		return out, errors.New(reason)
	}
	headline := lastLine(stdout.String())
	r.store.update(run, func() { sr.Headline = headline })
	return out, nil
}

func (r *Runner) runApproval(ctx context.Context, run *Run, sr *StepRun, step Step, sc *scope) (map[string]string, error) {
	r.store.update(run, func() {
		sr.Status = "waiting"
		sr.Question = stepScope(step, sc, nil).fill(step.Approval, false)
		sr.Artifacts = r.artifacts(run.Project, stepScope(step, sc, nil), step.Show)
		run.Status = "waiting"
	})
	select {
	case d := <-run.decisions:
		headline := map[string]string{"approve": "Approved", "reject": "Rejected", "revise": "Sent back to " + sr.Revise}[d.Choice]
		if d.Note != "" {
			headline += ": " + d.Note
		}
		r.store.update(run, func() { run.Status = "running"; sr.Headline = headline })
		return map[string]string{"decision": d.Choice, "note": d.Note}, nil
	case <-ctx.Done():
		return nil, errors.New("stopped")
	}
}

func (r *Runner) artifacts(project string, sc *scope, patterns []string) []Artifact {
	var found []Artifact
	seen := map[string]bool{}
	for _, pattern := range patterns {
		path := sc.fill(pattern, false)
		if !filepath.IsAbs(path) {
			path = filepath.Join(project, path)
		}
		matches, _ := filepath.Glob(path)
		sort.Strings(matches)
		for _, m := range matches {
			if info, err := os.Stat(m); err != nil || info.IsDir() || seen[m] {
				continue
			}
			seen[m] = true
			found = append(found, Artifact{Kind: kindOf(m), Path: m, Name: filepath.Base(m)})
		}
	}
	return found
}

func kindOf(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".webm", ".mov":
		return "video"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return "image"
	case ".md":
		return "markdown"
	case ".diff", ".patch":
		return "diff"
	case ".json":
		return "json"
	default:
		return "text"
	}
}

func runGroup(ctx context.Context, cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		case <-finished:
		}
	}()
	return cmd.Wait()
}

func stepScope(step Step, sc *scope, out map[string]string) *scope {
	local := map[string]any{}
	for k, v := range step.With {
		local[k] = sc.fill(v, false)
	}
	for k, v := range out {
		local[k] = v
	}
	return sc.with(local)
}

func toAny(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func nonEmptyLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "==>"))
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func lastLine(s string) string {
	return lastLines(s, 1)
}

func lastLines(s string, n int) string {
	lines := nonEmptyLines(s)
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func firstLine(s string) string {
	lines := nonEmptyLines(s)
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}
