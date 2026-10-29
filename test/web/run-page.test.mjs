import test from "node:test";
import assert from "node:assert/strict";
import { $, $$, answers, calls, click, dialogs, fail, go, page, reset, route, settle, sources, type } from "./page.mjs";
import { RUN, at, callRecord, has, head, isOpen, library, openRun, panel, planDone, press, row, runOf, testDone, texts } from "./run-fixtures.mjs";

const hash = () => globalThis.location.hash;
const buttons = () => texts($("#meta"), "button");
const button = (label) => $$("button").find((b) => b.textContent === label);
const posted = (path) => calls.filter((c) => c.method === "POST" && c.path === path).map((c) => JSON.parse(c.body || "null"));

test("a live run follows its event stream and closes it on leaving", async () => {
  reset();
  const source = await openRun(runOf("running", { plan: { status: "running", started_at: at(15), actions: 2, log: "plan" } }));
  assert.equal(source.url, `/api/runs/${RUN}/events`);
  assert.equal($("#subject").textContent, "Login fails");
  assert.equal($(".page[data-run] .eyebrow a").getAttribute("href"), "#/chain/fix");
  assert.match($("#meta .pill").textContent, /Running/);
  assert.deepEqual(buttons(), ["Stop run"]);
  assert.equal($("#banner").textContent, "");
  assert.equal(row("plan").parentElement.className, "slot");
  assert.match(row("plan").className, /\brunning\b/);
  assert.match(row("plan").querySelector(".srow-line").textContent, /^Working · 2 actions · /);
  assert.equal(row("plan").querySelector(".srow-stats").textContent, "");
  assert.ok(calls.some((c) => c.path === "/api/runs"));
  assert.equal($(".ol-if .ol-value").textContent, "");

  source.send(runOf("running", { plan: planDone, test: { status: "running", started_at: at(15, 42) } }));
  assert.match(row("plan").className, /\bdone\b/);
  assert.equal(row("plan").querySelector(".state").textContent, "✓");
  assert.equal(row("plan").querySelector(".srow-line").textContent, "Change the session check");
  assert.equal(row("plan").querySelector(".srow-stats").textContent, "42.0s · $0.12");
  assert.equal(row("plan").querySelector(".srow-caret").textContent, "▸");
  assert.match(row("test").querySelector(".srow-line").textContent, /^Running · /);

  source.send(runOf("running", { plan: planDone, test: { ...testDone, output: { verdict: "" } } }));
  assert.equal($(".ol-if .ol-value").textContent, "value empty");

  const verdict = "pass ".repeat(20).trim();
  source.send(runOf("done", { plan: planDone, test: { ...testDone, output: { verdict } }, ship: { status: "done" } }, { ended_at: at(16) }));
  assert.equal($(".ol-if .ol-value").textContent, `value “${verdict.slice(0, 60)}…”`);
  assert.equal($(".ol-if .ol-value").title, verdict);
  assert.ok($(".ol-if").classList.contains("ol-ran"));
  assert.match($("#meta .pill").textContent, /Done/);

  await go("#/");
  assert.equal(source.closed, true);
});

test("the subject falls back to the first input, then the title, can link out, and shows in full on click", async () => {
  reset();
  const source = await openRun(runOf("running", {}, { subject: "", link: "https://example.test/issue/7", inputs: { empty: "", request: "Login fails" } }));
  const link = $("#subject a");
  assert.equal(link.textContent, "Login fails");
  assert.equal($("#subject").title, "Login fails");
  assert.equal(link.getAttribute("href"), "https://example.test/issue/7");
  assert.equal(link.getAttribute("target"), "_blank");
  click(link);
  assert.equal($("#subject").classList.contains("whole"), false);

  source.send(runOf("running", {}, { subject: "", inputs: {} }));
  assert.equal($("#subject").textContent, "Fix a bug");
  click($("#subject"));
  assert.equal($("#subject").classList.contains("whole"), true);
  click($("#subject"));
  assert.equal($("#subject").classList.contains("whole"), false);
});

