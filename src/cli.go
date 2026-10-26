package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type inputFlags map[string]string

func (f inputFlags) String() string { return "" }

func (f inputFlags) Set(v string) error {
	name, value, found := strings.Cut(v, "=")
	if !found {
		return fmt.Errorf("write inputs as name=value, got %q", v)
	}
	f[name] = value
	return nil
}

type cli struct {
	core       *Core
	command    string
	folder     string
	addr       string
	file       string
	scope      string
	note       string
	about      string
	below      bool
	detach     bool
	inputs     inputFlags
	positional []string
}

func runCommand(command string, args []string) error {
	c, err := parseCommand(command, args)
	if err != nil {
		return err
	}
	switch command {
	case "init":
		return c.init()
	case "approve", "revise", "reject":
		return c.decide()
	case "cancel":
		return c.cancel()
	case "show":
		return c.show()
	}
	project, err := c.core.Project(c.folder)
	if err != nil {
		return err
	}
	return c.inProject(project)
}

func parseCommand(command string, args []string) (*cli, error) {
	c := &cli{command: command, inputs: inputFlags{}}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.StringVar(&c.addr, "addr", envOr("SWARM_ADDR", "localhost:7070"), "where swarm serve listens, for page links")
	flags.StringVar(&c.folder, "project", envOr("SWARM_PROJECT", "."), "the project folder")
	flags.BoolVar(&c.below, "below", false, "get: the shared version below this project's copy")
	flags.StringVar(&c.file, "file", "", "put, memory set: read from this file instead of stdin")
	flags.StringVar(&c.scope, "scope", "", "put: project or shared; default is where it already lives, else project")
	flags.StringVar(&c.note, "note", "", "approve, revise, reject: a note for the record; revise needs one")
	flags.StringVar(&c.about, "about", "", "memory add: what the notes came from")
	flags.BoolVar(&c.detach, "detach", false, "run: print the run's id and return; the run keeps going")
	flags.Var(c.inputs, "input", "run: a chain input as name=value; repeat for more")
	for len(args) > 0 {
		name, _, _ := strings.Cut(strings.TrimLeft(args[0], "-"), "=")
		if strings.HasPrefix(args[0], "-") && args[0] != "--" && flags.Lookup(name) == nil {
			c.positional, args = append(c.positional, args[0]), args[1:]
			continue
		}
		if err := flags.Parse(args); err != nil {
			return nil, err
		}
		args = flags.Args()
		if len(args) > 0 {
			c.positional, args = append(c.positional, args[0]), args[1:]
		}
	}
	folder, err := filepath.Abs(c.folder)
	if err != nil {
		return nil, err
	}
	c.folder = folder
	c.core = openCore(defaultHome())
	return c, nil
}

func (c *cli) arg(what string) (string, error) {
	if len(c.positional) == 0 {
		return "", fmt.Errorf("swarm %s needs %s", c.command, what)
	}
	return c.positional[0], nil
}

func (c *cli) inProject(project string) error {
	switch c.command {
	case "list":
		return c.list(project)
	case "check":
		return c.check(project)
	case "get":
		return c.get(project)
	case "put":
		return c.put(project)
	case "rm":
		return c.remove(project)
	case "memory":
		return c.memory(project)
	case "settings":
		return c.settings(project)
	case "runs":
		c.runs(project)
		return nil
	case "run":
		return c.run(project)
	}
	return fmt.Errorf("no command %q", c.command)
}

func refused(err error) error {
	var refusal *RefusedError
	if errors.As(err, &refusal) {
		return fmt.Errorf("refused:\n- %s", strings.Join(refusal.Problems, "\n- "))
	}
	return err
}

func readInput(file string) ([]byte, error) {
	if file != "" {
		return os.ReadFile(file)
	}
	return io.ReadAll(os.Stdin)
}

