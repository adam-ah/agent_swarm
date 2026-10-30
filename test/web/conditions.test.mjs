import test from "node:test";
import assert from "node:assert/strict";
import { $, click, page, settle, type } from "./page.mjs";
import { body, button, control, labelled, open, own, shown, start, text } from "./editor-fixtures.mjs";

const read = (condition) => page(`readCondition(${JSON.stringify(condition)})`);
const write = (parts) => page(`writeCondition(${JSON.stringify(parts)})`);
const describe = (condition) => page(`describeCondition(${JSON.stringify(condition)})`);

test("a condition is read as a name, an operator and a value", () => {
  assert.deepEqual(read("change.verdict == DONE"), { name: "change.verdict", op: "is", value: "DONE" });
  assert.deepEqual(read("  approve.decision != 'approve'  "), { name: "approve.decision", op: "isnt", value: "approve" });
  assert.deepEqual(read("review.verdict==\"APPROVE\""), { name: "review.verdict", op: "is", value: "APPROVE" });
  assert.deepEqual(read("output == "), { name: "output", op: "empty", value: "" });
  assert.deepEqual(read("output != \"\""), { name: "output", op: "set", value: "" });
  assert.deepEqual(read("test_command !="), { name: "test_command", op: "set", value: "" });
  assert.deepEqual(read("plan.steps == two words"), { name: "plan.steps", op: "is", value: "two words" });
  for (const unreadable of ["{{ready}} == yes", "Verdict == DONE", "exit_code > 0", "approve.decision", "", undefined]) {
    assert.equal(read(unreadable), null, String(unreadable));
  }
});

test("a condition is written back in the form it is read", () => {
  assert.equal(write({ name: "", op: "is", value: "DONE" }), undefined);
  assert.equal(write({ name: "output", op: "empty", value: "ignored" }), "output == ");
  assert.equal(write({ name: "output", op: "set", value: "" }), "output != ");
  assert.equal(write({ name: "change.verdict", op: "is", value: "DONE" }), "change.verdict == DONE");
  assert.equal(write({ name: "approve.decision", op: "isnt", value: "approve" }), "approve.decision != approve");
  for (const parts of [
    { name: "a.b", op: "is", value: "x" },
    { name: "a.b", op: "isnt", value: "x" },
    { name: "a.b", op: "empty", value: "" },
    { name: "a.b", op: "set", value: "" },
  ]) {
    assert.deepEqual(read(write(parts)), parts);
  }
});

test("a condition is described in plain words, and an unreadable one is shown as written", () => {
  assert.equal(describe("change.verdict == DONE"), "change.verdict is DONE");
  assert.equal(describe("approve.decision != approve"), "approve.decision is not approve");
  assert.equal(describe("output == "), "output is empty");
  assert.equal(describe("output != "), "output is not empty");
  assert.equal(describe("exit_code > 0"), "exit_code > 0");
  assert.equal(describe(undefined), undefined);
});

