package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func finishedRun(t *testing.T, core *Core, project, chain string, steps []*StepRun) string {
	t.Helper()
	id, err := core.store.claim()
	if err != nil {
		t.Fatal(err)
	}
	ended := time.Now()
	run := &Run{ID: id, Chain: chain, Project: project, Status: "failed", StartedAt: ended, EndedAt: &ended, Steps: steps}
	core.store.update(run, func() {})
	return id
}

func flakySteps() []*StepRun {
	return []*StepRun{
		{ID: "say", Path: "say", Kind: "tool", Uses: "say", Status: "done"},
		{ID: "flaky", Path: "flaky", Kind: "tool", Uses: "flaky", Status: "failed"},
	}
}

func TestRerunFromScratchStartsAnotherRun(t *testing.T) {
	core, project := engineSetup(t)
	first, err := core.StartRun(project, "hello", map[string]string{"value": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	killWorkerOnCleanup(t, core, first.ID)
	awaitRunEnd(t, core, first.ID)
	id, err := core.Rerun(first.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	killWorkerOnCleanup(t, core, id)
	if id == first.ID {
		t.Fatal("rerun from scratch reused the run")
	}
	if done := awaitRunEnd(t, core, id); done.Status != "done" || done.Inputs["value"] != "hi" || done.Steps[0].Output["output"] != "hi" {
		t.Errorf("rerun = %s %v (%s)", done.Status, done.Inputs, done.Reason)
	}
}

func TestRerunFromTheFailedStepInAWorker(t *testing.T) {
	core, project := engineSetup(t)
	first, err := core.StartRun(project, "flaky", nil)
	if err != nil {
		t.Fatal(err)
	}
	killWorkerOnCleanup(t, core, first.ID)
	if failed := awaitRunEnd(t, core, first.ID); failed.Status != "failed" {
		t.Fatalf("first attempt = %s (%s)", failed.Status, failed.Reason)
	}
	writeFiles(t, project, map[string]string{"fixed": ""})
	if err = core.store.signal(first.ID, cancelFile, nil); err != nil {
		t.Fatal(err)
	}
	id, err := core.Rerun(first.ID, false)
	if err != nil || id != first.ID {
		t.Fatalf("rerun = %s, %v", id, err)
	}
	done := awaitRunEnd(t, core, id)
	if done.Status != "done" || done.ResumedAt == nil || done.Steps[0].Round != 0 || done.Steps[1].Round != 1 || len(done.Steps[1].Earlier) != 1 {
		t.Errorf("rerun = %s (%s), steps %+v %+v", done.Status, done.Reason, done.Steps[0], done.Steps[1])
	}
	if tries, _ := os.ReadFile(filepath.Join(core.store.runDir(id), "tries")); string(tries) != "x\nx\n" {
		t.Errorf("tries = %q", tries)
	}
}

func TestRerunRefuses(t *testing.T) {
	core, project := engineSetup(t)
	broken := t.TempDir()
	writeFiles(t, broken, map[string]string{".swarm/settings.json": "{"})
	writeFiles(t, project, map[string]string{".swarm/chains/bad.json": `{"title": "Bad", "steps": [{"id": "x", "title": "X", "tool": "nope"}]}`})
	going := fakeRunAt(t, core, "waiting", "")
	tests := []struct {
		id      string
		scratch bool
		want    string
	}{
		{"0101-000000", false, "no run 0101-000000"},
		{going, false, "the run is still going"},
		{finishedRun(t, core, broken, "flaky", flakySteps()), false, "settings.json"},
		{finishedRun(t, core, project, "gone", nil), false, "no chain named gone"},
		{finishedRun(t, core, project, "flaky", flakySteps()[:1]), false, "the chain's steps changed since this run"},
		{finishedRun(t, core, project, "bad", nil), false, "fix the chain first"},
		{finishedRun(t, core, project, "bad", nil), true, "fix the chain first"},
	}
	for _, tt := range tests {
		if id, err := core.Rerun(tt.id, tt.scratch); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("Rerun(%s, %v) = %s, %v, want %q", tt.id, tt.scratch, id, err, tt.want)
		}
	}
}

func TestResumeRefuses(t *testing.T) {
	core, project := engineSetup(t)
	lib, err := core.Library(project)
	if err != nil {
		t.Fatal(err)
	}
	chain := lib.Chains["flaky"]
	if err := core.runner.resume(lib, chain, "0101-000000"); err == nil {
		t.Error("resumed a missing run")
	}
	stuck := finishedRun(t, core, project, "flaky", flakySteps())
	if err := os.MkdirAll(filepath.Join(core.store.runDir(stuck), decisionFile, "inside"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := core.runner.resume(lib, chain, stuck); err == nil {
		t.Error("resumed a run whose signals could not be cleared")
	}
	core.runner.swarm = filepath.Join(t.TempDir(), "missing")
	unstartable := finishedRun(t, core, project, "flaky", flakySteps())
	if _, err := core.Rerun(unstartable, false); err == nil {
		t.Fatal("resumed with a worker that does not exist")
	}
	if run := core.store.load(unstartable); run.Status != "failed" || !strings.HasPrefix(run.Reason, "its process could not start") {
		t.Errorf("abandoned run = %s %q", run.Status, run.Reason)
	}
	noLog := finishedRun(t, core, project, "flaky", flakySteps())
	if err := os.MkdirAll(filepath.Join(core.store.runDir(noLog), "worker.log"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := core.Rerun(noLog, false); err == nil {
		t.Error("resumed without a worker log")
	}
}

func TestDeleteRun(t *testing.T) {
	core, project := engineSetup(t)
	if err := core.DeleteRun("0101-000000"); err == nil {
		t.Error("deleted a missing run")
	}
	if err := core.DeleteRun(fakeRunAt(t, core, "waiting", "")); err == nil || !strings.Contains(err.Error(), "stop the run") {
		t.Errorf("deleted a run in progress: %v", err)
	}
	id := finishedRun(t, core, project, "flaky", flakySteps())
	if err := core.DeleteRun(id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(core.store.runDir(id)); !os.IsNotExist(err) {
		t.Errorf("run folder still there: %v", err)
	}
}
