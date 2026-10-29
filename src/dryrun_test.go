package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const flowChain = `{"title": "Flow", "inputs": [{"name": "topic", "label": "Topic"}], "steps": [
  {"id": "judge", "title": "Judge", "agent": "judge"},
  {"id": "note", "title": "Note", "tool": "echo", "with": {}, "when": "judge.verdict == CHANGES"},
  {"id": "again", "title": "Again", "repeat": "judge", "when": "judge.verdict == CHANGES"},
  {"id": "free", "title": "Free", "agent": "free"},
  {"id": "ok", "title": "OK", "user": "Keep {{judge.summary}} for {{topic}} and {{ghost.field}}?", "revise": "judge"},
  {"id": "ship", "title": "Ship", "tool": "ship", "with": {"what": "{{judge.summary}}"}}
]}`

func flowProject(t *testing.T) string {
	t.Helper()
	_, project := surfaceProject(t)
	t.Setenv("TMPDIR", t.TempDir())
	surfaceFiles(t, project, map[string]string{
		".swarm/chains/flow.json":       flowChain,
		".swarm/chains/flow/judge.md":   "Judge {{topic}}.\n\n## Return\n\n- `verdict`: `APPROVE` or `CHANGES`\n- `summary`: one line\n- `files` (list): files\n- `score` (number): 1 to 10\n- `sure` (bool): sure\n",
		".swarm/chains/flow/free.md":    "Say anything.",
		".swarm/tools/ship.json":        `{"title": "Ship", "run": "echo shipped {{what}}\necho done"}`,
		".swarm/chains/loops.json":      `{"title": "Loops", "steps": [{"id": "a", "title": "A", "tool": "echo"}, {"id": "ok", "title": "OK", "user": "OK?", "revise": "a"}]}`,
		".swarm/chains/needs.json":      `{"title": "Needs", "inputs": [{"name": "x", "label": "X"}], "steps": [{"id": "a", "title": "A", "tool": "echo"}]}`,
		".swarm/chains/broken_ref.json": `{"title": "Broken", "steps": [{"id": "a", "title": "A", "tool": "ghost"}]}`,
	})
	return project
}

func dryRun(t *testing.T, args ...string) string {
	t.Helper()
	out, err := surfaceCLI(t, "test", args...)
	if err != nil {
		t.Fatalf("swarm test %v: %v\n%s", args, err, out)
	}
	return out
}

