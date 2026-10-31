package main

import (
	"strings"
	"testing"
	"time"
)

func TestCLIRunFollowsToTheEnd(t *testing.T) {
	shorten(t, &followEvery, 10*time.Millisecond)
	core, project := surfaceProject(t)
	_, err := surfaceCLI(t, "run")
	surfaceFails(t, err, "swarm run needs a chain name")
	_, err = surfaceCLI(t, "run", "ghost")
	surfaceFails(t, err, "no chain named ghost")

	out, err := surfaceCLI(t, "run", "hello")
	surfaceContains(t, out, err, "following; Ctrl-C detaches", "✓ Greet: hello", "\ndone\n")
	if strings.Contains(out, "on the page") {
		t.Errorf("no page is running, but: %s", out)
	}

	addr := surfaceServer(t, core)
	out, err = surfaceCLI(t, "run", "broken", "-addr", addr)
	surfaceFails(t, err, "the run did not finish")
	for _, want := range []string{"on the page: http://" + addr + "/#/run/", "✗ Boom: broken", "failed: Boom failed: broken"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, run := range core.store.list(project) {
		surfaceWait(t, core, run.ID, "done", "failed")
	}

	_, err = surfaceOutput(t, func() error { return (&cli{core: core}).follow("0101-000000") })
	surfaceFails(t, err, "no run 0101-000000")
}

func TestCLIRunDetachedAndApproved(t *testing.T) {
	core, project := surfaceProject(t)
	out, err := surfaceCLI(t, "run", "ask", "-detach")
	surfaceContains(t, out, err, "detached; poll with swarm show ")
	id := strings.TrimPrefix(strings.SplitN(out, "\n", 2)[0], "run ")
	surfaceWait(t, core, id, "waiting")

	out, err = surfaceCLI(t, "show", id)
	surfaceContains(t, out, err, id+"  waiting  Ask", "⏸ OK: Keep it?", "swarm revise -note TEXT (back to Greet)")
	out, err = surfaceCLI(t, "runs")
	surfaceContains(t, out, err, id+"  waiting")
	out, err = surfaceCLI(t, "approve", id, "-note", "fine")
	surfaceContains(t, out, err, "approved "+id)
	run := surfaceWait(t, core, id, "done")
	if run.Steps[1].Headline != "Approved: fine" || run.Project != project {
		t.Errorf("approved run = %+v", run.Steps[1])
	}
}

func TestCLIDecideAndCancel(t *testing.T) {
	core, project := surfaceProject(t)
	waiting := func() string {
		return surfaceRun(t, core, project, func(run *Run) {
			run.Status = "waiting"
			run.Steps[0].Status, run.Steps[0].Revise = "waiting", "Greet"
		}).ID
	}
	_, err := surfaceCLI(t, "approve")
	surfaceFails(t, err, "swarm approve needs a run id")
	_, err = surfaceCLI(t, "reject", "0101-000000")
	surfaceFails(t, err, "no run 0101-000000")
	sent := waiting()
	out, err := surfaceCLI(t, "revise", sent, "-note", "again")
	surfaceContains(t, out, err, "sent back "+sent)
	rejected := waiting()
	out, err = surfaceCLI(t, "reject", rejected)
	surfaceContains(t, out, err, "rejected "+rejected)

	_, err = surfaceCLI(t, "cancel")
	surfaceFails(t, err, "swarm cancel needs a run id")
	finished := surfaceRun(t, core, project, nil)
	_, err = surfaceCLI(t, "cancel", finished.ID)
	surfaceFails(t, err, "no such run in progress")
	out, err = surfaceCLI(t, "cancel", sent)
	surfaceContains(t, out, err, "stopping "+sent)
}

func TestCLIShow(t *testing.T) {
	core, project := surfaceProject(t)
	_, err := surfaceCLI(t, "show")
	surfaceFails(t, err, "swarm show needs a run id")
	_, err = surfaceCLI(t, "show", "0101-000000")
	surfaceFails(t, err, "no run 0101-000000")

	run := surfaceRun(t, core, project, func(run *Run) {
		run.Status, run.Subject, run.Reason = "failed", "the subject", "Boom failed: broken"
		run.Steps = append(run.Steps, &StepRun{Path: "group", Title: "Group", Status: "failed", Error: "1 of 1 failed",
			Children: []*StepRun{{Path: "group.0", Title: "Item", Status: "failed", Error: "broken\nmore"}}})
	})
	addr := surfaceServer(t, core)
	out, err := surfaceCLI(t, "show", run.ID, "-addr", addr)
	surfaceContains(t, out, err, run.ID+"  failed  Hello\nthe subject", "✓ Greet\n", "✗ Group: 1 of 1 failed\n", "  ✗ Item: broken\n", "\nBoom failed: broken\n")
}

func TestStepLine(t *testing.T) {
	long := strings.Repeat("x", 170)
	tests := []struct {
		step StepRun
		link string
		want string
	}{
		{StepRun{Title: "T", Kind: "tool", Status: "running"}, "", "… T [tool]\n"},
		{StepRun{Title: "T", Status: "waiting", Question: "Q?"}, "", "⏸ T: Q?\n  answer with swarm approve / swarm reject\n"},
		{StepRun{Title: "T", Status: "waiting", Question: "Q?", Revise: "Fix"}, "http://x/#/run/1",
			"⏸ T: Q?\n  answer with swarm approve / swarm revise -note TEXT (back to Fix) / swarm reject, or on the page: http://x/#/run/1\n"},
		{StepRun{Title: "T", Status: "done", Headline: "\n\nfirst\nsecond"}, "", "✓ T: first\n"},
		{StepRun{Title: "T", Status: "done", Headline: long}, "", "✓ T: " + long[:160] + "…\n"},
		{StepRun{Title: "T", Status: "failed", Error: "oops"}, "", "✗ T: oops\n"},
		{StepRun{Title: "T", Status: "rejected", Headline: "Rejected"}, "", "✗ T (rejected): Rejected\n"},
		{StepRun{Title: "T", Status: "stopped"}, "", "■ T (ended the run)\n"},
		{StepRun{Title: "T", Status: "skipped"}, "", "- T (skipped)\n"},
		{StepRun{Title: "T", Status: "pending"}, "", "  T\n"},
	}
	for _, tt := range tests {
		step := tt.step
		if got := stepLine(&step, "", tt.link); got != tt.want {
			t.Errorf("stepLine(%s) = %q, want %q", step.Status, got, tt.want)
		}
	}
}

func TestReportPrintsOnlyChangedSteps(t *testing.T) {
	steps := []*StepRun{{Path: "a", Title: "A", Status: "done"}}
	seen := map[string]string{}
	out, _ := surfaceOutput(t, func() error {
		report(steps, "", seen, "")
		report(steps, "", seen, "")
		return nil
	})
	if out != "✓ A\n" {
		t.Errorf("report = %q", out)
	}
}
