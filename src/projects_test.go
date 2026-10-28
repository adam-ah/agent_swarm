package main

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestProjectsSavedAndSession(t *testing.T) {
	core, saved := surfaceProject(t)
	if projects, fallback := core.Projects(); !slices.Equal(projects, []string{saved}) || fallback != saved {
		t.Errorf("projects = %v, default %s", projects, fallback)
	}
	if again, err := core.AddProject(saved); err != nil || again != saved {
		t.Errorf("adding twice = %s, %v", again, err)
	}

	session := t.TempDir()
	for range 2 {
		if err := core.openSession(session); err != nil {
			t.Fatal(err)
		}
	}
	projects, fallback := core.Projects()
	if !slices.Equal(projects, []string{saved, session}) || fallback != session {
		t.Errorf("with a session: %v, default %s", projects, fallback)
	}
	if got, err := core.Project(""); err != nil || got != session {
		t.Errorf("Project(\"\") = %s, %v", got, err)
	}
	if got, err := core.Project(saved); err != nil || got != saved {
		t.Errorf("Project(saved) = %s, %v", got, err)
	}
	if fresh := openCore(core.home); !slices.Equal(fresh.saved(), []string{saved}) {
		t.Errorf("the session project was saved: %v", fresh.saved())
	}
}

func TestProjectErrors(t *testing.T) {
	core, project := surfaceProject(t)
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := core.Project(missing); err == nil {
		t.Error("an unknown project was accepted")
	}
	if err := core.openSession(missing); err == nil || core.fallback != "" {
		t.Errorf("openSession(missing) = %v, fallback %q", err, core.fallback)
	}
	file := filepath.Join(project, ".swarm", "tools", "echo.json")
	if _, err := core.AddProject(file); err == nil {
		t.Error("a file was added as a project")
	}
	empty := openCore(t.TempDir())
	if projects, fallback := empty.Projects(); len(projects) != 0 || fallback != "" {
		t.Errorf("no projects = %v, default %q", projects, fallback)
	}
	if _, err := empty.Project(""); err == nil {
		t.Error("no project was accepted")
	}
}
