package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var placeholder = regexp.MustCompile(`\{\{\s*([a-z][a-z0-9_.]*)\s*\}\}`)

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
	if v, ok := s.local[name]; ok {
		return text(v), true
	}
	if v, ok := s.values[name]; ok {
		return v, true
	}
	if step, field, dotted := strings.Cut(name, "."); dotted {
		if out, ok := s.outputs[step]; ok {
			if v, ok := out[field]; ok {
				return text(v), true
			}
		}
	}
	if s.parent != nil {
		return s.parent.lookup(name)
	}
	return "", false
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
		raw, _ := json.Marshal(v)
		return string(raw)
	}
}
