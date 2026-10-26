package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Artifact struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
	Name string `json:"name"`
}

type StepRun struct {
	ID        string         `json:"id"`
	Path      string         `json:"path"`
	Title     string         `json:"title"`
	Kind      string         `json:"kind"`
	Uses      string         `json:"uses"`
	Always    bool           `json:"always,omitempty"`
	Status    string         `json:"status"`
	Headline  string         `json:"headline,omitempty"`
	Output    map[string]any `json:"output,omitempty"`
	Artifacts []Artifact     `json:"artifacts,omitempty"`
	StartedAt *time.Time     `json:"started_at,omitempty"`
	EndedAt   *time.Time     `json:"ended_at,omitempty"`
	Cost      float64        `json:"cost,omitempty"`
	Tokens    int            `json:"tokens,omitempty"`
	Runner    string         `json:"runner,omitempty"`
	Actions   int            `json:"actions,omitempty"`
	Error     string         `json:"error,omitempty"`
	Ended     string         `json:"ended,omitempty"`
	Question  string         `json:"question,omitempty"`
	Revise    string         `json:"revise,omitempty"`
	Round     int            `json:"round,omitempty"`
	Log       string         `json:"log,omitempty"`
	Children  []*StepRun     `json:"children,omitempty"`
	Earlier   []*StepRun     `json:"earlier,omitempty"`
}

func (sr *StepRun) logName() string {
	if sr.Round == 0 {
		return sr.Path
	}
	return fmt.Sprintf("%s-%d", sr.Path, sr.Round+1)
}

func again(old *StepRun) *StepRun {
	fresh := &StepRun{ID: old.ID, Path: old.Path, Title: old.Title, Kind: old.Kind, Uses: old.Uses, Always: old.Always, Revise: old.Revise, Status: "pending", Round: old.Round + 1, Earlier: old.Earlier}
	for _, child := range old.Children {
		fresh.Children = append(fresh.Children, again(child))
	}
	old.Children, old.Earlier = nil, nil
	if old.StartedAt != nil {
		fresh.Earlier = append(fresh.Earlier, old)
	}
	return fresh
}

type Run struct {
	ID        string            `json:"id"`
	Chain     string            `json:"chain"`
	Title     string            `json:"title"`
	Project   string            `json:"project"`
	Subject   string            `json:"subject,omitempty"`
	Link      string            `json:"link,omitempty"`
	Inputs    map[string]string `json:"inputs"`
	Status    string            `json:"status"`
	Reason    string            `json:"reason,omitempty"`
	StartedAt time.Time         `json:"started_at"`
	EndedAt   *time.Time        `json:"ended_at,omitempty"`
	Cost      float64           `json:"cost"`
	Steps     []*StepRun        `json:"steps"`

	decisions chan decision
	cancel    context.CancelFunc
}

type decision struct {
	Choice string `json:"choice"`
	Note   string `json:"note"`
}

type Store struct {
	dir  string
	mu   sync.Mutex
	runs map[string]*Run
	subs map[string]map[chan []byte]struct{}
}

func openStore(dir string) (*Store, error) {
	s := &Store{dir: dir, runs: map[string]*Run{}, subs: map[string]map[chan []byte]struct{}{}}
	files, _ := filepath.Glob(filepath.Join(dir, "*", "run.json"))
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		run := &Run{}
		if json.Unmarshal(raw, run) != nil {
			continue
		}
		if run.Status == "running" || run.Status == "waiting" {
			run.Status = "interrupted"
			run.Reason = "swarm was restarted while this run was in progress"
		}
		s.runs[run.ID] = run
	}
	return s, nil
}

func (s *Store) newID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	base := time.Now().Format("0102-150405")
	id := base
	for n := 2; s.runs[id] != nil; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	s.runs[id] = &Run{ID: id}
	return id
}

func (s *Store) put(run *Run) {
	s.mu.Lock()
	s.runs[run.ID] = run
	s.mu.Unlock()
	s.update(run, func() {})
}

func (s *Store) update(run *Run, change func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change()
	raw, _ := json.Marshal(run)
	dir := filepath.Join(s.dir, run.ID)
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "run.json"), raw, 0o644)
	for ch := range s.subs[run.ID] {
		select {
		case ch <- raw:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- raw:
			default:
			}
		}
	}
}

func (s *Store) snapshot(id string) ([]byte, *Run) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[id]
	if run == nil || run.Chain == "" {
		return nil, nil
	}
	raw, _ := json.Marshal(run)
	return raw, run
}

func (s *Store) list(project string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []map[string]any{}
	for _, run := range s.runs {
		if run.Chain == "" || (run.Project != "" && run.Project != project) {
			continue
		}
		out = append(out, map[string]any{
			"id": run.ID, "chain": run.Chain, "title": run.Title, "subject": run.Subject,
			"status": run.Status, "started_at": run.StartedAt, "inputs": run.Inputs,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["started_at"].(time.Time).After(out[j]["started_at"].(time.Time))
	})
	return out
}

func (s *Store) hasArtifact(id, path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[id]
	if run == nil {
		return false
	}
	return stepsHave(run.Steps, path)
}

func stepsHave(steps []*StepRun, path string) bool {
	return findStep(steps, func(step *StepRun) bool {
		for _, a := range step.Artifacts {
			if a.Path == path {
				return true
			}
		}
		return false
	}) != nil
}

func findStep(steps []*StepRun, match func(*StepRun) bool) *StepRun {
	for _, step := range steps {
		if match(step) {
			return step
		}
		if found := findStep(step.Children, match); found != nil {
			return found
		}
		if found := findStep(step.Earlier, match); found != nil {
			return found
		}
	}
	return nil
}

func (s *Store) hasLog(id, log string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[id]
	return run != nil && log != "" && findStep(run.Steps, func(step *StepRun) bool { return step.Log == log }) != nil
}

func (s *Store) waiting(id string) *StepRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run := s.runs[id]; run != nil {
		if step := findStep(run.Steps, func(step *StepRun) bool { return step.Status == "waiting" }); step != nil {
			copied := *step
			return &copied
		}
	}
	return nil
}

func (s *Store) live(id string) (*Run, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[id]
	if run == nil || run.cancel == nil {
		return nil, ""
	}
	return run, run.Status
}

func (s *Store) active() []*Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Run
	for _, run := range s.runs {
		if run.cancel != nil && (run.Status == "running" || run.Status == "waiting") {
			out = append(out, run)
		}
	}
	return out
}

func (s *Store) subscribe(id string) chan []byte {
	ch := make(chan []byte, 16)
	s.mu.Lock()
	if s.subs[id] == nil {
		s.subs[id] = map[chan []byte]struct{}{}
	}
	s.subs[id][ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

func (s *Store) unsubscribe(id string, ch chan []byte) {
	s.mu.Lock()
	delete(s.subs[id], ch)
	s.mu.Unlock()
}

func (s *Store) logDir(run *Run) string {
	return filepath.Join(s.dir, run.ID)
}
