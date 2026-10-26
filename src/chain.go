package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Field struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Fields []Field

type Tool struct {
	Title  string   `json:"title"`
	Run    string   `json:"run"`
	Show   []string `json:"show,omitempty"`
	Source string   `json:"source,omitempty"`
}

type Prompt struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

type Input struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Default  string `json:"default,omitempty"`
	Optional bool   `json:"optional,omitempty"`
}

type Step struct {
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	Tool     string            `json:"tool,omitempty"`
	Agent    string            `json:"agent,omitempty"`
	Runner   string            `json:"runner,omitempty"`
	Chain    string            `json:"chain,omitempty"`
	Approval string            `json:"approval,omitempty"`
	Revise   string            `json:"revise,omitempty"`
	With     map[string]string `json:"with,omitempty"`
	Tools    []string          `json:"tools,omitempty"`
	Returns  Fields            `json:"returns,omitempty"`
	Save     string            `json:"save,omitempty"`
	Show     []string          `json:"show,omitempty"`
	When     string            `json:"when,omitempty"`
	StopIf   string            `json:"stop_if,omitempty"`
	Stop     string            `json:"stop,omitempty"`
	Headline string            `json:"headline,omitempty"`
	Always   bool              `json:"always,omitempty"`
}

func (s Step) Kind() string {
	switch {
	case s.Agent != "":
		return "agent"
	case s.Approval != "":
		return "approval"
	case s.Chain != "":
		return "chain"
	default:
		return "tool"
	}
}

func (s Step) Uses() string {
	return s.Tool + s.Agent + s.Chain
}

