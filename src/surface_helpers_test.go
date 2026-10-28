package main

import (
	"context"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const surfaceClosedAddr = "127.0.0.1:1"

func surfaceFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for path, text := range files {
		if err := writeFile(filepath.Join(dir, path), []byte(text)); err != nil {
			t.Fatal(err)
		}
	}
}

func surfaceProject(t *testing.T) (*Core, string) {
	t.Helper()
	home, project, bin := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("SWARM_HOME", home)
	t.Setenv("SWARM_PROJECT", project)
	t.Setenv("SWARM_ADDR", surfaceClosedAddr)
	t.Setenv("SWARM_SERVE_ANYWAY", "")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin:/bin")
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	surfaceFiles(t, project, map[string]string{
		".swarm/tools/echo.json":    `{"title": "Echo", "run": "echo hello"}`,
		".swarm/tools/fail.json":    `{"title": "Fail", "run": "echo broken >&2; exit 1"}`,
		".swarm/chains/hello.json":  `{"title": "Hello", "steps": [{"id": "greet", "title": "Greet", "tool": "echo"}]}`,
		".swarm/chains/broken.json": `{"title": "Broken", "steps": [{"id": "boom", "title": "Boom", "tool": "fail"}]}`,
		".swarm/chains/ask.json":    `{"title": "Ask", "steps": [{"id": "greet", "title": "Greet", "tool": "echo"}, {"id": "ok", "title": "OK", "user": "Keep it?", "revise": "greet"}]}`,
	})
	core := openCore(home)
	if _, err := core.AddProject(project); err != nil {
		t.Fatal(err)
	}
	return core, project
}

func surfaceOutput(t *testing.T, run func() error) (string, error) {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	copied := make(chan string, 1)
	go func() {
		raw, _ := io.ReadAll(read)
		copied <- string(raw)
	}()
	stdout, stderr := os.Stdout, os.Stderr
	runErr := func() error {
		os.Stdout, os.Stderr = write, write
		defer func() {
			os.Stdout, os.Stderr = stdout, stderr
			_ = write.Close()
		}()
		return run()
	}()
	out := <-copied
	_ = read.Close()
	return out, runErr
}

func surfaceCLI(t *testing.T, command string, args ...string) (string, error) {
	t.Helper()
	return surfaceOutput(t, func() error { return runCommand(command, args) })
}

func surfaceStdin(t *testing.T, text string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	stdin := os.Stdin
	os.Stdin = file
	t.Cleanup(func() {
		os.Stdin = stdin
		_ = file.Close()
	})
}

func surfaceRun(t *testing.T, core *Core, project string, change func(*Run)) *Run {
	t.Helper()
	id, err := core.store.claim()
	if err != nil {
		t.Fatal(err)
	}
	run := &Run{ID: id, Chain: "hello", Title: "Hello", Project: project, Status: "done", StartedAt: time.Now(),
		Steps: []*StepRun{{ID: "greet", Path: "greet", Title: "Greet", Kind: "tool", Status: "done", Log: "greet"}}}
	if change != nil {
		change(run)
	}
	core.store.update(run, func() {})
	return run
}

func surfaceWait(t *testing.T, core *Core, id string, statuses ...string) *Run {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for {
		run := core.store.load(id)
		if run != nil && slices.Contains(statuses, run.Status) && (run.going() || !processAlive(run.PID)) {
			return run
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s did not reach %v: %+v", id, statuses, run)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func surfaceServer(t *testing.T, core *Core) string {
	t.Helper()
	srv := &server{core: core, quit: make(chan struct{})}
	page := httptest.NewServer(srv.routes())
	t.Cleanup(page.Close)
	return strings.TrimPrefix(page.URL, "http://")
}

func surfaceRecord(srv *server, method, target string, body io.Reader) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), method, target, body))
	return rec
}

func surfaceRequest(t *testing.T, core *Core, method, target string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	return surfaceRecord(&server{core: core, quit: make(chan struct{})}, method, target, body)
}

func surfaceExpect(t *testing.T, rec *httptest.ResponseRecorder, status int, contains string) {
	t.Helper()
	if rec.Code != status || !strings.Contains(rec.Body.String(), contains) {
		t.Errorf("got %d %s, want %d containing %q", rec.Code, rec.Body.String(), status, contains)
	}
}
