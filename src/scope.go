package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var placeholder = regexp.MustCompile(`\{\{\s*([a-z][a-z0-9_.]*)\s*\}\}`)

var builtinNames = []string{"project", "home", "swarm", "memory", "feedback", "run_id", "run_folder", "run_status", "guide", "catalog"}

func placeholders(template string) []string {
	var names []string
	for _, m := range placeholder.FindAllStringSubmatch(template, -1) {
		if !slices.Contains(names, m[1]) {
			names = append(names, m[1])
		}
	}
	return names
}

type scope struct {
	parent   *scope
	local    map[string]any
	values   map[string]string
	steps    []*StepRun
	end      *runEnd
	sessions map[string]agentSession
}

func newScope(values map[string]string) *scope {
	return &scope{values: values, sessions: map[string]agentSession{}}
}

func (s *scope) with(local map[string]any) *scope {
	return &scope{parent: s.parent, local: local, values: s.values, steps: s.steps, end: s.end, sessions: s.sessions}
}

func (s *scope) child(values map[string]string) *scope {
	return &scope{parent: s, values: values, sessions: map[string]agentSession{}}
}

func (s *scope) lookup(name string) (string, bool) {
	v, ok := s.value(name)
	return text(v), ok
}

func (s *scope) value(name string) (any, bool) {
	if v, ok := s.local[name]; ok {
		return v, true
	}
	if v, ok := s.values[name]; ok {
		return v, true
	}
	if name == "run_status" && s.end != nil {
		return s.end.current(), true
	}
	if id, field, dotted := strings.Cut(name, "."); dotted {
		if sr := stepNamed(s.steps, id); sr != nil {
			return fieldOf(sr, field)
		}
	}
	if s.parent != nil {
		return s.parent.value(name)
	}
	return nil, false
}

func perItem(sr *StepRun) bool {
	return len(sr.Children) > 0 && sr.Children[0].Kind == "item"
}

func stepNamed(steps []*StepRun, id string) *StepRun {
	for _, sr := range steps {
		if sr.ID == id {
			return sr
		}
	}
	for _, sr := range steps {
		if sr.Kind != "chain" || perItem(sr) {
			continue
		}
		if inner := stepNamed(sr.Children, id); inner != nil {
			return inner
		}
	}
	return nil
}

func fieldOf(sr *StepRun, field string) (any, bool) {
	switch {
	case sr.Status != "done" && sr.Status != "failed" && sr.Status != "stopped" && sr.Status != "rejected":
		return nil, false
	case sr.Kind != "chain":
		v, ok := sr.Output[field]
		return v, ok
	case perItem(sr) || len(sr.Children) == 0:
		return sr.Headline, field == "summary"
	}
	id, rest, _ := strings.Cut(field, ".")
	if inner := stepNamed(sr.Children, id); inner != nil {
		return fieldOf(inner, rest)
	}
	return nil, false
}

func lonePlaceholder(template string) (string, bool) {
	trimmed := strings.TrimSpace(template)
	m := placeholder.FindStringSubmatch(trimmed)
	if m == nil || m[0] != trimmed {
		return "", false
	}
	return m[1], true
}

func (s *scope) fill(template string, shell bool) string {
	return placeholder.ReplaceAllStringFunc(template, func(match string) string {
		value, ok := s.lookup(placeholder.FindStringSubmatch(match)[1])
		if !ok {
			return match
		}
		if shell {
			return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
		}
		return value
	})
}

func (s *scope) complete(template string) (string, bool) {
	filled := s.fill(template, false)
	return filled, !placeholder.MatchString(filled)
}

func (s *scope) holds(cond string) bool {
	name, op, want, err := parseCondition(cond)
	if err != nil {
		return false
	}
	got, _ := s.lookup(name)
	return (got == want) == (op == "==")
}

func unmet(cond string) string {
	name, op, want, err := parseCondition(cond)
	if err != nil {
		return cond + " is not true"
	}
	switch {
	case op == "!=" && want == "":
		return name + " is empty"
	case op == "==" && want == "":
		return name + " is not empty"
	case op == "!=":
		return name + " is " + want
	default:
		return name + " is not " + want
	}
}

func text(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = text(item)
		}
		return strings.Join(parts, "\n")
	case float64, bool:
		return fmt.Sprint(v)
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(raw)
	}
}
