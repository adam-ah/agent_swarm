package main

import "fmt"

type agentSession struct {
	id     string
	runner string
	title  string
}

func resumePoint(step Step, sr *StepRun, sc *scope, runner string, settings Settings) (agentSession, string, error) {
	if own, ok := sc.sessions[step.ID]; ok && sr.SentBack != "" && own.runner == runner && settings.canResume(runner) {
		return own, sentBackPrompt(sr.SentBack), nil
	}
	if step.Resume == "" {
		return agentSession{}, "", nil
	}
	s, ok := sc.sessions[step.Resume]
	switch {
	case !settings.canResume(runner):
		return s, "", fmt.Errorf("%s cannot resume a session; only claude, codex and agents that run them can", runner)
	case !ok:
		return s, "", fmt.Errorf("nothing to resume: step %s has not run yet in this run", step.Resume)
	case s.runner != runner:
		return s, "", fmt.Errorf("step %s ran with %s and this step runs with %s; a session resumes only on the agent that made it", step.Resume, s.runner, runner)
	}
	return s, "", nil
}

func sentBackPrompt(note string) string {
	return "A person sent your last answer back with this note. Follow it, then answer again in full, in the same form as before.\n\n" + note + "\n"
}

func (s *scope) keepSession(step Step, session agentSession) {
	s.sessions[step.ID] = session
	if step.Resume != "" {
		s.sessions[step.Resume] = session
	}
}
