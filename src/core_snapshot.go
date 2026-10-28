package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func swarmDir(project string) string {
	return filepath.Join(project, ".swarm")
}

func copyTree(src, dest string) (int, error) {
	if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
		return 0, os.MkdirAll(dest, 0o755)
	}
	tree := os.DirFS(src)
	if err := os.CopyFS(dest, tree); err != nil {
		return 0, err
	}
	count := 0
	err := fs.WalkDir(tree, ".", func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			count++
		}
		return err
	})
	return count, err
}

func (c *Core) Snapshot(project, dest string) (int, error) {
	if _, err := os.Stat(dest); err == nil {
		return 0, fmt.Errorf("%s already exists; save into a new folder", dest)
	}
	return copyTree(swarmDir(project), dest)
}

func (c *Core) Restore(project, from string) error {
	if info, err := os.Stat(from); err != nil || !info.IsDir() {
		return fmt.Errorf("nothing was saved in %s; .swarm is left as it is", from)
	}
	dir := swarmDir(project)
	fresh, old := dir+".restoring", dir+".replaced"
	for _, path := range []string{fresh, old} {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	if _, err := copyTree(from, fresh); err != nil {
		return err
	}
	if err := os.Rename(dir, old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(fresh, dir); err != nil {
		return err
	}
	return os.RemoveAll(old)
}

func (c *Core) Diff(project, before string) (string, int, error) {
	if _, err := os.Stat(before); err != nil {
		return "", 0, fmt.Errorf("nothing was saved in %s", before)
	}
	work, err := os.MkdirTemp("", "swarm-diff-")
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = os.RemoveAll(work) }()
	base, after := filepath.Join(work, "before"), filepath.Join(work, "after")
	if _, err = copyTree(before, base); err != nil {
		return "", 0, err
	}
	if _, err = copyTree(swarmDir(project), after); err != nil {
		return "", 0, err
	}
	if err = c.addOverridden(base, after); err != nil {
		return "", 0, err
	}
	cmd := exec.Command("diff", "-ruN", "before", "after")
	cmd.Dir = work
	out, err := cmd.Output()
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		return "", 0, fmt.Errorf("diff failed: %w", err)
	}
	files := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "diff ") {
			files++
		}
	}
	return string(out), files, nil
}

func (c *Core) addOverridden(base, after string) error {
	return fs.WalkDir(os.DirFS(after), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !c.overrides(base, rel) {
			return err
		}
		shared, err := os.ReadFile(filepath.Join(c.home, rel))
		if err != nil {
			return err
		}
		return writeFile(filepath.Join(base, rel), shared)
	})
}

func (c *Core) overrides(base, rel string) bool {
	if kind, _, err := splitItem(rel); err != nil || kind == "settings" {
		return false
	}
	_, inBase := os.Stat(filepath.Join(base, rel))
	_, inShared := os.Stat(filepath.Join(c.home, rel))
	return inBase != nil && inShared == nil
}
