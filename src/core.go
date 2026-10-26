package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var itemExt = map[string]string{"chains": ".json", "tools": ".json", "prompts": ".md"}

type RefusedError struct {
	Problems []string
}

func (e *RefusedError) Error() string {
	return "refused: " + strings.Join(e.Problems, "; ")
}

type Core struct {
	home     string
	store    *Store
	runner   *Runner
	mu       sync.Mutex
	session  []string
	fallback string
}

func openCore(home string) *Core {
	store := openStore(filepath.Join(home, "runs"))
	return &Core{home: home, store: store, runner: newRunner(home, store)}
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

func (c *Core) Library(project string) (*Library, error) {
	return loadLibrary(c.home, project)
}

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
			return &RefusedError{[]string{"not a valid chain: " + err.Error()}}
		}
		chain.Name = name
		lib.Chains[name] = chain
		if problems := lib.check(chain); len(problems) > 0 {
			return &RefusedError{problems}
		}
		return saveChain(dir, name, chain)
	case "tools":
		var tool Tool
		if err := strictJSON(content, &tool); err != nil {
			return &RefusedError{[]string{"not a valid tool: " + err.Error()}}
		}
		var problems []string
		if strings.TrimSpace(tool.Title) == "" {
			problems = append(problems, "a tool needs a title")
		}
		if strings.TrimSpace(tool.Run) == "" {
			problems = append(problems, "a tool needs a run command")
		}
		if len(problems) > 0 {
			return &RefusedError{problems}
		}
		return saveTool(dir, name, tool)
	default:
		if strings.TrimSpace(string(content)) == "" {
			return &RefusedError{[]string{"a prompt cannot be empty"}}
		}
		return savePrompt(dir, name, string(content))
	}
}

func (c *Core) DeleteItem(project, path string) error {
	kind, name, err := splitItem(path)
	if err != nil {
		return err
	}
	if kind == "settings" {
		return &RefusedError{[]string{"settings can't be deleted; save new ones instead"}}
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
			return &RefusedError{[]string{fmt.Sprintf("%s is still used by %s", path, strings.Join(users, ", "))}}
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
	if d.Choice != "approve" && d.Choice != "reject" && d.Choice != "revise" {
		return errors.New("choice must be approve, revise or reject")
	}
	run := c.store.load(id)
	if run == nil {
		return fmt.Errorf("no run %s", id)
	}
	step := findStep(run.Steps, func(s *StepRun) bool { return s.Status == "waiting" })
	if run.Status != "waiting" || step == nil {
		return errors.New("the run is not waiting for you")
	}
	if d.Choice == "revise" && step.Revise == "" {
		return fmt.Errorf("%q cannot send the run back; give the approval step a revise step first", step.Title)
	}
	if d.Choice == "revise" && strings.TrimSpace(d.Note) == "" {
		return errors.New("say what to change in the note")
	}
	if c.store.hasSignal(id, decisionFile) {
		return errors.New("a decision is already being applied")
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return c.store.signal(id, decisionFile, raw)
}

func (c *Core) Stop(id string) error {
	run := c.store.load(id)
	if run == nil || !run.going() {
		return errors.New("no such run in progress")
	}
	return c.store.signal(id, cancelFile, nil)
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
