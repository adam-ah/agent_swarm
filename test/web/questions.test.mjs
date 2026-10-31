import test from "node:test";
import assert from "node:assert/strict";
import { $, $$, calls, click, dialogs, fail, page, reset, route, settle, type } from "./page.mjs";
import { RUN, at, head, library as runLibrary, openRun, panel, planDone, row, runOf, texts } from "./run-fixtures.mjs";
import { library, makeChange, open, shipChangeset, solo, start } from "./editor-fixtures.mjs";

const posted = () => calls.filter((c) => c.method === "POST" && c.path === `/api/runs/${RUN}/decide`).map((c) => JSON.parse(c.body));
const button = (label, root = globalThis.document) => [...root.querySelectorAll("button")].find((b) => b.textContent === label);
const FIRST = ["Which login page?", "Keep the old error text?"];

test("an agent that asks waits for an answer to each question, or for you to let it decide", async () => {
  reset();
  route("POST", `/api/runs/${RUN}/decide`, { ok: "true" });
  const asking = (asks) => runOf("waiting", { plan: { status: "waiting", started_at: at(15), log: "plan", asks } });
  const source = await openRun(asking([{ questions: FIRST, asked_at: at(15, 30) }]));

  assert.equal($(".decision .decision-head").textContent, "Plan the fix asks you");
  assert.deepEqual(texts($(".decision"), ".ask .question"), FIRST);
  assert.equal(row("plan").querySelector(".srow-line").textContent, "Asking you questions above");
  type($$(".decision textarea")[0], "The admin one");
  click(button("Send answers"));
  await settle();
  assert.deepEqual(posted(), [{ choice: "answer", answers: ["The admin one", ""], step: "plan" }]);
  assert.equal(button("Send answers").disabled, true);

  source.send(asking([{ questions: FIRST, answers: ["The admin one", ""], answered_at: at(16), log: "plan-ask-1" }, { questions: ["Which browser?"], asked_at: at(16, 30) }]));
  assert.deepEqual(texts($(".decision"), ".ask .question"), ["Which browser?"]);
  route("POST", `/api/runs/${RUN}/decide`, fail(409, "the run is not waiting"));
  click(button("Decide yourself"));
  await settle();
  assert.deepEqual(posted().at(-1), { choice: "decide", step: "plan" });
  assert.equal(dialogs.alert.at(-1), "the run is not waiting");
  assert.equal(button("Decide yourself").disabled, false);
});

test("parallel items waiting concurrently each get their own answer, headed by their item", async () => {
  reset();
  route("POST", `/api/runs/${RUN}/decide`, { ok: "true" });
  const lib = structuredClone(runLibrary);
  lib.chains[1].steps.push({ id: "check", title: "Check the part", user: "Is {{part}} right?" });
  const files = ["auth.go", "auth_test.go"];
  const items = (checks) => runOf("waiting", { plan: planDone, build: { status: "running", children: checks.map((check, i) => ({
    id: String(i + 1), path: `build.${i + 1}`, title: files[i], kind: "item", uses: "part", status: "running",
    children: [
      { id: "compile", path: `build.${i + 1}.compile`, title: "Compile it", kind: "tool", uses: "compile", status: "done" },
      { id: "check", path: `build.${i + 1}.check`, title: "Check the part", kind: "user", uses: "", ...check },
    ],
  })) } });
  const waits = (file) => ({ status: "waiting", question: `Is ${file} right?` });
  const source = await openRun(items([waits("auth.go"), waits("auth_test.go")]), lib);

  assert.deepEqual(texts($("#decision"), ".decision-head"), ["auth.go · Check the part", "auth_test.go · Check the part"]);
  assert.deepEqual(texts($("#decision"), ".question"), ["Is auth.go right?", "Is auth_test.go right?"]);
  assert.equal(row("build.2.check").querySelector(".srow-line").textContent, "Waiting for your answer above");
  const [first, second] = $$(".decision");
  type(second.querySelector("textarea"), "Too slow");
  click(button("Approve", first));
  await settle();
  assert.deepEqual(posted(), [{ choice: "approve", note: "", step: "build.1.check" }]);

  source.send(items([{ status: "done", headline: "Approved" }, waits("auth_test.go")]));
  assert.deepEqual($$(".decision"), [second]);
  assert.equal(second.querySelector("textarea").value, "Too slow");
  click(button("Reject", second));
  await settle();
  assert.deepEqual(posted().at(-1), { choice: "reject", note: "Too slow", step: "build.2.check" });
});

test("a step that asked lists each question with its answer and how it went on", async () => {
  reset();
  const asks = [
    { questions: FIRST, answers: ["The admin one", " "], answered_at: at(16), log: "plan-ask-1" },
    { questions: ["Which browser?"], decided: true, answered_at: at(17), log: "plan-ask-2" },
  ];
  await openRun(runOf("running", { plan: { ...planDone, asks } }));

  assert.ok(texts(row("plan"), ".chip").includes("asked you"));
  click(head("plan"));
  const asked = panel(row("plan"), "Questions it asked you");
  assert.equal(asked.querySelector("summary").textContent, "Questions it asked you (3)");
  assert.deepEqual(texts(asked, "dt"), [...FIRST, "Which browser?"]);
  assert.deepEqual(texts(asked, "dd"), ["The admin one", "left to the agent", "left to the agent"]);
  assert.equal(asked.querySelectorAll("dd.hint").length, 2);
  assert.deepEqual(texts(asked, ".call > summary"), ["How it went on after your answers (round 1)", "How it went on after your answers (round 2)"]);
});

test("a prompt that returns questions says it can ask, and its step offers the answers to later steps", async () => {
  const chain = makeChange();
  chain.prompts.change.returns = [...chain.prompts.change.returns, { name: "questions", value: "list" }];
  start(library([chain, shipChangeset(), solo()]));
  await open("#/prompt/make-change/change");

  assert.match($$(".panel-body.used-by")[1].lastChild.textContent, /^It can ask you questions and waits for your answers/);
  page("editor = { at: { name: 'make-change' } }");
  assert.deepEqual(page("fieldsOf")({ agent: "change" }), ["summary", "files", "verdict", "questions", "answers"]);
  page("editor = null");
});
