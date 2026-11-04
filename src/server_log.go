package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

type console struct {
	mu  sync.Mutex
	out io.Writer
	now func() time.Time
}

func newConsole(out io.Writer) *console {
	return &console{out: out, now: time.Now}
}

func (c *console) logf(level, format string, args ...any) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = fmt.Fprintf(c.out, "%s [%s] %s\n", c.now().Format(time.DateTime), level, fmt.Sprintf(format, args...))
}

func (c *console) infof(format string, args ...any)  { c.logf("INFO", format, args...) }
func (c *console) warnf(format string, args ...any)  { c.logf("WARN", format, args...) }
func (c *console) errorf(format string, args ...any) { c.logf("ERROR", format, args...) }

var readOnlyPosts = map[string]bool{"/api/check": true, "/api/markdown": true}

type recorder struct {
	http.ResponseWriter

	status int
	failed bytes.Buffer
}

func (r *recorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if r.status >= 400 && r.failed.Len() < 4096 {
		r.failed.Write(b)
	}
	return r.ResponseWriter.Write(b)
}

func (r *recorder) Flush() {
	if f, good := r.ResponseWriter.(http.Flusher); good {
		f.Flush()
	}
}

func (r *recorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (r *recorder) reason() string {
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(r.failed.Bytes(), &body) == nil && body.Error != "" {
		return body.Error
	}
	return strings.TrimSpace(r.failed.String())
}

func (s *server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		rec := &recorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		line := fmt.Sprintf("%s %s %d (%s)", r.Method, r.URL.Path, rec.status, time.Since(started).Round(time.Millisecond))
		switch {
		case rec.status >= 500:
			s.log.errorf("%s%s", line, suffix(rec.reason()))
		case rec.status >= 400:
			s.log.warnf("%s%s", line, suffix(rec.reason()))
		case r.Method != http.MethodGet && !readOnlyPosts[r.URL.Path]:
			s.log.infof("%s", line)
		}
	})
}

type watchedRun struct {
	status string
	steps  map[string]string
}

type runWatch struct {
	store *Store
	log   *console
	page  string
	runs  map[string]watchedRun
	going int
}

func newRunWatch(store *Store, log *console, page string) *runWatch {
	return &runWatch{store: store, log: log, page: page, runs: map[string]watchedRun{}}
}

func watchRuns(w *runWatch, quit <-chan struct{}, every time.Duration) {
	w.scan(false)
	w.reportGoing()
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-quit:
			return
		case <-ticker.C:
			w.scan(true)
		}
	}
}

func (w *runWatch) reportGoing() {
	var ids []string
	for id, seen := range w.runs {
		if seen.status == "running" || seen.status == "waiting" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		w.log.infof("idle: no runs going")
		return
	}
	slices.Sort(ids)
	w.log.infof("%d runs going: %s", len(ids), strings.Join(ids, ", "))
}

func eachStep(steps []*StepRun, visit func(*StepRun)) {
	for _, s := range steps {
		visit(s)
		eachStep(s.Children, visit)
	}
}

func (w *runWatch) scan(announce bool) {
	present := map[string]bool{}
	going, ended := 0, false
	for _, run := range w.store.all() {
		present[run.ID] = true
		if run.going() {
			going++
		}
		before, known := w.runs[run.ID]
		now := watchedRun{status: run.Status, steps: map[string]string{}}
		eachStep(run.Steps, func(s *StepRun) { now.steps[s.logName()] = s.Status })
		w.runs[run.ID] = now
		if !announce {
			continue
		}
		switch {
		case !known:
			w.log.infof("run %s started (%s, %s) %s", run.ID, run.Chain, run.Project, w.link(run.ID))
		case run.going() && before.status != "running" && before.status != "waiting":
			from := run.RerunFrom
			if from == "" {
				from = "its failed step"
			}
			w.log.infof("run %s rerun from %s (%s)", run.ID, from, run.Chain)
		}
		eachStep(run.Steps, func(s *StepRun) {
			if before.steps[s.logName()] != s.Status {
				w.step(run, s)
			}
		})
		if !run.going() && (!known || before.status != run.Status) {
			ended = true
			w.ended(run)
		}
	}
	for id := range w.runs {
		if !present[id] {
			delete(w.runs, id)
			if announce {
				w.log.infof("run %s deleted", id)
			}
		}
	}
	if announce && going == 0 && (w.going > 0 || ended) {
		w.log.infof("idle: no runs going")
	}
	w.going = going
}

func (w *runWatch) link(id string) string {
	return w.page + "/#/run/" + id
}

func stepStats(s *StepRun) string {
	var parts []string
	if s.StartedAt != nil && s.EndedAt != nil {
		parts = append(parts, span(elapsed(*s.StartedAt, s.EndedAt)))
	}
	if s.Cost > 0 && !holdsSteps(s) {
		parts = append(parts, money(s.Cost))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

func (w *runWatch) step(run *Run, s *StepRun) {
	name := "run " + run.ID + " step " + s.Path
	switch s.Status {
	case "running":
		kind := slices.DeleteFunc([]string{s.Kind, s.Uses, s.Runner}, func(p string) bool { return p == "" })
		w.log.infof("%s started (%s)", name, strings.Join(kind, ", "))
	case "waiting":
		if ask := s.openAsk(); ask != nil {
			w.log.infof("%s asks (%s): %s", name, w.link(run.ID), strings.Join(ask.Questions, " · "))
			return
		}
		w.log.infof("%s waiting (%s)%s", name, w.link(run.ID), suffix(s.Question))
	case "done":
		w.log.infof("%s done%s%s", name, stepStats(s), suffix(s.Headline))
	case "failed":
		w.log.errorf("%s failed%s%s", name, stepStats(s), suffix(s.Error))
	case "rejected", "stopped", "skipped":
		w.log.infof("%s %s%s%s", name, s.Status, stepStats(s), suffix(s.Headline))
	}
}

func (w *runWatch) ended(run *Run) {
	parts := []string{run.Chain, span(elapsed(run.StartedAt, run.EndedAt))}
	if run.Cost > 0 {
		parts = append(parts, money(run.Cost))
	}
	line := fmt.Sprintf("run %s %s (%s)%s", run.ID, run.Status, strings.Join(parts, ", "), suffix(run.Reason))
	switch run.Status {
	case "failed", "interrupted":
		w.log.errorf("%s", line)
	default:
		w.log.infof("%s", line)
	}
}