type Chain struct {
	Name        string   `json:"name,omitempty"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Subject     string   `json:"subject,omitempty"`
	Link        string   `json:"link,omitempty"`
	Inputs      []Input  `json:"inputs,omitempty"`
	Vars        Fields   `json:"vars,omitempty"`
	Steps       []Step   `json:"steps"`
	Problems    []string `json:"problems,omitempty"`
	Source      string   `json:"source,omitempty"`
}

type Library struct {
	Home     string
	Project  string
	Tools    map[string]Tool
	Chains   map[string]*Chain
	Prompts  map[string]Prompt
	Settings Settings
}

func sourceDir(home, project, source string) string {
	switch source {
	case "project":
		return filepath.Join(project, ".swarm")
	case "shared":
		return home
	}
	return ""
}

func layers(home, project string) []struct {
	name string
	fsys fs.FS
} {
	return []struct {
		name string
		fsys fs.FS
	}{
		{"shared", os.DirFS(home)},
		{"project", os.DirFS(filepath.Join(project, ".swarm"))},
	}
}

func decodeJSON(fsys fs.FS, path string, v any) error {
	raw, err := fs.ReadFile(fsys, path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

var (
	stepID   = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	fileName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
)

func loadLibrary(home, project string) (*Library, error) {
	lib := &Library{Home: home, Project: project, Tools: map[string]Tool{}, Chains: map[string]*Chain{}, Prompts: map[string]Prompt{}}
	settings, err := loadSettings(home, project)
	if err != nil {
		return nil, err
	}
	lib.Settings = settings
	for _, layer := range layers(home, project) {
		source := layer.name
		tools, _ := fs.Glob(layer.fsys, "tools/*.json")
		for _, file := range tools {
			var tool Tool
			if err := decodeJSON(layer.fsys, file, &tool); err != nil {
				return nil, fmt.Errorf("%s %w", source, err)
			}
			tool.Source = source
			lib.Tools[strings.TrimSuffix(path.Base(file), ".json")] = tool
		}
		prompts, _ := fs.Glob(layer.fsys, "prompts/*.md")
		for _, file := range prompts {
			text, err := fs.ReadFile(layer.fsys, file)
			if err == nil {
				lib.Prompts[strings.TrimSuffix(path.Base(file), ".md")] = Prompt{Text: string(text), Source: source}
			}
		}
		chains, _ := fs.Glob(layer.fsys, "chains/*.json")
		for _, file := range chains {
			name := strings.TrimSuffix(path.Base(file), ".json")
			chain := &Chain{Title: name}
			if err := decodeJSON(layer.fsys, file, chain); err != nil {
				chain.Problems = append(chain.Problems, err.Error())
			}
			chain.Name = name
			chain.Source = source
			lib.Chains[name] = chain
		}
	}
	for _, chain := range lib.Chains {
		if len(chain.Problems) == 0 {
			chain.Problems = lib.check(chain)
		}
	}
	return lib, nil
}

func (lib *Library) check(chain *Chain) []string {
	problems := []string{}
	seen := map[string]bool{}
	for _, step := range chain.Steps {
		where := fmt.Sprintf("step %q", step.ID)
		if !stepID.MatchString(step.ID) {
			problems = append(problems, where+": ids are lowercase letters, digits and underscores, starting with a letter")
		}
		if seen[step.ID] {
			problems = append(problems, where+": duplicate id")
		}
		kinds := 0
		for _, set := range []bool{step.Tool != "", step.Agent != "", step.Approval != "", step.Chain != ""} {
			if set {
				kinds++
			}
		}
		if kinds != 1 {
			problems = append(problems, where+": needs exactly one of tool, agent, approval or chain")
		}
		if step.Tool != "" {
			if _, ok := lib.Tools[step.Tool]; !ok {
				problems = append(problems, fmt.Sprintf("%s: no tool named %q", where, step.Tool))
			}
		}
		if step.Agent != "" {
			if _, ok := lib.Prompts[step.Agent]; !ok {
				problems = append(problems, fmt.Sprintf("%s: no prompt prompts/%s.md", where, step.Agent))
			}
		}
		if step.Runner != "" && step.Agent == "" {
			problems = append(problems, where+": runner only applies to agent steps")
		}
		if step.Runner != "" && !lib.Settings.knows(step.Runner) {
			problems = append(problems, fmt.Sprintf("%s: no agent named %q; use claude, codex, or add it under agents in settings", where, step.Runner))
		}
		if step.Chain != "" {
			if _, ok := lib.Chains[step.Chain]; !ok {
				problems = append(problems, fmt.Sprintf("%s: no chain named %q", where, step.Chain))
			} else if lib.calls(step.Chain, chain.Name, 0) {
				problems = append(problems, fmt.Sprintf("%s: %q calls back into %q", where, step.Chain, chain.Name))
			}
		}
		if step.Revise != "" && step.Approval == "" {
			problems = append(problems, where+": revise only applies to approval steps")
		} else if step.Revise != "" && !seen[step.Revise] {
			problems = append(problems, fmt.Sprintf("%s: revise must name an earlier step in this chain, not %q", where, step.Revise))
		}
		for _, f := range step.Returns {
			if _, err := fieldSchema(f.Value); err != nil {
				problems = append(problems, fmt.Sprintf("%s: returns %s: %v", where, f.Name, err))
			}
		}
		for _, c := range []string{step.When, step.StopIf} {
			if c == "" {
				continue
			}
			if _, _, _, err := parseCondition(c); err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", where, err))
			}
		}
		seen[step.ID] = true
	}
	for _, in := range chain.Inputs {
		if !stepID.MatchString(in.Name) {
			problems = append(problems, fmt.Sprintf("input %q: names are lowercase letters, digits and underscores", in.Name))
		}
	}
	return problems
}

func (c *Chain) stepIndex(id string) int {
	for i, step := range c.Steps {
		if step.ID == id {
			return i
		}
	}
	return -1
}

func (lib *Library) calls(from, target string, depth int) bool {
	if from == target || depth > 8 {
		return true
	}
	chain := lib.Chains[from]
	if chain == nil {
		return false
	}
	for _, step := range chain.Steps {
		if step.Chain != "" && lib.calls(step.Chain, target, depth+1) {
			return true
		}
	}
	return false
}

func (lib *Library) sortedChains() []*Chain {
	out := make([]*Chain, 0, len(lib.Chains))
	for _, c := range lib.Chains {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func saveChain(dir, name string, chain *Chain) error {
	chain.Name = ""
	chain.Problems = nil
	chain.Source = ""
	return saveJSON(filepath.Join(dir, "chains", name+".json"), chain)
}

func saveTool(dir, name string, tool Tool) error {
	tool.Source = ""
	return saveJSON(filepath.Join(dir, "tools", name+".json"), tool)
}

func savePrompt(dir, name, text string) error {
	return writeFile(filepath.Join(dir, "prompts", name+".md"), []byte(text))
}

func memoryPath(project string) string {
	return filepath.Join(project, ".swarm", "memory.md")
}

func readMemory(project string) string {
	raw, _ := os.ReadFile(memoryPath(project))
	return string(raw)
}

func (lib *Library) catalog() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Agent steps run with %s unless a step sets runner. Agents available: %s.\n\n",
		lib.Settings.defaultRunner(), strings.Join(lib.Settings.runners(), ", "))
	b.WriteString("Chains:\n")
	for _, c := range lib.sortedChains() {
		fmt.Fprintf(&b, "- %s (%s): %s\n", c.Name, c.Source, c.Title)
		for _, s := range c.Steps {
			uses := strings.TrimSpace(s.Kind() + " " + s.Uses())
			if s.Runner != "" {
				uses += " via " + s.Runner
			}
			fmt.Fprintf(&b, "    %s: %s [%s]\n", s.ID, s.Title, uses)
		}
	}
	b.WriteString("Tools:\n")
	for _, name := range sortedKeys(lib.Tools) {
		fmt.Fprintf(&b, "- %s (%s): %s\n", name, lib.Tools[name].Source, lib.Tools[name].Title)
	}
	b.WriteString("Prompts:\n")
	for _, name := range sortedKeys(lib.Prompts) {
		fmt.Fprintf(&b, "- %s (%s): %s\n", name, lib.Prompts[name].Source, firstLine(lib.Prompts[name].Text))
	}
	return b.String()
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func readShared(home, rel string) ([]byte, string, error) {
	raw, err := os.ReadFile(filepath.Join(home, rel))
	if err != nil {
		return nil, "", fmt.Errorf("%s is not in the shared library %s", rel, home)
	}
	return raw, "shared", nil
}

func (lib *Library) problems() []string {
	var out []string
	for _, chain := range lib.sortedChains() {
		for _, p := range chain.Problems {
			out = append(out, fmt.Sprintf("%s (%s): %s", chain.Name, chain.Source, p))
		}
	}
	return out
}

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return nil
}

func saveJSON(path string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	return writeFile(path, buf.Bytes())
}

func writeFile(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func fieldSchema(kind string) (map[string]any, error) {
	kind = strings.TrimSpace(kind)
	switch kind {
	case "text":
		return map[string]any{"type": "string"}, nil
	case "number":
		return map[string]any{"type": "number"}, nil
	case "bool":
		return map[string]any{"type": "boolean"}, nil
	case "list":
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, nil
	}
	if strings.Contains(kind, "|") {
		var options []string
		for _, option := range strings.Split(kind, "|") {
			options = append(options, strings.TrimSpace(option))
		}
		return map[string]any{"type": "string", "enum": options}, nil
	}
	return nil, fmt.Errorf("%q is not text, number, bool, list or A | B | C", kind)
}

func returnsSchema(fields Fields) map[string]any {
	properties := map[string]any{}
	var required []string
	for _, f := range fields {
		schema, _ := fieldSchema(f.Value)
		properties[f.Name] = schema
		required = append(required, f.Name)
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

var condition = regexp.MustCompile(`^\s*([a-z][a-z0-9_.]*)\s*(==|!=)\s*(.*?)\s*$`)

func parseCondition(text string) (name, op, value string, err error) {
	m := condition.FindStringSubmatch(text)
	if m == nil {
		return "", "", "", fmt.Errorf("cannot read condition %q; write it as name == value or name != value", text)
	}
	return m[1], m[2], strings.Trim(m[3], `"'`), nil
}
