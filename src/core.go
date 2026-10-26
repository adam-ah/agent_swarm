package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

var itemExt = map[string]string{"chains": ".json", "tools": ".json", "prompts": ".md"}

type Invalid struct {
	Problems []string
}

func (e *Invalid) Error() string {
	return "refused: " + strings.Join(e.Problems, "; ")
}

type Core struct {
	home     string
	store    *Store
	runner   *Runner
	mu       sync.Mutex
	projects []string
	fallback string
}

func splitItem(path string) (kind, name string, err error) {
	if path == "settings.json" || path == "settings" {
		return "settings", "settings", nil
	}
	kind, file, found := strings.Cut(path, "/")
	ext, known := itemExt[kind]
	name = strings.TrimSuffix(file, ext)
	if !found || !known || !fileName.MatchString(name) {
		return "", "", fmt.Errorf("%q is not chains/<name>.json, tools/<name>.json or prompts/<name>.md", path)
	}
	return kind, name, nil
}

/* projects */

func (c *Core) loadProjects(first string) error {
	_ = readJSON(filepath.Join(c.home, "projects.json"), &c.projects)
	if first != "" {
		abs, err := c.openProject(first, false)
		c.fallback = abs
		return err
	}
	if len(c.projects) > 0 {
		c.fallback = c.projects[0]
	}
	return nil
}

func (c *Core) AddProject(folder string) (string, error) {
	return c.openProject(folder, true)
}

func (c *Core) openProject(folder string, remember bool) (string, error) {
	abs, err := filepath.Abs(folder)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s is not a folder", abs)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !slices.Contains(c.projects, abs) {
		c.projects = append(c.projects, abs)
	}
	if remember {
		var saved []string
		_ = readJSON(filepath.Join(c.home, "projects.json"), &saved)
		if !slices.Contains(saved, abs) {
			if err := saveJSON(filepath.Join(c.home, "projects.json"), append(saved, abs)); err != nil {
				return "", err
			}
		}
	}
	if c.fallback == "" {
		c.fallback = abs
	}
	return abs, nil
}

func (c *Core) Projects() ([]string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.projects), c.fallback
}

