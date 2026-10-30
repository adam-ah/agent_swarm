# swarm

Runs chains of agent, tool and user steps you design, and shows every run live on a page.

## Build, install, start

```bash
./build.sh install               # builds ./swarm and copies it to ~/.local/bin
swarm init /path/to/project      # makes the folder a swarm project
swarm serve [PORT] [-host IP]    # 127.0.0.1:7070 by default; -host 0.0.0.0 for every interface
```

The build packs `src/web/` and `src/samples/` into the single binary. Nothing is written until you ask: `swarm` on its own shows what is set up and what to run next, and `swarm samples` writes the samples into `~/.swarm` (the `make-change` and `edit-chain` chains, their tools and prompts, and `GUIDE.txt`). After that they are ordinary files you own. Run history and the project list live in `~/.swarm` too. Each project's own chains, tools, prompts and memory go in its `.swarm/`, committed with the project.

## One server, two ways in

The page and the `swarm` command are clients of the same server. Every read, save, delete, run, approval and memory note goes through the same API. The server checks a chain, tool or prompt before it saves it and refuses a broken one with the reasons, whichever way it arrived. Run `swarm help` for the commands.

## Which agent runs

An agent step whose prompt returns `questions` can stop and ask you what it needs to know; it waits for your answers, on the page or with `swarm answer`, and carries on in the same session until it has none left.

Agent steps run Claude Code (`claude -p`), Codex (`codex exec`, with your own Codex config) or a command you add, such as `pi`. `settings.json` in `~/.swarm` or a project's `.swarm/` picks the default, and a step overrides it with `"runner"`. The Settings page, `swarm settings` and `swarm help` cover the details.

## For agents

`src/samples/GUIDE.txt` is plain text written for agents. It covers the file formats, placeholders, flow rules, and how to inspect, create, change, check and run chains with the `swarm` command. `swarm guide` prints it. `swarm init` prints a line to add to a project's CLAUDE.md or AGENTS.md, so agents there know to read it.

## Changing swarm

- **On the page:** a chain's page starts a run and edits the chain; tools, prompts and Memory have their own pages. "Save as a copy for this project only", under More, makes a per-project copy.
- **By an agent:** run the `edit-chain` chain with the change in words ("in make-change, add a lint step before the tests"). An agent drafts files and saves them into this project with `swarm put`, using the guide and a list of everything that exists. `swarm check` confirms the chains load, and you approve the diff or send it back with a note. A rejection puts the files back.
- **Per project:** a value that differs between projects, such as the test command, goes under `inputs` in the project's `.swarm/settings.json` (or on the Settings page), so the shared chain stays as it is.

## Source layout

| Path | What |
| --- | --- |
| `build.sh` | builds `./swarm`; `./build.sh install` also copies it to `~/.local/bin` |
| `release.sh` | tags the next version (patch by default, or `./release.sh minor` / `major`) and pushes the tag |
| `.github/workflows/release.yml` | pushing a `v*` tag runs `build.sh`, then publishes Linux and macOS builds (amd64, arm64) as a GitHub release |
| `src/*.go` | the core, the server, the runner and the commands; `assets.go` embeds the two folders below |
| `src/web/` | the page |
| `src/samples/` | sample chains, tools, prompts and `GUIDE.txt`, written to `~/.swarm` by `swarm samples` |
