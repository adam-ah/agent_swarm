package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestCheckDraftJudgesUnsavedChainsToolsAndPrompts(t *testing.T) {
	core, project := surfaceProject(t)
	surfaceFiles(t, project, map[string]string{
		".swarm/chains/ask/review.md": "Review it.\n\n## Return\n\n- `verdict`: `APPROVE` or `CHANGES`\n",
	})
	verdicts, err := core.CheckDraft(project, Draft{
		Chains: map[string]json.RawMessage{
			"hello": json.RawMessage(`{"title": "Hello", "steps": [{"id": "greet", "title": "Greet", "tool": "ghost"}]}`),
			"ask":   json.RawMessage(`{"title": "Ask", "steps": [{"id": "look", "title": "Look", "agent": "review"}, {"id": "again", "title": "Again", "repeat": "look", "when": "look.verdict == CHANGES"}]}`),
			"bad":   json.RawMessage(`{"title": "Bad", "stepz": []}`),
		},
		Prompts: map[string]string{"ask/review": "Look again.\n\n## Return\n\n- `verdict`: `APPROVE` or `CHANGES`\n- `notes`: what to fix\n", "ask/empty": " "},
		Tools:   map[string]Tool{"echo": {Title: "Echo", Run: "echo {{who}}"}, "blank": {}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]*Verdict{
		"chains/hello.json":   {Problems: []string{`step "greet": no tool named "ghost"`}, Warnings: []string{}},
		"chains/ask.json":     {Problems: []string{}, Warnings: []string{`step "look" returns notes, but nothing reads {{look.notes}}`}},
		"chains/bad.json":     {Problems: []string{`not a valid chain: json: unknown field "stepz"`}},
		"chains/ask/empty.md": {Problems: []string{"a prompt cannot be empty"}},
		"tools/blank.json":    {Problems: []string{"a tool needs a title", "a tool needs a run command"}},
	}
	if !reflect.DeepEqual(verdicts, want) {
		for path, v := range verdicts {
			t.Errorf("%s: %+v", path, *v)
		}
	}
}

func TestCheckDraftReachesChainsThatUseAChangedTool(t *testing.T) {
	core, project := surfaceProject(t)
	verdicts, err := core.CheckDraft(project, Draft{Tools: map[string]Tool{"echo": {Title: "Echo", Run: "echo {{who}}"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := verdicts["chains/hello.json"].Warnings; len(got) != 1 || !strings.Contains(got[0], "{{who}}, which nothing fills") {
		t.Errorf("hello warnings = %v", got)
	}
	if _, broken := verdicts["chains/broken.json"]; broken || len(verdicts) != 2 {
		t.Errorf("verdicts reach %v, want hello and ask only", verdicts)
	}
}

func TestCheckDraftRoute(t *testing.T) {
	core, project := surfaceProject(t)
	target := "/api/check?project=" + url.QueryEscape(project)
	rec := surfaceRequest(t, core, http.MethodPost, target, strings.NewReader(`{"tools": {"echo": {"title": "", "run": "echo"}}}`))
	surfaceExpect(t, rec, http.StatusOK, `"tools/echo.json":{"problems":["a tool needs a title"]`)
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPost, target, strings.NewReader("{")), http.StatusBadRequest, "unexpected EOF")
}

func TestRemovePrintsWarningsForChainsThatUsedIt(t *testing.T) {
	core, project := surfaceProject(t)
	surfaceFiles(t, core.home, map[string]string{"tools/echo.json": `{"title": "Echo", "run": "echo {{who}}"}`})
	out, err := surfaceCLI(t, "rm", "tools/echo.json", "-project", project)
	surfaceContains(t, out, err, "deleted tools/echo.json", "warnings:", "hello (project): step \"greet\": tools/echo.json reads {{who}}, which nothing fills")
}
