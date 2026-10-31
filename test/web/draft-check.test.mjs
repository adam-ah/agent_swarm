import test from "node:test";
import assert from "node:assert/strict";
import { $, $$, calls, click, fail, go, route, settle, type } from "./page.mjs";
import { body, button, control, open, start, text } from "./editor-fixtures.mjs";

const CHECKED = 200;
const problems = () => text($("#problems"));
const checks = () => calls.filter((c) => c.method === "POST" && c.path === "/api/check").map(body);
const uses = () => $$(".used-by .use").map((u) => [...u.children].map(text));

test("the editor checks the unsaved chain as you edit and shows what swarm would say", async () => {
  start();
  let reply = {
    "chains/make-change.json": { problems: ["step \"merge\": no tool named \"gone\""], warnings: [] },
    "tools/merge.json": { problems: ["a tool needs a title"] },
  };
  route("POST", "/api/check", () => reply);
  await open("#/chain/make-change/merge");
  calls.length = 0;
  type(control("Title"), "Merge it");
  assert.equal(problems(), "");
  await settle(CHECKED);

  assert.equal(problems(), "Unsaved; swarm check fails:step \"merge\": no tool named \"gone\"tools/merge.json: a tool needs a title");
  const sent = checks().at(-1);
  assert.equal(sent.chains["make-change"].steps.find((s) => s.id === "merge").title, "Merge it");
  assert.deepEqual([sent.prompts, sent.tools], [{}, {}]);

  reply = { "chains/make-change.json": { problems: [], warnings: ["approve: nothing reads approve.note"] }, "chains/solo.json": { problems: [], warnings: ["only: reads {{x}}"] } };
  type(control("What it does"), "Merge the branch now");
  await settle(CHECKED);
  assert.equal(problems(), "Unsaved; warnings:approve: nothing reads approve.notechains/solo.json: only: reads {{x}}");
  assert.equal(checks().at(-1).tools.merge.title, "Merge the branch now");

  reply = { "chains/make-change.json": { problems: [], warnings: [] } };
  type(control("Title"), "Merge");
  await settle(CHECKED);
  assert.equal(problems(), "Unsaved; swarm check passes.");

  route("PUT", /^\/api\/items\/chains\/make-change\.json$/, () => fail(422, "refused", ["step \"merge\": nope"]));
  click(button("Save"));
  await settle();
  assert.equal(problems(), "Save refused:step \"merge\": nope");
  type(control("Title"), "Merge again");
  await settle(CHECKED);
  assert.equal(problems(), "Unsaved; swarm check passes.");
});

test("a check that answers after you leave the chain changes nothing", async () => {
  start();
  route("POST", "/api/check", { "chains/make-change.json": { problems: ["late"], warnings: [] } });
  await open("#/chain/make-change/merge");
  type(control("Title"), "Merge it");
  await open("#/chain/ship-changeset");
  await settle(CHECKED);
  assert.equal(problems(), "");
});

test("a tool page shows what is wrong with each chain using it, and checks the unsaved tool", async () => {
  start();
  route("POST", "/api/check", { "tools/read_issue.json": { problems: ["a tool needs a run command"] }, "chains/make-change.json": { problems: [], warnings: ["read_issue: reads {{who}}"] } });
  await go("#/tool/read_issue");
  await settle();
  const solo = ["Solo", "Only step", "cannot run: only: the tool read_issue needs a value for issue"];
  assert.deepEqual(uses(), [["Make a change", "Read the issue", "⚠ approve: nothing reads approve.note"], solo]);

  type(control("Command"), "");
  await settle(CHECKED);
  assert.deepEqual(checks().at(-1), { tools: { read_issue: { title: "Read an issue", run: "" } } });
  assert.equal(text($(".savebar .status")), "Unsaved; invalid: a tool needs a run command");
  assert.deepEqual(uses(), [["Make a change", "Read the issue", "⚠ read_issue: reads {{who}}"], solo]);
});

test("a prompt page checks the unsaved text against its chain", async () => {
  start();
  route("POST", "/api/check", { "chains/make-change.json": { problems: ["step \"change\": chains/make-change/change.md has no fields"], warnings: [] } });
  await go("#/prompt/make-change/change");
  await settle();
  type($("#main textarea.prompt"), "Just do it.");
  await settle(CHECKED);
  assert.deepEqual(checks().at(-1), { prompts: { "make-change/change": "Just do it." } });
  assert.equal(text($(".savebar .status")), "Unsaved");
  assert.deepEqual(uses(), [["Make a change", "Make the change", "cannot run: step \"change\": chains/make-change/change.md has no fields"]]);
});
