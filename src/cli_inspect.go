package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
)

func (c *cli) showStep(run *Run) error {
	step := findStep(run.Steps, func(s *StepRun) bool { return s.Path == c.step })
	if step == nil {
		return fmt.Errorf("no step %s in run %s; swarm show %s lists the steps", c.step, run.ID, run.ID)
	}
	if c.asJSON {
		return printJSON(step)
	}
	fmt.Print(stepDetail(run.ID, step, c.link(run.ID)))
	return nil
}

func stepDetail(id string, s *StepRun, link string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s  %s\n%s\n", s.Path, s.Status, s.Title, strings.Join(stepFacts(s), " · "))
	if line := sessionLine(s); line != "" {
		b.WriteString(line + "\n")
	}
	for _, part := range [][2]string{{"error", s.Error}, {"ended the run", s.Ended}, {"sent back with", s.SentBack}, {"feedback", s.Feedback},
		{"loop", s.Loop}, {"headline", s.Headline}, {"question", s.Question}} {
		b.WriteString(block(part[0], part[1], ""))
	}
	output := s.Output
	if _, parsed := output["output"]; parsed && s.Kind == "tool" && len(output) > 1 {
		output = maps.Clone(output)
		delete(output, "output")
	}
	if fields := fieldsText(output, "  "); fields != "" {
		b.WriteString("fields:\n" + fields)
	}
	if len(s.Artifacts) > 0 {
		fmt.Fprintf(&b, "shows (%d; swarm files %s PATH prints one):\n", len(s.Artifacts), id)
		for _, a := range s.Artifacts {
			fmt.Fprintf(&b, "  %-8s %s\n", a.Kind, a.Path)
		}
	}
	b.WriteString(asksText(s.Asks))
	if len(s.Children) > 0 {
		fmt.Fprintf(&b, "steps (%d):\n%s", len(s.Children), stepLines(s.Children, "  ", link))
	}
	for n, earlier := range s.Earlier {
		fmt.Fprintf(&b, "earlier attempt %d:\n%s", n+1, indented(stepDetail(id, earlier, link), "  "))
	}
	b.WriteString(logsText(id, s))
	return b.String()
}

func stepFacts(s *StepRun) []string {
	facts := []string{strings.TrimSpace(s.Kind + " " + s.Uses)}
	if s.Runner != "" {
		facts = append(facts, "runner "+s.Runner)
	}
	if s.StartedAt != nil {
		facts = append(facts, "started "+clock(*s.StartedAt))
		if s.EndedAt != nil {
			facts = append(facts, "ended "+clock(*s.EndedAt), "ran "+took(elapsed(*s.StartedAt, s.EndedAt)))
		} else {
			facts = append(facts, s.Status+" "+span(elapsed(*s.StartedAt, nil)))
		}
	}
	if s.Cost > 0 {
		facts = append(facts, money(s.Cost))
	}
	if s.Tokens > 0 {
		facts = append(facts, tokens(s.Tokens))
	}
	if s.Actions > 0 {
		facts = append(facts, fmt.Sprintf("%d actions", s.Actions))
	}
	if s.Rounds != "" {
		facts = append(facts, "round "+s.Rounds)
	} else if s.Round > 0 {
		facts = append(facts, fmt.Sprintf("attempt %d", s.Round+1))
	}
	if s.Repeats > 0 {
		facts = append(facts, fmt.Sprintf("%d repeats", s.Repeats))
	}
	if s.Always {
		facts = append(facts, "always")
	}
	return facts
}

func sessionLine(s *StepRun) string {
	if s.Kind != "agent" || s.EndedAt == nil {
		return ""
	}
	line := "new session"
	if s.Resumed != "" {
		line = "resumed the session of " + s.Resumed
	}
	if s.Session == "" {
		return line + " · no session id, not resumable"
	}
	return line + " · session " + s.Session
}

func asksText(asks []*Ask) string {
	var b strings.Builder
	for n, ask := range asks {
		state := "waiting for answers"
		switch {
		case ask.Decided:
			state = "left to the agent"
		case ask.AnsweredAt != nil:
			state = "answered " + clock(*ask.AnsweredAt)
		}
		if ask.AskedAt != nil {
			state = "asked " + clock(*ask.AskedAt) + ", " + state
		}
		fmt.Fprintf(&b, "questions, round %d (%s):\n", n+1, state)
		for i, question := range ask.Questions {
			b.WriteString(indented(fmt.Sprintf("%d. %s", i+1, question), "  "))
			if ask.AnsweredAt == nil && !ask.Decided {
				continue
			}
			answer := leftToAgent
			if i < len(ask.Answers) && strings.TrimSpace(ask.Answers[i]) != "" {
				answer = ask.Answers[i]
			}
			b.WriteString(indented("→ "+answer, "     "))
		}
	}
	return b.String()
}

