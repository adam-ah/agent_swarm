package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

func (c *cli) link(id string) string {
	if !pageUp(c.addr) {
		return ""
	}
	return "http://" + c.addr + "/#/run/" + id
}

func (c *cli) runs(project string) error {
	list := c.core.store.list(project)
	if c.asJSON {
		return printJSON(list)
	}
	if len(list) == 0 {
		fmt.Println("No runs.")
	}
	for _, r := range list {
		ran := ""
		if r.EndedAt != nil {
			ran = span(elapsed(r.StartedAt, r.EndedAt))
		} else if r.Status == "running" || r.Status == "waiting" {
			ran = "+" + span(elapsed(r.StartedAt, nil))
		}
		fmt.Printf("%s  %-11s %-16s %8s %7s  %s\n", r.ID, statusText(r.Status, r.FailedItems), r.Chain, ran, money(r.Cost), firstLine(r.Subject))
	}
	return nil
}

func (c *cli) run(project string) error {
	chain, err := c.arg("a chain name")
	if err != nil {
		return err
	}
	run, err := c.core.StartRun(project, chain, c.inputs)
	if err != nil {
		return err
	}
	fmt.Println("run " + run.ID)
	if link := c.link(run.ID); link != "" {
		fmt.Println("on the page: " + link)
	}
	if c.detach {
		fmt.Println("detached; poll with swarm show " + run.ID)
		return nil
	}
	fmt.Println("following; Ctrl-C detaches, swarm cancel " + run.ID + " stops the run")
	return c.follow(run.ID)
}

func (c *cli) decide() error {
	id, err := c.arg("a run id")
	if err != nil {
		return err
	}
	d := decision{Choice: c.command, Note: c.note}
	if c.command == "answer" {
		d = decision{Choice: "answer", Answers: c.answers}
	}
	if c.command == "answer" && c.leave {
		d = decision{Choice: "decide"}
	}
	d.Step = c.step
	if err := c.core.Decide(id, d); err != nil {
		return err
	}
	fmt.Println(map[string]string{"approve": "approved", "revise": "sent back", "reject": "rejected", "answer": "answered", "decide": "left to the agent:"}[d.Choice] + " " + id)
	return nil
}

func (c *cli) cancel() error {
	id, err := c.arg("a run id")
	if err != nil {
		return err
	}
	if err := c.core.Stop(id); err != nil {
		return err
	}
	fmt.Println("stopping " + id + "; always steps still run")
	return nil
}

func (c *cli) show() error {
	id, err := c.arg("a run id")
	if err != nil {
		return err
	}
	run := c.core.store.load(id)
	if run == nil {
		return fmt.Errorf("no run %s", id)
	}
	if c.step != "" {
		return c.showStep(run)
	}
	if c.asJSON {
		return printJSON(run.shown())
	}
	fmt.Printf("%s  %s  %s\n%s\n%s\n", run.ID, statusText(run.Status, failedItems(run.Steps)), run.Title, run.Subject, runStats(run))
	if len(run.Inputs) > 0 {
		fmt.Print("inputs:\n" + fieldsText(toAny(run.Inputs), "  "))
	}
	fmt.Println()
	report(run.Steps, "", map[string]string{}, c.link(id))
	if run.Reason != "" {
		fmt.Printf("\n%s\n", run.Reason)
	}
	if len(run.Stopped) > 0 {
		fmt.Print("\n" + block(fmt.Sprintf("killed outside a step (%d)", len(run.Stopped)), strings.Join(run.Stopped, "\n"), ""))
	}
	fmt.Printf("\nswarm show %s -step PATH: one step in full · swarm log %s -step PATH: its call · swarm files %s: run files\n", id, id, id)
	return nil
}

func runStats(run *Run) string {
	stats := []string{"started " + clock(run.StartedAt)}
	if run.ResumedAt != nil {
		from := run.RerunFrom
		if from == "" {
			from = "its failed step"
		}
		stats = append(stats, "rerun from "+from+" at "+clock(*run.ResumedAt))
	}
	switch {
	case run.EndedAt != nil:
		stats = append(stats, "ran "+span(elapsed(run.StartedAt, run.EndedAt)))
	case run.going():
		stats = append(stats, span(elapsed(run.StartedAt, nil))+" so far")
	}
	if run.Cost > 0 {
		stats = append(stats, money(run.Cost))
	}
	return strings.Join(stats, " · ")
}

