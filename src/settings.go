package main

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
)

var builtinRunners = []string{"claude", "codex"}

type AgentCLI struct {
	Command string   `json:"command,omitempty"`
	Runs    string   `json:"runs,omitempty"`
	Args    []string `json:"args,omitempty"`
}

func (cli AgentCLI) defined() bool {
	return cli.Command != "" || cli.Runs != ""
}

type Settings struct {
	Agent  string              `json:"agent,omitempty"`
	Agents map[string]AgentCLI `json:"agents,omitempty"`
	Inputs map[string]string   `json:"inputs,omitempty"`
}

func settingsPath(dir string) string {
	return filepath.Join(dir, "settings.json")
}

func newSettings() Settings {
	return Settings{Agents: map[string]AgentCLI{}, Inputs: map[string]string{}}
}

func addLayer(merged *Settings, layer Settings) {
	if layer.Agent != "" {
		merged.Agent = layer.Agent
	}
	maps.Copy(merged.Agents, layer.Agents)
	maps.Copy(merged.Inputs, layer.Inputs)
}

func loadSettings(home, project string) (Settings, error) {
	merged := newSettings()
	for _, dir := range []string{home, filepath.Join(project, ".swarm")} {
		var layer Settings
		if err := readJSON(settingsPath(dir), &layer); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return merged, err
		}
		addLayer(&merged, layer)
	}
	return merged, nil
}

func (s Settings) inputValue(in Input) string {
	if v := s.Inputs[in.Name]; v != "" {
		return v
	}
	return in.Default
}

func isBuiltinRunner(name string) bool {
	return slices.Contains(builtinRunners, name)
}

func (s Settings) defaultRunner() string {
	if s.Agent != "" {
		return s.Agent
	}
	return "claude"
}

func (s Settings) runnerFor(step Step) string {
	if step.Runner != "" {
		return step.Runner
	}
	return s.defaultRunner()
}

func (s Settings) knows(name string) bool {
	return isBuiltinRunner(name) || s.Agents[name].defined()
}

func (s Settings) builtinFor(runner string) string {
	if isBuiltinRunner(runner) {
		return runner
	}
	return s.Agents[runner].Runs
}

func (s Settings) canResume(runner string) bool {
	return s.builtinFor(runner) != ""
}

func (s Settings) runners() []string {
	names := append([]string{}, builtinRunners...)
	var custom []string
	for name, cli := range s.Agents {
		if !isBuiltinRunner(name) && cli.defined() {
			custom = append(custom, name)
		}
	}
	sort.Strings(custom)
	return append(names, custom...)
}

func (s Settings) problems() []string {
	var problems []string
	for name, cli := range s.Agents {
		if !fileName.MatchString(name) {
			problems = append(problems, fmt.Sprintf("agent %q: names are lowercase letters, digits, - and _", name))
		}
		switch {
		case isBuiltinRunner(name) && cli.Command != "":
			problems = append(problems, fmt.Sprintf("agent %q is built in; give it args, not a command", name))
		case isBuiltinRunner(name) && cli.Runs != "":
			problems = append(problems, fmt.Sprintf("agent %q is built in; give it args, not runs", name))
		case !isBuiltinRunner(name) && !cli.defined():
			problems = append(problems, fmt.Sprintf("agent %q needs a command, or runs set to claude or codex", name))
		case cli.Command != "" && cli.Runs != "":
			problems = append(problems, fmt.Sprintf("agent %q has both a command and runs; keep one", name))
		case cli.Runs != "" && !isBuiltinRunner(cli.Runs):
			problems = append(problems, fmt.Sprintf("agent %q: runs must be claude or codex, not %q", name, cli.Runs))
		}
	}
	for _, name := range sortedKeys(s.Inputs) {
		if !stepID.MatchString(name) {
			problems = append(problems, fmt.Sprintf("input %q: names are lowercase letters, digits and underscores", name))
		}
	}
	if s.Agent != "" && !s.knows(s.Agent) {
		problems = append(problems, fmt.Sprintf("the default agent %q is neither claude, codex nor one defined under agents", s.Agent))
	}
	return problems
}

func installedRunners() map[string]string {
	found := map[string]string{}
	for _, name := range builtinRunners {
		if path, err := exec.LookPath(name); err == nil {
			found[name] = path
		}
	}
	return found
}
