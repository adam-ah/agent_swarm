package main

import (
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func engineSetup(t *testing.T) (*Core, string) {
	t.Helper()
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("SWARM_HOME", home)
	writeFiles(t, project, map[string]string{
		".swarm/tools/say.json":    `{"title": "Say", "run": "echo {{value}}"}`,
		".swarm/tools/whoami.json": `{"title": "Who am I", "run": "echo {{run_id}} $SWARM_PROJECT"}`,
		".swarm/tools/slow.json":   `{"title": "Slow", "run": "sleep 20; echo late"}`,
		".swarm/tools/flaky.json":  `{"title": "Flaky", "run": "echo x >> {{run_folder}}/tries; test -f fixed || { echo broken >&2; exit 1; }; echo fixed"}`,
		".swarm/chains/hello.json": `{"title": "Hello", "inputs": [{"name": "value", "label": "Value"}], "steps": [{"id": "say", "title": "Say", "tool": "say"}, {"id": "who", "title": "Who", "tool": "whoami"}]}`,
		".swarm/chains/ask.json":   `{"title": "Ask", "steps": [{"id": "say", "title": "Say", "tool": "say", "with": {"value": "draft"}}, {"id": "check", "title": "Check", "user": "Keep {{say.output}}?", "revise": "say", "show": ["*.md"]}]}`,
		".swarm/chains/slow.json":  `{"title": "Slow", "steps": [{"id": "wait", "title": "Wait", "tool": "slow"}]}`,
		".swarm/chains/flaky.json": `{"title": "Flaky", "steps": [{"id": "say", "title": "Say", "tool": "say", "with": {"value": "first"}}, {"id": "flaky", "title": "Flaky", "tool": "flaky"}]}`,
		"notes.md":                 "# Notes\n",
	})
	return openCore(home), project
}

func queueRun(t *testing.T, core *Core, project, chain string, inputs map[string]string) *Run {
	t.Helper()
	id, err := core.store.claim()
	if err != nil {
		t.Fatal(err)
	}
	run := &Run{ID: id, Chain: chain, Project: project, Inputs: inputs, Status: "running", StartedAt: time.Now()}
	core.store.update(run, func() {})
	return run
}

func awaitRun(t *testing.T, core *Core, id string, ready func(*Run) bool) *Run {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		run := core.store.load(id)
		if run != nil && ready(run) {
			return run
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s never got there; last seen %+v", id, run)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func awaitRunEnd(t *testing.T, core *Core, id string) *Run {
	t.Helper()
	return awaitRun(t, core, id, func(run *Run) bool { return !run.going() })
}

func stepWaiting(round int) func(*Run) bool {
	return func(run *Run) bool {
		step := findStep(run.Steps, func(s *StepRun) bool { return s.Status == "waiting" })
		return run.Status == "waiting" && step != nil && step.Round == round
	}
}

func killWorkerOnCleanup(t *testing.T, core *Core, id string) {
	t.Helper()
	t.Cleanup(func() {
		run, err := core.store.read(id)
		if err != nil || run.PID <= 0 || run.PID == os.Getpid() {
			return
		}
		cmdline, _ := os.ReadFile("/proc/" + strconv.Itoa(run.PID) + "/cmdline")
		if strings.Contains(string(cmdline), id) {
			_ = syscall.Kill(run.PID, syscall.SIGKILL)
		}
	})
}

func resetWorkSignals(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { signal.Reset(syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP) })
}

func runWork(t *testing.T, core *Core, id string) chan error {
	t.Helper()
	resetWorkSignals(t)
	errs := make(chan error, 1)
	go func() { errs <- work([]string{"-home", core.home, id}) }()
	return errs
}

func TestStartRunRunsTheChainInAWorker(t *testing.T) {
	core, project := engineSetup(t)
	run, err := core.StartRun(project, "hello", map[string]string{"value": " hi "})
	if err != nil {
		t.Fatal(err)
	}
	killWorkerOnCleanup(t, core, run.ID)
	if run.Inputs["value"] != "hi" || run.Status != "running" || len(run.Steps) != 2 {
		t.Errorf("started run = %+v", run)
	}
	done := awaitRunEnd(t, core, run.ID)
	if done.Status != "done" || done.PID == 0 || done.PID == os.Getpid() {
		t.Fatalf("run = %s by %d (%s)", done.Status, done.PID, done.Reason)
	}
	if got := done.Steps[1].Output["output"]; got != run.ID+" "+project {
		t.Errorf("who = %v", got)
	}
	if _, err := os.Stat(filepath.Join(core.store.runDir(run.ID), "worker.log")); err != nil {
		t.Errorf("worker log: %v", err)
	}
}

func TestStartRunTakesInputsFromSettings(t *testing.T) {
	core, project := engineSetup(t)
	core.runner.swarm = filepath.Join(t.TempDir(), "missing")
	writeFiles(t, project, map[string]string{".swarm/settings.json": `{"inputs": {"value": "from settings"}}`})
	_, err := core.StartRun(project, "hello", nil)
	if err == nil {
		t.Fatal("started a worker that does not exist")
	}
	runs := core.store.list(project)
	if len(runs) != 1 || runs[0].Status != "failed" || runs[0].Inputs["value"] != "from settings" {
		t.Fatalf("runs = %+v", runs)
	}
	if run := core.store.load(runs[0].ID); !strings.HasPrefix(run.Reason, "its process could not start: ") || run.EndedAt == nil {
		t.Errorf("abandoned run = %q, ended %v", run.Reason, run.EndedAt)
	}
}

func TestStartRunRefuses(t *testing.T) {
	core, project := engineSetup(t)
	writeFiles(t, project, map[string]string{
		".swarm/chains/empty.json":  `{"title": "Empty", "steps": []}`,
		".swarm/chains/broken.json": `{"title": "Broken", "steps": [{"id": "x", "title": "X", "tool": "nope"}]}`,
	})
	tests := []struct {
		chain string
		want  string
	}{
		{"missing", "no chain named missing"},
		{"empty", "empty has no steps yet"},
		{"broken", "fix the chain first"},
		{"hello", "Value is required"},
	}
	for _, tt := range tests {
		if _, err := core.StartRun(project, tt.chain, map[string]string{"value": " "}); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("StartRun(%s) = %v, want %q", tt.chain, err, tt.want)
		}
	}
	full := openCore(t.TempDir())
	writeFiles(t, full.home, map[string]string{"runs": "not a folder"})
	if _, err := full.StartRun(project, "slow", nil); err == nil {
		t.Error("started without a runs folder")
	}
	writeFiles(t, project, map[string]string{".swarm/settings.json": "{"})
	if _, err := core.StartRun(project, "hello", nil); err == nil {
		t.Error("started with broken settings")
	}
}

