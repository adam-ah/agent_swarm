package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"
)

type agentResult struct {
	structured map[string]any
	text       string
	cost       float64
	tokens     int
	session    string
}

type agentCall struct {
	run        *Run
	sr         *StepRun
	step       Step
	prompt     string
	schema     []byte
	session    string
	log        string
	transcript io.Writer
	cli        AgentCLI
	limits     limits
}

func (r *Runner) runAgent(ctx context.Context, lib *Library, chain *Chain, run *Run, sr *StepRun, step Step, sc *scope) (map[string]any, error) {
	runner := lib.Settings.runnerFor(step)
	resume, followUp, err := resumePoint(step, sr, sc, runner, lib.Settings)
	if err != nil {
		return nil, err
	}
	r.store.update(run, func() { sr.Runner, sr.Resumed = runner, resume.title })

	prompt := chain.Prompts[step.Agent]
	call := agentCall{
		run: run, sr: sr, step: step, log: sr.Log, cli: lib.Settings.Agents[runner], session: resume.id,
		prompt: withFeedback(stepScope(step, sc, nil).fill(prompt.Text, false), prompt.Text, sc),
	}
	if followUp != "" {
		call.prompt = followUp
	}
	if len(prompt.Returns) > 0 {
		call.schema, err = json.Marshal(returnsSchema(prompt.Returns))
		if err != nil {
			return nil, err
		}
	}
	builtin := lib.Settings.builtinFor(runner)
	call.limits = limitsFor(step, agentStall, builtin == "")
	result, err := r.callAgent(ctx, call, builtin, sc)
	if err == nil && len(prompt.Returns) > 0 && result.fields(prompt.Returns) == nil && builtin != "" && result.session != "" {
		call.log, call.session, call.prompt = sr.Log+"-fields", result.session, fieldsPrompt(prompt.Returns)
		r.store.update(run, func() { sr.AskedAgain = call.log })
		result, err = r.callAgent(ctx, call, builtin, sc)
	}
	if err != nil {
		return nil, err
	}

	out := map[string]any{}
	if len(prompt.Returns) > 0 {
		fields := result.fields(prompt.Returns)
		if fields == nil {
			return nil, fmt.Errorf("%s did not return the fields in chains/%s/%s.md", runner, chain.Name, step.Agent)
		}
		for _, f := range prompt.Returns {
			out[f.Name] = fields[f.Name]
			if out[f.Name] == nil {
				out[f.Name] = ""
			}
		}
	} else {
		out["output"] = result.text
	}
	headline := text(out["summary"])
	if headline == "" {
		headline = firstLine(result.text)
	}
	r.store.update(run, func() { sr.Headline = headline })
	return out, nil
}

func withFeedback(filled, template string, sc *scope) string {
	feedback, _ := sc.lookup("feedback")
	if feedback == "" || slices.Contains(placeholders(template), "feedback") {
		return filled
	}
	return strings.TrimRight(filled, "\n") + "\n\n## Notes from the person\n\nA person sent an earlier attempt back with these notes, oldest first. Follow them.\n\n" + feedback + "\n"
}

func (r *Runner) stream(parent context.Context, call agentCall, cmd *exec.Cmd, stdin string, onLine func([]byte, *callLog) bool) (string, error) {
	cmd.Dir = call.run.Project
	if cmd.Env == nil {
		cmd.Env = r.environment(call.run)
	}
	mark := r.stepMark(call.run, call.log)
	cmd.Env = append(cmd.Env, stepVar+mark)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	output, input, err := os.Pipe()
	if err != nil {
		return "", err
	}
	defer func() { _ = output.Close() }()
	log := startCall(r.logFile(call.run, call.log, ".call.json"), cmd, stdin, true)
	g, ctx := r.guard(parent, mark, call.limits, log)
	stderr := &capture{g: g}
	cmd.Stdout, cmd.Stderr, cmd.WaitDelay = input, stderr, exitGrace
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	err = r.launch(cmd)
	_ = input.Close()
	if err != nil {
		g.finish("")
		log.finish(cmd, err, "", "")
		return "", err
	}
	done := make(chan error, 1)
	grace := exitGrace
	go func() {
		done <- r.wait(ctx, cmd)
		time.AfterFunc(grace, func() {
			g.stop("")
			_ = output.Close()
		})
	}()
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, min(64*1024, longestLine)), longestLine)
	answered := false
	for scanner.Scan() {
		g.progress()
		line := scanner.Bytes()
		_, _ = call.transcript.Write(append(append([]byte{}, line...), '\n'))
		if onLine(line, log) && !answered {
			answered = true
			g.stopAfter(answerGrace)
		}
	}
	if scanErr := scanner.Err(); scanErr != nil && !errors.Is(scanErr, os.ErrClosed) {
		g.stop("swarm could not read its output (" + scanErr.Error() + ")")
	}
	err = <-done
	why := g.finish("")
	log.finish(cmd, err, "", stderr.String())
	if why != "" {
		return "", fmt.Errorf("%s, so swarm stopped it; its last action was %s", why, log.lastAction())
	}
	return lastLines(stderr.String(), 3), err
}

