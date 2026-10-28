package main

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
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
	mux.HandleFunc("GET /api/items/{kind}/{file...}", s.getItem)
	mux.HandleFunc("PUT /api/items/{kind}/{file...}", s.putItem)
	mux.HandleFunc("GET /api/items/{file}", s.getItem)
	mux.HandleFunc("PUT /api/items/{file}", s.putItem)
	mux.HandleFunc("DELETE /api/items/{kind}/{file...}", s.deleteItem)
	mux.HandleFunc("POST /api/chains/{name}/transfer", s.transferChain)
	mux.HandleFunc("GET /api/memory", s.getMemory)
	mux.HandleFunc("PUT /api/memory", s.putMemory)
	mux.HandleFunc("POST /api/memory/notes", s.addNotes)
	mux.HandleFunc("GET /api/runs", s.runs)
	mux.HandleFunc("POST /api/runs", s.startRun)
	mux.HandleFunc("GET /api/runs/{id}", s.run)
	mux.HandleFunc("GET /api/runs/{id}/events", s.events)
	mux.HandleFunc("POST /api/runs/{id}/decide", s.decide)
	mux.HandleFunc("POST /api/runs/{id}/stop", s.stop)
	mux.HandleFunc("POST /api/runs/{id}/rerun", s.rerun)
	mux.HandleFunc("DELETE /api/runs/{id}", s.deleteRun)
	mux.HandleFunc("GET /api/runs/{id}/file", s.file)
	mux.HandleFunc("GET /api/runs/{id}/doc", s.doc)
	mux.HandleFunc("GET /api/runs/{id}/call", s.call)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeBody(w, status, "application/json", raw)
}

func writeBody(w http.ResponseWriter, status int, kind string, raw []byte) {
	w.Header().Set("Content-Type", kind)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

func fail(w http.ResponseWriter, err error) {
	var refused *RefusedError
	if errors.As(err, &refused) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error(), "problems": refused.Problems})
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

func (s *server) shutdown(w http.ResponseWriter, _ *http.Request) {
	ok(w)
	go close(s.quit)
}

func (s *server) listProjects(w http.ResponseWriter, _ *http.Request) {
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
		"chains": lib.sortedChains(), "tools": lib.Tools,
		"agent_tools": agentTools, "builtins": builtinNames, "input_values": lib.Settings.Inputs, "project": lib.Project,
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
	view, err := s.core.Settings(project)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
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
	writeJSON(w, http.StatusOK, map[string]any{"problems": problems, "warnings": lib.allWarnings(), "chains": len(lib.Chains), "tools": len(lib.Tools), "prompts": lib.promptCount()})
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

func (s *server) transferChain(w http.ResponseWriter, r *http.Request) {
	project, good := s.project(w, r)
	if !good {
		return
	}
	var body struct {
		To   string `json:"to"`
		Move bool   `json:"move"`
	}
	if !readBody(w, r, &body) {
		return
	}
	done, err := s.core.TransferChain(project, r.PathValue("name"), body.To, body.Move)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, done)
}

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
