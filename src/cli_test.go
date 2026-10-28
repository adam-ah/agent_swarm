package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func surfaceContains(t *testing.T, out string, err error, want ...string) {
	t.Helper()
	if err != nil {
		t.Fatalf("error %v, output %s", err, out)
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
}

func surfaceFails(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want one containing %q", err, want)
	}
}

func TestCLIInit(t *testing.T) {
	core, _ := surfaceProject(t)
	fresh := t.TempDir()
	out, err := surfaceCLI(t, "init", fresh)
	surfaceContains(t, out, err, fresh+" is a swarm project", "swarm samples", "CLAUDE.md")
	if _, statErr := os.Stat(memoryPath(fresh)); statErr != nil {
		t.Error(statErr)
	}

	surfaceFiles(t, core.home, map[string]string{"GUIDE.txt": "guide"})
	second := t.TempDir()
	surfaceFiles(t, second, map[string]string{".swarm/memory.md": "kept\n"})
	t.Setenv("SWARM_PROJECT", second)
	out, err = surfaceCLI(t, "init")
	surfaceContains(t, out, err, second+" is a swarm project")
	if strings.Contains(out, "swarm samples") || core.Memory(second) != "kept\n" {
		t.Errorf("init with samples and memory: %s, memory %q", out, core.Memory(second))
	}

	_, err = surfaceCLI(t, "init", filepath.Join(fresh, "missing"))
	surfaceFails(t, err, "is not a folder")
	blocked := t.TempDir()
	surfaceFiles(t, memoryPath(blocked), map[string]string{"x": "x"})
	_, err = surfaceCLI(t, "init", blocked)
	surfaceFails(t, err, "memory.md")
}

func TestCLIParsing(t *testing.T) {
	_, project := surfaceProject(t)
	_, err := surfaceCLI(t, "run", "-input", "no-equals")
	surfaceFails(t, err, "write inputs as name=value")
	_, err = surfaceCLI(t, "get", "-weird", "chains/hello.json")
	surfaceFails(t, err, `"-weird" is not`)
	_, err = surfaceCLI(t, "list", "-project", t.TempDir())
	surfaceFails(t, err, "is not a swarm project yet")
	_, err = surfaceCLI(t, "bogus", "--", "x")
	surfaceFails(t, err, `no command "bogus"`)
	c, err := parseCommand("run", []string{"hello", "-input", "a=1", "-detach", "-project=" + project})
	if err != nil || c.positional[0] != "hello" || c.inputs["a"] != "1" || !c.detach || c.folder != project || c.inputs.String() != "" {
		t.Errorf("parsed %+v, %v", c, err)
	}
}

func TestCLIListAndCheck(t *testing.T) {
	_, project := surfaceProject(t)
	out, err := surfaceCLI(t, "list")
	surfaceContains(t, out, err, project+": shared from", "hello")
	out, err = surfaceCLI(t, "check")
	surfaceContains(t, out, err, "ok: 3 chains, 2 tools, 0 prompts")

	surfaceFiles(t, project, map[string]string{
		".swarm/tools/shout.json":  `{"title": "Shout", "run": "echo {{message}}"}`,
		".swarm/chains/shout.json": `{"title": "Shout", "steps": [{"id": "shout", "title": "Shout", "tool": "shout"}]}`,
	})
	out, err = surfaceCLI(t, "check")
	surfaceContains(t, out, err, "warnings:", "which nothing fills")

	surfaceFiles(t, project, map[string]string{".swarm/chains/lost.json": `{"title": "Lost", "steps": [{"id": "a", "title": "A", "tool": "ghost"}]}`})
	_, err = surfaceCLI(t, "check")
	surfaceFails(t, err, "lost (project)")

	surfaceFiles(t, project, map[string]string{".swarm/settings.json": "{"})
	for _, command := range []string{"list", "check", "settings"} {
		_, err = surfaceCLI(t, command)
		surfaceFails(t, err, "settings.json")
	}
}

func TestCLIGetPutRemove(t *testing.T) {
	_, project := surfaceProject(t)
	_, err := surfaceCLI(t, "get")
	surfaceFails(t, err, "swarm get needs a path")
	out, err := surfaceCLI(t, "get", "chains/hello.json")
	surfaceContains(t, out, err, `"title": "Hello"`)
	_, err = surfaceCLI(t, "get", "chains/hello.json", "-below")
	surfaceFails(t, err, "not in the shared library")

	_, err = surfaceCLI(t, "put")
	surfaceFails(t, err, "swarm put needs a path")
	file := filepath.Join(t.TempDir(), "extra.json")
	surfaceFiles(t, filepath.Dir(file), map[string]string{"extra.json": `{"title": "Extra", "run": "echo {{message}}"}`})
	out, err = surfaceCLI(t, "put", "tools/extra.json", "-file", file)
	surfaceContains(t, out, err, "saved tools/extra.json")
	_, err = surfaceCLI(t, "put", "tools/extra.json", "-file", file+".missing")
	surfaceFails(t, err, "no such file")
	surfaceStdin(t, `{"title": "Uses extra", "steps": [{"id": "x", "title": "X", "tool": "extra"}]}`)
	out, err = surfaceCLI(t, "put", "chains/extra.json")
	surfaceContains(t, out, err, "saved chains/extra.json", "warnings:", "which nothing fills")
	if _, statErr := os.Stat(filepath.Join(project, ".swarm", "chains", "extra.json")); statErr != nil {
		t.Error(statErr)
	}
	surfaceStdin(t, `{"title": ""}`)
	_, err = surfaceCLI(t, "put", "tools/empty.json")
	surfaceFails(t, err, "refused:\n- a tool needs a title\n- a tool needs a run command")

	_, err = surfaceCLI(t, "rm")
	surfaceFails(t, err, "swarm rm needs a path")
	_, err = surfaceCLI(t, "rm", "tools/echo.json")
	surfaceFails(t, err, "refused:\n- tools/echo.json is still used by")
	_, err = surfaceCLI(t, "rm", "nonsense")
	surfaceFails(t, err, `"nonsense" is not`)
	out, err = surfaceCLI(t, "rm", "chains/extra.json")
	surfaceContains(t, out, err, "deleted chains/extra.json")
}

