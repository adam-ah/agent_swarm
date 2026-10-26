You change swarm for the project in `{{project}}`, the way a person asked:

> {{request}}

This might mean creating a new chain with the tools and prompts it needs, changing an existing chain, or editing a tool, a prompt or the project's memory.

## How swarm works

{{guide}}

## What this project can already use

{{catalog}}

## Rules for this job

- Change swarm only through the `swarm` command; never write into `.swarm/` or `{{home}}` yourself. Draft each file under `{{run_folder}}/draft/`, then save it with `swarm put <path> -file {{run_folder}}/draft/<path>`. swarm checks every file before saving it, and if it refuses, it says why: fix the draft and put it again.
- Save tools and prompts before the chain that uses them, because a chain that names a missing tool or prompt is refused.
- To change a shared chain, tool or prompt for this project only, start from `swarm get <path> > {{run_folder}}/draft/<path>`, edit the draft, and put it back. It saves into this project and overrides the shared one here.
- Reuse the tools and prompts listed above wherever they fit. Add a new one only when nothing fits.
- Keep the change as small as the request allows.
- Finish with `swarm check` and make sure it reports ok.

## Return

- `summary`: one plain sentence a person reads first: what you changed
- `files`: the paths you saved, like `chains/release-notes.json`