test("rows open on click or key, and a failed step opens by itself", async () => {
  reset();
  const failed = { status: "failed", started_at: at(15, 42), ended_at: at(15, 50), error: "2 tests failed\nTestLogin", log: "test", output: { verdict: "fail" } };
  const run = runOf("failed", { plan: planDone, test: failed }, { ended_at: at(15, 50) });
  const source = await openRun(run);

  assert.match(row("test").className, /\bopen\b/);
  assert.equal(row("test").querySelector(".error-box").textContent, failed.error);
  assert.equal(has(row("test"), ".srow-line"), false);
  assert.equal(head("test").getAttribute("aria-expanded"), "true");
  assert.equal(row("test").querySelector(".srow-caret").textContent, "▾");

  assert.equal(isOpen("plan"), false);
  click(head("plan"));
  assert.equal(head("plan").getAttribute("aria-expanded"), "true");
  const returned = panel(row("plan"), "What the agent returned");
  assert.equal(returned.open, true);
  assert.deepEqual(texts(returned, "dt"), ["files"]);
  assert.deepEqual(texts(returned, "li"), ["auth.go", "auth_test.go"]);
  click(head("plan"));
  assert.equal(isOpen("plan"), false);

  press(head("plan"), "Enter");
  assert.equal(isOpen("plan"), true);
  press(head("plan"), " ");
  assert.equal(isOpen("plan"), false);
  press(head("plan"), "a");
  assert.equal(isOpen("plan"), false);

  assert.equal(head("ship").getAttribute("role"), null);
  click(head("ship"));
  assert.equal(isOpen("ship"), false);

  click(head("test"));
  assert.equal(isOpen("test"), false);
  assert.equal(row("test").querySelector(".srow-line").textContent, "2 tests failed");
  source.send({ ...run, steps: run.steps.map((s) => (s.id === "test" ? { ...s, cost: 0.5 } : s)) });
  assert.equal(isOpen("test"), false, "a failed step opens only the first time it fails");
});

test("opening a step of a live run shows how it was called", async () => {
  reset();
  route("GET", `/api/runs/${RUN}/call`, callRecord);
  await openRun(runOf("running", { plan: planDone, test: { status: "running", started_at: at(15, 42), log: "test" } }));
  click(head("plan"));
  await settle(20);
  const call = panel(row("plan"), "How it was called");
  assert.equal(call.open, true);
  assert.ok(calls.some((c) => c.path === `/api/runs/${RUN}/call` && c.query.log === "plan"));
  assert.equal(call.querySelector(".cmd-line").textContent, "$ claude");
});

test("opening a step after the run ended leaves how it was called closed", async () => {
  reset();
  route("GET", `/api/runs/${RUN}/call`, callRecord);
  await openRun(runOf("done", { plan: planDone, test: testDone }, { ended_at: at(16) }));
  click(head("plan"));
  await settle(20);
  const call = panel(row("plan"), "How it was called");
  assert.equal(call.open, false);
  assert.equal(calls.filter((c) => c.path.endsWith("/call")).length, 0);

  click(call.querySelector("summary"));
  await settle(20);
  assert.equal(calls.filter((c) => c.path.endsWith("/call")).length, 1);
  assert.match(call.textContent, /Planned the fix/);
});

test("a waiting run takes a decision: send back with a note, approve, or reject", async () => {
  reset();
  route("POST", `/api/runs/${RUN}/decide`, { ok: "true" });
  const review = { status: "waiting", started_at: at(16), question: "Ship this change?", artifacts: [{ kind: "image", path: "/runs/1/after.png", name: "after.png" }] };
  const waiting = (changes) => runOf("waiting", { plan: planDone, test: testDone, review: { ...review, ...changes } });
  const source = await openRun(waiting({}));

  assert.deepEqual(buttons(), ["Stop run"]);
  assert.equal($(".decision .decision-head").textContent, "Review the change");
  assert.equal($(".decision .question").textContent, "Ship this change?");
  assert.equal($$(".decision .stills img").length, 1);
  assert.equal(row("review").querySelector(".srow-line").textContent, "Waiting for your answer above");
  const note = $(".decision textarea");
  assert.equal(note.placeholder, "Note: what to change. Needed to send it back to plan.");
  assert.deepEqual(texts($(".decision .row"), "button"), ["Approve", "Send back to plan", "Reject"]);

  click(button("Send back to plan"));
  await settle();
  assert.deepEqual(posted(`/api/runs/${RUN}/decide`), []);
  assert.equal(note.placeholder, "Say what to change, then send it back");

  type(note, "Check the expiry too");
  click(button("Send back to plan"));
  await settle();
  assert.deepEqual(posted(`/api/runs/${RUN}/decide`), [{ choice: "revise", note: "Check the expiry too" }]);
  assert.equal(button("Send back to plan").disabled, true);

  source.send(waiting({}));
  assert.ok($(".decision textarea") === note, "the same question keeps its form");

  source.send(waiting({ round: 1 }));
  assert.ok($(".decision textarea") !== note);
  assert.ok(texts(row("review"), ".chip").includes("attempt 2"));
  click(button("Approve"));
  await settle();
  assert.deepEqual(posted(`/api/runs/${RUN}/decide`).at(-1), { choice: "approve", note: "" });

  source.send(waiting({ round: 2, revise: "" }));
  assert.deepEqual(texts($(".decision .row"), "button"), ["Approve", "Reject"]);
  assert.equal($(".decision textarea").placeholder, "Optional note");
  route("POST", `/api/runs/${RUN}/decide`, fail(409, "the run is not waiting"));
  click(button("Reject"));
  await settle();
  assert.deepEqual(posted(`/api/runs/${RUN}/decide`).at(-1), { choice: "reject", note: "" });
  assert.deepEqual(dialogs.alert, ["the run is not waiting"]);
  assert.equal(button("Reject").disabled, false);

  source.send(runOf("running", { plan: planDone, test: testDone, review: { status: "done" } }));
  assert.equal($("#decision").textContent, "");
});

