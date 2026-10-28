package main

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestObjectWithFields(t *testing.T) {
	fields := Fields{{Name: "summary", Value: "text"}, {Name: "files", Value: "list"}}
	tests := []struct {
		name string
		text string
		want map[string]any
	}{
		{name: "object alone", text: `{"summary":"s","files":[]}`, want: map[string]any{"summary": "s", "files": []any{}}},
		{name: "object after prose", text: "done.\n{\"summary\":\"s\",\"files\":[\"a\"]}", want: map[string]any{"summary": "s", "files": []any{"a"}}},
		{name: "object in a code fence", text: "done.\n\n```json\n{\"summary\":\"s\",\"files\":[]}\n```", want: map[string]any{"summary": "s", "files": []any{}}},
		{name: "text after the object", text: `{"summary":"s","files":[]} and that is all`, want: map[string]any{"summary": "s", "files": []any{}}},
		{name: "last complete object wins", text: `{"summary":"one","files":[]}` + "\n" + `{"summary":"two","files":[]}`, want: map[string]any{"summary": "two", "files": []any{}}},
		{name: "objects missing a field are skipped", text: `{"summary":"s","files":[]} then {"summary":"partial"}`, want: map[string]any{"summary": "s", "files": []any{}}},
		{name: "extra fields are kept", text: `{"summary":"s","files":[],"note":1}`, want: map[string]any{"summary": "s", "files": []any{}, "note": 1.0}},
		{name: "nested values", text: `{"summary":{"b":[1,2]},"files":[]}`, want: map[string]any{"summary": map[string]any{"b": []any{1.0, 2.0}}, "files": []any{}}},
		{name: "no object has every field", text: `{"summary":"s"}`, want: nil},
		{name: "no object", text: "hello", want: nil},
		{name: "broken object", text: `{"summary":`, want: nil},
		{name: "empty", text: "", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := objectWithFields(tt.text, fields); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("objectWithFields(%q) = %v, want %v", tt.text, got, tt.want)
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
