package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var generatedChains = flag.Uint64("chains", 300, "how many generated chains the state machine tests run")

func listOf(rounds []json.RawMessage) json.RawMessage {
	parts := make([]string, len(rounds))
	for i, r := range rounds {
		parts[i] = string(r)
	}
	return json.RawMessage("[" + strings.Join(parts, ",") + "]")
}

type generated struct {
	lib      *Library
	chain    *Chain
	template string
	answers  map[string]json.RawMessage
}

func (c generated) String() string {
	var lines []string
	for _, chain := range []string{"main", "sub"} {
		for _, s := range c.lib.Chains[chain].Steps {
			s.User = strings.TrimPrefix(s.User, c.template)
			raw, err := json.Marshal(s)
			if err != nil {
				return err.Error()
			}
			lines = append(lines, chain+" "+string(raw))
		}
	}
	for _, id := range slices.Sorted(maps.Keys(c.answers)) {
		lines = append(lines, "mock "+id+" "+string(c.answers[id]))
	}
	return strings.Join(lines, "\n")
}

type gen struct{ r *rand.Rand }

func (g gen) pick(n int) int { return g.r.IntN(n) }

func (g gen) chance(percent int) bool { return g.r.IntN(100) < percent }

func (g gen) steps(ids []string, top bool) []Step {
	steps := make([]Step, 0, len(ids))
	chained := false
	for i, id := range ids {
		s := Step{ID: id, Title: strings.ToUpper(id)}
		switch roll := g.pick(100); {
		case i > 0 && roll < 22:
			s.Repeat, s.Rounds = ids[g.pick(i)], []int{0, 2}[g.pick(2)]
			s.Fail = []string{"", "", "out_of_rounds", "ended_early"}[g.pick(4)]
		case top && !chained && roll < 40:
			s.Chain, chained = "sub", true
		case top && roll < 55:
			s.User = "?"
			if i > 0 && g.chance(70) {
				s.Revise = ids[g.pick(i)]
			}
		default:
			s.Tool, s.Always = "t", g.chance(12)
			if g.chance(10) {
				s.StopIf = "v == 1"
			}
		}
		steps = append(steps, s)
	}
	return steps
}

func (g gen) conditions(steps []Step, readable [][]string) {
	for i := range steps {
		if odds := map[string]int{"repeat": 75, "tool": 35, "user": 15, "chain": 25}[steps[i].Kind()]; len(readable[i]) > 0 && g.chance(odds) {
			field := readable[i][g.pick(len(readable[i]))]
			want := map[bool]string{true: "approve", false: "1"}[strings.HasSuffix(field, ".decision")]
			if g.chance(30) {
				want = ""
			}
			steps[i].When = fmt.Sprintf("%s %s %s", field, []string{"==", "!="}[g.pick(2)], want)
		}
	}
}

func (g gen) answers(s Step) json.RawMessage {
	if s.Always {
		return json.RawMessage(`{"v": "1"}`)
	}
	var rounds []string
	for range 30 {
		switch {
		case s.User != "" && s.Revise != "" && g.chance(25):
			rounds = append(rounds, `"revise: again"`)
		case s.User != "" && g.chance(10):
			rounds = append(rounds, `"reject"`)
		case s.User != "":
			rounds = append(rounds, `"approve"`)
		case g.chance(3):
			rounds = append(rounds, `{"fail": "boom"}`)
		default:
			rounds = append(rounds, fmt.Sprintf(`{"v": "%d"}`, 1+g.pick(2)))
		}
	}
	return json.RawMessage("[" + strings.Join(rounds, ",") + "]")
}

func fieldName(s Step) []string {
	switch s.Kind() {
	case "tool":
		return []string{s.ID + ".v"}
	case "user":
		return []string{s.ID + ".decision"}
	}
	return nil
}

