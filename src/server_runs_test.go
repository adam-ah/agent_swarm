package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type surfacePlainWriter struct {
	header http.Header
	status int
	body   strings.Builder
}

func (w *surfacePlainWriter) Header() http.Header { return w.header }

func (w *surfacePlainWriter) Write(raw []byte) (int, error) { return w.body.Write(raw) }

func (w *surfacePlainWriter) WriteHeader(status int) { w.status = status }

func surfaceStarted(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.ID == "" {
		t.Fatalf("no run id in %d %s (%v)", rec.Code, rec.Body.String(), err)
	}
	return body.ID
}

func TestServerStartsAndListsRuns(t *testing.T) {
	core, project := surfaceProject(t)
	stored := surfaceRun(t, core, project, nil)
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, "/api/runs", nil), http.StatusOK, stored.ID)
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPost, "/api/runs", strings.NewReader("[")), http.StatusBadRequest, "error")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPost, "/api/runs", strings.NewReader(`{"chain": "ghost"}`)), http.StatusBadRequest, "no chain named ghost")

	rec := surfaceRequest(t, core, http.MethodPost, "/api/runs", strings.NewReader(`{"chain": "hello", "inputs": {}}`))
	surfaceExpect(t, rec, http.StatusCreated, "id")
	run := surfaceWait(t, core, surfaceStarted(t, rec), "done")
	if run.Steps[0].Headline != "hello" {
		t.Errorf("headline = %q", run.Steps[0].Headline)
	}
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, "/api/runs/"+run.ID, nil), http.StatusOK, `"status":"done"`)
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, "/api/runs/0101-000000", nil), http.StatusBadRequest, "no such run")
}

func TestServerRunEvents(t *testing.T) {
	shorten(t, &eventsEvery, 10*time.Millisecond)
	core, project := surfaceProject(t)
	run := surfaceRun(t, core, project, nil)
	srv := &server{core: core, quit: make(chan struct{})}
	surfaceExpect(t, surfaceRecord(srv, http.MethodGet, "/api/runs/0101-000000/events", nil), http.StatusBadRequest, "no such run")

	plain := &surfacePlainWriter{header: http.Header{}}
	srv.events(plain, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/runs/x/events", nil))
	if plain.status != http.StatusBadRequest || !strings.Contains(plain.body.String(), "streaming unsupported") {
		t.Errorf("without a flusher: %d %s", plain.status, plain.body.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		srv.routes().ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/runs/"+run.ID+"/events", nil))
	}()
	time.Sleep(30 * time.Millisecond)
	core.store.update(run, func() { run.Subject = "changed while watching" })
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-finished
	body := rec.Body.String()
	if strings.Count(body, "data: ") != 2 || !strings.Contains(body, "changed while watching") || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("events = %s", body)
	}
}

func TestServerDecideAndStop(t *testing.T) {
	core, project := surfaceProject(t)
	waiting := surfaceRun(t, core, project, func(run *Run) {
		run.Status = "waiting"
		run.Steps[0].Status, run.Steps[0].Question = "waiting", "Keep it?"
	})
	decide := "/api/runs/" + waiting.ID + "/decide"
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPost, decide, strings.NewReader("[")), http.StatusBadRequest, "error")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPost, decide, strings.NewReader(`{"choice": "maybe"}`)), http.StatusBadRequest, "choice must be")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPost, decide, strings.NewReader(`{"choice": "approve", "note": "fine"}`)), http.StatusOK, "ok")
	if !core.store.hasSignal(waiting.ID, decisionFile("greet")) {
		t.Error("no decision written")
	}

	surfaceExpect(t, surfaceRequest(t, core, http.MethodPost, "/api/runs/"+waiting.ID+"/stop", nil), http.StatusOK, "ok")
	if !core.store.hasSignal(waiting.ID, cancelFile) {
		t.Error("no cancel written")
	}
	finished := surfaceRun(t, core, project, nil)
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPost, "/api/runs/"+finished.ID+"/stop", nil), http.StatusBadRequest, "no such run in progress")
}

