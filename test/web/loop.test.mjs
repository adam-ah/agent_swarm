import test from "node:test";
import assert from "node:assert/strict";
import { $, $$, answers, click, dialogs, settle, type } from "./page.mjs";
import { body, button, control, labelled, library, makeChange, open, options, row, rows, shown, start, text } from "./editor-fixtures.mjs";
import { at, head, library as runLibrary, openRun, planDone, row as runRow, runOf } from "./run-fixtures.mjs";

const parts = (el) => [...el.children].map((c) => text(c));
const titles = () => rows().map((r) => r.querySelector(".title").textContent).slice(1);
const saved = async (calls) => {
  click(button("Save"));
  await settle();
  return body(calls.findLast((c) => c.path === "/api/items/chains/make-change.json")).steps;
};
const AGAIN = { id: "again", title: "Review again", repeat: "change", when: "review.verdict == CHANGES", rounds: 4, fail: "out_of_rounds" };

function looped(...extra) {
  const chain = makeChange();
  chain.steps.splice(4, 0, AGAIN, ...extra);
  return library([chain]);
}

test("a repeat step heads a block holding the steps it goes back over", async () => {
  start(looped());
  await open("#/chain/make-change");

  assert.deepEqual(parts($(".ol-loop > .ol-label")),
    ["Repeat", "(4× at most)", "while", "review.verdict", "is CHANGES", "set by Review it", "· the run fails if it still wants another round after the last"]);
  assert.deepEqual($$(".ol-loop .tree-row .title").map((t) => t.textContent), ["Make the change", "Run the tests", "Review it", "Review again"]);
  assert.equal(row("Review again").querySelector(".kind").textContent, "↻");
  assert.deepEqual(parts($(".ol-loop .ol-if > .ol-label")), ["If", "change.verdict", "is DONE", "set by Make the change"]);
});

test("the repeat step's inspector sets where it goes back to, how often and when the run fails", async () => {
  const { saved: calls } = start(looped());
  await open("#/chain/make-change/again");

  assert.deepEqual($$("#inspector .subpanel > h3").map((h3) => h3.firstChild.textContent), ["Repeat (4× at most)", "When it goes back"]);
  assert.deepEqual(options(control("Go back to")).map(([v]) => v), ["read_issue", "change", "test", "review"]);
  assert.equal(shown(control("At most")), "4");
  assert.deepEqual(options(control("At most")).map(([v]) => v), ["2", "3", "4", "5", "6", "8", "10"]);
  assert.equal(shown(control("When the loop ends")), "out_of_rounds");
  assert.equal(text(labelled("Goes back").querySelector(":scope > span > .hint")), "otherwise the loop ends here and the run goes on");

  type(control("Go back to"), "test");
  type(control("At most"), "3");
  type(control("When the loop ends"), "");
  await settle();
  assert.equal($("#inspector .subpanel > h3").firstChild.textContent, "Repeat (3× at most)");
  const step = (await saved(calls)).find((s) => s.id === "again");
  assert.deepEqual(step, { id: "again", title: "Review again", repeat: "test", when: "review.verdict == CHANGES" });
  assert.deepEqual(parts($(".ol-loop > .ol-label")), ["Repeat", "(3× at most)", "while", "review.verdict", "is CHANGES", "set by Review it", "· the run goes on either way"]);

  type(control("At most"), "8");
  type(control("When the loop ends"), "ended_early");
  await settle();
  const changed = (await saved(calls)).find((s) => s.id === "again");
  assert.deepEqual([changed.rounds, changed.fail], [8, "ended_early"]);
});

test("a repeat step is added after the selected step, going back to it, and any later step can become one", async () => {
  const { saved: calls } = start();
  await open("#/chain/make-change");
  click(row("Run the tests"));
  click(button("+ Repeat"));
  await settle();
  assert.equal($("#inspector .card-title").textContent, "Repeat");
  assert.equal(shown(control("Go back to")), "test");
  assert.deepEqual(parts($(".ol-loop > .ol-label")), ["Repeat", "(3× at most)", "every round", "· the run goes on either way"]);

  click(row("Review it"));
  type($("#inspector details.advanced select"), "repeat");
  await settle();
  const steps = await saved(calls);
  assert.deepEqual(steps.find((s) => s.id === "repeat"), { id: "repeat", title: "Repeat", repeat: "test" });
  assert.deepEqual(steps.find((s) => s.id === "review"), { id: "review", title: "Review it", repeat: "repeat" });

  start(library([{ name: "make-change", title: "Empty", steps: [], source: "project" }]));
  await open("#/chain/make-change");
  assert.equal(button("+ Repeat").disabled, true);
});