func TestDryRunAnswersEveryStepWithExamples(t *testing.T) {
	flowProject(t)
	out := dryRun(t, "flow", "-input", "topic=login")
	for _, want := range []string{
		"test run of flow: every step answered with a mock\nits files are in ",
		"✓ judge (agent judge) mocked: files=<judge.files> score=1 summary=<judge.summary> sure=true verdict=APPROVE",
		"- note (tool echo) Skipped: judge.verdict is not CHANGES",
		"- again (repeat judge) Skipped: judge.verdict is not CHANGES",
		"✓ free (agent free) mocked: output=<free.output>",
		"✓ ok (user) mocked: decision=approve note=\n    ! reads {{ghost.field}}, which nothing filled",
		"✓ ship (tool ship) mocked: output=<ship.output>\n    $ echo shipped '<judge.summary>' ↵ echo done",
		"the run ended done",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	folder := strings.TrimSpace(strings.SplitN(strings.SplitN(out, "its files are in ", 2)[1], "\n", 2)[0])
	if raw, err := os.ReadFile(filepath.Join(folder, "judge.txt")); err != nil || !strings.HasPrefix(string(raw), "Judge login.") {
		t.Errorf("judge's filled prompt = %q, %v", raw, err)
	}
}

func TestDryRunFollowsMockedRoundsRevisionsAndFailures(t *testing.T) {
	project := flowProject(t)
	mockFile := filepath.Join(project, "mocks.json")
	surfaceFiles(t, project, map[string]string{"mocks.json": `{
  "judge": [{"verdict": "CHANGES", "summary": "first", "extra": 1}, {"verdict": "APPROVE"}],
  "note": "noted",
  "ok": ["revise: tighter"],
  "ship": {"fail": "no network"}
}`})
	out := dryRun(t, "flow", "-input", "topic=x", "-mock", mockFile)
	for _, want := range []string{
		"✓ judge (agent judge) mocked: extra=1 files=<judge.files> score=1 summary=first sure=true verdict=CHANGES\n    ! the mock leaves out files, so it got an example",
		"! the mock sets extra, which the prompt's ## Return does not list",
		"✓ note (tool echo) mocked: output=noted",
		"✓ again (repeat judge) Sent the run back to Judge for round 2 of 3",
		"✓ judge (agent judge, round 2 of 3) mocked:",
		"✓ ok (user) mocked: decision=revise note=tighter",
		"✓ ok (user) mocked: decision=approve note=",
		"✗ ship (tool ship) failed: no network",
		"the run ended failed: Ship failed: no network",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestDryRunCanReallyRunChosenSteps(t *testing.T) {
	flowProject(t)
	out := dryRun(t, "hello", "-run", " greet ,")
	if !strings.Contains(out, "except greet, which really ran") || !strings.Contains(out, "✓ greet (tool echo) ran for real: output=hello") {
		t.Errorf("output:\n%s", out)
	}
}

func TestDryRunStopsARunThatGoesRoundForever(t *testing.T) {
	flowProject(t)
	shorten(t, &testBudget, 5)
	out := dryRun(t, "loops", "-mock", `{"ok": "revise: again"}`)
	if !strings.Contains(out, "the run ended failed: OK failed: the test stopped after 5 steps; a mock likely sends the run round forever") {
		t.Errorf("output:\n%s", out)
	}
}

func TestDryRunRefusesWhatItCannotDo(t *testing.T) {
	flowProject(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{nil, "swarm test needs a chain name"},
		{[]string{"ghost"}, "no chain named ghost"},
		{[]string{"broken_ref"}, `fix the chain first: step "a": no tool named "ghost"`},
		{[]string{"needs"}, "X is required"},
		{[]string{"hello", "-mock", "{nope"}, "-mock is a JSON object of step ids"},
		{[]string{"hello", "-mock", "/nowhere/mocks.json"}, "no such file"},
		{[]string{"hello", "-mock", `{"ghost": 1}`}, `-mock names "ghost", which is not a step of this chain`},
		{[]string{"hello", "-run", "ghost"}, `-run names "ghost", which is not a step of this chain`},
		{[]string{"ask", "-run", "ok"}, `-run names "ok", a user step; only tool and agent steps can run for real`},
	} {
		_, err := surfaceCLI(t, "test", c.args...)
		surfaceFails(t, err, c.want)
	}
}

func TestDryRunReportsBadMocks(t *testing.T) {
	flowProject(t)
	for mock, want := range map[string]string{
		`{"judge": "yes"}`:     "Judge failed: the mock for judge is not a JSON object of fields",
		`{"ok": "maybe"}`:      `OK failed: a user step's mock is approve, reject or revise: <note>, not "maybe"`,
		`{"ok": "reject: no"}`: "the run ended rejected: You rejected: OK (no)",
	} {
		out := dryRun(t, "flow", "-input", "topic=x", "-mock", mock)
		if !strings.Contains(out, want) {
			t.Errorf("mock %s: output lacks %q:\n%s", mock, want, out)
		}
	}
	out := dryRun(t, "hello", "-mock", `{"greet": [{"output": "one"}]}`)
	if !strings.Contains(out, "mocked: output=one") {
		t.Errorf("output:\n%s", out)
	}
	revise := dryRun(t, "hello", "-mock", `{"greet": "plain"}`)
	if !strings.Contains(revise, "mocked: output=plain") {
		t.Errorf("output:\n%s", revise)
	}
}
