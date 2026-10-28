package main

import (
	"fmt"
	"os"
	"path/filepath"
)

type SettingsView struct {
	Effective Settings          `json:"effective"`
	Default   string            `json:"default"`
	Runners   []string          `json:"runners"`
	Installed map[string]string `json:"installed"`
	Shared    *Settings         `json:"shared"`
	Project   *Settings         `json:"project"`
}

func (c *Core) Settings(project string) (*SettingsView, error) {
	lib, err := c.Library(project)
	if err != nil {
		return nil, err
	}
	layer := func(dir string) *Settings {
		var layer Settings
		if readJSON(settingsPath(dir), &layer) != nil {
			return nil
		}
		return &layer
	}
	return &SettingsView{
		Effective: lib.Settings,
		Default:   lib.Settings.defaultRunner(),
		Runners:   lib.Settings.runners(),
		Installed: installedRunners(),
		Shared:    layer(c.home),
		Project:   layer(filepath.Join(project, ".swarm")),
	}, nil
}

func (c *Core) putSettings(lib *Library, scope string, content []byte) error {
	var layer Settings
	if err := strictJSON(content, &layer); err != nil {
		return &RefusedError{[]string{"not valid settings: " + err.Error()}}
	}
	projectDir := filepath.Join(lib.Project, ".swarm")
	if scope == "" {
		scope = "shared"
		if _, err := os.Stat(settingsPath(projectDir)); err == nil {
			scope = "project"
		}
	}
	dir := map[string]string{"shared": c.home, "project": projectDir}[scope]
	if dir == "" {
		return fmt.Errorf("scope is project or shared, not %q", scope)
	}
	merged := newSettings()
	for _, layerDir := range []string{c.home, projectDir} {
		current := layer
		if layerDir != dir {
			current = Settings{}
			_ = readJSON(settingsPath(layerDir), &current)
		}
		addLayer(&merged, current)
	}
	problems := merged.problems()
	lib.Settings = merged
	for _, chain := range lib.sortedChains() {
		for _, p := range lib.check(chain) {
			problems = append(problems, fmt.Sprintf("chain %s would break: %s", chain.Name, p))
		}
	}
	if len(problems) > 0 {
		return &RefusedError{problems}
	}
	return saveJSON(settingsPath(dir), layer)
}