test("deleting a repeat step removes the loop and leaves its steps in place", async () => {
  const { saved: calls } = start(looped());
  await open("#/chain/make-change/again");
  click(button("Delete"));
  await settle();
  assert.deepEqual(dialogs.confirm, ["Delete the step \"Review again\"?"]);
  assert.equal($(".ol-loop"), null);
  assert.deepEqual(titles(), ["Read the issue", "Make the change", "Run the tests", "Review it", "Approve the change", "Merge", "Ship each changeset", "Tell the team", "Clean up"]);
  assert.equal((await saved(calls)).length, 9);
});

test("deleting the step a loop starts at moves the loop to the next step and drops what pointed at it", async () => {
  const { saved: calls } = start(looped());
  await open("#/chain/make-change/change");
  click(button("Delete"));
  await settle();
  assert.deepEqual(dialogs.confirm, ["Delete the step \"Make the change\"?"]);
  const steps = await saved(calls);
  assert.equal(steps.find((s) => s.id === "again").repeat, "test");
  assert.equal(steps.find((s) => s.id === "approve").revise, "test");
  assert.equal(steps.find((s) => s.id === "test").when, undefined);
  assert.equal($(".ol-if"), null);
});

test("deleting the only step of a loop deletes the loop too, after saying so", async () => {
  const lib = library([{ name: "make-change", title: "Loop", source: "project", steps: [
    { id: "poll", title: "Poll", tool: "merge" },
    { id: "again", title: "Poll again", repeat: "poll" },
    { id: "done", title: "Done", tool: "cleanup" },
  ] }]);
  const { saved: calls } = start(lib);
  await open("#/chain/make-change/poll");
  answers.confirm = false;
  click(button("Delete"));
  assert.deepEqual(dialogs.confirm, ["Delete the step \"Poll\"? \"Poll again\" only makes sense with it, so it goes too."]);
  assert.equal(titles().length, 3);
  answers.confirm = true;
  click(button("Delete"));
  await settle();
  assert.deepEqual((await saved(calls)).map((s) => s.id), ["done"]);
});

test("deleting an approval keeps what ran after it and drops the steps that ran only without it", async () => {
  const chain = makeChange();
  chain.steps[2].keep_until = "review";
  Object.assign(chain.steps[5], { stop_if: "approve.decision == revise", stop: "Sent back" });
  const { saved: calls } = start(library([chain]));
  await open("#/chain/make-change/approve");
  click(button("Delete"));
  await settle();
  assert.deepEqual(dialogs.confirm, ["Delete the step \"Approve the change\"? \"Tell the team\" only makes sense with it, so it goes too."]);
  assert.equal($(".ol-approved"), null);
  const steps = await saved(calls);
  assert.deepEqual(steps.map((s) => s.id), ["read_issue", "change", "test", "review", "merge", "ship", "cleanup"]);
  assert.deepEqual([steps[4].stop_if, steps[4].stop], [undefined, undefined]);

  await open("#/chain/make-change/review");
  click(button("Delete"));
  await settle();
  const after = await saved(calls);
  assert.equal(after.find((s) => s.id === "test").keep_until, "merge");
});

test("deleting a resumed agent passes its own session on, and a dangling keep or revise is dropped", async () => {
  const chain = { name: "make-change", title: "Resume", source: "project", steps: [
    { id: "plan", title: "Plan", agent: "change" },
    { id: "review", title: "Review", agent: "change", resume: "plan" },
    { id: "fix", title: "Fix", agent: "change", resume: "review" },
    { id: "ok", title: "Ok?", user: "Ok?", revise: "fix" },
    { id: "up", title: "Up", tool: "merge", keep_until: "last" },
    { id: "last", title: "Last", tool: "cleanup" },
  ], prompts: makeChange().prompts };
  const { saved: calls } = start(library([chain]));
  await open("#/chain/make-change/review");
  click(button("Delete"));
  await settle();
  assert.equal((await saved(calls)).find((s) => s.id === "fix").resume, "plan");

  await open("#/chain/make-change/fix");
  click(button("Delete"));
  await settle();
  assert.equal((await saved(calls)).find((s) => s.id === "ok").revise, undefined);

  await open("#/chain/make-change/last");
  click(button("Delete"));
  await settle();
  assert.equal((await saved(calls)).find((s) => s.id === "up").keep_until, undefined);
});

test("the run page shows each round of a loop", async () => {
  const lib = structuredClone(runLibrary);
  lib.chains[0].steps.splice(2, 0, { id: "again", title: "Test again", repeat: "test", when: "test.verdict != pass", rounds: 3 });
  const run = runOf("running", {
    plan: { status: "done", started_at: at(15), ended_at: at(15, 5) },
    test: { status: "done", rounds: "2 of 3", round: 1, loop: "Round 2 of 3: Test again sent the run back here", started_at: at(15, 6), ended_at: at(15, 7) },
  });
  run.steps.splice(2, 0, { id: "again", path: "again", title: "Test again", kind: "repeat", uses: "test", status: "pending", rounds: "2 of 3" });
  await openRun(run, lib);

  assert.deepEqual(parts($(".ol-loop > .ol-label")).slice(0, 5), ["Repeat", "(3× at most)", "while", "test.verdict", "is not pass"]);
  assert.equal(runRow("test").querySelector(".chip.repeat").textContent, "round 2 of 3");
  assert.equal(runRow("again").querySelector(".kind").textContent, "↻");
  click(head("test"));
  assert.match(text(runRow("test").querySelector(".srow-body")), /Round 2 of 3: Test again sent the run back here/);
});

