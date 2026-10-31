package main

import (
	"fmt"
	"slices"
	"strings"
)

func (lib *Library) callersOf(name string) []string {
	var callers []string
	for _, user := range lib.usersOf("chains", name) {
		chain, _, _ := strings.Cut(user, ".")
		if chain != name && !slices.Contains(callers, chain) {
			callers = append(callers, chain)
		}
	}
	return callers
}

func (lib *Library) catalog() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Agent steps run with %s unless a step sets runner. Agents available: %s.\n\n",
		lib.Settings.defaultRunner(), strings.Join(lib.Settings.runners(), ", "))
	var subchains []*Chain
	b.WriteString("Chains to start from:\n")
	for _, c := range lib.sortedChains() {
		if len(lib.callersOf(c.Name)) > 0 {
			subchains = append(subchains, c)
			continue
		}
		writeChain(&b, c, "")
	}
	if len(subchains) > 0 {
		b.WriteString("Sub-chains, run by other chains:\n")
	}
	for _, c := range subchains {
		writeChain(&b, c, "; used by "+strings.Join(lib.callersOf(c.Name), ", "))
	}
	b.WriteString("Tools:\n")
	for _, name := range sortedKeys(lib.Tools) {
		fmt.Fprintf(&b, "- %s (%s): %s\n", name, lib.Tools[name].Source, lib.Tools[name].Title)
	}
	b.WriteString("Prompts, each kept with its chain:\n")
	for _, c := range lib.sortedChains() {
		for _, name := range sortedKeys(c.Prompts) {
			fmt.Fprintf(&b, "- chains/%s/%s.md: %s\n", c.Name, name, firstLine(c.Prompts[name].Text))
		}
	}
	return b.String()
}

func writeChain(b *strings.Builder, c *Chain, usedBy string) {
	fmt.Fprintf(b, "- %s (%s): %s%s\n", c.Name, c.Source, c.Title, usedBy)
	for _, s := range c.Steps {
		uses := strings.TrimSpace(s.Kind() + " " + s.Uses())
		if s.Runner != "" {
			uses += " via " + s.Runner
		}
		fmt.Fprintf(b, "    %s: %s [%s]\n", s.ID, s.Title, uses)
	}
}
