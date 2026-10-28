package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestKindOf(t *testing.T) {
	tests := map[string]string{
		"a.mp4": "video", "a.WEBM": "video", "a.mov": "video",
		"a.png": "image", "a.jpg": "image", "a.jpeg": "image", "a.gif": "image", "a.webp": "image",
		"a.md": "markdown", "a.diff": "diff", "a.patch": "diff", "a.json": "json",
		"a.txt": "text", "Makefile": "text",
	}
	for path, want := range tests {
		if got := kindOf(path); got != want {
			t.Errorf("kindOf(%s) = %s, want %s", path, got, want)
		}
	}
}

func TestArtifactsMatchEachFileOnce(t *testing.T) {
	project := t.TempDir()
	writeFiles(t, project, map[string]string{"a.md": "", "b.png": "", "c.txt": "", "sub/d.json": ""})
	sc := newScope(map[string]string{"dir": project})
	r := &Runner{}
	found := r.artifacts(project, sc, []string{"*.md", "{{dir}}/*.png", "*", "sub/*.json", "*.md"})
	got := make([]string, 0, len(found))
	for _, a := range found {
		got = append(got, a.Name+":"+a.Kind)
		if !filepath.IsAbs(a.Path) {
			t.Errorf("%s is not an absolute path", a.Path)
		}
	}
	if want := []string{"a.md:markdown", "b.png:image", "c.txt:text", "d.json:json"}; !reflect.DeepEqual(got, want) {
		t.Errorf("artifacts = %v, want %v", got, want)
	}
}

func TestToolFailureReasons(t *testing.T) {
	tests := []struct {
		name string
		run  string
		want string
	}{
		{"last three lines of stderr", "printf '1\\n2\\n3\\n4\\n5\\n' >&2; exit 1", "3\n4\n5"},
		{"stdout when stderr is empty", "echo only out; exit 1", "only out"},
		{"the exit when it printed nothing", "exit 4", "exit status 4; it printed nothing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lib := sequenceLibrary(t)
			lib.Tools["fail"] = Tool{Run: tt.run}
			run, _ := execute(t, lib, &Chain{Name: "main", Steps: []Step{{ID: "fail", Title: "Fail", Tool: "fail"}}})
			if step := run.Steps[0]; step.Status != "failed" || step.Error != tt.want {
				t.Errorf("step = %s %q, want %q", step.Status, step.Error, tt.want)
			}
		})
	}
}

func TestToolFailsWhenItCannotStart(t *testing.T) {
	lib := sequenceLibrary(t)
	lib.Project = filepath.Join(lib.Project, "missing")
	run, _ := execute(t, lib, &Chain{Name: "main", Steps: []Step{{ID: "say", Title: "Say", Tool: "say", With: map[string]string{"value": "x"}}}})
	if step := run.Steps[0]; step.Status != "failed" || !strings.HasSuffix(step.Error, "; it printed nothing") {
		t.Errorf("step = %s %q", step.Status, step.Error)
	}
}

func TestToolHeadlineIsItsSummaryOrLastLine(t *testing.T) {
	lib := sequenceLibrary(t)
	lib.Tools["json"] = Tool{Run: `echo '{"summary": "from json"}'`}
	lib.Tools["lines"] = Tool{Run: "echo first; echo; echo last"}
	run, _ := execute(t, lib, &Chain{Name: "main", Steps: []Step{{ID: "json", Title: "J", Tool: "json"}, {ID: "lines", Title: "L", Tool: "lines"}}})
	if run.Steps[0].Headline != "from json" || run.Steps[1].Headline != "last" {
		t.Errorf("headlines = %q, %q", run.Steps[0].Headline, run.Steps[1].Headline)
	}
}

func TestStoppingAWaitingRun(t *testing.T) {
	shorten(t, &listenEvery, 10*time.Millisecond)
	core, project := engineSetup(t)
	run := queueRun(t, core, project, "ask", nil)
	errs := runWork(t, core, run.ID)
	awaitRun(t, core, run.ID, stepWaiting(0))
	if err := core.Stop(run.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errs:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the waiting run did not stop")
	}
	done := core.store.load(run.ID)
	if done.Status != "stopped" || done.Steps[1].Status != "failed" || done.Steps[1].Error != "stopped" {
		t.Errorf("run = %s %q, check = %s %q", done.Status, done.Reason, done.Steps[1].Status, done.Steps[1].Error)
	}
}

func TestLineHelpers(t *testing.T) {
	if got := firstLine("\n  \n==> one\ntwo"); got != "one" {
		t.Errorf("firstLine = %q", got)
	}
	if got := firstLine(" \n "); got != "" {
		t.Errorf("firstLine of blanks = %q", got)
	}
	if got := lastLines("a\nb\nc", 2); got != "b\nc" {
		t.Errorf("lastLines = %q", got)
	}
	if got := lastLine(""); got != "" {
		t.Errorf("lastLine of nothing = %q", got)
	}
}
