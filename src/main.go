package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const usage = `swarm runs chains of agent, tool and approval steps. The page and these commands are two
views of the same server: every change goes through it and is checked before it is saved.
Only the commands marked "writes" change anything on disk.

  swarm guide                            how chains, tools, prompts and memory work; read it first
  swarm samples                          writes the sample chains, tools, prompts and guide into ~/.swarm,
                                         leaving any file you already have alone, apart from
                                         GUIDE.txt, which always matches this version
  swarm init [DIR]                       writes: makes a folder a swarm project
  swarm serve                            the page, on http://localhost:7070; the commands below need it
  swarm stop                             stop the server; runs still going are cancelled first and
                                         their cleanup steps run

  swarm list                             every chain, tool and prompt this project can use
  swarm get PATH [-below]                print chains/<name>.json, tools/<name>.json or prompts/<name>.md;
                                         -below gives the shared version a project copy overrides
  swarm put PATH [-file F] [-scope project|shared]
                                         writes: saves one, from F or stdin; refused with reasons if invalid
  swarm rm PATH                          writes: deletes one; refused while a chain still uses it
  swarm check                            confirm every chain loads

  swarm memory                           print what this project has learned
  swarm memory add [-about TEXT] NOTE... writes: adds notes
  swarm memory set [-file F]             writes: replaces it, from F or stdin

  swarm run CHAIN [-input name=value]... [-detach]
                                         writes: starts a run and prints each step as it finishes
  swarm runs                             recent runs in this project
  swarm show RUN                         a run's steps
  swarm approve RUN [-note TEXT]         writes: answers the approval a run is waiting on
  swarm revise RUN -note TEXT            writes: sends the run back to the approval's revise step,
                                         with the note as {{feedback}}
  swarm reject RUN [-note TEXT]          writes
  swarm cancel RUN                       writes: stops one run; its cleanup steps still run

  swarm settings                         which agent CLI runs agent steps, and what is installed
  swarm put settings.json [-scope project|shared] [-file F]
                                         writes: changes them; refused if they would break a chain

Every command takes -project DIR (default: the current folder, or $SWARM_PROJECT) and
-addr HOST:PORT (default: $SWARM_ADDR or localhost:7070). Shared files, run history and the
project list live in ~/.swarm.

Agents
  An agent step runs one of these, in its own headless mode, in the project folder:
    claude   Claude Code (claude -p). The step's "tools" list limits what it may use.
    codex    Codex (codex exec). Runs with your own Codex config, sandbox and approval
             settings, and ignores the step's "tools" list.
    custom   any command you add in settings, for example pi. It gets the prompt on stdin
             (and in $SWARM_PROMPT_FILE) and must print the step's fields as a JSON object last.
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
	case "init", "list", "get", "put", "rm", "check", "memory", "settings", "run", "runs", "show", "approve", "revise", "reject", "cancel":
		err = clientCommand(command, args)
	default:
		err = fmt.Errorf("no command %q\n\n%s", command, usage)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func defaultHome() string {
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

func overview() {
	home := defaultHome()
	folder, _ := os.Getwd()
	addr := envOr("SWARM_ADDR", "localhost:7070")
	var projects []string
	_ = readJSON(filepath.Join(home, "projects.json"), &projects)
	isProject := false
	for _, p := range projects {
		isProject = isProject || p == folder
	}
	_, serverErr := http.Get("http://" + addr + "/api/projects")

	fmt.Print("swarm runs chains of agent, tool and approval steps, and shows every run live on a page.\n\n")
	state := func(done bool, yes, no string) {
		mark := "✗"
		if done {
			mark = "✓"
		}
		fmt.Printf("  %s %s\n", mark, map[bool]string{true: yes, false: no}[done])
	}
	state(samplesWritten(home), "sample chains, tools, prompts and guide are in "+home, "no sample chains yet in "+home)
	state(isProject, folder+" is a swarm project", folder+" is not a swarm project")
	state(serverErr == nil, "the page is running on http://"+addr, "the page is not running")
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
	}
	if serverErr != nil {
		fmt.Println("  swarm serve       start the page, in a terminal you keep open")
	} else {
		fmt.Println("  open              http://" + addr)
		if isProject {
			fmt.Println("  swarm list        what this project can run")
		}
		fmt.Println("  swarm stop        stop the page; runs still going are cancelled first")
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
		return errors.New(`swarm serve runs in a terminal of your own, so you can see its address and stop it.
