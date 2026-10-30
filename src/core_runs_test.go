package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fakeRunAt(t *testing.T, core *Core, status, revise string) string {
	t.Helper()
	id, err := core.store.claim()
	if err != nil {
		t.Fatal(err)
	}
	step := &StepRun{ID: "check", Title: "Check", Kind: "user", Status: "done", Revise: revise}
	if status == "waiting" {
		step.Status = "waiting"
	}
	run := &Run{ID: id, Chain: "ask", Status: status, PID: os.Getpid(), StartedAt: time.Now(), Steps: []*StepRun{step}}
	core.store.update(run, func() {})
	return id
}

func TestDecideRefuses(t *testing.T) {
	core := openCore(t.TempDir())
	waiting := fakeRunAt(t, core, "waiting", "Say")
	noRevise := fakeRunAt(t, core, "waiting", "")
	done := fakeRunAt(t, core, "done", "Say")
	tests := []struct {
		id   string
		d    decision
		want string
	}{
		{waiting, decision{Choice: "maybe"}, "choice must be approve, revise or reject"},
		{"0101-000000", decision{Choice: "approve"}, "no run 0101-000000"},
		{done, decision{Choice: "approve"}, "the run is not waiting for you"},
		{noRevise, decision{Choice: "revise", Note: "smaller"}, `"Check" cannot send the run back`},
		{waiting, decision{Choice: "revise", Note: " "}, "say what to change in the note"},
	}
	for _, tt := range tests {
		if err := core.Decide(tt.id, tt.d); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("Decide(%+v) = %v, want %q", tt.d, err, tt.want)
		}
	}
	if err := core.Decide(waiting, decision{Choice: "reject", Note: "no"}); err != nil {
		t.Fatal(err)
	}
	var got decision
	if err := readJSON(filepath.Join(core.store.runDir(waiting), decisionFile), &got); err != nil || !reflect.DeepEqual(got, decision{Choice: "reject", Note: "no"}) {
		t.Errorf("decision file = %+v, %v", got, err)
	}
	if err := core.Decide(waiting, decision{Choice: "approve"}); err == nil || !strings.Contains(err.Error(), "already being applied") {
		t.Errorf("second decision = %v", err)
	}
}

func TestStopSignalsOnlyARunInProgress(t *testing.T) {
	core := openCore(t.TempDir())
	if err := core.Stop("0101-000000"); err == nil {
		t.Error("stopped a missing run")
	}
	if err := core.Stop(fakeRunAt(t, core, "done", "")); err == nil {
		t.Error("stopped a finished run")
	}
	going := fakeRunAt(t, core, "waiting", "")
	if err := core.Stop(going); err != nil || !core.store.hasSignal(going, cancelFile) {
		t.Errorf("stop = %v, cancel file %v", err, core.store.hasSignal(going, cancelFile))
	}
}

func TestRememberNotes(t *testing.T) {
	core, project := openCore(t.TempDir()), t.TempDir()
	if n, err := core.RememberNotes(project, "about", []string{" ", "\n", "- ", " -"}); n != 0 || err != nil || core.Memory(project) != "" {
		t.Errorf("empty notes = %d, %v, memory %q", n, err, core.Memory(project))
	}
	day := time.Now().Format("2006-01-02")
	n, err := core.RememberNotes(project, "Fix the login\nand more", []string{"- keep tests fast ", "use rg"})
	if want := "## " + day + " · Fix the login\n- keep tests fast\n- use rg\n"; n != 2 || err != nil || core.Memory(project) != want {
		t.Errorf("notes = %d, %v, memory %q, want %q", n, err, core.Memory(project), want)
	}
	if err := core.SetMemory(project, "# Notes\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := core.RememberNotes(project, " ", []string{"short"}); err != nil {
		t.Fatal(err)
	}
	if want := "# Notes\n\n## " + day + "\n- short\n"; core.Memory(project) != want {
		t.Errorf("memory = %q, want %q", core.Memory(project), want)
	}
}

func TestCoreTarget(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	core, own := openCore(home), filepath.Join(project, ".swarm")
	lib := &Library{Project: project, Tools: map[string]Tool{"t": {Source: "shared"}}, Chains: map[string]*Chain{"c": {Source: "shared"}}}
	tests := []struct{ kind, name, scope, want string }{
		{"chains", "c", "", home},
		{"tools", "t", "", home},
		{"prompts", "c/p", "", home},
		{"prompts", "x/p", "", own},
		{"chains", "new", "", own},
		{"tools", "t", "project", own},
		{"chains", "new", "shared", home},
	}
	for _, tt := range tests {
		if got, err := core.target(lib, tt.kind, tt.name, tt.scope); err != nil || got != tt.want {
			t.Errorf("target(%s %s %q) = %s, %v, want %s", tt.kind, tt.name, tt.scope, got, err, tt.want)
		}
	}
	if _, err := core.target(lib, "tools", "t", "elsewhere"); err == nil {
		t.Error("accepted an unknown scope")
	}
}

func itemSetup(t *testing.T) (*Core, string, string) {
	t.Helper()
	home, project, broken := t.TempDir(), t.TempDir(), t.TempDir()
	writeFiles(t, home, map[string]string{
		"tools/shout.json":      `{"title": "Shout", "run": "echo HI"}`,
		"chains/greet.json":     `{"title": "Greet", "steps": [{"id": "a", "title": "A", "tool": "shout"}, {"id": "b", "title": "B", "agent": "think"}]}`,
		"chains/greet/think.md": "Think.",
		"chains/greet/spare.md": "Not used.",
	})
	writeFiles(t, project, map[string]string{
		".swarm/tools/shout.json":  `{"title": "Shout here", "run": "echo hi"}`,
		".swarm/tools/lonely.json": `{"title": "Lonely", "run": "echo alone"}`,
		".swarm/settings.json":     `{"agent": "claude"}`,
	})
	writeFiles(t, broken, map[string]string{".swarm/settings.json": "{"})
	return openCore(home), project, broken
}

