package main

import (
	"encoding/json"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
)

type Call struct {
	Command   []string   `json:"command"`
	Dir       string     `json:"dir"`
	Stdin     string     `json:"stdin,omitempty"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	ExitCode  *int       `json:"exit_code,omitempty"`
	Error     string     `json:"error,omitempty"`
	Stdout    string     `json:"stdout,omitempty"`
	Stderr    string     `json:"stderr,omitempty"`
	Result    string     `json:"result,omitempty"`
	Stream    bool       `json:"stream,omitempty"`
	Limits    string     `json:"limits,omitempty"`
	Stopped   []string   `json:"stopped,omitempty"`
	KeptUntil string     `json:"kept_until,omitempty"`
	Kept      []string   `json:"kept,omitempty"`
	Actions   []*Action  `json:"actions,omitempty"`
}

type Action struct {
	ID        string     `json:"id"`
	Tool      string     `json:"tool"`
	Input     string     `json:"input"`
	Output    string     `json:"output,omitempty"`
	Failed    bool       `json:"failed,omitempty"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

type callLog struct {
	mu    sync.Mutex
	path  string
	call  Call
	timer *time.Timer
}

func startCall(path string, cmd *exec.Cmd, stdin string, stream bool) *callLog {
	c := &callLog{path: path, call: Call{Command: cmd.Args, Dir: cmd.Dir, Stdin: stdin, StartedAt: time.Now(), Stream: stream}}
	c.mu.Lock()
	c.save()
	c.mu.Unlock()
	return c
}

func (c *callLog) edit(change func(*Call)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	change(&c.call)
	if c.timer == nil {
		c.timer = time.AfterFunc(time.Second, func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.timer = nil
			c.save()
		})
	}
}

func (c *callLog) begin(id, tool, input string) {
	c.edit(func(call *Call) {
		call.Actions = append(call.Actions, &Action{ID: id, Tool: tool, Input: input, StartedAt: time.Now()})
	})
}

func (c *callLog) end(id, tool, input, output string, failed bool) {
	now := time.Now()
	c.edit(func(call *Call) {
		var action *Action
		for _, a := range call.Actions {
			if a.ID == id && a.EndedAt == nil {
				action = a
			}
		}
		if action == nil {
			action = &Action{ID: id, Tool: tool, Input: input, StartedAt: now}
			call.Actions = append(call.Actions, action)
		}
		action.Output, action.Failed, action.EndedAt = output, failed, &now
	})
}

func (c *callLog) stopped(processes []string) {
	if len(processes) == 0 {
		return
	}
	c.edit(func(call *Call) {
		for _, p := range processes {
			if !slices.Contains(call.Stopped, p) {
				call.Stopped = append(call.Stopped, p)
			}
		}
	})
}

func (c *callLog) kept(until string, processes []string) {
	if len(processes) == 0 {
		return
	}
	c.edit(func(call *Call) { call.KeptUntil, call.Kept = until, processes })
}

func (c *callLog) lastAction() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.call.Actions) == 0 {
		return "none yet"
	}
	last := c.call.Actions[len(c.call.Actions)-1]
	return last.Tool + ": " + actionLabel(last.Input)
}

func (c *callLog) finish(cmd *exec.Cmd, err error, stdout, stderr string) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.call.EndedAt = &now
	c.call.Stdout, c.call.Stderr = stdout, stderr
	if cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		c.call.ExitCode = &code
	} else if err != nil {
		c.call.Error = err.Error()
	}
	c.save()
}

func (c *callLog) save() {
	raw, err := json.Marshal(c.call)
	if err != nil {
		return
	}
	_ = writeFile(c.path, raw)
}

func rawText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var joined strings.Builder
		for _, p := range parts {
			if p.Type == "text" {
				joined.WriteString(p.Text)
			}
		}
		if joined.Len() > 0 {
			return joined.String()
		}
	}
	return prettyRaw(raw)
}

func prettyRaw(raw json.RawMessage) string {
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(pretty)
}
