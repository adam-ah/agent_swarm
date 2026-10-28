package main

import (
	"fmt"
	"strings"
	"time"
)

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
	for _, set := range []bool{step.Tool != "", step.Agent != "", step.User != "", step.Chain != ""} {
		if set {
			kinds++
		}
	}
	if kinds != 1 {
		problems = append(problems, where+": needs exactly one of tool, agent, user or chain")
	}
	problems = append(problems, lib.referenceProblems(chain, step, where)...)
	problems = append(problems, fieldProblems(chain, step, where, seen)...)
	problems = append(problems, conditionProblems(step, where)...)
	if step.Resume != "" {
		problems = append(problems, lib.resumeProblems(chain, step, where, seen)...)
	}
	return problems
}

func (lib *Library) resumeProblems(chain *Chain, step Step, where string, seen map[string]bool) []string {
	runner := lib.Settings.runnerFor(step)
	switch {
	case step.Agent == "":
		return []string{where + ": resume only applies to agent steps"}
	case !seen[step.Resume] || chain.Steps[chain.stepIndex(step.Resume)].Agent == "":
		return []string{fmt.Sprintf("%s: resume must name an earlier agent step in this chain, not %q", where, step.Resume)}
	case !lib.Settings.canResume(runner):
		return []string{fmt.Sprintf("%s: %s cannot resume a session; only claude, codex and agents that run them can", where, runner)}
	}
	if other := lib.Settings.runnerFor(chain.Steps[chain.stepIndex(step.Resume)]); other != runner {
		return []string{fmt.Sprintf("%s: %s runs with %s and this step with %s; a session resumes only on the agent that made it", where, step.Resume, other, runner)}
	}
	return nil
}

func (lib *Library) referenceProblems(chain *Chain, step Step, where string) []string {
	var problems []string
	if step.Tool != "" {
		if _, ok := lib.Tools[step.Tool]; !ok {
			problems = append(problems, fmt.Sprintf("%s: no tool named %q", where, step.Tool))
		}
	}
	if step.Agent != "" {
		prompt, ok := chain.Prompts[step.Agent]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: no prompt chains/%s/%s.md", where, chain.Name, step.Agent))
		}
		for _, p := range prompt.Problems {
			problems = append(problems, fmt.Sprintf("%s: chains/%s/%s.md %s", where, chain.Name, step.Agent, p))
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
		if lists := lib.listsPassed(chain, step); len(lists) > 1 {
			problems = append(problems, fmt.Sprintf("%s: passes %d lists (%s); it can run %s once per item of only one", where, len(lists), strings.Join(lists, ", "), step.Chain))
		}
	}
	return problems
}

func fieldProblems(chain *Chain, step Step, where string, seen map[string]bool) []string {
	var problems []string
	if step.Revise != "" && step.User == "" {
		problems = append(problems, where+": revise only applies to user steps")
	} else if step.Revise != "" && !seen[step.Revise] {
		problems = append(problems, fmt.Sprintf("%s: revise must name an earlier step in this chain, not %q", where, step.Revise))
	}
	if len(step.Show) > 0 && step.User == "" {
		problems = append(problems, where+": show only applies to user steps")
	}
	for _, limit := range [][2]string{{"stall", step.Stall}, {"timeout", step.Timeout}} {
		name, value := limit[0], limit[1]
		if d, err := time.ParseDuration(value); value != "" && step.Tool == "" && step.Agent == "" {
			problems = append(problems, fmt.Sprintf("%s: %s only applies to tool and agent steps", where, name))
		} else if value != "" && (err != nil || d < 0) {
			problems = append(problems, fmt.Sprintf("%s: %s is a duration such as 30m or 1h, or 0 for no limit, not %q", where, name, value))
		}
	}
	return append(problems, keepProblems(chain, step, where, seen)...)
}

func keepProblems(chain *Chain, step Step, where string, seen map[string]bool) []string {
	switch {
	case step.KeepUntil == "":
		return nil
	case step.Tool == "":
		return []string{where + ": keep_until only applies to tool steps"}
	case seen[step.KeepUntil] || step.KeepUntil == step.ID || chain.stepIndex(step.KeepUntil) < 0:
		return []string{fmt.Sprintf("%s: keep_until must name a later step in this chain, not %q", where, step.KeepUntil)}
	}
	return nil
}

func conditionProblems(step Step, where string) []string {
	var problems []string
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
