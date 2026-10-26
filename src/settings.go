package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

var builtinRunners = []string{"claude", "codex"}

type AgentCLI struct {
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
}

type Settings struct {
	Agent  string              `json:"agent,omitempty"`
	Agents map[string]AgentCLI `json:"agents,omitempty"`
}

func settingsPath(dir string) string {
	return filepath.Join(dir, "settings.json")
}

func loadSettings(home, project string) (Settings, error) {
	merged := Settings{Agents: map[string]AgentCLI{}}
	for _, dir := range []string{home, filepath.Join(project, ".swarm")} {
		var layer Settings
		if err := readJSON(settingsPath(dir), &layer); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return merged, err
		}
		if layer.Agent != "" {
			merged.Agent = layer.Agent
		}
		for name, cli := range layer.Agents {
			merged.Agents[name] = cli
		}
	}
	return merged, nil
}

func isBuiltinRunner(name string) bool {
	for _, b := range builtinRunners {
		if b == name {
			return true
		}
	}
	return false
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
	return isBuiltinRunner(name) || s.Agents[name].Command != ""
}

func (s Settings) runners() []string {
	names := append([]string{}, builtinRunners...)
	var custom []string
	for name, cli := range s.Agents {
		if !isBuiltinRunner(name) && cli.Command != "" {
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
		if isBuiltinRunner(name) && cli.Command != "" {
			problems = append(problems, fmt.Sprintf("agent %q is built in; give it args, not a command", name))
		}
		if !isBuiltinRunner(name) && cli.Command == "" {
			problems = append(problems, fmt.Sprintf("agent %q needs a command", name))
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
