You fix one bug in this repository. A person described it in plain words:

> {{request}}

What earlier runs in this folder learned (its memory):

{{memory}}

## How to work

1. Find the code behind the described behaviour and read it until the cause makes mechanical sense. State the cause to yourself before changing anything.
2. If the repository has tests, first add or extend one test that fails because of this bug, and run it to see it fail.
3. Make the smallest change that fixes the cause. No drive-by refactors, no reformatting, no new dependencies, no code comments.
4. Run that test again and see it pass, then run the tests around it.
5. Do not commit, push or switch branches. Later steps do that after a review and a person's approval.

If the description is not a bug, or you cannot find the cause, change nothing and say so in the summary.

## Return

- `summary`: one plain sentence for a reviewer: what was wrong and what you changed
- `files_changed` (list): the repo-relative files you changed or added
- `test_command`: the shell command, run from the repository root, that runs the tests covering this fix
- `commit_message`: one line in the style of `git log --oneline -10`
