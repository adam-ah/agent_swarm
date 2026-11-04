package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	agentStall     = 15 * time.Minute
	toolStall      = 10 * time.Minute
	defaultTimeout = 2 * time.Hour
	answerGrace    = 5 * time.Second
	exitGrace      = 2 * time.Second
	stopGrace      = 3 * time.Second
	unkillable     = 10 * time.Second
	captureLimit   = 8 << 20
	longestLine    = 64 << 20
)

var adoptOrphans = becomeSubreaper

const (
	runVar    = "SWARM_RUN="
	stepVar   = "SWARM_STEP="
	workerVar = "SWARM_WORKER="
)

type proc struct {
	parent  int
	started int64
	cpu     int64
	run     string
	step    string
	worker  bool
	command string
}

type procs map[int]proc

func scanProcs() procs {
	found := procs{}
	dirs, _ := os.ReadDir("/proc")
	for _, d := range dirs {
		pid, err := strconv.Atoi(d.Name())
		if err != nil {
			continue
		}
		p, live := procStat(pid)
		if !live {
			continue
		}
		env, _ := os.ReadFile(filepath.Join("/proc", d.Name(), "environ"))
		for _, kv := range bytes.Split(env, []byte{0}) {
			if v, found := bytes.CutPrefix(kv, []byte(runVar)); found {
				p.run = string(v)
			} else if v, found := bytes.CutPrefix(kv, []byte(stepVar)); found {
				p.step = string(v)
			} else if bytes.HasPrefix(kv, []byte(workerVar)) {
				p.worker = true
			}
		}
		cmdline, _ := os.ReadFile(filepath.Join("/proc", d.Name(), "cmdline"))
		p.command = strings.TrimSpace(string(bytes.ReplaceAll(cmdline, []byte{0}, []byte{' '})))
		found[pid] = p
	}
	return found
}

func procStat(pid int) (proc, bool) {
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return proc{}, false
	}
	fields := strings.Fields(string(raw[bytes.LastIndexByte(raw, ')')+1:]))
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" {
		return proc{}, false
	}
	var p proc
	p.parent, _ = strconv.Atoi(fields[1])
	p.started, _ = strconv.ParseInt(fields[19], 10, 64)
	for _, f := range fields[11:15] {
		n, _ := strconv.ParseInt(f, 10, 64)
		p.cpu += n
	}
	return p, true
}

func (ps procs) same(pids map[int]int64) map[int]bool {
	still := map[int]bool{}
	for pid, started := range pids {
		if p, live := ps[pid]; live && p.started == started {
			still[pid] = true
		}
	}
	return still
}

func (ps procs) family(roots map[int]bool) map[int]bool {
	children := map[int][]int{}
	for pid, p := range ps {
		children[p.parent] = append(children[p.parent], pid)
	}
	out := map[int]bool{}
	var add func(int)
	add = func(pid int) {
		if out[pid] {
			return
		}
		out[pid] = true
		for _, child := range children[pid] {
			add(child)
		}
	}
	for pid := range roots {
		if _, live := ps[pid]; live {
			add(pid)
		}
	}
	delete(out, os.Getpid())
	return out
}

func (ps procs) cpu(pids map[int]bool) int64 {
	var total int64
	for pid := range pids {
		total += ps[pid].cpu
	}
	return total
}

func killAll(owned func(procs) map[int]bool) []string {
	var stopped []string
	termed := map[int]bool{}
	start := time.Now()
	for {
		ps := scanProcs()
		victims := owned(ps)
		if len(victims) == 0 {
			break
		}
		late := time.Since(start) >= stopGrace
		if time.Since(start) >= 2*stopGrace {
			for pid := range victims {
				stopped = append(stopped, ps.label(pid)+" (could not be stopped)")
			}
			break
		}
		for pid := range victims {
			if !termed[pid] {
				termed[pid] = true
				stopped = append(stopped, ps.label(pid))
				_ = syscall.Kill(pid, syscall.SIGTERM)
			} else if late {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	sort.Strings(stopped)
	return stopped
}

func runMark(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}

func (s *Store) sweep() []string {
	base := runMark(s.dir) + string(filepath.Separator)
	ids := map[string]bool{}
	for _, p := range scanProcs() {
		if id, inStore := strings.CutPrefix(p.run, base); inStore && !strings.Contains(id, string(filepath.Separator)) {
			ids[id] = true
		}
	}
	var stopped []string
	for id := range ids {
		if run := s.load(id); run != nil && run.going() {
			continue
		}
		killed := s.sweepRun(id)
		if run, err := s.read(id); err == nil && len(killed) > 0 {
			s.update(run, func() {
				for _, p := range killed {
					run.Stopped = append(run.Stopped, "after the run had ended: "+p)
				}
			})
		}
		stopped = append(stopped, killed...)
	}
	return stopped
}

func reapOrphans(store *Store, log *console, quit <-chan struct{}, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-quit:
			return
		case <-ticker.C:
			for _, p := range store.sweep() {
				log.warnf("killed %s: its run is no longer going", p)
			}
		}
	}
}

func (s *Store) sweepRun(id string) []string {
	mark := runMark(s.runDir(id))
	return killAll(func(ps procs) map[int]bool {
		roots := map[int]bool{}
		for pid, p := range ps {
			if p.run == mark {
				roots[pid] = true
			}
		}
		return ps.family(roots)
	})
}

func durationText(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	s := d.String()
	for _, zero := range []string{"m0s", "h0m"} {
		if strings.HasSuffix(s, zero) {
			s = s[:len(s)-2]
		}
	}
	return s
}

func (ps procs) label(pid int) string {
	return fmt.Sprintf("%d %s", pid, clip(ps[pid].command))
}

func clip(s string) string {
	const longest = 160
	if len(s) <= longest {
		return s
	}
	return s[:longest] + "…"
}

func actionLabel(input string) string {
	var fields map[string]any
	if json.Unmarshal([]byte(input), &fields) == nil {
		for _, key := range []string{"command", "file_path", "path", "pattern", "url", "query", "description", "prompt"} {
			if s, ok := fields[key].(string); ok {
				return clip(firstLine(s))
			}
		}
	}
	return clip(firstLine(input))
}
