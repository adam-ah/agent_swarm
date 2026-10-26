package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
)

type agentResult struct {
	structured map[string]any
	text       string
	cost       float64
	tokens     int
}

type agentCall struct {
	run        *Run
	sr         *StepRun
	step       Step
	prompt     string
	schema     []byte
	transcript io.Writer
	cli        AgentCLI
}

func (r *Runner) runAgent(ctx context.Context, lib *Library, run *Run, sr *StepRun, step Step, sc *scope) (map[string]string, error) {
	runner := lib.Settings.runnerFor(step)
	r.store.update(run, func() { sr.Runner = runner })
	transcript, err := os.Create(r.logFile(run, sr, ".jsonl"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = transcript.Close() }()

	prompt := lib.Prompts[step.Agent]
	call := agentCall{
		run: run, sr: sr, step: step, transcript: transcript, cli: lib.Settings.Agents[runner],
		prompt: withFeedback(stepScope(step, sc, nil).fill(prompt.Text, false), prompt.Text, sc),
	}
	if len(prompt.Returns) > 0 {
		call.schema, err = json.Marshal(returnsSchema(prompt.Returns))
		if err != nil {
			return nil, err
		}
	}
	var result agentResult
	switch {
	case runner == "claude":
		result, err = r.runClaude(ctx, call)
	case runner == "codex":
		result, err = r.runCodex(ctx, call)
	case call.cli.Command != "":
		result, err = r.runCustom(ctx, call)
	default:
		err = fmt.Errorf("no agent named %q; use claude, codex, or add it under agents in settings", runner)
	}
	r.store.update(run, func() {
		sr.Cost += result.cost
		sr.Tokens += result.tokens
		run.Cost += result.cost
	})
	if ctx.Err() != nil {
		return nil, errors.New("stopped")
	}
	if err != nil {
		return nil, err
	}

	out := map[string]string{}
	if len(prompt.Returns) > 0 {
		if result.structured == nil {
			return nil, fmt.Errorf("%s did not return the fields in prompts/%s.md", runner, step.Agent)
		}
		for _, f := range prompt.Returns {
			out[f.Name] = text(result.structured[f.Name])
		}
	} else {
		out["output"] = result.text
	}
	headline := out["summary"]
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

func (r *Runner) stream(ctx context.Context, call agentCall, cmd *exec.Cmd, stdin string, onLine func([]byte, *callLog)) (string, error) {
	cmd.Dir = call.run.Project
	if cmd.Env == nil {
		cmd.Env = r.environment(call.run)
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	log := startCall(r.logFile(call.run, call.sr, ".call.json"), cmd, stdin, true)
	done := make(chan error, 1)
	go func() { done <- runGroup(ctx, cmd) }()
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		_, _ = call.transcript.Write(append(append([]byte{}, line...), '\n'))
		onLine(line, log)
	}
	err = <-done
	log.finish(cmd, err, "", stderr.String())
	return lastLines(stderr.String(), 3), err
}

func (r *Runner) countActions(call agentCall, n int) {
	if n > 0 {
		r.store.update(call.run, func() { call.sr.Actions += n })
	}
}

func (r *Runner) runClaude(ctx context.Context, call agentCall) (agentResult, error) {
	args := []string{"-p", call.prompt, "--output-format", "stream-json", "--verbose"}
	if call.schema != nil {
		args = append(args, "--json-schema", string(call.schema))
	}
	args = append(args, call.cli.Args...)
	if len(call.step.Tools) > 0 {
		args = append(args, "--allowedTools", strings.Join(call.step.Tools, ","))
	}
	var final struct {
		IsError    bool           `json:"is_error"`
		Result     string         `json:"result"`
		Structured map[string]any `json:"structured_output"`
		Cost       float64        `json:"total_cost_usd"`
	}
	got := false
	stderr, runErr := r.stream(ctx, call, exec.Command("claude", args...), "", func(line []byte, log *callLog) {
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
			return
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
			got = json.Unmarshal(line, &final) == nil
			log.edit(func(c *Call) { c.Result = prettyRaw(line) })
		}
	})
	result := agentResult{structured: final.Structured, text: final.Result, cost: final.Cost}
	switch {
	case !got:
		return result, fmt.Errorf("claude ended without a result: %s", orError(stderr, runErr))
	case final.IsError:
		return result, fmt.Errorf("claude reported an error: %s", lastLines(final.Result, 3))
	}
	return result, nil
}

func (r *Runner) runCodex(ctx context.Context, call agentCall) (agentResult, error) {
	args := []string{"exec", "--json", "--skip-git-repo-check"}
	if call.schema != nil {
		schemaFile := r.logFile(call.run, call.sr, ".schema.json")
		if err := os.WriteFile(schemaFile, call.schema, 0o644); err != nil {
			return agentResult{}, err
		}
		args = append(args, "--output-schema", schemaFile)
	}
	args = append(append(args, call.cli.Args...), "-")

	var result agentResult
	var failure string
	stderr, runErr := r.stream(ctx, call, exec.Command("codex", args...), call.prompt, func(line []byte, log *callLog) {
		var event struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Error   struct {
				Message string `json:"message"`
			} `json:"error"`
			Item  json.RawMessage `json:"item"`
			Usage struct {
				Input  int `json:"input_tokens"`
				Output int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(line, &event) != nil {
			return
		}
		var item codexItem
		_ = json.Unmarshal(event.Item, &item)
		action := item.Type != "agent_message" && item.Type != "reasoning"
		switch event.Type {
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
		case "turn.failed", "error":
			failure = event.Message + event.Error.Message
		}
	})
	if failure != "" {
		return result, fmt.Errorf("codex reported an error: %s", failure)
	}
	if runErr != nil && result.text == "" {
		return result, fmt.Errorf("codex ended without a result: %s", orError(stderr, runErr))
	}
	if call.schema != nil {
		result.structured = lastJSONObject(result.text)
	}
	return result, nil
}

func (r *Runner) runCustom(ctx context.Context, call agentCall) (agentResult, error) {
	prompt := call.prompt
	if call.schema != nil {
		prompt += "\n\nWhen you are done, print one JSON object as the last thing you output, matching this JSON Schema:\n" + string(call.schema) + "\n"
	}
	promptFile := r.logFile(call.run, call.sr, ".prompt.txt")
	if err := os.WriteFile(promptFile, []byte(prompt), 0o644); err != nil {
		return agentResult{}, err
	}
	command := strings.Join(append([]string{call.cli.Command}, call.cli.Args...), " ")
	cmd := exec.Command("bash", "-c", command)
	cmd.Env = append(r.environment(call.run), "SWARM_PROMPT_FILE="+promptFile)
	var output strings.Builder
	stderr, runErr := r.stream(ctx, call, cmd, prompt, func(line []byte, _ *callLog) {
		output.Write(line)
		output.WriteByte('\n')
	})
	result := agentResult{text: strings.TrimSpace(output.String())}
	if runErr != nil {
		return result, fmt.Errorf("%s failed: %s", call.sr.Runner, orError(stderr, runErr))
	}
	if call.schema != nil {
		result.structured = lastJSONObject(result.text)
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

func lastJSONObject(text string) map[string]any {
	trimmed := strings.TrimSpace(text)
	for i := strings.LastIndex(trimmed, "{"); i >= 0; i = strings.LastIndex(trimmed[:i], "{") {
		var object map[string]any
		dec := json.NewDecoder(strings.NewReader(trimmed[i:]))
		if dec.Decode(&object) == nil && strings.TrimSpace(trimmed[i+int(dec.InputOffset()):]) == "" {
			return object
		}
	}
	return nil
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
