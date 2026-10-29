package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	listenEvery = 300 * time.Millisecond
	eventsEvery = 400 * time.Millisecond
	followEvery = time.Second
)

type Runner struct {
	store    *Store
	swarm    string
	home     string
	adopting bool
	ending   sync.Mutex
	mu       sync.Mutex
	running  map[int]bool
	kept     []keptGroup
	mocks    *mocks
}

func newRunner(home string, store *Store) *Runner {
	self, _ := os.Executable()
	return &Runner{store: store, swarm: self, home: home}
}

func (r *Runner) logFile(run *Run, log, ext string) string {
	return filepath.Join(r.store.logDir(run), log+ext)
}

func (r *Runner) environment(run *Run) []string {
	return append(unmarked(os.Environ()),
		runVar+runMark(r.store.runDir(run.ID)),
		"GIT_TERMINAL_PROMPT=0",
		"SWARM_HOME="+r.home,
		"SWARM_PROJECT="+run.Project,
		"PATH="+filepath.Dir(r.swarm)+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func resolveInputs(lib *Library, chain *Chain, inputs map[string]string) (map[string]string, error) {
	if len(chain.Steps) == 0 {
		return nil, fmt.Errorf("%s has no steps yet", chain.Name)
	}
	resolved := map[string]string{}
	for _, in := range chain.Inputs {
		v := strings.TrimSpace(inputs[in.Name])
		if v == "" {
			v = lib.Settings.inputValue(in)
		}
		if v == "" && !in.Optional {
			return nil, fmt.Errorf("%s is required", in.Label)
		}
		resolved[in.Name] = v
	}
	return resolved, nil
}

func (r *Runner) start(lib *Library, chain *Chain, inputs map[string]string) (*Run, error) {
	if len(chain.Problems) > 0 {
		return nil, fmt.Errorf("fix the chain first: %s", strings.Join(chain.Problems, "; "))
	}
	resolved, err := resolveInputs(lib, chain, inputs)
	if err != nil {
		return nil, err
	}
	id, err := r.store.claim()
	if err != nil {
		return nil, err
	}
	run := &Run{ID: id, Chain: chain.Name, Title: chain.Title, Project: lib.Project, Inputs: resolved, Status: "running", StartedAt: time.Now(), Steps: expand(lib, chain, "")}
	r.store.update(run, func() {})
	if err := r.spawn(run); err != nil {
		r.abandon(run, "its process could not start: "+err.Error())
		return nil, err
	}
	return run, nil
}

func (r *Runner) spawn(run *Run) error {
	output, err := os.Create(filepath.Join(r.store.logDir(run), "worker.log"))
	if err != nil {
		return err
	}
	defer func() { _ = output.Close() }()
	cmd := exec.Command(r.swarm, "_run", "-home", r.home, run.ID)
	cmd.Dir = run.Project
	cmd.Env = append(unmarked(os.Environ()), workerVar+runMark(r.store.runDir(run.ID)))
	cmd.Stdout, cmd.Stderr = output, output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func (r *Runner) abandon(run *Run, reason string) {
	finished := time.Now()
	r.store.update(run, func() {
		run.Status, run.Reason, run.EndedAt = "failed", reason, &finished
	})
}

func work(args []string) error {
	flags := flag.NewFlagSet("_run", flag.ContinueOnError)
	home := flags.String("home", defaultHome(), "the shared library and run history")
	if err := flags.Parse(args); err != nil {
		return err
	}
	store := openStore(filepath.Join(*home, "runs"))
	run, err := store.read(flags.Arg(0))
	if err != nil {
		return err
	}
	if run.PID != 0 || !run.going() {
		return errors.New("run " + run.ID + " has already started")
	}
	r := newRunner(*home, store)
	lib, err := loadLibrary(*home, run.Project)
	if err != nil {
		r.abandon(run, err.Error())
		return err
	}
	chain := lib.Chains[run.Chain]
	if chain == nil || len(chain.Problems) > 0 {
		r.abandon(run, "the chain "+run.Chain+" is missing or broken")
		return nil
	}
	r.adopting = adoptOrphans()
	store.sweep()
	if stopped := store.sweepRun(run.ID); len(stopped) > 0 {
		r.noteStopped(run, "left by its previous worker", stopped)
	}
	r.work(lib, chain, run)
	return nil
}

func unmarked(env []string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		return strings.HasPrefix(kv, runVar) || strings.HasPrefix(kv, stepVar) || strings.HasPrefix(kv, workerVar)
	})
}

func (r *Runner) noteStopped(run *Run, when string, stopped []string) {
	if len(stopped) == 0 {
		return
	}
	r.store.update(run, func() {
		for _, p := range stopped {
			run.Stopped = append(run.Stopped, when+": "+p)
		}
	})
}

func (r *Runner) work(lib *Library, chain *Chain, run *Run) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	run.cancel, run.decisions = cancel, make(chan decision, 1)
	r.store.update(run, func() {
		run.PID = os.Getpid()
		if run.ResumedAt == nil {
			run.Steps = expand(lib, chain, "")
		}
	})
	finished, handled := make(chan struct{}), make(chan struct{})
	interrupts := make(chan os.Signal, 2)
	signal.Notify(interrupts, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(interrupts)
	go func() {
		defer close(handled)
		r.interrupted(run, interrupts, finished, cancel)
	}()
	go r.listen(run, listenEvery, finished)
	r.execute(ctx, lib, chain, run, newScope(r.values(lib, run)))
	close(finished)
	<-handled
}

func (r *Runner) interrupted(run *Run, interrupts <-chan os.Signal, finished <-chan struct{}, cancel context.CancelFunc) {
	select {
	case <-finished:
		return
	case <-interrupts:
		cancel()
	}
	select {
	case <-finished:
		return
	case <-interrupts:
		r.ending.Lock()
		r.noteStopped(run, "when the worker was told to stop a second time", r.endRun(run))
		r.ending.Unlock()
	}
	select {
	case <-finished:
	case <-time.After(unkillable):
		os.Exit(1)
	}
}

func (r *Runner) values(lib *Library, run *Run) map[string]string {
	values := map[string]string{"project": lib.Project, "home": lib.Home, "swarm": r.swarm, "memory": readMemory(lib.Project), "feedback": ""}
	maps.Copy(values, run.Inputs)
	values["run_id"] = run.ID
	values["run_folder"] = r.store.logDir(run)
	values["guide"] = guideText()
	values["catalog"] = lib.catalog()
	return values
}

func (r *Runner) listen(run *Run, every time.Duration, finished <-chan struct{}) {
	answer := filepath.Join(r.store.runDir(run.ID), decisionFile)
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-finished:
			return
		case <-ticker.C:
		}
		if r.store.hasSignal(run.ID, cancelFile) {
			run.cancel()
		}
		var d decision
		if readJSON(answer, &d) == nil {
			_ = os.Remove(answer)
			select {
			case run.decisions <- d:
			default:
			}
		}
	}
}
