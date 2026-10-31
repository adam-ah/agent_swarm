package main

import (
	"errors"
	"fmt"
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

func (c *cli) runs(project string) {
	for _, r := range c.core.store.list(project) {
		fmt.Printf("%s  %-11s %-16s %s\n", r.ID, r.Status, r.Chain, firstLine(r.Subject))
	}
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
	fmt.Printf("%s  %s  %s\n%s\n\n", run.ID, run.Status, run.Title, run.Subject)
	report(run.Steps, "", map[string]string{}, c.link(id))
	if run.Reason != "" {
		fmt.Printf("\n%s\n", run.Reason)
	}
	return nil
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
			fmt.Printf("\n%s", run.Status)
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
	switch s.Status {
	case "running":
		return fmt.Sprintf("%s… %s [%s]\n", indent, s.Title, s.Kind)
	case "waiting":
		answers := "swarm approve / swarm reject"
		if s.Revise != "" {
			answers = "swarm approve / swarm revise -note TEXT (back to " + s.Revise + ") / swarm reject"
		}
		answers += stepFlag(s)
		if link != "" {
			answers += ", or on the page: " + link
		}
		return fmt.Sprintf("%s⏸ %s: %s\n%s  answer with %s\n", indent, s.Title, s.Question, indent, answers)
	case "done":
		return fmt.Sprintf("%s✓ %s%s\n", indent, s.Title, suffix(s.Headline))
	case "failed":
		return fmt.Sprintf("%s✗ %s%s\n", indent, s.Title, suffix(s.Error))
	case "rejected":
		return fmt.Sprintf("%s✗ %s (rejected)%s\n", indent, s.Title, suffix(s.Headline))
	case "stopped":
		return fmt.Sprintf("%s■ %s (ended the run)%s\n", indent, s.Title, suffix(s.Headline))
	case "skipped":
		return fmt.Sprintf("%s- %s (skipped)\n", indent, s.Title)
	}
	return fmt.Sprintf("%s  %s\n", indent, s.Title)
}

func askLine(s *StepRun, ask *Ask, indent, link string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s? %s asks:\n", indent, s.Title)
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
