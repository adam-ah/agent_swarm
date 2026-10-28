package main

import (
	"context"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLimits(t *testing.T) {
	cases := []struct {
		step     Step
		stall    time.Duration
		cpu      bool
		describe string
		tick     time.Duration
	}{
		{Step{}, toolStall, true, "stopped after 10m with no output and no CPU use, or after 2h in all", 2 * time.Second},
		{Step{}, agentStall, false, "stopped after 15m with nothing new from the agent, or after 2h in all", 2 * time.Second},
		{Step{Stall: "0", Timeout: "90m"}, toolStall, true, "stopped after 1h30m in all", 2 * time.Second},
		{Step{Stall: "0", Timeout: "0"}, agentStall, false, "never stopped by swarm", 2 * time.Second},
		{Step{Stall: "1s", Timeout: "soon"}, agentStall, false, "stopped after 1s with nothing new from the agent, or after 2h in all", 200 * time.Millisecond},
		{Step{Stall: "1ms"}, toolStall, true, "stopped after 1ms with no output and no CPU use, or after 2h in all", 10 * time.Millisecond},
	}
	for _, c := range cases {
		l := limitsFor(c.step, c.stall, c.cpu)
		if got := l.describe(); got != c.describe {
			t.Errorf("%+v describes as %q, want %q", c.step, got, c.describe)
		}
		if got := l.tick(); got != c.tick {
			t.Errorf("%+v ticks every %s, want %s", c.step, got, c.tick)
		}
	}
}

func TestDurationText(t *testing.T) {
	for d, want := range map[time.Duration]string{0: "", 15 * time.Minute: "15m", time.Hour: "1h", 90 * time.Minute: "1h30m", 10 * time.Second: "10s", 200 * time.Millisecond: "200ms"} {
		if got := durationText(d); got != want {
			t.Errorf("durationText(%s) = %q, want %q", d, got, want)
		}
	}
}

func TestActionLabel(t *testing.T) {
	for input, want := range map[string]string{
		`{"command": "ls -la\nmore"}`:      "ls -la",
		`{"file_path": "/src/main.go"}`:    "/src/main.go",
		`{"other": 1}`:                     `{"other": 1}`,
		"plain text":                       "plain text",
		strings.Repeat("x", 200):           strings.Repeat("x", 160) + "…",
		`{"pattern": "TODO", "path": "."}`: ".",
	} {
		if got := actionLabel(input); got != want {
			t.Errorf("actionLabel(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLastActionWithoutActions(t *testing.T) {
	log := &callLog{}
	if got := log.lastAction(); got != "none yet" {
		t.Errorf("lastAction = %q", got)
	}
	log.call.Actions = []*Action{{Tool: "Read", Input: `{"file_path": "a.go"}`}}
	if got := log.lastAction(); got != "Read: a.go" {
		t.Errorf("lastAction = %q", got)
	}
}

func TestCaptureKeepsTheTail(t *testing.T) {
	shorten(t, &captureLimit, 10)
	c := &capture{g: &guard{}}
	for range 3 {
		if _, err := c.Write([]byte("0123456789")); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.String(); got != "[the first 20 bytes were dropped]\n0123456789" || string(c.Bytes()) != "0123456789" {
		t.Errorf("capture = %q", got)
	}
	small := &capture{g: &guard{}}
	_, _ = small.Write([]byte("hi"))
	if small.String() != "hi" {
		t.Errorf("small capture = %q", small.String())
	}
}

func TestUnmarkedDropsSwarmMarks(t *testing.T) {
	env := []string{"PATH=/bin", runVar + "/a", stepVar + "/a/b", workerVar + "/a", "HOME=/h"}
	if got := unmarked(env); !reflect.DeepEqual(got, []string{"PATH=/bin", "HOME=/h"}) {
		t.Errorf("unmarked = %v", got)
	}
	if len(env) != 5 {
		t.Error("unmarked changed its argument")
	}
}

func TestWaitEscalatesToSIGKILL(t *testing.T) {
	shorten(t, &stopGrace, 30*time.Millisecond)
	r := &Runner{}
	cmd := exec.Command("bash", "-c", `trap "" TERM; sleep 3641`)
	if err := r.launch(cmd); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.wait(ctx, cmd); err == nil || !strings.Contains(err.Error(), "killed") {
		t.Errorf("wait = %v, want it killed", err)
	}
}

type stuckWriter struct{ entered, release chan struct{} }

func (w stuckWriter) Write(p []byte) (int, error) {
	close(w.entered)
	<-w.release
	return len(p), nil
}

func TestWaitGivesUpOnAProcessThatWillNotEnd(t *testing.T) {
	shorten(t, &stopGrace, 10*time.Millisecond)
	shorten(t, &unkillable, 30*time.Millisecond)
	stuck := stuckWriter{entered: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(func() { close(stuck.release) })
	r := &Runner{}
	cmd := exec.Command("sh", "-c", "echo hi; sleep 3642")
	cmd.Stdout = stuck
	if err := r.launch(cmd); err != nil {
		t.Fatal(err)
	}
	<-stuck.entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := r.wait(ctx, cmd)
	if want := fmt.Sprintf("process %d did not end even after SIGKILL, so swarm stopped waiting for it", cmd.Process.Pid); err == nil || err.Error() != want {
		t.Errorf("wait = %v, want %q", err, want)
	}
}

func TestKillAllGivesUpOnWhatWillNotDie(t *testing.T) {
	shorten(t, &stopGrace, 10*time.Millisecond)
	ghost := 1 << 30
	stopped := killAll(func(procs) map[int]bool { return map[int]bool{ghost: true} })
	if !contains(stopped, fmt.Sprintf("%d  (could not be stopped)", ghost)) {
		t.Errorf("stopped %q", stopped)
	}
}

func TestCheckLimitsAndKeepUntil(t *testing.T) {
	lib := sequenceLibrary(t)
	agent := func(change func(*Step)) *Chain {
		chain := agentChain("")
		change(&chain.Steps[0])
		return chain
	}
	tools := func(steps ...Step) *Chain { return &Chain{Name: "main", Steps: steps} }
	up := Step{ID: "up", Title: "Up", Tool: "say", KeepUntil: "down"}
	down := Step{ID: "down", Title: "Down", Tool: "say"}
	cases := []struct {
		chain *Chain
		want  string
	}{
		{agent(func(s *Step) { s.Stall, s.Timeout = "30m", "2h" }), ""},
		{agent(func(s *Step) { s.Stall = "0" }), ""},
		{agent(func(s *Step) { s.Stall = "soon" }), `step "do": stall is a duration such as 30m or 1h, or 0 for no limit, not "soon"`},
		{agent(func(s *Step) { s.Timeout = "-1m" }), `step "do": timeout is a duration such as 30m or 1h, or 0 for no limit, not "-1m"`},
		{agent(func(s *Step) { s.KeepUntil = "do" }), `step "do": keep_until only applies to tool steps`},
		{tools(Step{ID: "ask", Title: "Ask", User: "ok?", Timeout: "5m"}), `step "ask": timeout only applies to tool and agent steps`},
		{tools(up, down), ""},
		{tools(down, Step{ID: "up", Title: "Up", Tool: "say", KeepUntil: "down"}), `step "up": keep_until must name a later step in this chain, not "down"`},
		{tools(Step{ID: "up", Title: "Up", Tool: "say", KeepUntil: "up"}), `step "up": keep_until must name a later step in this chain, not "up"`},
		{tools(Step{ID: "up", Title: "Up", Tool: "say", KeepUntil: "ghost"}), `step "up": keep_until must name a later step in this chain, not "ghost"`},
	}
	for _, c := range cases {
		if got := strings.Join(lib.check(c.chain), "; "); got != c.want {
			t.Errorf("%+v: problems %q, want %q", c.chain.Steps, got, c.want)
		}
	}
}
