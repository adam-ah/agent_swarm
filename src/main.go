package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const usage = `swarm runs chains of agent, tool and user steps. Read "swarm guide" first. Every change is
validated before saving; * marks commands that write.

  swarm guide                     how chains, tools, prompts, settings and memory work
  swarm samples                 * write the samples and guide into ~/.swarm; keeps your files,
                                  replaces GUIDE.txt
  swarm init [DIR]              * make a folder a project

  swarm list                      chains, tools and prompts this project can use
  swarm get PATH [-below]         print chains/<c>.json, chains/<c>/<prompt>.md, tools/<t>.json or
                                  settings.json; -below: the shared one a project copy overrides
  swarm put PATH [-file F] [-scope project|shared]
                                * save from F or stdin; refused with reasons if invalid
  swarm rm PATH                 * delete; refused while a chain uses it
  swarm check                     validate everything and list warnings
  swarm settings                  configured and installed agents

  swarm snapshot DIR            * copy .swarm into DIR
  swarm diff DIR [FILE]           .swarm changes since that copy
  swarm restore DIR             * put that copy back

  swarm memory                    print memory.md
  swarm memory add [-about TEXT] NOTE...
                                * add notes
  swarm memory set [-file F]    * replace it

  swarm run CHAIN [-input name=value]... [-detach]
                                * start a run in the background and print steps as they end;
                                  stopping the command leaves the run going
  swarm runs                      recent runs
  swarm show RUN                  a run's steps
  swarm approve RUN [-note T]   * answer the user step a run waits on
  swarm revise RUN -note T      * send the run back to the step's revise target with the note
  swarm reject RUN [-note T]    *
  swarm cancel RUN              * stop a run; "always" steps still run

  swarm serve                     the page, on 0.0.0.0:7070
  swarm stop                      close the page; runs keep going

-project DIR: the project (default $SWARM_PROJECT, else the current folder). $SWARM_HOME
replaces ~/.swarm. -addr HOST:PORT for serve and stop (default $SWARM_ADDR).
`

