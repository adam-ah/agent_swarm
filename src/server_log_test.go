package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func testConsole() (*console, *bytes.Buffer) {
	var out bytes.Buffer
	c := newConsole(&out)
	c.now = func() time.Time { return time.Date(2026, 11, 4, 14, 3, 12, 0, time.UTC) }
	return c, &out
}

func takeLines(t *testing.T, out *bytes.Buffer, want ...string) {
	t.Helper()
	got := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	out.Reset()
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d:\n%s", len(got), len(want), strings.Join(got, "\n"))
	}
	for i := range want {
		if line := strings.TrimPrefix(got[i], "2026-11-04 14:03:12 "); line != want[i] {
			t.Errorf("line %d:\n got %s\nwant %s", i, got[i], want[i])
		}
	}
}

func TestConsoleLineFormat(t *testing.T) {
	c, out := testConsole()
	c.infof("server started on %s", "http://x")
	c.warnf("w")
	c.errorf("e")
	if got := out.String(); got != "2026-11-04 14:03:12 [INFO] server started on http://x\n2026-11-04 14:03:12 [WARN] w\n2026-11-04 14:03:12 [ERROR] e\n" {
		t.Errorf("got %q", got)
	}
	var none *console
	none.infof("nothing")
}

func TestRunWatchLogsRunLifecycle(t *testing.T) {
	store := testStore(t)
	c, out := testConsole()
	w := newRunWatch(store, c, "http://x")
	saveTestRun(t, store, &Run{ID: "1104-100000", Chain: "old", Status: "done"})
	w.scan(false)
	w.reportGoing()
	takeLines(t, out, "[INFO] idle: no runs going")

	start := time.Date(2026, 11, 4, 14, 0, 0, 0, time.UTC)
	later := start.Add(41 * time.Second)
	plan := &StepRun{ID: "plan", Path: "plan", Kind: "agent", Uses: "plan", Runner: "sonnet", Status: "running", StartedAt: &start}
	ok := &StepRun{ID: "ok", Path: "ok", Kind: "user", Status: "pending"}
	run := saveTestRun(t, store, &Run{ID: "1104-140000", Chain: "make-change", Project: "/p", Status: "running", PID: os.Getpid(), StartedAt: start, Steps: []*StepRun{plan, ok}})
	w.scan(true)
	takeLines(t, out,
		"[INFO] run 1104-140000 started (make-change, /p) http://x/#/run/1104-140000",
		"[INFO] run 1104-140000 step plan started (agent, plan, sonnet)")

	store.update(run, func() {
		plan.Status, plan.EndedAt, plan.Cost, plan.Headline = "done", &later, 0.18, "Plan the change"
		ok.Status, ok.Question = "waiting", "Keep it?"
		run.Status = "waiting"
	})
	w.scan(true)
	takeLines(t, out,
		"[INFO] run 1104-140000 step plan done (41s, $0.18): Plan the change",
		"[INFO] run 1104-140000 step ok waiting (http://x/#/run/1104-140000): Keep it?")
	w.scan(true)
	if out.Len() != 0 {
		t.Errorf("unchanged runs logged: %s", out)
	}

	store.update(run, func() {
		ok.Status, ok.Error = "failed", "no answer\nmore"
		run.Status, run.Reason, run.EndedAt, run.Cost = "failed", "OK failed", &later, 0.18
	})
	w.scan(true)
	takeLines(t, out,
		"[ERROR] run 1104-140000 step ok failed: no answer",
		"[ERROR] run 1104-140000 failed (make-change, 41s, $0.18): OK failed",
		"[INFO] idle: no runs going")

	store.update(run, func() {
		run.Status, run.RerunFrom, run.EndedAt = "running", "ok", nil
		ok.Status, ok.Error = "running", ""
	})
	w.scan(true)
	takeLines(t, out,
		"[INFO] run 1104-140000 rerun from ok (make-change)",
		"[INFO] run 1104-140000 step ok started (user)")
	w.reportGoing()
	takeLines(t, out, "[INFO] 1 runs going: 1104-140000")

	if err := os.RemoveAll(store.runDir("1104-100000")); err != nil {
		t.Fatal(err)
	}
	w.scan(true)
	takeLines(t, out, "[INFO] run 1104-100000 deleted")
}

func TestRunWatchLogsARunThatEndsBetweenScans(t *testing.T) {
	store := testStore(t)
	c, out := testConsole()
	w := newRunWatch(store, c, "http://x")
	w.scan(false)
	start := time.Date(2026, 11, 4, 14, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Second)
	saveTestRun(t, store, &Run{ID: "1104-140000", Chain: "hello", Project: "/p", Status: "done", StartedAt: start, EndedAt: &end,
		Steps: []*StepRun{{ID: "greet", Path: "greet", Kind: "tool", Status: "done", StartedAt: &start, EndedAt: &end, Headline: "hello"}}})
	w.scan(true)
	takeLines(t, out,
		"[INFO] run 1104-140000 started (hello, /p) http://x/#/run/1104-140000",
		"[INFO] run 1104-140000 step greet done (2s): hello",
		"[INFO] run 1104-140000 done (hello, 2s)",
		"[INFO] idle: no runs going")
}

func TestRequestLogShowsWritesAndFailures(t *testing.T) {
	core, project := surfaceProject(t)
	c, out := testConsole()
	srv := &server{core: core, quit: make(chan struct{}), log: c}
	page := httptest.NewServer(srv.routes())
	defer page.Close()
	send := func(method, path, body string) {
		req, _ := http.NewRequestWithContext(context.Background(), method, page.URL+path, strings.NewReader(body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	send(http.MethodGet, "/api/projects", "")
	send(http.MethodPost, "/api/check?project="+project, `{}`)
	send(http.MethodPost, "/api/markdown", `{"text":"x"}`)
	if out.Len() != 0 {
		t.Errorf("reads logged: %s", out)
	}
	send(http.MethodPut, "/api/memory?project="+project, `{"text":"note"}`)
	send(http.MethodPost, "/api/runs?project="+project, `{"chain":"ghost"}`)
	send(http.MethodGet, "/api/runs/0101-000000", "")
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines: %q", lines)
	}
	for i, want := range []string{"[INFO] PUT /api/memory 200 (", "[WARN] POST /api/runs 400 (", "[WARN] GET /api/runs/0101-000000 400 ("} {
		if !strings.Contains(lines[i], want) {
			t.Errorf("line %d: %q lacks %q", i, lines[i], want)
		}
	}
	if !strings.HasSuffix(lines[2], "): no such run") {
		t.Errorf("failure reason missing: %q", lines[2])
	}
	out.Reset()
	srv.close("swarm stop")
	srv.close("interrupt")
	takeLines(t, out, "[INFO] server shutting down (swarm stop)")
}
