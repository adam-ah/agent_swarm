package main

import (
	"testing"
)

func statusSay(id string, always bool) Step {
	return Step{ID: id, Title: id, Tool: "say", With: map[string]string{"value": "{{run_status}}"}, Always: always}
}

func TestAlwaysStepsReadHowTheRunEnded(t *testing.T) {
	cases := []struct {
		name      string
		work      Step
		decisions []string
		want      string
	}{
		{name: "failed", work: Step{Tool: "fail"}, want: "failed"},
		{name: "stopped", work: Step{Tool: "say", With: map[string]string{"value": "x"}, StopIf: "output == x"}, want: "stopped"},
		{name: "rejected", work: Step{User: "Keep it?"}, decisions: []string{"reject"}, want: "rejected"},
		{name: "still going", work: Step{Tool: "say", With: map[string]string{"value": "x"}}, want: "running"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lib := sequenceLibrary(t)
			lib.Tools["fail"] = Tool{Run: "echo broke >&2; exit 1"}
			c.work.ID, c.work.Title = "work", "Work"
			chain := &Chain{Name: "main", Steps: []Step{statusSay("before", false), c.work, statusSay("tidy", true)}}
			run, folder := execute(t, lib, chain, c.decisions...)
			if deferred := run.Steps[2].Status == "deferred"; deferred != (c.want == "failed") {
				t.Fatalf("tidy %s after the run %s", run.Steps[2].Status, run.Status)
			}
			if c.want == "failed" {
				cleanUpFailed(t, lib, chain, run, folder)
			}
			if before, tidy := run.Steps[0].Output["output"], run.Steps[2].Output["output"]; before != "running" || tidy != c.want {
				t.Errorf("before saw %v, tidy saw %v, want running and %s", before, tidy, c.want)
			}
		})
	}
}

func TestAPerItemChainReadsHowItsItemEnded(t *testing.T) {
	lib := sequenceLibrary(t)
	lib.Tools["fail"] = Tool{Run: `[ {{value}} != two ] || { echo broke >&2; exit 1; }`}
	lib.Chains["card"] = &Chain{Name: "card", Inputs: []Input{{Name: "value"}}, Steps: []Step{{ID: "work", Title: "Work", Tool: "fail"}, statusSay("tidy", true)}}
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "list", Title: "List", Tool: "items"},
		{ID: "each", Title: "Each", Chain: "card", KeepGoing: true, With: map[string]string{"value": "{{list.items}}"}},
		statusSay("after", false),
		statusSay("end", true),
	}}
	run, folder := execute(t, lib, chain)
	if tidy, end := run.Steps[1].Children[1].Children[1].Status, run.Steps[3].Status; run.Status != "failed" || tidy != "deferred" || end != "deferred" {
		t.Fatalf("run %s (%s); the failed item's tidy %s, end %s; want failed, deferred, deferred", run.Status, run.Reason, tidy, end)
	}
	if want := "Each: card ran 3 times: 2 done, 1 failed"; run.Reason != want {
		t.Errorf("reason %q, want %q", run.Reason, want)
	}
	cleanUpFailed(t, lib, chain, run, folder)
	saw := make([]any, 0, len(run.Steps[1].Children))
	for _, item := range run.Steps[1].Children {
		saw = append(saw, item.Children[1].Output["output"])
	}
	if end := run.Steps[3].Output["output"]; end != "failed" {
		t.Errorf("end saw %v after cancel, want failed", end)
	}
	if want := []any{"running", "failed", "running"}; run.Status != "failed" || run.Steps[2].Output["output"] != "running" || len(saw) != 3 || saw[0] != want[0] || saw[1] != want[1] || saw[2] != want[2] {
		t.Errorf("run %s; items' tidy saw %v, want %v; after saw %v", run.Status, saw, want, run.Steps[2].Output["output"])
	}
	if got := summaryOf(run).FailedItems; got != 1 {
		t.Errorf("summary failed_items = %d, want 1", got)
	}
}

func TestFailedItemsCountNestedItemsButNotEarlierAttempts(t *testing.T) {
	failed := &StepRun{Kind: "item", Status: "failed"}
	steps := []*StepRun{
		{Kind: "chain", Status: "done", Children: []*StepRun{
			{Kind: "item", Status: "done", Children: []*StepRun{{Kind: "chain", Status: "done", Children: []*StepRun{failed, failed}}}},
			failed,
		}},
		{Kind: "tool", Status: "done", Earlier: []*StepRun{{Kind: "item", Status: "failed"}}},
		{Kind: "tool", Status: "failed"},
	}
	if got := failedItems(steps); got != 3 {
		t.Errorf("failedItems = %d, want 3", got)
	}
	for _, c := range []struct {
		status string
		failed int
		want   string
	}{{"done", 0, "done"}, {"done", 1, "done, 1 item failed"}, {"done", 4, "done, 4 items failed"}, {"failed", 2, "failed"}, {"stopped", 0, "stopped"}} {
		if got := statusText(c.status, c.failed); got != c.want {
			t.Errorf("statusText(%s, %d) = %q, want %q", c.status, c.failed, got, c.want)
		}
	}
}

func TestAlwaysStepsSeeAStoppedRun(t *testing.T) {
	quickStops(t)
	lib := sequenceLibrary(t)
	lib.Tools["slow"] = Tool{Run: "sleep 20"}
	core, run, cancel, ended := startLive(t, lib, &Chain{Name: "main", Steps: []Step{{ID: "slow", Title: "Slow", Tool: "slow"}, statusSay("tidy", true)}})
	awaitRun(t, core, run.ID, func(r *Run) bool { return r.Steps[0].Status == "running" })
	cancel()
	<-ended
	if run.Status != "stopped" || run.Steps[1].Output["output"] != "stopped" {
		t.Errorf("run %s; tidy saw %v, want stopped", run.Status, run.Steps[1].Output["output"])
	}
}
