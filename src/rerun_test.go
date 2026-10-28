package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func rerunFailed(t *testing.T, lib *Library, chain *Chain, run *Run, folder string, decisions ...string) {
	t.Helper()
	if !fits(lib, chain, run.Steps) {
		t.Fatal("the run's steps do not fit its chain")
	}
	rewind(run.Steps)
	run.Status = "running"
	run.decisions = make(chan decision, len(decisions))
	for _, d := range decisions {
		choice, note, _ := strings.Cut(d, ":")
		run.decisions <- decision{Choice: choice, Note: note}
	}
	r := &Runner{store: openStore(filepath.Dir(folder)), swarm: "true", home: t.TempDir()}
	r.execute(context.Background(), lib, chain, run, newScope(map[string]string{"run_folder": folder}))
}

func flakyLibrary(t *testing.T) (*Library, string) {
	t.Helper()
	lib := sequenceLibrary(t)
	fixed := filepath.Join(lib.Project, "fixed")
	lib.Tools["count"] = Tool{Run: `echo x >> {{run_folder}}/counted; echo '{"word": "kept"}'`}
	lib.Tools["flaky"] = Tool{Run: "test -f " + fixed + " || { echo broken >&2; exit 1; }; echo {{value}}"}
	return lib, fixed
}

func TestRerunFromFailedStepKeepsFinishedSteps(t *testing.T) {
	lib, fixed := flakyLibrary(t)
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "first", Title: "First", Tool: "count"},
		{ID: "flaky", Title: "Flaky", Tool: "flaky", With: map[string]string{"value": "{{first.word}}"}},
		{ID: "cleanup", Title: "Clean up", Tool: "say", With: map[string]string{"value": "bye"}, Always: true},
	}}
	run, folder := execute(t, lib, chain)
	if got := statuses(run.Steps); run.Status != "failed" || !reflect.DeepEqual(got, []string{"done", "failed", "done"}) {
		t.Fatalf("first attempt = %s %v", run.Status, got)
	}
	if err := os.WriteFile(fixed, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	rerunFailed(t, lib, chain, run, folder)

	if got := statuses(run.Steps); run.Status != "done" || !reflect.DeepEqual(got, []string{"done", "done", "done"}) {
		t.Fatalf("rerun = %s %v (%s)", run.Status, got, run.Reason)
	}
	if counted, _ := os.ReadFile(filepath.Join(folder, "counted")); string(counted) != "x\n" {
		t.Errorf("first ran again: counted %q", counted)
	}
	if got := run.Steps[1].Output["output"]; got != "kept" {
		t.Errorf("flaky read %v from the kept step, want kept", got)
	}
	if n := len(run.Steps[1].Earlier); n != 1 || run.Steps[1].Earlier[0].Status != "failed" || run.Steps[1].Log == run.Steps[1].Earlier[0].Log {
		t.Errorf("flaky's failed attempt was not kept apart: %d earlier", n)
	}
	if len(run.Steps[0].Earlier) != 0 || len(run.Steps[2].Earlier) != 1 {
		t.Errorf("earlier attempts: first %d, cleanup %d; want 0 and 1", len(run.Steps[0].Earlier), len(run.Steps[2].Earlier))
	}
}

func TestRerunFromFailedStepRunsOnlyTheFailedItem(t *testing.T) {
	lib, fixed := flakyLibrary(t)
	lib.Tools["picky"] = Tool{Run: `[ {{value}} != two ] || test -f ` + fixed + ` || { echo "no two" >&2; exit 1; }; echo ok`}
	lib.Chains["fragile"] = &Chain{Name: "fragile", Inputs: []Input{{Name: "value"}}, Steps: []Step{
		{ID: "say", Title: "Say it", Tool: "say"},
		{ID: "picky", Title: "Picky", Tool: "picky"},
	}}
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "list", Title: "List", Tool: "items"},
		{ID: "each", Title: "Each", Chain: "fragile", With: map[string]string{"value": "{{list.items}}"}},
	}}
	run, folder := execute(t, lib, chain)
	each := run.Steps[1]
	if got := statuses(each.Children); run.Status != "failed" || !reflect.DeepEqual(got, []string{"done", "failed", "done"}) {
		t.Fatalf("first attempt = %s %v", run.Status, got)
	}
	if err := os.WriteFile(fixed, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	rerunFailed(t, lib, chain, run, folder)

	each = run.Steps[1]
	if got := statuses(each.Children); run.Status != "done" || !reflect.DeepEqual(got, []string{"done", "done", "done"}) {
		t.Fatalf("rerun = %s %v (%s)", run.Status, got, run.Reason)
	}
	for i, item := range each.Children {
		say, picky := item.Children[0], item.Children[1]
		if say.Round != 0 {
			t.Errorf("item %d said it again", i+1)
		}
		if want := map[bool]int{true: 1, false: 0}[i == 1]; picky.Round != want {
			t.Errorf("item %d picky round = %d, want %d", i+1, picky.Round, want)
		}
	}
	if each.Headline != "fragile ran 3 times: 3 done" {
		t.Errorf("each headline = %q", each.Headline)
	}
}

func TestRerunFromFailedStepKeepsTheNotesSentBack(t *testing.T) {
	lib, fixed := flakyLibrary(t)
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "change", Title: "Change", Tool: "say", With: map[string]string{"value": "changed"}},
		{ID: "review", Title: "Review", User: "Good?", Revise: "change"},
		{ID: "flaky", Title: "Flaky", Tool: "flaky", With: map[string]string{"value": "{{feedback}}"}},
	}}
	run, folder := execute(t, lib, chain, "revise:smaller", "approve")
	if run.Status != "failed" || run.Feedback != "smaller" {
		t.Fatalf("first attempt = %s, feedback %q", run.Status, run.Feedback)
	}
	if err := os.WriteFile(fixed, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	rerunFailed(t, lib, chain, run, folder)

	if got := run.Steps[2].Output["output"]; run.Status != "done" || got != "smaller" {
		t.Errorf("rerun = %s, flaky read feedback %v, want smaller", run.Status, got)
	}
}

