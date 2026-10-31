import test from "node:test";
import assert from "node:assert/strict";
import { $, $$, answers, click, dialogs, page, settle, type } from "./page.mjs";
import { body, button, control, labelled, open, options, row, shown, start, text, tick } from "./editor-fixtures.mjs";

const inspector = () => $("#inspector");
const chip = () => $("#inspector .inspector-head .chip").textContent;
const heading = () => $("#inspector .card-title").textContent;
const sections = () => $$("#inspector .subpanel > h3").map((h3) => h3.firstChild.textContent);
const sets = () => $$("#inspector .subpanel .reads-list > .read").filter((r) => r.closest(".subpanel").querySelector("h3").firstChild.textContent === "What it sets")
  .map((r) => [r.querySelector(".ol-name").textContent, text(r.querySelector(".ol-from")), r.querySelector(".ol-from").classList.contains("missing")]);
const strip = (el) => [...el.closest(".attached").querySelectorAll(":scope > .reads-strip .read")].map((r) => [r.querySelector(".ol-name").textContent, text(r.querySelector(".ol-from"))]);
const savedStep = async (saved, id) => {
  click(button("Save"));
  await settle();
  return body(saved.findLast((c) => c.path === "/api/items/chains/make-change.json")).steps.find((s) => s.id === id);
};
const choose = (select, value) => type(select, value);

test("a tool step shows its tool, the values the tool needs and what reads its output", async () => {
  const { saved } = start();
  await open("#/chain/make-change/read_issue");

  assert.equal(chip(), "tool");
  assert.equal(heading(), "Read the issue");
  assert.equal(text(labelled("ID").querySelector(".hint")), "later steps read its fields as {{read_issue.output}}");
  assert.deepEqual(sections(), ["What it does", "What it sets", "When it runs", "When swarm stops it"]);
  const picker = control("Tool");
  assert.equal(shown(picker), "read_issue");
  assert.deepEqual(options(picker), [["cleanup", "cleanup: Clean up"], ["merge", "merge: Merge the branch"], ["read_issue", "read_issue: Read an issue"],
    ["run_tests", "run_tests: Run the tests"], ["__new", "+ New tool…"]]);
  assert.equal(text($("#inspector .picked > .hint")), "Read an issue. Open tools/read_issue.json");
  assert.equal($("#inspector .picked > .hint a").getAttribute("href"), "#/tool/read_issue");
  assert.equal(text($("#inspector .shared-edit summary")), "Edit tools/read_issue.json here · used by 2 steps, a change applies to all of them");
  assert.equal(picker.closest(".attached").querySelector(".reads-strip").hidden, true);
  assert.equal(control("issue").value, "{{request}}");
  assert.deepEqual(strip(control("issue")), [["request", "asked when the run starts: “What should change?”"]]);
  assert.deepEqual(sets(), [["read_issue.output", "read by the value issue_text", false]]);

  choose(picker, "merge");
  assert.equal(text($("#inspector .shared-edit summary")), "Edit tools/merge.json here · used by 3 steps, a change applies to all of them");
  assert.equal(control("branch").placeholder, "empty: this chain's {{branch}}");
  assert.deepEqual(strip(control("branch")), [["branch", "worked out when the run starts"]]);
  assert.deepEqual(strip(control("Tool")), [["run_folder", "filled in by swarm"]]);
  assert.equal(text($("#inspector .group-body .hint")), "issue: {{request}} is not read any more remove");

  click(button("remove"));
  assert.equal($("#inspector .group-body .hint"), null);
  type(control("What it does"), "Merge into a branch");
  type(control("Command"), "git merge {{branch}} --into {{target}}");
  assert.equal(control("target").placeholder, "needs a value, e.g. {{step_id.field}}");
  assert.equal(control("Tool").closest(".attached").querySelector(".reads-strip").hidden, true);
  type(control("target"), "main");
  type(control("branch"), "{{read_issue.output}}");
  assert.deepEqual(strip(control("branch")), [["read_issue.output", "set by Read the issue"]]);
  type(control("branch"), "");
  type(control("branch"), "{{read_issue.output}}");

  const step = await savedStep(saved, "read_issue");
  assert.deepEqual(step, { id: "read_issue", title: "Read the issue", tool: "merge", with: { target: "main", branch: "{{read_issue.output}}" } });
  assert.deepEqual(body(saved[0]), { title: "Merge into a branch", run: "git merge {{branch}} --into {{target}}" });
});

