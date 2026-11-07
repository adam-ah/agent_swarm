# swarm

Runs chains of agent, tool and approval steps you define, and shows every run live in the browser.

![A make-change run waiting for approval](docs/screenshot.png)

## Features

- Chains in JSON, prompts in Markdown, tools as shell commands; all committed with the project in `.swarm/`
- Agent steps run Claude Code (`claude -p`), Codex (`codex exec`) or any CLI you configure, per step
- Live run page: status, timing, cost, fields, tool calls and transcript for every step
- Approval steps show diffs, images, videos and Markdown; approve, reject, or send back to an earlier step with a note
- Agents can ask questions mid-step and resume the same session with the answers
- `when` and `stop_if` conditions, `always` steps, review loops (`repeat`), sub-chains
- Sub-chains per list item, sequentially or in parallel, with `keep_going`
- Rerun a run from any step; earlier attempts are kept
- Per-project memory that prompts read and runs add to
- Chains, tools and prompts are validated before they are saved; a broken one is refused with reasons
- `swarm test`: dry run through the real engine with mocked answers
- `edit-chain` sample: an agent changes your chains, you approve the diff
- Every value on the page is also in the CLI, with `-json`
- Single static binary for Linux and macOS

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
