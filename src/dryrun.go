package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

type mocks struct {
	answers map[string]json.RawMessage
	live    map[string]bool
	used    map[string]int
	notes   map[*StepRun][]string
	files   map[*StepRun]string
	ran     map[*StepRun]bool
	folder  string
	steps   int
	order   []*StepRun
}

var testBudget = 200

func readMocks(spec string) (map[string]json.RawMessage, error) {
	if spec == "" {
		return map[string]json.RawMessage{}, nil
	}
	raw := []byte(spec)
	if !strings.HasPrefix(strings.TrimSpace(spec), "{") {
		var err error
		if raw, err = os.ReadFile(spec); err != nil {
			return nil, err
		}
	}
	var each map[string]json.RawMessage
	if err := json.Unmarshal(raw, &each); err != nil {
		return nil, fmt.Errorf("-mock is a JSON object of step ids: %w", err)
	}
	return each, nil
}

func newMocks(answers map[string]json.RawMessage, live []string, steps []*StepRun) (*mocks, error) {
	known := map[string]string{}
	var walk func([]*StepRun)
	walk = func(steps []*StepRun) {
		for _, sr := range steps {
			known[sr.ID], known[sr.Path] = sr.Kind, sr.Kind
			walk(sr.Children)
		}
	}
	walk(steps)
	m := &mocks{answers: answers, live: map[string]bool{}, used: map[string]int{}, notes: map[*StepRun][]string{}, files: map[*StepRun]string{}, ran: map[*StepRun]bool{}}
	for key := range answers {
		if known[key] == "" {
			return nil, fmt.Errorf("-mock names %q, which is not a step of this chain", key)
		}
	}
	for _, key := range live {
		switch known[key] {
		case "":
			return nil, fmt.Errorf("-run names %q, which is not a step of this chain", key)
		case "tool", "agent":
			m.live[key] = true
		default:
			return nil, fmt.Errorf("-run names %q, a %s step; only tool and agent steps can run for real", key, known[key])
		}
	}
	return m, nil
}

func (m *mocks) runs(sr *StepRun) bool {
	if m.live[sr.Path] || m.live[sr.ID] {
		m.ran[sr] = true
		return true
	}
	return false
}

func (m *mocks) next(sr *StepRun) (json.RawMessage, bool) {
	key := sr.Path
	if _, ok := m.answers[key]; !ok {
		key = sr.ID
	}
	answer, ok := m.answers[key]
	var rounds []json.RawMessage
	if !ok || json.Unmarshal(answer, &rounds) != nil {
		return answer, ok
	}
	i := m.used[key]
	m.used[key]++
	if i >= len(rounds) {
		return nil, false
	}
	return rounds[i], true
}

func (m *mocks) answer(lib *Library, chain *Chain, sr *StepRun, step Step, sc *scope) (map[string]any, error) {
	if m.steps++; m.steps > testBudget {
		return nil, fmt.Errorf("the test stopped after %d steps; a mock likely sends the run round forever", testBudget)
	}
	template := map[string]string{"agent": chain.Prompts[step.Agent].Text, "user": step.User + "\n" + strings.Join(step.Show, "\n"), "tool": lib.Tools[step.Tool].Run}[step.Kind()]
	filled := stepScope(step, sc, nil).fill(template, false)
	for _, name := range placeholders(filled) {
		m.notes[sr] = append(m.notes[sr], fmt.Sprintf("reads {{%s}}, which nothing filled", name))
	}
	if step.Kind() == "tool" {
		command := strings.ReplaceAll(strings.TrimSpace(stepScope(step, sc, nil).fill(template, true)), "\n", " ↵ ")
		m.notes[sr] = append(m.notes[sr], "would run: "+clip(command))
	}
	if folder, ok := sc.lookup("run_folder"); ok {
		file := filepath.Join(folder, sr.logName()+".txt")
		if writeFile(file, []byte(filled)) == nil {
			m.files[sr] = file
		}
	}
	raw, mocked := m.next(sr)
	var failed struct {
		Fail string `json:"fail"`
	}
	if mocked && json.Unmarshal(raw, &failed) == nil && failed.Fail != "" {
		return nil, errors.New(failed.Fail)
	}
	switch step.Kind() {
	case "agent":
		return m.agentAnswer(sr, chain.Prompts[step.Agent].Returns, raw, mocked)
	case "user":
		return userAnswer(step, raw, mocked)
	}
	fields := map[string]any{"output": "<" + step.ID + ".output>"}
	if !mocked {
		for name := range lib.reads(chain, 0) {
			if field, ours := strings.CutPrefix(name, step.ID+"."); ours {
				fields[field] = "<" + name + ">"
			}
		}
		return fields, nil
	}
	if json.Unmarshal(raw, &fields) == nil {
		return fields, nil
	}
	return map[string]any{"output": text(jsonValue(raw))}, nil
}

func (m *mocks) agentAnswer(sr *StepRun, returns Fields, raw json.RawMessage, mocked bool) (map[string]any, error) {
	out := map[string]any{}
	if mocked && json.Unmarshal(raw, &out) != nil {
		return nil, fmt.Errorf("the mock for %s is not a JSON object of fields", sr.ID)
	}
	if len(returns) == 0 && len(out) == 0 {
		out["output"] = "<" + sr.ID + ".output>"
	}
	for _, f := range returns {
		if _, set := out[f.Name]; !set {
			out[f.Name] = example(sr.ID, f)
			if mocked {
				m.notes[sr] = append(m.notes[sr], fmt.Sprintf("the mock leaves out %s, so it got an example", f.Name))
			}
		}
	}
	for name := range out {
		if len(returns) > 0 && !slices.ContainsFunc(returns, func(f Field) bool { return f.Name == name }) {
			m.notes[sr] = append(m.notes[sr], fmt.Sprintf("the mock sets %s, which the prompt's ## Return does not list", name))
		}
	}
	return out, nil
}