func main() {
	command := "overview"
	args := os.Args[1:]
	if len(args) > 0 {
		command, args = args[0], args[1:]
	}
	if err := dispatch(command, args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func dispatch(command string, args []string) error {
	switch command {
	case "overview":
		overview()
		return nil
	case "serve":
		return serve(args)
	case "guide":
		fmt.Print(guideText())
		return nil
	case "samples":
		return samples()
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	case "stop":
		return stopServer(args)
	case "_run":
		return work(args)
	case "init", "list", "get", "put", "rm", "check", "memory", "settings", "run", "runs", "show", "approve", "revise", "reject", "cancel",
		"snapshot", "diff", "restore":
		return runCommand(command, args)
	}
	return fmt.Errorf("no command %q\n\n%s", command, usage)
}

func defaultHome() string {
	if home := os.Getenv("SWARM_HOME"); home != "" {
		return home
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".swarm")
	}
	return ".swarm"
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func pageRequest(method, addr, path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, "http://"+addr+path, http.NoBody)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	var body struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(res.Body).Decode(&body)
	if res.StatusCode >= http.StatusMultipleChoices {
		return errors.New(body.Error)
	}
	return nil
}

func pageUp(addr string) bool {
	return pageRequest(http.MethodGet, addr, "/api/projects") == nil
}

func overview() {
	home := defaultHome()
	folder, _ := os.Getwd()
	addr := envOr("SWARM_ADDR", "localhost:7070")
	core := openCore(home)
	_, projectErr := core.Project(folder)
	isProject := projectErr == nil
	serving := pageUp(addr)

	fmt.Print("swarm runs chains of agent, tool and user steps; swarm serve shows every run live on a page.\n\n")
	state := func(done bool, yes, no string) {
		mark := "✗"
		if done {
			mark = "✓"
		}
		fmt.Printf("  %s %s\n", mark, map[bool]string{true: yes, false: no}[done])
	}
	state(samplesWritten(home), "sample chains, tools, prompts and guide are in "+home, "no sample chains yet in "+home)
	state(isProject, folder+" is a swarm project", folder+" is not a swarm project")
	state(serving, "the page is running on http://"+addr, "the page is not running")
	settings, _ := loadSettings(home, folder)
	installed := installedRunners()
	for _, name := range builtinRunners {
		path, found := installed[name]
		state(found, name+" is installed ("+path+")", name+" is not on PATH")
	}
	for _, name := range settings.runners() {
		if !isBuiltinRunner(name) {
			cli := settings.Agents[name]
			if cli.Runs != "" {
				fmt.Printf("  ✓ %s runs %s %s\n", name, cli.Runs, strings.Join(cli.Args, " "))
			} else {
				fmt.Printf("  ✓ %s is set up as a custom agent: %s\n", name, cli.Command)
			}
		}
	}
	fmt.Printf("  agent steps run with %s unless a step says otherwise (see swarm guide, \"AGENTS\")\n", settings.defaultRunner())

	fmt.Print("\nNothing has been written. Next:\n\n")
	fmt.Println("  swarm guide       read how it works")
	if !samplesWritten(home) {
		fmt.Println("  swarm samples     write the sample chains, tools, prompts and guide into " + home)
	}
	if !isProject {
		fmt.Println("  swarm init        make this folder a swarm project")
	} else {
		fmt.Println("  swarm list        what this project can run")
	}
	if serving {
		fmt.Println("  open              http://" + addr)
		fmt.Println("  swarm stop        close the page; runs keep going")
	} else {
		fmt.Println("  swarm serve       the page, in a terminal you keep open")
	}
	fmt.Println("  swarm help        every command")
}

func samples() error {
	written, kept, err := writeSamples(defaultHome())
	if err != nil {
		return err
	}
	for _, f := range written {
		fmt.Println("wrote " + filepath.Join(defaultHome(), f))
	}
	if len(kept) > 0 {
		fmt.Printf("left %d file(s) you already have untouched\n", len(kept))
	}
	return nil
}

func serve(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := flags.String("addr", envOr("SWARM_ADDR", "0.0.0.0:7070"), "where to serve")
	home := flags.String("home", defaultHome(), "the shared library, run history and project list")
	project := flags.String("project", envOr("SWARM_PROJECT", "."), "the folder the page opens on, kept for this session without adding it to the project list")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !inTerminal() && os.Getenv("SWARM_SERVE_ANYWAY") == "" {
		fmt.Fprint(os.Stderr, `swarm serve runs in a terminal of your own, so you can see its address and stop it.
This is not a terminal, which usually means an agent started it.
Agents: every other swarm command works without it; ask the person if they want the page.
To run it without a terminal on purpose, for example as a service, set SWARM_SERVE_ANYWAY=1.
`)
		return errors.New("swarm serve refused to start outside a terminal")
	}
	homeDir, err := filepath.Abs(*home)
	if err != nil {
		return err
	}
	core := openCore(homeDir)
	if err := core.openSession(*project); err != nil {
		return err
	}
	projects, opened := core.Projects()
	srv := &server{core: core, quit: make(chan struct{})}
	httpServer := &http.Server{Addr: *addr, Handler: srv.routes(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-srv.quit
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
	}()
	go reapOrphans(core.store, srv.quit, 30*time.Second)
	log.Printf("swarm on http://%s, opens on %s, shared library %s, projects %v", *addr, opened, homeDir, projects)
	if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Print("swarm server stopped")
	return nil
}

func inTerminal() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func stopServer(args []string) error {
	flags := flag.NewFlagSet("stop", flag.ContinueOnError)
	addr := flags.String("addr", envOr("SWARM_ADDR", "localhost:7070"), "the swarm page")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("swarm stop stops the page; to stop one run use swarm cancel %s", flags.Arg(0))
	}
	if !pageUp(*addr) {
		fmt.Println("the swarm page is not running on " + *addr)
		return nil
	}
	if err := pageRequest(http.MethodPost, *addr, "/api/shutdown"); err != nil {
		return err
	}
	for range 50 {
		if !pageUp(*addr) {
			fmt.Println("swarm page stopped; runs keep going")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("the page did not stop within 5 seconds")
}
