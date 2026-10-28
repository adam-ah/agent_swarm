package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

func (r *Runner) callAgent(ctx context.Context, call agentCall, builtin string, sc *scope) (agentResult, error) {
	transcript, err := os.Create(r.logFile(call.run, call.log, ".jsonl"))
	if err != nil {
		return agentResult{}, err
	}
	defer func() { _ = transcript.Close() }()
	call.transcript = transcript

	var result agentResult
	switch {
	case builtin == "claude":
		result, err = r.runClaude(ctx, call)
	case builtin == "codex":
		result, err = r.runCodex(ctx, call)
	case call.cli.Command != "":
		result, err = r.runCustom(ctx, call)
	default:
		err = fmt.Errorf("no agent named %q; use claude, codex, or add it under agents in settings", call.sr.Runner)
	}
	r.store.update(call.run, func() {
		call.sr.Cost += result.cost
		call.sr.Tokens += result.tokens
		call.sr.Session = result.session
		call.run.Cost += result.cost
	})
	if result.session != "" {
		sc.keepSession(call.step, agentSession{id: result.session, runner: call.sr.Runner, title: call.step.Title})
	}
	if ctx.Err() != nil {
		return result, errors.New("stopped")
	}
	return result, err
}

func (a agentResult) fields(returns Fields) map[string]any {
	if a.structured != nil {
		return a.structured
	}
	return objectWithFields(a.text, returns)
}

func fieldsPrompt(returns Fields) string {
	names := make([]string, len(returns))
	for i, f := range returns {
		names[i] = f.Name
	}
	return "Your answer did not include the fields this step needs: " + strings.Join(names, ", ") +
		". Change nothing else. Reply with one JSON object holding those fields, filled in from the work you just did.\n"
}

func objectWithFields(text string, fields Fields) map[string]any {
	for i := strings.LastIndex(text, "{"); i >= 0; i = strings.LastIndex(text[:i], "{") {
		var object map[string]any
		if json.NewDecoder(strings.NewReader(text[i:])).Decode(&object) == nil && hasFields(object, fields) {
			return object
		}
	}
	return nil
}

func hasFields(object map[string]any, fields Fields) bool {
	for _, f := range fields {
		if _, ok := object[f.Name]; !ok {
			return false
		}
	}
	return true
}
