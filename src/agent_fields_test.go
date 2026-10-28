package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeClaude(t *testing.T, script string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func agentChain(runner string) *Chain {
	return &Chain{Name: "main", Steps: []Step{{ID: "do", Title: "Do it", Agent: "do", Runner: runner}},
		Prompts: map[string]Prompt{"do": {Text: "Do it.", Returns: Fields{{Name: "summary", Value: "text"}}}}}
}

func TestAgentIsAskedAgainForMissingFields(t *testing.T) {
	fakeClaude(t, `case "$*" in
*"--resume s1"*"--model sonnet"*) echo '{"type":"result","result":"","structured_output":{"summary":"asked"},"session_id":"s1","total_cost_usd":0.5}' ;;
*"--model sonnet"*) echo '{"type":"result","result":"I did it.","session_id":"s1","total_cost_usd":1}' ;;
esac
`)
	lib := sequenceLibrary(t)
	lib.Settings.Agents["sonnet"] = AgentCLI{Runs: "claude", Args: []string{"--model", "sonnet"}}
	run, folder := execute(t, lib, agentChain("sonnet"))

	step := run.Steps[0]
	if step.Status != "done" || step.Output["summary"] != "asked" {
		t.Fatalf("step = %s %v (%s), want done with the asked-for summary", step.Status, step.Output, step.Error)
	}
	if step.AskedAgain != "do-fields" || step.Cost != 1.5 {
		t.Errorf("asked again = %q, cost = %v", step.AskedAgain, step.Cost)
	}
	raw, err := os.ReadFile(filepath.Join(folder, "do-fields.call.json"))
	if err != nil || !strings.Contains(string(raw), "did not include the fields this step needs: summary") || !strings.Contains(string(raw), "--json-schema") {
		t.Errorf("retry call = %s, %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(folder, "do.call.json")); err != nil {
		t.Errorf("first call log: %v", err)
	}
}

func TestAgentFailsWhenAskingAgainStillGivesNoFields(t *testing.T) {
	fakeClaude(t, `echo '{"type":"result","result":"no fields here","session_id":"s1"}'`)
	run, _ := execute(t, sequenceLibrary(t), agentChain(""))

	step := run.Steps[0]
	if step.Status != "failed" || step.AskedAgain != "do-fields" || !strings.Contains(step.Error, "did not return the fields") {
		t.Errorf("step = %s, asked again %q, error %q", step.Status, step.AskedAgain, step.Error)
	}
}

func TestCustomAgentFieldsInACodeFence(t *testing.T) {
	lib := sequenceLibrary(t)
	lib.Settings.Agents["fenced"] = AgentCLI{Command: "printf 'Done.\\n\\n```json\\n{\"summary\": \"fenced\"}\\n```\\n'"}
	run, _ := execute(t, lib, agentChain("fenced"))

	step := run.Steps[0]
	if step.Status != "done" || step.Output["summary"] != "fenced" || step.AskedAgain != "" {
		t.Errorf("step = %s %v, asked again %q (%s)", step.Status, step.Output, step.AskedAgain, step.Error)
	}
}