const leftToAgent = "agent decides"

func logsText(id string, s *StepRun) string {
	logs := [][2]string{}
	if s.Log != "" {
		logs = append(logs, [2]string{s.Log, "call"})
	}
	if s.AskedAgain != "" {
		logs = append(logs, [2]string{s.AskedAgain, "re-prompt: the answer lacked a JSON object with every field"})
	}
	for n, ask := range s.Asks {
		if ask.Log != "" {
			logs = append(logs, [2]string{ask.Log, fmt.Sprintf("call after the answers to round %d", n+1)})
		}
	}
	if len(logs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("logs:\n")
	for _, log := range logs {
		fmt.Fprintf(&b, "  swarm log %s -step %-24s %s\n", id, log[0], log[1])
	}
	if s.Kind == "agent" {
		b.WriteString("  add -stream for the agent's transcript\n")
	}
	return b.String()
}

func stepLines(steps []*StepRun, indent, link string) string {
	var b strings.Builder
	for _, s := range steps {
		b.WriteString(stepLine(s, indent, link))
		b.WriteString(stepLines(s.Children, indent+"  ", link))
	}
	return b.String()
}

func (c *cli) callLog() error {
	run, err := c.loadRun()
	if err != nil {
		return err
	}
	if c.step == "" {
		return fmt.Errorf("swarm log needs -step PATH; swarm show %s lists the steps", run.ID)
	}
	log, err := logOf(run, c.step)
	if err != nil {
		return err
	}
	ext := ".call.json"
	if c.stream {
		ext = ".jsonl"
	}
	raw, err := os.ReadFile(filepath.Join(c.core.store.runDir(run.ID), log+ext))
	switch {
	case err != nil && c.stream:
		return fmt.Errorf("no transcript for %s; only agent steps keep one", log)
	case err != nil:
		return fmt.Errorf("nothing recorded for %s yet", log)
	case c.stream:
		_, err = os.Stdout.Write(raw)
		return err
	}
	if c.asJSON {
		var out bytes.Buffer
		if err := json.Indent(&out, raw, "", "  "); err != nil {
			return err
		}
		fmt.Println(out.String())
		return nil
	}
	var call Call
	if err := json.Unmarshal(raw, &call); err != nil {
		return err
	}
	fmt.Print(callText(call, run.ID, log))
	return nil
}

func (c *cli) loadRun() (*Run, error) {
	id, err := c.arg("a run id")
	if err != nil {
		return nil, err
	}
	run := c.core.store.load(id)
	if run == nil {
		return nil, fmt.Errorf("no run %s", id)
	}
	return run, nil
}

func logOf(run *Run, name string) (string, error) {
	step := findStep(run.Steps, func(s *StepRun) bool { return s.Path == name })
	if step != nil && step.Log != "" {
		return step.Log, nil
	}
	if findStep(run.Steps, func(s *StepRun) bool { return s.madeLog(name) }) != nil {
		return name, nil
	}
	if step != nil {
		return "", fmt.Errorf("%s is a %s step and made no call; swarm show %s -step %s shows it", name, step.Kind, run.ID, name)
	}
	return "", fmt.Errorf("no step or log %s in run %s; swarm show %s lists the steps", name, run.ID, run.ID)
}

func callText(call Call, id, log string) string {
	var b strings.Builder
	var words, long []string
	for i, arg := range call.Command {
		if len(arg) > 160 || strings.Contains(arg, "\n") {
			words = append(words, fmt.Sprintf("<argument %d>", i))
			long = append(long, block(sized(fmt.Sprintf("argument %d", i), arg), arg, ""))
			continue
		}
		words = append(words, shellWord(arg))
	}
	if call.Stdin != "" {
		words = append(words, "< stdin")
	}
	b.WriteString("$ " + strings.Join(words, " ") + "\n" + strings.Join(long, ""))
	facts := []string{"in " + call.Dir, "started " + clock(call.StartedAt)}
	if call.EndedAt != nil {
		facts = append(facts, "ran "+took(elapsed(call.StartedAt, call.EndedAt)))
	} else {
		facts = append(facts, "running "+span(elapsed(call.StartedAt, nil)))
	}
	if call.ExitCode != nil {
		facts = append(facts, exitText(*call.ExitCode))
	}
	b.WriteString(strings.Join(facts, " · ") + "\n")
	b.WriteString(block("limits", call.Limits, ""))
	b.WriteString(block("error", call.Error, ""))
	b.WriteString(block(fmt.Sprintf("killed at step end (%d)", len(call.Stopped)), strings.Join(call.Stopped, "\n"), ""))
	b.WriteString(block(fmt.Sprintf("kept until %s ends (%d)", call.KeptUntil, len(call.Kept)), strings.Join(call.Kept, "\n"), ""))
	b.WriteString(block(sized("stdin", call.Stdin), call.Stdin, ""))
	b.WriteString(actionsText(call))
	b.WriteString(block(sized("result", call.Result), call.Result, ""))
	b.WriteString(printed("stdout", call.Stdout, !call.Stream && call.EndedAt != nil))
	b.WriteString(printed("stderr", call.Stderr, call.EndedAt != nil))
	if call.Stream {
		fmt.Fprintf(&b, "transcript: swarm log %s -step %s -stream\n", id, log)
	}
	return b.String()
}

func exitText(code int) string {
	if code == -1 {
		return "killed"
	}
	return fmt.Sprintf("exit %d", code)
}

func printed(label, text string, ended bool) string {
	if text == "" && ended {
		return label + ": printed nothing\n"
	}
	return block(sized(label, text), text, "")
}

func actionsText(call Call) string {
	if len(call.Actions) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "tool calls (%d; -json has each input and output):\n", len(call.Actions))
	for _, a := range call.Actions {
		ran := "running"
		if a.EndedAt != nil {
			ran = took(elapsed(a.StartedAt, a.EndedAt))
		} else if call.EndedAt != nil {
			ran = "unfinished"
		}
		mark := " "
		if a.Failed {
			mark = "✗"
		}
		fmt.Fprintf(&b, "  +%-7s %10s %s %-10s %s\n", span(elapsed(call.StartedAt, &a.StartedAt)), ran, mark, a.Tool, actionLabel(a.Input))
	}
	return b.String()
}