test("a new tool is made from the tool picker and saved with the chain", async () => {
  const { saved } = start();
  await open("#/chain/make-change/merge");

  choose(control("Tool"), "__new");
  assert.deepEqual(dialogs.prompt, ["Name for the new tool"]);
  assert.equal(shown(control("Tool")), "merge");
  assert.equal(page("editor.dirty"), true);

  answers.prompt = "lint";
  choose(control("Tool"), "__new");
  assert.equal(shown(control("Tool")), "lint");
  assert.equal(text($("#inspector .picked > .hint")), "lint. Open tools/lint.json");
  assert.equal(text($("#inspector .shared-edit summary")), "Edit tools/lint.json here · only this step uses it");
  assert.equal($("#inspector .shared-edit").open, true);
  assert.equal(text($("#inspector .group-body")), "It needs no values from this step.");

  const step = await savedStep(saved, "merge");
  assert.deepEqual(step, { id: "merge", title: "Merge", tool: "lint" });
  assert.deepEqual(saved.map((c) => c.path), ["/api/items/tools/lint.json", "/api/items/chains/make-change.json"]);
  assert.deepEqual(body(saved[0]), { title: "lint", run: "echo hello" });
  assert.equal($("#inspector .shared-edit").open, true);

  $("#inspector .shared-edit").open = false;
  await settle();
  assert.equal(page("editor.openShared.has('tools/lint.json')"), false);
});

test("an agent step picks its prompt, runner, context and the tools it may use", async () => {
  const { saved } = start();
  await open("#/chain/make-change/review");

  assert.equal(chip(), "agent");
  const picker = control("Prompt");
  assert.equal(shown(picker), "review-change");
  assert.deepEqual(options(picker), [["change", "change.md"], ["review-change", "review-change.md"], ["__new", "+ New prompt…"]]);
  assert.equal(text($("#inspector .picked > .hint")),
    "It returns verdict (APPROVE | CHANGES), notes, from the prompt's ## Return section. Open chains/make-change/review-change.md");
  assert.deepEqual(strip(picker), [["change.summary", "set by Make the change"], ["run_folder", "filled in by swarm"]]);
  assert.equal(text($("#inspector .shared-edit summary")), "Edit chains/make-change/review-change.md here · only this step uses it");
  assert.deepEqual(sets(), [["review.verdict", "nothing reads it", true], ["review.notes", "nothing reads it", true]]);

  const runner = control("Runs with");
  assert.deepEqual(options(runner), [["", "the default (claude)"], ["claude", "claude"], ["codex", "codex"], ["sonnet", "sonnet"]]);
  assert.equal(shown(runner), "sonnet");
  assert.equal(text(labelled("Runs with").querySelector(".hint")), "change the default under Settings");
  choose(runner, "codex");
  assert.equal(text(labelled("Runs with").querySelector(".hint")), "Codex uses your own Codex config and ignores the list below");
  choose(control("Runs with"), "");

  const context = control("Context");
  assert.deepEqual(options(context), [["", "a new context"], ["change", "continue the session of Make the change"]]);
  choose(context, "change");
  assert.deepEqual($$(".tree-note").map(text).filter((n) => n.includes("session")), ["⤷ continues the session of Make the change"]);

  const checks = () => $$("#inspector .checks .check").map((c) => [c.textContent, c.classList.contains("on")]);
  assert.deepEqual(checks().filter(([, on]) => on).map(([name]) => name), ["Read", "Grep"]);
  tick($$("#inspector .checks input")[0]);
  tick($$("#inspector .checks input")[2]);
  assert.deepEqual(checks().filter(([, on]) => on).map(([name]) => name), ["Bash", "Read"]);

  type($("#inspector textarea.prompt"), "Review {{change.summary}} against {{issue_text}}.\n\n## Return\n\n- `verdict`: `APPROVE` or `CHANGES`\n");
  assert.deepEqual(strip(control("Prompt")), [["change.summary", "set by Make the change"], ["issue_text", "worked out when the run starts"]]);

  const step = await savedStep(saved, "review");
  assert.deepEqual(step, { id: "review", title: "Review it", agent: "review-change", tools: ["Bash", "Read"], resume: "change" });
  assert.equal(saved[0].path, "/api/items/chains/make-change/review-change.md");
  assert.match(saved[0].body, /against \{\{issue_text\}\}/);
});

