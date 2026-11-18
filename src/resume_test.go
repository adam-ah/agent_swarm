package main

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func pickyLibrary(t *testing.T) (*Library, string) {
	t.Helper()
	lib, fixed := flakyLibrary(t)
	lib.Tools["picky"] = Tool{Run: `echo x >> {{run_folder}}/picked; [ {{value}} != two ] || test -f ` + fixed + ` || { echo "no two" >&2; exit 1; }; echo ok`}
	lib.Tools["mark"] = Tool{Run: "touch {{run_folder}}/cleaned"}
	lib.Chains["fragile"] = &Chain{Name: "fragile", Inputs: []Input{{Name: "value"}}, Steps: []Step{
		{ID: "picky", Title: "Picky", Tool: "picky"},
		{ID: "cleanup", Title: "Clean up", Tool: "mark", Always: true},
	}}
	return lib, fixed
}

func TestResumeRetriesOnlyTheFailedItemsOfAKeepGoingStep(t *testing.T) {
	lib, fixed := pickyLibrary(t)
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "list", Title: "List", Tool: "items"},
		{ID: "each", Title: "Each", Chain: "fragile", Parallel: true, KeepGoing: true, With: map[string]string{"value": "{{list.items}}"}},
		{ID: "after", Title: "After", Tool: "count"},
		{ID: "tidy", Title: "Tidy", Tool: "say", With: map[string]string{"value": "{{run_status}}"}, Always: true},
	}}
	run, folder := failThenFix(t, lib, chain, fixed)
	if got := statuses(run.Steps); !reflect.DeepEqual(got, []string{"done", "done", "done", "deferred"}) {
		t.Fatalf("first attempt = %v", got)
	}
	if _, err := os.Stat(filepath.Join(folder, "each.2", "cleaned")); !os.IsNotExist(err) {
		t.Fatalf("the failed item cleaned up before resume: %v", err)
	}

	rerunFailed(t, lib, chain, run, folder)

	if run.Status != "done" || run.Steps[3].Output["output"] != "running" || run.Steps[1].Headline != "fragile ran 3 times: 3 done" {
		t.Fatalf("resume = %s (%s), each %q, tidy saw %v", run.Status, run.Reason, run.Steps[1].Headline, run.Steps[3].Output["output"])
	}
	for i, item := range run.Steps[1].Children {
		picked, _ := os.ReadFile(filepath.Join(folder, item.Path, "picked"))
		if want := map[bool]string{true: "x\nx\n", false: "x\n"}[i == 1]; string(picked) != want {
			t.Errorf("item %d picked %q, want %q", i+1, picked, want)
		}
		if _, err := os.Stat(filepath.Join(folder, item.Path, "cleaned")); err != nil {
			t.Errorf("item %d did not clean up: %v", i+1, err)
		}
	}
	if counted, _ := os.ReadFile(filepath.Join(folder, "counted")); string(counted) != "x\nx\n" {
		t.Errorf("after ran %q, want twice", counted)
	}
}

func TestAFailedNestedItemLeavesItsWaveDoneAndTheRunGoesOn(t *testing.T) {
	lib, fixed := pickyLibrary(t)
	lib.Tools["waves"] = Tool{Run: `echo '{"items": ["w1"]}'`}
	lib.Chains["wave"] = &Chain{Name: "wave", Inputs: []Input{{Name: "value"}}, Steps: []Step{
		{ID: "list", Title: "List", Tool: "items"},
		{ID: "each", Title: "Each", Chain: "fragile", Parallel: true, KeepGoing: true, With: map[string]string{"value": "{{list.items}}"}},
	}}
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "plan", Title: "Plan", Tool: "waves"},
		{ID: "waves", Title: "Waves", Chain: "wave", With: map[string]string{"value": "{{plan.items}}"}},
		{ID: "after", Title: "After", Tool: "count"},
		{ID: "tidy", Title: "Tidy", Tool: "say", With: map[string]string{"value": "{{run_status}}"}, Always: true},
	}}
	run, folder := failThenFix(t, lib, chain, fixed)
	wave := run.Steps[1].Children[0]
	if got := statuses(run.Steps); !reflect.DeepEqual(got, []string{"done", "done", "done", "deferred"}) || wave.Status != "done" {
		t.Fatalf("first attempt = %v, wave %s", got, wave.Status)
	}
	if want := "Each: fragile ran 3 times: 2 done, 1 failed"; run.Reason != want || wave.Headline != want {
		t.Errorf("reason %q, wave headline %q, want %q", run.Reason, wave.Headline, want)
	}

	rerunFailed(t, lib, chain, run, folder)

	if run.Status != "done" || run.Steps[3].Output["output"] != "running" {
		t.Fatalf("resume = %s (%s), tidy saw %v", run.Status, run.Reason, run.Steps[3].Output["output"])
	}
	for i, item := range run.Steps[1].Children[0].Children[1].Children {
		if want := map[bool]int{true: 1, false: 0}[i == 1]; item.Children[0].Round != want {
			t.Errorf("item %d picky round %d, want %d", i+1, item.Children[0].Round, want)
		}
	}
	if counted, _ := os.ReadFile(filepath.Join(folder, "counted")); string(counted) != "x\nx\n" {
		t.Errorf("after ran %q, want twice", counted)
	}
}