test("the stop button asks first and reports a refusal", async () => {
  reset();
  route("POST", `/api/runs/${RUN}/stop`, { ok: "true" });
  await openRun(runOf("running", { plan: { status: "running", started_at: at(15) } }));
  const stops = () => calls.filter((c) => c.path === `/api/runs/${RUN}/stop`);

  answers.confirm = false;
  click(button("Stop run"));
  await settle();
  assert.equal(stops().length, 0);
  assert.match(dialogs.confirm[0], /Stop this run\?/);

  answers.confirm = true;
  click(button("Stop run"));
  await settle();
  assert.equal(stops().length, 1);
  assert.equal(stops()[0].method, "POST");

  route("POST", `/api/runs/${RUN}/stop`, fail(409, "the run already ended"));
  click(button("Stop run"));
  await settle();
  assert.deepEqual(dialogs.alert, ["the run already ended"]);
});

test("a failed run names its step and reruns from it or from scratch", async () => {
  reset();
  const fresh = "1028-110000";
  route("POST", `/api/runs/${RUN}/rerun`, (c) => ({ id: JSON.parse(c.body).from === "scratch" ? fresh : RUN }));
  const failed = { status: "failed", started_at: at(15, 42), ended_at: at(15, 50), error: "2 tests failed", log: "test" };
  await openRun(runOf("failed", { plan: planDone, test: failed }, { ended_at: at(15, 50), cost: 0.42 }));

  assert.equal($(".banner.failed").textContent, "Failed at Run the tests");
  assert.deepEqual(buttons(), ["Rerun from Run the tests", "Rerun from scratch", "Delete run"]);
  assert.ok(texts($("#meta"), "span").includes("$0.42"));

  click(head("test"));
  assert.equal(isOpen("test"), false);
  click($(".banner a"));
  assert.equal(isOpen("test"), true);
  assert.ok(row("test").classList.contains("flash"));
  assert.equal(hash(), `#/run/${RUN}`);

  click(button("Rerun from Run the tests"));
  await settle(20);
  assert.deepEqual(posted(`/api/runs/${RUN}/rerun`), [{ from: "failed" }]);
  assert.equal(hash(), `#/run/${RUN}`);
  assert.equal(calls.at(-1).path, "/api/runs");

  click(button("Rerun from scratch"));
  await settle(30);
  assert.deepEqual(posted(`/api/runs/${RUN}/rerun`).at(-1), { from: "scratch" });
  assert.equal(hash(), `#/run/${fresh}`);
  assert.equal(sources.at(-1).url, `/api/runs/${fresh}/events`);
});

test("a rerun the server refuses is reported", async () => {
  reset();
  route("POST", `/api/runs/${RUN}/rerun`, fail(400, "the run folder is gone"));
  const compile = { id: "compile", path: "build.compile", title: "Compile it", kind: "tool", uses: "compile", status: "failed", error: "cc: error" };
  await openRun(runOf("failed", { plan: planDone, test: testDone, build: { status: "failed", children: [compile] } }, { ended_at: at(16) }));
  assert.equal($(".banner.failed").textContent, "Failed at Build each part");
  click(button("Rerun from Compile it"));
  await settle();
  assert.deepEqual(dialogs.alert, ["the run folder is gone"]);
  assert.equal(hash(), `#/run/${RUN}`);
});

