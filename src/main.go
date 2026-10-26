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
	"time"
)

const usage = `swarm runs chains of agent, tool and user steps. Every command works on its own, straight
on the files; swarm serve adds a page that does the same in a browser. Every change is checked
before it is saved. Only the commands marked "writes" change anything on disk.

  swarm guide                            how chains, tools, prompts and memory work; read it first
  swarm samples                          writes the sample chains, tools, prompts and guide into ~/.swarm,
                                         leaving any file you already have alone, apart from
                                         GUIDE.txt, which always matches this version
  swarm init [DIR]                       writes: makes a folder a swarm project

  swarm list                             every chain, tool and prompt this project can use
  swarm get PATH [-below]                print chains/<name>.json, tools/<name>.json or prompts/<name>.md;
                                         -below gives the shared version a project copy overrides
  swarm put PATH [-file F] [-scope project|shared]
                                         writes: saves one, from F or stdin; refused with reasons if invalid
  swarm rm PATH                          writes: deletes one; refused while a chain still uses it
  swarm check                            confirm every chain loads, and list what looks wrong

  swarm memory                           print what this project has learned
  swarm memory add [-about TEXT] NOTE... writes: adds notes
  swarm memory set [-file F]             writes: replaces it, from F or stdin

  swarm run CHAIN [-input name=value]... [-detach]
                                         writes: starts a run in the background and prints each step
                                         as it finishes; the run keeps going if you stop watching
  swarm runs                             recent runs in this project
  swarm show RUN                         a run's steps
  swarm approve RUN [-note TEXT]         writes: answers the user step a run is waiting on
  swarm revise RUN -note TEXT            writes: sends the run back to the user step's revise step,
                                         with the note for the agents that run again
  swarm reject RUN [-note TEXT]          writes
  swarm cancel RUN                       writes: stops one run; its cleanup steps still run

  swarm settings                         which agent CLI runs agent steps, and what is installed
  swarm put settings.json [-scope project|shared] [-file F]
                                         writes: changes them; refused if they would break a chain

  swarm serve                            the page, on http://localhost:7070
  swarm stop                             closes the page; runs keep going

Every command takes -project DIR (default: the current folder, or $SWARM_PROJECT). Shared files,
run history and the project list live in ~/.swarm, or $SWARM_HOME. serve and stop take
-addr HOST:PORT (default: $SWARM_ADDR or localhost:7070).

Agents
  An agent step runs one of these, in its own headless mode, in the project folder:
    claude   Claude Code (claude -p). The step's "tools" list limits what it may use.
    codex    Codex (codex exec). Runs with your own Codex config, sandbox and approval
             settings, and ignores the step's "tools" list.
    custom   any command you add in settings, for example pi. It gets the prompt on stdin
             (and in $SWARM_PROMPT_FILE) and must print the prompt's fields as a JSON object last.
  settings.json in ~/.swarm (every project) or <project>/.swarm (that project) chooses:
    {
      "agent": "codex",
      "agents": {
        "claude": { "args": ["--model", "opus"] },
        "pi":     { "command": "pi -p" }
      }
    }
  "agent" is the default for every agent step; a step overrides it with "runner": "claude".
  "args" adds arguments to claude or codex. The page has the same choices under Settings.
`

func main() {
	command := "overview"
	args := os.Args[1:]
	if len(args) > 0 {
		command, args = args[0], args[1:]
	}
	var err error
	switch command {
	case "overview":
		overview()
	case "serve":
		err = serve(args)
	case "guide":
		fmt.Print(guideText())
	case "samples":
		err = samples()
	case "help", "-h", "--help":
		fmt.Print(usage)
	case "stop":
		err = stopServer(args)
	case "_run":
		err = work(args)
	case "init", "list", "get", "put", "rm", "check", "memory", "settings", "run", "runs", "show", "approve", "revise", "reject", "cancel":
		err = runCommand(command, args)
	default:
		err = fmt.Errorf("no command %q\n\n%s", command, usage)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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
			fmt.Printf("  ✓ %s is set up as a custom agent: %s\n", name, settings.Agents[name].Command)
		}
	}
	fmt.Printf("  agent steps run with %s unless a step says otherwise (see swarm help, \"Agents\")\n", settings.defaultRunner())

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
	addr := flags.String("addr", envOr("SWARM_ADDR", "localhost:7070"), "where to serve")
	home := flags.String("home", defaultHome(), "the shared library, run history and project list")
	project := flags.String("project", "", "a folder to open for this session, without adding it to the project list")
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
	if *project != "" {
		if err := core.openSession(*project); err != nil {
			return err
		}
	}
	projects, _ := core.Projects()
	srv := &server{core: core, quit: make(chan struct{})}
	httpServer := &http.Server{Addr: *addr, Handler: srv.routes(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-srv.quit
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
	}()
	log.Printf("swarm on http://%s, shared library %s, projects %v", *addr, homeDir, projects)
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