func reviewLoop(t *testing.T, gate Step) (*Library, *Chain, string) {
	t.Helper()
	lib := sequenceLibrary(t)
	approve := filepath.Join(lib.Project, "approve")
	lib.Tools["review"] = Tool{Run: `echo x >> {{run_folder}}/reviews; v=CHANGES; [ ! -f ` + approve + ` ] || v=APPROVE; echo "{\"verdict\": \"$v\"}"`}
	lib.Tools["gate"] = Tool{Run: `[ {{review.verdict}} = APPROVE ] || { echo unfixed >&2; exit 1; }`}
	again := Step{ID: "again", Title: "Review again", Repeat: "review", Rounds: 2, When: "review.verdict == CHANGES"}
	steps := []Step{{ID: "review", Title: "Review", Tool: "review"}, again}
	if gate.ID == "" {
		steps[1].Fail = "out_of_rounds"
	} else {
		steps = append(steps, gate)
	}
	steps = append(steps, Step{ID: "tidy", Title: "Tidy", Tool: "say", With: map[string]string{"value": "{{review.verdict}}"}, Always: true})
	return lib, &Chain{Name: "main", Steps: steps}, approve
}

func reviewCount(t *testing.T, folder string) int {
	t.Helper()
	got, _ := os.ReadFile(filepath.Join(folder, "reviews"))
	return strings.Count(string(got), "x")
}

func resumeLoop(t *testing.T, lib *Library, chain *Chain, approve string) {
	t.Helper()
	run, folder := execute(t, lib, chain)
	if run.Status != "failed" || reviewCount(t, folder) != 2 || run.Steps[len(run.Steps)-1].Status != "deferred" {
		t.Fatalf("first attempt = %s (%s), %d reviews", run.Status, run.Reason, reviewCount(t, folder))
	}
	rerunFailed(t, lib, chain, run, folder)
	if run.Status != "failed" || reviewCount(t, folder) != 4 {
		t.Fatalf("resume = %s (%s), %d reviews, want failed after 4", run.Status, run.Reason, reviewCount(t, folder))
	}
	if err := os.WriteFile(approve, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	rerunFailed(t, lib, chain, run, folder)
	if tidy := run.Steps[len(run.Steps)-1]; run.Status != "done" || reviewCount(t, folder) != 5 || tidy.Output["output"] != "APPROVE" {
		t.Errorf("second resume = %s (%s), %d reviews, tidy saw %v", run.Status, run.Reason, reviewCount(t, folder), tidy.Output["output"])
	}
}

func TestResumeGivesALoopThatRanOutOfRoundsAFullSet(t *testing.T) {
	lib, chain, approve := reviewLoop(t, Step{})
	resumeLoop(t, lib, chain, approve)
}

func TestResumeGoesBackToRetryFrom(t *testing.T) {
	lib, chain, approve := reviewLoop(t, Step{ID: "gate", Title: "Gate", Tool: "gate", RetryFrom: "review"})
	resumeLoop(t, lib, chain, approve)
}

func TestResumeWithoutRetryFromRetriesOnlyTheFailedStep(t *testing.T) {
	lib, chain, _ := reviewLoop(t, Step{ID: "gate", Title: "Gate", Tool: "gate"})
	run, folder := execute(t, lib, chain)
	rerunFailed(t, lib, chain, run, folder)
	if gate := run.Steps[2]; reviewCount(t, folder) != 2 || gate.Status != "failed" || gate.Round != 1 {
		t.Errorf("resume ran %d reviews, gate %s round %d", reviewCount(t, folder), gate.Status, gate.Round)
	}
}

func TestRetryFromNamesAnEarlierStep(t *testing.T) {
	lib, chain, _ := reviewLoop(t, Step{ID: "gate", Title: "Gate", Tool: "gate", RetryFrom: "tidy"})
	if problems := lib.check(chain); !slices.ContainsFunc(problems, func(p string) bool {
		return strings.Contains(p, `retry_from must name an earlier step in this chain, not "tidy"`)
	}) {
		t.Errorf("problems = %v", problems)
	}
}

func TestResumeRetriesAFailedAgentInItsSession(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls")
	fakeClaude(t, `printf '%s\n' "$*" >> `+calls+`
case "$*" in
*"--resume s1"*) echo '{"type":"result","result":"resumed","session_id":"s1"}' ;;
*"Quick"*) echo '{"type":"result","is_error":true,"result":"spend limit","session_id":"s2"}' ;;
*) echo '{"type":"system","subtype":"init","session_id":"s1"}'
   echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}'
   exit 1 ;;
esac
`)
	lib := sequenceLibrary(t)
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "do", Title: "Do", Agent: "do"},
		{ID: "quick", Title: "Quick", Agent: "quick"},
	}, Prompts: map[string]Prompt{"do": {Text: "Do it."}, "quick": {Text: "Quick."}}}
	run, folder := execute(t, lib, chain)
	if do := run.Steps[0]; run.Status != "failed" || do.Session != "s1" || do.Actions != 1 {
		t.Fatalf("first attempt = %s (%s), session %q, %d actions", run.Status, run.Reason, do.Session, do.Actions)
	}

	rerunFailed(t, lib, chain, run, folder)

	do := run.Steps[0]
	if !do.Retry || do.Output["output"] != "resumed" || do.Resumed != "Do (failed attempt)" {
		t.Errorf("retry = %+v", do)
	}
	log, _ := os.ReadFile(calls)
	if !strings.Contains(string(log), "Your last attempt ended with this error:\n\nclaude ended without a result") {
		t.Errorf("the retry prompt is missing:\n%s", log)
	}
	if quick := run.Steps[1]; run.Status != "failed" || quick.Status != "failed" || quick.Session != "s2" {
		t.Fatalf("quick = %s, session %q", quick.Status, quick.Session)
	}

	rerunFailed(t, lib, chain, run, folder)

	log, _ = os.ReadFile(calls)
	if quick := run.Steps[1]; !quick.Retry || quick.Resumed != "" || strings.Count(string(log), "Quick.") != 2 || strings.Contains(string(log), "--resume s2") {
		t.Errorf("quick failed without progress, yet its retry resumed %q", quick.Resumed)
	}
}
