package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var inspectStart = time.Date(2026, 11, 3, 14, 57, 11, 0, time.UTC)

func at(d time.Duration) *time.Time {
	t := inspectStart.Add(d)
	return &t
}

func inspectRun(t *testing.T) (*Core, *Run) {
	t.Helper()
	core, project := surfaceProject(t)
	run := surfaceRun(t, core, project, func(run *Run) {
		run.Status, run.StartedAt, run.EndedAt, run.Cost = "failed", inspectStart, at(5*time.Minute), 1.5
		run.Inputs = map[string]string{"request": "add a lint step\nbefore tests"}
		run.Stopped = []string{"123 sleep 99"}
		earlier := &StepRun{Path: "build", Title: "Build", Kind: "agent", Status: "failed", Error: "first try", Log: "build", StartedAt: at(0), EndedAt: at(time.Minute)}
		run.Steps = append(run.Steps, &StepRun{
			ID: "build", Path: "build", Title: "Build", Kind: "agent", Uses: "build-card", Runner: "claude", Status: "failed",
			StartedAt: at(time.Minute), EndedAt: at(4*time.Minute + 2*time.Second), Cost: 0.84, Tokens: 41200, Actions: 37, Round: 1,
			Error: "build fail:\nmissing semicolon at card.ts:3", Session: "s1", Log: "build-2", AskedAgain: "build-2-fields",
			Output:    map[string]any{"summary": "short", "files": []any{"a.ts", "b.ts"}, "notes": "line one\nline two", "score": 3.0},
			Artifacts: []Artifact{{Kind: "diff", Path: filepath.Join(core.store.runDir(run.ID), "card.diff"), Name: "card.diff"}},
			Asks:      []*Ask{{Questions: []string{"Which file?", "Tabs?"}, Answers: []string{"card.ts", ""}, Log: "build-2-ask-1", AskedAt: at(2 * time.Minute), AnsweredAt: at(3 * time.Minute)}},
			Earlier:   []*StepRun{earlier},
		}, &StepRun{Path: "group", Title: "Group", Kind: "chain", Status: "done", Cost: 2,
			Children: []*StepRun{{Path: "group.0", Title: "Item", Kind: "item", Status: "done"}}})
	})
	dir := core.store.runDir(run.ID)
	exit := 1
	call := Call{Command: []string{"claude", "-p", strings.Repeat("a", 280), "--model", "it's"}, Dir: "/work", Stdin: "the stdin",
		StartedAt: inspectStart.Add(time.Minute), EndedAt: at(4*time.Minute + 2*time.Second), ExitCode: &exit, Stream: true,
		Result: "the result", Stderr: "warn: x", Stopped: []string{"9 node"}, Limits: "kill after 15m with nothing new",
		Actions: []*Action{
			{ID: "1", Tool: "Bash", Input: `{"command":"npm test"}`, Output: "ok", StartedAt: inspectStart.Add(61 * time.Second), EndedAt: at(63 * time.Second)},
			{ID: "2", Tool: "Read", Input: `{"file_path":"/x.ts"}`, Failed: true, StartedAt: inspectStart.Add(2 * time.Minute), EndedAt: at(2*time.Minute + 300*time.Millisecond)},
			{ID: "3", Tool: "Bash", Input: "sleep", StartedAt: inspectStart.Add(3 * time.Minute)},
		}}
	raw, err := json.Marshal(call)
	if err != nil {
		t.Fatal(err)
	}
	surfaceFiles(t, dir, map[string]string{
		"build-2.call.json": string(raw), "build-2.jsonl": `{"type":"system"}` + "\n", "card.diff": "+added\n", "notes/plan.md": "# Plan\n",
		"greet.call.json": `{"command":["echo","hello"],"dir":"/w","started_at":"2026-11-03T14:57:11Z","ended_at":"2026-11-03T14:57:12Z","exit_code":0,"stdout":"hello\n"}`,
	})
	return core, run
}

