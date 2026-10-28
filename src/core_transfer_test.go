package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for path, text := range files {
		if err := writeFile(filepath.Join(dir, path), []byte(text)); err != nil {
			t.Fatal(err)
		}
	}
}

func transferSetup(t *testing.T) (*Core, string, string) {
	t.Helper()
	home, from, to := t.TempDir(), t.TempDir(), t.TempDir()
	writeFiles(t, home, map[string]string{
		"chains/shared.json":       `{"title": "Shared", "steps": [{"id": "s", "title": "S", "agent": "s"}]}`,
		"chains/shared/s.md":       "Shared chain's prompt.",
		"tools/shared_tool.json":   `{"title": "Shared tool", "run": "echo s"}`,
		"chains/unrelated.json":    `{"title": "Unrelated", "steps": [{"id": "x", "title": "X", "tool": "shared_tool"}]}`,
		"chains/unrelated/x.md":    "Not used.",
		"chains/unrelated/more.md": "Not used either.",
	})
	writeFiles(t, from, map[string]string{
		".swarm/chains/main.json":  `{"title": "Main", "steps": [{"id": "a", "title": "A", "tool": "t"}, {"id": "b", "title": "B", "agent": "p"}, {"id": "c", "title": "C", "tool": "shared_tool"}, {"id": "d", "title": "D", "chain": "sub"}]}`,
		".swarm/chains/main/p.md":  "Project prompt.",
		".swarm/chains/sub.json":   `{"title": "Sub", "steps": [{"id": "u", "title": "U", "tool": "u"}, {"id": "v", "title": "V", "agent": "v"}]}`,
		".swarm/chains/sub/v.md":   "Sub prompt.",
		".swarm/chains/other.json": `{"title": "Other", "steps": [{"id": "u", "title": "U", "tool": "u"}]}`,
		".swarm/tools/t.json":      `{"title": "T", "run": "echo t"}`,
		".swarm/tools/u.json":      `{"title": "U", "run": "echo u"}`,
	})
	core := openCore(home)
	for _, dir := range []string{from, to} {
		if _, err := core.AddProject(dir); err != nil {
			t.Fatal(err)
		}
	}
	return core, from, to
}

func exists(dir, path string) bool {
	_, err := os.Stat(filepath.Join(dir, ".swarm", path))
	return err == nil
}

func TestTransferChainCopiesWhatItUses(t *testing.T) {
	core, from, to := transferSetup(t)
	done, err := core.TransferChain(from, "main", to, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tools/t.json", "chains/main/p.md", "tools/u.json", "chains/sub/v.md", "chains/sub.json", "chains/main.json"}
	if !reflect.DeepEqual(done.Copied, want) {
		t.Errorf("copied %v, want %v", done.Copied, want)
	}
	if exists(to, "tools/shared_tool.json") || !exists(from, "chains/main.json") {
		t.Error("copied the shared tool, or removed the source")
	}
	lib, _ := core.Library(to)
	if len(lib.Chains["main"].Problems) > 0 {
		t.Errorf("copied chain has problems: %v", lib.Chains["main"].Problems)
	}

	again, err := core.TransferChain(from, "main", to, false)
	if err != nil || len(again.Copied) != 0 || !reflect.DeepEqual(again.Same, want) {
		t.Errorf("second copy = %+v, %v", again, err)
	}
}

func TestTransferChainRefusesADifferentFileThere(t *testing.T) {
	core, from, to := transferSetup(t)
	writeFiles(t, to, map[string]string{".swarm/chains/main/p.md": "Someone else's prompt."})
	_, err := core.TransferChain(from, "main", to, false)
	var refused *RefusedError
	if !errors.As(err, &refused) || len(refused.Problems) != 1 {
		t.Fatalf("err = %v", err)
	}
	if exists(to, "tools/t.json") || exists(to, "chains/main.json") {
		t.Error("wrote files although it refused")
	}
}

func TestTransferChainMoveLeavesWhatOthersUse(t *testing.T) {
	core, from, to := transferSetup(t)
	done, err := core.TransferChain(from, "main", to, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"chains/main.json", "chains/sub.json", "chains/sub/v.md", "chains/main/p.md", "tools/t.json"}; !reflect.DeepEqual(done.Removed, want) {
		t.Errorf("removed %v, want %v", done.Removed, want)
	}
	if want := []string{"tools/u.json"}; !reflect.DeepEqual(done.Kept, want) {
		t.Errorf("kept %v, want %v", done.Kept, want)
	}
	if exists(from, "chains/main.json") || exists(from, "chains/main") || exists(from, "chains/sub") || !exists(from, "tools/u.json") || !exists(to, "chains/main/p.md") {
		t.Error("files are not where the move should leave them")
	}
}

func TestTransferSharedChainTakesItsPrompts(t *testing.T) {
	core, from, to := transferSetup(t)
	done, err := core.TransferChain(from, "shared", to, false)
	if want := []string{"chains/shared/s.md", "chains/shared.json"}; err != nil || !reflect.DeepEqual(done.Copied, want) {
		t.Fatalf("copied %+v, %v, want %v", done, err, want)
	}
	lib, _ := core.Library(to)
	if chain := lib.Chains["shared"]; chain.Source != "project" || len(chain.Problems) > 0 || chain.Prompts["s"].Text != "Shared chain's prompt." {
		t.Errorf("project copy = %+v", chain)
	}
}

func TestProjectCopyTakesThePrompts(t *testing.T) {
	core, from, _ := transferSetup(t)
	raw, _, _ := core.GetItem(from, "chains/shared.json", false)
	if err := core.PutItem(from, "chains/shared.json", "project", raw); err != nil {
		t.Fatal(err)
	}
	if !exists(from, "chains/shared/s.md") {
		t.Error("the project copy has no prompt")
	}
	if err := core.DeleteItem(from, "chains/shared.json"); err != nil || exists(from, "chains/shared") {
		t.Errorf("deleting the project copy left its prompts: %v", err)
	}
}

func TestLegacyPromptsMoveIntoTheirChain(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	writeFiles(t, home, map[string]string{
		"prompts/fix.md":   "Fix it.",
		"chains/fix.json":  `{"title": "Fix", "steps": [{"id": "a", "title": "A", "agent": "fix"}]}`,
		"chains/also.json": `{"title": "Also", "steps": [{"id": "a", "title": "A", "agent": "fix"}]}`,
	})
	writeFiles(t, project, map[string]string{".swarm/chains/mine.json": `{"title": "Mine", "steps": [{"id": "a", "title": "A", "agent": "fix"}]}`})
	lib, err := loadLibrary(home, project)
	if err != nil {
		t.Fatal(err)
	}
	if problems := lib.problems(); len(problems) > 0 {
		t.Errorf("problems after adopting legacy prompts: %q", problems)
	}
	for _, path := range []string{filepath.Join(home, "chains/fix/fix.md"), filepath.Join(home, "chains/also/fix.md"), filepath.Join(project, ".swarm/chains/mine/fix.md")} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was not written", path)
		}
	}
}

func TestTransferChainMoveRefusesAChainOthersCall(t *testing.T) {
	core, from, to := transferSetup(t)
	if _, err := core.TransferChain(from, "sub", to, true); err == nil || exists(to, "chains/sub.json") {
		t.Errorf("moved a chain that main calls: %v", err)
	}
	if _, err := core.TransferChain(from, "main", from, false); err == nil {
		t.Error("copied a chain onto its own project")
	}
}
