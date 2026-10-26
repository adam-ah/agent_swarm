package main

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestLastJSONObject(t *testing.T) {
	tests := []struct {
		name string
		text string
		want map[string]any
	}{
		{name: "object alone", text: `{"a":1}`, want: map[string]any{"a": 1.0}},
		{name: "object after prose", text: "done.\n{\"verdict\":\"bug\"}", want: map[string]any{"verdict": "bug"}},
		{name: "trailing whitespace", text: "{\"a\":1}\n\n  ", want: map[string]any{"a": 1.0}},
		{name: "last of several objects wins", text: `{"x":1}` + "\n" + `{"y":2}`, want: map[string]any{"y": 2.0}},
		{name: "nested object", text: `{"a":{"b":[1,2]}}`, want: map[string]any{"a": map[string]any{"b": []any{1.0, 2.0}}}},
		{name: "text after the object is not accepted", text: `{"a":1} more`, want: nil},
		{name: "no object", text: "hello", want: nil},
		{name: "broken object", text: `{"a":`, want: nil},
		{name: "empty", text: "", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lastJSONObject(tt.text); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("lastJSONObject(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestCodexItemInput(t *testing.T) {
	tests := []struct {
		name string
		item codexItem
		raw  string
		want string
	}{
		{name: "command wins", item: codexItem{Command: "go test ./..."}, raw: `{"command":"go test ./..."}`, want: "go test ./..."},
		{name: "other item is pretty json", item: codexItem{Type: "mcp_tool_call"}, raw: `{"tool":"x"}`, want: "{\n  \"tool\": \"x\"\n}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.item.input(json.RawMessage(tt.raw)); got != tt.want {
				t.Errorf("input() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCodexItemOutput(t *testing.T) {
	zero, one := 0, 1
	tests := []struct {
		name       string
		item       codexItem
		raw        string
		wantOutput string
		wantFailed bool
	}{
		{name: "command output", item: codexItem{Command: "ls", Output: "a.go"}, wantOutput: "a.go"},
		{name: "exit zero", item: codexItem{Command: "ls", Output: "a.go", ExitCode: &zero}, wantOutput: "a.go"},
		{name: "exit one fails", item: codexItem{Command: "ls", Output: "no", ExitCode: &one}, wantOutput: "no", wantFailed: true},
		{name: "failed status fails", item: codexItem{Command: "ls", Output: "no", Status: "failed"}, wantOutput: "no", wantFailed: true},
		{name: "non command item is pretty json", item: codexItem{Type: "mcp_tool_call", Status: "completed"}, raw: `{"ok":true}`, wantOutput: "{\n  \"ok\": true\n}"},
		{name: "non command item failed status", item: codexItem{Type: "mcp_tool_call", Status: "failed"}, raw: `{"ok":false}`, wantOutput: "{\n  \"ok\": false\n}", wantFailed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, failed := tt.item.output(json.RawMessage(tt.raw))
			if output != tt.wantOutput || failed != tt.wantFailed {
				t.Errorf("output() = (%q, %v), want (%q, %v)", output, failed, tt.wantOutput, tt.wantFailed)
			}
		})
	}
}

func TestOrError(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		err    error
		want   string
	}{
		{name: "stderr first", stderr: "boom", err: errors.New("exit 1"), want: "boom"},
		{name: "error when no stderr", stderr: "", err: errors.New("exit 1"), want: "exit 1"},
		{name: "nothing at all", stderr: "", err: nil, want: "no output"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := orError(tt.stderr, tt.err); got != tt.want {
				t.Errorf("orError() = %q, want %q", got, tt.want)
			}
		})
	}
}