test("the codex default runner explains that the tool list is ignored", async () => {
  const lib = start().lib;
  lib.default_runner = "codex";
  await open("#/chain/make-change/change");
  assert.equal(shown(control("Runs with")), "");
  assert.equal(text(labelled("Runs with").querySelector(".hint")), "Codex uses your own Codex config and ignores the list below");
});

test("a new prompt starts from the template and an agent without returns answers in free text", async () => {
  const { saved } = start();
  await open("#/chain/make-change/change");

  assert.deepEqual(sets(), [
    ["change.summary", "read by Review it, Approve the change", false],
    ["change.files", "read by Run the tests, Ship each changeset", false],
    ["change.verdict", "read by Run the tests", false],
  ]);
  choose(control("Prompt"), "__new");
  assert.deepEqual(dialogs.prompt, ["Name for the new prompt (lowercase letters, digits, - and _)"]);
  assert.equal(shown(control("Prompt")), "change");

  answers.prompt = "plan";
  choose(control("Prompt"), "__new");
  assert.equal(shown(control("Prompt")), "plan");
  assert.equal(text($("#inspector .picked > .hint")), "It has no ## Return section, so it answers in free text, read as {{change.output}}. Open chains/make-change/plan.md");
  assert.equal($("#inspector textarea.prompt").value, page("NEW_PROMPT"));
  assert.equal($("#inspector .shared-edit").open, true);
  assert.equal(text(labelled("ID").querySelector(".hint")), "later steps read its fields as {{change.output}}");
  assert.deepEqual(sets(), [
    ["change.output", "shown on the run page", false],
    ["change.files", "read by Run the tests, Ship each changeset", false],
    ["change.verdict", "read by Run the tests", false],
    ["change.summary", "read by Review it, Approve the change", false],
  ]);

  const step = await savedStep(saved, "change");
  assert.equal(step.agent, "plan");
  assert.deepEqual(saved.map((c) => c.path), ["/api/items/chains/make-change/plan.md", "/api/items/chains/make-change.json"]);
  assert.equal(saved[0].body, page("NEW_PROMPT"));

  const link = $$("#inspector .reads-list .ol-from a").find((a) => a.textContent === "Ship each changeset");
  click(link);
  assert.equal(heading(), "Ship each changeset");
});

test("a user step asks a question, shows files and can send the run back", async () => {
  const { saved } = start();
  await open("#/chain/make-change/approve");

  assert.equal(chip(), "user");
  const question = control("Question");
  assert.equal(question.value, "Merge it? {{change.summary}}");
  assert.equal(question.getAttribute("rows"), "3");
  assert.deepEqual(strip(question), [["change.summary", "set by Make the change"]]);
  assert.equal(control("Files to show with it").value, "{{run_folder}}/change.diff");
  assert.deepEqual(strip(control("Files to show with it")), [["run_folder", "filled in by swarm"]]);
  const back = control("Send back to");
  assert.deepEqual(options(back), [["", "nowhere: only approve or reject"], ["read_issue", "Read the issue"], ["change", "Make the change"], ["test", "Run the tests"], ["review", "Review it"]]);
  assert.equal(shown(back), "change");
  assert.deepEqual(sets(), [["approve.decision", "read by Tell the team", false], ["approve.note", "nothing reads it", false]]);

  click($$("#inspector .reads-list .ol-from a").find((a) => a.textContent === "Tell the team"));
  assert.equal(heading(), "Tell the team");
  assert.equal(text(labelled("Runs").querySelector(":scope > span > .hint")), undefined);
  click(row("Approve the change"));

  type(control("Question"), "");
  assert.equal(control("Question").closest(".attached").querySelector(".reads-strip").hidden, true);
  type(control("Files to show with it"), "  \n{{run_folder}}/demo.mp4\n");
  type(control("Files to show with it"), "");
  choose(control("Send back to"), "");
  assert.equal($(".ol-revise"), null);

  const step = await savedStep(saved, "approve");
  assert.deepEqual(step, { id: "approve", title: "Approve the change", user: "Continue?" });
});

