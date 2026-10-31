import test from "node:test";
import assert from "node:assert/strict";
import { $, click, reset, route, settle, type } from "./page.mjs";
import { RUN, at, callRecord, head, openRun, panel, planDone, row, runOf, testDone } from "./run-fixtures.mjs";
import { body, button, control, labelled, open, options, start, text } from "./editor-fixtures.mjs";

const saved = async (calls, id) => {
  click(button("Save"));
  await settle();
  return body(calls.findLast((c) => c.path === "/api/items/chains/make-change.json")).steps.find((s) => s.id === id);
};

test("a tool step sets its idle limit, time limit and what it leaves running", async () => {
  const { saved: calls } = start();
  await open("#/chain/make-change/test");

  assert.equal(control("Idle limit").placeholder, "10m");
  assert.equal(text(labelled("Idle limit").querySelector(".hint")), "killed after this long with no output and no CPU use; default 10m, 0 = no limit");
  assert.equal(control("Time limit").placeholder, "2h");
  const keep = control("Background processes");
  assert.deepEqual(options(keep).map(([v]) => v), ["", "review", "approve", "merge", "ship", "tell", "cleanup"]);
  assert.equal(options(keep)[6][1], "kept until Clean up ends");

  type(control("Idle limit"), " 30m ");
  type(control("Time limit"), "4h");
  type(control("Background processes"), "cleanup");
  await settle();
  const step = await saved(calls, "test");
  assert.deepEqual([step.stall, step.timeout, step.keep_until], ["30m", "4h", "cleanup"]);

  type(control("Idle limit"), "");
  type(control("Time limit"), "");
  type(control("Background processes"), "");
  await settle();
  const cleared = await saved(calls, "test");
  assert.deepEqual([cleared.stall, cleared.timeout, cleared.keep_until], [undefined, undefined, undefined]);
});

test("an agent step has limits but nothing to leave running, and a user step has neither", async () => {
  start();
  await open("#/chain/make-change/change");
  assert.equal(control("Idle limit").placeholder, "15m");
  assert.equal(text(labelled("Idle limit").querySelector(".hint")), "killed after this long with nothing new from the agent; default 15m, 0 = no limit");
  assert.equal(labelled("Background processes"), undefined);

  await open("#/chain/make-change/approve");
  assert.equal(labelled("Idle limit"), undefined);
});

test("the call panel lists its limits and the processes swarm stopped or kept", async () => {
  reset();
  route("GET", `/api/runs/${RUN}/call`, {
    ...callRecord,
    limits: "stopped after 10m with no output and no CPU use, or after 2h in all",
    stopped: ["101 sleep 3600", "102 node server.js"],
    kept_until: "after_down",
    kept: ["103 uvicorn app:app"],
  });
  await openRun(runOf("done", { plan: planDone, test: testDone }, { ended_at: at(16) }));
  click(head("plan"));
  const call = panel(row("plan"), "Call");
  click(call.querySelector("summary"));
  await settle(20);

  assert.match(call.querySelector(".call-meta").textContent, /stopped after 10m with no output and no CPU use, or after 2h in all/);
  const stopped = panel(call, "Killed at step end (2)");
  assert.equal(stopped.querySelector("pre").textContent, "101 sleep 3600\n102 node server.js");
  assert.equal(panel(call, "Kept until after_down ends (1)").querySelector("pre").textContent, "103 uvicorn app:app");
});

test("the run page lists processes stopped outside a step and keeps the list open across updates", async () => {
  reset();
  const run = runOf("stopped", { plan: planDone }, { ended_at: at(16), stopped: ["after Up: 201 vite"] });
  const source = await openRun(run);
  const list = () => $("#stopped details");
  assert.equal(list().querySelector("summary").textContent, "Killed outside a step (1)");
  click(list().querySelector("summary"));
  list().open = true;

  source.send({ ...run, cost: 1 });
  await settle(20);
  assert.equal(list().open, true);

  source.send({ ...run, stopped: [...run.stopped, "when the run ended: 202 node"] });
  await settle(20);
  assert.equal(list().querySelector("summary").textContent, "Killed outside a step (2)");
  assert.equal(list().open, true);

  source.send({ ...run, stopped: [] });
  await settle(20);
  assert.equal(list(), null);
});