func TestGetItemLayers(t *testing.T) {
	core, project, _ := itemSetup(t)
	if _, _, err := core.GetItem(project, "bad", false); err == nil {
		t.Error("read a bad path")
	}
	if raw, source, err := core.GetItem(project, "settings.json", false); err != nil || source != "project" || !strings.Contains(string(raw), "claude") {
		t.Errorf("settings = %s %s %v", raw, source, err)
	}
	if _, _, err := core.GetItem(project, "settings.json", true); err == nil {
		t.Error("found shared settings that do not exist")
	}
	if raw, source, err := core.GetItem(project, "tools/shout.json", true); err != nil || source != "shared" || !strings.Contains(string(raw), "HI") {
		t.Errorf("shared tool = %s %s %v", raw, source, err)
	}
}

func TestPutItemRefuses(t *testing.T) {
	core, project, broken := itemSetup(t)
	tests := []struct{ path, scope, content, want string }{
		{"bad", "", "", "is not chains"},
		{"settings.json", "", `{"agent": "nobody"}`, "refused: "},
		{"tools/x.json", "elsewhere", `{"title": "X", "run": "x"}`, "scope is project or shared"},
		{"tools/x.json", "", `{"nope": 1}`, "not a valid tool"},
		{"tools/x.json", "", `{"title": " ", "run": ""}`, "a tool needs a title; a tool needs a run command"},
		{"chains/greet/think.md", "", "  ", "a prompt cannot be empty"},
		{"chains/greet/think.md", "", "Do.\n\n## Return\n\n- `Bad Name`: x\n", "## Return: cannot read"},
	}
	for _, tt := range tests {
		if err := core.PutItem(project, tt.path, tt.scope, []byte(tt.content)); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("PutItem(%s, %q) = %v, want %q", tt.path, tt.content, err, tt.want)
		}
	}
	if err := core.PutItem(broken, "tools/x.json", "", []byte(`{"title": "X", "run": "x"}`)); err == nil {
		t.Error("saved into a project with broken settings")
	}
}

func TestPutItemSavesWhereTheItemLives(t *testing.T) {
	core, project, _ := itemSetup(t)
	if err := core.PutItem(project, "tools/fresh.json", "", []byte(`{"title": "Fresh", "run": "echo fresh"}`)); err != nil {
		t.Fatal(err)
	}
	if err := core.PutItem(project, "chains/greet/think.md", "", []byte("Think harder.")); err != nil {
		t.Fatal(err)
	}
	if !exists(project, "tools/fresh.json") {
		t.Error("the new tool is not in the project")
	}
	if raw, err := os.ReadFile(filepath.Join(core.home, "chains/greet/think.md")); err != nil || string(raw) != "Think harder." {
		t.Errorf("shared prompt = %q, %v", raw, err)
	}
}

func TestDeleteItem(t *testing.T) {
	core, project, broken := itemSetup(t)
	refusals := map[string]string{
		"bad":                     "is not chains",
		"settings.json":           "settings can't be deleted",
		"tools/nothing.json":      "nothing at tools/nothing.json",
		"chains/greet/nothing.md": "nothing at",
		"chains/nochain/x.md":     "nothing at",
		"chains/greet/think.md":   "is still used by greet.b",
	}
	for path, want := range refusals {
		if err := core.DeleteItem(project, path); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("DeleteItem(%s) = %v, want %q", path, err, want)
		}
	}
	if err := core.DeleteItem(broken, "tools/x.json"); err == nil {
		t.Error("deleted from a project with broken settings")
	}
	if err := core.DeleteItem(project, "tools/shout.json"); err != nil || exists(project, "tools/shout.json") {
		t.Fatalf("deleting the project copy of a shared tool = %v", err)
	}
	var refused *RefusedError
	if err := core.DeleteItem(project, "tools/shout.json"); !errors.As(err, &refused) || !strings.Contains(err.Error(), "still used by greet.a") {
		t.Errorf("deleting a tool in use = %v", err)
	}
	for _, path := range []string{"chains/greet/spare.md", "tools/lonely.json"} {
		if err := core.DeleteItem(project, path); err != nil {
			t.Errorf("DeleteItem(%s) = %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(core.home, "chains/greet/spare.md")); !os.IsNotExist(err) || exists(project, "tools/lonely.json") {
		t.Error("the unused items are still there")
	}
}

func TestRemoveItemReportsWhatItCannotRemove(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"tools/x.json/inside": "a folder where the tool should be"})
	if err := removeItem(dir, "tools", "x"); err == nil {
		t.Error("removed a folder with files in it")
	}
}

func TestChainsUsingAndCheck(t *testing.T) {
	core, project, broken := itemSetup(t)
	problems, lib, err := core.Check(project)
	if err != nil || len(problems) != 0 {
		t.Fatalf("check = %v, %v", problems, err)
	}
	tests := map[string][]string{
		"settings.json":         nil,
		"bad":                   nil,
		"tools/shout.json":      {"greet"},
		"tools/lonely.json":     {},
		"chains/greet.json":     {"greet"},
		"chains/greet/think.md": {"greet"},
	}
	for path, want := range tests {
		if got := lib.chainsUsing(path); !reflect.DeepEqual(got, want) {
			t.Errorf("chainsUsing(%s) = %#v, want %#v", path, got, want)
		}
	}
	if _, _, err := core.Check(broken); err == nil {
		t.Error("checked a project with broken settings")
	}
}
