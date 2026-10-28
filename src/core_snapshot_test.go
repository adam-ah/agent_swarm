package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := writeFile(path, []byte(text)); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestSnapshotDiffRestore(t *testing.T) {
	home, project, saved := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "before")
	c := &Core{home: home}
	write(t, filepath.Join(home, "tools", "lint.json"), "{\n  \"title\": \"Lint\",\n  \"run\": \"eslint .\"\n}\n")
	write(t, filepath.Join(project, ".swarm", "memory.md"), "- tests run with make test\n")

	if count, err := c.Snapshot(project, saved); err != nil || count != 1 {
		t.Fatalf("Snapshot() = %d, %v, want 1 file", count, err)
	}
	if _, err := c.Snapshot(project, saved); err == nil {
		t.Error("Snapshot() into an existing folder succeeded, want a refusal")
	}

	write(t, filepath.Join(project, ".swarm", "memory.md"), "- tests run with ./test.sh\n")
	write(t, filepath.Join(project, ".swarm", "tools", "lint.json"), "{\n  \"title\": \"Lint\",\n  \"run\": \"eslint src\"\n}\n")
	text, files, err := c.Diff(project, saved)
	if err != nil || files != 2 {
		t.Fatalf("Diff() files = %d, %v, want 2\n%s", files, err, text)
	}
	for _, want := range []string{"-- tests run with make test", "+- tests run with ./test.sh", `-  "run": "eslint ."`, `+  "run": "eslint src"`} {
		if !strings.Contains(text, want) {
			t.Errorf("Diff() lacks %q\n%s", want, text)
		}
	}
	if strings.Contains(text, `+  "title": "Lint"`) {
		t.Errorf("Diff() shows the whole new project copy instead of what changed from the shared tool\n%s", text)
	}

	if err := c.Restore(project, saved); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(project, ".swarm", "memory.md")); got != "- tests run with make test\n" {
		t.Errorf("memory after Restore() = %q", got)
	}
	if _, err := os.Stat(filepath.Join(project, ".swarm", "tools", "lint.json")); err == nil {
		t.Error("Restore() kept the project copy made after the snapshot")
	}
}

func TestRestoreWithoutSnapshotKeepsSwarm(t *testing.T) {
	project := t.TempDir()
	c := &Core{home: t.TempDir()}
	write(t, filepath.Join(project, ".swarm", "memory.md"), "keep me\n")
	if err := c.Restore(project, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("Restore() from a missing folder succeeded, want an error")
	}
	if got := read(t, filepath.Join(project, ".swarm", "memory.md")); got != "keep me\n" {
		t.Errorf("memory after a failed Restore() = %q", got)
	}
}
