package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const sequentialNote = "runs its items in parallel in a real run; this test runs them sequentially"

const defaultMaxParallel = 8

func (s Step) maxParallel() int {
	if s.MaxParallel > 0 {
		return s.MaxParallel
	}
	return defaultMaxParallel
}

type slots struct {
	free    chan struct{}
	running sync.WaitGroup
}

func (s *slots) start(ctx context.Context, work func()) bool {
	select {
	case s.free <- struct{}{}:
	case <-ctx.Done():
		return false
	}
	s.running.Add(1)
	go func() {
		defer func() {
			<-s.free
			s.running.Done()
		}()
		work()
	}()
	return true
}

func listInput(step Step, sc *scope) (string, []any, error) {
	key := ""
	var items []any
	for _, k := range sortedKeys(step.With) {
		name, lone := lonePlaceholder(step.With[k])
		if !lone {
			continue
		}
		v, _ := sc.value(name)
		list, isList := v.([]any)
		if !isList {
			continue
		}
		if key != "" {
			return "", nil, fmt.Errorf("%s and %s are both lists; a step runs its chain once per item of only one", step.With[key], step.With[k])
		}
		key, items = k, list
	}
	return key, items, nil
}

func eachProblems(step Step, where string) []string {
	perItem := false
	for _, v := range step.With {
		if _, lone := lonePlaceholder(v); lone && step.Chain != "" {
			perItem = true
		}
	}
	var problems []string
	for _, option := range []struct {
		name string
		set  bool
	}{{"parallel", step.Parallel}, {"max_parallel", step.MaxParallel != 0}, {"keep_going", step.KeepGoing}} {
		if option.set && !perItem {
			problems = append(problems, fmt.Sprintf("%s: %s only applies to a chain step that runs once per item of a list", where, option.name))
		}
	}
	if step.MaxParallel != 0 && !step.Parallel {
		problems = append(problems, where+": max_parallel only applies with parallel")
	}
	if step.MaxParallel < 0 || step.MaxParallel == 1 {
		problems = append(problems, fmt.Sprintf("%s: max_parallel is the most items that run concurrently, 2 or more, not %d", where, step.MaxParallel))
	}
	return problems
}

func eachHeadline(step Step, count int, source string) string {
	at := ""
	if step.Parallel && count > 1 {
		at = " in parallel"
	}
	if step.Parallel && count > step.maxParallel() {
		at += fmt.Sprintf(" (%d at most)", step.maxParallel())
	}
	headline := fmt.Sprintf("Runs %s %s%s, once per item of %s", step.Chain, times(count), at, source)
	if step.KeepGoing {
		headline += "; a failed item does not fail this step"
	}
	return headline
}

func (r *Runner) runEach(ctx context.Context, p pass, sr *StepRun, step Step, values map[string]string, key string, items []any) (map[string]any, error) {
	sub := p.lib.Chains[step.Chain]
	source, _ := lonePlaceholder(step.With[key])
	labels := headingsOf(items)
	r.store.update(p.run, func() {
		if !sameItems(sr.Children, items, sub) {
			sr.Children = make([]*StepRun, len(items))
			for i, item := range items {
				path := fmt.Sprintf("%s.%d", sr.Path, i+1)
				sr.Children[i] = &StepRun{ID: strconv.Itoa(i + 1), Path: path, Title: text(item), Label: labels[i], Kind: "item", Uses: step.Chain, Status: "pending", Children: expand(p.lib, sub, path+".")}
			}
		}
		sr.Headline = eachHeadline(step, len(items), source)
	})
	pool := &slots{free: make(chan struct{}, 1)}
	if step.Parallel && r.mocks == nil {
		pool.free = make(chan struct{}, step.maxParallel())
	}
	if step.Parallel && r.mocks != nil {
		r.mocks.notes[sr] = append(r.mocks.notes[sr], sequentialNote)
	}
	folder, _ := p.sc.lookup("run_folder")
	var mu sync.Mutex
	ended := map[string]int{}
	count := func(status string) {
		mu.Lock()
		defer mu.Unlock()
		ended[status]++
	}
	ran := 0
	for i, item := range items {
		group := sr.Children[i]
		if group.Status == "done" {
			ran++
			count("done")
			continue
		}
		own, err := itemValues(values, key, item, folder, group.Path)
		if err != nil {
			pool.running.Wait()
			return nil, err
		}
		if ctx.Err() == nil && pool.start(ctx, func() { count(r.runItem(ctx, p, sub, sr, group, own)) }) {
			ran++
			continue
		}
		r.store.update(p.run, func() { skipAll(group) })
	}
	pool.running.Wait()

	summary := eachSummary(step.Chain, source, len(items), ran, ended)
	r.store.update(p.run, func() { sr.Headline = summary })
	out := map[string]any{"summary": summary}
	if ctx.Err() != nil {
		return out, errors.New("stopped")
	}
	if ended["failed"] > 0 && !step.KeepGoing {
		return out, fmt.Errorf("%d of %d failed", ended["failed"], len(items))
	}
	return out, nil
}

func itemValues(values map[string]string, key string, item any, folder, path string) (map[string]string, error) {
	own := maps.Clone(values)
	own[key] = text(item)
	if folder == "" {
		return own, nil
	}
	own["run_folder"] = filepath.Join(folder, path)
	return own, os.MkdirAll(own["run_folder"], 0o755)
}

func eachSummary(chain, source string, count, ran int, ended map[string]int) string {
	if count == 0 {
		return fmt.Sprintf("%s did not run: %s is empty", chain, source)
	}
	var counts []string
	for _, status := range []string{"done", "rejected", "stopped", "failed"} {
		if ended[status] > 0 {
			counts = append(counts, fmt.Sprintf("%d %s", ended[status], status))
		}
	}
	if ran < count {
		counts = append(counts, fmt.Sprintf("%d not started", count-ran))
	}
	return fmt.Sprintf("%s ran %s: %s", chain, times(ran), strings.Join(counts, ", "))
}

func (r *Runner) runItem(ctx context.Context, p pass, sub *Chain, sr, group *StepRun, values map[string]string) string {
	end := &runEnd{status: "done"}
	started := time.Now()
	r.store.update(p.run, func() { group.Status, group.StartedAt = "running", &started })
	r.runSteps(ctx, pass{lib: p.lib, run: p.run, chain: sub, steps: group.Children, sc: p.sc.child(values), end: end, feedback: &group.Feedback})
	finished := time.Now()
	r.store.update(p.run, func() {
		group.Status, group.Headline, group.EndedAt = end.status, end.reason, &finished
		group.Cost = costOf(group.Children)
		sr.Cost = costOf(sr.Children)
	})
	return end.status
}

func headingsOf(items []any) []string {
	labels := make([]string, len(items))
	for i, item := range items {
		labels[i] = headingOf(text(item))
	}
	return labels
}

func headingOf(item string) string {
	if !filepath.IsAbs(item) {
		return ""
	}
	f, err := os.Open(item)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	lines := bufio.NewScanner(io.LimitReader(f, 4096))
	for lines.Scan() {
		if heading, ok := strings.CutPrefix(lines.Text(), "# "); ok {
			return strings.TrimSpace(heading)
		}
	}
	return ""
}

func times(n int) string {
	if n == 1 {
		return "once"
	}
	return fmt.Sprintf("%d times", n)
}
