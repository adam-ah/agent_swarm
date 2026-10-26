A run just finished in this repository.

- The request: {{request}}
- What the fixer said it changed: {{fix.summary}}
- What the reviewer said: {{review.summary}}

What this folder's memory already holds:

{{memory}}

Decide what a future run here would otherwise have to rediscover: how to run the tests, where things live, conventions the code follows, traps you hit. Look at the repository if you need to confirm something. Leave out anything already in memory, anything only about this one bug, and anything you are not sure is true. Returning no notes is a good answer when nothing new was learned.

## Return

- `summary`: one plain line a person reads first: what you chose to remember, or that nothing was new
- `notes` (list): one line each, each a fact that stays true after this run