test("the condition editor sets each operator and offers the names of earlier values", async () => {
  const { saved } = start();
  await open("#/chain/make-change/test");
  const runs = labelled("Runs");
  const parts = () => [...runs.querySelector(".cond").children].map((el) => el.tagName.toLowerCase());
  const when = async () => {
    click(button("Save"));
    await settle();
    return body(saved.findLast((c) => c.path === "/api/items/chains/make-change.json")).steps.find((s) => s.id === "test").when;
  };
  const label = () => [...(own(".ol-if > .ol-label")[0]?.children || [])].map(text);

  assert.equal(shown(control("Runs")), "on");
  assert.deepEqual(parts(), ["select", "input", "select", "input", "datalist"]);
  const [name, value] = runs.querySelectorAll(".cond input");
  assert.equal(name.value, "change.verdict");
  assert.equal(name.placeholder, "request");
  assert.equal(name.getAttribute("list"), runs.querySelector("datalist").id);
  assert.equal(value.value, "DONE");
  assert.deepEqual([...runs.querySelectorAll("datalist option")].map((o) => o.value),
    ["request", "test_command", "branch", "issue_text", "read_issue.output", "change.summary", "change.files", "change.verdict"]);
  assert.deepEqual([...runs.querySelector(".cond select:not(:first-child)").options].map((o) => o.textContent), ["is", "is not", "is empty", "is not empty"]);
  assert.deepEqual([...runs.querySelectorAll(".reads-strip .read")].map((r) => text(r)), ["change.verdictset by Make the change"]);

  const op = () => runs.querySelectorAll(".cond select")[1];
  type(op(), "isnt");
  assert.deepEqual(label(), ["If", "change.verdict", "is not DONE", "set by Make the change"]);
  type(op(), "empty");
  assert.deepEqual(parts(), ["select", "input", "select", "datalist"]);
  assert.deepEqual(label(), ["If", "change.verdict", "is empty", "set by Make the change"]);
  type(op(), "set");
  assert.deepEqual(label(), ["If", "change.verdict", "is not empty", "set by Make the change"]);
  assert.equal(await when(), "change.verdict != ");

  const fresh = labelled("Runs");
  const ops = () => fresh.querySelectorAll(".cond select")[1];
  assert.equal(shown(ops()), "set");
  type(ops(), "is");
  type(fresh.querySelectorAll(".cond input")[1], "STUCK");
  type(fresh.querySelector(".cond input"), " read_issue.output ");
  assert.deepEqual(label(), ["If", "read_issue.output", "is STUCK", "set by Read the issue"]);
  assert.deepEqual([...fresh.querySelectorAll(".reads-strip .read code")].map((c) => c.textContent), ["read_issue.output"]);
  type(fresh.querySelector(".cond input"), "");
  assert.deepEqual(label(), []);
  type(fresh.querySelector(".cond input"), "read_issue.output");

  type(fresh.querySelector(".cond select"), "off");
  assert.deepEqual([...fresh.querySelector(".cond").children].map((el) => el.tagName.toLowerCase()), ["select"]);
  assert.equal(fresh.querySelector(".reads-strip").hidden, true);
  assert.deepEqual(label(), []);
  type(fresh.querySelector(".cond select"), "on");
  assert.equal(fresh.querySelector(".cond input").value, "read_issue.output");
  assert.equal(await when(), "read_issue.output == STUCK");

  type(labelled("Runs").querySelector(".cond select"), "off");
  assert.equal(await when(), undefined);
});

test("a condition the editor cannot read is kept as text and edited as text", async () => {
  const { saved } = start();
  await open("#/chain/solo/only");

  assert.deepEqual([...$(".ol-if > .ol-label").children].map(text), ["If", "{{ready}} == yes"]);
  assert.deepEqual(own(".tree-note").map(text), ["⤷ stops the run here if its exit_code > 0"]);
  const runs = labelled("Runs");
  const raw = runs.querySelector(".cond input");
  assert.equal(raw.value, "{{ready}} == yes");
  assert.equal(raw.className, "code wide");
  assert.equal(text(runs.querySelector(".cond .hint")), "write it as name == value or name != value");
  assert.equal(labelled("Stops the run after it").querySelector(".cond input").value, "exit_code > 0");
  assert.equal(labelled("Message when it stops the run").hidden, false);

  type(raw, "ready == yes");
  assert.deepEqual([...$(".ol-if > .ol-label").children].map(text), ["If", "ready", "is yes", "nothing sets it"]);
  type(labelled("Stops the run after it").querySelector(".cond input"), "only.output == ");
  assert.deepEqual(own(".tree-note").map(text), ["⤷ stops the run here if its only.output is empty"]);

  click(button("Save"));
  await settle();
  const step = body(saved.at(-1)).steps[0];
  assert.equal(step.when, "ready == yes");
  assert.equal(step.stop_if, "only.output == ");
});
