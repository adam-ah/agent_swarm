package main

import "fmt"

func (lib *Library) check(chain *Chain) []string {
	problems := []string{}
	seen := map[string]bool{}
	for _, step := range chain.Steps {
		problems = append(problems, lib.stepProblems(chain, step, seen)...)
		seen[step.ID] = true
	}
	for _, in := range chain.Inputs {
		if !stepID.MatchString(in.Name) {
			problems = append(problems, fmt.Sprintf("input %q: names are lowercase letters, digits and underscores", in.Name))
		}
	}
	return problems
}

func (lib *Library) stepProblems(chain *Chain, step Step, seen map[string]bool) []string {
	where := fmt.Sprintf("step %q", step.ID)
	var problems []string
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
	problems = append(problems, lib.referenceProblems(chain, step, where)...)
	problems = append(problems, fieldProblems(step, where, seen)...)
	return problems
}

func (lib *Library) referenceProblems(chain *Chain, step Step, where string) []string {
	var problems []string
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
	return problems
}

func fieldProblems(step Step, where string, seen map[string]bool) []string {
	var problems []string
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
	return problems
}
