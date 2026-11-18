package main

import (
	"strings"
	"testing"
	"time"
)

func tidiedProject(t *testing.T) (*Core, string) {
	t.Helper()
	shorten(t, &followEvery, 10*time.Millisecond)
	core, project := surfaceProject(t)
	surfaceFiles(t, project, map[string]string{
		".swarm/tools/flaky.json":  `{"title": "Flaky", "run": "test -f fixed || { echo broken >&2; exit 1; }; echo fixed"}`,
		".swarm/tools/status.json": `{"title": "Status", "run": "echo {{run_status}}"}`,
		".swarm/chains/tidied.json": `{"title": "Tidied", "steps": [{"id": "boom", "title": "Boom", "tool": "flaky"},
			{"id": "tidy", "title": "Tidy", "tool": "status", "always": true}]}`,
	})
	return core, project
}

func failTidied(t *testing.T, core *Core) string {
	t.Helper()
	out, err := surfaceCLI(t, "run", "tidied")
	surfaceFails(t, err, "the run did not finish")
	if !strings.Contains(out, "○ Tidy [tidy] (deferred): runs on resume or cancel") {
		t.Errorf("run output lacks the deferred step:\n%s", out)
	}
	id := strings.TrimPrefix(strings.SplitN(out, "\n", 2)[0], "run ")
	id = strings.Fields(id)[0]
	surfaceWait(t, core, id, "failed")
	return id
}

func TestCLIResume(t *testing.T) {
	core, project := tidiedProject(t)
	_, err := surfaceCLI(t, "resume")
	surfaceFails(t, err, "swarm resume needs a run id")
	done := surfaceRun(t, core, project, nil)
	_, err = surfaceCLI(t, "resume", done.ID)
	surfaceFails(t, err, "is done; nothing to resume")

	id := failTidied(t, core)
	out, err := surfaceCLI(t, "show", id)
	surfaceContains(t, out, err, "○ Tidy [tidy] (deferred)", "swarm resume "+id+": retry the failed steps and continue",
		"swarm cancel "+id+": run the deferred \"always\" steps")

	surfaceFiles(t, project, map[string]string{"fixed": ""})
	out, err = surfaceCLI(t, "resume", id)
	surfaceContains(t, out, err, "resuming "+id+" from Boom\n", "✓ Boom [boom · ", "✓ Tidy [tidy · ", "s]: running", "\ndone\n")
	if run := core.store.load(id); run.Status != "done" || run.RerunFrom != "Boom" || run.Cleanup {
		t.Errorf("resumed run = %s, from %q, cleanup %v", run.Status, run.RerunFrom, run.Cleanup)
	}

	out, err = surfaceCLI(t, "resume", id, "-from", "tidy", "-detach")
	surfaceContains(t, out, err, "resuming "+id+" from Tidy\n")
	surfaceWait(t, core, id, "done")
}

func TestCLICancelRunsTheDeferredSteps(t *testing.T) {
	core, _ := tidiedProject(t)
	id := failTidied(t, core)
	out, err := surfaceCLI(t, "cancel", id)
	surfaceContains(t, out, err, "running the deferred \"always\" steps of "+id)
	run := surfaceWait(t, core, id, "failed")
	if boom, tidy := run.Steps[0], run.Steps[1]; boom.Status != "failed" || boom.Round != 0 || tidy.Status != "done" || tidy.Output["output"] != "failed" {
		t.Errorf("after cancel: boom %s round %d, tidy %s saw %v", boom.Status, boom.Round, tidy.Status, tidy.Output["output"])
	}
	if !run.Cleanup || run.Reason != "Boom failed: broken" {
		t.Errorf("cleanup %v, reason %q", run.Cleanup, run.Reason)
	}
	out, err = surfaceCLI(t, "show", id)
	surfaceContains(t, out, err, "deferred \"always\" steps run at ", "swarm resume "+id+": retry the failed steps and continue")
	if strings.Contains(out, "swarm cancel "+id) {
		t.Errorf("show offers cancel with nothing deferred:\n%s", out)
	}
	_, err = surfaceCLI(t, "cancel", id)
	surfaceFails(t, err, "no such run in progress")
}

func TestCLIResumeHelp(t *testing.T) {
	surfaceProject(t)
	out, err := surfaceOutput(t, func() error { return dispatch("resume", []string{"-h"}) })
	surfaceContains(t, out, err, "swarm resume RUN [-from PATH] [-detach]", "-from PATH: rerun from that step instead")
}
