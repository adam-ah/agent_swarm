package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func loopChain(approveAt string, again Step) *Chain {
	again.ID, again.Title, again.Repeat, again.When = "again", "Review again", "review", "review.verdict == CHANGES"
	return &Chain{Name: "main", Steps: []Step{
		{ID: "review", Title: "Review", Tool: "review", With: map[string]string{"approve_at": approveAt}},
		{ID: "fix", Title: "Fix", Tool: "say", With: map[string]string{"value": "fixed"}, When: "review.verdict == CHANGES"},
		again,
		{ID: "after", Title: "After", Tool: "say", With: map[string]string{"value": "after"}},
	}}
}

func runLoop(t *testing.T, approveAt string, again Step) (*Run, string) {
	t.Helper()
	lib := sequenceLibrary(t)
	lib.Tools["review"] = Tool{Run: `n=$(( $(cat {{run_folder}}/n 2>/dev/null || echo 0) + 1 )); echo $n > {{run_folder}}/n
if [ $n -ge {{approve_at}} ]; then echo '{"verdict": "APPROVE"}'; else echo '{"verdict": "CHANGES"}'; fi`}
	chain := loopChain(approveAt, again)
	if problems := lib.check(chain); len(problems) > 0 {
		t.Fatalf("chain problems: %v", problems)
	}
	return execute(t, lib, chain)
}

func reviews(t *testing.T, folder string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(folder, "n"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(raw))
}

func TestRepeatGoesBackUntilItsConditionFails(t *testing.T) {
	run, folder := runLoop(t, "2", Step{})
	if run.Status != "done" || reviews(t, folder) != "2" {
		t.Fatalf("run %s after %s reviews (%s)", run.Status, reviews(t, folder), run.Reason)
	}
	review, fix, again, after := run.Steps[0], run.Steps[1], run.Steps[2], run.Steps[3]
	if got := statuses(run.Steps); !slices.Equal(got, []string{"done", "skipped", "skipped", "done"}) {
		t.Errorf("statuses = %v", got)
	}
	if review.Rounds != "2 of 3" || review.Loop != "Round 2 of 3: Review again sent the run back here" || len(review.Earlier) != 1 {
		t.Errorf("review rounds %q, loop %q, %d earlier", review.Rounds, review.Loop, len(review.Earlier))
	}
	if fix.Rounds != "2 of 3" || len(fix.Earlier) != 1 || fix.Earlier[0].Status != "done" {
		t.Errorf("fix rounds %q, earlier %v", fix.Rounds, fix.Earlier)
	}
	if len(again.Earlier) != 1 || again.Earlier[0].Headline != "Sent the run back to Review for round 2 of 3" || again.Repeats != 1 {
		t.Errorf("again earlier %v, repeats %d", again.Earlier, again.Repeats)
	}
	if after.Output["output"] != "after" {
		t.Errorf("after = %v", after.Output)
	}
}

func TestRepeatGoesOnWhenItsRoundsRunOut(t *testing.T) {
	run, folder := runLoop(t, "9", Step{})
	if run.Status != "done" || reviews(t, folder) != "3" {
		t.Fatalf("run %s after %s reviews", run.Status, reviews(t, folder))
	}
	if again := run.Steps[2]; again.Status != "done" || again.Headline != "All 3 rounds ran, so the run went on" {
		t.Errorf("again = %s %q", again.Status, again.Headline)
	}
}

func TestRepeatCanFailWhenItsRoundsRunOut(t *testing.T) {
	run, folder := runLoop(t, "9", Step{Rounds: 2, Fail: "out_of_rounds"})
	if want := "Review again failed: all 2 rounds ran and it still wanted to go back to Review"; run.Status != "failed" || run.Reason != want {
		t.Fatalf("run %s %q, want failed with %q", run.Status, run.Reason, want)
	}
	if reviews(t, folder) != "2" || run.Steps[3].Status != "skipped" {
		t.Errorf("%s reviews, after %s", reviews(t, folder), run.Steps[3].Status)
	}
}

func TestRepeatCanFailWhenItEndsEarly(t *testing.T) {
	run, _ := runLoop(t, "1", Step{Fail: "ended_early"})
	if want := "Review again failed: the loop ended after round 1 of 3, because review.verdict is not CHANGES"; run.Status != "failed" || run.Reason != want {
		t.Fatalf("run %s %q, want failed with %q", run.Status, run.Reason, want)
	}
	passed, _ := runLoop(t, "9", Step{Fail: "ended_early"})
	if passed.Status != "done" {
		t.Errorf("a loop that ran every round = %s %q", passed.Status, passed.Reason)
	}
}

