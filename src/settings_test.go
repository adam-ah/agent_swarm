package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeSettingsFile(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSettingsMergesProjectOverShared(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	writeSettingsFile(t, home, `{"agent":"codex","agents":{"pi":{"command":"pi -p"},"gem":{"command":"gem"}}}`)
	writeSettingsFile(t, filepath.Join(project, ".swarm"), `{"agents":{"pi":{"command":"pi --fast"},"claude":{"args":["--model","opus"]}}}`)

	got, err := loadSettings(home, project)
	if err != nil {
		t.Fatalf("loadSettings() error = %v", err)
	}
	want := Settings{
		Agent: "codex",
		Agents: map[string]AgentCLI{
			"pi":     {Command: "pi --fast"},
			"gem":    {Command: "gem"},
			"claude": {Args: []string{"--model", "opus"}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadSettings() = %+v, want %+v", got, want)
	}
}

func TestLoadSettingsProjectAgentOverridesShared(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	writeSettingsFile(t, home, `{"agent":"codex"}`)
	writeSettingsFile(t, filepath.Join(project, ".swarm"), `{"agent":"claude"}`)

	got, err := loadSettings(home, project)
	if err != nil {
		t.Fatalf("loadSettings() error = %v", err)
	}
	if got.Agent != "claude" {
		t.Errorf("loadSettings().Agent = %q, want project value claude", got.Agent)
	}
}

func TestLoadSettingsWithoutFiles(t *testing.T) {
	got, err := loadSettings(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("loadSettings() error = %v", err)
	}
	if got.Agent != "" || got.Agents == nil || len(got.Agents) != 0 {
		t.Errorf("loadSettings() = %+v, want empty settings with a non-nil agents map", got)
	}
}

func TestLoadSettingsReportsBadJSON(t *testing.T) {
	home := t.TempDir()
	writeSettingsFile(t, home, `{"agent":`)
	if _, err := loadSettings(home, t.TempDir()); err == nil {
		t.Error("loadSettings() with broken JSON returned no error")
	}
}

func TestIsBuiltinRunner(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "claude", want: true},
		{name: "codex", want: true},
		{name: "pi", want: false},
		{name: "", want: false},
		{name: "Claude", want: false},
	}
	for _, tt := range tests {
		if got := isBuiltinRunner(tt.name); got != tt.want {
			t.Errorf("isBuiltinRunner(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestSettingsDefaultAndRunnerFor(t *testing.T) {
	tests := []struct {
		name        string
		settings    Settings
		step        Step
		wantDefault string
		wantRunner  string
	}{
		{name: "nothing set", settings: Settings{}, step: Step{}, wantDefault: "claude", wantRunner: "claude"},
		{name: "default set", settings: Settings{Agent: "codex"}, step: Step{}, wantDefault: "codex", wantRunner: "codex"},
		{name: "step overrides default", settings: Settings{Agent: "codex"}, step: Step{Runner: "claude"}, wantDefault: "codex", wantRunner: "claude"},
		{name: "custom step runner", settings: Settings{}, step: Step{Runner: "pi"}, wantDefault: "claude", wantRunner: "pi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.settings.defaultRunner(); got != tt.wantDefault {
				t.Errorf("defaultRunner() = %q, want %q", got, tt.wantDefault)
			}
			if got := tt.settings.runnerFor(tt.step); got != tt.wantRunner {
				t.Errorf("runnerFor() = %q, want %q", got, tt.wantRunner)
			}
		})
	}
}

func TestSettingsKnows(t *testing.T) {
	s := Settings{Agents: map[string]AgentCLI{
		"pi":     {Command: "pi -p"},
		"claude": {Args: []string{"--model", "opus"}},
		"argsel": {Args: []string{"-x"}},
	}}
	tests := []struct {
		name string
		want bool
	}{
		{name: "claude", want: true},
		{name: "codex", want: true},
		{name: "pi", want: true},
		{name: "argsel", want: false},
		{name: "ghost", want: false},
	}
	for _, tt := range tests {
		if got := s.knows(tt.name); got != tt.want {
			t.Errorf("knows(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestSettingsRunners(t *testing.T) {
	s := Settings{Agents: map[string]AgentCLI{
		"zeta":   {Command: "zeta"},
		"alpha":  {Command: "alpha"},
		"claude": {Args: []string{"--model", "opus"}},
		"noargs": {Args: []string{"-x"}},
	}}
	want := []string{"claude", "codex", "alpha", "zeta"}
	if got := s.runners(); !reflect.DeepEqual(got, want) {
		t.Errorf("runners() = %v, want %v", got, want)
	}
	if got := (Settings{}).runners(); !reflect.DeepEqual(got, []string{"claude", "codex"}) {
		t.Errorf("runners() with no custom agents = %v", got)
	}
}

func TestSettingsProblems(t *testing.T) {
	tests := []struct {
		name     string
		settings Settings
		want     []string
	}{
		{name: "empty is valid", settings: Settings{}},
		{name: "valid custom agent", settings: Settings{Agent: "pi", Agents: map[string]AgentCLI{"pi": {Command: "pi -p"}}}},
		{name: "bad name", settings: Settings{Agents: map[string]AgentCLI{"Pi": {Command: "pi"}}}, want: []string{`agent "Pi": names are lowercase letters, digits, - and _`}},
		{name: "builtin with command", settings: Settings{Agents: map[string]AgentCLI{"claude": {Command: "claude"}}}, want: []string{`agent "claude" is built in; give it args, not a command`}},
		{name: "custom without command", settings: Settings{Agents: map[string]AgentCLI{"pi": {Args: []string{"-p"}}}}, want: []string{`agent "pi" needs a command`}},
		{name: "unknown default", settings: Settings{Agent: "pi"}, want: []string{`the default agent "pi" is neither claude, codex nor one defined under agents`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.settings.problems()
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("problems() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInstalledRunners(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	got := installedRunners()
	if got["codex"] != filepath.Join(bin, "codex") {
		t.Errorf("installedRunners()[codex] = %q", got["codex"])
	}
	if _, ok := got["claude"]; ok {
		t.Errorf("installedRunners() reports claude, which is not on PATH")
	}
}
