package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func askingChain(runner string) *Chain {
	chain := agentChain(runner)
	chain.Prompts["do"] = Prompt{Text: "Do it.", Returns: Fields{{Name: "summary", Value: "text"}, {Name: questionsField, Value: "list"}}}
	return chain
}

func callArgs(t *testing.T, project string) []string {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(project, "args"))
	return strings.Split(strings.TrimSpace(string(raw)), "\n---\n")
}

func TestAgentAsksUntilItHasItsAnswers(t *testing.T) {
	fakeClaude(t, `printf '%s\n---\n' "$*" >> "$SWARM_PROJECT/args"
case "$*" in
*"A: keep it"*) echo '{"type":"result","structured_output":{"summary":"Painted it blue","questions":[]},"session_id":"s1"}' ;;
*"A: blue"*) echo '{"type":"result","structured_output":{"summary":"","questions":["Keep the old name?"]},"session_id":"s1"}' ;;
*) echo '{"type":"result","structured_output":{"summary":"","questions":["Which color?","How dark?"]},"session_id":"s1"}' ;;
esac
`)
	lib := sequenceLibrary(t)
	run, _ := executeWith(t, lib, askingChain(""), decision{Choice: "answer", Answers: []string{"blue", " "}}, decision{Choice: "answer", Answers: []string{"keep it"}})

	step := run.Steps[0]
	if step.Status != "done" || step.Headline != "Painted it blue" {
		t.Fatalf("step = %s %q (%s)", step.Status, step.Headline, step.Error)
	}
	if len(step.Asks) != 2 || step.Asks[0].Log != "do-ask-1" || step.Asks[1].Log != "do-ask-2" || step.Asks[0].Decided || step.Asks[0].AnsweredAt == nil {
		t.Fatalf("asks = %+v", step.Asks)
	}
	want := "Q: Which color?\nA: blue\n\nQ: How dark?\nA: " + notAnswered + "\n\nQ: Keep the old name?\nA: keep it"
	if step.Output[answersField] != want {
		t.Errorf("answers = %q, want %q", step.Output[answersField], want)
	}
	calls := callArgs(t, lib.Project)
	if len(calls) != 3 || !strings.Contains(calls[1], "--resume s1") || !strings.Contains(calls[1], "The person answered your questions:") || strings.Contains(calls[2], "Which color?") {
		t.Errorf("calls = %q", calls)
	}
}

func TestAgentLeftToDecideMustStopAsking(t *testing.T) {
	fakeClaude(t, `printf '%s\n---\n' "$*" >> "$SWARM_PROJECT/args"
echo '{"type":"result","structured_output":{"summary":"","questions":["Which color?"]},"session_id":"s1"}'
`)
	lib := sequenceLibrary(t)
	run, _ := executeWith(t, lib, askingChain(""), decision{Choice: "decide"})

	step := run.Steps[0]
	if step.Status != "failed" || step.Error != "it asked 1 more questions after you told it to decide for itself" || !step.Asks[0].Decided {
		t.Errorf("step = %s %q, asks %+v", step.Status, step.Error, step.Asks)
	}
	if calls := callArgs(t, lib.Project); len(calls) != 2 || !strings.Contains(calls[1], decideYourself[:40]) {
		t.Errorf("calls = %q", calls)
	}
}

func TestAgentThatCannotResumeGetsThePromptAgainWithTheAnswers(t *testing.T) {
	lib := sequenceLibrary(t)
	lib.Settings.Agents["plain"] = AgentCLI{Command: `if grep -q 'A: blue'; then echo '{"summary":"blue it is","questions":[]}'; else echo '{"summary":"","questions":["Which color?"]}'; fi`}
	run, _ := executeWith(t, lib, askingChain("plain"), decision{Choice: "answer", Answers: []string{"blue"}})

	step := run.Steps[0]
	if step.Status != "done" || step.Output["summary"] != "blue it is" || step.Output[answersField] != "Q: Which color?\nA: blue" {
		t.Errorf("step = %s %v (%s)", step.Status, step.Output, step.Error)
	}
}

