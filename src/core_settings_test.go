package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsViewLayers(t *testing.T) {
	core, project := surfaceProject(t)
	view, err := core.Settings(project)
	if err != nil {
		t.Fatal(err)
	}
	if view.Shared != nil || view.Project != nil || view.Default != "claude" || view.Installed["claude"] == "" {
		t.Errorf("view without files = %+v", view)
	}

	surfaceFiles(t, core.home, map[string]string{"settings.json": `{"agents": {"pi": {"command": "pi -p"}}}`})
	surfaceFiles(t, project, map[string]string{".swarm/settings.json": `{"agent": "pi", "inputs": {"branch": "main"}}`})
	view, err = core.Settings(project)
	if err != nil {
		t.Fatal(err)
	}
	if view.Shared == nil || view.Shared.Agents["pi"].Command != "pi -p" || view.Project == nil || view.Project.Agent != "pi" {
		t.Errorf("layers = %+v, %+v", view.Shared, view.Project)
	}
	if view.Default != "pi" || strings.Join(view.Runners, ",") != "claude,codex,pi" || view.Effective.Inputs["branch"] != "main" {
		t.Errorf("view = %+v", view)
	}

	surfaceFiles(t, project, map[string]string{".swarm/settings.json": "{"})
	if _, err := core.Settings(project); err == nil {
		t.Error("broken settings gave no error")
	}
}

func TestPutSettingsChoosesTheLayer(t *testing.T) {
	core, project := surfaceProject(t)
	shared, own := settingsPath(core.home), settingsPath(filepath.Join(project, ".swarm"))
	if err := core.PutItem(project, "settings.json", "", []byte(`{"agent": "codex"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(shared); err != nil {
		t.Errorf("without a project file the shared one is written: %v", err)
	}
	if err := core.PutItem(project, "settings", "project", []byte(`{"inputs": {"branch": "main"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := core.PutItem(project, "settings.json", "", []byte(`{"agent": "claude"}`)); err != nil {
		t.Fatal(err)
	}
	var layer Settings
	if err := readJSON(own, &layer); err != nil || layer.Agent != "claude" || layer.Inputs != nil {
		t.Errorf("project layer = %+v, %v", layer, err)
	}
	lib, err := core.Library(project)
	if err != nil || lib.Settings.Agent != "claude" {
		t.Errorf("merged = %+v, %v", lib, err)
	}
}

func TestPutSettingsRefusals(t *testing.T) {
	core, project := surfaceProject(t)
	surfaceFiles(t, core.home, map[string]string{"settings.json": `{"agents": {"pi": {"command": "pi -p"}}}`})
	surfaceFiles(t, project, map[string]string{
		".swarm/chains/uses-pi.json":  `{"title": "Uses pi", "steps": [{"id": "a", "title": "A", "agent": "do", "runner": "pi"}]}`,
		".swarm/chains/uses-pi/do.md": "Do it.",
	})
	tests := []struct {
		name, scope, content, want string
	}{
		{"not json", "", "{", "not valid settings"},
		{"unknown field", "", `{"agnt": "x"}`, "not valid settings"},
		{"unknown scope", "elsewhere", `{}`, `scope is project or shared, not "elsewhere"`},
		{"unknown default agent", "project", `{"agent": "ghost"}`, `the default agent "ghost"`},
		{"breaks a chain", "shared", `{}`, `chain uses-pi would break: step "a": no agent named "pi"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := core.PutItem(project, "settings.json", tt.scope, []byte(tt.content))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want %q", err, tt.want)
			}
			var refusal *RefusedError
			if tt.scope != "elsewhere" && !errors.As(err, &refusal) {
				t.Errorf("%v is not a refusal", err)
			}
		})
	}
	if raw, err := os.ReadFile(settingsPath(core.home)); err != nil || !strings.Contains(string(raw), "pi -p") {
		t.Errorf("shared settings changed: %s, %v", raw, err)
	}
}