func (r *Runner) countActions(call agentCall, n int) {
	if n > 0 {
		r.store.update(call.run, func() { call.sr.Actions += n })
	}
}

func claudeArgs(call agentCall) []string {
	args := []string{"-p", call.prompt, "--output-format", "stream-json", "--verbose"}
	if call.session != "" {
		args = append(args, "--resume", call.session)
	}
	if call.schema != nil {
		args = append(args, "--json-schema", string(call.schema))
	}
	args = append(args, call.cli.Args...)
	if len(call.step.Tools) > 0 {
		args = append(args, "--allowedTools", strings.Join(call.step.Tools, ","))
	}
	return args
}

func (r *Runner) runClaude(ctx context.Context, call agentCall) (agentResult, error) {
	var final struct {
		IsError    bool           `json:"is_error"`
		Result     string         `json:"result"`
		Structured map[string]any `json:"structured_output"`
		Cost       float64        `json:"total_cost_usd"`
		SessionID  string         `json:"session_id"`
	}
	got := false
	stderr, runErr := r.stream(ctx, call, exec.Command("claude", claudeArgs(call)...), "", func(line []byte, log *callLog) bool {
		var event struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type      string          `json:"type"`
					ID        string          `json:"id"`
					Name      string          `json:"name"`
					Input     json.RawMessage `json:"input"`
					ToolUseID string          `json:"tool_use_id"`
					Content   json.RawMessage `json:"content"`
					IsError   bool            `json:"is_error"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &event) != nil {
			return false
		}
		switch event.Type {
		case "assistant":
			calls := 0
			for _, c := range event.Message.Content {
				if c.Type == "tool_use" {
					calls++
					log.begin(c.ID, c.Name, prettyRaw(c.Input))
				}
			}
			r.countActions(call, calls)
		case "user":
			for _, c := range event.Message.Content {
				if c.Type == "tool_result" {
					log.end(c.ToolUseID, "", "", rawText(c.Content), c.IsError)
				}
			}
		case "result":
			if answersTaskNotice(line) {
				return false
			}
			if !got {
				got = json.Unmarshal(line, &final) == nil
				log.edit(func(c *Call) { c.Result = prettyRaw(line) })
			}
			return true
		}
		return false
	})
	result := agentResult{structured: final.Structured, text: final.Result, cost: final.Cost, session: final.SessionID}
	switch {
	case !got:
		return result, fmt.Errorf("claude ended without a result: %s", orError(stderr, runErr))
	case final.IsError:
		return result, fmt.Errorf("claude reported an error: %s", lastLines(final.Result, 3))
	}
	return result, nil
}

func answersTaskNotice(line []byte) bool {
	var result struct {
		Origin struct {
			Kind string `json:"kind"`
		} `json:"origin"`
	}
	return json.Unmarshal(line, &result) == nil && result.Origin.Kind == "task-notification"
}

func codexArgs(call agentCall, schemaFile string) []string {
	args := []string{"exec"}
	if call.session != "" {
		args = append(args, "resume")
	}
	args = append(args, "--json", "--skip-git-repo-check")
	if schemaFile != "" {
		args = append(args, "--output-schema", schemaFile)
	}
	args = append(args, call.cli.Args...)
	if call.session != "" {
		args = append(args, call.session)
	}
	return append(args, "-")
}

func (r *Runner) runCodex(ctx context.Context, call agentCall) (agentResult, error) {
	schemaFile := ""
	if call.schema != nil {
		schemaFile = r.logFile(call.run, call.log, ".schema.json")
		if err := os.WriteFile(schemaFile, call.schema, 0o644); err != nil {
			return agentResult{}, err
		}
	}

	var result agentResult
	var failure string
	stderr, runErr := r.stream(ctx, call, exec.Command("codex", codexArgs(call, schemaFile)...), call.prompt, func(line []byte, log *callLog) bool {
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
			Message  string `json:"message"`
			Error    struct {
				Message string `json:"message"`
			} `json:"error"`
			Item  json.RawMessage `json:"item"`
			Usage struct {
				Input  int `json:"input_tokens"`
				Output int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(line, &event) != nil {
			return false
		}
		var item codexItem
		_ = json.Unmarshal(event.Item, &item)
		action := item.Type != "agent_message" && item.Type != "reasoning"
		switch event.Type {
		case "thread.started":
			result.session = event.ThreadID
		case "item.started":
			if action {
				r.countActions(call, 1)
				log.begin(item.ID, item.Type, item.input(event.Item))
			}
		case "item.completed":
			if item.Type == "agent_message" {
				result.text = item.Text
				log.edit(func(c *Call) { c.Result = item.Text })
			} else if action {
				output, failed := item.output(event.Item)
				log.end(item.ID, item.Type, item.input(event.Item), output, failed)
			}
		case "turn.completed":
			result.tokens += event.Usage.Input + event.Usage.Output
			return true
		case "turn.failed", "error":
			failure = event.Message + event.Error.Message
			return event.Type == "turn.failed"
		}
		return false
	})
	if failure != "" {
		return result, fmt.Errorf("codex reported an error: %s", failure)
	}
	if runErr != nil && result.text == "" {
		return result, fmt.Errorf("codex ended without a result: %s", orError(stderr, runErr))
	}
	return result, nil
}

func (r *Runner) runCustom(ctx context.Context, call agentCall) (agentResult, error) {
	prompt := call.prompt
	if call.schema != nil {
		prompt += "\n\nWhen you are done, print one JSON object as the last thing you output, matching this JSON Schema:\n" + string(call.schema) + "\n"
	}
	promptFile := r.logFile(call.run, call.log, ".prompt.txt")
	if err := os.WriteFile(promptFile, []byte(prompt), 0o644); err != nil {
		return agentResult{}, err
	}
	command := strings.Join(append([]string{call.cli.Command}, call.cli.Args...), " ")
	cmd := exec.Command("bash", "-c", command)
	cmd.Env = append(r.environment(call.run), "SWARM_PROMPT_FILE="+promptFile)
	var output strings.Builder
	stderr, runErr := r.stream(ctx, call, cmd, prompt, func(line []byte, _ *callLog) bool {
		output.Write(line)
		output.WriteByte('\n')
		return false
	})
	result := agentResult{text: strings.TrimSpace(output.String())}
	if runErr != nil {
		return result, fmt.Errorf("%s failed: %s", call.sr.Runner, orError(stderr, runErr))
	}
	return result, nil
}

type codexItem struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Text     string `json:"text"`
	Command  string `json:"command"`
	Output   string `json:"aggregated_output"`
	ExitCode *int   `json:"exit_code"`
	Status   string `json:"status"`
}

func (item codexItem) input(raw json.RawMessage) string {
	if item.Command != "" {
		return item.Command
	}
	return prettyRaw(raw)
}

func (item codexItem) output(raw json.RawMessage) (string, bool) {
	failed := item.Status == "failed" || (item.ExitCode != nil && *item.ExitCode != 0)
	if item.Command != "" {
		return item.Output, failed
	}
	return prettyRaw(raw), failed
}

func orError(stderr string, err error) string {
	if stderr != "" {
		return stderr
	}
	if err != nil {
		return err.Error()
	}
	return "no output"
}
