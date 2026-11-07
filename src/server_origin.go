package main

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func (s *server) sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.crossOrigin(r); err != nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) crossOrigin(r *http.Request) error {
	if s.loopbackOnly && !loopbackHost(r.Host) {
		return fmt.Errorf("refused Host %q: swarm serve listens on loopback, so it answers only localhost and loopback addresses", r.Host)
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	if parsed, err := url.Parse(origin); err != nil || !strings.EqualFold(parsed.Host, r.Host) {
		return fmt.Errorf("refused Origin %q: cross-origin requests are not allowed (Host %q)", origin, r.Host)
	}
	return nil
}

func loopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = strings.Trim(hostport, "[]")
	}
	host = strings.ToLower(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
