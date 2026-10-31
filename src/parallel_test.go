package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func startLive(t *testing.T, lib *Library, chain *Chain) (*Core, *Run, context.CancelFunc, <-chan struct{}) {
	t.Helper()
	store := openStore(filepath.Join(t.TempDir(), "runs"))
	id, err := store.claim()
	if err != nil {
		t.Fatal(err)
	}
	run := &Run{ID: id, Chain: chain.Name, Project: lib.Project, Status: "running", StartedAt: time.Now(), PID: os.Getpid(), Steps: expand(lib, chain, ""), inbox: newInbox()}
	r := &Runner{store: store, swarm: "true", home: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	finished, ended := make(chan struct{}), make(chan struct{})
	go r.listen(run, 5*time.Millisecond, finished)
	go func() {
		defer close(ended)
		r.execute(ctx, lib, chain, run, newScope(map[string]string{"run_folder": store.logDir(run)}))
		close(finished)
	}()
	t.Cleanup(func() {
		cancel()
		<-ended
	})
	return &Core{store: store}, run, cancel, ended
}

func gathering(t *testing.T, lib *Library, name string, count string) string {
	t.Helper()
	gate := filepath.Join(lib.Project, name)
	if err := os.Mkdir(gate, 0o755); err != nil {
		t.Fatal(err)
	}
	return strings.NewReplacer("GATE", gate, "COUNT", count).Replace(`touch GATE/{{value}}
for i in $(seq 150); do [ "$(ls GATE | wc -l)" -ge COUNT ] && break; sleep 0.02; done
[ "$(ls GATE | wc -l)" -ge COUNT ] || { echo "met $(ls GATE | wc -l) of COUNT" >&2; exit 1; }
`)
}

func TestParallelItemsRunAtOnce(t *testing.T) {
	lib := sequenceLibrary(t)
	lib.Tools["meet"] = Tool{Run: gathering(t, lib, "gate", "3") + "[ {{value}} != three ] || sleep 0.3\necho met"}
	lib.Tools["count"] = Tool{Run: "ls {{run_folder}}/each.*/said | wc -l"}
	lib.Chains["meet"] = &Chain{Name: "meet", Inputs: []Input{{Name: "value"}}, Steps: []Step{
		{ID: "meet", Title: "Meet the others", Tool: "meet"},
		{ID: "say", Title: "Say it", Tool: "say"},
	}}
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "list", Title: "List", Tool: "items"},
		{ID: "each", Title: "Each", Chain: "meet", Parallel: true, With: map[string]string{"value": "{{list.items}}"}},
		{ID: "after", Title: "After", Tool: "count"},
	}}
	run, folder := execute(t, lib, chain)

	each := run.Steps[1]
	if run.Status != "done" || each.Status != "done" || each.Headline != "meet ran 3 times: 3 done" {
		t.Fatalf("run %s (%s), each %s %q", run.Status, run.Reason, each.Status, each.Headline)
	}
	for i, item := range each.Children {
		if item.Status != "done" || item.EndedAt == nil || item.EndedAt.After(*each.EndedAt) {
			t.Errorf("item %d = %s, ended %v, the step ended %v", i, item.Status, item.EndedAt, each.EndedAt)
		}
		if said, err := os.ReadFile(filepath.Join(folder, item.Path, "said")); err != nil || string(said) != item.Title+"\n" {
			t.Errorf("item %d folder has %q, %v", i, said, err)
		}
	}
	if got := run.Steps[2].Output["output"]; got != "3" {
		t.Errorf("after found %v items done, want 3", got)
	}
}

func fragileLibrary(t *testing.T) *Library {
	t.Helper()
	lib := sequenceLibrary(t)
	lib.Tools["picky"] = Tool{Run: `[ {{value}} != two ] || { echo "no two" >&2; exit 1; }; sleep 0.2; echo ok`}
	lib.Tools["mark"] = Tool{Run: "touch {{run_folder}}/cleaned"}
	lib.Chains["fragile"] = &Chain{Name: "fragile", Inputs: []Input{{Name: "value"}}, Steps: []Step{
		{ID: "picky", Title: "Picky", Tool: "picky"},
		{ID: "cleanup", Title: "Clean up", Tool: "mark", Always: true},
	}}
	return lib
}

func fragileChain(parallel, keepGoing bool) *Chain {
	return &Chain{Name: "main", Steps: []Step{
		{ID: "list", Title: "List", Tool: "items"},
		{ID: "each", Title: "Each", Chain: "fragile", Parallel: parallel, KeepGoing: keepGoing, With: map[string]string{"value": "{{list.items}}"}},
		{ID: "after", Title: "After", Tool: "say", With: map[string]string{"value": "after"}},
	}}
}

