package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Artifact struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
	Name string `json:"name"`
}

type StepRun struct {
	ID         string         `json:"id"`
	Path       string         `json:"path"`
	Title      string         `json:"title"`
	Label      string         `json:"label,omitempty"`
	Kind       string         `json:"kind"`
	Uses       string         `json:"uses"`
	Always     bool           `json:"always,omitempty"`
	Status     string         `json:"status"`
	Headline   string         `json:"headline,omitempty"`
	Output     map[string]any `json:"output,omitempty"`
	Artifacts  []Artifact     `json:"artifacts,omitempty"`
	StartedAt  *time.Time     `json:"started_at,omitempty"`
	EndedAt    *time.Time     `json:"ended_at,omitempty"`
	Cost       float64        `json:"cost,omitempty"`
	Tokens     int            `json:"tokens,omitempty"`
	Runner     string         `json:"runner,omitempty"`
	Actions    int            `json:"actions,omitempty"`
	Error      string         `json:"error,omitempty"`
	Ended      string         `json:"ended,omitempty"`
	Question   string         `json:"question,omitempty"`
	Revise     string         `json:"revise,omitempty"`
	Round      int            `json:"round,omitempty"`
	SentBack   string         `json:"sent_back,omitempty"`
	Feedback   string         `json:"feedback,omitempty"`
	Session    string         `json:"session,omitempty"`
	Resumed    string         `json:"resumed,omitempty"`
	Log        string         `json:"log,omitempty"`
	AskedAgain string         `json:"asked_again,omitempty"`
	Asks       []*Ask         `json:"asks,omitempty"`
	Repeats    int            `json:"repeats,omitempty"`
	Rounds     string         `json:"rounds,omitempty"`
	Loop       string         `json:"loop,omitempty"`
	Children   []*StepRun     `json:"children,omitempty"`
	Earlier    []*StepRun     `json:"earlier,omitempty"`
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
	ResumedAt *time.Time        `json:"resumed_at,omitempty"`
	RerunFrom string            `json:"rerun_from,omitempty"`
	EndedAt   *time.Time        `json:"ended_at,omitempty"`
	Cost      float64           `json:"cost"`
	PID       int               `json:"pid,omitempty"`
	Feedback  string            `json:"feedback,omitempty"`
	Stopped   []string          `json:"stopped,omitempty"`
	Steps     []*StepRun        `json:"steps"`

	inbox  *inbox
	cancel context.CancelFunc
}

func (run *Run) going() bool {
	return run.Status == "running" || run.Status == "waiting"
}

type decision struct {
	Step    string   `json:"step,omitempty"`
	Choice  string   `json:"choice"`
	Note    string   `json:"note"`
	Answers []string `json:"answers,omitempty"`
}

const (
	cancelFile = "cancel"
	startGrace = 30 * time.Second
)

var runID = regexp.MustCompile(`^[0-9]{4}-[0-9]{6}(-[0-9]+)?$`)

type cachedRun struct {
	modified time.Time
	size     int64
	run      *Run
}

type Store struct {
	dir   string
	mu    sync.Mutex
	cache map[string]cachedRun
}

func openStore(dir string) *Store {
	return &Store{dir: dir, cache: map[string]cachedRun{}}
}

func (s *Store) runDir(id string) string {
	return filepath.Join(s.dir, id)
}

func (s *Store) logDir(run *Run) string {
	return s.runDir(run.ID)
}

func (s *Store) claim() (string, error) {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return "", err
	}
	base := time.Now().Format("0102-150405")
	for n := 1; n < 1000; n++ {
		id := base
		if n > 1 {
			id = fmt.Sprintf("%s-%d", base, n)
		}
		err := os.Mkdir(s.runDir(id), 0o755)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	return "", errors.New("could not find a free run id")
}

func (s *Store) update(run *Run, change func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change()
	raw, err := json.Marshal(run)
	if err != nil {
		return
	}
	_ = writeFile(filepath.Join(s.runDir(run.ID), "run.json"), raw)
}