test("an ended run reruns from the failed step, the loop it is in, each parent, or from scratch", async () => {
  reset();
  route("POST", `/api/runs/${RUN}/rerun`, { id: RUN });
  const lib = structuredClone(library);
  lib.chains[0].steps.splice(2, 0, { id: "again", title: "Test again", repeat: "plan", when: "test.verdict != pass", rounds: 3 });
  const looped = runOf("failed", { plan: planDone, test: testDone }, { ended_at: at(16) });
  looped.steps.splice(2, 0, { id: "again", path: "again", title: "Test again", kind: "repeat", uses: "plan", status: "failed", error: "all 3 rounds ran" });
  const source = await openRun(looped, lib);
  assert.deepEqual(buttons(), ["Rerun from Test again", "Rerun from Plan the fix (start of the loop)", "Rerun from scratch", "Delete run"]);
  click(button("Rerun from Plan the fix (start of the loop)"));
  await settle();
  assert.deepEqual(posted(`/api/runs/${RUN}/rerun`), [{ from: "step", step: "plan" }]);

  const compile = { id: "compile", path: "build.1.compile", title: "Compile it", kind: "tool", uses: "compile", status: "failed", error: "cc: error" };
  const item = { id: "1", path: "build.1", title: "auth.go", kind: "item", uses: "part", status: "failed", children: [compile] };
  source.send(runOf("failed", { plan: planDone, test: testDone, build: { status: "failed", children: [item] } }, { ended_at: at(16) }));
  assert.deepEqual(buttons(), ["Rerun from Compile it", "Rerun from auth.go (this item)", "Rerun from Build each part (the whole sub-chain)", "Rerun from scratch", "Delete run"]);
  route("POST", `/api/runs/${RUN}/rerun`, fail(400, "the run folder is gone"));
  click(button("Rerun from auth.go (this item)"));
  await settle();
  assert.deepEqual(posted(`/api/runs/${RUN}/rerun`).at(-1), { from: "step", step: "build.1" });
  assert.deepEqual(dialogs.alert, ["the run folder is gone"]);

  source.send(runOf("done", { plan: planDone, test: testDone }, { ended_at: at(16), resumed_at: at(15, 55), rerun_from: "Plan the fix" }));
  assert.ok(texts($("#meta"), "span").some((t) => t.startsWith("rerun from Plan the fix at ")));
});

test("stopped, rejected and interrupted runs say how they ended", async () => {
  reset();
  const stopped = { status: "stopped", started_at: at(15, 42), ended_at: at(15, 50), ended: "Stopped by you" };
  const source = await openRun(runOf("stopped", { plan: planDone, test: stopped }, { ended_at: at(15, 50) }));
  assert.equal($(".banner.stopped").textContent, "Stopped at Run the tests: Stopped by you");
  assert.deepEqual(buttons(), ["Rerun from Run the tests", "Rerun from scratch", "Delete run"]);

  const rejected = { status: "rejected", output: { decision: "reject", note: "Too risky" } };
  source.send(runOf("rejected", { plan: planDone, test: testDone, build: { status: "skipped" }, review: rejected }, { ended_at: at(16) }));
  assert.equal($(".banner.rejected").textContent, "Rejected at Review the change: Too risky");
  assert.equal(buttons()[0], "Rerun from Review the change");

  const done = { status: "done" };
  const all = { plan: planDone, test: testDone, build: done, review: done, ship: done, learn: { status: "skipped" }, cleanup: done };
  source.send(runOf("interrupted", all, { reason: "because the server restarted", resumed_at: at(17), ended_at: at(18) }));
  assert.equal($(".banner.interrupted").textContent, "Interrupted because the server restarted");
  assert.equal(buttons()[0], "Rerun from where it ended");
  assert.ok(texts($("#meta"), "span").some((t) => t.startsWith("rerun from its failed step at ")));
});

