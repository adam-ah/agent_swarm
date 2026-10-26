package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

type server struct {
	core *Core
	quit chan struct{}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFiles, "web")
	mux.Handle("GET /", http.FileServerFS(static))
	mux.HandleFunc("POST /api/shutdown", s.shutdown)
	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("POST /api/projects", s.addProject)
	mux.HandleFunc("GET /api/library", s.library)
	mux.HandleFunc("GET /api/catalog", s.catalog)
	mux.HandleFunc("GET /api/settings", s.settings)
	mux.HandleFunc("GET /api/check", s.check)
	mux.HandleFunc("GET /api/items/{kind}/{file}", s.getItem)
	mux.HandleFunc("PUT /api/items/{kind}/{file}", s.putItem)
	mux.HandleFunc("GET /api/items/{file}", s.getItem)
	mux.HandleFunc("PUT /api/items/{file}", s.putItem)
	mux.HandleFunc("DELETE /api/items/{kind}/{file}", s.deleteItem)
	mux.HandleFunc("GET /api/memory", s.getMemory)
	mux.HandleFunc("PUT /api/memory", s.putMemory)
	mux.HandleFunc("POST /api/memory/notes", s.addNotes)
	mux.HandleFunc("GET /api/runs", s.runs)
	mux.HandleFunc("POST /api/runs", s.startRun)
	mux.HandleFunc("GET /api/runs/{id}", s.run)
	mux.HandleFunc("GET /api/runs/{id}/events", s.events)
	mux.HandleFunc("POST /api/runs/{id}/decide", s.decide)
	mux.HandleFunc("POST /api/runs/{id}/stop", s.stop)
	mux.HandleFunc("GET /api/runs/{id}/file", s.file)
	mux.HandleFunc("GET /api/runs/{id}/doc", s.doc)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error) {
	var invalid *Invalid
	if errors.As(err, &invalid) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error(), "problems": invalid.Problems})
		return
	}
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}

func ok(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *server) project(w http.ResponseWriter, r *http.Request) (string, bool) {
	project, err := s.core.Project(r.URL.Query().Get("project"))
	if err != nil {
		fail(w, err)
		return "", false
	}
	return project, true
}

func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		fail(w, err)
		return false
	}
	return true
}

func (s *server) shutdown(w http.ResponseWriter, r *http.Request) {
	active := s.core.store.active()
	var cancelled []string
	for _, run := range active {
		run.cancel()
		cancelled = append(cancelled, fmt.Sprintf("%s (%s)", run.ID, run.Chain))
	}
	go func() {
		deadline := time.Now().Add(30 * time.Second)
		for len(s.core.store.active()) > 0 && time.Now().Before(deadline) {
			time.Sleep(200 * time.Millisecond)
		}
		close(s.quit)
	}()
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": cancelled})
}

/* projects and library */

func (s *server) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, fallback := s.core.Projects()
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects, "default": fallback, "home": s.core.home})
}

func (s *server) addProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if !readBody(w, r, &body) {
		return
	}
	project, err := s.core.AddProject(body.Path)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"project": project})
}

var agentTools = []string{"Bash", "Read", "Grep", "Glob", "Write", "Edit", "WebFetch", "WebSearch"}

func (s *server) library(w http.ResponseWriter, r *http.Request) {
	project, good := s.project(w, r)
	if !good {
		return
	}
	lib, err := s.core.Library(project)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"chains": lib.sortedChains(), "tools": lib.Tools, "prompts": lib.Prompts,
		"agent_tools": agentTools, "project": lib.Project,
		"runners": lib.Settings.runners(), "default_runner": lib.Settings.defaultRunner(),
	})
}

func (s *server) catalog(w http.ResponseWriter, r *http.Request) {
	project, good := s.project(w, r)
	if !good {
		return
	}
	lib, err := s.core.Library(project)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"project": project, "home": s.core.home, "catalog": lib.catalog()})
}

func (s *server) settings(w http.ResponseWriter, r *http.Request) {
	project, good := s.project(w, r)
	if !good {
		return
	}
	lib, err := s.core.Library(project)
	if err != nil {
		fail(w, err)
		return
	}
	layer := func(dir string) any {
		var layer Settings
		if readJSON(settingsPath(dir), &layer) != nil {
			return nil
		}
		return layer
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"effective": lib.Settings,
		"default":   lib.Settings.defaultRunner(),
		"runners":   lib.Settings.runners(),
		"installed": installedRunners(),
		"shared":    layer(s.core.home),
		"project":   layer(filepath.Join(project, ".swarm")),
	})
}

func (s *server) check(w http.ResponseWriter, r *http.Request) {
	project, good := s.project(w, r)
	if !good {
		return
	}
	problems, lib, err := s.core.Check(project)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"problems": problems, "chains": len(lib.Chains), "tools": len(lib.Tools), "prompts": len(lib.Prompts)})
}