func innerLibrary(t *testing.T) (*Library, string) {
	t.Helper()
	lib, fixed := flakyLibrary(t)
	lib.Chains["inner"] = &Chain{Name: "inner", Inputs: []Input{{Name: "value"}}, Steps: []Step{
		{ID: "say", Title: "Say it", Tool: "say"},
		{ID: "count", Title: "Count", Tool: "count"},
	}}
	return lib, fixed
}

func failThenFix(t *testing.T, lib *Library, chain *Chain, fixed string) (*Run, string) {
	t.Helper()
	run, folder := execute(t, lib, chain)
	if run.Status != "failed" {
		t.Fatalf("first attempt = %s (%s)", run.Status, run.Reason)
	}
	if err := os.WriteFile(fixed, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return run, folder
}

func refuseShorterInner(t *testing.T, lib *Library, chain *Chain, run *Run) {
	t.Helper()
	inner := lib.Chains["inner"]
	lib.Chains["inner"] = &Chain{Name: "inner", Steps: inner.Steps[:1]}
	if fits(lib, chain, run.Steps) {
		t.Error("the run fits after inner lost a step")
	}
	lib.Chains["inner"] = inner
}

func TestRerunKeepsAFinishedSubChain(t *testing.T) {
	lib, fixed := innerLibrary(t)
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "nested", Title: "Nested", Chain: "inner", With: map[string]string{"value": "deep"}},
		{ID: "flaky", Title: "Flaky", Tool: "flaky", With: map[string]string{"value": "{{nested.say.output}}-{{count.word}}"}},
		{ID: "check", Title: "Check", User: "Keep {{flaky.output}}?"},
	}}
	run, folder := failThenFix(t, lib, chain, fixed)
	refuseShorterInner(t, lib, chain, run)

	rerunFailed(t, lib, chain, run, folder, "approve:kept")

	if got := run.Steps[1].Output["output"]; run.Status != "done" || got != "deep-kept" {
		t.Errorf("rerun = %s (%s), flaky said %v", run.Status, run.Reason, got)
	}
	if check := run.Steps[2]; check.Question != "Keep deep-kept?" || check.Headline != "Approved: kept" {
		t.Errorf("check = %q, %q", check.Question, check.Headline)
	}
	if counted, _ := os.ReadFile(filepath.Join(folder, "counted")); string(counted) != "x\n" {
		t.Errorf("the sub-chain ran again: counted %q", counted)
	}
}

func TestRerunKeepsAFinishedListStep(t *testing.T) {
	lib, fixed := innerLibrary(t)
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "list", Title: "List", Tool: "items"},
		{ID: "each", Title: "Each", Chain: "inner", With: map[string]string{"value": "{{list.items}}"}},
		{ID: "flaky", Title: "Flaky", Tool: "flaky", With: map[string]string{"value": "{{each.summary}}"}},
	}}
	run, folder := failThenFix(t, lib, chain, fixed)
	refuseShorterInner(t, lib, chain, run)

	rerunFailed(t, lib, chain, run, folder)

	if got := run.Steps[2].Output["output"]; run.Status != "done" || got != "inner ran 3 times: 3 done" {
		t.Errorf("rerun = %s (%s), flaky said %v", run.Status, run.Reason, got)
	}
	for _, item := range run.Steps[1].Children {
		if counted, _ := os.ReadFile(filepath.Join(folder, item.Path, "counted")); string(counted) != "x\n" {
			t.Errorf("item %s ran again: counted %q", item.Path, counted)
		}
	}
}

func TestRerunResumesAKeptAgentSession(t *testing.T) {
	fakeClaude(t, `case "$*" in
*"--resume s1"*) echo '{"type":"result","result":"resumed","session_id":"s1"}' ;;
*) echo '{"type":"result","result":"fresh","session_id":"s1"}' ;;
esac
`)
	lib, fixed := flakyLibrary(t)
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "do", Title: "Do", Agent: "do"},
		{ID: "flaky", Title: "Flaky", Tool: "flaky", With: map[string]string{"value": "x"}},
		{ID: "more", Title: "More", Agent: "do", Resume: "do"},
	}, Prompts: map[string]Prompt{"do": {Text: "Do it."}}}
	run, folder := failThenFix(t, lib, chain, fixed)

	rerunFailed(t, lib, chain, run, folder)

	if got := run.Steps[2].Output["output"]; run.Status != "done" || got != "resumed" || run.Steps[0].Round != 0 {
		t.Errorf("rerun = %s (%s), more said %v", run.Status, run.Reason, got)
	}
}

func TestFitsRefusesAChangedChain(t *testing.T) {
	lib, _ := flakyLibrary(t)
	chain := &Chain{Name: "main", Steps: []Step{{ID: "first", Title: "First", Tool: "count"}}}
	run, _ := execute(t, lib, chain)
	if !fits(lib, chain, run.Steps) {
		t.Error("the same chain does not fit")
	}
	grown := &Chain{Name: "main", Steps: append(chain.Steps, Step{ID: "more", Title: "More", Tool: "count"})}
	if fits(lib, grown, run.Steps) {
		t.Error("a chain with another step fits")
	}
	renamed := &Chain{Name: "main", Steps: []Step{{ID: "first", Title: "First", Tool: "say"}}}
	if fits(lib, renamed, run.Steps) {
		t.Error("a step that now uses another tool fits")
	}
}