func TestServerRerunAndDelete(t *testing.T) {
	core, project := surfaceProject(t)
	going := surfaceRun(t, core, project, func(run *Run) { run.Status = "running" })
	finished := surfaceRun(t, core, project, nil)
	rerun := func(id, body string) *httptest.ResponseRecorder {
		return surfaceRequest(t, core, http.MethodPost, "/api/runs/"+id+"/rerun", strings.NewReader(body))
	}
	surfaceExpect(t, rerun(finished.ID, "["), http.StatusBadRequest, "error")
	surfaceExpect(t, rerun(finished.ID, `{"from": "middle"}`), http.StatusBadRequest, "from must be")
	surfaceExpect(t, rerun(going.ID, `{"from": "scratch"}`), http.StatusBadRequest, "still going")
	surfaceExpect(t, rerun(going.ID, `{"from": "step", "step": "greet"}`), http.StatusBadRequest, "stop the run first")
	surfaceExpect(t, rerun(finished.ID, `{"from": "step", "step": "nope"}`), http.StatusBadRequest, "the run has no step nope")

	fresh := surfaceStarted(t, rerun(finished.ID, `{"from": "scratch"}`))
	if fresh == finished.ID {
		t.Errorf("rerun from scratch kept the id %s", fresh)
	}
	surfaceWait(t, core, fresh, "done")

	broken, err := core.StartRun(project, "broken", nil)
	if err != nil {
		t.Fatal(err)
	}
	surfaceWait(t, core, broken.ID, "failed")
	rec := rerun(broken.ID, `{"from": "failed"}`)
	surfaceExpect(t, rec, http.StatusOK, broken.ID)
	if again := surfaceWait(t, core, broken.ID, "failed"); again.ResumedAt == nil {
		t.Error("the rerun did not resume the run")
	}

	surfaceExpect(t, surfaceRequest(t, core, http.MethodDelete, "/api/runs/"+going.ID, nil), http.StatusBadRequest, "stop the run")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodDelete, "/api/runs/"+finished.ID, nil), http.StatusOK, "ok")
	if core.store.load(finished.ID) != nil {
		t.Error("the run is still there")
	}
}

func TestServerRunFilesDocsAndCalls(t *testing.T) {
	core, project := surfaceProject(t)
	doc := filepath.Join(project, "notes.md")
	surfaceFiles(t, project, map[string]string{"notes.md": "# Findings\n\n- one\n"})
	run := surfaceRun(t, core, project, func(run *Run) {
		run.Steps[0].Artifacts = []Artifact{{Kind: "markdown", Path: doc, Name: "notes.md"}}
	})
	base := "/api/runs/" + run.ID
	query := "?path=" + url.QueryEscape(doc)
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, base+"/file"+query, nil), http.StatusOK, "# Findings")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, base+"/file?path=/etc/passwd", nil), http.StatusBadRequest, "not an artifact")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, base+"/doc"+query, nil), http.StatusOK, "<h1>Findings</h1>")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, base+"/doc?path=/etc/passwd", nil), http.StatusBadRequest, "not an artifact")

	rendered := surfaceRequest(t, core, http.MethodPost, "/api/markdown", strings.NewReader(`{"text": "**Ship** it\n\n<script>alert(1)</script>\n\n[go](javascript:alert(1))"}`))
	var reply struct {
		HTML string `json:"html"`
	}
	if err := json.Unmarshal(rendered.Body.Bytes(), &reply); err != nil || !strings.Contains(reply.HTML, "<p><strong>Ship</strong> it</p>") {
		t.Errorf("rendered markdown = %d %s", rendered.Code, rendered.Body.String())
	}
	for _, unsafe := range []string{"script", "javascript"} {
		if strings.Contains(reply.HTML, unsafe) {
			t.Errorf("rendered markdown keeps %s: %s", unsafe, reply.HTML)
		}
	}
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPost, "/api/markdown", strings.NewReader("not json")), http.StatusBadRequest, "invalid")

	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, base+"/call?log=ghost", nil), http.StatusBadRequest, "no such step")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, base+"/call?log=greet", nil), http.StatusBadRequest, "nothing recorded")
	surfaceFiles(t, core.store.runDir(run.ID), map[string]string{"greet.call.json": `{"command": "echo hello"}`, "greet.jsonl": "{\"line\": 1}\n"})
	rec := surfaceRequest(t, core, http.MethodGet, base+"/call?log=greet", nil)
	surfaceExpect(t, rec, http.StatusOK, "echo hello")
	if kind := rec.Header().Get("Content-Type"); kind != "application/json" {
		t.Errorf("call content type = %s", kind)
	}
	rec = surfaceRequest(t, core, http.MethodGet, base+"/call?log=greet&part=stream", nil)
	surfaceExpect(t, rec, http.StatusOK, `"line": 1`)
	if kind := rec.Header().Get("Content-Type"); !strings.HasPrefix(kind, "text/plain") {
		t.Errorf("stream content type = %s", kind)
	}

	if err := os.Remove(doc); err != nil {
		t.Fatal(err)
	}
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, base+"/doc"+query, nil), http.StatusBadRequest, "error")
}
