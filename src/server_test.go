package main

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func TestServerStaticPageAndShutdown(t *testing.T) {
	core, _ := surfaceProject(t)
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, "/", nil), http.StatusOK, "<html")

	srv := &server{core: core, quit: make(chan struct{})}
	rec := surfaceRecord(srv, http.MethodPost, "/api/shutdown", nil)
	surfaceExpect(t, rec, http.StatusOK, `"ok":"true"`)
	select {
	case <-srv.quit:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not close quit")
	}
}

func TestServerProjects(t *testing.T) {
	core, project := surfaceProject(t)
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, "/api/projects", nil), http.StatusOK, project)
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPost, "/api/projects", strings.NewReader("{")), http.StatusBadRequest, "error")
	missing := filepath.Join(t.TempDir(), "missing")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPost, "/api/projects", strings.NewReader(`{"path": "`+missing+`"}`)), http.StatusBadRequest, "is not a folder")
	other := t.TempDir()
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPost, "/api/projects", strings.NewReader(`{"path": "`+other+`"}`)), http.StatusOK, other)
	if projects, _ := core.Projects(); len(projects) != 2 {
		t.Errorf("projects = %v, want two", projects)
	}
}

func TestServerRoutesNeedAKnownProject(t *testing.T) {
	core, _ := surfaceProject(t)
	routes := [][2]string{
		{http.MethodGet, "/api/library"}, {http.MethodGet, "/api/catalog"}, {http.MethodGet, "/api/settings"}, {http.MethodGet, "/api/check"},
		{http.MethodGet, "/api/items/chains/hello.json"}, {http.MethodPut, "/api/items/chains/hello.json"}, {http.MethodDelete, "/api/items/chains/hello.json"},
		{http.MethodPost, "/api/chains/hello/transfer"}, {http.MethodGet, "/api/memory"}, {http.MethodPut, "/api/memory"},
		{http.MethodPost, "/api/memory/notes"}, {http.MethodGet, "/api/runs"}, {http.MethodPost, "/api/runs"},
	}
	for _, route := range routes {
		rec := surfaceRequest(t, core, route[0], route[1]+"?project="+url.QueryEscape("/nowhere"), strings.NewReader("{}"))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "is not a swarm project") {
			t.Errorf("%s %s = %d %s", route[0], route[1], rec.Code, rec.Body.String())
		}
	}
}

func TestServerReportsABrokenLibrary(t *testing.T) {
	core, project := surfaceProject(t)
	surfaceFiles(t, project, map[string]string{".swarm/settings.json": "{"})
	for _, path := range []string{"/api/library", "/api/catalog", "/api/settings", "/api/check"} {
		surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, path, nil), http.StatusBadRequest, "error")
	}
}

func TestServerLibraryViews(t *testing.T) {
	core, project := surfaceProject(t)
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, "/api/library", nil), http.StatusOK, `"default_runner":"claude"`)
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, "/api/catalog?project="+url.QueryEscape(project), nil), http.StatusOK, "hello")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, "/api/settings", nil), http.StatusOK, `"default":"claude"`)
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, "/api/check", nil), http.StatusOK, `"chains":3`)
}

func TestServerItems(t *testing.T) {
	core, _ := surfaceProject(t)
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, "/api/items/chains/hello.json", nil), http.StatusOK, `"source":"project"`)
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, "/api/items/chains/hello.json?below=1", nil), http.StatusBadRequest, "error")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, "/api/items/settings.json", nil), http.StatusBadRequest, "error")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPut, "/api/items/settings.json?scope=project", strings.NewReader(`{"agent": "codex"}`)), http.StatusOK, "ok")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, "/api/items/settings.json", nil), http.StatusOK, "codex")

	surfaceExpect(t, surfaceRequest(t, core, http.MethodPut, "/api/items/tools/extra.json", strings.NewReader(`{"title": "Extra", "run": "true"}`)), http.StatusOK, "ok")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPut, "/api/items/tools/extra.json", strings.NewReader(`{"title": ""}`)), http.StatusUnprocessableEntity, "a tool needs a title")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPut, "/api/items/tools/extra.json", iotest.ErrReader(errors.New("body broke"))), http.StatusBadRequest, "body broke")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPut, "/api/items/nonsense/x", strings.NewReader("x")), http.StatusBadRequest, "is not")

	surfaceExpect(t, surfaceRequest(t, core, http.MethodDelete, "/api/items/tools/echo.json", nil), http.StatusUnprocessableEntity, "still used by")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodDelete, "/api/items/tools/extra.json", nil), http.StatusOK, "ok")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodDelete, "/api/items/tools/extra.json", nil), http.StatusBadRequest, "nothing at")
}

func TestServerTransferChain(t *testing.T) {
	core, _ := surfaceProject(t)
	other := t.TempDir()
	if _, err := core.AddProject(other); err != nil {
		t.Fatal(err)
	}
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPost, "/api/chains/hello/transfer", strings.NewReader("[")), http.StatusBadRequest, "error")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPost, "/api/chains/hello/transfer", strings.NewReader(`{"to": "/nowhere"}`)), http.StatusBadRequest, "not a swarm project")
	body := strings.NewReader(`{"to": "` + other + `"}`)
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPost, "/api/chains/hello/transfer", body), http.StatusOK, "chains/hello.json")
	if _, err := os.Stat(filepath.Join(other, ".swarm", "chains", "hello.json")); err != nil {
		t.Error(err)
	}
}

func TestServerMemory(t *testing.T) {
	core, project := surfaceProject(t)
	surfaceExpect(t, surfaceRequest(t, core, http.MethodGet, "/api/memory", nil), http.StatusOK, "memory.md")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPut, "/api/memory", strings.NewReader("[")), http.StatusBadRequest, "error")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPut, "/api/memory", strings.NewReader(`{"text": "# Notes\n"}`)), http.StatusOK, "ok")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPost, "/api/memory/notes", strings.NewReader("[")), http.StatusBadRequest, "error")
	notes := strings.NewReader(`{"about": "a test", "notes": ["- keep it short", " "]}`)
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPost, "/api/memory/notes", notes), http.StatusOK, `"added":1`)
	if text := core.Memory(project); !strings.Contains(text, "# Notes") || !strings.Contains(text, "· a test\n- keep it short") {
		t.Errorf("memory = %q", text)
	}

	if err := os.Remove(memoryPath(project)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(memoryPath(project), "blocked"), 0o755); err != nil {
		t.Fatal(err)
	}
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPut, "/api/memory", strings.NewReader(`{"text": "x"}`)), http.StatusBadRequest, "error")
	surfaceExpect(t, surfaceRequest(t, core, http.MethodPost, "/api/memory/notes", strings.NewReader(`{"notes": ["x"]}`)), http.StatusBadRequest, "error")
}
