package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	return openStore(filepath.Join(t.TempDir(), "runs"))
}

func saveTestRun(t *testing.T, store *Store, run *Run) *Run {
	t.Helper()
	if err := os.MkdirAll(store.runDir(run.ID), 0o755); err != nil {
		t.Fatal(err)
	}
	store.update(run, func() {})
	return run
}

func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func TestStoreClaimFindsAFreeID(t *testing.T) {
	store := testStore(t)
	seen := map[string]bool{}
	for range 3 {
		id, err := store.claim()
		if err != nil || !runID.MatchString(id) || seen[id] {
			t.Fatalf("claim = %q, %v", id, err)
		}
		seen[id] = true
	}
}

func TestStoreClaimFailsWithoutAFolder(t *testing.T) {
	file := filepath.Join(t.TempDir(), "runs")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if id, err := openStore(file).claim(); err == nil {
		t.Errorf("claimed %s inside a file", id)
	}
	if os.Geteuid() == 0 {
		return
	}
	locked := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	if id, err := openStore(locked).claim(); err == nil {
		t.Errorf("claimed %s in a read-only folder", id)
	}
}

func TestStoreUpdateSkipsARunItCannotSave(t *testing.T) {
	store := testStore(t)
	run := saveTestRun(t, store, &Run{ID: "0101-000001", Chain: "c"})
	store.update(run, func() { run.Steps = []*StepRun{{Output: map[string]any{"bad": make(chan int)}}} })
	if got, err := store.read(run.ID); err != nil || len(got.Steps) != 0 {
		t.Errorf("read = %+v, %v; want the run saved before", got, err)
	}
}

func TestStoreReadCachesUntilTheFileChanges(t *testing.T) {
	store := testStore(t)
	run := saveTestRun(t, store, &Run{ID: "0101-000001", Chain: "c", Status: "done"})
	first, err := store.read(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second, _ := store.read(run.ID); second != first {
		t.Error("the second read did not come from the cache")
	}
	store.update(run, func() { run.Title = "a longer title than before" })
	third, err := store.read(run.ID)
	if err != nil || third == first || third.Title != "a longer title than before" {
		t.Errorf("read after a change = %+v, %v", third, err)
	}
}

func TestStoreReadRefuses(t *testing.T) {
	store := testStore(t)
	if err := writeFile(filepath.Join(store.runDir("0101-000002"), "run.json"), []byte("{")); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../etc", "0101-000001", "0101-000002"} {
		if run, err := store.read(id); err == nil {
			t.Errorf("read(%q) = %+v", id, run)
		}
		if run := store.load(id); run != nil {
			t.Errorf("load(%q) = %+v", id, run)
		}
	}
	saveTestRun(t, store, &Run{ID: "0101-000003"})
	if run := store.load("0101-000003"); run != nil {
		t.Errorf("loaded a run without a chain: %+v", run)
	}
}

func TestSettle(t *testing.T) {
	old, recent, dead := time.Now().Add(-time.Hour), time.Now(), deadPID(t)
	tests := []struct {
		name string
		run  Run
		want string
	}{
		{"finished", Run{Status: "done", PID: dead, StartedAt: old}, "done"},
		{"live process", Run{Status: "running", PID: os.Getpid(), StartedAt: old}, "running"},
		{"dead process", Run{Status: "running", PID: dead, StartedAt: old}, "interrupted"},
		{"waiting on a dead process", Run{Status: "waiting", PID: dead, StartedAt: recent}, "interrupted"},
		{"not started yet", Run{Status: "running", StartedAt: recent}, "running"},
		{"never started", Run{Status: "running", StartedAt: old}, "interrupted"},
		{"just resumed", Run{Status: "running", StartedAt: old, ResumedAt: &recent}, "running"},
		{"resumed long ago", Run{Status: "running", StartedAt: old, ResumedAt: &old}, "interrupted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := tt.run
			got := settle(&tt.run)
			if got.Status != tt.want {
				t.Errorf("status = %s, want %s", got.Status, tt.want)
			}
			if tt.want == "interrupted" && (got.Reason == "" || tt.run.Status != before.Status) {
				t.Errorf("reason %q, original now %s", got.Reason, tt.run.Status)
			}
		})
	}
}

func TestProcessAlive(t *testing.T) {
	for pid, want := range map[int]bool{-1: false, 0: false, os.Getpid(): true, 1: true, deadPID(t): false} {
		if got := processAlive(pid); got != want {
			t.Errorf("processAlive(%d) = %v, want %v", pid, got, want)
		}
	}
}

func TestStoreSnapshot(t *testing.T) {
	store := testStore(t)
	if raw := store.snapshot("0101-000001"); raw != nil {
		t.Errorf("snapshot of a missing run = %s", raw)
	}
	saveTestRun(t, store, &Run{ID: "0101-000001", Chain: "c", Status: "running", PID: deadPID(t)})
	var got Run
	if err := json.Unmarshal(store.snapshot("0101-000001"), &got); err != nil || got.ID != "0101-000001" || got.Status != "interrupted" {
		t.Errorf("snapshot = %+v, %v", got, err)
	}
}

