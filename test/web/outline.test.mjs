import test from "node:test";
import assert from "node:assert/strict";
import { $, $$, click, page } from "./page.mjs";
import { library, open, row, start, text } from "./editor-fixtures.mjs";

const tool = (id, title, extra = {}) => ({ id, title, tool: "cleanup", ...extra });

function demo() {
  return {
    name: "demo",
    title: "Outline demo",
    inputs: [{ name: "test_command", label: "Test command", optional: true }, { name: "request", label: "" }],
    vars: [{ name: "branch", value: "main" }],
    steps: [
      { id: "plan", title: "Plan", agent: "plan", when: "test_command != " },
      tool("recall", "Recall", { when: "memory == " }),
      tool("on_main", "On main", { when: "branch == main" }),
      tool("also_main", "Also on main", { when: "branch == main" }),
      tool("ghost", "Ghost", { when: "nope.field == x" }),
      tool("mystery", "Mystery", { when: "mystery != 1" }),
      tool("asked", "Asked", { when: "request == x" }),
      tool("feedback", "Feedback", { when: "feedback == " }),
      tool("planned", "Planned", { when: "plan.output != " }),
      { id: "gate", title: "Go ahead?", user: "Go ahead?" },
    ],
    source: "project",
  };
}

const undo = () => ({
  name: "undo",
  title: "Undo on rejection",
  steps: [
    { id: "gate", title: "Ship it?", user: "Ship it?" },
    tool("undo", "Undo", { when: "gate.decision == reject", always: true }),
    tool("tidy", "Tidy", { always: true }),
  ],
  source: "project",
});

const tidy = () => ({
  name: "tidy",
  title: "Tidy after a rejection",
  steps: [{ id: "gate", title: "Ship it?", user: "Ship it?" }, tool("tidy", "Tidy", { always: true })],
  source: "project",
});

const plain = () => ({
  name: "plain",
  title: "No approval",
  steps: [tool("early", "Early cleanup", { always: true }), tool("build", "Build"), tool("tidy", "Tidy", { always: true }), tool("report", "Report", { always: true })],
  source: "project",
});

const labels = (selector) => $$(selector).map((block) => [...block.querySelector(":scope > .ol-label").children].map(text));
const kids = (block) => [...block.querySelectorAll(":scope > .ol-kids > .tree-row .title")].map((t) => t.textContent);
const heading = () => $("#inspector .card-title, #inspector h2").textContent;

test("each condition says where its value comes from and same conditions share a block", async () => {
  start(library([demo(), undo(), tidy(), plain()]));
  await open("#/chain/demo");

  assert.deepEqual(labels(".ol-if"), [
    ["If", "test_command", "is not empty", "input “Test command”, project default in Settings"],
    ["If", "memory", "is empty", "built-in from Memory"],
    ["If", "branch", "is main", "var, computed at start"],
    ["If", "nope.field", "is x", "nothing sets it"],
    ["If", "mystery", "is not 1", "nothing sets it"],
    ["If", "request", "is x", "input “request”"],
    ["If", "feedback", "is empty", "built-in"],
    ["If", "plan.output", "is not empty", "set by Plan"],
  ]);
  const blocks = $$(".ol-if");
  assert.deepEqual(kids(blocks[2]), ["On main", "Also on main"]);
  assert.ok(blocks[3].querySelector(".ol-from").classList.contains("missing"));
  assert.equal(blocks[0].querySelector("a[href=\"#/settings\"]").textContent, "Settings");
  assert.equal(blocks[1].querySelector("a[href=\"#/memory\"]").textContent, "Memory");

  assert.deepEqual(labels(".ol-approved"), [["Approved", "nothing more runs"]]);
  assert.deepEqual(labels(".ol-rejected"), [["Rejected", "nothing more runs"]]);
  assert.equal($(".ol-revise"), null);
  assert.equal($(".ol-always"), null);

  const link = (at) => $$(".ol-if")[at].querySelector(".ol-from a");
  click(link(7));
  assert.equal(heading(), "Plan");
  click(link(0));
  assert.equal(heading(), "The chain");
  click(row("Plan"));
  click(link(2));
  assert.equal(heading(), "The chain");
});

