package main

import (
	"fmt"
	"reflect"
	"testing"
)

func testLibrary() *Library {
	return &Library{
		Tools:    map[string]Tool{"run_tests": {Title: "Run tests", Run: "make test"}},
		Chains:   map[string]*Chain{},
		Settings: Settings{Agents: map[string]AgentCLI{}},
	}
}

func testPrompts() map[string]Prompt {
	return map[string]Prompt{"triage": {Text: "Triage the bug"}}
}

func sameProblems(got, want []string) bool {
	if len(got) == 0 && len(want) == 0 {
		return true
	}
	return reflect.DeepEqual(got, want)
}

func TestLibraryCheck(t *testing.T) {
	tool := Step{ID: "a", Title: "A", Tool: "run_tests"}
	tests := []struct {
		name    string
		chain   string
		steps   []Step
		inputs  []Input
		prompts map[string]Prompt
		setup   func(*Library)
		want    []string
	}{
		{name: "valid tool step", chain: "main", steps: []Step{tool}},
		{name: "valid agent step", chain: "main", steps: []Step{{ID: "a", Title: "A", Agent: "triage"}}},
		{name: "valid user step with earlier revise", chain: "main", steps: []Step{tool, {ID: "ok", Title: "OK", User: "Go?", Revise: "a"}}},
		{name: "valid user step showing a file", chain: "main", steps: []Step{{ID: "ok", Title: "OK", User: "Go?", Show: []string{"change.diff"}}}},
		{name: "show on tool step", chain: "main", steps: []Step{{ID: "a", Title: "A", Tool: "run_tests", Show: []string{"change.diff"}}}, want: []string{`step "a": show only applies to user steps`}},
		{name: "valid conditions", chain: "main", steps: []Step{{ID: "a", Title: "A", Tool: "run_tests", When: "x == y", StopIf: "x != z"}}},
		{name: "bad id", chain: "main", steps: []Step{{ID: "Bad", Title: "A", Tool: "run_tests"}}, want: []string{`step "Bad": ids are lowercase letters, digits and underscores, starting with a letter`}},
		{name: "id starting with digit", chain: "main", steps: []Step{{ID: "1a", Title: "A", Tool: "run_tests"}}, want: []string{`step "1a": ids are lowercase letters, digits and underscores, starting with a letter`}},
		{name: "duplicate id", chain: "main", steps: []Step{tool, tool}, want: []string{`step "a": duplicate id`}},
		{name: "no kind", chain: "main", steps: []Step{{ID: "a", Title: "A"}}, want: []string{`step "a": needs exactly one of tool, agent, user or chain`}},
		{name: "two kinds", chain: "main", steps: []Step{{ID: "a", Title: "A", Tool: "run_tests", Agent: "triage"}}, want: []string{`step "a": needs exactly one of tool, agent, user or chain`}},
		{name: "unknown tool", chain: "main", steps: []Step{{ID: "a", Title: "A", Tool: "nope"}}, want: []string{`step "a": no tool named "nope"`}},
		{name: "unknown prompt", chain: "main", steps: []Step{{ID: "a", Title: "A", Agent: "nope"}}, want: []string{`step "a": no prompt chains/main/nope.md`}},
		{name: "runner on tool step", chain: "main", steps: []Step{{ID: "a", Title: "A", Tool: "run_tests", Runner: "claude"}}, want: []string{`step "a": runner only applies to agent steps`}},
		{name: "unknown runner", chain: "main", steps: []Step{{ID: "a", Title: "A", Agent: "triage", Runner: "pi"}}, want: []string{`step "a": no agent named "pi"; use claude, codex, or add it under agents in settings`}},
		{name: "custom runner defined", chain: "main", steps: []Step{{ID: "a", Title: "A", Agent: "triage", Runner: "pi"}}, setup: func(lib *Library) {
			lib.Settings.Agents["pi"] = AgentCLI{Command: "pi -p"}
		}},
		{name: "unknown chain", chain: "main", steps: []Step{{ID: "a", Title: "A", Chain: "ghost"}}, want: []string{`step "a": no chain named "ghost"`}},
		{name: "chain calls itself", chain: "main", steps: []Step{{ID: "a", Title: "A", Chain: "main"}}, setup: func(lib *Library) {
			lib.Chains["main"] = &Chain{Name: "main"}
		}, want: []string{`step "a": "main" calls back into "main"`}},
		{name: "chain calls back indirectly", chain: "b", steps: []Step{{ID: "x", Title: "X", Chain: "a"}}, setup: func(lib *Library) {
			lib.Chains["a"] = &Chain{Name: "a", Steps: []Step{{ID: "y", Title: "Y", Chain: "b"}}}
			lib.Chains["b"] = &Chain{Name: "b"}
		}, want: []string{`step "x": "a" calls back into "b"`}},
		{name: "valid sub-chain", chain: "main", steps: []Step{{ID: "a", Title: "A", Chain: "other"}}, setup: func(lib *Library) {
			lib.Chains["other"] = &Chain{Name: "other", Steps: []Step{tool}}
		}},
		{name: "revise on tool step", chain: "main", steps: []Step{tool, {ID: "b", Title: "B", Tool: "run_tests", Revise: "a"}}, want: []string{`step "b": revise only applies to user steps`}},
		{name: "revise to a later step", chain: "main", steps: []Step{{ID: "ok", Title: "OK", User: "Go?", Revise: "later"}, tool}, want: []string{`step "ok": revise must name an earlier step in this chain, not "later"`}},
		{name: "revise to itself", chain: "main", steps: []Step{{ID: "ok", Title: "OK", User: "Go?", Revise: "ok"}}, want: []string{`step "ok": revise must name an earlier step in this chain, not "ok"`}},
		{name: "prompt with a bad return kind", chain: "main", steps: []Step{{ID: "a", Title: "A", Agent: "triage"}},
			prompts: map[string]Prompt{"triage": newPrompt("Triage it.\n\n## Return\n\n- `color` (red): its shade\n", "shared")},
			want:    []string{`step "a": chains/main/triage.md ## Return: color: "red" is not text, number, bool, list or A | B | C`}},
		{name: "bad when", chain: "main", steps: []Step{{ID: "a", Title: "A", Tool: "run_tests", When: "verdict"}}, want: []string{`step "a": cannot read condition "verdict"; write it as name == value or name != value`}},
		{name: "bad stop_if", chain: "main", steps: []Step{{ID: "a", Title: "A", Tool: "run_tests", StopIf: "x < 3"}}, want: []string{`step "a": cannot read condition "x < 3"; write it as name == value or name != value`}},
		{name: "bad input name", chain: "main", steps: []Step{tool}, inputs: []Input{{Name: "Bad Name"}}, want: []string{`input "Bad Name": names are lowercase letters, digits and underscores`}},
		{name: "valid input name", chain: "main", steps: []Step{tool}, inputs: []Input{{Name: "issue_2"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lib := testLibrary()
			if tt.setup != nil {
				tt.setup(lib)
			}
			prompts := tt.prompts
			if prompts == nil {
				prompts = testPrompts()
			}
			chain := &Chain{Name: tt.chain, Steps: tt.steps, Inputs: tt.inputs, Prompts: prompts}
			lib.Chains[tt.chain] = chain
			if got := lib.check(chain); !sameProblems(got, tt.want) {
				t.Errorf("check() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLibraryCheckReturnsNonNilSlice(t *testing.T) {
	lib := testLibrary()
	chain := &Chain{Name: "main", Steps: []Step{{ID: "a", Title: "A", Tool: "run_tests"}}}
	if got := lib.check(chain); got == nil {
		t.Error("check() returned nil for a valid chain, want an empty slice")
	}
}

func TestLibraryCalls(t *testing.T) {
	deep := map[string]*Chain{}
	for i := range 10 {
		name := fmt.Sprintf("chain%d", i)
		deep[name] = &Chain{Name: name, Steps: []Step{{ID: "s", Title: "S", Chain: fmt.Sprintf("chain%d", i+1)}}}
	}
	tests := []struct {
		name   string
		chains map[string]*Chain
		from   string
		target string
		want   bool
	}{
		{name: "same name", from: "a", target: "a", want: true},
		{name: "direct call", chains: map[string]*Chain{"a": {Name: "a", Steps: []Step{{ID: "s", Chain: "b"}}}, "b": {Name: "b"}}, from: "a", target: "b", want: true},
		{name: "transitive call", chains: map[string]*Chain{
			"a": {Name: "a", Steps: []Step{{ID: "s", Chain: "b"}}},
			"b": {Name: "b", Steps: []Step{{ID: "s", Chain: "c"}}},
			"c": {Name: "c"},
		}, from: "a", target: "c", want: true},
		{name: "no path", chains: map[string]*Chain{
			"a": {Name: "a", Steps: []Step{{ID: "s", Chain: "b"}}},
			"b": {Name: "b"},
		}, from: "b", target: "a", want: false},
		{name: "missing chain", from: "ghost", target: "a", want: false},
		{name: "steps without chains", chains: map[string]*Chain{"a": {Name: "a", Steps: []Step{{ID: "s", Tool: "t"}}}}, from: "a", target: "b", want: false},
		{name: "nesting deeper than eight counts as a call", chains: deep, from: "chain0", target: "elsewhere", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lib := testLibrary()
			lib.Chains = tt.chains
			if lib.Chains == nil {
				lib.Chains = map[string]*Chain{}
			}
			if got := lib.calls(tt.from, tt.target, 0); got != tt.want {
				t.Errorf("calls(%q, %q) = %v, want %v", tt.from, tt.target, got, tt.want)
			}
		})
	}
}

func TestChainStepIndex(t *testing.T) {
	chain := &Chain{Steps: []Step{{ID: "a"}, {ID: "b"}, {ID: "c"}}}
	tests := []struct {
		id   string
		want int
	}{
		{id: "a", want: 0},
		{id: "c", want: 2},
		{id: "missing", want: -1},
		{id: "", want: -1},
	}
	for _, tt := range tests {
		if got := chain.stepIndex(tt.id); got != tt.want {
			t.Errorf("stepIndex(%q) = %d, want %d", tt.id, got, tt.want)
		}
	}
}

func TestLibraryCatalog(t *testing.T) {
	lib := testLibrary()
	lib.Settings = Settings{Agent: "codex", Agents: map[string]AgentCLI{"pi": {Command: "pi -p"}}}
	lib.Tools["run_tests"] = Tool{Title: "Run tests", Run: "make test", Source: "shared"}
	lib.Chains["fix"] = &Chain{Name: "fix", Title: "Fix bug", Source: "project", Steps: []Step{
		{ID: "t", Title: "Test", Tool: "run_tests"},
		{ID: "a", Title: "Agent", Agent: "triage", Runner: "pi"},
	}, Prompts: map[string]Prompt{"triage": {Text: "Triage the bug\nmore detail"}}}
	want := "Agent steps run with codex unless a step sets runner. Agents available: claude, codex, pi.\n\n" +
		"Chains:\n" +
		"- fix (project): Fix bug\n" +
		"    t: Test [tool run_tests]\n" +
		"    a: Agent [agent triage via pi]\n" +
		"Tools:\n" +
		"- run_tests (shared): Run tests\n" +
		"Prompts, each kept with its chain:\n" +
		"- chains/fix/triage.md: Triage the bug\n"
	if got := lib.catalog(); got != want {
		t.Errorf("catalog() =\n%s\nwant\n%s", got, want)
	}
}

func TestLibraryProblemsPrefixesChainName(t *testing.T) {
	lib := testLibrary()
	lib.Chains["fix"] = &Chain{Name: "fix", Source: "shared", Problems: []string{"step \"a\": no tool named \"x\""}}
	lib.Chains["ok"] = &Chain{Name: "ok", Source: "project"}
	want := []string{`fix (shared): step "a": no tool named "x"`}
	if got := lib.problems(); !reflect.DeepEqual(got, want) {
		t.Errorf("problems() = %q, want %q", got, want)
	}
}