func TestWorkRunsAQueuedRunInProcess(t *testing.T) {
	core, project := engineSetup(t)
	run := queueRun(t, core, project, "hello", map[string]string{"value": "hi"})
	resetWorkSignals(t)
	if err := work([]string{"-home", core.home, run.ID}); err != nil {
		t.Fatal(err)
	}
	done := core.store.load(run.ID)
	if done.Status != "done" || done.PID != os.Getpid() || len(done.Steps) != 2 {
		t.Fatalf("run = %+v", done)
	}
	if got := done.Steps[0].Output["output"]; got != "hi" {
		t.Errorf("say = %v", got)
	}
	if got := done.Steps[1].Output["output"]; got != run.ID+" "+project {
		t.Errorf("who = %v", got)
	}
}

func TestWorkRefuses(t *testing.T) {
	core, project := engineSetup(t)
	if err := work([]string{"-unknown"}); err == nil {
		t.Error("accepted an unknown flag")
	}
	if err := work([]string{"-home", core.home, "not-an-id"}); err == nil {
		t.Error("worked on a bad id")
	}
	started := queueRun(t, core, project, "hello", nil)
	core.store.update(started, func() { started.PID = 1 })
	if err := work([]string{"-home", core.home, started.ID}); err == nil || !strings.Contains(err.Error(), "already started") {
		t.Errorf("worked on a started run: %v", err)
	}
	gone := queueRun(t, core, project, "gone", nil)
	if err := work([]string{"-home", core.home, gone.ID}); err != nil {
		t.Errorf("missing chain: %v", err)
	}
	if run := core.store.load(gone.ID); run.Status != "failed" || run.Reason != "the chain gone is missing or broken" {
		t.Errorf("missing chain run = %s %q", run.Status, run.Reason)
	}
	writeFiles(t, project, map[string]string{".swarm/settings.json": "{"})
	broken := queueRun(t, core, project, "hello", nil)
	if err := work([]string{"-home", core.home, broken.ID}); err == nil {
		t.Error("worked with broken settings")
	}
	if run := core.store.load(broken.ID); run.Status != "failed" || !strings.Contains(run.Reason, "settings.json") {
		t.Errorf("broken settings run = %s %q", run.Status, run.Reason)
	}
}

func TestWorkTakesDecisionsFromTheRunFolder(t *testing.T) {
	shorten(t, &listenEvery, 10*time.Millisecond)
	core, project := engineSetup(t)
	run := queueRun(t, core, project, "ask", nil)
	errs := runWork(t, core, run.ID)
	waiting := awaitRun(t, core, run.ID, stepWaiting(0))
	check := waiting.Steps[1]
	if check.Question != "Keep draft?" || len(check.Artifacts) != 1 || check.Artifacts[0].Name != "notes.md" || check.Artifacts[0].Kind != "markdown" {
		t.Errorf("waiting step = %+v", check)
	}
	if err := core.Decide(run.ID, decision{Choice: "revise", Note: "shorter"}); err != nil {
		t.Fatal(err)
	}
	awaitRun(t, core, run.ID, stepWaiting(1))
	if err := core.Decide(run.ID, decision{Choice: "approve", Note: "fine"}); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	done := core.store.load(run.ID)
	if done.Status != "done" || done.Feedback != "shorter" || done.Steps[0].SentBack != "shorter" || done.Steps[1].Headline != "Approved: fine" {
		t.Errorf("run = %s, feedback %q, steps %+v %+v", done.Status, done.Feedback, done.Steps[0], done.Steps[1])
	}
	if core.store.hasSignal(run.ID, decisionFile) {
		t.Error("the decision file was left behind")
	}
}

func TestWorkStopsOnTheCancelFile(t *testing.T) {
	shorten(t, &listenEvery, 10*time.Millisecond)
	core, project := engineSetup(t)
	run := queueRun(t, core, project, "slow", nil)
	errs := runWork(t, core, run.ID)
	awaitRun(t, core, run.ID, func(r *Run) bool { return len(r.Steps) == 1 && r.Steps[0].Status == "running" })
	if err := core.Stop(run.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errs:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the run did not stop")
	}
	done := core.store.load(run.ID)
	if done.Status != "stopped" || done.Steps[0].Status != "failed" || !strings.Contains(done.Reason, "You stopped the run during: Wait") {
		t.Errorf("run = %s %q, step %s", done.Status, done.Reason, done.Steps[0].Status)
	}
}
