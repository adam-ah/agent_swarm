import test from "node:test";
import assert from "node:assert/strict";
import { $, $$, calls, click, fail, page, route, settle, type } from "./page.mjs";
import { body, control, labelled, open, start, text } from "./editor-fixtures.mjs";

const startButton = () => [...$("form.start").querySelectorAll("button")].find((b) => b.textContent === "Start run");
const fields = () => $$("form.start .start-fields > label").map((l) => [l.className.trim(), text(l.querySelector("span")), l.querySelector("input, textarea").tagName.toLowerCase()]);

test("the start form asks for the chain's inputs, starting from this project's values", async () => {
  start();
  await open("#/chain/make-change");

  assert.equal(text($("form.start .editbar-title")), "Start a run");
  assert.deepEqual(fields(), [
    ["field wide", "What should change?", "textarea"],
    ["field", "Test command optional · from Settings", "input"],
  ]);
  const [request, testCommand] = $$("form.start .start-fields textarea, form.start .start-fields input");
  assert.equal(request.placeholder, "Describe it in plain words");
  assert.equal(request.value, "");
  assert.equal(testCommand.value, "make test");
  assert.equal(testCommand.placeholder, "leave empty to skip");
  assert.equal($("form.start .hint[title]").getAttribute("title"), "This project's starting value, set in Settings");
  assert.equal(startButton().disabled, false);
});

test("starting a run sends the inputs and opens the new run", async () => {
  start();
  const posted = [];
  route("POST", "/api/runs", (call) => {
    posted.push(body(call));
    return { id: "1028-120000" };
  });
  await open("#/chain/make-change");

  const [request, testCommand] = $$("form.start .start-fields textarea, form.start .start-fields input");
  type(request, "Fix the login page");
  type(testCommand, "");
  click(startButton());
  await settle(30);
  assert.deepEqual(posted, [{ chain: "make-change", inputs: { request: "Fix the login page", test_command: "" } }]);
  assert.ok(calls.some((c) => c.method === "GET" && c.path === "/api/runs"));
  assert.equal(globalThis.location.hash, "#/run/1028-120000");
});

test("the start form shows the swarm run command for the values in it", async () => {
  start();
  await open("#/chain/make-change");
  const command = () => $("form.start .start-cli pre").textContent;

  assert.equal(command(), "swarm run make-change -project /work/demo -input request='...' -input test_command='make test'");
  const [request, testCommand] = $$("form.start .start-fields textarea, form.start .start-fields input");
  type(request, "Fix the user's login page");
  type(testCommand, "");
  assert.equal(command(), "swarm run make-change -project /work/demo -input request='Fix the user'\\''s login page'");
  type(request, "fix-42");
  assert.equal(command(), "swarm run make-change -project /work/demo -input request=fix-42");
});

test("a refused start shows why next to the button", async () => {
  start();
  route("POST", "/api/runs", fail(422, "refused", ["request: it needs a value"]));
  await open("#/chain/make-change");

  click(startButton());
  await settle();
  assert.equal($("form.start .error-text").textContent, "Refused:\n- request: it needs a value");
  assert.equal(globalThis.location.hash, "#/chain/make-change");
});

test("a run cannot start while the chain has unsaved changes", async () => {
  start();
  await open("#/chain/make-change");

  type(control("Title"), "Make a bigger change");
  click(startButton());
  await settle();
  assert.equal($("form.start .error-text").textContent, "Save or discard your changes to the chain first.");
  assert.equal(calls.some((c) => c.method === "POST"), false);
});

test("a chain with problems cannot start, and a chain without inputs shows no fields", async () => {
  start();
  await open("#/chain/solo");
  assert.equal(startButton().disabled, true);
  assert.equal($("form.start .start-fields"), null);

  await open("#/chain/ship-changeset");
  assert.deepEqual(fields(), [
    ["field wide", "Changeset to apply", "textarea"],
    ["field", "Dry run optional", "input"],
    ["field wide", "target", "textarea"],
  ]);
  assert.equal($$("form.start textarea")[1].value, "main");
});

test("only steps after an approval, and not cleanup steps, wait for it", async () => {
  start();
  await open("#/chain/make-change");
  const gate = (id) => page(`approvalBefore(editor.at.draft.steps.find((s) => s.id === ${JSON.stringify(id)}), editor.at.draft.steps)?.id ?? null`);
  assert.equal(gate("review"), null);
  assert.equal(gate("approve"), null);
  assert.equal(gate("merge"), "approve");
  assert.equal(gate("ship"), "approve");
  assert.equal(gate("cleanup"), null);
  assert.equal(labelled("Title").querySelector("input").value, "Make a change");
});