func example(id string, f Field) any {
	switch kind := strings.TrimSpace(f.Value); {
	case kind == "list":
		return []any{"<" + id + "." + f.Name + ">"}
	case kind == "number":
		return 1.0
	case kind == "bool":
		return true
	case strings.Contains(kind, "|"):
		return strings.TrimSpace(strings.Split(kind, "|")[0])
	}
	return "<" + id + "." + f.Name + ">"
}

func userAnswer(step Step, raw json.RawMessage, mocked bool) (map[string]any, error) {
	answer := "approve"
	if mocked {
		answer = text(jsonValue(raw))
	}
	choice, note, _ := strings.Cut(answer, ":")
	choice, note = strings.TrimSpace(choice), strings.TrimSpace(note)
	switch {
	case choice == "revise" && step.Revise == "":
		return nil, fmt.Errorf("the mock says revise, but %s has no revise step", step.ID)
	case choice != "approve" && choice != "revise" && choice != "reject":
		return nil, fmt.Errorf("a user step's mock is approve, reject or revise: <note>, not %q", answer)
	}
	return map[string]any{"decision": choice, "note": note}, nil
}

func jsonValue(raw json.RawMessage) any {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	return v
}

func (c *Core) TestRun(project, chainName string, inputs map[string]string, answers map[string]json.RawMessage, live []string) (*Run, *mocks, error) {
	lib, err := c.Library(project)
	if err != nil {
		return nil, nil, err
	}
	chain := lib.Chains[chainName]
	if chain == nil {
		return nil, nil, fmt.Errorf("no chain named %s", chainName)
	}
	if len(chain.Problems) > 0 {
		return nil, nil, fmt.Errorf("fix the chain first: %s", strings.Join(chain.Problems, "; "))
	}
	resolved, err := resolveInputs(lib, chain, inputs)
	if err != nil {
		return nil, nil, err
	}
	dir, err := os.MkdirTemp("", "swarm-test-")
	if err != nil {
		return nil, nil, err
	}
	store := openStore(dir)
	id, err := store.claim()
	if err != nil {
		return nil, nil, err
	}
	run := &Run{ID: id, Chain: chainName, Title: chain.Title, Project: lib.Project, Inputs: resolved, Status: "running", StartedAt: time.Now(), Steps: expand(lib, chain, "")}
	m, err := newMocks(answers, live, run.Steps)
	if err != nil {
		return nil, nil, err
	}
	m.folder = store.logDir(run)
	r := newRunner(c.home, store)
	r.mocks = m
	r.execute(context.Background(), lib, chain, run, newScope(r.values(lib, run)))
	return run, m, nil
}

func (c *cli) test(project string) error {
	name, err := c.arg("a chain name")
	if err != nil {
		return err
	}
	answers, err := readMocks(c.mock)
	if err != nil {
		return err
	}
	var live []string
	for _, id := range strings.Split(c.real, ",") {
		if id = strings.TrimSpace(id); id != "" {
			live = append(live, id)
		}
	}
	run, m, err := c.core.TestRun(project, name, c.inputs, answers, live)
	if err != nil {
		return err
	}
	fmt.Printf("test run of %s: every step answered with a mock", name)
	if len(live) > 0 {
		fmt.Printf(" except %s, which really ran", strings.Join(live, ", "))
	}
	fmt.Printf("\nits files are in %s\n\n", m.folder)
	m.report()
	reason := ""
	if run.Reason != "" {
		reason = ": " + run.Reason
	}
	fmt.Printf("\nthe run ended %s%s\n", run.Status, reason)
	return nil
}

func (m *mocks) report() {
	for _, sr := range m.order {
		m.line(sr, strings.Repeat("  ", strings.Count(sr.Path, ".")))
	}
}

func (m *mocks) line(sr *StepRun, indent string) {
	mark := map[string]string{"done": "✓", "skipped": "-", "failed": "✗", "stopped": "■", "rejected": "✗"}[sr.Status]
	what := strings.TrimSpace(sr.Kind + " " + sr.Uses)
	if sr.Rounds != "" {
		what += ", round " + sr.Rounds
	}
	detail := sr.Headline
	switch {
	case sr.Status == "failed":
		detail = "failed: " + sr.Error
	case sr.Status != "skipped" && sr.Kind != "repeat" && sr.Kind != "chain" && sr.Kind != "item":
		how := "mocked"
		if m.ran[sr] {
			how = "ran for real"
		}
		detail = how + ": " + fieldsLine(sr.Output)
	}
	fmt.Printf("%s%s %s (%s) %s\n", indent, mark, sr.Path, what, detail)
	for _, note := range m.notes[sr] {
		mark := "!"
		if strings.HasPrefix(note, "would run: ") {
			mark = "$"
		}
		fmt.Printf("%s    %s %s\n", indent, mark, strings.TrimPrefix(note, "would run: "))
	}
	if file := m.files[sr]; file != "" {
		fmt.Printf("%s    filled text: %s\n", indent, file)
	}
}

func fieldsLine(out map[string]any) string {
	keys := make([]string, 0, len(out))
	for k := range out {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := strings.ReplaceAll(text(out[k]), "\n", " ")
		if len(v) > 60 {
			v = v[:60] + "…"
		}
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, " ")
}