func TestStepSkippedInALaterRoundDropsItsEarlierFields(t *testing.T) {
	lib := sequenceLibrary(t)
	lib.Tools["review"] = Tool{Run: `n=$(( $(cat {{run_folder}}/n 2>/dev/null || echo 0) + 1 )); echo $n > {{run_folder}}/n
if [ $n -ge 2 ]; then echo '{"verdict": "APPROVE"}'; else echo '{"verdict": "CHANGES"}'; fi`}
	lib.Tools["respond"] = Tool{Run: `echo '{"changed": "yes"}'`}
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "review", Title: "Review", Tool: "review"},
		{ID: "respond", Title: "Respond", Tool: "respond", When: "review.verdict == CHANGES"},
		{ID: "again", Title: "Review again", Repeat: "review", When: "respond.changed == yes"},
	}}
	run, folder := execute(t, lib, chain)
	if run.Status != "done" || reviews(t, folder) != "2" {
		t.Fatalf("run %s after %s reviews (%s)", run.Status, reviews(t, folder), run.Reason)
	}
	if again := run.Steps[2]; again.Status != "skipped" || again.Headline != "Skipped: respond.changed is not yes" {
		t.Errorf("again = %s %q", again.Status, again.Headline)
	}
}

func TestSubChainFieldsComeFromItsLatestRound(t *testing.T) {
	lib := sequenceLibrary(t)
	lib.Tools["check"] = Tool{Run: `n=$(( $(cat {{run_folder}}/n 2>/dev/null || echo 0) + 1 )); echo $n > {{run_folder}}/n
if [ $n -ge 2 ]; then echo '{"changed": "no"}'; else echo '{"changed": "yes"}'; fi`}
	lib.Chains["inner"] = &Chain{Name: "inner", Steps: []Step{{ID: "check", Title: "Check", Tool: "check"}}}
	run, folder := execute(t, lib, &Chain{Name: "main", Steps: []Step{
		{ID: "work", Title: "Work", Chain: "inner"},
		{ID: "again", Title: "Again", Repeat: "work", Rounds: 4, When: "check.changed == yes"},
	}})
	if run.Status != "done" || reviews(t, folder) != "2" {
		t.Errorf("run %s after %s checks; again %q", run.Status, reviews(t, folder), run.Steps[1].Headline)
	}
}

func TestRerunInsideALoopKeepsItsRound(t *testing.T) {
	lib := sequenceLibrary(t)
	lib.Tools["count"] = Tool{Run: `echo x >> {{run_folder}}/n`}
	lib.Tools["fix"] = Tool{Run: `echo x >> {{run_folder}}/fixes; [ "$(wc -l < {{run_folder}}/fixes)" -ne 2 ] || { echo flaky >&2; exit 1; }`}
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "review", Title: "Review", Tool: "count"},
		{ID: "fix", Title: "Fix", Tool: "fix"},
		{ID: "again", Title: "Again", Repeat: "review"},
	}}
	run, folder := execute(t, lib, chain)
	if run.Status != "failed" {
		t.Fatalf("first attempt = %s %q", run.Status, run.Reason)
	}
	rerunFailed(t, lib, chain, run, folder)
	if raw, _ := os.ReadFile(filepath.Join(folder, "n")); run.Status != "done" || strings.Count(string(raw), "x") != 3 {
		t.Errorf("run %s after %d reviews, want 3; again %q", run.Status, strings.Count(string(raw), "x"), run.Steps[2].Headline)
	}
}

func TestReviseBeforeALoopRestartsItsRounds(t *testing.T) {
	lib := sequenceLibrary(t)
	run, _ := execute(t, lib, &Chain{Name: "main", Steps: []Step{
		{ID: "work", Title: "Work", Tool: "say", With: map[string]string{"value": "w"}},
		{ID: "ask", Title: "Ask", User: "Keep it?", Revise: "work"},
		{ID: "again", Title: "Ask again", Repeat: "ask", Rounds: 2},
	}}, "approve", "revise:redo", "approve", "approve")
	ask := run.Steps[1]
	if run.Status != "done" || len(ask.Earlier) != 3 || run.Steps[2].Headline != "All 2 rounds ran, so the run went on" {
		t.Errorf("run %s (%s), ask asked %d times, want 4; again %q", run.Status, run.Reason, len(ask.Earlier)+1, run.Steps[2].Headline)
	}
}

