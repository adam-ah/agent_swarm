package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func originRequest(srv *server, method, target, host, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader("{}"))
	req.Host = host
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	return rec
}

func TestServerRefusesCrossOriginRequests(t *testing.T) {
	core, _ := surfaceProject(t)
	srv := &server{core: core, quit: make(chan struct{})}
	for _, origin := range []string{"https://evil.example", "http://127.0.0.1:7071", "null", "http://[::1"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			rec := originRequest(srv, method, "/api/runs", "127.0.0.1:7070", origin)
			surfaceExpect(t, rec, http.StatusForbidden, "cross-origin requests are not allowed")
		}
	}
	for _, origin := range []string{"", "http://127.0.0.1:7070", "HTTP://127.0.0.1:7070"} {
		surfaceExpect(t, originRequest(srv, http.MethodGet, "/api/projects", "127.0.0.1:7070", origin), http.StatusOK, "projects")
	}
}

func TestServerOnLoopbackRefusesOtherHosts(t *testing.T) {
	core, _ := surfaceProject(t)
	srv := &server{core: core, quit: make(chan struct{}), loopbackOnly: true}
	for _, host := range []string{"evil.example:7070", "192.168.1.5:7070", "localhost.evil.example"} {
		rec := originRequest(srv, http.MethodPost, "/api/runs", host, "http://"+host)
		surfaceExpect(t, rec, http.StatusForbidden, "answers only localhost and loopback addresses")
	}
	for _, host := range []string{"127.0.0.1:7070", "localhost:7070", "[::1]:7070", "app.localhost", "127.0.0.2"} {
		surfaceExpect(t, originRequest(srv, http.MethodGet, "/api/projects", host, "http://"+host), http.StatusOK, "projects")
	}
	open := &server{core: core, quit: make(chan struct{})}
	surfaceExpect(t, originRequest(open, http.MethodGet, "/api/projects", "devbox:7070", "http://devbox:7070"), http.StatusOK, "projects")
}

func TestServerLogsRefusedOrigin(t *testing.T) {
	core, _ := surfaceProject(t)
	var out strings.Builder
	srv := &server{core: core, quit: make(chan struct{}), log: newConsole(&out)}
	originRequest(srv, http.MethodPost, "/api/runs", "127.0.0.1:7070", "https://evil.example")
	if !strings.Contains(out.String(), `POST /api/runs 403`) || !strings.Contains(out.String(), `refused Origin "https://evil.example"`) {
		t.Errorf("log = %q", out.String())
	}
}