func TestParallelFailureFailsTheStepOnceEveryItemHasEnded(t *testing.T) {
	lib := fragileLibrary(t)
	run, folder := execute(t, lib, fragileChain(true, false))

	each := run.Steps[1]
	if run.Status != "failed" || each.Status != "failed" || each.Error != "1 of 3 failed" || each.Headline != "fragile ran 3 times: 2 done, 1 failed" {
		t.Fatalf("run %s, each %s %q %q", run.Status, each.Status, each.Error, each.Headline)
	}
	if got := statuses(each.Children); !reflect.DeepEqual(got, []string{"done", "failed", "done"}) {
		t.Errorf("items = %v", got)
	}
	for i, item := range each.Children {
		if _, err := os.Stat(filepath.Join(folder, item.Path, "cleaned")); err != nil || item.Children[1].Status != "done" {
			t.Errorf("item %d cleanup = %s, %v", i, item.Children[1].Status, err)
		}
	}
	if run.Steps[2].Status != "skipped" {
		t.Errorf("after = %s, want skipped", run.Steps[2].Status)
	}
}

func TestKeepGoingCountsAFailedItemAndTheRunGoesOn(t *testing.T) {
	for _, parallel := range []bool{false, true} {
		lib := fragileLibrary(t)
		run, _ := execute(t, lib, fragileChain(parallel, true))

		each := run.Steps[1]
		if run.Status != "done" || each.Status != "done" || each.Error != "" || each.Headline != "fragile ran 3 times: 2 done, 1 failed" {
			t.Errorf("parallel %v: run %s (%s), each %s %q %q", parallel, run.Status, run.Reason, each.Status, each.Error, each.Headline)
		}
		if got := statuses(each.Children); !reflect.DeepEqual(got, []string{"done", "failed", "done"}) {
			t.Errorf("parallel %v: items = %v", parallel, got)
		}
		if run.Steps[2].Status != "done" {
			t.Errorf("parallel %v: after = %s, want done", parallel, run.Steps[2].Status)
		}
	}
}

func TestCancelStopsEveryParallelItem(t *testing.T) {
	quickStops(t)
	lib := fragileLibrary(t)
	lib.Tools["slow"] = Tool{Run: "sleep 20"}
	lib.Chains["slow"] = &Chain{Name: "slow", Inputs: []Input{{Name: "value"}}, Steps: []Step{
		{ID: "wait", Title: "Wait", Tool: "slow"},
		{ID: "cleanup", Title: "Clean up", Tool: "mark", Always: true},
	}}
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "list", Title: "List", Tool: "items"},
		{ID: "each", Title: "Each", Chain: "slow", Parallel: true, With: map[string]string{"value": "{{list.items}}"}},
	}}
	core, run, cancel, ended := startLive(t, lib, chain)
	awaitRun(t, core, run.ID, func(r *Run) bool {
		return len(r.Steps[1].Children) == 3 && reflect.DeepEqual(statuses(r.Steps[1].Children), []string{"running", "running", "running"})
	})
	stopped := time.Now()
	cancel()
	<-ended

	if took := time.Since(stopped); took > 10*time.Second || run.Status != "stopped" {
		t.Fatalf("run %s after %s", run.Status, took)
	}
	for i, item := range run.Steps[1].Children {
		if item.Status != "stopped" || item.Children[0].Status != "failed" || item.Children[1].Status != "done" {
			t.Errorf("item %d = %s, wait %s, cleanup %s", i, item.Status, item.Children[0].Status, item.Children[1].Status)
		}
	}
}

func waitingPaths(run *Run) []string {
	waiting := waitingSteps(run.Steps)
	paths := make([]string, 0, len(waiting))
	for _, step := range waiting {
		paths = append(paths, step.Path)
	}
	return paths
}

