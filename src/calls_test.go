package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func readCall(t *testing.T, path string) Call {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var call Call
	if err := json.Unmarshal(raw, &call); err != nil {
		t.Fatalf("call file is not JSON: %v\n%s", err, raw)
	}
	return call
}

func TestStartCallWritesCommandImmediately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "call.json")
	cmd := exec.Command("echo", "hi")
	cmd.Dir = t.TempDir()

	startCall(path, cmd, "prompt text", true)

	call := readCall(t, path)
	if !reflect.DeepEqual(call.Command, []string{"echo", "hi"}) {
		t.Errorf("Command = %v", call.Command)
	}
	if call.Dir != cmd.Dir || call.Stdin != "prompt text" || !call.Stream {
		t.Errorf("Dir, Stdin, Stream = %q, %q, %v", call.Dir, call.Stdin, call.Stream)
	}
	if call.StartedAt.IsZero() || call.EndedAt != nil {
		t.Errorf("StartedAt = %v, EndedAt = %v", call.StartedAt, call.EndedAt)
	}
}

func TestCallLogBeginEndFinish(t *testing.T) {
	path := filepath.Join(t.TempDir(), "call.json")
	cmd := exec.Command("echo", "hi")
	log := startCall(path, cmd, "", false)

	log.begin("t1", "Bash", `{"command":"ls"}`)
	log.end("t1", "Bash", "", "file.txt", false)
	log.begin("t2", "Read", "a.go")
	log.end("t2", "Read", "", "missing", true)
	log.finish(cmd, errors.New("boom"), "out", "err")

	call := readCall(t, path)
	if len(call.Actions) != 2 {
		t.Fatalf("got %d actions, want 2: %+v", len(call.Actions), call.Actions)
	}
	first, second := call.Actions[0], call.Actions[1]
	if first.ID != "t1" || first.Tool != "Bash" || first.Input != `{"command":"ls"}` || first.Output != "file.txt" || first.Failed {
		t.Errorf("first action = %+v", first)
	}
	if second.ID != "t2" || second.Output != "missing" || !second.Failed {
		t.Errorf("second action = %+v", second)
	}
	if first.EndedAt == nil || second.EndedAt == nil {
		t.Errorf("ended actions have no EndedAt: %+v %+v", first, second)
	}
	if call.Stdout != "out" || call.Stderr != "err" || call.EndedAt == nil {
		t.Errorf("stdout, stderr, ended = %q, %q, %v", call.Stdout, call.Stderr, call.EndedAt)
	}
	if call.Error != "boom" || call.ExitCode != nil {
		t.Errorf("Error = %q, ExitCode = %v; want the error and no exit code", call.Error, call.ExitCode)
	}
}

func TestCallLogEndWithoutBeginAddsAction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "call.json")
	cmd := exec.Command("true")
	log := startCall(path, cmd, "", false)

	log.end("orphan", "Grep", "pattern", "match", false)
	log.finish(cmd, nil, "", "")

	call := readCall(t, path)
	if len(call.Actions) != 1 || call.Actions[0].ID != "orphan" || call.Actions[0].Tool != "Grep" || call.Actions[0].Output != "match" {
		t.Errorf("actions = %+v", call.Actions)
	}
}

func TestCallLogEndTwiceAddsSecondAction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "call.json")
	cmd := exec.Command("true")
	log := startCall(path, cmd, "", false)

	log.begin("x", "Bash", "ls")
	log.end("x", "Bash", "", "one", false)
	log.end("x", "Bash", "", "two", false)
	log.finish(cmd, nil, "", "")

	call := readCall(t, path)
	if len(call.Actions) != 2 || call.Actions[0].Output != "one" || call.Actions[1].Output != "two" {
		t.Errorf("actions = %+v", call.Actions)
	}
}

func TestCallLogFinishRecordsExitCode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "call.json")
	cmd := exec.Command("sh", "-c", "exit 3")
	log := startCall(path, cmd, "", false)

	runErr := cmd.Run()
	log.finish(cmd, runErr, "", "")

	call := readCall(t, path)
	if call.ExitCode == nil || *call.ExitCode != 3 {
		t.Errorf("ExitCode = %v, want 3", call.ExitCode)
	}
	if call.Error != "" {
		t.Errorf("Error = %q, want empty when the process ran", call.Error)
	}
}

func TestRawText(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "json string", raw: `"hello"`, want: "hello"},
		{name: "text parts joined", raw: `[{"type":"text","text":"a"},{"type":"image"},{"type":"text","text":"b"}]`, want: "ab"},
		{name: "parts without text fall back to pretty json", raw: `[{"type":"image"}]`, want: "[\n  {\n    \"type\": \"image\"\n  }\n]"},
		{name: "object is pretty printed", raw: `{"a":1}`, want: "{\n  \"a\": 1\n}"},
		{name: "not json is returned as is", raw: `not json`, want: "not json"},
		{name: "empty", raw: ``, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rawText(json.RawMessage(tt.raw)); got != tt.want {
				t.Errorf("rawText(%s) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestPrettyRaw(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "object", raw: `{"a":[1,2]}`, want: "{\n  \"a\": [\n    1,\n    2\n  ]\n}"},
		{name: "not json", raw: `{oops`, want: `{oops`},
		{name: "empty", raw: ``, want: ``},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prettyRaw(json.RawMessage(tt.raw)); got != tt.want {
				t.Errorf("prettyRaw(%s) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