func TestRepeatWithoutAConditionRunsEveryRound(t *testing.T) {
	lib := sequenceLibrary(t)
	lib.Tools["count"] = Tool{Run: `echo x >> {{run_folder}}/n; wc -l < {{run_folder}}/n`}
	run, folder := execute(t, lib, &Chain{Name: "main", Steps: []Step{
		{ID: "count", Title: "Count", Tool: "count"},
		{ID: "again", Title: "Again", Repeat: "count", Rounds: 4},
	}})
	if raw, _ := os.ReadFile(filepath.Join(folder, "n")); run.Status != "done" || strings.Count(string(raw), "x") != 4 {
		t.Errorf("run %s, counted %q", run.Status, raw)
	}
}

func TestRunHeadingIsSetBeforeAnyStepRuns(t *testing.T) {
	lib := sequenceLibrary(t)
	chain := &Chain{Name: "main", Subject: "Say {{greeting}}", Vars: Fields{{Name: "greeting", Value: "hello"}}, Steps: []Step{
		{ID: "skip", Title: "Skip", Tool: "say", When: "greeting == bye"},
	}}
	if run, _ := execute(t, lib, chain); run.Subject != "Say hello" {
		t.Errorf("subject = %q", run.Subject)
	}
}

func TestCheckRepeatSteps(t *testing.T) {
	lib := sequenceLibrary(t)
	say := Step{ID: "say", Title: "Say", Tool: "say"}
	chain := func(again Step) *Chain {
		again.ID, again.Title = "again", "Again"
		return &Chain{Name: "main", Steps: []Step{say, again}}
	}
	cases := []struct {
		step Step
		want string
	}{
		{Step{Repeat: "say", Rounds: 5, Fail: "out_of_rounds", When: "say.output != done"}, ""},
		{Step{Repeat: "again"}, `step "again": repeat must name an earlier step in this chain, not "again"`},
		{Step{Repeat: "ghost"}, `step "again": repeat must name an earlier step in this chain, not "ghost"`},
		{Step{Repeat: "say", Rounds: 1}, `step "again": rounds is the most times the loop runs, 2 or more, not 1`},
		{Step{Repeat: "say", Rounds: -2}, `step "again": rounds is the most times the loop runs, 2 or more, not -2`},
		{Step{Repeat: "say", Fail: "sometimes"}, `step "again": fail is out_of_rounds or ended_early, not "sometimes"`},
		{Step{Repeat: "say", Always: true}, `step "again": a repeat step takes only when, rounds and fail`},
		{Step{Repeat: "say", With: map[string]string{"value": "x"}}, `step "again": a repeat step takes only when, rounds and fail`},
		{Step{Tool: "say", Rounds: 3}, `step "again": rounds and fail only apply to repeat steps`},
		{Step{Tool: "say", Repeat: "say"}, `step "again": needs exactly one of tool, agent, user, chain or repeat`},
	}
	for _, c := range cases {
		if got := strings.Join(lib.check(chain(c.step)), "; "); got != c.want {
			t.Errorf("%+v: problems %q, want %q", c.step, got, c.want)
		}
	}
}

func TestCheckRefusesCrossingLoops(t *testing.T) {
	lib := sequenceLibrary(t)
	say := func(id string) Step { return Step{ID: id, Title: id, Tool: "say"} }
	crossing := &Chain{Name: "main", Steps: []Step{say("a"), say("b"), {ID: "r1", Title: "R1", Repeat: "a"}, {ID: "r2", Title: "R2", Repeat: "b"}}}
	if got := strings.Join(lib.check(crossing), "; "); got != `step "r2": its loop from b crosses the loop of step "r1"; nest loops or keep them apart` {
		t.Errorf("crossing loops: %q", got)
	}
	nested := &Chain{Name: "main", Steps: []Step{say("a"), say("b"), {ID: "r1", Title: "R1", Repeat: "b"}, {ID: "r2", Title: "R2", Repeat: "a"}, {ID: "r3", Title: "R3", Repeat: "a"}}}
	if got := lib.check(nested); len(got) != 0 {
		t.Errorf("nested loops: %v", got)
	}
}

func TestRepeatStepWarningsAndCatalog(t *testing.T) {
	lib := sequenceLibrary(t)
	chain := &Chain{Name: "main", Title: "Main", Steps: []Step{
		{ID: "say", Title: "Say", Tool: "say", With: map[string]string{"value": "x"}},
		{ID: "again", Title: "Again", Repeat: "say", When: "ghost.verdict == CHANGES"},
	}}
	lib.Chains["main"] = chain
	if got := strings.Join(lib.warnings(chain), "; "); got != `step "again": run only if reads {{ghost.verdict}}, but there is no step ghost` {
		t.Errorf("warnings = %q", got)
	}
	if catalog := lib.catalog(); !strings.Contains(catalog, "again: Again [repeat say]") {
		t.Errorf("catalog:\n%s", catalog)
	}
}
