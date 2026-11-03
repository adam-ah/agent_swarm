package main

import (
	"encoding/json"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

func eachChain(t *testing.T, earlierOnly bool, test func(seed uint64, c generated) bool) {
	t.Helper()
	project, ran := t.TempDir(), uint64(0)
	for seed := range *generatedChains {
		c, ok := generate(project, seed, earlierOnly)
		if ok && test(seed, c) {
			ran++
		}
		if t.Failed() {
			t.Fatalf("seed %d:\n%s", seed, c)
		}
	}
	if ran < *generatedChains/3 {
		t.Fatalf("only %d of %d generated chains ran", ran, *generatedChains)
	}
}

func tooLong(run *Run) bool {
	return strings.Contains(run.Reason, "the test stopped after")
}

func TestChainsMatchTheModel(t *testing.T) {
	eachChain(t, false, func(_ uint64, c generated) bool {
		m := newModel(c)
		f := m.start(c.chain)
		h := newHarness(t, c, c.answers)
		calls := h.execute()
		if m.overflow || tooLong(h.run) {
			return false
		}
		if diff := diffCalls(calls, m.calls); diff != "" {
			t.Errorf("%s", diff)
		}
		if got, want := treeStatuses(h.run.Steps, nil), m.statuses(f, "", nil); !slices.Equal(got, want) {
			t.Errorf("statuses %v, want %v", got, want)
		}
		if h.run.Status != m.status {
			t.Errorf("run %s (%s), want %s", h.run.Status, h.run.Reason, m.status)
		}
		return true
	})
}

func withFailure(answers map[string]json.RawMessage, id string, at int) map[string]json.RawMessage {
	out := maps.Clone(answers)
	var rounds []json.RawMessage
	_ = json.Unmarshal(answers[id], &rounds)
	out[id] = listOf(slices.Insert(rounds, at, json.RawMessage(`{"fail": "flaky"}`)))
	return out
}

func callsOf(calls []call, id string) int {
	n := 0
	for _, c := range calls {
		if idOf(c.path) == id {
			n++
		}
	}
	return n
}

func sameEnd(t *testing.T, got, want *harness, gotCalls, wantCalls []call) {
	t.Helper()
	if diff := diffCalls(gotCalls, wantCalls); diff != "" {
		t.Errorf("%s", diff)
	}
	if g, w := treeStatuses(got.run.Steps, nil), treeStatuses(want.run.Steps, nil); !slices.Equal(g, w) {
		t.Errorf("statuses %v, want %v", g, w)
	}
	if got.run.Status != want.run.Status {
		t.Errorf("run %s (%s), want %s (%s)", got.run.Status, got.run.Reason, want.run.Status, want.run.Reason)
	}
}

func TestRerunAfterAFailureMatchesAnUninterruptedRun(t *testing.T) {
	eachChain(t, true, func(seed uint64, c generated) bool {
		want := newHarness(t, c, c.answers)
		wantCalls := want.execute()
		if tooLong(want.run) || len(wantCalls) == 0 {
			return false
		}
		k := rand.New(rand.NewPCG(seed, 2)).IntN(len(wantCalls))
		id := idOf(wantCalls[k].path)
		if string(c.answers[id][0]) != "[" {
			return false
		}
		h := newHarness(t, c, withFailure(c.answers, id, callsOf(wantCalls[:k], id)))
		first := h.execute()
		if h.run.Status != "failed" || len(first) <= k || diffCalls(first[:k+1], wantCalls[:k+1]) != "" {
			t.Fatalf("the failure at call %d did not end the run there: %s (%s)", k+1, h.run.Status, h.run.Reason)
		}
		h.reload()
		rewind(h.run.Steps)
		sameEnd(t, h, want, slices.Concat(first[:k], h.execute()), wantCalls)
		return true
	})
}

func isCurrent(steps []*StepRun, sr *StepRun) bool {
	return slices.ContainsFunc(steps, func(s *StepRun) bool { return s == sr || isCurrent(s.Children, sr) })
}

func TestRerunFromAStepMatchesTheFirstRun(t *testing.T) {
	eachChain(t, true, func(seed uint64, c generated) bool {
		h := newHarness(t, c, c.answers)
		wantCalls := h.execute()
		if tooLong(h.run) {
			return false
		}
		var current []int
		for i, sr := range h.called {
			if isCurrent(h.run.Steps, sr) && !sr.Always {
				current = append(current, i)
			}
		}
		if len(current) == 0 {
			return false
		}
		q := current[rand.New(rand.NewPCG(seed, 3)).IntN(len(current))]
		want := *h
		wantRun := *h.run
		want.run = &wantRun
		replay := map[string]json.RawMessage{}
		for id, raw := range c.answers {
			var rounds []json.RawMessage
			if json.Unmarshal(raw, &rounds) != nil {
				replay[id] = raw
				continue
			}
			replay[id] = listOf(rounds[callsOf(wantCalls[:q], id):])
		}
		h.reload()
		if rewindFrom(h.run.Steps, wantCalls[q].path) == nil {
			t.Fatalf("no step %s to rerun from", wantCalls[q].path)
		}
		h.mock(replay)
		sameEnd(t, h, &want, h.execute(), wantCalls[q:])
		return true
	})
}
