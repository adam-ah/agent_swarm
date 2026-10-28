package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

func (r *Runner) runStep(ctx context.Context, lib *Library, run *Run, sr *StepRun, step Step, sc *scope) (map[string]any, error) {
	switch step.Kind() {
	case "agent":
		return r.runAgent(ctx, lib, run, sr, step, sc)
	case "user":
		return r.askUser(ctx, run, sr, step, sc)
	default:
		return r.runTool(ctx, lib, run, sr, step, sc)
	}
}

func (r *Runner) runTool(ctx context.Context, lib *Library, run *Run, sr *StepRun, step Step, sc *scope) (map[string]any, error) {
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

	out := map[string]any{"output": strings.TrimSpace(stdout.String())}
	var fields map[string]any
	if json.Unmarshal(stdout.Bytes(), &fields) == nil {
		maps.Copy(out, fields)
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
	if fields != nil {
		headline = text(out["summary"])
	}
	r.store.update(run, func() { sr.Headline = headline })
	return out, nil
}

func (r *Runner) askUser(ctx context.Context, run *Run, sr *StepRun, step Step, sc *scope) (map[string]any, error) {
	r.store.update(run, func() {
		sr.Status = "waiting"
		sr.Question = stepScope(step, sc, nil).fill(step.User, false)
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
		return map[string]any{"decision": d.Choice, "note": d.Note}, nil
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

func stepScope(step Step, sc *scope, out map[string]any) *scope {
	local := map[string]any{}
	for k, v := range step.With {
		local[k] = sc.fill(v, false)
	}
	maps.Copy(local, out)
	return sc.with(local)
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