func (c *Core) Project(requested string) (string, error) {
	if requested == "" {
		requested = c.fallback
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !slices.Contains(c.projects, requested) {
		return "", fmt.Errorf("%s is not a swarm project yet; run swarm init in it or add it on the page", requested)
	}
	return requested, nil
}

func (c *Core) Library(project string) (*Library, error) {
	return loadLibrary(c.home, project)
}

/* items */

func (c *Core) GetItem(project, path string, below bool) ([]byte, string, error) {
	kind, name, err := splitItem(path)
	if err != nil {
		return nil, "", err
	}
	rel := kind + "/" + name + itemExt[kind]
	if kind == "settings" {
		rel = "settings.json"
	}
	if !below {
		if raw, err := os.ReadFile(filepath.Join(project, ".swarm", rel)); err == nil {
			return raw, "project", nil
		}
	}
	return readShared(c.home, rel)
}

func strictJSON(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func (c *Core) target(lib *Library, kind, name, scope string) (string, error) {
	if scope == "" {
		switch kind {
		case "chains":
			if chain := lib.Chains[name]; chain != nil {
				scope = chain.Source
			}
		case "tools":
			scope = lib.Tools[name].Source
		case "prompts":
			scope = lib.Prompts[name].Source
		}
	}
	switch scope {
	case "", "project":
		return sourceDir(c.home, lib.Project, "project"), nil
	case "shared":
		return sourceDir(c.home, lib.Project, "shared"), nil
	}
	return "", fmt.Errorf("scope is project or shared, not %q", scope)
}

func (c *Core) PutItem(project, path, scope string, content []byte) error {
	kind, name, err := splitItem(path)
	if err != nil {
		return err
	}
	lib, err := c.Library(project)
	if err != nil {
		return err
	}
	if kind == "settings" {
		return c.putSettings(lib, scope, content)
	}
	dir, err := c.target(lib, kind, name, scope)
	if err != nil {
		return err
	}
	switch kind {
	case "chains":
		chain := &Chain{}
		if err := strictJSON(content, chain); err != nil {
			return &Invalid{[]string{"not a valid chain: " + err.Error()}}
		}
		chain.Name = name
		lib.Chains[name] = chain
		if problems := lib.check(chain); len(problems) > 0 {
			return &Invalid{problems}
		}
		return saveChain(dir, name, chain)
	case "tools":
		var tool Tool
		if err := strictJSON(content, &tool); err != nil {
			return &Invalid{[]string{"not a valid tool: " + err.Error()}}
		}
		var problems []string
		if strings.TrimSpace(tool.Title) == "" {
			problems = append(problems, "a tool needs a title")
		}
		if strings.TrimSpace(tool.Run) == "" {
			problems = append(problems, "a tool needs a run command")
		}
		if len(problems) > 0 {
			return &Invalid{problems}
		}
		return saveTool(dir, name, tool)
	default:
		if strings.TrimSpace(string(content)) == "" {
			return &Invalid{[]string{"a prompt cannot be empty"}}
		}
		return savePrompt(dir, name, string(content))
	}
}

func (c *Core) putSettings(lib *Library, scope string, content []byte) error {
	var layer Settings
	if err := strictJSON(content, &layer); err != nil {
		return &Invalid{[]string{"not valid settings: " + err.Error()}}
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
	merged := Settings{Agents: map[string]AgentCLI{}}
	for _, layerDir := range []string{c.home, projectDir} {
		current := layer
		if layerDir != dir {
			current = Settings{}
			_ = readJSON(settingsPath(layerDir), &current)
		}
		if current.Agent != "" {
			merged.Agent = current.Agent
		}
		for name, cli := range current.Agents {
			merged.Agents[name] = cli
		}
	}
	problems := merged.problems()
	lib.Settings = merged
	for _, chain := range lib.sortedChains() {
		for _, p := range lib.check(chain) {
			problems = append(problems, fmt.Sprintf("chain %s would break: %s", chain.Name, p))
		}
	}
	if len(problems) > 0 {
		return &Invalid{problems}
	}
	return saveJSON(settingsPath(dir), layer)
}

func (c *Core) DeleteItem(project, path string) error {
	kind, name, err := splitItem(path)
	if err != nil {
		return err
	}
	if kind == "settings" {
		return &Invalid{[]string{"settings can't be deleted; save new ones instead"}}
	}
	lib, err := c.Library(project)
	if err != nil {
		return err
	}
	var source string
	switch kind {
	case "chains":
		if chain := lib.Chains[name]; chain != nil {
			source = chain.Source
		}
	case "tools":
		source = lib.Tools[name].Source
	case "prompts":
		source = lib.Prompts[name].Source
	}
	if source == "" {
		return fmt.Errorf("nothing at %s", path)
	}
	rel := kind + "/" + name + itemExt[kind]
	sharedRemains := false
	if source == "project" {
		_, _, err := readShared(c.home, rel)
		sharedRemains = err == nil
	}
	if !sharedRemains {
		if users := lib.usersOf(kind, name); len(users) > 0 {
			return &Invalid{[]string{fmt.Sprintf("%s is still used by %s", path, strings.Join(users, ", "))}}
		}
	}
	return os.Remove(filepath.Join(sourceDir(c.home, project, source), rel))
}

func (lib *Library) usersOf(kind, name string) []string {
	var users []string
	for _, chain := range lib.sortedChains() {
		for _, step := range chain.Steps {
			uses := map[string]string{"chains": step.Chain, "tools": step.Tool, "prompts": step.Agent}[kind]
			if uses == name {
				users = append(users, chain.Name+"."+step.ID)
			}
		}
	}
	return users
}

func (c *Core) Check(project string) ([]string, *Library, error) {
	lib, err := c.Library(project)
	if err != nil {
		return nil, nil, err
	}
	return lib.problems(), lib, nil
}

/* memory */

func (c *Core) Memory(project string) string {
	raw, _ := os.ReadFile(memoryPath(project))
	return string(raw)
}

func (c *Core) SetMemory(project, text string) error {
	return writeFile(memoryPath(project), []byte(text))
}

func (c *Core) RememberNotes(project, about string, notes []string) (int, error) {
	var kept []string
	for _, note := range notes {
		if note = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(note), "- ")); note != "" {
			kept = append(kept, "- "+note)
		}
	}
	if len(kept) == 0 {
		return 0, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	heading := time.Now().Format("2006-01-02")
	if about = strings.TrimSpace(firstLine(about)); about != "" {
		heading += " · " + about
	}
	text := strings.TrimLeft(strings.TrimRight(c.Memory(project), "\n")+"\n\n## "+heading+"\n"+strings.Join(kept, "\n")+"\n", "\n")
	return len(kept), writeFile(memoryPath(project), []byte(text))
}

/* runs */

func (c *Core) StartRun(project, chainName string, inputs map[string]string) (*Run, error) {
	lib, err := c.Library(project)
	if err != nil {
		return nil, err
	}
	chain := lib.Chains[chainName]
	if chain == nil {
		return nil, fmt.Errorf("no chain named %s", chainName)
	}
	return c.runner.start(lib, chain, inputs)
}

func (c *Core) Decide(id string, d decision) error {
	run, status := c.store.live(id)
	if run == nil {
		return errors.New("no such run in progress")
	}
	if d.Choice != "approve" && d.Choice != "reject" && d.Choice != "revise" {
		return errors.New("choice must be approve, revise or reject")
	}
	step := c.store.waiting(id)
	if status != "waiting" || step == nil {
		return errors.New("the run is not waiting for you")
	}
	if d.Choice == "revise" && step.Revise == "" {
		return fmt.Errorf("%q cannot send the run back; give the approval step a revise step first", step.Title)
	}
	if d.Choice == "revise" && strings.TrimSpace(d.Note) == "" {
		return errors.New("say what to change in the note")
	}
	select {
	case run.decisions <- d:
		return nil
	default:
		return errors.New("a decision is already being applied")
	}
}

func (c *Core) Stop(id string) error {
	run, _ := c.store.live(id)
	if run == nil {
		return errors.New("no such run in progress")
	}
	run.cancel()
	return nil
}
