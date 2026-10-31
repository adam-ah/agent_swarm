package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
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
	if chain, file, nested := strings.Cut(strings.TrimPrefix(path, "chains/"), "/"); strings.HasPrefix(path, "chains/") && nested {
		prompt := strings.TrimSuffix(file, ".md")
		if !strings.HasSuffix(file, ".md") || !fileName.MatchString(chain) || !fileName.MatchString(prompt) {
			return "", "", fmt.Errorf("%q is not chains/<chain>/<prompt>.md", path)
		}
		return "prompts", chain + "/" + prompt, nil
	}
	kind, file, found := strings.Cut(path, "/")
	ext, known := itemExt[kind]
	name = strings.TrimSuffix(file, ext)
	if !found || !known || kind == "prompts" || !fileName.MatchString(name) {
		return "", "", fmt.Errorf("%q is not chains/<name>.json, tools/<name>.json or chains/<chain>/<prompt>.md (a prompt lives with its chain)", path)
	}
	return kind, name, nil
}

func itemRel(kind, name string) string {
	switch kind {
	case "settings":
		return "settings.json"
	case "prompts":
		return "chains/" + name + ".md"
	}
	return kind + "/" + name + itemExt[kind]
}

func (c *Core) Library(project string) (*Library, error) {
	return loadLibrary(c.home, project)
}

func (c *Core) GetItem(project, path string, below bool) ([]byte, string, error) {
	kind, name, err := splitItem(path)
	if err != nil {
		return nil, "", err
	}
	rel := itemRel(kind, name)
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
		return c.putChain(lib, project, dir, name, content)
	case "tools":
		var tool Tool
		if err := strictJSON(content, &tool); err != nil {
			return &RefusedError{[]string{"not a valid tool: " + err.Error()}}
		}
		if problems := toolProblems(tool); len(problems) > 0 {
			return &RefusedError{problems}
		}
		return saveTool(dir, name, tool)
	default:
		if problems := promptProblems(string(content)); len(problems) > 0 {
			return &RefusedError{problems}
		}
		chain, prompt, _ := strings.Cut(name, "/")
		return savePrompt(dir, chain, prompt, string(content))
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
		owner, prompt, _ := strings.Cut(name, "/")
		if chain := lib.Chains[owner]; chain != nil {
			if _, has := chain.Prompts[prompt]; has {
				source = chain.Source
			}
		}
	}
	if source == "" {
		return fmt.Errorf("nothing at %s", path)
	}
	rel := itemRel(kind, name)
	sharedRemains := false
	if source == "project" && kind != "prompts" {
		_, _, err := readShared(c.home, rel)
		sharedRemains = err == nil
	}
	if !sharedRemains {
		if users := lib.usersOf(kind, name); len(users) > 0 {
			return &RefusedError{[]string{fmt.Sprintf("%s is still used by %s", path, strings.Join(users, ", "))}}
		}
	}
	return removeItem(sourceDir(c.home, project, source), kind, name)
}

func removeItem(dir, kind, name string) error {
	if err := os.Remove(filepath.Join(dir, itemRel(kind, name))); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if kind == "chains" {
		return os.RemoveAll(filepath.Join(dir, "chains", name))
	}
	return nil
}

func (lib *Library) usersOf(kind, name string) []string {
	owner, prompt, _ := strings.Cut(name, "/")
	var users []string
	for _, chain := range lib.sortedChains() {
		for _, step := range chain.Steps {
			uses := map[string]string{"chains": step.Chain, "tools": step.Tool}[kind] == name
			if kind == "prompts" {
				uses = chain.Name == owner && step.Agent == prompt
			}
			if uses {
				users = append(users, chain.Name+"."+step.ID)
			}
		}
	}
	return users
}

func (lib *Library) chainsUsing(path string) []string {
	kind, name, err := splitItem(path)
	if err != nil || kind == "settings" {
		return nil
	}
	chains := []string{}
	if kind == "chains" {
		chains = append(chains, name)
	}
	for _, user := range lib.usersOf(kind, name) {
		chain, _, _ := strings.Cut(user, ".")
		if !slices.Contains(chains, chain) {
			chains = append(chains, chain)
		}
	}
	return chains
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
	unlock, err := lockMemory(project)
	if err != nil {
		return err
	}
	defer unlock()
	return writeFile(memoryPath(project), []byte(text))
}

func lockMemory(project string) (func(), error) {
	dir := filepath.Dir(memoryPath(project))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	folder, err := syscall.Open(dir, syscall.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(folder, syscall.LOCK_EX); err != nil {
		_ = syscall.Close(folder)
		return nil, err
	}
	return func() { _ = syscall.Close(folder) }, nil
}

func (c *Core) RememberNotes(project, about string, notes []string) (int, error) {
	var kept []string
	for _, note := range notes {
		note = strings.TrimSpace(note)
		if note == "-" {
			continue
		}
		if note = strings.TrimSpace(strings.TrimPrefix(note, "- ")); note != "" {
			kept = append(kept, "- "+note)
		}
	}
	if len(kept) == 0 {
		return 0, nil
	}
	unlock, err := lockMemory(project)
	if err != nil {
		return 0, err
	}
	defer unlock()
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

func (c *Core) Stop(id string) error {
	run := c.store.load(id)
	if run == nil || !run.going() {
		return errors.New("no such run in progress")
	}
	return c.store.signal(id, cancelFile, nil)
}

func (c *Core) putChain(lib *Library, project, dir, name string, content []byte) error {
	chain := &Chain{}
	if err := strictJSON(content, chain); err != nil {
		return &RefusedError{[]string{"not a valid chain: " + err.Error()}}
	}
	chain.Name = name
	chain.Prompts = promptsIn(os.DirFS(dir), name, "")
	inherited := map[string]Prompt{}
	if old := lib.Chains[name]; old != nil && sourceDir(c.home, project, old.Source) != dir {
		for prompt, p := range old.Prompts {
			if _, has := chain.Prompts[prompt]; !has {
				inherited[prompt], chain.Prompts[prompt] = p, p
			}
		}
	}
	lib.Chains[name] = chain
	if problems := lib.check(chain); len(problems) > 0 {
		return &RefusedError{problems}
	}
	for prompt, p := range inherited {
		if err := savePrompt(dir, name, prompt, p.Text); err != nil {
			return err
		}
	}
	return saveChain(dir, name, chain)
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
			owner, _, _ := strings.Cut(name, "/")
			if chain := lib.Chains[owner]; chain != nil {
				scope = chain.Source
			}
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
