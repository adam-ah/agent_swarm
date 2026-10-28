package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func shorten[T any](t *testing.T, setting *T, value T) {
	t.Helper()
	old := *setting
	*setting = value
	t.Cleanup(func() { *setting = old })
}

func quickStops(t *testing.T) {
	t.Helper()
	shorten(t, &stopGrace, 150*time.Millisecond)
	shorten(t, &exitGrace, 50*time.Millisecond)
	shorten(t, &answerGrace, 20*time.Millisecond)
	shorten(t, &unkillable, time.Second)
}

func recordedCall(t *testing.T, folder, log string) Call {
	t.Helper()
	var call Call
	raw, err := os.ReadFile(filepath.Join(folder, log+".call.json"))
	if err != nil || json.Unmarshal(raw, &call) != nil {
		t.Fatalf("call record %s: %v", log, err)
	}
	return call
}

func runProcesses(folder string) []string {
	var left []string
	for _, p := range scanProcs() {
		if p.run == runMark(folder) {
			left = append(left, p.command)
		}
	}
	return left
}

func toolRun(t *testing.T, run string, step Step) (*Run, string) {
	t.Helper()
	lib := sequenceLibrary(t)
	lib.Tools["try"] = Tool{Run: run}
	step.ID, step.Title, step.Tool = "try", "Try", "try"
	return execute(t, lib, &Chain{Name: "main", Steps: []Step{step}})
}

func contains(list []string, want string) bool {
	return strings.Contains(strings.Join(list, "\n"), want)
}

func TestToolThatSleepsIsStoppedWhenIdle(t *testing.T) {
	quickStops(t)
	run, folder := toolRun(t, "sleep 3601", Step{Stall: "100ms"})
	step := run.Steps[0]
	if want := "no output and no CPU use for 100ms, so swarm stopped it"; step.Status != "failed" || step.Error != want {
		t.Fatalf("step = %s %q, want failed with %q", step.Status, step.Error, want)
	}
	call := recordedCall(t, folder, "try")
	if !contains(call.Stopped, "sleep 3601") || call.Limits != "stopped after 100ms with no output and no CPU use, or after 2h in all" {
		t.Errorf("call stopped %v, limits %q", call.Stopped, call.Limits)
	}
	if left := runProcesses(folder); len(left) > 0 {
		t.Errorf("still running: %v", left)
	}
}

func TestToolThatKeepsTalkingHitsItsTimeLimit(t *testing.T) {
	quickStops(t)
	run, folder := toolRun(t, "while true; do echo tick; sleep 0.01; done", Step{Timeout: "80ms", Stall: "50ms"})
	if step := run.Steps[0]; step.Error != "it ran longer than its time limit of 80ms, so swarm stopped it" {
		t.Fatalf("step = %s %q", step.Status, step.Error)
	}
	if left := runProcesses(folder); len(left) > 0 {
		t.Errorf("still running: %v", left)
	}
}

func TestToolThatBusiesItselfIsNotIdle(t *testing.T) {
	quickStops(t)
	run, _ := toolRun(t, "timeout 0.2 sh -c 'while :; do :; done'; echo done", Step{Stall: "60ms"})
	if step := run.Steps[0]; step.Status != "done" || step.Output["output"] != "done" {
		t.Errorf("a busy silent tool = %s %q, want done", step.Status, step.Error)
	}
}

func TestToolThatLeavesServersBehind(t *testing.T) {
	quickStops(t)
	started := time.Now()
	run, folder := toolRun(t, "setsid sleep 3602 > /dev/null 2>&1 & sleep 3603 & echo started", Step{})
	if step := run.Steps[0]; step.Status != "done" || step.Output["output"] != "started" {
		t.Fatalf("step = %s %v (%s)", step.Status, step.Output, step.Error)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("waited %s for the server holding its output", took)
	}
	call := recordedCall(t, folder, "try")
	if !contains(call.Stopped, "sleep 3602") || !contains(call.Stopped, "sleep 3603") {
		t.Errorf("stopped %v, want both servers", call.Stopped)
	}
	if left := runProcesses(folder); len(left) > 0 {
		t.Errorf("still running: %v", left)
	}
}

func TestForegroundServerWithABackgroundChild(t *testing.T) {
	quickStops(t)
	run, folder := toolRun(t, "sleep 3604 & exec sleep 3605", Step{Stall: "100ms"})
	if step := run.Steps[0]; step.Status != "failed" {
		t.Fatalf("step = %s, want failed", step.Status)
	}
	call := recordedCall(t, folder, "try")
	if !contains(call.Stopped, "sleep 3604") || !contains(call.Stopped, "sleep 3605") {
		t.Errorf("stopped %v, want the server and its child", call.Stopped)
	}
	if left := runProcesses(folder); len(left) > 0 {
		t.Errorf("still running: %v", left)
	}
}

