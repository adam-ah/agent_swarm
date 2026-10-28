package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var placeholder = regexp.MustCompile(`\{\{\s*([a-z][a-z0-9_.]*)\s*\}\}`)

var builtinNames = []string{"project", "home", "swarm", "memory", "feedback", "run_id", "run_folder", "guide", "catalog"}

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
	parent  *scope
	local   map[string]any
	values  map[string]string
	outputs map[string]map[string]any
}

func newScope(values map[string]string) *scope {
	return &scope{values: values, outputs: map[string]map[string]any{}}
}

func (s *scope) with(local map[string]any) *scope {
	return &scope{parent: s.parent, local: local, values: s.values, outputs: s.outputs}
}

func (s *scope) child(values map[string]string) *scope {
	return &scope{parent: s, values: values, outputs: map[string]map[string]any{}}
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
	if step, field, dotted := strings.Cut(name, "."); dotted {
		if out, ok := s.outputs[step]; ok {
			if v, ok := out[field]; ok {
				return v, true
			}
		}
	}
	if s.parent != nil {
		return s.parent.value(name)
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

func (s *scope) flatten() map[string]any {
	out := map[string]any{}
	for step, fields := range s.outputs {
		for field, v := range fields {
			out[step+"."+field] = v
		}
	}
	return out
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
