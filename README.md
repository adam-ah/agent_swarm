# swarm

Runs chains of agent, tool and approval steps you define, and shows every run live in the browser.

![A make-change run waiting for approval](docs/screenshot.png)

## Features

- **Your own agent pipelines.** Chain agents, shell commands and your approvals: make the change, run the tests, have a second agent review it, you approve, it commits.
- **The agents you already use.** Claude Code, Codex or any agent CLI, mixed in one chain.
- **Every run, live.** Each step's progress, output, cost and every tool call the agent made.
- **Nothing lands without you.** See the diff, screenshots or video before anything is committed. Approve, reject, or send it back with a note.
- **Agents ask instead of guessing.** An agent can stop to ask you something, then pick up where it left off.
- **Loops and branches.** Fix and review again until the reviewer is happy, skip steps that don't apply, run one chain over a list of tasks in parallel.
- **Retry one step, not the whole run.** Rerun from any step; earlier attempts stay visible.
- **It learns your project.** Runs save what they learned to a project memory that later runs read.
- **Change a chain by describing it.** An agent edits the chain and you approve the diff.
- **Test a chain before it touches anything.** Dry runs with made-up agent answers.
- **Scriptable.** Everything on the page is also in the `swarm` CLI, so scripts and other agents can drive it.
- **One binary, no setup.** Chains live in your repo, next to the code they work on.

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