func TestAgentThatDoesNotAskHasEmptyAnswers(t *testing.T) {
	fakeClaude(t, `echo '{"type":"result","structured_output":{"summary":"Done","questions":[]},"session_id":"s1"}'`)
	run, _ := execute(t, sequenceLibrary(t), askingChain(""))
	if step := run.Steps[0]; step.Status != "done" || step.Output[answersField] != "" || len(step.Asks) != 0 {
		t.Errorf("step = %s %v, asks %v", step.Status, step.Output, step.Asks)
	}
}

func TestAskingStopsWithTheRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := openStore(filepath.Join(t.TempDir(), "runs"))
	id, _ := store.claim()
	sr := &StepRun{ID: "do"}
	run := &Run{ID: id, Status: "running", Steps: []*StepRun{sr}, decisions: make(chan decision)}
	_, err := (&Runner{store: store}).askPerson(ctx, run, sr, []string{"Which?"})
	if err == nil || err.Error() != "stopped" || sr.Status != "waiting" || sr.openAsk() == nil {
		t.Errorf("err = %v, step = %s, open = %v", err, sr.Status, sr.openAsk())
	}
}

func TestDecisionCheck(t *testing.T) {
	asking := &StepRun{Title: "Plan", Kind: "agent", Asks: []*Ask{{Questions: []string{"A?", "B?"}}}}
	user := &StepRun{Title: "Check", Kind: "user", Revise: "Plan"}
	tests := []struct {
		step *StepRun
		d    decision
		want string
	}{
		{asking, decision{Choice: "answer", Answers: []string{"a", "b"}}, ""},
		{asking, decision{Choice: "decide"}, ""},
		{asking, decision{Choice: "approve"}, `"Plan" is asking you questions; answer them or let it decide`},
		{asking, decision{Choice: "answer", Answers: []string{"a"}}, "it asked 2 questions and got 1 answers"},
		{user, decision{Choice: "answer", Answers: []string{"a"}}, `"Check" is not asking questions; approve, revise or reject it`},
		{user, decision{Choice: "decide"}, `"Check" is not asking questions`},
		{user, decision{Choice: "revise", Note: "smaller"}, ""},
	}
	for _, tt := range tests {
		err := tt.d.check(tt.step)
		switch {
		case err == nil && tt.want != "":
			t.Errorf("check(%+v) passed, want %q", tt.d, tt.want)
		case err != nil && (tt.want == "" || !strings.Contains(err.Error(), tt.want)):
			t.Errorf("check(%+v) = %q, want %q", tt.d, err, tt.want)
		}
	}
	now := time.Now()
	asking.Asks[0].AnsweredAt = &now
	if asking.openAsk() != nil {
		t.Error("an answered ask is still open")
	}
}

func TestDecideWritesTheAnswers(t *testing.T) {
	core := openCore(t.TempDir())
	id := fakeRunAt(t, core, "waiting", "")
	run := core.store.load(id)
	core.store.update(run, func() { run.Steps[0].Kind, run.Steps[0].Asks = "agent", []*Ask{{Questions: []string{"Which?"}}} })
	if err := core.Decide(id, decision{Choice: "answer", Answers: []string{"that one"}}); err != nil {
		t.Fatal(err)
	}
	var got decision
	if err := readJSON(filepath.Join(core.store.runDir(id), decisionFile), &got); err != nil || !reflect.DeepEqual(got.Answers, []string{"that one"}) {
		t.Errorf("decision file = %+v, %v", got, err)
	}
}

func TestQuestionsMustBeAList(t *testing.T) {
	tests := map[string][]string{
		"## Return\n\n- `summary`: s\n- `questions` (list): what to ask": nil,
		"## Return\n\n- `questions`: what to ask":                        {"## Return: questions must be a list: - `questions` (list): what you need the person to answer"},
		"## Return\n\n- `questions` (list): q\n- `answers`: a":           {"## Return: answers is filled by swarm with the person's answers to questions; take it out"},
		"## Return\n\n- `answers`: a":                                    nil,
	}
	for text, want := range tests {
		if got := newPrompt(text, "").Problems; !reflect.DeepEqual(got, want) {
			t.Errorf("problems of %q = %q, want %q", text, got, want)
		}
	}
}

