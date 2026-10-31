package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const sequentialNote = "runs its items in parallel in a real run; this test runs them sequentially"

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
	}{{"parallel", step.Parallel}, {"keep_going", step.KeepGoing}} {
		if option.set && !perItem {
			problems = append(problems, fmt.Sprintf("%s: %s only applies to a chain step that runs once per item of a list", where, option.name))
		}
	}
	return problems
}

func eachHeadline(step Step, count int, source string) string {
	at := ""
	if step.Parallel && count > 1 {
		at = " in parallel"
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
	r.store.update(p.run, func() {
		if !sameItems(sr.Children, items, sub) {
			sr.Children = make([]*StepRun, len(items))
			for i, item := range items {
				path := fmt.Sprintf("%s.%d", sr.Path, i+1)
				sr.Children[i] = &StepRun{ID: strconv.Itoa(i + 1), Path: path, Title: text(item), Kind: "item", Uses: step.Chain, Status: "pending", Children: expand(p.lib, sub, path+".")}
			}
		}
		sr.Headline = eachHeadline(step, len(items), source)
	})
	together := step.Parallel && r.mocks == nil
	if step.Parallel && r.mocks != nil {
		r.mocks.notes[sr] = append(r.mocks.notes[sr], sequentialNote)
	}
	folder, _ := p.sc.lookup("run_folder")
	var mu sync.Mutex
	var running sync.WaitGroup
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
		if ctx.Err() != nil {
			r.store.update(p.run, func() { skipAll(group) })
			continue
		}
		ran++
		own := maps.Clone(values)
		own[key] = text(item)
		if folder != "" {
			own["run_folder"] = filepath.Join(folder, group.Path)
			if err := os.MkdirAll(own["run_folder"], 0o755); err != nil {
				running.Wait()
				return nil, err
			}
		}
		if !together {
			count(r.runItem(ctx, p, sub, sr, group, own))
			continue
		}
		running.Add(1)
		go func() {
			defer running.Done()
			count(r.runItem(ctx, p, sub, sr, group, own))
		}()
	}
	running.Wait()

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

func times(n int) string {
	if n == 1 {
		return "once"
	}
	return fmt.Sprintf("%d times", n)
}
