package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type limits struct {
	stall   time.Duration
	timeout time.Duration
	cpu     bool
}

func limitsFor(step Step, stall time.Duration, cpu bool) limits {
	l := limits{stall: stall, timeout: defaultTimeout, cpu: cpu}
	if d, err := time.ParseDuration(step.Stall); err == nil {
		l.stall = d
	}
	if d, err := time.ParseDuration(step.Timeout); err == nil {
		l.timeout = d
	}
	return l
}

func (l limits) quiet() string {
	if l.cpu {
		return "no output and no CPU use"
	}
	return "nothing new from the agent"
}

func (l limits) describe() string {
	var parts []string
	if l.stall > 0 {
		parts = append(parts, fmt.Sprintf("after %s with %s", durationText(l.stall), l.quiet()))
	}
	if l.timeout > 0 {
		parts = append(parts, "after "+durationText(l.timeout)+" in all")
	}
	if len(parts) == 0 {
		return "never stopped by swarm"
	}
	return "stopped " + strings.Join(parts, ", or ")
}

func (l limits) tick() time.Duration {
	tick := 2 * time.Second
	for _, d := range []time.Duration{l.stall / 5, l.timeout / 5} {
		if d > 0 && d < tick {
			tick = d
		}
	}
	return max(tick, 10*time.Millisecond)
}

type keptGroup struct {
	mark  string
	until string
	pids  map[int]int64
}

func (g keptGroup) roots(ps procs) map[int]bool {
	roots := ps.same(g.pids)
	for pid, p := range ps {
		if p.step == g.mark {
			roots[pid] = true
		}
	}
	return roots
}

func (r *Runner) stepMark(run *Run, log string) string {
	return runMark(r.store.runDir(run.ID)) + "/" + log
}

func (r *Runner) runGroup(ctx context.Context, cmd *exec.Cmd) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := r.launch(cmd); err != nil {
		return err
	}
	return r.wait(ctx, cmd)
}

func (r *Runner) launch(cmd *exec.Cmd) error {
	cmd.SysProcAttr = childAttr()
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := cmd.Start(); err != nil {
		return err
	}
	if r.running == nil {
		r.running = map[int]bool{}
	}
	r.running[cmd.Process.Pid] = true
	return nil
}

