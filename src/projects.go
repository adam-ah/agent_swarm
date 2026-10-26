package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

func (c *Core) AddProject(folder string) (string, error) {
	return c.openProject(folder, true)
}

func (c *Core) Projects() ([]string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	known := c.saved()
	for _, p := range c.session {
		if !slices.Contains(known, p) {
			known = append(known, p)
		}
	}
	fallback := c.fallback
	if fallback == "" && len(known) > 0 {
		fallback = known[0]
	}
	return known, fallback
}

func (c *Core) Project(requested string) (string, error) {
	known, fallback := c.Projects()
	if requested == "" {
		requested = fallback
	}
	if !slices.Contains(known, requested) {
		return "", fmt.Errorf("%s is not a swarm project yet; run swarm init in it", requested)
	}
	return requested, nil
}

func (c *Core) openSession(folder string) error {
	abs, err := c.openProject(folder, false)
	c.fallback = abs
	return err
}

func (c *Core) openProject(folder string, remember bool) (string, error) {
	abs, err := filepath.Abs(folder)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(abs); statErr != nil || !info.IsDir() {
		return "", fmt.Errorf("%s is not a folder", abs)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !remember {
		if !slices.Contains(c.session, abs) {
			c.session = append(c.session, abs)
		}
		return abs, nil
	}
	saved := c.saved()
	if slices.Contains(saved, abs) {
		return abs, nil
	}
	return abs, saveJSON(filepath.Join(c.home, "projects.json"), append(saved, abs))
}

func (c *Core) saved() []string {
	var saved []string
	_ = readJSON(filepath.Join(c.home, "projects.json"), &saved)
	return saved
}