test("a sub-chain step fills the inputs of the chain it runs", async () => {
  const { saved } = start();
  await open("#/chain/make-change/ship");

  assert.equal(chip(), "chain");
  assert.deepEqual(sections(), ["What it does", "When it runs"]);
  const chain = control("Chain to run");
  assert.deepEqual(options(chain), [["ship-changeset", "Ship a changeset"], ["solo", "Solo"]]);
  assert.equal(shown(chain), "ship-changeset");
  assert.equal(control("Changeset to apply").value, "{{change.files}}");
  assert.equal(control("Changeset to apply").placeholder, "needs a value, e.g. {{step_id.field}}");
  assert.equal(control("Dry run").placeholder, "empty: optional");
  assert.equal(control("target").placeholder, "empty: main");
  assert.match(text($("#inspector .subpanel > .form > .hint")), /^Runs ship-changeset once per item of change\.files, one after another\./);
  assert.equal($("#inspector a[href=\"#/chain/ship-changeset\"]").textContent, "Open the ship-changeset chain →");
  assert.equal(shown(control("Stops the run after it")), "off");

  type(control("Changeset to apply"), "{{request}}");
  assert.deepEqual(strip(control("Changeset to apply")), [["request", "asked when the run starts: “What should change?”"]]);
  assert.equal($$(".tree-note").some((n) => n.textContent.includes("once per item")), false);
  type(control("Dry run"), "yes");
  type(control("Dry run"), "");

  choose(control("Chain to run"), "solo");
  assert.match(text($("#inspector .subpanel > .form > .hint")), /^Runs it once\. To run it once per item of a list/);
  assert.equal($("#inspector a[href=\"#/chain/solo\"]").textContent, "Open the solo chain →");
  assert.equal(text($("#inspector .group-body .hint")), "changeset: {{request}} is not read any more remove");
  click(button("remove"));
  assert.equal(text($("#inspector .group-body")), "It needs no values from this step.");

  const step = await savedStep(saved, "ship");
  assert.deepEqual(step, { id: "ship", title: "Ship each changeset", chain: "solo" });
});

test("a sub-chain step run per item can run its items at once and go on past a failed item", async () => {
  const { saved } = start();
  await open("#/chain/make-change/ship");
  const hint = () => text($("#inspector .subpanel > .form > .hint"));
  const toggle = (label) => $$("#inspector .form .toggle").find((t) => text(t) === label)?.querySelector("input");
  const together = "Runs the items at the same time";
  const onward = "Goes on when an item fails, counting it in the summary";

  assert.equal(toggle(together).checked, false);
  assert.equal(toggle(onward).checked, false);
  click(toggle(together));
  assert.match(hint(), /^Runs ship-changeset once per item of change\.files, all at the same time\. .* a failure fails this step once every item has run\.$/);
  click(toggle(onward));
  assert.match(hint(), /a failure is counted in the summary and the run goes on\.$/);
  assert.ok($$(".tree-note").map(text).includes("⤷ runs ship-changeset once per item of change.files, all at once, going on past a failed item"));
  let step = await savedStep(saved, "ship");
  assert.equal(step.parallel, true);
  assert.equal(step.keep_going, true);

  choose($("#inspector details.advanced select"), "tool");
  step = await savedStep(saved, "ship");
  assert.equal("parallel" in step || "keep_going" in step, false);
});