func TestStoreListFiltersByProjectNewestFirst(t *testing.T) {
	store := testStore(t)
	if got := store.list("/a"); got == nil || len(got) != 0 {
		t.Errorf("empty list = %#v", got)
	}
	now := time.Now()
	saveTestRun(t, store, &Run{ID: "0101-000001", Chain: "c", Project: "/a", Status: "done", StartedAt: now.Add(-2 * time.Hour)})
	saveTestRun(t, store, &Run{ID: "0101-000002", Chain: "c", Project: "/b", Status: "done", StartedAt: now.Add(-time.Hour)})
	saveTestRun(t, store, &Run{ID: "0101-000003", Chain: "c", Status: "done", StartedAt: now.Add(-time.Minute)})
	saveTestRun(t, store, &Run{ID: "0101-000004", Chain: "c", Project: "/a", Status: "waiting", PID: os.Getpid(), StartedAt: now,
		Steps: []*StepRun{{ID: "go", Status: "done"}, {ID: "ok", Status: "waiting", Question: "Ship it?"}}})
	saveTestRun(t, store, &Run{ID: "0101-000005", Project: "/a", StartedAt: now})
	got := store.list("/a")
	ids := make([]string, 0, len(got))
	for _, sum := range got {
		ids = append(ids, sum.ID)
	}
	if want := []string{"0101-000004", "0101-000003", "0101-000001"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("list = %v, want %v", ids, want)
	}
	if got[0].Question != "Ship it?" || got[1].Question != "" {
		t.Errorf("questions = %q, %q", got[0].Question, got[1].Question)
	}
}

func TestStoreFindsArtifactsAndLogs(t *testing.T) {
	store := testStore(t)
	saveTestRun(t, store, &Run{ID: "0101-000001", Chain: "c", Status: "done", Steps: []*StepRun{
		{ID: "a", Log: "a-2", Earlier: []*StepRun{{ID: "a", Log: "a"}}},
		{ID: "b", Children: []*StepRun{{ID: "c", Log: "b.c", AskedAgain: "b.c-fields", Artifacts: []Artifact{{Path: "/p/x.md"}, {Path: "/p/y.md"}}}}},
	}})
	for path, want := range map[string]bool{"/p/y.md": true, "/p/z.md": false} {
		if got := store.hasArtifact("0101-000001", path); got != want {
			t.Errorf("hasArtifact(%s) = %v", path, got)
		}
	}
	for log, want := range map[string]bool{"a": true, "a-2": true, "b.c": true, "b.c-fields": true, "": false, "z": false} {
		if got := store.hasLog("0101-000001", log); got != want {
			t.Errorf("hasLog(%q) = %v", log, got)
		}
	}
	if store.hasArtifact("0101-000009", "/p/x.md") || store.hasLog("0101-000009", "a") {
		t.Error("found something in a missing run")
	}
}

func TestStoreSignals(t *testing.T) {
	store := testStore(t)
	id := "0101-000001"
	if store.hasSignal(id, cancelFile) {
		t.Error("a signal before any was sent")
	}
	if err := store.signal(id, cancelFile, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.signal(id, decisionFile, []byte(`{"choice":"approve"}`)); err != nil {
		t.Fatal(err)
	}
	if !store.hasSignal(id, cancelFile) || !store.hasSignal(id, decisionFile) {
		t.Error("signals were not kept")
	}
	for range 2 {
		if err := store.clearSignals(id); err != nil {
			t.Fatal(err)
		}
	}
	if store.hasSignal(id, cancelFile) || store.hasSignal(id, decisionFile) {
		t.Error("signals were not cleared")
	}
	if err := os.MkdirAll(filepath.Join(store.runDir(id), cancelFile, "inside"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.clearSignals(id); err == nil {
		t.Error("cleared a cancel folder that has files in it")
	}
}

func TestStoreRemove(t *testing.T) {
	store := testStore(t)
	if err := store.remove("../runs"); err == nil {
		t.Error("removed a path that is not a run id")
	}
	saveTestRun(t, store, &Run{ID: "0101-000001", Chain: "c"})
	if _, err := store.read("0101-000001"); err != nil {
		t.Fatal(err)
	}
	if err := store.remove("0101-000001"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.runDir("0101-000001")); !os.IsNotExist(err) {
		t.Errorf("run folder still there: %v", err)
	}
	if run, err := store.read("0101-000001"); err == nil {
		t.Errorf("read a removed run: %+v", run)
	}
}

func TestAgainCopiesChildrenAndKeepsStartedAttempts(t *testing.T) {
	started := time.Now()
	old := &StepRun{ID: "s", Path: "s", Status: "failed", Round: 1, StartedAt: &started, Children: []*StepRun{{ID: "c", Status: "done"}}}
	fresh := again(old)
	if fresh.Round != 2 || fresh.Status != "pending" || len(fresh.Children) != 1 || fresh.Children[0].Round != 1 {
		t.Errorf("again = %+v", fresh)
	}
	if len(fresh.Earlier) != 1 || fresh.Earlier[0] != old || old.Children != nil {
		t.Errorf("earlier = %v, old children = %v", fresh.Earlier, old.Children)
	}
	if never := again(&StepRun{ID: "n"}); len(never.Earlier) != 0 {
		t.Errorf("kept an attempt that never started: %v", never.Earlier)
	}
}