func TestCLIShowTimingCostAndJSON(t *testing.T) {
	_, run := inspectRun(t)
	out, err := surfaceCLI(t, "show", run.ID)
	surfaceContains(t, out, err, "started 14:57:11 · ran 5m 00s · $1.50\n", "inputs:\n  request:\n    add a lint step\n    before tests\n",
		"✗ Build [build · 14:58:11 · 3m 02s · $0.84]: build fail:\n", "✓ Group [group]\n", "killed outside a step (1): 123 sleep 99\n")

	out, err = surfaceCLI(t, "show", run.ID, "-json")
	var back Run
	if err != nil || json.Unmarshal([]byte(out), &back) != nil || back.Steps[1].Output["notes"] != "line one\nline two" {
		t.Errorf("show -json = %v\n%s", err, out)
	}
}

func TestCLIShowStepInFull(t *testing.T) {
	_, run := inspectRun(t)
	out, err := surfaceCLI(t, "show", run.ID, "-step", "build")
	surfaceContains(t, out, err,
		"build  failed  Build\nagent build-card · runner claude · started 14:58:11 · ended 15:01:13 · ran 3m 02s · $0.84 · 41k tokens · 37 actions · attempt 2\n",
		"new session · session s1\n", "error:\n  build fail:\n  missing semicolon at card.ts:3\n",
		"fields:\n  files:\n    - a.ts\n    - b.ts\n  notes:\n    line one\n    line two\n  score: 3\n  summary: short\n",
		"shows (1; swarm files "+run.ID+" PATH prints one):\n  diff     ",
		"questions, round 1 (asked 14:59:11, answered 15:00:11):\n  1. Which file?\n     → card.ts\n  2. Tabs?\n     → agent decides\n",
		"earlier attempt 1:\n  build  failed  Build\n  agent · started 14:57:11 · ended 14:58:11 · ran 1m 00s\n  new session · no session id, not resumable\n  error: first try\n",
		"swarm log "+run.ID+" -step build-2                  call\n", "-step build-2-fields", "-step build-2-ask-1", "add -stream")

	out, err = surfaceCLI(t, "show", run.ID, "-step", "group")
	surfaceContains(t, out, err, "steps (1):\n  ✓ Item [group.0]\n")
	out, err = surfaceCLI(t, "show", run.ID, "-step", "build", "-json")
	surfaceContains(t, out, err, `"asked_again": "build-2-fields"`)
	_, err = surfaceCLI(t, "show", run.ID, "-step", "ghost")
	surfaceFails(t, err, "no step ghost in run "+run.ID)
}

func TestCLILog(t *testing.T) {
	_, run := inspectRun(t)
	out, err := surfaceCLI(t, "log", run.ID, "-step", "build")
	surfaceContains(t, out, err, "$ claude -p <argument 2> --model 'it'\\''s' < stdin\nargument 2 (280 B):\n  aaaa",
		"in /work · started 14:58:11 · ran 3m 02s · exit 1\n", "limits: kill after 15m", "killed at step end (1): 9 node\n", "stdin (9 B): the stdin\n",
		"tool calls (3; -json has each input and output):\n  +1s ", "2.0s   Bash       npm test\n  +1m 00s ", "0.3s ✗ Read       /x.ts\n  +2m 00s  unfinished   Bash       sleep\n",
		"result (10 B): the result\n", "stderr (7 B): warn: x\n", "transcript: swarm log "+run.ID+" -step build-2 -stream\n")
	if strings.Contains(out, "stdout") {
		t.Errorf("a streamed call has no stdout block:\n%s", out)
	}

	out, err = surfaceCLI(t, "log", run.ID, "-step", "greet")
	surfaceContains(t, out, err, "$ echo hello\n", "exit 0", "stdout (6 B): hello\n", "stderr: printed nothing\n")
	out, err = surfaceCLI(t, "log", run.ID, "-step", "build", "-stream")
	surfaceContains(t, out, err, `{"type":"system"}`)
	out, err = surfaceCLI(t, "log", run.ID, "-step", "build", "-json")
	surfaceContains(t, out, err, `"output": "ok"`)

	_, err = surfaceCLI(t, "log", run.ID)
	surfaceFails(t, err, "swarm log needs -step PATH")
	_, err = surfaceCLI(t, "log", run.ID, "-step", "group")
	surfaceFails(t, err, "group is a chain step and made no call")
	_, err = surfaceCLI(t, "log", run.ID, "-step", "ghost")
	surfaceFails(t, err, "no step or log ghost in run")
	_, err = surfaceCLI(t, "log", run.ID, "-step", "build-2-fields")
	surfaceFails(t, err, "nothing recorded for build-2-fields yet")
	_, err = surfaceCLI(t, "log", run.ID, "-step", "greet", "-stream")
	surfaceFails(t, err, "no transcript for greet")
	_, err = surfaceCLI(t, "log", run.ID, "-step", "greet", "-project", t.TempDir())
	if err != nil {
		t.Errorf("log reads any project's run: %v", err)
	}
	_, err = surfaceCLI(t, "log", "0101-000000", "-step", "x")
	surfaceFails(t, err, "no run 0101-000000")
}