func TestToolThatIgnoresSIGTERMIsKilled(t *testing.T) {
	quickStops(t)
	shorten(t, &stopGrace, 50*time.Millisecond)
	run, folder := toolRun(t, `trap "" TERM; sleep 3606`, Step{Stall: "50ms"})
	if step := run.Steps[0]; step.Status != "failed" {
		t.Fatalf("step = %s, want failed", step.Status)
	}
	if left := runProcesses(folder); len(left) > 0 {
		t.Errorf("still running after SIGKILL: %v", left)
	}
}

func TestKeepUntilKeepsAServerForLaterSteps(t *testing.T) {
	quickStops(t)
	lib := sequenceLibrary(t)
	lib.Tools["up"] = Tool{Run: "setsid sleep 3607 > /dev/null 2>&1 & echo $! > {{run_folder}}/pid"}
	lib.Tools["check"] = Tool{Run: "kill -0 $(cat {{run_folder}}/pid) 2>/dev/null && echo alive || echo gone"}
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "up", Title: "Up", Tool: "up", KeepUntil: "down"},
		{ID: "during", Title: "During", Tool: "check"},
		{ID: "down", Title: "Down", Tool: "say", With: map[string]string{"value": "down"}, When: "during.output == never"},
		{ID: "after", Title: "After", Tool: "check"},
	}}
	run, folder := execute(t, lib, chain)
	if got := []any{run.Steps[1].Output["output"], run.Steps[2].Status, run.Steps[3].Output["output"]}; got[0] != "alive" || got[1] != "skipped" || got[2] != "gone" {
		t.Fatalf("during, down, after = %v", got)
	}
	call := recordedCall(t, folder, "up")
	if call.KeptUntil != "down" || !contains(call.Kept, "sleep 3607") || len(call.Stopped) > 0 {
		t.Errorf("up kept %v until %q, stopped %v", call.Kept, call.KeptUntil, call.Stopped)
	}
	if !contains(run.Stopped, "after Down: ") || !contains(run.Stopped, "sleep 3607") {
		t.Errorf("run stopped %v", run.Stopped)
	}
}

func TestEndRunStopsWhateverIsLeft(t *testing.T) {
	quickStops(t)
	store := testStore(t)
	run := saveTestRun(t, store, &Run{ID: "1028-120000", Status: "running"})
	cmd := exec.Command("sleep", "3608")
	cmd.Env = append(os.Environ(), runVar+runMark(store.runDir(run.ID)))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	r := &Runner{store: store}
	stopped := r.endRun(run)
	_ = cmd.Wait()
	if !contains(stopped, "sleep 3608") {
		t.Errorf("stopped %v", stopped)
	}
}

func TestSweepStopsProcessesOfRunsThatEnded(t *testing.T) {
	quickStops(t)
	store := testStore(t)
	ended := saveTestRun(t, store, &Run{ID: "1028-120001", Chain: "c", Status: "failed"})
	going := saveTestRun(t, store, &Run{ID: "1028-120002", Chain: "c", Status: "running", PID: os.Getpid()})
	cmds := make([]*exec.Cmd, 0, 2)
	for _, c := range []struct{ run, arg string }{{ended.ID, "3609"}, {going.ID, "3610"}} {
		cmd := exec.Command("sleep", c.arg)
		cmd.Env = append(os.Environ(), runVar+runMark(store.runDir(c.run)))
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, cmd)
	}
	t.Cleanup(func() {
		_ = cmds[1].Process.Kill()
		_ = cmds[1].Wait()
	})
	stopped := store.sweep()
	_ = cmds[0].Wait()
	if !contains(stopped, "sleep 3609") || contains(stopped, "sleep 3610") {
		t.Fatalf("swept %v, want only the ended run's process", stopped)
	}
	if run, _ := store.read(ended.ID); !contains(run.Stopped, "after the run had ended: ") {
		t.Errorf("ended run's record: %v", run.Stopped)
	}
}

func TestReapOrphansStopsProcessesOfEndedRunsUntilAsked(t *testing.T) {
	quickStops(t)
	store := testStore(t)
	ended := saveTestRun(t, store, &Run{ID: "1028-120003", Chain: "c", Status: "done"})
	orphan := exec.Command("sleep", "3611")
	orphan.Env = append(os.Environ(), runVar+runMark(store.runDir(ended.ID)))
	if err := orphan.Start(); err != nil {
		t.Fatal(err)
	}
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		reapOrphans(store, quit, time.Millisecond)
		close(done)
	}()
	_ = orphan.Wait()
	close(quit)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reapOrphans kept going after quit")
	}
}
