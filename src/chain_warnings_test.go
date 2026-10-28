package main

import (
	"os"
	"path/filepath"
	"testing"
)

func warningLibrary() *Library {
	return &Library{
		Tools: map[string]Tool{"commit": {Title: "Commit", Run: "git commit -m {{message}}"}},
		Prompts: map[string]Prompt{
			"fix":  newPrompt("Fix {{request}}.\n\n## Return\n\n- `summary`: one line\n- `commit_message`: one line\n", "shared"),
			"free": newPrompt("Say something.", "shared"),
		},
		Chains:   map[string]*Chain{},
		Settings: Settings{Agents: map[string]AgentCLI{}},
	}
}

const unreadMessage = `step "fix" returns commit_message, but nothing reads {{fix.commit_message}}`

func TestLibraryWarnings(t *testing.T) {
	fix := Step{ID: "fix", Title: "Fix", Agent: "fix"}
	commit := func(message string) Step {
		return Step{ID: "commit", Title: "Commit", Tool: "commit", With: map[string]string{"message": message}}
	}
	request := []Input{{Name: "request", Label: "Request"}}
	tests := []struct {
		name  string
		chain Chain
		setup func(*Library)
		want  []string
	}{
		{name: "everything filled and used", chain: Chain{Inputs: request, Subject: "{{commit.output}}", Steps: []Step{fix, commit("{{fix.commit_message}}")}}},
		{name: "field the prompt does not return", chain: Chain{Inputs: request, Steps: []Step{fix, commit("{{fix.comit_message}}")}},
			want: []string{`step "commit": the value for message reads {{fix.comit_message}}, but fix returns only summary, commit_message`, unreadMessage}},
		{name: "value nothing fills", chain: Chain{Inputs: request, Steps: []Step{fix, {ID: "commit", Title: "Commit", Tool: "commit"}}},
			want: []string{`step "commit": tools/commit.json reads {{message}}, which nothing fills`, unreadMessage}},
		{name: "value the tool does not read", chain: Chain{Inputs: request, Steps: []Step{fix, {ID: "commit", Title: "Commit", Tool: "commit", With: map[string]string{"message": "{{fix.commit_message}}", "extra": "x"}}}},
			want: []string{`step "commit": passes extra, but tools/commit.json does not read {{extra}}`}},
		{name: "step that has not run yet", chain: Chain{Inputs: request, Steps: []Step{commit("{{fix.commit_message}}"), fix}},
			want: []string{`step "commit": the value for message reads {{fix.commit_message}} before fix runs`}},
		{name: "step that does not exist", chain: Chain{Inputs: request, Steps: []Step{fix, commit("{{ghost.summary}}")}},
			want: []string{`step "commit": the value for message reads {{ghost.summary}}, but there is no step ghost`, unreadMessage}},
		{name: "free text is output", chain: Chain{Steps: []Step{{ID: "say", Title: "Say", Agent: "free"}, commit("{{say.result}}")}},
			want: []string{`step "commit": the value for message reads {{say.result}}, but say returns only output`}},
		{name: "unused input and var", chain: Chain{Inputs: []Input{{Name: "request", Label: "Request"}, {Name: "spare", Label: "Spare"}}, Vars: Fields{{Name: "dir", Value: "{{run_folder}}/x"}}, Steps: []Step{fix, commit("{{fix.commit_message}}")}},
			want: []string{`input "spare" is not used by any step`, `var "dir" is not used`}},
		{name: "user step reads earlier fields and stops on its own", chain: Chain{Inputs: request, Steps: []Step{fix, {ID: "ok", Title: "OK", User: "Keep {{fix.commit_message}}?", Show: []string{"{{run_folder}}/change.diff"}, StopIf: "decision == reject", Stop: "{{note}}"}}}},
		{name: "a tool's own fields are not known", chain: Chain{Inputs: request, Steps: []Step{fix, {ID: "commit", Title: "Commit", Tool: "commit", With: map[string]string{"message": "{{fix.commit_message}}"}, StopIf: "sha == ", Stop: "no {{sha}}"}}}},
		{name: "condition on a step that does not exist", chain: Chain{Inputs: request, Steps: []Step{fix, {ID: "commit", Title: "Commit", Tool: "commit", With: map[string]string{"message": "{{fix.commit_message}}"}, When: "review.verdict == APPROVE"}}},
			want: []string{`step "commit": run only if reads {{review.verdict}}, but there is no step review`}},
		{name: "a field read only by the step's own stop", chain: Chain{Inputs: request, Steps: []Step{{ID: "fix", Title: "Fix", Agent: "fix", StopIf: "commit_message == "}}}},
		{name: "a field only the calling chain reads", chain: Chain{Inputs: request, Steps: []Step{fix}}, setup: func(lib *Library) {
			lib.Chains["parent"] = &Chain{Name: "parent", Steps: []Step{{ID: "sub", Title: "Sub", Chain: "main", With: map[string]string{"request": "x"}}, commit("{{fix.commit_message}}")}}
		}},
		{name: "sub-chain values", chain: Chain{Inputs: request, Steps: []Step{{ID: "sub", Title: "Sub", Chain: "child", With: map[string]string{"extra": "x"}}}}, setup: func(lib *Library) {
			lib.Chains["child"] = &Chain{Name: "child", Inputs: []Input{{Name: "request"}, {Name: "branch"}, {Name: "note", Optional: true}}, Steps: []Step{{ID: "c", Title: "C", Tool: "commit", With: map[string]string{"message": "{{request}} {{branch}} {{note}}"}}}}
		}, want: []string{`step "sub": nothing passes branch, which child needs`, `step "sub": passes extra, but child has no input extra`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lib := warningLibrary()
			if tt.setup != nil {
				tt.setup(lib)
			}
			chain := tt.chain
			chain.Name = "main"
			lib.Chains["main"] = &chain
			if got := lib.warnings(&chain); !sameProblems(got, tt.want) {
				t.Errorf("warnings() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSamplesHaveNoWarnings(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	if _, _, err := writeSamples(home); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".swarm"), 0o755); err != nil {
		t.Fatal(err)
	}
	lib, err := loadLibrary(home, project)
	if err != nil {
		t.Fatal(err)
	}
	if problems := lib.problems(); len(problems) > 0 {
		t.Errorf("samples have problems: %q", problems)
	}
	if warnings := lib.allWarnings(); len(warnings) > 0 {
		t.Errorf("samples have warnings: %q", warnings)
	}
}
