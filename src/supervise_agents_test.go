package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestAgentStepEndsWhenItAnswers(t *testing.T) {
	quickStops(t)
	fakeClaude(t, `setsid sleep 3621 &
`+untilRunning("sleep 3621")+`
echo '{"type":"result","result":"","structured_output":{"summary":"answered"},"session_id":"s1"}'
echo '{"type":"result","result":"","structured_output":{"summary":"later"},"session_id":"s1"}'
sleep 3622
`)
	started := time.Now()
	run, folder := execute(t, sequenceLibrary(t), agentChain(""))

	if step := run.Steps[0]; step.Status != "done" || step.Output["summary"] != "answered" {
		t.Fatalf("step = %s %v (%s), want done with the first answer", step.Status, step.Output, step.Error)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("the step waited %s for processes the agent left running", took)
	}
	call := recordedCall(t, folder, "do")
	if !contains(call.Stopped, "sleep 3621") || !contains(call.Stopped, "sleep 3622") {
		t.Errorf("stopped %v, want both sleeps", call.Stopped)
	}
	if call.Limits != "stopped after 15m with nothing new from the agent, or after 2h in all" {
		t.Errorf("limits %q", call.Limits)
	}
	if left := runProcesses(folder); len(left) > 0 {
		t.Errorf("still running: %v", left)
	}
}

func TestAgentStepWaitsPastItsAnswerToABackgroundTaskNotice(t *testing.T) {
	quickStops(t)
	fakeClaude(t, `echo '{"type":"system","subtype":"task_notification","status":"stopped","summary":"Background shell command didn'"'"'t finish before the previous session ended"}'
echo '{"type":"result","subtype":"success","result":"","num_turns":0,"origin":{"kind":"task-notification"},"session_id":"s1"}'
sleep 0.3
echo '{"type":"result","result":"","structured_output":{"summary":"fixed all seven"},"session_id":"s1"}'
`)
	run, _ := execute(t, sequenceLibrary(t), agentChain(""))
	if step := run.Steps[0]; step.Status != "done" || step.Output["summary"] != "fixed all seven" {
		t.Fatalf("step = %s %v (%s), want the answer to the prompt", step.Status, step.Output, step.Error)
	}
}

func TestAgentStepStopsWhenNothingHappens(t *testing.T) {
	quickStops(t)
	fakeClaude(t, `printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"npx vitest run\nmore"}}]}}'
sleep 3623
`)
	chain := agentChain("")
	chain.Steps[0].Stall = "100ms"
	run, folder := execute(t, sequenceLibrary(t), chain)

	want := "claude ended without a result: nothing new from the agent for 100ms, so swarm stopped it; its last action was Bash: npx vitest run"
	if step := run.Steps[0]; step.Status != "failed" || step.Error != want {
		t.Fatalf("step = %s %q, want failed with %q", step.Status, step.Error, want)
	}
	if left := runProcesses(folder); len(left) > 0 {
		t.Errorf("still running: %v", left)
	}
}

func TestAgentStepHitsItsTimeLimit(t *testing.T) {
	quickStops(t)
	fakeClaude(t, `while true; do echo '{"type":"system"}'; sleep 0.01; done`)
	chain := agentChain("")
	chain.Steps[0].Timeout = "80ms"
	run, _ := execute(t, sequenceLibrary(t), chain)
	if step := run.Steps[0]; !strings.Contains(step.Error, "it ran longer than its time limit of 80ms, so swarm stopped it; its last action was none yet") {
		t.Errorf("step = %s %q", step.Status, step.Error)
	}
}

func TestCustomAgentLeavingItsOutputOpen(t *testing.T) {
	quickStops(t)
	lib := sequenceLibrary(t)
	lib.Settings.Agents["quick"] = AgentCLI{Command: `sleep 3624 & ` + untilRunning("sleep 3624") + `echo '{"summary": "quick"}'`}
	started := time.Now()
	run, folder := execute(t, lib, agentChain("quick"))
	if step := run.Steps[0]; step.Status != "done" || step.Output["summary"] != "quick" {
		t.Fatalf("step = %s %v (%s)", step.Status, step.Output, step.Error)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("waited %s for a background process holding its output", took)
	}
	if call := recordedCall(t, folder, "do"); !contains(call.Stopped, "sleep 3624") {
		t.Errorf("stopped %v", call.Stopped)
	}
}

func TestAgentWithAnOverlongLineIsStopped(t *testing.T) {
	quickStops(t)
	shorten(t, &longestLine, 1024)
	fakeClaude(t, `head -c 5000 /dev/zero | tr '\0' x; echo; sleep 3625`)
	run, folder := execute(t, sequenceLibrary(t), agentChain(""))
	if step := run.Steps[0]; !strings.Contains(step.Error, "swarm could not read its output (bufio.Scanner: token too long), so swarm stopped it") {
		t.Errorf("step = %s %q", step.Status, step.Error)
	}
	if left := runProcesses(folder); len(left) > 0 {
		t.Errorf("still running: %v", left)
	}
}

func TestStoppingTheRunStopsTheAgentsProcesses(t *testing.T) {
	quickStops(t)
	fakeClaude(t, `setsid sleep 3626 & sleep 3627`)
	store := testStore(t)
	id, err := store.claim()
	if err != nil {
		t.Fatal(err)
	}
	lib := sequenceLibrary(t)
	chain := agentChain("")
	run := &Run{ID: id, Project: lib.Project, Status: "running", Steps: expand(lib, chain, "")}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	r := &Runner{store: store, swarm: "true", home: t.TempDir()}
	r.execute(ctx, lib, chain, run, newScope(map[string]string{"run_folder": store.logDir(run)}))
	if run.Status != "stopped" {
		t.Errorf("run = %s (%s), want stopped", run.Status, run.Reason)
	}
	if left := runProcesses(store.logDir(run)); len(left) > 0 {
		t.Errorf("still running: %v", left)
	}
}