func itemPath(r *http.Request) string {
	if r.PathValue("kind") == "" {
		return r.PathValue("file")
	}
	return r.PathValue("kind") + "/" + r.PathValue("file")
}

func (s *server) getItem(w http.ResponseWriter, r *http.Request) {
	project, good := s.project(w, r)
	if !good {
		return
	}
	raw, source, err := s.core.GetItem(project, itemPath(r), r.URL.Query().Get("below") != "")
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"source": source, "content": string(raw)})
}

func (s *server) putItem(w http.ResponseWriter, r *http.Request) {
	project, good := s.project(w, r)
	if !good {
		return
	}
	content, err := io.ReadAll(r.Body)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.core.PutItem(project, itemPath(r), r.URL.Query().Get("scope"), content); err != nil {
		fail(w, err)
		return
	}
	ok(w)
}

func (s *server) deleteItem(w http.ResponseWriter, r *http.Request) {
	project, good := s.project(w, r)
	if !good {
		return
	}
	if err := s.core.DeleteItem(project, itemPath(r)); err != nil {
		fail(w, err)
		return
	}
	ok(w)
}

/* memory */

func (s *server) getMemory(w http.ResponseWriter, r *http.Request) {
	project, good := s.project(w, r)
	if !good {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": s.core.Memory(project), "path": memoryPath(project)})
}

func (s *server) putMemory(w http.ResponseWriter, r *http.Request) {
	project, good := s.project(w, r)
	if !good {
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if !readBody(w, r, &body) {
		return
	}
	if err := s.core.SetMemory(project, body.Text); err != nil {
		fail(w, err)
		return
	}
	ok(w)
}

func (s *server) addNotes(w http.ResponseWriter, r *http.Request) {
	project, good := s.project(w, r)
	if !good {
		return
	}
	var body struct {
		About string   `json:"about"`
		Notes []string `json:"notes"`
	}
	if !readBody(w, r, &body) {
		return
	}
	added, err := s.core.RememberNotes(project, body.About, body.Notes)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"added": added})
}

/* runs */

func (s *server) runs(w http.ResponseWriter, r *http.Request) {
	project, good := s.project(w, r)
	if !good {
		return
	}
	writeJSON(w, http.StatusOK, s.core.store.list(project))
}

func (s *server) startRun(w http.ResponseWriter, r *http.Request) {
	project, good := s.project(w, r)
	if !good {
		return
	}
	var req struct {
		Chain  string            `json:"chain"`
		Inputs map[string]string `json:"inputs"`
	}
	if !readBody(w, r, &req) {
		return
	}
	run, err := s.core.StartRun(project, req.Chain, req.Inputs)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": run.ID})
}

func (s *server) run(w http.ResponseWriter, r *http.Request) {
	raw, _ := s.core.store.snapshot(r.PathValue("id"))
	if raw == nil {
		fail(w, errors.New("no such run"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

func (s *server) events(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	flusher, good := w.(http.Flusher)
	if !good {
		fail(w, errors.New("streaming unsupported"))
		return
	}
	ch := s.core.store.subscribe(id)
	defer s.core.store.unsubscribe(id, ch)
	raw, _ := s.core.store.snapshot(id)
	if raw == nil {
		fail(w, errors.New("no such run"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	send := func(data []byte) {
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(data)
		_, _ = w.Write([]byte("\n\n"))
		flusher.Flush()
	}
	send(raw)
	for {
		select {
		case data := <-ch:
			send(data)
		case <-r.Context().Done():
			return
		}
	}
}

func (s *server) decide(w http.ResponseWriter, r *http.Request) {
	var d decision
	if !readBody(w, r, &d) {
		return
	}
	if err := s.core.Decide(r.PathValue("id"), d); err != nil {
		fail(w, err)
		return
	}
	ok(w)
}

func (s *server) stop(w http.ResponseWriter, r *http.Request) {
	if err := s.core.Stop(r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	ok(w)
}

func (s *server) artifactPath(r *http.Request) (string, bool) {
	path := r.URL.Query().Get("path")
	return path, s.core.store.hasArtifact(r.PathValue("id"), path)
}

func (s *server) file(w http.ResponseWriter, r *http.Request) {
	path, good := s.artifactPath(r)
	if !good {
		fail(w, errors.New("not an artifact of this run"))
		return
	}
	http.ServeFile(w, r, path)
}

var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

func (s *server) doc(w http.ResponseWriter, r *http.Request) {
	path, good := s.artifactPath(r)
	if !good {
		fail(w, errors.New("not an artifact of this run"))
		return
	}
	source, err := os.ReadFile(path)
	if err != nil {
		fail(w, err)
		return
	}
	var html bytes.Buffer
	if err := markdown.Convert(source, &html); err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html.Bytes())
}
