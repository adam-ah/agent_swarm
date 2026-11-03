package main

import (
	"testing"
)

func TestScopeFill(t *testing.T) {
	tests := []struct {
		name     string
		values   map[string]string
		steps    []*StepRun
		local    map[string]any
		template string
		shell    bool
		want     string
	}{
		{name: "plain value", values: map[string]string{"name": "x"}, template: "hi {{name}}", want: "hi x"},
		{name: "spaces inside braces", values: map[string]string{"name": "x"}, template: "{{  name  }}", want: "x"},
		{name: "unknown placeholder stays", template: "{{missing}}", want: "{{missing}}"},
		{name: "uppercase name is not a placeholder", values: map[string]string{"Name": "x"}, template: "{{Name}}", want: "{{Name}}"},
		{name: "dotted output", steps: []*StepRun{doneStep("read", map[string]any{"title": "T"})}, template: "{{read.title}}", want: "T"},
		{name: "missing dotted field stays", steps: []*StepRun{doneStep("read", map[string]any{})}, template: "{{read.title}}", want: "{{read.title}}"},
		{name: "local beats values", values: map[string]string{"name": "values"}, local: map[string]any{"name": "local"}, template: "{{name}}", want: "local"},
		{name: "list joins with newlines", local: map[string]any{"items": []any{"a", "b"}}, template: "{{items}}", want: "a\nb"},
		{name: "number", local: map[string]any{"n": 3.0}, template: "{{n}}", want: "3"},
		{name: "bool", local: map[string]any{"ok": true}, template: "{{ok}}", want: "true"},
		{name: "object becomes json", local: map[string]any{"o": map[string]any{"a": 1.0}}, template: "{{o}}", want: `{"a":1}`},
		{name: "shell quotes plain value", values: map[string]string{"f": "a b"}, template: "cat {{f}}", shell: true, want: "cat 'a b'"},
		{name: "shell escapes single quote", values: map[string]string{"f": "it's"}, template: "{{f}}", shell: true, want: `'it'\''s'`},
		{name: "shell leaves unknown placeholder", template: "{{nope}}", shell: true, want: "{{nope}}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newScope(tt.values)
			s.steps = tt.steps
			if tt.local != nil {
				s = s.with(tt.local)
			}
			if got := s.fill(tt.template, tt.shell); got != tt.want {
				t.Errorf("fill(%q) = %q, want %q", tt.template, got, tt.want)
			}
		})
	}
}

func TestScopeChildFallsBackToParent(t *testing.T) {
	parent := newScope(map[string]string{"request": "bug", "name": "outer"})
	child := parent.child(map[string]string{"name": "inner"})
	tests := []struct {
		name      string
		wantValue string
		wantOK    bool
	}{
		{name: "name", wantValue: "inner", wantOK: true},
		{name: "request", wantValue: "bug", wantOK: true},
		{name: "absent", wantValue: "", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := child.lookup(tt.name)
			if got != tt.wantValue || ok != tt.wantOK {
				t.Errorf("lookup(%q) = (%q, %v), want (%q, %v)", tt.name, got, ok, tt.wantValue, tt.wantOK)
			}
		})
	}
}

func TestScopeWithKeepsValuesAndOutputs(t *testing.T) {
	base := newScope(map[string]string{"a": "1"})
	base.steps = []*StepRun{doneStep("step", map[string]any{"b": "2"})}
	local := base.with(map[string]any{"c": "3"})
	for name, want := range map[string]string{"a": "1", "step.b": "2", "c": "3"} {
		if got := local.fill("{{"+name+"}}", false); got != want {
			t.Errorf("with: fill {{%s}} = %q, want %q", name, got, want)
		}
	}
	if got := base.fill("{{c}}", false); got != "{{c}}" {
		t.Errorf("with must not change the original scope; {{c}} = %q", got)
	}
}

func TestScopeComplete(t *testing.T) {
	s := newScope(map[string]string{"name": "x"})
	tests := []struct {
		template string
		want     string
		wantDone bool
	}{
		{template: "hello {{name}}", want: "hello x", wantDone: true},
		{template: "hello {{name}} {{other}}", want: "hello x {{other}}", wantDone: false},
		{template: "no placeholders", want: "no placeholders", wantDone: true},
	}
	for _, tt := range tests {
		got, done := s.complete(tt.template)
		if got != tt.want || done != tt.wantDone {
			t.Errorf("complete(%q) = (%q, %v), want (%q, %v)", tt.template, got, done, tt.want, tt.wantDone)
		}
	}
}

