# swarm

Runs chains of agent, tool and approval steps you define, and shows every run live in the browser.

![A make-change run waiting for approval](docs/screenshot.png)

## Features

- Build a chain from agent steps, shell commands and approval steps. The included `make-change` chain makes a change on a branch, runs the tests, has a second agent review it, and asks you before it commits and pushes.
- Agent steps run Claude Code, Codex or any other agent CLI. Steps in one chain can use different agents.
- The run page updates live with each step's status, output, duration and cost, and every tool call an agent made.
- Approval steps show the diff and any screenshots or videos a step produced. You approve, reject, or send the work back to an earlier step with a note.
- An agent can stop and ask you questions, then continue in the same session once you answer.
- Steps can be skipped by a condition, repeated (fix and review until the reviewer approves), or run once per item in a list, sequentially or in parallel.
- A run can be rerun from any step. Earlier attempts are kept.
- Each project has a memory file. Runs add notes to it and later runs read it.
- To change a chain, describe the change in words. The `edit-chain` chain has an agent edit it, and you approve the diff.
- `swarm test` walks through a chain with mocked agent answers and runs nothing.
- Everything on the page is also in the `swarm` CLI, so scripts and agents can use it.
- One binary for Linux and macOS. Chains, prompts and tools live in your repo under `.swarm/`.

## Install

Download a build from [Releases](https://github.com/adam-ah/agent_swarm/releases) and put `swarm` on your `PATH`, or build from source (Go 1.23+):

```bash
cd src && go build -o ~/.local/bin/swarm .
```

## Start

```bash
swarm samples                 # sample chains, tools and the guide into ~/.swarm
swarm init                    # make the current folder a project
swarm serve                   # the page, on http://127.0.0.1:7070
swarm run make-change -input request="..."
```

`swarm help` lists commands. `swarm guide` documents the file formats; it is written for agents too.

The page has no login. Keep it on `127.0.0.1` unless every machine that can reach the port is trusted.

## License

MIT
