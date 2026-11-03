# AGENTS.md

## CLI parity

The `swarm` command returns every piece of data the page shows: runs, steps, timing, cost, fields, errors, questions, earlier attempts, calls, tool calls, transcripts, run files, chains, tools, prompts, settings and memory.

- Text output prints values in full or says which command prints them in full. No value is reachable only on the page, through the HTTP API, or by reading files under `.swarm/` or `~/.swarm/`.
- Read commands take `-json` for the same data the page's API returns.
- Every command answers `-h` with its own usage.
- A change that adds data to the page adds it to the CLI, with a test, in the same change.

## Wording

swarm is for software engineers. Write every label, heading, hint, button, placeholder, confirm and prompt text, CLI message and error for them: terse, precise, standard terms.

- Use the standard term, never a paraphrase:

  | Write | Not |
  | --- | --- |
  | in parallel, concurrently | at the same time, all at once |
  | sequential(ly) | one after another, in turn |
  | keep going, continue | goes on |
  | session, resume | context, continue the session |
  | kill, killed | stop, stopped (for processes) |
  | computed at start | worked out when the run starts |
  | CLI | from a terminal |
  | default, override, inherited | starting value, wins over, from every project's settings |

- Lead with the noun or verb and drop filler: "Stop message", not "Message when it stops the run". "Discard unsaved changes?", not "You have unsaved changes. Leave without saving?".
- A toggle names the option and its default: "Run in parallel (default: sequential)", "Keep going if an item fails (default: a failed item fails the run)".
- Name the thing: the chain, step, item, file path or JSON key (`subject`, `with`, `keep_going`). No vague "it", "this" or "the items".
- Hints are one-line fragments. No "you", "please", reassurance, or explaining the obvious.
- Headings follow the existing patterns: "Repeat (3× at most)", "Repeat (seq)", "Repeat (parallel)".
- Empty states are one or two words: "No runs.", "Unused", "Empty".
- Confirms ask for the action and state the consequence: "Delete run and its logs?", "Stop run? \"always\" steps still run."
- When copy changes, update the tests that assert it in the same change.
