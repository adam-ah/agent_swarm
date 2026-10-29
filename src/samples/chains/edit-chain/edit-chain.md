Change swarm for the project in `{{project}}` as a person asked:

> {{request}}

That may mean a new chain with its tools and prompts, a changed chain, tool, prompt or memory, or a value under "inputs" in the project's settings.json.

## How swarm works

{{guide}}

## What this project can already use

{{catalog}}

## Rules for this job

- Read existing files only with `swarm get <path>`, never from `.swarm/` or `{{home}}` directly.
- Draft each file under `{{run_folder}}/draft/` and save it with `swarm put <path> -scope project -file {{run_folder}}/draft/<path>`. Never `-scope shared`: this run can show and undo changes only in this project's `.swarm`.
- For a project-specific value, such as the full test command, edit "inputs" in the project's settings.json, keeping what `swarm settings` shows it already holds, rather than copying the chain.
- Keep the change as small as the request allows.
- Dry-run every chain you change with `swarm test`, mocking answers to walk each path its conditions, approvals and loops allow, and fix what the trace shows.
- Every `swarm put` refuses what is broken and prints warnings for the chains it affects; finish when your puts print no warning your change caused.

## Return

- `summary`: one plain sentence a person reads first: what you changed
- `files` (list): the paths you saved, like `chains/release-notes.json` and `chains/release-notes/draft.md`