func TestCLIMemory(t *testing.T) {
	core, project := surfaceProject(t)
	out, err := surfaceCLI(t, "memory")
	surfaceContains(t, out, err)
	out, err = surfaceCLI(t, "memory", "add", "-about", "the tests", "first note", "second note")
	surfaceContains(t, out, err, "remembered 2 note(s)")
	out, err = surfaceCLI(t, "memory")
	surfaceContains(t, out, err, "· the tests\n- first note\n- second note")

	file := filepath.Join(t.TempDir(), "memory.md")
	surfaceFiles(t, filepath.Dir(file), map[string]string{"memory.md": "# From a file\n"})
	out, err = surfaceCLI(t, "memory", "set", "-file", file)
	surfaceContains(t, out, err, "memory replaced")
	surfaceStdin(t, "# From stdin\n")
	out, err = surfaceCLI(t, "memory", "set")
	surfaceContains(t, out, err, "memory replaced")
	if core.Memory(project) != "# From stdin\n" {
		t.Errorf("memory = %q", core.Memory(project))
	}
	_, err = surfaceCLI(t, "memory", "set", "-file", file+".missing")
	surfaceFails(t, err, "no such file")
	_, err = surfaceCLI(t, "memory", "forget")
	surfaceFails(t, err, `takes add or set, not "forget"`)

	if err = os.Remove(memoryPath(project)); err != nil {
		t.Fatal(err)
	}
	surfaceFiles(t, memoryPath(project), map[string]string{"x": "x"})
	_, err = surfaceCLI(t, "memory", "add", "note")
	surfaceFails(t, err, "memory.md")
	_, err = surfaceCLI(t, "memory", "set", "-file", file)
	surfaceFails(t, err, "memory.md")
}

func TestCLISettings(t *testing.T) {
	core, project := surfaceProject(t)
	out, err := surfaceCLI(t, "settings")
	surfaceContains(t, out, err, "agent steps run with claude", "agents: claude, codex", "  claude: ", "  codex: not on PATH")
	if strings.Contains(out, "shared, ") {
		t.Errorf("no settings files, but: %s", out)
	}
	surfaceFiles(t, core.home, map[string]string{"settings.json": `{"agents": {"pi": {"command": "pi -p"}}}`})
	surfaceFiles(t, project, map[string]string{".swarm/settings.json": `{"agent": "pi"}`})
	out, err = surfaceCLI(t, "settings")
	surfaceContains(t, out, err, "agent steps run with pi", "agents: claude, codex, pi", "shared, "+core.home+"/settings.json:", `"command": "pi -p"`, "this project, .swarm/settings.json:")
}

func TestCLISnapshotDiffRestore(t *testing.T) {
	_, project := surfaceProject(t)
	saved := filepath.Join(t.TempDir(), "saved")
	_, err := surfaceCLI(t, "snapshot")
	surfaceFails(t, err, "needs a folder")
	out, err := surfaceCLI(t, "snapshot", saved)
	surfaceContains(t, out, err, "saved 5 file(s) from "+project+"/.swarm")
	_, err = surfaceCLI(t, "snapshot", saved)
	surfaceFails(t, err, "already exists")

	surfaceFiles(t, project, map[string]string{".swarm/tools/echo.json": `{"title": "Echo", "run": "echo changed"}`})
	out, err = surfaceCLI(t, "diff", saved)
	surfaceContains(t, out, err, "echo changed")
	written := filepath.Join(t.TempDir(), "out.diff")
	out, err = surfaceCLI(t, "diff", saved, written)
	surfaceContains(t, out, err, "1 file(s) changed")
	if raw, readErr := os.ReadFile(written); readErr != nil || !strings.Contains(string(raw), "echo changed") {
		t.Errorf("diff file = %s, %v", raw, readErr)
	}
	_, err = surfaceCLI(t, "diff", saved, filepath.Join(written, "below"))
	surfaceFails(t, err, "out.diff")
	_, err = surfaceCLI(t, "diff", saved+".missing")
	surfaceFails(t, err, "nothing was saved")

	out, err = surfaceCLI(t, "restore", saved)
	surfaceContains(t, out, err, "put "+project+"/.swarm back as it was")
	_, err = surfaceCLI(t, "restore", saved+".missing")
	surfaceFails(t, err, "nothing was saved")
}