func (c *cli) follow(id string) error {
	seen := map[string]string{}
	link := c.link(id)
	for {
		run := c.core.store.load(id)
		if run == nil {
			return fmt.Errorf("no run %s", id)
		}
		report(run.Steps, "", seen, link)
		if !run.going() {
			fmt.Printf("\n%s", statusText(run.Status, failedItems(run.Steps)))
			if run.Reason != "" {
				fmt.Printf(": %s", run.Reason)
			}
			fmt.Println()
			if run.Status != "done" {
				return errors.New("the run did not finish")
			}
			return nil
		}
		time.Sleep(followEvery)
	}
}

func report(steps []*StepRun, indent string, seen map[string]string, link string) {
	for _, s := range steps {
		if seen[s.Path] != s.Status {
			seen[s.Path] = s.Status
			fmt.Print(stepLine(s, indent, link))
		}
		report(s.Children, indent+"  ", seen, link)
	}
}

func stepLine(s *StepRun, indent, link string) string {
	if ask := s.openAsk(); s.Status == "waiting" && ask != nil {
		return askLine(s, ask, indent, link)
	}
	title := shownTitle(s) + stepTag(s)
	switch s.Status {
	case "running":
		return fmt.Sprintf("%s… %s\n", indent, shownTitle(s)+stepTag(s, s.Kind))
	case "waiting":
		answers := "swarm approve / swarm reject"
		if s.Revise != "" {
			answers = "swarm approve / swarm revise -note TEXT (back to " + s.Revise + ") / swarm reject"
		}
		answers += stepFlag(s)
		if link != "" {
			answers += ", or on the page: " + link
		}
		return fmt.Sprintf("%s⏸ %s: %s\n%s  answer with %s\n", indent, title, s.Question, indent, answers)
	case "done":
		return fmt.Sprintf("%s✓ %s%s\n", indent, title, suffix(s.Headline))
	case "failed":
		return fmt.Sprintf("%s✗ %s%s\n", indent, title, suffix(s.Error))
	case "rejected":
		return fmt.Sprintf("%s✗ %s (rejected)%s\n", indent, title, suffix(s.Headline))
	case "stopped":
		return fmt.Sprintf("%s■ %s (ended the run)%s\n", indent, title, suffix(s.Headline))
	case "skipped":
		return fmt.Sprintf("%s- %s (skipped)\n", indent, title)
	}
	return fmt.Sprintf("%s  %s\n", indent, title)
}

func stepTag(s *StepRun, extra ...string) string {
	parts := append([]string{s.Path}, extra...)
	if s.StartedAt != nil {
		parts = append(parts, clock(*s.StartedAt))
		switch {
		case s.EndedAt != nil:
			parts = append(parts, took(elapsed(*s.StartedAt, s.EndedAt)))
		case s.Status == "running":
			parts = append(parts, "running "+span(elapsed(*s.StartedAt, nil)))
		}
	}
	if s.Cost > 0 && !holdsSteps(s) {
		parts = append(parts, money(s.Cost))
	} else if s.Tokens > 0 {
		parts = append(parts, tokens(s.Tokens))
	}
	if s.Status == "running" && s.Kind == "agent" {
		parts = append(parts, fmt.Sprintf("%d actions", s.Actions))
	}
	parts = slices.DeleteFunc(parts, func(p string) bool { return p == "" })
	if len(parts) == 0 {
		return ""
	}
	return " [" + strings.Join(parts, " · ") + "]"
}

func shownTitle(s *StepRun) string {
	if s.Kind != "item" {
		return s.Title
	}
	if s.Label != "" {
		return s.Label
	}
	if _, rest, found := strings.Cut(s.Title, "/runs/"); found {
		if _, inRun, ok := strings.Cut(rest, "/"); ok {
			return inRun
		}
	}
	return s.Title
}

func holdsSteps(s *StepRun) bool {
	return s.Kind == "chain" || s.Kind == "item"
}

func askLine(s *StepRun, ask *Ask, indent, link string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s? %s asks:\n", indent, shownTitle(s)+stepTag(s))
	for i, q := range ask.Questions {
		fmt.Fprintf(&b, "%s  %d. %s\n", indent, i+1, q)
	}
	how := "swarm answer RUN -a TEXT (once per question, in order) / swarm answer RUN -decide (it chooses)" + stepFlag(s)
	if link != "" {
		how += ", or on the page: " + link
	}
	fmt.Fprintf(&b, "%s  answer with %s\n", indent, how)
	return b.String()
}

func stepFlag(s *StepRun) string {
	for _, part := range strings.Split(s.Path, ".") {
		if _, err := strconv.Atoi(part); err == nil {
			return ", adding -step " + s.Path
		}
	}
	return ""
}

func suffix(text string) string {
	text = firstLine(text)
	if text == "" {
		return ""
	}
	if len(text) > 160 {
		text = text[:160] + "…"
	}
	return ": " + text
}
