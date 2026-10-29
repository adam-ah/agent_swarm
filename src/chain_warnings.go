package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

type outputs struct {
	fields []string
	any    bool
}

type template struct {
	where string
	text  string
}

type warner struct {
	lib      *Library
	chain    *Chain
	names    map[string]bool
	earlier  map[string]outputs
	all      map[string]outputs
	warnings []string
}

func outputsOf(chain *Chain, step Step) outputs {
	switch step.Kind() {
	case "agent":
		returns := chain.Prompts[step.Agent].Returns
		if len(returns) == 0 {
			return outputs{fields: []string{"output"}}
		}
		names := make([]string, 0, len(returns))
		for _, f := range returns {
			names = append(names, f.Name)
		}
		return outputs{fields: names}
	case "user":
		return outputs{fields: []string{"decision", "note"}}
	case "repeat":
		return outputs{}
	}
	return outputs{any: true}
}

func (lib *Library) stepTemplates(chain *Chain, step Step) []template {
	switch step.Kind() {
	case "tool":
		return []template{{"tools/" + step.Tool + ".json", lib.Tools[step.Tool].Run}}
	case "agent":
		return []template{{"chains/" + chain.Name + "/" + step.Agent + ".md", chain.Prompts[step.Agent].Text}}
	case "user":
		return []template{{"the question", step.User}, {"the files to show", strings.Join(step.Show, "\n")}}
	}
	return nil
}

func (lib *Library) produced(chain *Chain, step Step, into map[string]outputs, depth int) {
	into[step.ID] = outputsOf(chain, step)
	if len(lib.listsPassed(chain, step)) > 0 {
		into[step.ID] = outputs{fields: []string{"summary"}}
		return
	}
	if sub := lib.Chains[step.Chain]; sub != nil && depth < 8 {
		for _, s := range sub.Steps {
			if _, taken := into[s.ID]; !taken {
				lib.produced(sub, s, into, depth+1)
			}
		}
	}
}

func (lib *Library) listsPassed(chain *Chain, step Step) []string {
	var lists []string
	for _, k := range sortedKeys(step.With) {
		name, lone := lonePlaceholder(step.With[k])
		id, field, dotted := strings.Cut(name, ".")
		if !lone || !dotted || step.Chain == "" {
			continue
		}
		if i := chain.stepIndex(id); i >= 0 && chain.Steps[i].Agent != "" {
			for _, f := range chain.Prompts[chain.Steps[i].Agent].Returns {
				if f.Name == field && f.Value == "list" {
					lists = append(lists, name)
				}
			}
		}
	}
	return lists
}

func (lib *Library) warnings(chain *Chain) []string {
	w := &warner{lib: lib, chain: chain, names: map[string]bool{}, earlier: map[string]outputs{}, all: map[string]outputs{}}
	for _, step := range chain.Steps {
		lib.produced(chain, step, w.all, 0)
	}
	for _, name := range builtinNames {
		w.names[name] = true
	}
	for _, in := range chain.Inputs {
		w.names[in.Name] = true
	}
	for _, v := range chain.Vars {
		w.read(fmt.Sprintf("var %q", v.Name), v.Value, nil, false, w.earlier)
		w.names[v.Name] = true
	}
	for _, step := range chain.Steps {
		w.step(step)
		lib.produced(chain, step, w.earlier, 0)
	}
	w.read("the run heading", chain.Subject, nil, false, w.all)
	w.read("the run link", chain.Link, nil, false, w.all)
	reads := lib.reads(chain, 0)
	for _, in := range chain.Inputs {
		if !reads[in.Name] {
			w.warnf("input %q is not used by any step", in.Name)
		}
	}
	for _, v := range chain.Vars {
		if !reads[v.Name] {
			w.warnf("var %q is not used", v.Name)
		}
	}
	for _, parent := range lib.Chains {
		if slices.ContainsFunc(parent.Steps, func(s Step) bool { return s.Chain == chain.Name }) {
			maps.Copy(reads, lib.reads(parent, 0))
		}
	}
	for _, step := range chain.Steps {
		w.unread(step, reads)
	}
	return w.warnings
}

func (w *warner) unread(step Step, reads map[string]bool) {
	if step.Kind() != "agent" {
		return
	}
	own := map[string]bool{}
	if name, _, _, err := parseCondition(step.StopIf); err == nil {
		own[name] = true
	}
	for _, name := range placeholders(step.Stop) {
		own[name] = true
	}
	for _, field := range outputsOf(w.chain, step).fields {
		if field == "summary" || field == "output" || own[field] || reads[step.ID+"."+field] {
			continue
		}
		w.warnf("step %q returns %s, but nothing reads {{%s.%s}}", step.ID, field, step.ID, field)
	}
}

