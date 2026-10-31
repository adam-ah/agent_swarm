package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func sequenceLibrary(t *testing.T) *Library {
	t.Helper()
	return &Library{
		Project: t.TempDir(),
		Tools: map[string]Tool{
			"items": {Run: `echo '{"items": ["one", "two", "three"], "word": "solo"}'`},
			"say":   {Run: "echo {{value}} > {{run_folder}}/said; echo {{value}}"},
		},
		Chains: map[string]*Chain{
			"sub": {Name: "sub", Inputs: []Input{{Name: "value"}}, Steps: []Step{
				{ID: "say", Title: "Say it", Tool: "say"},
				{ID: "approve", Title: "Approve it", User: "Keep {{value}}?"},
				{ID: "cleanup", Title: "Clean up", Tool: "say", With: map[string]string{"value": "cleanup"}, Always: true},
			}},
		},
		Settings: Settings{Agents: map[string]AgentCLI{}},
	}
}

func execute(t *testing.T, lib *Library, chain *Chain, decisions ...string) (*Run, string) {
	t.Helper()
	each := make([]decision, len(decisions))
	for i, d := range decisions {
		choice, note, _ := strings.Cut(d, ":")
		each[i] = decision{Choice: choice, Note: note}
	}
	return executeWith(t, lib, chain, each...)
}

func executeWith(t *testing.T, lib *Library, chain *Chain, decisions ...decision) (*Run, string) {
	t.Helper()
	store := openStore(filepath.Join(t.TempDir(), "runs"))
	id, err := store.claim()
	if err != nil {
		t.Fatal(err)
	}
	run := &Run{ID: id, Project: lib.Project, Status: "running", Steps: expand(lib, chain, ""), inbox: newInbox(decisions...)}
	r := &Runner{store: store, swarm: "true", home: t.TempDir()}
	r.execute(context.Background(), lib, chain, run, newScope(map[string]string{"run_folder": store.logDir(run)}))
	return run, store.logDir(run)
}

func statuses(steps []*StepRun) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Status
	}
	return out
}

func TestChainStepRunsOncePerListItem(t *testing.T) {
	lib := sequenceLibrary(t)
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "list", Title: "List", Tool: "items"},
		{ID: "each", Title: "Each", Chain: "sub", With: map[string]string{"value": "{{list.items}}"}},
		{ID: "after", Title: "After", Tool: "say", With: map[string]string{"value": "after"}},
	}}
	run, folder := execute(t, lib, chain, "approve", "reject", "approve")

	if run.Status != "done" {
		t.Fatalf("run status = %s (%s), want done", run.Status, run.Reason)
	}
	each := run.Steps[1]
	if got := statuses(each.Children); !reflect.DeepEqual(got, []string{"done", "rejected", "done"}) {
		t.Errorf("item statuses = %v", got)
	}
	for i, want := range []string{"one", "two", "three"} {
		item := each.Children[i]
		if item.Kind != "item" || item.Title != want || item.Path != "each."+item.ID {
			t.Errorf("item %d = %+v", i, item)
		}
		if got := item.Children[0].Output["output"]; got != want {
			t.Errorf("item %d said %v, want %s", i, got, want)
		}
		if said, err := os.ReadFile(filepath.Join(folder, item.Path, "said")); err != nil || string(said) != "cleanup\n" {
			t.Errorf("item %d folder has %q, %v", i, said, err)
		}
		if item.Children[2].Status != "done" {
			t.Errorf("item %d cleanup = %s, want done", i, item.Children[2].Status)
		}
	}
	if each.Status != "done" || each.Headline != "sub ran 3 times: 2 done, 1 rejected" {
		t.Errorf("each = %s %q", each.Status, each.Headline)
	}
	if run.Steps[2].Status != "done" {
		t.Errorf("after = %s, want done", run.Steps[2].Status)
	}
}

func TestChainStepWithPlainValueRunsOnceAndSharesTheEnding(t *testing.T) {
	lib := sequenceLibrary(t)
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "list", Title: "List", Tool: "items"},
		{ID: "once", Title: "Once", Chain: "sub", With: map[string]string{"value": "{{list.word}}"}},
		{ID: "after", Title: "After", Tool: "say", With: map[string]string{"value": "after"}},
	}}
	run, _ := execute(t, lib, chain, "reject")

	if run.Status != "rejected" {
		t.Fatalf("run status = %s, want rejected", run.Status)
	}
	once := run.Steps[1]
	if got := statuses(once.Children); !reflect.DeepEqual(got, []string{"done", "rejected", "done"}) {
		t.Errorf("nested statuses = %v", got)
	}
	if got := once.Children[0].Output["output"]; got != "solo" {
		t.Errorf("nested step said %v, want solo", got)
	}
	if run.Steps[2].Status != "skipped" {
		t.Errorf("after = %s, want skipped", run.Steps[2].Status)
	}
}

func TestChainStepWithEmptyList(t *testing.T) {
	lib := sequenceLibrary(t)
	lib.Tools["items"] = Tool{Run: `echo '{"items": []}'`}
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "list", Title: "List", Tool: "items"},
		{ID: "each", Title: "Each", Chain: "sub", With: map[string]string{"value": "{{list.items}}"}},
	}}
	run, _ := execute(t, lib, chain)

	each := run.Steps[1]
	if run.Status != "done" || len(each.Children) != 0 || each.Headline != "sub did not run: list.items is empty" {
		t.Errorf("run %s, each %q with %d children", run.Status, each.Headline, len(each.Children))
	}
}

func TestChainStepRefusesTwoLists(t *testing.T) {
	lib := sequenceLibrary(t)
	lib.Chains["sub"].Inputs = append(lib.Chains["sub"].Inputs, Input{Name: "other"})
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "list", Title: "List", Tool: "items"},
		{ID: "each", Title: "Each", Chain: "sub", With: map[string]string{"value": "{{list.items}}", "other": "{{list.items}}"}},
	}}
	run, _ := execute(t, lib, chain)

	if run.Status != "failed" || run.Steps[1].Error != "{{list.items}} and {{list.items}} are both lists; a step runs its chain once per item of only one" {
		t.Errorf("run %s, error %q", run.Status, run.Steps[1].Error)
	}
}

func TestCheckRefusesTwoDeclaredLists(t *testing.T) {
	lib := sequenceLibrary(t)
	lib.Chains["sub"].Inputs = append(lib.Chains["sub"].Inputs, Input{Name: "other"})
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "split", Title: "Split", Agent: "split"},
		{ID: "each", Title: "Each", Chain: "sub", With: map[string]string{"value": "{{split.a}}", "other": "{{split.b}}"}},
	}, Prompts: map[string]Prompt{"split": newPrompt("Split.\n\n## Return\n\n- `a` (list): one\n- `b` (list): two\n", "shared")}}
	want := []string{`step "each": passes 2 lists (split.b, split.a); it can run sub once per item of only one`}
	if got := lib.check(chain); !reflect.DeepEqual(got, want) {
		t.Errorf("check = %q, want %q", got, want)
	}
}

func TestLonePlaceholder(t *testing.T) {
	tests := []struct {
		template string
		name     string
		lone     bool
	}{
		{"{{split.changesets}}", "split.changesets", true},
		{"  {{ split.changesets }} ", "split.changesets", true},
		{"Files: {{x.files}}", "", false},
		{"{{a}}{{b}}", "", false},
		{"plain", "", false},
	}
	for _, tt := range tests {
		name, lone := lonePlaceholder(tt.template)
		if name != tt.name || lone != tt.lone {
			t.Errorf("lonePlaceholder(%q) = %q, %v", tt.template, name, lone)
		}
	}
}
