package main

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func promptsIn(fsys fs.FS, chain, source string) map[string]Prompt {
	prompts := map[string]Prompt{}
	files, _ := fs.Glob(fsys, "chains/"+chain+"/*.md")
	for _, file := range files {
		if text, err := fs.ReadFile(fsys, file); err == nil {
			prompts[strings.TrimSuffix(path.Base(file), ".md")] = newPrompt(string(text), source)
		}
	}
	return prompts
}

func savePrompt(dir, chain, name, text string) error {
	return writeFile(filepath.Join(dir, "chains", chain, name+".md"), []byte(text))
}

func (lib *Library) promptCount() int {
	n := 0
	for _, c := range lib.Chains {
		n += len(c.Prompts)
	}
	return n
}

func (lib *Library) adoptLegacyPrompts(chain *Chain) {
	own := sourceDir(lib.Home, lib.Project, chain.Source)
	for _, step := range chain.Steps {
		if _, has := chain.Prompts[step.Agent]; step.Agent == "" || has || !fileName.MatchString(step.Agent) {
			continue
		}
		for _, dir := range []string{own, lib.Home} {
			text, err := os.ReadFile(filepath.Join(dir, "prompts", step.Agent+".md"))
			if err != nil {
				continue
			}
			if savePrompt(own, chain.Name, step.Agent, string(text)) == nil {
				chain.Prompts[step.Agent] = newPrompt(string(text), chain.Source)
			}
			break
		}
	}
}