func (w *warner) warnf(format string, args ...any) {
	w.warnings = append(w.warnings, fmt.Sprintf(format, args...))
}

func (w *warner) read(where, text string, local map[string]bool, anyLocal bool, steps map[string]outputs) {
	for _, name := range placeholders(text) {
		if w.names[name] || local[name] {
			continue
		}
		id, field, dotted := strings.Cut(name, ".")
		out, ran := steps[id]
		_, exists := w.all[id]
		switch {
		case !dotted && anyLocal:
		case !dotted:
			w.warnf("%s reads {{%s}}, which nothing fills", where, name)
		case ran && (out.any || slices.Contains(out.fields, field)):
		case ran:
			w.warnf("%s reads {{%s}}, but %s returns only %s", where, name, id, strings.Join(out.fields, ", "))
		case exists:
			w.warnf("%s reads {{%s}} before %s runs", where, name, id)
		default:
			w.warnf("%s reads {{%s}}, but there is no step %s", where, name, id)
		}
	}
}

func (w *warner) step(step Step) {
	where := fmt.Sprintf("step %q", step.ID)
	with := map[string]bool{}
	for _, k := range sortedKeys(step.With) {
		with[k] = true
		w.read(where+": the value for "+k, step.With[k], nil, false, w.earlier)
	}
	if name, _, _, err := parseCondition(step.When); err == nil {
		w.read(where+": run only if", "{{"+name+"}}", nil, false, w.earlier)
	}
	if step.Kind() == "repeat" {
		return
	}
	reads := map[string]bool{}
	for _, t := range w.lib.stepTemplates(w.chain, step) {
		w.read(where+": "+t.where, t.text, with, false, w.earlier)
		for _, name := range placeholders(t.text) {
			reads[name] = true
		}
	}
	own := outputsOf(w.chain, step)
	local := maps.Clone(with)
	for _, f := range own.fields {
		local[f] = true
	}
	w.read(where+": the stop message", step.Stop, local, own.any, w.earlier)
	if name, _, _, err := parseCondition(step.StopIf); err == nil {
		w.read(where+": stop the chain if", "{{"+name+"}}", local, own.any, w.earlier)
		reads[name] = true
	}
	for _, name := range placeholders(step.Stop) {
		reads[name] = true
	}
	if step.Chain != "" {
		w.subchain(where, step, with)
		return
	}
	for _, k := range sortedKeys(step.With) {
		if !reads[k] {
			w.warnf("%s: passes %s, but %s does not read {{%s}}", where, k, w.lib.stepTemplates(w.chain, step)[0].where, k)
		}
	}
}

func (w *warner) subchain(where string, step Step, with map[string]bool) {
	sub := w.lib.Chains[step.Chain]
	if sub == nil {
		return
	}
	inputs := map[string]bool{}
	for _, in := range sub.Inputs {
		inputs[in.Name] = true
		if !with[in.Name] && !w.names[in.Name] && !in.Optional && in.Default == "" {
			w.warnf("%s: nothing passes %s, which %s needs", where, in.Name, step.Chain)
		}
	}
	for _, k := range sortedKeys(step.With) {
		if !inputs[k] {
			w.warnf("%s: passes %s, but %s has no input %s", where, k, step.Chain, k)
		}
	}
}

func (lib *Library) reads(chain *Chain, depth int) map[string]bool {
	out := map[string]bool{}
	add := func(text string) {
		for _, name := range placeholders(text) {
			out[name] = true
		}
	}
	add(chain.Subject)
	add(chain.Link)
	for _, v := range chain.Vars {
		add(v.Value)
	}
	for _, step := range chain.Steps {
		for _, v := range step.With {
			add(v)
		}
		for _, t := range lib.stepTemplates(chain, step) {
			add(t.text)
		}
		add(step.Stop)
		for _, c := range []string{step.When, step.StopIf} {
			if name, _, _, err := parseCondition(c); err == nil {
				out[name] = true
			}
		}
		if sub := lib.Chains[step.Chain]; sub != nil && depth < 8 {
			maps.Copy(out, lib.reads(sub, depth+1))
		}
	}
	return out
}
