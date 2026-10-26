package main

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

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
	raw := s.core.store.snapshot(r.PathValue("id"))
	if raw == nil {
		fail(w, errors.New("no such run"))
		return
	}
	writeBody(w, http.StatusOK, "application/json", raw)
}

func (s *server) events(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	flusher, good := w.(http.Flusher)
	if !good {
		fail(w, errors.New("streaming unsupported"))
		return
	}
	last := s.core.store.snapshot(id)
	if last == nil {
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
	send(last)
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if raw := s.core.store.snapshot(id); raw != nil && !bytes.Equal(raw, last) {
				last = raw
				send(raw)
			}
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

func (s *server) call(w http.ResponseWriter, r *http.Request) {
	id, log := r.PathValue("id"), r.URL.Query().Get("log")
	if !s.core.store.hasLog(id, log) {
		fail(w, errors.New("no such step in this run"))
		return
	}
	ext, kind := ".call.json", "application/json"
	if r.URL.Query().Get("part") == "stream" {
		ext, kind = ".jsonl", "text/plain; charset=utf-8"
	}
	raw, err := os.ReadFile(filepath.Join(s.core.store.runDir(id), log+ext))
	if err != nil {
		fail(w, errors.New("nothing recorded for this step yet"))
		return
	}
	writeBody(w, http.StatusOK, kind, raw)
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
	writeBody(w, http.StatusOK, "text/html; charset=utf-8", html.Bytes())
}