func (c *cli) init() error {
	if len(c.positional) > 0 {
		folder, err := filepath.Abs(c.positional[0])
		if err != nil {
			return err
		}
		c.folder = folder
	}
	project, err := c.core.AddProject(c.folder)
	if err != nil {
		return err
	}
	if c.core.Memory(project) == "" {
		if err := c.core.SetMemory(project, ""); err != nil {
			return err
		}
	}
	fmt.Printf("%s is a swarm project. Wrote its .swarm/memory.md.\n\n", project)
	if !samplesWritten(c.core.home) {
		fmt.Println("  swarm samples                  write the sample chains, tools, prompts and guide into " + c.core.home)
	}
	fmt.Print(`  swarm guide                    how chains, tools, prompts and memory work
  swarm list                     what this project can run
  swarm run change-swarm -input request="make a chain that ..."
  swarm serve                    the page, to watch and edit in a browser

So agents working here know about swarm, add this line to the project's CLAUDE.md or AGENTS.md:

  This project uses swarm for multi-step agent work. Run "swarm guide" before creating, changing or running a chain.
`)
	return nil
}

func (c *cli) list(project string) error {
	lib, err := c.core.Library(project)
	if err != nil {
		return err
	}
	fmt.Printf("%s: shared from %s, overridden by %s/.swarm\n\n%s", project, c.core.home, project, lib.catalog())
	return nil
}

func (c *cli) check(project string) error {
	problems, lib, err := c.core.Check(project)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "\n"))
	}
	fmt.Printf("ok: %d chains, %d tools, %d prompts\n", len(lib.Chains), len(lib.Tools), len(lib.Prompts))
	printWarnings(lib.allWarnings())
	return nil
}

func printWarnings(warnings []string) {
	if len(warnings) == 0 {
		return
	}
	fmt.Println("warnings:")
	for _, w := range warnings {
		fmt.Println("- " + w)
	}
}

func (c *cli) get(project string) error {
	path, err := c.arg("a path like chains/fix-from-text.json")
	if err != nil {
		return err
	}
	raw, _, err := c.core.GetItem(project, path, c.below)
	if err != nil {
		return err
	}
	fmt.Print(string(raw))
	return nil
}

func (c *cli) put(project string) error {
	path, err := c.arg("a path like chains/my-chain.json")
	if err != nil {
		return err
	}
	content, err := readInput(c.file)
	if err != nil {
		return err
	}
	if err := c.core.PutItem(project, path, c.scope, content); err != nil {
		return refused(err)
	}
	fmt.Println("saved " + path)
	if lib, err := c.core.Library(project); err == nil {
		printWarnings(lib.allWarnings(lib.chainsUsing(path)...))
	}
	return nil
}

func (c *cli) remove(project string) error {
	path, err := c.arg("a path like tools/old_tool.json")
	if err != nil {
		return err
	}
	if err := c.core.DeleteItem(project, path); err != nil {
		return refused(err)
	}
	fmt.Println("deleted " + path)
	return nil
}

func (c *cli) memory(project string) error {
	if len(c.positional) == 0 {
		fmt.Print(c.core.Memory(project))
		return nil
	}
	switch c.positional[0] {
	case "add":
		added, err := c.core.RememberNotes(project, c.about, c.positional[1:])
		if err != nil {
			return err
		}
		fmt.Printf("remembered %d note(s)\n", added)
	case "set":
		text, err := readInput(c.file)
		if err != nil {
			return err
		}
		if err := c.core.SetMemory(project, string(text)); err != nil {
			return err
		}
		fmt.Println("memory replaced")
	default:
		return fmt.Errorf("swarm memory takes add or set, not %q", c.positional[0])
	}
	return nil
}

func (c *cli) settings(project string) error {
	view, err := c.core.Settings(project)
	if err != nil {
		return err
	}
	fmt.Printf("agent steps run with %s unless a step sets runner\n", view.Default)
	fmt.Printf("agents: %s\n", strings.Join(view.Runners, ", "))
	for _, name := range builtinRunners {
		if path, found := view.Installed[name]; found {
			fmt.Printf("  %s: %s\n", name, path)
		} else {
			fmt.Printf("  %s: not on PATH\n", name)
		}
	}
	layers := []struct {
		label    string
		settings *Settings
	}{{"shared, " + c.core.home + "/settings.json", view.Shared}, {"this project, .swarm/settings.json", view.Project}}
	for _, layer := range layers {
		if layer.settings == nil {
			continue
		}
		raw, err := json.MarshalIndent(layer.settings, "", "  ")
		if err != nil {
			return err
		}
		fmt.Printf("\n%s:\n%s\n", layer.label, raw)
	}
	return nil
}