func TestScopeHolds(t *testing.T) {
	s := newScope(map[string]string{"verdict": "bug", "empty": ""})
	s.steps = []*StepRun{doneStep("triage", map[string]any{"kind": "ui"})}
	tests := []struct {
		name string
		cond string
		want bool
	}{
		{name: "equal", cond: "verdict == bug", want: true},
		{name: "equal fails", cond: "verdict == other", want: false},
		{name: "not equal", cond: "verdict != other", want: true},
		{name: "not equal fails", cond: "verdict != bug", want: false},
		{name: "quoted value", cond: `verdict == "bug"`, want: true},
		{name: "output field", cond: "triage.kind == ui", want: true},
		{name: "missing name equals empty", cond: `missing == ""`, want: true},
		{name: "missing name is not a value", cond: "missing == bug", want: false},
		{name: "empty value", cond: "empty == ''", want: true},
		{name: "unreadable condition", cond: "verdict bug", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.holds(tt.cond); got != tt.want {
				t.Errorf("holds(%q) = %v, want %v", tt.cond, got, tt.want)
			}
		})
	}
}

func TestUnmet(t *testing.T) {
	tests := []struct {
		cond string
		want string
	}{
		{cond: "learn.notes != ", want: "learn.notes is empty"},
		{cond: "summary == ", want: "summary is not empty"},
		{cond: "approve.decision != approve", want: "approve.decision is approve"},
		{cond: `verdict == "bug"`, want: "verdict is not bug"},
		{cond: "verdict bug", want: "verdict bug is not true"},
	}
	for _, tt := range tests {
		if got := unmet(tt.cond); got != tt.want {
			t.Errorf("unmet(%q) = %q, want %q", tt.cond, got, tt.want)
		}
	}
}

func doneStep(id string, output map[string]any) *StepRun {
	return &StepRun{ID: id, Kind: "tool", Status: "done", Output: output}
}

func TestStepFieldsComeFromTheCurrentAttempt(t *testing.T) {
	inner := doneStep("check", map[string]any{"changed": "yes"})
	shadowed := doneStep("say", map[string]any{"output": "inner"})
	item := &StepRun{ID: "1", Kind: "item", Status: "done", Children: []*StepRun{doneStep("hidden", map[string]any{"v": "x"})}}
	s := newScope(nil)
	s.steps = []*StepRun{
		doneStep("say", map[string]any{"output": "outer"}),
		{ID: "work", Kind: "chain", Status: "done", Children: []*StepRun{inner, shadowed}},
		{ID: "each", Kind: "chain", Status: "done", Headline: "sub ran once: 1 done", Children: []*StepRun{item}},
		{ID: "skipped", Kind: "tool", Status: "skipped", Output: map[string]any{"v": "old"}},
		{ID: "pending", Kind: "tool", Status: "pending", Output: map[string]any{"v": "old"}},
		{ID: "broke", Kind: "tool", Status: "failed", Output: map[string]any{"output": "partial"}},
	}
	child := s.child(nil)
	child.steps = []*StepRun{{ID: "check", Kind: "tool", Status: "skipped", Output: map[string]any{"changed": "yes"}}}
	for name, want := range map[string]string{
		"say.output": "outer", "check.changed": "yes", "work.check.changed": "yes", "work.say.output": "inner",
		"each.summary": "sub ran once: 1 done", "hidden.v": "{{hidden.v}}", "skipped.v": "{{skipped.v}}",
		"pending.v": "{{pending.v}}", "broke.output": "partial",
	} {
		if got := s.fill("{{"+name+"}}", false); got != want {
			t.Errorf("{{%s}} = %q, want %q", name, got, want)
		}
	}
	if got := child.fill("{{check.changed}}", false); got != "{{check.changed}}" {
		t.Errorf("a skipped step in the sub-chain hides the caller's: {{check.changed}} = %q", got)
	}
}

func TestText(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{name: "nil", in: nil, want: ""},
		{name: "string", in: "hi", want: "hi"},
		{name: "integer float", in: 4.0, want: "4"},
		{name: "fraction", in: 0.5, want: "0.5"},
		{name: "bool", in: false, want: "false"},
		{name: "list", in: []any{"a", 1.0, true}, want: "a\n1\ntrue"},
		{name: "empty list", in: []any{}, want: ""},
		{name: "object", in: map[string]any{"k": "v"}, want: `{"k":"v"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := text(tt.in); got != tt.want {
				t.Errorf("text(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
