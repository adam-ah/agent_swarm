package main

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func surfaceDispatch(t *testing.T, command string, args ...string) (string, error) {
	t.Helper()
	return surfaceOutput(t, func() error { return dispatch(command, args) })
}

func surfaceChdir(t *testing.T, dir string) {
	t.Helper()
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(before); err != nil {
			t.Error(err)
		}
	})
}

func TestOverviewOfAFreshFolder(t *testing.T) {
	surfaceProject(t)
	surfaceChdir(t, t.TempDir())
	out, err := surfaceDispatch(t, "overview")
	surfaceContains(t, out, err, "✗ no sample chains yet", "is not a swarm project", "✗ the page is not running", "✓ claude is installed (",
		"✗ codex is not on PATH", "agent steps run with claude", "swarm samples ", "swarm init ", "swarm serve ")
}

func TestOverviewOfAProjectWithThePageUp(t *testing.T) {
	core, project := surfaceProject(t)
	surfaceFiles(t, core.home, map[string]string{"GUIDE.txt": "guide"})
	surfaceFiles(t, project, map[string]string{".swarm/settings.json": `{"agent": "pi", "agents": {"sonnet": {"runs": "claude", "args": ["--model", "sonnet"]}, "pi": {"command": "pi -p"}}}`})
	addr := surfaceServer(t, core)
	t.Setenv("SWARM_ADDR", addr)
	surfaceChdir(t, project)
	out, err := surfaceDispatch(t, "overview")
	surfaceContains(t, out, err, "✓ sample chains, tools, prompts and guide are in", project+" is a swarm project", "✓ the page is running on http://"+addr,
		"✓ sonnet runs claude --model sonnet", "✓ pi is set up as a custom agent: pi -p", "agent steps run with pi", "swarm list ", "open              http://"+addr, "swarm stop ")
	if strings.Contains(out, "swarm samples ") || strings.Contains(out, "swarm init ") {
		t.Errorf("suggests what is done already:\n%s", out)
	}
}

func TestDispatchPrintsGuideAndHelp(t *testing.T) {
	surfaceProject(t)
	out, err := surfaceDispatch(t, "guide")
	if err != nil || out != guideText() || out == "" {
		t.Errorf("guide = %d bytes, %v", len(out), err)
	}
	for _, command := range []string{"help", "-h", "--help"} {
		if help, helpErr := surfaceDispatch(t, command); helpErr != nil || help != usage {
			t.Errorf("%s = %q, %v", command, help, helpErr)
		}
	}
	_, err = surfaceDispatch(t, "nope")
	surfaceFails(t, err, "no command \"nope\"\n\nswarm runs chains")
	out, err = surfaceDispatch(t, "list")
	surfaceContains(t, out, err, "hello")
	_, err = surfaceDispatch(t, "_run", "-home", t.TempDir(), "not-a-run")
	surfaceFails(t, err, `"not-a-run" is not a run id`)
}

func TestDispatchSamples(t *testing.T) {
	core, _ := surfaceProject(t)
	out, err := surfaceDispatch(t, "samples")
	surfaceContains(t, out, err, "wrote "+filepath.Join(core.home, "GUIDE.txt"))
	if strings.Contains(out, "untouched") {
		t.Errorf("first samples kept files: %s", out)
	}
	out, err = surfaceDispatch(t, "samples")
	surfaceContains(t, out, err, "file(s) you already have untouched")

	blocked := filepath.Join(t.TempDir(), "home")
	surfaceFiles(t, filepath.Dir(blocked), map[string]string{"home": "a file"})
	t.Setenv("SWARM_HOME", blocked)
	_, err = surfaceDispatch(t, "samples")
	surfaceFails(t, err, "not a directory")
}

func TestDefaultHome(t *testing.T) {
	surfaceProject(t)
	t.Setenv("SWARM_HOME", "")
	t.Setenv("HOME", "/nowhere")
	if home := defaultHome(); home != "/nowhere/.swarm" {
		t.Errorf("home = %s", home)
	}
	t.Setenv("HOME", "")
	if home := defaultHome(); home != ".swarm" {
		t.Errorf("home without $HOME = %s", home)
	}
}

func TestServeRefusesWithoutATerminal(t *testing.T) {
	surfaceProject(t)
	out, err := surfaceDispatch(t, "serve", "-addr", surfaceClosedAddr)
	surfaceFails(t, err, "refused to start outside a terminal")
	if !strings.Contains(out, "This is not a terminal") {
		t.Errorf("no reason given: %s", out)
	}
	_, err = surfaceDispatch(t, "serve", "-bogus")
	surfaceFails(t, err, "flag provided but not defined")
	t.Setenv("SWARM_SERVE_ANYWAY", "1")
	_, err = surfaceDispatch(t, "serve", "-addr", surfaceClosedAddr, "-project", filepath.Join(t.TempDir(), "missing"))
	surfaceFails(t, err, "is not a folder")
}

func TestServeReportsAnAddressItCannotUse(t *testing.T) {
	surfaceProject(t)
	t.Setenv("SWARM_SERVE_ANYWAY", "1")
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	_, err := surfaceDispatch(t, "serve", "-addr", "no-port")
	surfaceFails(t, err, "missing port in address")
}

func TestStopWhenNothingRuns(t *testing.T) {
	surfaceProject(t)
	out, err := surfaceDispatch(t, "stop")
	surfaceContains(t, out, err, "the swarm page is not running on "+surfaceClosedAddr)
	out, err = surfaceDispatch(t, "stop", "-addr", "bad host")
	surfaceContains(t, out, err, "not running on bad host")
	_, err = surfaceDispatch(t, "stop", "0101-000000")
	surfaceFails(t, err, "use swarm cancel 0101-000000")
	_, err = surfaceDispatch(t, "stop", "-bogus")
	surfaceFails(t, err, "flag provided but not defined")
	t.Setenv("SWARM_ADDR", "")
	out, err = surfaceDispatch(t, "stop", "-addr", surfaceClosedAddr)
	surfaceContains(t, out, err, "not running on "+surfaceClosedAddr)
}

func TestStopWaitsForThePageToClose(t *testing.T) {
	core, _ := surfaceProject(t)
	srv := &server{core: core, quit: make(chan struct{})}
	page := httptest.NewServer(srv.routes())
	defer page.Close()
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		<-srv.quit
		time.Sleep(30 * time.Millisecond)
		page.Close()
	}()
	out, err := surfaceDispatch(t, "stop", "-addr", strings.TrimPrefix(page.URL, "http://"))
	surfaceContains(t, out, err, "swarm page stopped; runs keep going")
	<-closed
}

func TestStopReportsARefusal(t *testing.T) {
	surfaceProject(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/projects", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, map[string]string{}) })
	mux.HandleFunc("POST /api/shutdown", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "not now"})
	})
	page := httptest.NewServer(mux)
	defer page.Close()
	_, err := surfaceDispatch(t, "stop", "-addr", strings.TrimPrefix(page.URL, "http://"))
	surfaceFails(t, err, "not now")
}
