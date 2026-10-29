package main

import (
	"encoding/json"
	"strings"
)

type Draft struct {
	Chains  map[string]json.RawMessage `json:"chains"`
	Prompts map[string]string          `json:"prompts"`
	Tools   map[string]Tool            `json:"tools"`
}

type Verdict struct {
	Problems []string `json:"problems"`
	Warnings []string `json:"warnings"`
}

func toolProblems(tool Tool) []string {
	var problems []string
	if strings.TrimSpace(tool.Title) == "" {
		problems = append(problems, "a tool needs a title")
	}
	if strings.TrimSpace(tool.Run) == "" {
		problems = append(problems, "a tool needs a run command")
	}
	return problems
}

func promptProblems(text string) []string {
	if strings.TrimSpace(text) == "" {
		return []string{"a prompt cannot be empty"}
	}
	return newPrompt(text, "").Problems
}

func (c *Core) CheckDraft(project string, d Draft) (map[string]*Verdict, error) {
	lib, err := c.Library(project)
	if err != nil {
		return nil, err
	}
	out := map[string]*Verdict{}
	affected := map[string]bool{}
	for name, raw := range d.Chains {
		path := "chains/" + name + ".json"
		chain := &Chain{}
		if err := strictJSON(raw, chain); err != nil {
			out[path] = &Verdict{Problems: []string{"not a valid chain: " + err.Error()}}
			continue
		}
		chain.Name = name
		if old := lib.Chains[name]; old != nil {
			chain.Prompts, chain.Source = old.Prompts, old.Source
		}
		lib.Chains[name] = chain
		for _, user := range lib.chainsUsing(path) {
			affected[user] = true
		}
	}
	for key, text := range d.Prompts {
		owner, name, _ := strings.Cut(key, "/")
		if chain := lib.Chains[owner]; chain != nil {
			chain.Prompts = withPrompt(chain.Prompts, name, newPrompt(text, chain.Source))
			affected[owner] = true
		}
		if problems := promptProblems(text); len(problems) > 0 {
			out["chains/"+key+".md"] = &Verdict{Problems: problems}
		}
	}
	for name, tool := range d.Tools {
		lib.Tools[name] = tool
		for _, user := range lib.chainsUsing("tools/" + name + ".json") {
			affected[user] = true
		}
		if problems := toolProblems(tool); len(problems) > 0 {
			out["tools/"+name+".json"] = &Verdict{Problems: problems}
		}
	}
	for name := range affected {
		chain := lib.Chains[name]
		v := &Verdict{Problems: lib.check(chain), Warnings: []string{}}
		if len(v.Problems) == 0 {
			v.Warnings = lib.warnings(chain)
		}
		out["chains/"+name+".json"] = v
	}
	return out, nil
}

func withPrompt(prompts map[string]Prompt, name string, p Prompt) map[string]Prompt {
	out := map[string]Prompt{name: p}
	for k, v := range prompts {
		if k != name {
			out[k] = v
		}
	}
	return out
}