func (s *Store) read(id string) (*Run, error) {
	if !runID.MatchString(id) {
		return nil, fmt.Errorf("%q is not a run id", id)
	}
	path := filepath.Join(s.runDir(id), "run.json")
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("no run %s", id)
	}
	s.mu.Lock()
	hit, found := s.cache[id]
	s.mu.Unlock()
	if found && hit.modified.Equal(info.ModTime()) && hit.size == info.Size() {
		return hit.run, nil
	}
	run := &Run{}
	if err := readJSON(path, run); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[id] = cachedRun{modified: info.ModTime(), size: info.Size(), run: run}
	s.mu.Unlock()
	return run, nil
}

func (s *Store) load(id string) *Run {
	run, err := s.read(id)
	if err != nil || run.Chain == "" {
		return nil
	}
	return settle(run)
}

func settle(run *Run) *Run {
	started := run.StartedAt
	if run.ResumedAt != nil {
		started = *run.ResumedAt
	}
	if !run.going() || processAlive(run.PID) || (run.PID == 0 && time.Since(started) < startGrace) {
		return run
	}
	ended := *run
	ended.Status = "interrupted"
	ended.Reason = "its process ended before the run finished"
	return &ended
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func (s *Store) snapshot(id string) []byte {
	run := s.load(id)
	if run == nil {
		return nil
	}
	raw, err := json.Marshal(run)
	if err != nil {
		return nil
	}
	return raw
}

type RunSummary struct {
	ID        string            `json:"id"`
	Chain     string            `json:"chain"`
	Title     string            `json:"title"`
	Subject   string            `json:"subject"`
	Status    string            `json:"status"`
	StartedAt time.Time         `json:"started_at"`
	EndedAt   *time.Time        `json:"ended_at,omitempty"`
	Inputs    map[string]string `json:"inputs"`
	Cost      float64           `json:"cost,omitempty"`
	Question  string            `json:"question,omitempty"`
}

func summaryOf(run *Run) RunSummary {
	sum := RunSummary{ID: run.ID, Chain: run.Chain, Title: run.Title, Subject: run.Subject, Status: run.Status, StartedAt: run.StartedAt, EndedAt: run.EndedAt, Inputs: run.Inputs, Cost: run.Cost}
	if waiting := findStep(run.Steps, func(step *StepRun) bool { return step.Status == "waiting" }); waiting != nil && run.Status == "waiting" {
		sum.Question = waiting.Question
		if ask := waiting.openAsk(); ask != nil {
			sum.Question = waiting.Title + " asks: " + strings.Join(ask.Questions, " · ")
		}
	}
	return sum
}

func (s *Store) all() []*Run {
	var runs []*Run
	files, _ := filepath.Glob(filepath.Join(s.dir, "*", "run.json"))
	for _, file := range files {
		if run := s.load(filepath.Base(filepath.Dir(file))); run != nil {
			runs = append(runs, run)
		}
	}
	return runs
}

func (s *Store) list(project string) []RunSummary {
	out := []RunSummary{}
	for _, run := range s.all() {
		if run.Project != "" && run.Project != project {
			continue
		}
		out = append(out, summaryOf(run))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

func (s *Store) hasArtifact(id, path string) bool {
	run := s.load(id)
	return run != nil && findStep(run.Steps, func(step *StepRun) bool {
		for _, a := range step.Artifacts {
			if a.Path == path {
				return true
			}
		}
		return false
	}) != nil
}

func (s *Store) hasLog(id, log string) bool {
	run := s.load(id)
	return run != nil && log != "" && findStep(run.Steps, func(step *StepRun) bool { return step.madeLog(log) }) != nil
}

func (sr *StepRun) madeLog(log string) bool {
	return sr.Log == log || sr.AskedAgain == log || slices.ContainsFunc(sr.Asks, func(a *Ask) bool { return a.Log == log })
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

func (s *Store) signal(id, name string, raw []byte) error {
	return writeFile(filepath.Join(s.runDir(id), name), raw)
}

func (s *Store) clearSignals(id string) error {
	decisions, _ := filepath.Glob(filepath.Join(s.runDir(id), decisionFile("*")))
	for _, file := range append(decisions, filepath.Join(s.runDir(id), cancelFile)) {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *Store) remove(id string) error {
	if !runID.MatchString(id) {
		return fmt.Errorf("%q is not a run id", id)
	}
	s.mu.Lock()
	delete(s.cache, id)
	s.mu.Unlock()
	return os.RemoveAll(s.runDir(id))
}

func (s *Store) hasSignal(id, name string) bool {
	_, err := os.Stat(filepath.Join(s.runDir(id), name))
	return err == nil
}
