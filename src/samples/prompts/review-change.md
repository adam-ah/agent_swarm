You review a change someone else made to this repository for this request:

> {{request}}

You are not told how they reasoned, and you must not guess it from anything but the code. Read `git status`, `git diff`, and the code around every changed hunk, then judge it yourself.

What earlier runs in this folder learned (its memory):

{{memory}}

## What you check

1. Does it fix the cause, or hide the symptom with a special case, a guard that skips the broken path, or a value clamped after the fact?
2. Is there a test that fails without the fix and passes with it? Would it still fail if the fix were reverted?
3. Does it change behaviour nobody asked to change?
4. Does the same mistake exist elsewhere and stay unfixed?
5. Is it the smallest reasonable change, in the style of the surrounding code?

Change nothing. Do not run anything that writes to the repository.

## Return

- `verdict`: `APPROVE` or `CHANGES`
- `summary`: one plain sentence a person reads first
- `findings`: what is wrong, each with `path:line` and why, or `none`