test("steps that run only on a rejection sit under Rejected, the others under Then, always", async () => {
  start(library([demo(), undo(), tidy(), plain()]));
  await open("#/chain/undo");
  assert.deepEqual(labels(".ol-rejected"), [["Rejected", ""]]);
  assert.deepEqual(kids($(".ol-rejected")), ["Undo"]);
  assert.deepEqual(labels(".ol-always"), [["Then, always", "also after failure, stop or reject"]]);
  assert.deepEqual(kids($(".ol-always")), ["Tidy"]);

  await open("#/chain/tidy");
  assert.deepEqual(labels(".ol-approved"), [["Approved", "nothing more runs"]]);
  assert.deepEqual(labels(".ol-rejected"), [["Rejected", "only THEN, ALWAYS steps run"]]);
  assert.deepEqual(kids($(".ol-always")), ["Tidy"]);
});

test("a chain without an approval puts only its trailing cleanup steps under Then, always", async () => {
  start(library([demo(), undo(), tidy(), plain()]));
  await open("#/chain/plain");
  assert.deepEqual([...$("#tree > .tree-kids").children].map((el) => el.className.trim().replace(/\s+/g, " ")),
    ["tree-row", "tree-row", "ol-block ol-always"]);
  assert.deepEqual(kids($(".ol-always")), ["Tidy", "Report"]);
  assert.equal($(".ol-approved"), null);
});

test("the outline lists the steps of every block and can show each value next to its source", async () => {
  start(library([demo()]));
  await open("#/");
  const view = page(`(() => {
    const chain = ${JSON.stringify(demo())};
    const blocks = [];
    const el = h("div", {}, renderOutline(outlineOf(chain.steps), {
      chain,
      step: (step) => h("div", { class: "s" }, step.id),
      value: (name, from) => h("span", { class: "v" }, name + " from " + from.kind),
    }, blocks));
    return { el, blocks: blocks.map((b) => [b.el.className, b.steps.map((s) => s.id)]), all: stepsIn(outlineOf(chain.steps)).map((s) => s.id) };
  })()`);
  assert.deepEqual([...view.el.querySelectorAll(".v")].map((v) => v.textContent), [
    "test_command from input", "memory from builtin", "branch from var", "nope.field from missing",
    "mystery from missing", "request from input", "feedback from builtin", "plan.output from step",
  ]);
  assert.equal(view.el.querySelector(".ol-from a[href=\"#\"]"), null);
  assert.deepEqual([...view.el.querySelectorAll(".ol-from a")].map((a) => a.getAttribute("href")), ["#/settings", "#/memory"]);
  assert.deepEqual(view.blocks.map(([cls]) => cls), [...Array(8).fill("ol-block ol-if"), "ol-block ol-approved", "ol-block ol-rejected"]);
  assert.deepEqual(view.blocks[2][1], ["on_main", "also_main"]);
  assert.deepEqual(view.all, ["plan", "recall", "on_main", "also_main", "ghost", "mystery", "asked", "feedback", "planned", "gate"]);
});

test("a run is not approved when a step's condition says the decision was not approve or was reject", () => {
  const notApproved = (when) => page(`notApproved(${JSON.stringify(when)}, "gate")`);
  assert.equal(notApproved("gate.decision != approve"), true);
  assert.equal(notApproved("gate.decision == reject"), true);
  assert.equal(notApproved("gate.decision == approve"), false);
  assert.equal(notApproved("gate.decision != reject"), false);
  assert.equal(notApproved("other.decision != approve"), false);
  assert.equal(notApproved(undefined), false);
});
