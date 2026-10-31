package main

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func itemsWaiting(t *testing.T, core *Core, project string) *Run {
	t.Helper()
	item := func(n, title, status string) *StepRun {
		return &StepRun{ID: n, Path: "each." + n, Title: title, Kind: "item", Status: "running", Children: []*StepRun{
			{ID: "approve", Path: "each." + n + ".approve", Title: "Approve it", Kind: "user", Status: status, Question: "Keep " + title + "?"},
		}}
	}
	return surfaceRun(t, core, project, func(run *Run) {
		run.Status = "waiting"
		run.Steps = append(run.Steps, &StepRun{ID: "each", Path: "each", Title: "Each", Kind: "chain", Status: "running",
			Children: []*StepRun{item("1", "one", "waiting"), item("2", "two", "waiting"), item("3", "three", "done")}})
	})
}

const twoWaiting = "- each.1.approve: Approve it (one)\n- each.2.approve: Approve it (two)"

func TestDecideNamesTheStepWhenSeveralWait(t *testing.T) {
	core, project := surfaceProject(t)
	run := itemsWaiting(t, core, project)

	if err := core.Decide(run.ID, decision{Choice: "approve"}); err == nil || err.Error() != "2 steps wait for you; name one with -step:\n"+twoWaiting {
		t.Errorf("without a step: %v", err)
	}
	if err := core.Decide(run.ID, decision{Step: "each.3.approve", Choice: "approve"}); err == nil || err.Error() != "each.3.approve is not waiting for you; waiting now:\n"+twoWaiting {
		t.Errorf("a step that is not waiting: %v", err)
	}
	if err := core.Decide(run.ID, decision{Step: "each.2.approve", Choice: "revise", Note: "x"}); err == nil || !strings.Contains(err.Error(), "cannot send the run back") {
		t.Errorf("revise without a revise step: %v", err)
	}
	if err := core.Decide(run.ID, decision{Step: "each.2.approve", Choice: "reject"}); err != nil {
		t.Fatal(err)
	}
	if err := core.Decide(run.ID, decision{Step: "each.2.approve", Choice: "approve"}); err == nil || err.Error() != "a decision for each.2.approve is already being applied" {
		t.Errorf("deciding twice: %v", err)
	}
	if err := core.Decide(run.ID, decision{Step: "each.1.approve", Choice: "approve"}); err != nil {
		t.Errorf("the other item: %v", err)
	}
	var sent decision
	if err := readJSON(core.store.runDir(run.ID)+"/"+decisionFile("each.2.approve"), &sent); err != nil || sent.Step != "each.2.approve" || sent.Choice != "reject" {
		t.Errorf("signal = %+v, %v", sent, err)
	}
}

func TestDecideFillsInTheOnlyWaitingStep(t *testing.T) {
	core, project := surfaceProject(t)
	run := surfaceRun(t, core, project, func(run *Run) {
		run.Status = "waiting"
		run.Steps[0].Status = "waiting"
	})
	if err := core.Decide(run.ID, decision{Choice: "approve"}); err != nil {
		t.Fatal(err)
	}
	var sent decision
	if err := readJSON(core.store.runDir(run.ID)+"/"+decisionFile("greet"), &sent); err != nil || sent.Step != "greet" {
		t.Errorf("signal = %+v, %v", sent, err)
	}
}

func TestCLIDecideNamesTheStep(t *testing.T) {
	core, project := surfaceProject(t)
	run := itemsWaiting(t, core, project)
	_, err := surfaceCLI(t, "reject", run.ID)
	surfaceFails(t, err, "2 steps wait for you; name one with -step:\n"+twoWaiting)
	out, err := surfaceCLI(t, "approve", run.ID, "-step", "each.2.approve")
	surfaceContains(t, out, err, "approved "+run.ID)
	if !core.store.hasSignal(run.ID, decisionFile("each.2.approve")) {
		t.Error("no decision written for each.2.approve")
	}
}

func TestWaitingStepInAnItemSaysWhichStepToName(t *testing.T) {
	user := &StepRun{Path: "each.2.approve", Title: "T", Status: "waiting", Question: "Q?"}
	if got, want := stepLine(user, "", "http://x/#/run/1"), "⏸ T: Q?\n  answer with swarm approve / swarm reject, adding -step each.2.approve, or on the page: http://x/#/run/1\n"; got != want {
		t.Errorf("user step = %q, want %q", got, want)
	}
	asking := &StepRun{Path: "each.2.plan", Title: "P", Status: "waiting", Asks: []*Ask{{Questions: []string{"Which?"}}}}
	want := "? P asks:\n  1. Which?\n  answer with swarm answer RUN -a TEXT (once per question, in order) / swarm answer RUN -decide (it chooses), adding -step each.2.plan\n"
	if got := stepLine(asking, "", ""); got != want {
		t.Errorf("asking step = %q, want %q", got, want)
	}
}

func TestMemoryAddFromManyProcessesAtOnceKeepsEveryNote(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := openCore(home).RememberNotes(project, "", []string{fmt.Sprintf("note %d", i)}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	memory := readMemory(project)
	for i := range 40 {
		if !strings.Contains(memory, fmt.Sprintf("- note %d\n", i)) {
			t.Errorf("note %d is missing", i)
		}
	}
}
