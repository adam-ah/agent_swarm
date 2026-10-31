package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	questionsField = "questions"
	answersField   = "answers"
	notAnswered    = "(not answered; left to the agent)"
	decideYourself = "The person asks you to decide the open questions yourself. Make the call you think is best for each, " +
		"say what you chose, and answer again in full, in the same form as before, with an empty questions list.\n"
	useAnswers = "Use these answers, then answer again in full, in the same form as before. " +
		"Ask more questions only if you still cannot go on without them.\n"
)

type Ask struct {
	Questions  []string   `json:"questions"`
	Answers    []string   `json:"answers,omitempty"`
	Decided    bool       `json:"decided,omitempty"`
	Log        string     `json:"log,omitempty"`
	AskedAt    *time.Time `json:"asked_at,omitempty"`
	AnsweredAt *time.Time `json:"answered_at,omitempty"`
}

func (p Prompt) asks() bool {
	return slices.ContainsFunc(p.Returns, func(f Field) bool { return f.Name == questionsField })
}

func questionProblems(fields Fields) []string {
	i := slices.IndexFunc(fields, func(f Field) bool { return f.Name == questionsField })
	if i < 0 {
		return nil
	}
	var problems []string
	if fields[i].Value != "list" {
		problems = append(problems, "## Return: questions must be a list: - `questions` (list): what you need the person to answer")
	}
	if slices.ContainsFunc(fields, func(f Field) bool { return f.Name == answersField }) {
		problems = append(problems, "## Return: answers is filled by swarm with the person's answers to questions; take it out")
	}
	return problems
}

func questionsIn(out map[string]any) []string {
	list, _ := out[questionsField].([]any)
	var questions []string
	for _, q := range list {
		if q := strings.TrimSpace(text(q)); q != "" {
			questions = append(questions, q)
		}
	}
	return questions
}

func (sr *StepRun) openAsk() *Ask {
	if len(sr.Asks) == 0 || sr.Asks[len(sr.Asks)-1].AnsweredAt != nil {
		return nil
	}
	return sr.Asks[len(sr.Asks)-1]
}

func (d decision) check(step *StepRun) error {
	open := step.openAsk()
	switch {
	case open != nil && d.Choice == "decide":
		return nil
	case open != nil && d.Choice != "answer":
		return fmt.Errorf("%q is asking you questions; answer them or let it decide", step.Title)
	case open != nil && len(d.Answers) != len(open.Questions):
		return fmt.Errorf("it asked %d questions and got %d answers", len(open.Questions), len(d.Answers))
	case open != nil:
		return nil
	case d.Choice == "answer" || d.Choice == "decide":
		return fmt.Errorf("%q is not asking questions; approve, revise or reject it", step.Title)
	case d.Choice != "approve" && d.Choice != "reject" && d.Choice != "revise":
		return errors.New("choice must be approve, revise or reject")
	case d.Choice == "revise" && step.Revise == "":
		return fmt.Errorf("%q cannot send the run back; give the user step a revise step first", step.Title)
	case d.Choice == "revise" && strings.TrimSpace(d.Note) == "":
		return errors.New("say what to change in the note")
	}
	return nil
}

func (r *Runner) clarify(ctx context.Context, call agentCall, builtin string, sc *scope, returns Fields, missing error, result agentResult, out map[string]any) (agentResult, map[string]any, error) {
	first := call.prompt
	var asks []*Ask
	for {
		questions := questionsIn(out)
		if len(questions) == 0 {
			out[answersField] = qaText(asks)
			return result, out, nil
		}
		if len(asks) > 0 && asks[len(asks)-1].Decided {
			return result, nil, fmt.Errorf("it asked %d more questions after you told it to decide for itself", len(questions))
		}
		ask, err := r.askPerson(ctx, call.run, call.sr, questions)
		if err != nil {
			return result, nil, err
		}
		asks = append(asks, ask)
		call.log = fmt.Sprintf("%s-ask-%d", call.sr.Log, len(asks))
		if builtin != "" && result.session != "" {
			call.session, call.prompt = result.session, answersPrompt(asks[len(asks)-1:])
		} else {
			call.session, call.prompt = "", strings.TrimRight(first, "\n")+"\n\n## Your questions\n\nYou asked the person questions before this answer.\n\n"+answersPrompt(asks)
		}
		r.store.update(call.run, func() { ask.Log = call.log })
		if result, out, err = r.answer(ctx, call, builtin, sc, returns, missing); err != nil {
			return result, nil, err
		}
	}
}

func (r *Runner) askPerson(ctx context.Context, run *Run, sr *StepRun, questions []string) (*Ask, error) {
	now := time.Now()
	ask := &Ask{Questions: questions, AskedAt: &now}
	if r.mocks != nil {
		ask.Decided, ask.AnsweredAt = true, &now
		r.mocks.notes[sr] = append(r.mocks.notes[sr], "asked "+strings.Join(questions, " | ")+"; nobody answers in a dry run, so it was told to decide")
		r.store.update(run, func() { sr.Asks = append(sr.Asks, ask) })
		return ask, nil
	}
	answers, done := run.inbox.await(sr.Path)
	defer done()
	r.store.update(run, func() {
		sr.Asks = append(sr.Asks, ask)
		sr.Status, run.Status = "waiting", "waiting"
	})
	select {
	case d := <-answers:
		now := time.Now()
		blank := !slices.ContainsFunc(d.Answers, func(a string) bool { return strings.TrimSpace(a) != "" })
		r.store.update(run, func() {
			ask.Answers, ask.Decided, ask.AnsweredAt = d.Answers, d.Choice == "decide" || blank, &now
			sr.Status = "running"
			run.refreshStatus()
		})
		return ask, nil
	case <-ctx.Done():
		return nil, errors.New("stopped")
	}
}

func answersPrompt(asks []*Ask) string {
	last := asks[len(asks)-1]
	if last.Decided {
		answered := asks[:len(asks)-1]
		if len(answered) == 0 {
			return decideYourself
		}
		return "The person answered these:\n\n" + qaText(answered) + "\n\n" + decideYourself
	}
	return "The person answered your questions:\n\n" + qaText(asks) + "\n\n" + useAnswers
}

func qaText(asks []*Ask) string {
	var b strings.Builder
	for _, ask := range asks {
		for i, q := range ask.Questions {
			answer := notAnswered
			if i < len(ask.Answers) && strings.TrimSpace(ask.Answers[i]) != "" {
				answer = strings.TrimSpace(ask.Answers[i])
			}
			fmt.Fprintf(&b, "Q: %s\nA: %s\n\n", q, answer)
		}
	}
	return strings.TrimSpace(b.String())
}