func generate(project string, seed uint64, earlierOnly bool) (generated, bool) {
	g := gen{rand.New(rand.NewPCG(seed, 1))}
	main := g.steps([]string{"a", "b", "c", "d", "e"}[:2+g.pick(4)], true)
	sub := g.steps([]string{"x", "y", "z"}[:1+g.pick(3)], false)
	if !slices.ContainsFunc(main, func(s Step) bool { return s.Chain != "" }) {
		sub = nil
	}
	var fields, observed []string
	mainReads, subReads := make([][]string, 0, len(main)), make([][]string, 0, len(sub))
	for _, s := range main {
		mainReads = append(mainReads, slices.Clone(fields))
		for _, x := range sub {
			if s.Chain != "" {
				subReads = append(subReads, slices.Clone(fields))
				fields = append(fields, fieldName(x)...)
			}
		}
		fields = append(fields, fieldName(s)...)
	}
	if len(fields) == 0 {
		return generated{}, false
	}
	for _, reads := range [][][]string{mainReads, subReads} {
		for i := range reads {
			if !earlierOnly {
				reads[i] = fields
			}
		}
	}
	g.conditions(main, mainReads)
	g.conditions(sub, subReads)
	for _, f := range fields {
		observed = append(observed, f+"={{"+f+"}}")
	}
	c := generated{template: strings.Join(observed, " "), answers: map[string]json.RawMessage{}}
	for _, steps := range [][]Step{main, sub} {
		for i := range steps {
			if steps[i].User != "" {
				steps[i].User = c.template
			}
			if k := steps[i].Kind(); k == "tool" || k == "user" {
				c.answers[steps[i].ID] = g.answers(steps[i])
			}
		}
	}
	c.chain = &Chain{Name: "main", Steps: main}
	c.lib = &Library{Project: project, Tools: map[string]Tool{"t": {Run: c.template}}, Settings: Settings{Agents: map[string]AgentCLI{}},
		Chains: map[string]*Chain{"main": c.chain, "sub": {Name: "sub", Steps: sub}}}
	return c, len(c.lib.check(c.chain)) == 0 && len(c.lib.check(c.lib.Chains["sub"])) == 0
}

type harness struct {
	t      *testing.T
	c      generated
	store  *Store
	run    *Run
	m      *mocks
	called []*StepRun
	logs   map[string]*StepRun
}

func newHarness(t *testing.T, c generated, answers map[string]json.RawMessage) *harness {
	t.Helper()
	store := openStore(filepath.Join(t.TempDir(), "runs"))
	id, err := store.claim()
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, c: c, store: store, logs: map[string]*StepRun{}}
	h.run = &Run{ID: id, Project: c.lib.Project, Status: "running", Steps: expand(c.lib, c.chain, ""), inbox: newInbox()}
	h.mock(answers)
	return h
}

func (h *harness) mock(answers map[string]json.RawMessage) {
	m, err := newMocks(answers, nil, h.run.Steps)
	if err != nil {
		h.t.Fatal(err)
	}
	h.m = m
}

func (h *harness) execute() []call {
	from := len(h.m.order)
	r := &Runner{store: h.store, swarm: "true", home: h.c.lib.Project, mocks: h.m}
	r.execute(context.Background(), h.c.lib, h.c.chain, h.run, newScope(map[string]string{"run_folder": h.store.logDir(h.run)}))
	var calls []call
	for _, sr := range h.m.order[from:] {
		file, ok := h.m.files[sr]
		if !ok || slices.Contains(h.called, sr) {
			continue
		}
		if other := h.logs[file]; other != nil && other != sr {
			h.t.Errorf("%s: two attempts write the log %s", sr.Path, filepath.Base(file))
		}
		h.logs[file] = sr
		raw, err := os.ReadFile(file)
		if err != nil {
			h.t.Fatal(err)
		}
		h.called = append(h.called, sr)
		calls = append(calls, call{sr.Path, string(raw)})
	}
	return calls
}

func (h *harness) reload() {
	run := &Run{}
	if err := readJSON(filepath.Join(h.store.runDir(h.run.ID), "run.json"), run); err != nil {
		h.t.Fatal(err)
	}
	run.Status, run.inbox = "running", newInbox()
	h.run = run
}

func treeStatuses(steps []*StepRun, into []string) []string {
	for _, sr := range steps {
		into = append(into, sr.Path+"="+sr.Status)
		into = treeStatuses(sr.Children, into)
	}
	return into
}

func diffCalls(got, want []call) string {
	for i := range max(len(got), len(want)) {
		switch {
		case i >= len(got):
			return fmt.Sprintf("call %d: missing %s seeing %q", i+1, want[i].path, want[i].saw)
		case i >= len(want):
			return fmt.Sprintf("call %d: extra %s seeing %q", i+1, got[i].path, got[i].saw)
		case got[i] != want[i]:
			return fmt.Sprintf("call %d: got %s seeing %q, want %s seeing %q", i+1, got[i].path, got[i].saw, want[i].path, want[i].saw)
		}
	}
	return ""
}

func idOf(path string) string {
	return path[strings.LastIndex(path, ".")+1:]
}