It refused to start here because this is not a terminal, which usually means an agent started it.
Agents: ask the person to run "swarm serve" instead.
To run it without a terminal on purpose, for example as a service, set SWARM_SERVE_ANYWAY=1.`)
	}
	homeDir, err := filepath.Abs(*home)
	if err != nil {
		return err
	}
	store, err := openStore(filepath.Join(homeDir, "runs"))
	if err != nil {
		return err
	}
	self, _ := os.Executable()
	core := &Core{home: homeDir, store: store, runner: &Runner{store: store, swarm: self, addr: *addr}}
	if err := core.loadProjects(*project); err != nil {
		return err
	}
	projects, _ := core.Projects()
	srv := &server{core: core, quit: make(chan struct{})}
	httpServer := &http.Server{Addr: *addr, Handler: srv.routes()}
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
	addr := flags.String("addr", envOr("SWARM_ADDR", "localhost:7070"), "the swarm server")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("swarm stop stops the server; to stop one run use swarm cancel %s", flags.Arg(0))
	}
	base := "http://" + *addr
	if _, err := http.Get(base + "/api/projects"); err != nil {
		fmt.Println("the swarm server is not running on " + *addr)
		return nil
	}
	res, err := http.Post(base+"/api/shutdown", "application/json", nil)
	if err != nil {
		return err
	}
	var body struct {
		Error     string
		Cancelled []string
	}
	_ = json.NewDecoder(res.Body).Decode(&body)
	res.Body.Close()
	if res.StatusCode >= 300 {
		return errors.New(body.Error)
	}
	if len(body.Cancelled) > 0 {
		fmt.Printf("cancelled %s; waiting for their cleanup steps\n", strings.Join(body.Cancelled, ", "))
	}
	for i := 0; i < 200; i++ {
		if _, err := http.Get(base + "/api/projects"); err != nil {
			fmt.Println("swarm server stopped")
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("the server did not stop within 40 seconds")
}

/* the client */

type client struct {
	base    string
	project string
}

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

func clientCommand(command string, args []string) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	addr := flags.String("addr", envOr("SWARM_ADDR", "localhost:7070"), "the swarm server")
	project := flags.String("project", envOr("SWARM_PROJECT", "."), "the project folder")
	below := flags.Bool("below", false, "get: the shared version below this project's copy")
	file := flags.String("file", "", "put, memory set: read from this file instead of stdin")
	scope := flags.String("scope", "", "put: project or shared; default is where it already lives, else project")
	note := flags.String("note", "", "approve, revise, reject: a note for the record; revise needs one")
	about := flags.String("about", "", "memory add: what the notes came from")
	detach := flags.Bool("detach", false, "run: print the run's link and return")
	inputs := inputFlags{}
	flags.Var(inputs, "input", "run: a chain input as name=value; repeat for more")

	var positional []string
	for len(args) > 0 {
		name, _, _ := strings.Cut(strings.TrimLeft(args[0], "-"), "=")
		if strings.HasPrefix(args[0], "-") && args[0] != "--" && flags.Lookup(name) == nil {
			positional, args = append(positional, args[0]), args[1:]
			continue
		}
		if err := flags.Parse(args); err != nil {
			return err
		}
		args = flags.Args()
		if len(args) > 0 {
			positional, args = append(positional, args[0]), args[1:]
		}
	}
	folder, err := filepath.Abs(*project)
	if err != nil {
		return err
	}
	c := client{base: "http://" + *addr, project: folder}
	if err := c.serverUp(*addr); err != nil {
		return err
	}
	arg := func(i int, what string) (string, error) {
		if len(positional) <= i {
			return "", fmt.Errorf("swarm %s needs %s", command, what)
		}
		return positional[i], nil
	}

	switch command {
	case "init":
		if len(positional) > 0 {
			if c.project, err = filepath.Abs(positional[0]); err != nil {
				return err
			}
		}
		return c.init()
	case "list":
		var res struct{ Project, Home, Catalog string }
		if err := c.call("GET", "/api/catalog", nil, &res); err != nil {
			return err
		}
		fmt.Printf("%s: shared from %s, overridden by %s/.swarm\n\n%s", res.Project, res.Home, res.Project, res.Catalog)
	case "check":
		var res struct {
			Problems               []string
			Chains, Tools, Prompts int
		}
		if err := c.call("GET", "/api/check", nil, &res); err != nil {
			return err
		}
		if len(res.Problems) > 0 {
			return errors.New(strings.Join(res.Problems, "\n"))
		}
		fmt.Printf("ok: %d chains, %d tools, %d prompts\n", res.Chains, res.Tools, res.Prompts)
	case "get":
		path, err := arg(0, "a path like chains/fix-from-text.json")
		if err != nil {
			return err
		}
		query := ""
		if *below {
			query = "?below=1"
		}
		var res struct{ Source, Content string }
		if err := c.call("GET", "/api/items/"+path+query, nil, &res); err != nil {
			return err
		}
		fmt.Print(res.Content)
	case "put":
		path, err := arg(0, "a path like chains/my-chain.json")
		if err != nil {
			return err
		}
		content, err := readInput(*file)
		if err != nil {
			return err
		}
		query := ""
		if *scope != "" {
			query = "?scope=" + url.QueryEscape(*scope)
		}
		if err := c.send("PUT", "/api/items/"+path+query, content, nil); err != nil {
			return err
		}
		fmt.Println("saved " + path)
	case "rm":
		path, err := arg(0, "a path like tools/old_tool.json")
		if err != nil {
			return err
		}
		if err := c.call("DELETE", "/api/items/"+path, nil, nil); err != nil {
			return err
		}
		fmt.Println("deleted " + path)
	case "memory":
		return c.memory(positional, *file, *about)
	case "run":
		chain, err := arg(0, "a chain name")
		if err != nil {
			return err
		}
		var res struct{ ID string }
		if err := c.call("POST", "/api/runs", map[string]any{"chain": chain, "inputs": inputs}, &res); err != nil {
			return err
		}
		fmt.Printf("run %s  %s/#/run/%s\n", res.ID, c.base, res.ID)
		if *detach {
			return nil
		}
		return c.follow(res.ID)
	case "settings":
		var res struct {
			Default   string
			Runners   []string
			Installed map[string]string
			Shared    any
			Project   any
		}
		if err := c.call("GET", "/api/settings", nil, &res); err != nil {
			return err
		}
		fmt.Printf("agent steps run with %s unless a step sets runner\n", res.Default)
		fmt.Printf("agents: %s\n", strings.Join(res.Runners, ", "))
		for _, name := range builtinRunners {
			if path, found := res.Installed[name]; found {
				fmt.Printf("  %s: %s\n", name, path)
			} else {
				fmt.Printf("  %s: not on PATH\n", name)
			}
		}
		for label, layer := range map[string]any{"shared, " + defaultHome() + "/settings.json": res.Shared, "this project, .swarm/settings.json": res.Project} {
			if layer != nil {
				raw, _ := json.MarshalIndent(layer, "", "  ")
				fmt.Printf("\n%s:\n%s\n", label, raw)
			}
		}
	case "runs":
		var runs []struct {
			ID, Chain, Title, Subject, Status string
			StartedAt                         time.Time `json:"started_at"`
		}
		if err := c.call("GET", "/api/runs", nil, &runs); err != nil {
			return err
		}
		for _, r := range runs {
			fmt.Printf("%s  %-11s %-16s %s\n", r.ID, r.Status, r.Chain, firstLine(r.Subject))
		}
	case "show":
		id, err := arg(0, "a run id")
		if err != nil {
			return err
		}
		run, err := c.getRun(id)
		if err != nil {
			return err
		}
		fmt.Printf("%s  %s  %s\n%s\n\n", run.ID, run.Status, run.Title, run.Subject)
		report(run.Steps, "", map[string]string{}, c.base+"/#/run/"+id)
		if run.Reason != "" {
			fmt.Printf("\n%s\n", run.Reason)
		}
	case "approve", "revise", "reject":
		id, err := arg(0, "a run id")
		if err != nil {
			return err
		}
		if err := c.call("POST", "/api/runs/"+id+"/decide", decision{Choice: command, Note: *note}, nil); err != nil {
			return err
		}
		fmt.Println(command + "d " + id)
	case "cancel":
		id, err := arg(0, "a run id")
		if err != nil {
			return err
		}
		if err := c.call("POST", "/api/runs/"+id+"/stop", nil, nil); err != nil {
			return err
		}
		fmt.Println("cancelling " + id + "; its cleanup steps still run")
	}
	return nil
}

func readInput(file string) ([]byte, error) {
	if file != "" {
		return os.ReadFile(file)
	}
	return io.ReadAll(os.Stdin)
}

func (c client) url(path string) string {
	if strings.HasPrefix(path, "/api/projects") || strings.HasPrefix(path, "/api/runs/") {
		return c.base + path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return c.base + path + sep + "project=" + url.QueryEscape(c.project)
}

func (c client) call(method, path string, body any, out any) error {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	return c.send(method, path, raw, out)
}

func (c client) send(method, path string, raw []byte, out any) error {
	req, err := http.NewRequest(method, c.url(path), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		var failure struct {
			Error    string
			Problems []string
		}
		_ = json.NewDecoder(res.Body).Decode(&failure)
		if len(failure.Problems) > 0 {
			return fmt.Errorf("refused:\n- %s", strings.Join(failure.Problems, "\n- "))
		}
		return errors.New(failure.Error)
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}

func (c client) serverUp(addr string) error {
	if _, err := http.Get(c.base + "/api/projects"); err != nil {
		return fmt.Errorf("the swarm server is not running on %s. Start it in a terminal of your own with:\n\n  swarm serve\n\nAgents: do not start it yourself; ask the person to run it", addr)
	}
	return nil
}

func (c client) init() error {
	var res struct{ Project string }
	if err := c.call("POST", "/api/projects", map[string]string{"path": c.project}, &res); err != nil {
		return err
	}
	c.project = res.Project
	var memory struct{ Text string }
	if err := c.call("GET", "/api/memory", nil, &memory); err != nil {
		return err
	}
	if memory.Text == "" {
		if err := c.call("PUT", "/api/memory", map[string]string{"text": ""}, nil); err != nil {
			return err
		}
	}
	fmt.Printf("%s is a swarm project. Wrote its .swarm/memory.md.\n\n", c.project)
	if !samplesWritten(defaultHome()) {
		fmt.Println("  swarm samples                  write the sample chains, tools, prompts and guide into " + defaultHome())
	}
	fmt.Printf(`  swarm guide                    how chains, tools, prompts and memory work
  swarm list                     what this project can run
  swarm run change-swarm -input request="make a chain that ..."
  %s