func TestParallelItemsWaitForYouAtOnce(t *testing.T) {
	lib := sequenceLibrary(t)
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "list", Title: "List", Tool: "items"},
		{ID: "each", Title: "Each", Chain: "sub", Parallel: true, With: map[string]string{"value": "{{list.items}}"}},
		{ID: "after", Title: "After", Tool: "say", With: map[string]string{"value": "after"}},
	}}
	core, run, _, ended := startLive(t, lib, chain)
	all := []string{"each.1.approve", "each.2.approve", "each.3.approve"}
	awaitRun(t, core, run.ID, func(r *Run) bool { return r.Status == "waiting" && reflect.DeepEqual(waitingPaths(r), all) })

	err := core.Decide(run.ID, decision{Choice: "approve"})
	want := "3 steps wait for you; name one with -step:\n- each.1.approve: Approve it (one)\n- each.2.approve: Approve it (two)\n- each.3.approve: Approve it (three)"
	if err == nil || err.Error() != want {
		t.Fatalf("deciding without a step = %v, want %q", err, want)
	}
	if err := core.Decide(run.ID, decision{Step: "each.2.approve", Choice: "reject", Note: "not two"}); err != nil {
		t.Fatal(err)
	}
	awaitRun(t, core, run.ID, func(r *Run) bool {
		return r.Steps[1].Children[1].Status == "rejected" && r.Status == "waiting" && len(waitingPaths(r)) == 2
	})
	for _, path := range []string{"each.1.approve", "each.3.approve"} {
		if err := core.Decide(run.ID, decision{Step: path, Choice: "approve"}); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	<-ended

	each := run.Steps[1]
	if got := statuses(each.Children); run.Status != "done" || !reflect.DeepEqual(got, []string{"done", "rejected", "done"}) {
		t.Fatalf("run %s (%s), items %v", run.Status, run.Reason, got)
	}
	if note := each.Children[1].Children[1].Output["note"]; note != "not two" {
		t.Errorf("item two's note = %v", note)
	}
	if run.Steps[2].Status != "done" {
		t.Errorf("after = %s, want done", run.Steps[2].Status)
	}
}

func TestNestedWavesRunInTurnWithTheirCardsAtOnce(t *testing.T) {
	lib := sequenceLibrary(t)
	table, log := filepath.Join(lib.Project, "table"), filepath.Join(lib.Project, "played")
	if err := os.Mkdir(table, 0o755); err != nil {
		t.Fatal(err)
	}
	lib.Tools["waves"] = Tool{Run: `echo '{"waves": ["w1", "w2"]}'`}
	lib.Tools["cards"] = Tool{Run: `printf '{"cards": ["%s-a", "%s-b", "%s-c"]}' {{wave}} {{wave}} {{wave}}`}
	lib.Tools["play"] = Tool{Run: strings.NewReplacer("TABLE", table, "LOG", log).Replace(`echo start {{card}} >> LOG
touch TABLE/{{card}}
for i in $(seq 150); do [ "$(ls TABLE | grep -c "^"{{wave}}-)" -ge 3 ] && break; sleep 0.02; done
[ "$(ls TABLE | grep -c "^"{{wave}}-)" -ge 3 ] || { echo "only some cards of" {{wave}} started >&2; exit 1; }
echo end {{card}} >> LOG`)}
	lib.Chains["card"] = &Chain{Name: "card", Inputs: []Input{{Name: "card"}}, Steps: []Step{{ID: "play", Title: "Play", Tool: "play"}}}
	lib.Chains["wave"] = &Chain{Name: "wave", Inputs: []Input{{Name: "wave"}}, Steps: []Step{
		{ID: "cards", Title: "Deal", Tool: "cards"},
		{ID: "each", Title: "Each card", Chain: "card", Parallel: true, With: map[string]string{"card": "{{cards.cards}}"}},
	}}
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "waves", Title: "Plan the waves", Tool: "waves"},
		{ID: "each", Title: "Each wave", Chain: "wave", With: map[string]string{"wave": "{{waves.waves}}"}},
	}}
	run, _ := execute(t, lib, chain)

	if run.Status != "done" {
		t.Fatalf("run %s (%s)", run.Status, run.Reason)
	}
	for _, wave := range run.Steps[1].Children {
		if got := statuses(wave.Children[1].Children); !reflect.DeepEqual(got, []string{"done", "done", "done"}) {
			t.Errorf("wave %s cards = %v", wave.Title, got)
		}
	}
	raw, _ := os.ReadFile(log)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	lastFirst, firstSecond := -1, len(lines)
	for i, line := range lines {
		if strings.Contains(line, "w1-") {
			lastFirst = i
		} else if i < firstSecond {
			firstSecond = i
		}
	}
	if len(lines) != 12 || lastFirst > firstSecond {
		t.Errorf("waves overlapped:\n%s", raw)
	}
}