func TestCLIFiles(t *testing.T) {
	core, run := inspectRun(t)
	dir := core.store.runDir(run.ID)
	out, err := surfaceCLI(t, "files", run.ID)
	surfaceContains(t, out, err, "run folder:\n", "  build-2.call.json", "  notes/plan.md ", "  run.json ",
		"shown by steps (1):\n  diff     "+filepath.Join(dir, "card.diff")+"  (build)\n", "swarm files "+run.ID+" PATH prints one")

	out, err = surfaceCLI(t, "files", run.ID, "notes/plan.md")
	if err != nil || out != "# Plan\n" {
		t.Errorf("files PATH = %q, %v", out, err)
	}
	out, err = surfaceCLI(t, "files", run.ID, filepath.Join(dir, "card.diff"))
	if err != nil || out != "+added\n" {
		t.Errorf("files ARTIFACT = %q, %v", out, err)
	}
	out, err = surfaceCLI(t, "files", run.ID, filepath.Join(dir, "notes", "plan.md"))
	if err != nil || out != "# Plan\n" {
		t.Errorf("files absolute path in the folder = %q, %v", out, err)
	}
	secret := filepath.Join(t.TempDir(), "secret")
	if writeErr := os.WriteFile(secret, []byte("x"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	for _, outside := range []string{"../" + run.ID + "/../x", secret} {
		_, err = surfaceCLI(t, "files", run.ID, outside)
		surfaceFails(t, err, "is outside run "+run.ID)
	}
	_, err = surfaceCLI(t, "files", run.ID, "ghost.txt")
	surfaceFails(t, err, "no file ghost.txt in run")
	_, err = surfaceCLI(t, "files")
	surfaceFails(t, err, "swarm files needs a run id")
}

func TestCLIRunsTimingCostAndJSON(t *testing.T) {
	core, project := surfaceProject(t)
	out, err := surfaceCLI(t, "runs")
	surfaceContains(t, out, err, "No runs.\n")
	ended := surfaceRun(t, core, project, func(run *Run) {
		run.StartedAt, run.EndedAt, run.Cost, run.Subject = inspectStart, at(3*time.Minute+12*time.Second), 1.234, "the subject"
	})
	going := surfaceRun(t, core, project, func(run *Run) { run.Status, run.PID = "running", os.Getpid() })
	out, err = surfaceCLI(t, "runs")
	surfaceContains(t, out, err, ended.ID+"  done        hello              3m 12s   $1.23  the subject\n", going.ID+"  running     hello                 +0s")
	out, err = surfaceCLI(t, "runs", "-json")
	var list []RunSummary
	if err != nil || json.Unmarshal([]byte(out), &list) != nil || len(list) != 2 || list[0].Cost+list[1].Cost != 1.234 {
		t.Errorf("runs -json = %v\n%s", err, out)
	}
}

func TestStepTagWhileRunning(t *testing.T) {
	started := time.Now().Add(-70 * time.Second)
	step := &StepRun{Path: "p", Title: "T", Kind: "agent", Status: "running", StartedAt: &started, Actions: 12, Tokens: 1500}
	if got, want := stepLine(step, "", ""), "… T [p · agent · "+clock(started)+" · running 1m 10s · 1.5k tokens · 12 actions]\n"; got != want {
		t.Errorf("stepLine = %q, want %q", got, want)
	}
}

func TestItemTitlesAndToolOutput(t *testing.T) {
	for title, want := range map[string]string{"/home/x/.swarm/runs/1103-145711/waves/1/5d539.md": "waves/1/5d539.md", "card 2": "card 2"} {
		if got := shownTitle(&StepRun{Kind: "item", Title: title}); got != want {
			t.Errorf("shownTitle(%q) = %q, want %q", title, got, want)
		}
	}
	if got := shownTitle(&StepRun{Kind: "item", Title: "/x/runs/1/a", Label: "A"}); got != "A" {
		t.Errorf("shownTitle with a label = %q", got)
	}
	tool := &StepRun{Path: "t", Kind: "tool", Output: map[string]any{"output": "{\"result\": \"fail\"}", "result": "fail"}}
	if got := stepDetail("1", tool, ""); strings.Contains(got, "output") || !strings.Contains(got, "result: fail") {
		t.Errorf("tool step detail repeats the raw output:\n%s", got)
	}
}

func TestCommandHelp(t *testing.T) {
	surfaceProject(t)
	for _, args := range [][]string{{"show", "-h"}, {"show", "RUN", "--help"}, {"help", "show"}} {
		out, err := surfaceDispatch(t, args[0], args[1:]...)
		surfaceContains(t, out, err, "  swarm show RUN [-json]", "  swarm show RUN -step PATH [-json]\n", "-project DIR: the project")
		if strings.Contains(out, "swarm runs") {
			t.Errorf("%v prints other commands:\n%s", args, out)
		}
	}
	out, err := surfaceDispatch(t, "runs", "-h")
	surfaceContains(t, out, err, "  swarm runs [-json]")
	out, err = surfaceDispatch(t, "memory", "-h")
	surfaceContains(t, out, err, "  swarm memory add [-about TEXT] NOTE...\n", "  swarm memory set [-file F]")
	out, err = surfaceDispatch(t, "answer", "-h")
	surfaceContains(t, out, err, "  swarm answer RUN -decide [-step PATH]\n", "the waiting\n")
	out, err = surfaceDispatch(t, "serve", "-h")
	surfaceContains(t, out, err, "  swarm serve [PORT] [-host IP]")
	_, err = surfaceDispatch(t, "help", "nope")
	surfaceFails(t, err, `no command "nope"`)
}

func TestFormatHelpers(t *testing.T) {
	for d, want := range map[time.Duration]string{0: "0s", 59 * time.Second: "59s", 61 * time.Second: "1m 01s", 2*time.Hour + 5*time.Minute: "2h 05m"} {
		if got := span(d); got != want {
			t.Errorf("span(%s) = %q, want %q", d, got, want)
		}
	}
	if took(1500*time.Millisecond) != "1.5s" || took(90*time.Second) != "1m 30s" {
		t.Errorf("took = %q, %q", took(1500*time.Millisecond), took(90*time.Second))
	}
	if sizeOf(10) != "10 B" || sizeOf(2048) != "2.0 KB" || sizeOf(3*1024*1024) != "3.0 MB" {
		t.Errorf("sizeOf = %q %q %q", sizeOf(10), sizeOf(2048), sizeOf(3*1024*1024))
	}
	if tokens(41200) != "41k tokens" || money(0) != "" || exitText(-1) != "killed" {
		t.Errorf("tokens = %q, money(0) = %q, exitText(-1) = %q", tokens(41200), money(0), exitText(-1))
	}
	if got := valueText(map[string]any{"a": true}); got != "{\n  \"a\": true\n}" {
		t.Errorf("valueText(map) = %q", got)
	}
	if got := valueText([]any{"x\ny", nil}); got != "- x\n  y\n- " {
		t.Errorf("valueText(list) = %q", got)
	}
	if got := block("label", strings.Repeat("x", 130), ""); !strings.HasPrefix(got, "label:\n  x") {
		t.Errorf("long single line = %q", got)
	}
}