test("past rounds of a loop stay listed and open, with what each step did, and the loop shows the value that sent it round", async () => {
  const lib = structuredClone(runLibrary);
  lib.chains[0].steps.splice(2, 0, { id: "again", title: "Test again", repeat: "test", when: "test.verdict != pass", rounds: 3 });
  const tested = (round, minute) => ({ id: "test", path: "test", title: "Run the tests", kind: "tool", status: "done", rounds: round > 1 ? `${round} of 3` : undefined,
    headline: `${round + 1} tests fail`, output: { verdict: "fail" }, log: round > 1 ? `test-${round}` : "test", started_at: at(minute, 0), ended_at: at(minute, 30), cost: 0.5 });
  const sentBack = (round, minute) => ({ id: "again", path: "again", title: "Test again", kind: "repeat", status: "done", rounds: round > 1 ? `${round} of 3` : undefined,
    headline: `Sent the run back to Run the tests for round ${round + 1} of 3`, started_at: at(minute, 31), ended_at: at(minute, 31) });
  const runAt = (round, earlierTests, earlierAgain, test = { status: "running", started_at: at(18) }) => {
    const run = runOf("running", { plan: planDone, test: { ...test, rounds: `${round} of 3`, round: round - 1, earlier: earlierTests } });
    run.steps.splice(2, 0, { id: "again", path: "again", title: "Test again", kind: "repeat", uses: "test", status: "pending", rounds: `${round} of 3`, round: round - 1, earlier: earlierAgain });
    return run;
  };
  const source = await openRun(runAt(2, [tested(1, 16)], [sentBack(1, 16)]), lib);

  const rounds = () => $$(".ol-loop .round");
  assert.deepEqual(rounds().map((r) => parts(r.querySelector("summary"))), [["Round 1 of 3", "31s · $0.50"]]);
  assert.equal(rounds()[0].open, true);
  assert.deepEqual([...rounds()[0].querySelectorAll(".past .srow-title")].map((t) => t.textContent), ["Run the tests", "Test again"]);
  assert.deepEqual([...rounds()[0].querySelectorAll(".past .srow-line")].map((l) => l.textContent), ["2 tests fail", "Sent the run back to Run the tests for round 2 of 3"]);
  assert.equal($(".ol-loop .round-now").textContent, "Round 2 of 3");
  assert.equal($(".ol-loop > .ol-label .ol-value").textContent, "value “fail” from round 1");
  assert.equal(runRow("test").querySelector(".srow-line").textContent.startsWith("Running"), true);

  const past = rounds()[0].querySelector(".past");
  click(past.querySelector("summary"));
  assert.equal(past.open, true);
  assert.ok([...past.querySelectorAll("details summary")].some((s) => s.textContent === "How it was called"));
  click(rounds()[0].querySelector(":scope > summary"));
  assert.equal(rounds()[0].open, false);

  source.send(runAt(2, [tested(1, 16)], [sentBack(1, 16)], { status: "running", started_at: at(18), actions: 4 }));
  assert.equal(rounds()[0].open, false, "a live update leaves the rounds alone");

  source.send(runAt(3, [tested(1, 16), tested(2, 17)], [sentBack(1, 16), sentBack(2, 17)]));
  assert.deepEqual(rounds().map((r) => r.querySelector(".ol-word").textContent), ["Round 1 of 3", "Round 2 of 3"]);
  assert.deepEqual(rounds().map((r) => r.open), [false, true], "a new round keeps what was folded");
  assert.equal(rounds()[0].querySelector(".past").open, true);
  assert.equal($(".ol-loop .round-now").textContent, "Round 3 of 3");

  source.send(runAt(3, [tested(1, 16), tested(2, 17)], [sentBack(1, 16), sentBack(2, 17)], { ...tested(3, 18), output: { verdict: "pass" } }));
  assert.equal($(".ol-loop > .ol-label .ol-value").textContent, "value “pass”");

  source.send(runAt(3, [tested(1, 16), { ...tested(2, 17), started_at: at(16, 31) }], [sentBack(1, 16), sentBack(2, 17)]));
  assert.deepEqual(rounds().map((r) => [r.querySelector(".ol-word").textContent, ...[...r.querySelectorAll(".past .srow-title")].map((t) => t.textContent)]),
    [["Round 1 of 3", "Run the tests", "Test again"], ["Round 2 of 3", "Run the tests", "Test again"]], "a round that starts in the same millisecond the last one ended keeps its own steps");
});