So agents working here know about swarm, add this line to the project's CLAUDE.md or AGENTS.md:

  This project uses swarm for multi-step agent work. Run "swarm guide" before creating, changing or running a chain. Never start or stop the swarm server yourself; ask the person to run "swarm serve".
`, c.base)
	return nil
}

func (c client) memory(positional []string, file, about string) error {
	if len(positional) == 0 {
		var res struct{ Text string }
		if err := c.call("GET", "/api/memory", nil, &res); err != nil {
			return err
		}
		fmt.Print(res.Text)
		return nil
	}
	switch positional[0] {
	case "add":
		var res struct{ Added int }
		if err := c.call("POST", "/api/memory/notes", map[string]any{"about": about, "notes": positional[1:]}, &res); err != nil {
			return err
		}
		fmt.Printf("remembered %d note(s)\n", res.Added)
	case "set":
		text, err := readInput(file)
		if err != nil {
			return err
		}
		if err := c.call("PUT", "/api/memory", map[string]string{"text": string(text)}, nil); err != nil {
			return err
		}
		fmt.Println("memory replaced")
	default:
		return fmt.Errorf("swarm memory takes add or set, not %q", positional[0])
	}
	return nil
}

func (c client) getRun(id string) (*Run, error) {
	var run Run
	if err := c.call("GET", "/api/runs/"+id, nil, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func (c client) follow(id string) error {
	seen := map[string]string{}
	link := c.base + "/#/run/" + id
	for {
		run, err := c.getRun(id)
		if err != nil {
			return err
		}
		report(run.Steps, "", seen, link)
		if run.Status != "running" && run.Status != "waiting" {
			fmt.Printf("\n%s", run.Status)
			if run.Reason != "" {
				fmt.Printf(": %s", run.Reason)
			}
			fmt.Println()
			if run.Status != "done" {
				return errors.New("the run did not finish")
			}
			return nil
		}
		time.Sleep(2 * time.Second)
	}
}

func report(steps []*StepRun, indent string, seen map[string]string, link string) {
	for _, s := range steps {
		if seen[s.Path] != s.Status {
			seen[s.Path] = s.Status
			switch s.Status {
			case "running":
				fmt.Printf("%s… %s [%s]\n", indent, s.Title, s.Kind)
			case "waiting":
				answers := "swarm approve / swarm reject"
				if s.Revise != "" {
					answers = "swarm approve / swarm revise -note TEXT (back to " + s.Revise + ") / swarm reject"
				}
				fmt.Printf("%s⏸ %s: %s\n%s  answer on the page (%s) or with %s\n", indent, s.Title, s.Question, indent, link, answers)
			case "done":
				fmt.Printf("%s✓ %s%s\n", indent, s.Title, suffix(s.Headline))
			case "failed":
				fmt.Printf("%s✗ %s%s\n", indent, s.Title, suffix(s.Error))
			case "skipped":
				fmt.Printf("%s- %s (skipped)\n", indent, s.Title)
			case "pending":
				fmt.Printf("%s  %s\n", indent, s.Title)
			}
		}
		report(s.Children, indent+"  ", seen, link)
	}
}

func suffix(text string) string {
	text = firstLine(text)
	if text == "" {
		return ""
	}
	if len(text) > 160 {
		text = text[:160] + "…"
	}
	return ": " + text
}
