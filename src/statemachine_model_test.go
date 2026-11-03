package main

import (
	"encoding/json"
	"strings"
)

type call struct {
	path string
	saw  string
}

type attempt struct {
	status   string
	fields   map[string]string
	repeats  int
	children []*attempt
}

type frame struct {
	steps    []Step
	attempts []*attempt
	parent   *frame
}

type model struct {
	chains   map[string]*Chain
	template string
	answers  map[string]json.RawMessage
	used     map[string]int
	calls    []call
	ended    bool
	status   string
	overflow bool
}

func newModel(c generated) *model {
	return &model{chains: c.lib.Chains, template: c.template, answers: c.answers, used: map[string]int{}, status: "done"}
}

func (m *model) fresh(step Step) *attempt {
	a := &attempt{status: "pending"}
	if step.Chain != "" {
		for _, s := range m.chains[step.Chain].Steps {
			a.children = append(a.children, m.fresh(s))
		}
	}
	return a
}

func (m *model) start(chain *Chain) *frame {
	f := &frame{steps: chain.Steps}
	for _, s := range chain.Steps {
		f.attempts = append(f.attempts, m.fresh(s))
	}
	m.body(f, "")
	return f
}

func (m *model) end(status string) {
	if !m.ended {
		m.status = status
	}
	m.ended = true
}

func ended(status string) bool {
	return status == "done" || status == "failed" || status == "stopped" || status == "rejected"
}

func (f *frame) find(id string) (Step, *attempt, bool) {
	for i, s := range f.steps {
		if s.ID == id {
			return s, f.attempts[i], true
		}
	}
	return Step{}, nil, false
}

func (m *model) inner(f *frame, id string) (*attempt, bool) {
	if _, a, ok := f.find(id); ok {
		return a, true
	}
	for i, s := range f.steps {
		if s.Chain == "" {
			continue
		}
		if a, ok := m.inner(&frame{steps: m.chains[s.Chain].Steps, attempts: f.attempts[i].children}, id); ok {
			return a, true
		}
	}
	return nil, false
}

func (m *model) lookup(f *frame, name string) (string, bool) {
	id, field, dotted := strings.Cut(name, ".")
	if !dotted {
		return "", false
	}
	for ; f != nil; f = f.parent {
		if a, ok := m.inner(f, id); ok {
			v, set := a.fields[field]
			return v, set && ended(a.status)
		}
	}
	return "", false
}

func (m *model) holds(f *frame, cond string) bool {
	name, op, want, err := parseCondition(cond)
	if err != nil {
		return false
	}
	got, _ := m.lookup(f, name)
	return (got == want) == (op == "==")
}

func (m *model) observe(f *frame, template string) string {
	return placeholder.ReplaceAllStringFunc(template, func(match string) string {
		if v, ok := m.lookup(f, placeholder.FindStringSubmatch(match)[1]); ok {
			return v
		}
		return match
	})
}

func (m *model) next(id string) json.RawMessage {
	var rounds []json.RawMessage
	if json.Unmarshal(m.answers[id], &rounds) != nil {
		return m.answers[id]
	}
	i := m.used[id]
	m.used[id]++
	if i >= len(rounds) {
		m.overflow = true
		return json.RawMessage(`{}`)
	}
	return rounds[i]
}

func skipped(a *attempt) {
	a.status = "skipped"
	for _, c := range a.children {
		skipped(c)
	}
}

func (m *model) body(f *frame, prefix string) {
	for i := 0; i < len(f.steps) && !m.overflow; i++ {
		i = m.one(f, prefix, i)
	}
}

func (m *model) one(f *frame, prefix string, i int) int {
	s, a := f.steps[i], f.attempts[i]
	if m.ended && !s.Always {
		skipped(a)
		return i
	}
	if s.When != "" && !m.holds(f, s.When) {
		skipped(a)
		if s.Repeat != "" && s.Fail == "ended_early" && a.repeats+1 < s.rounds() {
			a.status = "failed"
			m.end("failed")
		}
		return i
	}
	switch s.Kind() {
	case "repeat":
		return m.repeat(f, s, a, i)
	case "chain":
		m.sub(f, prefix, s, a)
		return i
	}
	template := m.template
	if s.User != "" {
		template += "\n"
	}
	m.calls = append(m.calls, call{prefix + s.ID, m.observe(f, template)})
	return m.answer(f, s, a, i)
}

func (m *model) repeat(f *frame, s Step, a *attempt, i int) int {
	target := f.index(s.Repeat)
	if round := a.repeats + 1; round < s.rounds() {
		f.back(m, target, i, false)
		f.attempts[i].repeats = round
		return target - 1
	}
	a.status = "done"
	if s.Fail == "out_of_rounds" {
		a.status = "failed"
		m.end("failed")
	}
	return i
}

func (m *model) sub(f *frame, prefix string, s Step, a *attempt) {
	a.status = "running"
	m.body(&frame{steps: m.chains[s.Chain].Steps, attempts: a.children, parent: f}, prefix+s.ID+".")
	a.status = "done"
	for _, c := range a.children {
		if c.status == "stopped" || c.status == "rejected" {
			a.status = c.status
		}
	}
	for _, c := range a.children {
		if c.status == "failed" {
			a.status = "failed"
			m.end("failed")
		}
	}
}

func (m *model) answer(f *frame, s Step, a *attempt, i int) int {
	raw := m.next(s.ID)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	if fail, _ := fields["fail"].(string); fail != "" {
		a.status = "failed"
		m.end("failed")
		return i
	}
	a.status, a.fields = "done", map[string]string{}
	if s.User != "" {
		var answer string
		_ = json.Unmarshal(raw, &answer)
		choice, note, _ := strings.Cut(answer, ":")
		a.fields["decision"], a.fields["note"] = strings.TrimSpace(choice), strings.TrimSpace(note)
	}
	for k, v := range fields {
		a.fields[k] = text(v)
	}
	if a.fields["decision"] == "reject" {
		a.status = "rejected"
		m.end("rejected")
	}
	if name, op, want, err := parseCondition(s.StopIf); s.StopIf != "" && !m.ended && err == nil && (a.fields[name] == want) == (op == "==") {
		a.status = "stopped"
		m.end("stopped")
	}
	if a.fields["decision"] == "revise" && !m.ended {
		target, _, _ := f.find(s.Revise)
		start := f.index(target.ID)
		f.back(m, start, i, true)
		return start - 1
	}
	return i
}

func (f *frame) index(id string) int {
	for j, t := range f.steps {
		if t.ID == id {
			return j
		}
	}
	return -1
}

func (f *frame) back(m *model, to, from int, restartLater bool) {
	for j := to; j <= from; j++ {
		f.attempts[j] = m.fresh(f.steps[j])
	}
	for j := from + 1; restartLater && j < len(f.steps); j++ {
		if f.steps[j].Repeat != "" && f.index(f.steps[j].Repeat) > to {
			f.attempts[j].repeats = 0
		}
	}
}

func (m *model) statuses(f *frame, prefix string, into []string) []string {
	for i, s := range f.steps {
		a := f.attempts[i]
		into = append(into, prefix+s.ID+"="+a.status)
		if s.Chain != "" {
			into = m.statuses(&frame{steps: m.chains[s.Chain].Steps, attempts: a.children}, prefix+s.ID+".", into)
		}
	}
	return into
}
