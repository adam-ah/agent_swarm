# swarm

Runs chains of agent, tool and approval steps you design, and shows every run live as a tree on a page.

## Build, install, start

```bash
./build.sh install               # builds ./swarm and copies it to ~/.local/bin
swarm init /path/to/project      # makes the folder a swarm project
swarm serve                      # http://localhost:7070; the other commands talk to it
```

The build packs `src/web/` and `src/samples/` into the single binary. Nothing is written until you ask: `swarm` on its own shows what is set up and what to run next, and `swarm samples` writes the samples into `~/.swarm` (the `change-swarm` and `fix-from-text` chains, their tools and prompts, and `GUIDE.txt`). After that they are ordinary files you own. Run history and the project list live in `~/.swarm` too. Each project's own chains, tools, prompts and memory go in its `.swarm/`, committed with the project.

## One server, two ways in

The page and the `swarm` command are clients of the same server. Every read, save, delete, run, approval and memory note goes through the same API. The server checks a chain, tool or prompt before it saves it and refuses a broken one with the reasons, whichever way it arrived. Run `swarm help` for the commands.

## Which agent runs

Agent steps run Claude Code (`claude -p`), Codex (`codex exec`, with your own Codex config) or a command you add, such as `pi`. `settings.json` in `~/.swarm` or a project's `.swarm/` picks the default, and a step overrides it with `"runner"`. The Settings page, `swarm settings` and `swarm help` cover the details.

## For agents

`src/samples/GUIDE.txt` is plain text written for agents. It covers the file formats, placeholders, flow rules, and how to inspect, create, change, check and run chains with the `swarm` command. `swarm guide` prints it. `swarm init` prints a line to add to a project's CLAUDE.md or AGENTS.md, so agents there know to read it.

## Changing swarm

- **On the page:** pick a chain to edit its tree, or open a tool, prompt or Memory. "Copy to this folder" makes a per-project copy.
- **By an agent:** run `change-swarm` with the change in words. An agent drafts files and saves them with `swarm put`, using the guide and a list of everything that exists. `swarm check` confirms the chains load, and you approve the diff. A rejection puts the files back.

## Source layout

| Path | What |
| --- | --- |
| `build.sh` | builds `./swarm`; `./build.sh install` also copies it to `~/.local/bin` |
| `src/*.go` | the core, the server, the runner and the commands; `assets.go` embeds the two folders below |
| `src/web/` | the page |
| `src/samples/` | sample chains, tools, prompts and `GUIDE.txt`, written to `~/.swarm` by `swarm samples` |
