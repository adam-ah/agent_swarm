package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeCodex(t *testing.T, script string) string {
	t.Helper()
	bin, record := t.TempDir(), t.TempDir()
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + record + "/args\ncat > " + record + "/stdin\n" + script
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return record
}

func recorded(t *testing.T, record, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(record, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestCodexAgentStreamsItsEvents(t *testing.T) {
	record := fakeCodex(t, `cat <<'EOF'
{"type":"thread.started","thread_id":"t1"}
{"type":"item.started","item":{"id":"i1","type":"command_execution","command":"ls"}}
{"type":"item.completed","item":{"id":"i1","type":"command_execution","command":"ls","aggregated_output":"a.go","exit_code":0,"status":"completed"}}
{"type":"item.started","item":{"id":"r1","type":"reasoning"}}
not json
{"type":"item.completed","item":{"id":"m1","type":"agent_message","text":"{\"summary\": \"codex did it\"}"}}
{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":5}}
EOF
`)
	run, folder := execute(t, sequenceLibrary(t), agentChain("codex"))
	step := run.Steps[0]
	if step.Status != "done" || step.Output["summary"] != "codex did it" || step.Headline != "codex did it" {
		t.Fatalf("step = %s %v (%s)", step.Status, step.Output, step.Error)
	}
	if step.Session != "t1" || step.Tokens != 15 || step.Actions != 1 || step.Runner != "codex" {
		t.Errorf("session %q, tokens %d, actions %d, runner %q", step.Session, step.Tokens, step.Actions, step.Runner)
	}
	if args := recorded(t, record, "args"); !strings.Contains(args, "--output-schema "+filepath.Join(folder, "do.schema.json")) {
		t.Errorf("args = %q", args)
	}
	if stdin := recorded(t, record, "stdin"); !strings.HasPrefix(stdin, "Do it.") {
		t.Errorf("prompt = %q", stdin)
	}
	for _, name := range []string{"do.schema.json", "do.call.json", "do.jsonl"} {
		if _, err := os.Stat(filepath.Join(folder, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestCodexAgentFailures(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		{"turn failed", `echo '{"type":"turn.failed","error":{"message":"quota"}}'`, "codex reported an error: quota"},
		{"error event", `echo '{"type":"error","message":"boom"}'`, "codex reported an error: boom"},
		{"no answer", "echo crashed >&2; exit 3", "codex ended without a result: crashed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeCodex(t, tt.script)
			run, _ := execute(t, sequenceLibrary(t), agentChain("codex"))
			if step := run.Steps[0]; step.Status != "failed" || step.Error != tt.want {
				t.Errorf("step = %s %q, want %q", step.Status, step.Error, tt.want)
			}
		})
	}
}

func TestClaudeAgentCountsToolCalls(t *testing.T) {
	fakeClaude(t, `printf '%s\n' "$*" > "$SWARM_PROJECT/args"
cat <<'EOF'
{"type":"assistant","message":{"content":[{"type":"text"},{"type":"tool_use","id":"u1","name":"Bash","input":{"command":"ls"}},{"type":"tool_use","id":"u2","name":"Read","input":{}}]}}
{"type":"user","message":{"content":[{"type":"text"},{"type":"tool_result","tool_use_id":"u1","content":"a.go","is_error":false}]}}
not json
{"type":"result","result":"All done.","structured_output":{"summary":null},"session_id":"c1","total_cost_usd":0.25}
EOF
`)
	lib := sequenceLibrary(t)
	chain := agentChain("")
	chain.Steps[0].Tools = []string{"Bash", "Read"}
	run, _ := execute(t, lib, chain)
	step := run.Steps[0]
	if step.Status != "done" || step.Output["summary"] != "" || step.Headline != "All done." {
		t.Fatalf("step = %s %v %q (%s)", step.Status, step.Output, step.Headline, step.Error)
	}
	if step.Actions != 2 || step.Cost != 0.25 || run.Cost != 0.25 || step.Session != "c1" {
		t.Errorf("actions %d, cost %v/%v, session %q", step.Actions, step.Cost, run.Cost, step.Session)
	}
	if args, _ := os.ReadFile(filepath.Join(lib.Project, "args")); !strings.Contains(string(args), "--allowedTools Bash,Read") {
		t.Errorf("args = %q", args)
	}
}

func TestClaudeAgentFailures(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		{"reported error", `echo '{"type":"result","is_error":true,"result":"rate limited"}'`, "claude reported an error: rate limited"},
		{"no result", "echo oops >&2; exit 3", "claude ended without a result: oops"},
		{"fails when asked again", `case "$*" in
*"--resume s1"*) echo again >&2; exit 1 ;;
*) echo '{"type":"result","result":"no fields","session_id":"s1"}' ;;
esac`, "claude ended without a result: again"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClaude(t, tt.script)
			run, _ := execute(t, sequenceLibrary(t), agentChain(""))
			if step := run.Steps[0]; step.Status != "failed" || step.Error != tt.want {
				t.Errorf("step = %s %q, want %q", step.Status, step.Error, tt.want)
			}
		})
	}
}

func TestAgentSentBackGetsTheNoteInItsOwnSession(t *testing.T) {
	fakeClaude(t, `printf '%s\n' "$*" >> "$SWARM_PROJECT/args"
echo '{"type":"result","result":"Changed.","session_id":"s1"}'
`)
	lib := sequenceLibrary(t)
	chain := &Chain{Name: "main", Steps: []Step{
		{ID: "do", Title: "Do it", Agent: "do"},
		{ID: "check", Title: "Check", User: "Good?", Revise: "do"},
	}, Prompts: map[string]Prompt{"do": {Text: "Do it."}}}
	run, _ := execute(t, lib, chain, "revise:smaller", "approve")
	if run.Status != "done" || run.Steps[0].Round != 1 || run.Steps[0].Resumed != "Do it" {
		t.Fatalf("run = %s (%s), do = %+v", run.Status, run.Reason, run.Steps[0])
	}
	args, _ := os.ReadFile(filepath.Join(lib.Project, "args"))
	calls := strings.Split(strings.TrimSpace(string(args)), "\n")
	if len(calls) < 2 || !strings.Contains(calls[len(calls)-1], "--resume s1") || !strings.Contains(string(args), "A person sent your last answer back") {
		t.Errorf("calls = %q", calls)
	}
}

func TestWithFeedbackAddsTheNotes(t *testing.T) {
	sc := newScope(map[string]string{"feedback": "smaller"})
	if got := withFeedback("Do it.\n", "Do it.\n", sc); !strings.HasSuffix(got, "## Notes from the person\n\nA person sent an earlier attempt back with these notes, oldest first. Follow them.\n\nsmaller\n") {
		t.Errorf("withFeedback = %q", got)
	}
	if got := withFeedback("Use smaller", "Use {{feedback}}", sc); got != "Use smaller" {
		t.Errorf("withFeedback with the placeholder = %q", got)
	}
	if got := withFeedback("Do it.", "Do it.", newScope(map[string]string{})); got != "Do it." {
		t.Errorf("withFeedback without feedback = %q", got)
	}
}

func TestAgentStepRefusals(t *testing.T) {
	lib := sequenceLibrary(t)
	lib.Settings.Agents["broken"] = AgentCLI{Command: "echo nope >&2; exit 2"}
	tests := []struct {
		name string
		step Step
		want string
	}{
		{"nothing to resume", Step{ID: "more", Title: "More", Agent: "do", Resume: "missing"}, "nothing to resume: step missing has not run yet in this run"},
		{"custom command fails", Step{ID: "do", Title: "Do", Agent: "do", Runner: "broken"}, "broken failed: nope"},
		{"unknown runner", Step{ID: "do", Title: "Do", Agent: "do", Runner: "ghost"}, `no agent named "ghost"; use claude, codex, or add it under agents in settings`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain := &Chain{Name: "main", Steps: []Step{tt.step}, Prompts: map[string]Prompt{"do": {Text: "Do it."}}}
			run, _ := execute(t, lib, chain)
			if step := run.Steps[0]; step.Status != "failed" || step.Error != tt.want {
				t.Errorf("step = %s %q, want %q", step.Status, step.Error, tt.want)
			}
		})
	}
}
