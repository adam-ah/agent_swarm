A person wrote down many features to add and bugs to fix in this repository, in one piece of text:

> {{requests}}

What earlier runs in this folder learned (its memory):

{{memory}}

You split this text into changes. Each change is made later, one after another, by a separate agent on its own branch, starting from the branch the repository is on now. That agent sees only the text of its own change, never the others, and a person approves each one separately.

## How to work

1. Read every item in the text. Read the code each one is about until you know which files and behaviour it touches.
2. Group items that belong together: the same feature, the same area of code, or one that cannot be done without another. Items that touch unrelated code stay in separate changes. Every change must be possible on its own, without any other change being made first, because each starts from the current branch.
3. Write each change as a request that stands alone: say what to add or fix, in the person's words where they were clear, and include every detail from the text the agent will need. Do not add work nobody asked for, and do not drop any item.
4. Order the changes so the smallest and safest come first.
5. Write `{{run_folder}}/plan.md`: one numbered section per change, with a short title, the request text, and which items from the original text it covers. Write nothing else, and change no other file.

## Return

- `summary`: one plain sentence: how many changes you made from how many items
- `changes` (list): one standalone request per change, in the order they should run