test("changing what kind of step it is resets the fields of the old kind", async () => {
  const { saved } = start();
  await open("#/chain/make-change/read_issue");
  const kind = () => $("#inspector details.advanced select");

  assert.deepEqual(options(kind()).map(([v]) => v), ["tool", "agent", "user", "chain"]);
  choose(kind(), "agent");
  assert.equal(chip(), "agent");
  assert.equal(shown(control("Prompt")), "change");
  choose(kind(), "user");
  assert.equal(chip(), "user");
  assert.equal(control("Question").value, "Continue?");
  choose(kind(), "chain");
  assert.equal(chip(), "chain");
  assert.equal(shown(control("Chain to run")), "ship-changeset");
  assert.equal(shown(kind()), "chain");
  choose(kind(), "tool");
  assert.equal(chip(), "tool");

  const step = await savedStep(saved, "read_issue");
  assert.deepEqual(step, { id: "read_issue", title: "Read the issue", tool: "cleanup" });
});

test("a step after an approval runs only once it is approved, and can stop the run or always run", async () => {
  const { saved } = start();
  await open("#/chain/make-change/merge");

  assert.equal(text(labelled("Runs").querySelector(":scope > span > .hint")), "only once you approve \"Approve the change\": a rejection ends the run before it");
  const message = labelled("Message when it stops the run");
  assert.equal(message.hidden, true);
  const stop = labelled("Stops the run after it");
  choose(stop.querySelector("select"), "on");
  assert.equal(message.hidden, true);
  assert.deepEqual([...stop.querySelectorAll("datalist option")].map((o) => o.value), ["output"]);
  type(stop.querySelector("input"), "output");
  assert.equal(message.hidden, false);
  type(stop.querySelectorAll("input")[1], "conflict");
  type(control("Message when it stops the run"), "Merge conflict: {{merge.output}}");
  assert.deepEqual($$(".tree-note").map(text).filter((n) => n.includes("conflict")), ["⤷ stops the run here if its output is conflict"]);
  assert.deepEqual(sets(), [["merge.output", "shown on the run page", false]]);

  click($("#inspector .toggle input"));
  assert.deepEqual($$(".ol-always .tree-row .title").map((t) => t.textContent), ["Merge", "Clean up"]);

  let step = await savedStep(saved, "merge");
  assert.deepEqual(step, { id: "merge", title: "Merge", tool: "merge", stop_if: "output == conflict", stop: "Merge conflict: {{merge.output}}", always: true });
  assert.deepEqual(sets(), [["merge.output", "read by its own stop condition", false]]);
  assert.equal(text(labelled("Runs").querySelector(":scope > span > .hint")), undefined);

  choose(labelled("Stops the run after it").querySelector("select"), "off");
  assert.equal(labelled("Message when it stops the run").hidden, false);
  type(control("Message when it stops the run"), "");
  click($("#inspector .toggle input"));
  step = await savedStep(saved, "merge");
  assert.deepEqual(step, { id: "merge", title: "Merge", tool: "merge" });
  choose(labelled("Stops the run after it").querySelector("select"), "on");
  choose(labelled("Stops the run after it").querySelector("select"), "off");
  assert.equal(labelled("Message when it stops the run").hidden, true);
});

test("an open shared editor stays open across saves and is forgotten once closed", async () => {
  start();
  await open("#/chain/make-change/test");
  const shared = () => $("#inspector .shared-edit");
  assert.equal(text($("#inspector .shared-edit summary")), "Edit tools/run_tests.json here · only this step uses it");
  assert.deepEqual(sets(), [["test.output", "read by its own stop condition", false]]);

  shared().open = true;
  await settle();
  type(control("Title"), "Run every test");
  click(button("Save"));
  await settle();
  assert.equal(shared().open, true);
  assert.equal(inspector().querySelector(".card-title").textContent, "Run every test");

  shared().open = false;
  await settle();
  click(button("+ User"));
  click(row("Run every test"));
  assert.equal(shared().open, false);
});
