package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

type Transfer struct {
	Copied  []string `json:"copied"`
	Same    []string `json:"same"`
	Removed []string `json:"removed"`
	Kept    []string `json:"kept"`
}

func (c *Core) TransferChain(from, name, to string, move bool) (*Transfer, error) {
	to, err := c.Project(to)
	if err != nil {
		return nil, err
	}
	if to == from {
		return nil, errors.New("the chain is in that project already; pick another one")
	}
	lib, err := c.Library(from)
	if err != nil {
		return nil, err
	}
	if lib.Chains[name] == nil {
		return nil, fmt.Errorf("no chain named %s", name)
	}
	top := "chains/" + name + ".json"
	if users := lib.usersOf("chains", name); move && len(users) > 0 {
		return nil, &RefusedError{[]string{fmt.Sprintf("%s is used by %s here, so it cannot move", top, strings.Join(users, ", "))}}
	}

	done := &Transfer{Copied: []string{}, Same: []string{}, Removed: []string{}, Kept: []string{}}
	own, write, err := c.planTransfer(lib, from, to, name, done)
	if err != nil {
		return nil, err
	}
	for _, path := range own {
		if raw, needed := write[path]; needed {
			if err := c.PutItem(to, path, "project", raw); err != nil {
				return done, fmt.Errorf("%s: %w", path, err)
			}
			done.Copied = append(done.Copied, path)
		}
	}
	if !move {
		return done, nil
	}
	return done, c.removeMoved(from, own, done)
}

func (c *Core) planTransfer(lib *Library, from, to, name string, done *Transfer) ([]string, map[string][]byte, error) {
	top := "chains/" + name + ".json"
	var own, conflicts []string
	write := map[string][]byte{}
	for _, path := range lib.itemsFor(name) {
		if path != top && lib.layerOf(path) != "project" && !strings.HasPrefix(path, "chains/"+name+"/") {
			continue
		}
		own = append(own, path)
		raw, _, err := c.GetItem(from, path, false)
		if err != nil {
			return nil, nil, err
		}
		there, err := os.ReadFile(filepath.Join(to, ".swarm", path))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			write[path] = raw
		case err != nil:
			return nil, nil, err
		case sameItem(path, raw, there):
			done.Same = append(done.Same, path)
		default:
			conflicts = append(conflicts, fmt.Sprintf("%s already has a different %s", filepath.Base(to), path))
		}
	}
	if len(conflicts) > 0 {
		return nil, nil, &RefusedError{conflicts}
	}
	return own, write, nil
}

func (c *Core) removeMoved(from string, own []string, done *Transfer) error {
	top := own[len(own)-1]
	if err := c.DeleteItem(from, top); err != nil {
		return err
	}
	done.Removed = append(done.Removed, top)
	for _, path := range slices.Backward(own[:len(own)-1]) {
		lib, err := c.Library(from)
		if err != nil {
			return err
		}
		kind, item, _ := splitItem(path)
		if len(lib.usersOf(kind, item)) > 0 {
			done.Kept = append(done.Kept, path)
			continue
		}
		if err := removeItem(filepath.Join(from, ".swarm"), kind, item); err != nil {
			return err
		}
		done.Removed = append(done.Removed, path)
	}
	return nil
}

func (lib *Library) itemsFor(chainName string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(path string) bool {
		if seen[path] {
			return false
		}
		seen[path] = true
		return true
	}
	var walk func(name string)
	walk = func(name string) {
		path := "chains/" + name + ".json"
		chain := lib.Chains[name]
		if !add(path) || chain == nil {
			return
		}
		for _, step := range chain.Steps {
			switch {
			case step.Chain != "":
				walk(step.Chain)
			case step.Tool != "" && add("tools/"+step.Tool+".json"):
				out = append(out, "tools/"+step.Tool+".json")
			case step.Agent != "" && add("chains/"+name+"/"+step.Agent+".md"):
				out = append(out, "chains/"+name+"/"+step.Agent+".md")
			}
		}
		out = append(out, path)
	}
	walk(chainName)
	return out
}

func (lib *Library) layerOf(path string) string {
	kind, name, err := splitItem(path)
	if err != nil {
		return ""
	}
	switch kind {
	case "chains":
		if chain := lib.Chains[name]; chain != nil {
			return chain.Source
		}
	case "tools":
		return lib.Tools[name].Source
	case "prompts":
		owner, _, _ := strings.Cut(name, "/")
		if chain := lib.Chains[owner]; chain != nil {
			return chain.Source
		}
	}
	return ""
}

func sameItem(path string, a, b []byte) bool {
	switch kind, _, _ := splitItem(path); kind {
	case "chains":
		return sameJSON[Chain](a, b)
	case "tools":
		return sameJSON[Tool](a, b)
	}
	return strings.TrimSpace(string(a)) == strings.TrimSpace(string(b))
}

func sameJSON[T any](a, b []byte) bool {
	var x, y T
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}