func (r *Runner) wait(ctx context.Context, cmd *exec.Cmd) error {
	pid := cmd.Process.Pid
	waited := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		r.mu.Lock()
		delete(r.running, pid)
		r.mu.Unlock()
		if errors.Is(err, exec.ErrWaitDelay) {
			err = nil
		}
		waited <- err
	}()
	select {
	case err := <-waited:
		return err
	case <-ctx.Done():
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	select {
	case err := <-waited:
		return err
	case <-time.After(stopGrace):
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
	select {
	case err := <-waited:
		return err
	case <-time.After(unkillable):
		return fmt.Errorf("process %d did not end even after SIGKILL, so swarm stopped waiting for it", pid)
	}
}

func (r *Runner) reapAdopted() {
	if !r.adopting {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, pid := range zombieChildren() {
		if !r.running[pid] {
			reap(pid)
		}
	}
}

func (r *Runner) keptFamily(ps procs) map[int]bool {
	roots := map[int]bool{}
	r.mu.Lock()
	for _, g := range r.kept {
		maps.Copy(roots, g.roots(ps))
	}
	r.mu.Unlock()
	return ps.family(roots)
}

func (r *Runner) stepOwned(mark string) func(procs) map[int]bool {
	return func(ps procs) map[int]bool {
		roots := map[int]bool{}
		for pid, p := range ps {
			if p.step == mark {
				roots[pid] = true
			}
		}
		kept := r.keptFamily(ps)
		if r.adopting {
			self := os.Getpid()
			r.mu.Lock()
			for pid, p := range ps {
				if p.parent == self && !p.worker && !r.running[pid] && !kept[pid] {
					roots[pid] = true
				}
			}
			r.mu.Unlock()
		}
		owned := ps.family(roots)
		for pid := range kept {
			delete(owned, pid)
		}
		return owned
	}
}

func (r *Runner) keep(mark, until string) []string {
	ps := scanProcs()
	owned := r.stepOwned(mark)(ps)
	listed := make([]string, 0, len(owned))
	pids := map[int]int64{}
	for pid := range owned {
		listed = append(listed, ps.label(pid))
		pids[pid] = ps[pid].started
	}
	sort.Strings(listed)
	if len(owned) > 0 {
		r.mu.Lock()
		r.kept = append(r.kept, keptGroup{mark: mark, until: until, pids: pids})
		r.mu.Unlock()
	}
	return listed
}

func (r *Runner) release(until string) []string {
	r.mu.Lock()
	var due, rest []keptGroup
	for _, g := range r.kept {
		if g.until == until {
			due = append(due, g)
		} else {
			rest = append(rest, g)
		}
	}
	r.kept = rest
	r.mu.Unlock()
	if len(due) == 0 {
		return nil
	}
	stopped := killAll(func(ps procs) map[int]bool {
		roots := map[int]bool{}
		for _, g := range due {
			maps.Copy(roots, g.roots(ps))
		}
		return ps.family(roots)
	})
	r.reapAdopted()
	return stopped
}

func (r *Runner) endRun(run *Run) []string {
	r.mu.Lock()
	r.kept = nil
	r.mu.Unlock()
	mark := runMark(r.store.runDir(run.ID))
	self := os.Getpid()
	stopped := killAll(func(ps procs) map[int]bool {
		roots := map[int]bool{}
		for pid, p := range ps {
			if p.run == mark || (r.adopting && p.parent == self && !p.worker) {
				roots[pid] = true
			}
		}
		return ps.family(roots)
	})
	r.reapAdopted()
	return stopped
}

type guard struct {
	r       *Runner
	mark    string
	limits  limits
	log     *callLog
	cancel  context.CancelFunc
	started time.Time
	last    atomic.Int64
	quit    chan struct{}
	mu      sync.Mutex
	over    bool
	why     string
}

func (r *Runner) guard(parent context.Context, mark string, l limits, log *callLog) (*guard, context.Context) {
	ctx, cancel := context.WithCancel(parent)
	g := &guard{r: r, mark: mark, limits: l, log: log, cancel: cancel, started: time.Now(), quit: make(chan struct{})}
	g.progress()
	log.edit(func(c *Call) { c.Limits = l.describe() })
	go g.watch(parent)
	return g, ctx
}

func (g *guard) progress() {
	g.last.Store(time.Now().UnixNano())
}

func (g *guard) watch(parent context.Context) {
	ticker := time.NewTicker(g.limits.tick())
	defer ticker.Stop()
	lastCPU := int64(-1)
	for {
		select {
		case <-g.quit:
			return
		case <-parent.Done():
			g.stop("")
			return
		case <-ticker.C:
		}
		if t := g.limits.timeout; t > 0 && time.Since(g.started) >= t {
			g.stop("it ran longer than its time limit of " + durationText(t))
			return
		}
		if g.limits.cpu {
			ps := scanProcs()
			if cpu := ps.cpu(g.r.stepOwned(g.mark)(ps)); cpu > lastCPU {
				g.progress()
				lastCPU = cpu
			}
		}
		if s := g.limits.stall; s > 0 && time.Since(time.Unix(0, g.last.Load())) >= s {
			g.stop(fmt.Sprintf("%s for %s", g.limits.quiet(), durationText(s)))
			return
		}
	}
}

func (g *guard) stop(why string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.over {
		return
	}
	if g.why == "" {
		g.why = why
	}
	g.log.stopped(killAll(g.r.stepOwned(g.mark)))
	g.r.reapAdopted()
	g.cancel()
}

func (g *guard) stopAfter(d time.Duration) {
	time.AfterFunc(d, func() { g.stop("") })
}

func (g *guard) finish(keepUntil string) string {
	close(g.quit)
	g.mu.Lock()
	g.over = true
	g.mu.Unlock()
	if keepUntil != "" {
		g.log.kept(keepUntil, g.r.keep(g.mark, keepUntil))
	} else {
		g.log.stopped(killAll(g.r.stepOwned(g.mark)))
		g.r.reapAdopted()
	}
	g.cancel()
	return g.why
}