test("a finished run can be deleted after confirming", async () => {
  reset();
  route("DELETE", `/api/runs/${RUN}`, fail(500, "cannot remove the run folder"));
  await openRun(runOf("done", { plan: planDone, test: testDone }, { ended_at: at(16) }));
  assert.equal($("#banner").textContent, "");
  assert.deepEqual(buttons(), ["Delete run"]);
  const deletes = () => calls.filter((c) => c.method === "DELETE");

  answers.confirm = false;
  click(button("Delete run"));
  await settle();
  assert.equal(deletes().length, 0);

  answers.confirm = true;
  click(button("Delete run"));
  await settle(20);
  assert.equal(deletes().length, 1);
  assert.deepEqual(dialogs.alert, ["cannot remove the run folder"]);
  assert.equal(hash(), `#/run/${RUN}`);

  route("DELETE", `/api/runs/${RUN}`, { ok: "true" });
  click(button("Delete run"));
  await settle(30);
  assert.equal(deletes().length, 2);
  assert.equal(deletes()[1].path, `/api/runs/${RUN}`);
  assert.equal(hash(), "#/");
  assert.match(dialogs.confirm[0], /Delete this run/);
});

test("sub-chains nest their rows, and per-item runs replace the outline", async () => {
  reset();
  const compile = { id: "compile", path: "build.compile", title: "Compile it", kind: "tool", uses: "compile", status: "done", started_at: at(15, 50), ended_at: at(15, 55), output: { output: "built" } };
  const source = await openRun(runOf("running", { plan: planDone, test: testDone, build: { status: "running", started_at: at(15, 50), cost: 0.5, tokens: 20000, children: [compile] } }));
  assert.equal(head("build").getAttribute("role"), null);
  assert.equal(row("build").querySelector(".kind").textContent, "C");
  assert.equal(row("build").querySelector(".srow-stats").textContent, "20k tokens");
  assert.equal(has(row("build"), '.kids .slot [id="row-build.compile"]'), true);
  click(head("build"));
  assert.equal(isOpen("build"), false);

  const item = (n, title, status) => ({
    id: String(n), path: `build.${n}`, title, kind: "item", uses: "part", status,
    children: [{ id: "compile", path: `build.${n}.compile`, title: "Compile it", kind: "tool", uses: "compile", status, started_at: at(15, 50), ended_at: at(15, 51) }],
  });
  const headline = "Runs part 2 times, once per item of plan.files";
  source.send(runOf("running", { plan: planDone, test: testDone, build: { status: "running", headline, children: [item(1, "auth.go", "done"), item(2, "auth_test.go", "running")] } }));
  assert.equal(row("build").querySelector(".srow-line").textContent, headline);
  assert.equal(has(row("build"), '[id="row-build.compile"]'), false);
  assert.deepEqual(texts(row("build"), ".srow.item > .srow-head .kind"), ["1", "2"]);
  assert.equal(row("build.1").querySelector(".srow-title").textContent, "auth.go");
  assert.equal(has(row("build.1"), '[id="row-build.1.compile"]'), true);

  click(head("build.1.compile"));
  const reads = panel(row("build.1.compile"), "What it read");
  assert.match(reads.textContent, /asked when the run starts: “Part”/);
  assert.equal(has(reads, ".ol-value"), false);
  assert.deepEqual([...row("build.1.compile").querySelectorAll(".srow-links a")].map((a) => a.getAttribute("href")), ["#/tool/compile", "#/chain/part/compile"]);

  source.send(runOf("running", { plan: planDone, test: { ...testDone, output: { verdict: "fail" } }, ship: { status: "skipped" }, learn: { status: "running" } }));
  assert.ok($(".ol-if").classList.contains("ol-skipped"));
  assert.ok($(".ol-approved").classList.contains("ol-skipped"));
  assert.ok($(".ol-rejected").classList.contains("ol-ran"));
});

test("running times tick every second until a step ends", async () => {
  reset();
  await openRun(runOf("running", { plan: { status: "running", started_at: at(15) } }));
  const live = $("#meta [data-since]");
  const working = row("plan").querySelector(".srow-line [data-since]");
  live.textContent = "-";
  working.textContent = "-";
  await settle(1100);
  assert.notEqual(live.textContent, "-");
  assert.notEqual(working.textContent, "-");
});

test("leaving with unsaved editor changes is held back on page unload", async () => {
  reset();
  const event = new globalThis.Event("beforeunload", { cancelable: true });
  page("editor = { dirty: true }");
  globalThis.window.dispatchEvent(event);
  page("editor = null");
  assert.equal(event.defaultPrevented, true);
});