func TestAnswersArePartOfWhatAnAskingStepReturns(t *testing.T) {
	lib := sequenceLibrary(t)
	chain := askingChain("")
	chain.Steps = append(chain.Steps, Step{ID: "after", Title: "After", Tool: "say", With: map[string]string{"value": "{{do.answers}}"}})
	lib.Chains["main"] = chain
	if warnings := lib.warnings(chain); len(warnings) != 0 {
		t.Errorf("warnings = %q", warnings)
	}
}

func TestAskLine(t *testing.T) {
	step := &StepRun{Title: "Plan", Status: "waiting", Asks: []*Ask{{Questions: []string{"Which?", "When?"}}}}
	want := "? Plan asks:\n  1. Which?\n  2. When?\n  answer with swarm answer RUN -a TEXT (once per question, in order) / swarm answer RUN -decide (it chooses), or on the page: http://x\n"
	if got := stepLine(step, "", "http://x"); got != want {
		t.Errorf("stepLine = %q, want %q", got, want)
	}
}

func TestRunSummaryShowsTheOpenQuestions(t *testing.T) {
	run := &Run{Status: "waiting", Steps: []*StepRun{{Title: "Plan", Status: "waiting", Asks: []*Ask{{Questions: []string{"Which?", "When?"}}}}}}
	if got := summaryOf(run).Question; got != "Plan asks: Which? · When?" {
		t.Errorf("question = %q", got)
	}
}

func TestDryRunAgentAsksAndAnswersWithItsNextMock(t *testing.T) {
	project := flowProject(t)
	surfaceFiles(t, project, map[string]string{
		".swarm/chains/asking.json":    `{"title": "Asking", "steps": [{"id": "plan", "title": "Plan", "agent": "plan"}, {"id": "use", "title": "Use", "tool": "ship", "with": {"what": "{{plan.answers}}"}}]}`,
		".swarm/chains/asking/plan.md": "Plan it.\n\n## Return\n\n- `summary`: one line\n- `questions` (list): what you need to know\n",
	})
	out := dryRun(t, "asking", "-mock", `{"plan": [{"summary": "", "questions": ["Which color?"]}, {"summary": "blue"}]}`)
	surfaceContains(t, out, nil, "summary=blue", "! asks Which color?, and answers with its next mock", "<answer 1>", "the run ended done")

	out = dryRun(t, "asking", "-mock", `{"plan": {"summary": "", "questions": ["Again?"]}}`)
	if strings.Count(out, "asks Again?") != 1 || !strings.Contains(out, "the run ended done") {
		t.Errorf("a single mock that asks:\n%s", out)
	}

	fakeClaude(t, `echo '{"type":"result","structured_output":{"summary":"","questions":["Which color?"]},"session_id":"s1"}'`)
	out = dryRun(t, "asking", "-run", "plan")
	surfaceContains(t, out, nil, "nobody answers in a dry run, so it was told to decide", "after you told it to decide for itself")
}

func TestCLIAnswer(t *testing.T) {
	core, project := surfaceProject(t)
	run := surfaceRun(t, core, project, func(run *Run) {
		run.Status = "waiting"
		run.Steps[0].Kind, run.Steps[0].Status, run.Steps[0].Asks = "agent", "waiting", []*Ask{{Questions: []string{"Which?", "When?"}}}
	})
	out, err := surfaceCLI(t, "show", run.ID)
	surfaceContains(t, out, err, "? Greet asks:\n  1. Which?\n  2. When?\n")
	_, err = surfaceCLI(t, "answer", run.ID, "-a", "this")
	surfaceFails(t, err, "it asked 2 questions and got 1 answers")
	out, err = surfaceCLI(t, "answer", run.ID, "-a", "this", "-a", "now")
	surfaceContains(t, out, err, "answered "+run.ID)
	file := filepath.Join(core.store.runDir(run.ID), decisionFile)
	var got decision
	if readErr := readJSON(file, &got); readErr != nil || !reflect.DeepEqual(got, decision{Choice: "answer", Answers: []string{"this", "now"}}) {
		t.Errorf("decision = %+v, %v", got, readErr)
	}
	_ = os.Remove(file)
	out, err = surfaceCLI(t, "answer", run.ID, "-decide")
	surfaceContains(t, out, err, "left to the agent: "+run.ID)
}
