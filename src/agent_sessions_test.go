package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestResumePoint(t *testing.T) {
	sc := newScope(nil)
	settings := Settings{Agents: map[string]AgentCLI{"pi": {Command: "pi -p"}, "sonnet": {Runs: "claude"}}}
	change := Step{ID: "change", Agent: "change"}
	fix := Step{ID: "fix", Agent: "fix", Resume: "change"}

	if s, followUp, err := resumePoint(change, &StepRun{}, sc, "claude", settings); err != nil || s.id != "" || followUp != "" {
		t.Errorf("first run = %+v %q %v, want a new context", s, followUp, err)
	}
	if _, _, err := resumePoint(fix, &StepRun{}, sc, "claude", settings); err == nil || !strings.Contains(err.Error(), "has not run yet") {
		t.Errorf("resume before change ran: %v", err)
	}

	sc.keepSession(change, agentSession{id: "s1", runner: "claude", title: "Make the change"})
	if s, followUp, err := resumePoint(fix, &StepRun{}, sc, "claude", settings); err != nil || s.id != "s1" || followUp != "" {
		t.Errorf("fix = %+v %q %v, want s1 with its own prompt", s, followUp, err)
	}
	if _, _, err := resumePoint(fix, &StepRun{}, sc, "codex", settings); err == nil {
		t.Error("codex resumed a claude session")
	}

	sc.keepSession(fix, agentSession{id: "s2", runner: "claude", title: "Fix"})
	s, followUp, err := resumePoint(change, &StepRun{SentBack: "smaller, please"}, sc, "claude", settings)
	if err != nil || s.id != "s2" || !strings.Contains(followUp, "smaller, please") {
		t.Errorf("sent back = %+v %q %v, want the newest session s2 with the note", s, followUp, err)
	}
	if s, followUp, _ := resumePoint(change, &StepRun{SentBack: "again"}, sc, "pi", settings); s.id != "" || followUp != "" {
		t.Errorf("custom runner resumed: %+v %q", s, followUp)
	}

	sc.keepSession(change, agentSession{id: "s3", runner: "sonnet", title: "Make the change"})
	if s, _, err := resumePoint(fix, &StepRun{}, sc, "sonnet", settings); err != nil || s.id != "s3" {
		t.Errorf("agent that runs claude = %+v %v, want s3", s, err)
	}
}

func TestAgentArgsResume(t *testing.T) {
	call := agentCall{prompt: "go", session: "abc", cli: AgentCLI{Args: []string{"-m", "x"}}}
	if got, want := claudeArgs(call), []string{"-p", "go", "--output-format", "stream-json", "--verbose", "--resume", "abc", "-m", "x"}; !reflect.DeepEqual(got, want) {
		t.Errorf("claudeArgs = %q", got)
	}
	if got, want := codexArgs(call, "s.json"), []string{"exec", "resume", "--json", "--skip-git-repo-check", "--output-schema", "s.json", "-m", "x", "abc", "-"}; !reflect.DeepEqual(got, want) {
		t.Errorf("codexArgs = %q", got)
	}
	call.session = ""
	if got, want := codexArgs(call, ""), []string{"exec", "--json", "--skip-git-repo-check", "-m", "x", "-"}; !reflect.DeepEqual(got, want) {
		t.Errorf("codexArgs without a session = %q", got)
	}
}

func TestCheckResume(t *testing.T) {
	lib := testLibrary()
	agent := Step{ID: "a", Title: "A", Agent: "triage"}
	tests := []struct {
		name  string
		steps []Step
		want  string
	}{
		{"earlier agent", []Step{agent, {ID: "b", Title: "B", Agent: "triage", Resume: "a"}}, ""},
		{"later step", []Step{{ID: "b", Title: "B", Agent: "triage", Resume: "a"}, agent}, `step "b": resume must name an earlier agent step in this chain, not "a"`},
		{"not an agent", []Step{{ID: "t", Title: "T", Tool: "run_tests"}, {ID: "b", Title: "B", Agent: "triage", Resume: "t"}}, `step "b": resume must name an earlier agent step in this chain, not "t"`},
		{"other runner", []Step{agent, {ID: "b", Title: "B", Agent: "triage", Runner: "codex", Resume: "a"}}, `step "b": a runs with claude and this step with codex; a session resumes only on the agent that made it`},
		{"tool step", []Step{agent, {ID: "t", Title: "T", Tool: "run_tests", Resume: "a"}}, `step "t": resume only applies to agent steps`},
	}
	for _, tt := range tests {
		got := lib.check(&Chain{Name: "c", Steps: tt.steps, Prompts: testPrompts()})
		if (tt.want == "" && len(got) > 0) || (tt.want != "" && !reflect.DeepEqual(got, []string{tt.want})) {
			t.Errorf("%s: check = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestSendBackMarksTheStepWithTheNote(t *testing.T) {
	lib := sequenceLibrary(t)
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "list", Title: "List", Tool: "items"},
		{ID: "approve", Title: "Approve", User: "Fine?", Revise: "list"},
	}}
	run, _ := execute(t, lib, chain, "revise:shorter", "approve")
	if run.Status != "done" || run.Steps[0].SentBack != "shorter" || run.Steps[0].Round != 1 {
		t.Errorf("run %s, list step %+v", run.Status, run.Steps[0])
	}
}