func TestDryRunRunsParallelItemsInOrder(t *testing.T) {
	surfaceFiles(t, flowProject(t), map[string]string{
		".swarm/chains/fan.json":  `{"title": "Fan", "steps": [{"id": "list", "title": "List", "tool": "echo"}, {"id": "each", "title": "Each", "chain": "pick", "parallel": true, "with": {"value": "{{list.items}}"}}]}`,
		".swarm/chains/pick.json": `{"title": "Pick", "inputs": [{"name": "value", "label": "Value"}], "steps": [{"id": "ok", "title": "OK", "user": "Keep {{value}}?"}]}`,
	})
	out := dryRun(t, "fan", "-mock", `{"list": {"items": ["one", "two", "three"]}, "ok": ["approve", "reject", "approve"]}`)
	for _, want := range []string{
		"✓ each (chain pick) pick ran 3 times: 2 done, 1 rejected\n    ! runs its items in parallel in a real run; this test runs them sequentially",
		"✓ each.1.ok (user) mocked: decision=approve",
		"✗ each.2.ok (user) mocked: decision=reject",
		"✓ each.3.ok (user) mocked: decision=approve",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestCheckParallelAndKeepGoingOnlyApplyToPerItemChainSteps(t *testing.T) {
	lib := sequenceLibrary(t)
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "list", Title: "List", Tool: "items", Parallel: true},
		{ID: "once", Title: "Once", Chain: "sub", KeepGoing: true, With: map[string]string{"value": "word: {{list.word}}"}},
		{ID: "each", Title: "Each", Chain: "sub", Parallel: true, KeepGoing: true, With: map[string]string{"value": "{{list.items}}"}},
		{ID: "capped", Title: "Capped", Chain: "sub", MaxParallel: 4, With: map[string]string{"value": "{{list.items}}"}},
		{ID: "one", Title: "One", Chain: "sub", Parallel: true, MaxParallel: 1, With: map[string]string{"value": "{{list.items}}"}},
	}}
	want := []string{
		`step "list": parallel only applies to a chain step that runs once per item of a list`,
		`step "once": keep_going only applies to a chain step that runs once per item of a list`,
		`step "capped": max_parallel only applies with parallel`,
		`step "one": max_parallel is the most items that run concurrently, 2 or more, not 1`,
	}
	if got := lib.check(chain); !reflect.DeepEqual(got, want) {
		t.Errorf("check = %q, want %q", got, want)
	}
}

func TestMaxParallelCapsRunningItemsAndFillsFreedSlots(t *testing.T) {
	lib := sequenceLibrary(t)
	gate, log := filepath.Join(lib.Project, "gate"), filepath.Join(lib.Project, "log")
	lib.Tools["five"] = Tool{Run: `echo '{"items": ["a", "b", "c", "d", "e"]}'`}
	lib.Tools["hold"] = Tool{Run: strings.NewReplacer("GATE", gate, "LOG", log).Replace("mkdir -p GATE; touch GATE/{{value}}; ls GATE | wc -l >> LOG; sleep 0.3; rm GATE/{{value}}")}
	lib.Chains["hold"] = &Chain{Name: "hold", Inputs: []Input{{Name: "value"}}, Steps: []Step{{ID: "hold", Title: "Hold a slot", Tool: "hold"}}}
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "list", Title: "List", Tool: "five"},
		{ID: "each", Title: "Each", Chain: "hold", Parallel: true, MaxParallel: 2, With: map[string]string{"value": "{{list.items}}"}},
	}}
	run, _ := execute(t, lib, chain)

	if run.Status != "done" || run.Steps[1].Headline != "hold ran 5 times: 5 done" {
		t.Fatalf("run %s (%s), each %q", run.Status, run.Reason, run.Steps[1].Headline)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	most := 0
	for _, line := range strings.Fields(string(raw)) {
		n, _ := strconv.Atoi(line)
		most = max(most, n)
	}
	if lines := len(strings.Fields(string(raw))); lines != 5 || most != 2 {
		t.Errorf("%d items logged, at most %d running at once, want 5 and 2", lines, most)
	}
}

func TestEachHeadlineNamesTheParallelCapOnlyWhenItBites(t *testing.T) {
	step := Step{Chain: "build-card", Parallel: true}
	for count, want := range map[int]string{
		8: "Runs build-card 8 times in parallel, once per item of list.briefs",
		9: "Runs build-card 9 times in parallel (8 at most), once per item of list.briefs",
	} {
		if got := eachHeadline(step, count, "list.briefs"); got != want {
			t.Errorf("eachHeadline(%d) = %q, want %q", count, got, want)
		}
	}
	step.MaxParallel = 3
	if got := eachHeadline(step, 4, "list.briefs"); got != "Runs build-card 4 times in parallel (3 at most), once per item of list.briefs" {
		t.Errorf("eachHeadline with max_parallel 3 = %q", got)
	}
}

func TestItemLabelIsTheFileHeading(t *testing.T) {
	dir := t.TempDir()
	card := filepath.Join(dir, "12a3a.md")
	if err := os.WriteFile(card, []byte("---\nid: 12a3a\n---\n#  12a3a: open folders \nbody\n# later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(plain, []byte("no heading\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for item, want := range map[string]string{card: "12a3a: open folders", plain: "", dir: "", "12a3a.md": "", filepath.Join(dir, "gone.md"): ""} {
		if got := headingOf(item); got != want {
			t.Errorf("headingOf(%q) = %q, want %q", item, got, want)
		}
	}
}