func (c *cli) runFiles() error {
	run, err := c.loadRun()
	if err != nil {
		return err
	}
	dir := c.core.store.runDir(run.ID)
	if len(c.positional) > 1 {
		return printRunFile(c.core.store, run.ID, dir, c.positional[1])
	}
	fmt.Println("run folder:")
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		fmt.Printf("  %-40s %9s\n", rel, sizeOf(int(info.Size())))
		return nil
	})
	if err != nil {
		return err
	}
	if shown := shownFiles(run.Steps, map[string]bool{}); len(shown) > 0 {
		fmt.Printf("\nshown by steps (%d):\n", len(shown))
		for _, line := range shown {
			fmt.Println("  " + line)
		}
	}
	fmt.Printf("\nswarm files %s PATH prints one\n", run.ID)
	return nil
}

func shownFiles(steps []*StepRun, seen map[string]bool) []string {
	var out []string
	for _, s := range steps {
		for _, a := range s.Artifacts {
			if !seen[a.Path] {
				seen[a.Path] = true
				out = append(out, fmt.Sprintf("%-8s %s  (%s)", a.Kind, a.Path, s.Path))
			}
		}
		out = append(out, shownFiles(s.Children, seen)...)
		out = append(out, shownFiles(s.Earlier, seen)...)
	}
	return out
}

func printRunFile(store *Store, id, dir, path string) error {
	outside := fmt.Errorf("%s is outside run %s; swarm files %s lists its files", path, id, id)
	full := filepath.Join(dir, path)
	switch {
	case filepath.IsAbs(path) && store.hasArtifact(id, path):
		full = path
	case filepath.IsAbs(path):
		rel, err := filepath.Rel(dir, path)
		if err != nil || !filepath.IsLocal(rel) {
			return outside
		}
		full = path
	case !filepath.IsLocal(path):
		return outside
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return fmt.Errorf("no file %s in run %s; swarm files %s lists its files", path, id, id)
	}
	_, err = os.Stdout.Write(raw)
	return err
}
