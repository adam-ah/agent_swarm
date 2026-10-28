import test from "node:test";
import assert from "node:assert/strict";
import { $, $$, answers, calls, click, dialogs, fail, page, route, settle, type } from "./page.mjs";
import { body, button, control, labelled, open, row, rows, solo, start, text, library, makeChange, shipChangeset } from "./editor-fixtures.mjs";

const parts = (el) => [...el.children].map((c) => text(c));
const savedChain = (saved, name = "make-change") => body(saved.findLast((c) => c.path === `/api/items/chains/${name}.json`));
const status = () => text($("#savebar .status"));
const heading = () => $("#inspector .card-title, #inspector h2").textContent;

test("the editor shows the chain's steps as a tree with branches, notes and read-only sub-chains", async () => {
  start();
  await open("#/chain/make-change");

  assert.equal(text($(".eyebrow")), "Chain · chains/make-change.json · shared by every project");
  assert.equal($("#chain-title").textContent, "Make a change");
  assert.equal(text($(".lead")), "Change the code, test it, review it and ask before merging.");
  assert.equal(text($("#problems")), "This chain runs, but these look wrong:approve: nothing reads approve.note");
  assert.deepEqual(rows().map((r) => r.querySelector(".title").textContent),
    ["Title, inputs and values", "Read the issue", "Make the change", "Run the tests", "Review it", "Approve the change", "Merge", "Ship each changeset", "Tell the team", "Clean up"]);
  assert.deepEqual(rows().map((r) => r.querySelector(".kind").textContent), ["≡", "T", "A", "T", "A", "U", "T", "C", "T", "T"]);
  assert.deepEqual(rows().map((r) => r.querySelector(".id")?.textContent ?? ""),
    ["", "read_issue", "change", "test", "review", "approve", "merge", "ship", "tell", "cleanup"]);
  assert.ok(row("Title, inputs and values").classList.contains("selected"));
  assert.deepEqual($$("#tree .tree-row.readonly .title").map((t) => t.textContent),
    ["Apply one changeset", "Run solo", "Only step", "Back to shipping", "Apply one changeset", "Run solo", "Only step", "Back to shipping"]);
  assert.deepEqual($$(".tree-note").map(text), ["⤷ stops the run here if its output is FAIL", "⤷ runs ship-changeset once per item of change.files"]);

  assert.deepEqual(parts($(".ol-if > .ol-label")), ["If", "change.verdict", "is DONE", "set by Make the change"]);
  assert.equal(text($(".ol-revise")), "Sent back⟲ runs again from “Make the change”, with your note");
  assert.deepEqual(parts($(".ol-rejected > .ol-label")), ["Rejected", "or an earlier step failed or stopped"]);
  assert.deepEqual(parts($(".ol-always > .ol-label")), ["Then, always", "even after a failure, a stop or a rejection"]);
  assert.deepEqual($$(".ol-approved > .ol-kids > .tree-row .title").map((t) => t.textContent), ["Merge", "Ship each changeset"]);
  assert.equal(text($("#tree-actions .hint")), "Add a step at the end:");
  assert.equal(button("Save").disabled, true);
  assert.equal(button("Discard").disabled, true);
  assert.equal($("#savebar .status"), null);

  click($(".ol-if .ol-from a"));
  assert.equal(heading(), "Make the change");
  assert.ok(row("Make the change").classList.contains("selected"));
  assert.equal(text($("#tree-actions .hint")), "Add a step after the selected one:");

  click(row("Title, inputs and values"));
  assert.equal(heading(), "The chain");
  click($(".ol-revise a"));
  assert.equal(heading(), "Make the change");
});

test("a link to a step opens the editor with that step selected, and an unknown chain says so", async () => {
  start();
  await open("#/chain/make-change/review");
  assert.ok(row("Review it").classList.contains("selected"));
  assert.equal(heading(), "Review it");

  await open("#/chain/nope");
  assert.equal(text($("#main .empty")), "There is no chain named nope in this project.");
});

test("adding a step of each kind puts it after the selected step with a unique id", async () => {
  const { saved } = start();
  await open("#/chain/make-change");

  click(button("+ Tool"));
  assert.equal(heading(), "New tool step");
  assert.ok(row("New tool step").classList.contains("selected"));
  assert.deepEqual($$(".ol-approved > .ol-kids > .tree-row .title").map((t) => t.textContent), ["Merge", "Ship each changeset", "New tool step"]);
  assert.equal(control("Tool").value, "cleanup");
  assert.equal(status(), "Unsaved. Saving changes it for every project.");
  assert.equal(text($("#problems")), "");
  assert.equal(button("Save").disabled, false);

  click(row("Read the issue"));
  click(button("+ Agent"));
  assert.equal(heading(), "New agent step");
  click(button("+ User"));
  assert.equal(heading(), "Your approval");
  click(button("+ Sub-chain"));
  assert.equal(heading(), "Sub-chain");
  click(button("+ Tool"));
  assert.equal(control("ID").value, "new_tool_step_2");

  click(button("Save"));
  await settle();
  const chain = savedChain(saved);
  assert.deepEqual(chain.steps.map((s) => s.id),
    ["read_issue", "new_agent_step", "your_approval", "sub_chain", "new_tool_step_2", "change", "test", "review", "approve", "merge", "ship", "tell", "cleanup", "new_tool_step"]);
  assert.deepEqual(chain.steps[1], { title: "New agent step", agent: "change", tools: ["Read", "Grep", "Glob"], id: "new_agent_step" });
  assert.deepEqual(chain.steps[2], { title: "Your approval", user: "Continue?", id: "your_approval" });
  assert.deepEqual(chain.steps[3], { title: "Sub-chain", chain: "ship-changeset", id: "sub_chain" });
  assert.deepEqual(chain.steps.at(-1), { title: "New tool step", tool: "cleanup", id: "new_tool_step" });
  assert.deepEqual(chain.steps.find((s) => s.id === "merge"), { id: "merge", title: "Merge", tool: "merge" });
  for (const key of ["name", "problems", "warnings", "source", "prompts"]) assert.equal(key in chain, false, key);
  assert.equal(status(), "Saved");
  assert.equal(button("Save").disabled, true);
  assert.equal(heading(), "New tool step");
});

test("a new step takes its id from its title until the id is edited by hand", async () => {
  const { saved } = start();
  await open("#/chain/make-change");

  click(button("+ Tool"));
  type(control("Title"), "2nd pass!");
  assert.equal(control("ID").value, "step_2nd_pass");
  assert.equal(heading(), "2nd pass!");
  assert.equal(text(labelled("ID").querySelector(".hint")), "later steps read its fields as {{step_2nd_pass.output}}");
  type(control("Title"), "Merge");
  assert.equal(control("ID").value, "merge_2");
  type(control("Title"), "");
  assert.equal(heading(), "(untitled)");
  assert.equal(control("ID").value, "step");

  type(control("ID"), "lint");
  type(control("Title"), "Lint the code");
  assert.equal(control("ID").value, "lint");
  assert.equal(row("Lint the code").querySelector(".id").textContent, "lint");

  click(button("Save"));
  await settle();
  assert.deepEqual(savedChain(saved).steps.at(-1), { title: "Lint the code", tool: "cleanup", id: "lint" });
});

test("steps move up and down and are removed after a confirmation", async () => {
  const { saved } = start();
  await open("#/chain/make-change/test");

  click(button("↑"));
  assert.equal(heading(), "Run the tests");
  click(row("Read the issue"));
  assert.equal(button("↑").disabled, true);
  click(button("↓"));
  click(row("Clean up"));
  assert.equal(button("↓").disabled, true);

  answers.confirm = false;
  click(button("Delete"));
  assert.deepEqual(dialogs.confirm, ["Delete the step \"Clean up\"?"]);
  assert.ok(row("Clean up"));
  answers.confirm = true;
  click(button("Delete"));
  assert.equal(row("Clean up"), undefined);
  assert.equal(heading(), "Tell the team");

  click(row("Make the change"));
  click(button("Delete"));
  assert.equal(heading(), "Review it");

  click(button("Save"));
  await settle();
  assert.deepEqual(savedChain(saved).steps.map((s) => s.id), ["test", "read_issue", "review", "approve", "merge", "ship", "tell"]);
});

test("removing every step of a chain selects the chain itself", async () => {
  start();
  await open("#/chain/solo/back");
  click(button("Delete"));
  assert.equal(heading(), "Only step");
  click(button("Delete"));
  assert.equal(heading(), "The chain");
  assert.equal(rows().length, 1);
  assert.equal(status(), "Unsaved changes");
});

test("the chain's own settings edit its title, inputs, values, heading and link", async () => {
  const { saved } = start();
  await open("#/chain/make-change");

  type(control("Title"), "Make a careful change");
  assert.equal($("#chain-title").textContent, "Make a careful change");
  type(control("Description"), "Slowly.");

  const inputs = () => [...labelled("Inputs").querySelectorAll(".kv-row")];
  assert.deepEqual(inputs()[1].querySelectorAll("input")[0].value, "test_command");
  assert.ok(inputs()[1].querySelector(".check").classList.contains("on"));
  click(button("+ add", labelled("Inputs")));
  const fresh = inputs()[2].querySelectorAll("input");
  type(fresh[0], "ticket");
  type(fresh[1], "Which ticket?");
  click(fresh[3]);
  assert.ok(inputs()[2].querySelector(".check").classList.contains("on"));
  click(inputs()[0].querySelector(".icon-btn"));
  assert.equal(inputs().length, 2);

  const values = labelled("Values");
  type(values.querySelectorAll(".kv-row")[0].querySelectorAll("input")[1], "swarm/{{request}}");
  click(button("×", values.querySelectorAll(".kv-row")[1]));
  type(control("Run heading"), "Change: {{request}}");
  type(control("Run link"), "");
  assert.equal(control("Run link").placeholder, "e.g. {{read_issue.url}}");

  click(button("Save"));
  await settle();
  const chain = savedChain(saved);
  assert.equal(chain.title, "Make a careful change");
  assert.equal(chain.description, "Slowly.");
  assert.deepEqual(chain.inputs, [
    { name: "test_command", label: "Test command", default: "./test.sh", optional: true },
    { name: "ticket", label: "Which ticket?", default: "", optional: true },
  ]);
  assert.deepEqual(chain.vars, [{ name: "branch", value: "swarm/{{request}}" }]);
  assert.equal(chain.subject, "Change: {{request}}");
  assert.equal(chain.link, "");
  assert.equal($("#chain-title").textContent, "Make a careful change");
});

test("saving writes the edited tools and prompts before the chain", async () => {
  const { saved } = start();
  await open("#/chain/make-change/test");

  type(control("What it does"), "Run the full suite");
  type(control("Command"), "{{test_command}} --all");
  click(row("Make the change"));
  type($("#inspector textarea.prompt"), "Change {{request}}.\n\n## Return\n\n- `summary`: what changed\n");

  click(button("Save"));
  await settle();
  assert.deepEqual(saved.map((c) => c.path), ["/api/items/tools/run_tests.json", "/api/items/chains/make-change/change.md", "/api/items/chains/make-change.json"]);
  assert.deepEqual(body(saved[0]), { title: "Run the full suite", run: "{{test_command}} --all" });
  assert.equal(saved[1].body, "Change {{request}}.\n\n## Return\n\n- `summary`: what changed\n");
  assert.equal(saved[1].query.scope, undefined);
  assert.equal(status(), "Saved");
  assert.equal(heading(), "Make the change");
  assert.ok(calls.findLast((c) => c.path === "/api/library"));
});

test("a refused save lists swarm's reasons and keeps the changes", async () => {
  start();
  route("PUT", "/api/items/chains/make-change.json", fail(422, "refused", ["merge: there is no tool named merge2", "ship: the chain ship-changeset needs changeset"]));
  await open("#/chain/make-change");

  type(control("Title"), "Broken");
  click(button("Save"));
  await settle();
  assert.equal(text($("#problems strong")), "Not saved. swarm refused it because:");
  assert.deepEqual($$("#problems li").map(text), ["merge: there is no tool named merge2", "ship: the chain ship-changeset needs changeset"]);
  assert.equal(status(), "Unsaved. Saving changes it for every project.");
  assert.equal(button("Save").disabled, false);
  assert.equal($("#chain-title").textContent, "Broken");

  route("PUT", "/api/items/chains/make-change.json", fail(500, "the disk is full"));
  click(button("Save"));
  await settle();
  assert.deepEqual(dialogs.alert, ["the disk is full"]);
});

test("a shared chain can be saved as a copy for this project only", async () => {
  const { saved, lib } = start();
  await open("#/chain/make-change/change");

  type($("#inspector textarea.prompt"), "Change {{request}} carefully.");
  click(button("Save as a copy for this project only"));
  await settle();
  assert.deepEqual(saved.map((c) => [c.path, c.query.scope]),
    [["/api/items/chains/make-change/change.md", "project"], ["/api/items/chains/make-change.json", "project"]]);
  assert.equal(status(), "Saved as this project's copy");
  assert.equal(lib.chains[0].source, "project");
  assert.equal(text($(".eyebrow")), "Chain · chains/make-change.json · only in this project");
  assert.equal(button("Save as a copy for this project only"), undefined);
  assert.ok(button("Delete this project's copy"));
  assert.equal($("#chains a[href=\"#/chain/make-change\"] .tag").textContent, "project");
});

test("discarding reloads the saved chain and keeps the selected step", async () => {
  start();
  await open("#/chain/make-change/review");

  type(control("Title"), "Review it twice");
  assert.equal(row("Review it twice").querySelector(".id").textContent, "review");
  click(button("Discard"));
  await settle();
  assert.equal(heading(), "Review it");
  assert.equal(button("Discard").disabled, true);
  assert.equal(text($("#problems")), "This chain runs, but these look wrong:approve: nothing reads approve.note");
});

test("a project chain with problems shows them and deletes only this project's copy", async () => {
  start();
  const deleted = [];
  await open("#/chain/solo");

  assert.equal(text($("#problems strong")), "This chain cannot run:");
  assert.deepEqual($$("#problems li").map(text), ["only: the tool read_issue needs a value for issue"]);

  answers.confirm = false;
  click(button("Delete this project's copy"));
  assert.deepEqual(dialogs.confirm, ["Delete the chain \"Solo\"? This removes this project's copy."]);
  assert.equal(calls.some((c) => c.method === "DELETE"), false);

  answers.confirm = true;
  route("DELETE", "/api/items/chains/solo.json", fail(422, "refused", ["chains/solo.json is used by ship-changeset"]));
  click(button("Delete this project's copy"));
  await settle();
  assert.deepEqual(dialogs.alert, ["Refused:\n- chains/solo.json is used by ship-changeset"]);
  assert.equal(globalThis.location.hash, "#/chain/solo");

  route("DELETE", "/api/items/chains/solo.json", (call) => {
    deleted.push(call);
    return { ok: "true" };
  });
  click(button("Delete this project's copy"));
  await settle(30);
  assert.equal(deleted.length, 1);
  assert.equal(globalThis.location.hash, "#/");
});

test("deleting a shared chain warns that every project loses it", async () => {
  start();
  route("DELETE", "/api/items/chains/make-change.json", { ok: "true" });
  await open("#/chain/make-change");
  click(button("Delete this chain for every project"));
  await settle(30);
  assert.deepEqual(dialogs.confirm, ["Delete the chain \"Make a change\"? This removes the shared chain, for every project."]);
  assert.equal(globalThis.location.hash, "#/");
});

test("a project chain offers no project copy and calls its edits plainly unsaved", async () => {
  start(library([makeChange(), shipChangeset(), solo()]));
  await open("#/chain/ship-changeset");
  assert.equal(text($(".eyebrow")), "Chain · chains/ship-changeset.json · only in this project");
  assert.equal($(".lead"), null);
  assert.equal(button("Save as a copy for this project only"), undefined);
  type(control("Title"), "Ship it");
  assert.equal(status(), "Unsaved changes");
});

test("a chain is copied or moved to another project with what it uses", async () => {
  start();
  await open("#/chain/make-change");
  assert.equal(text($(".more-menu .hint")), "Add another project to copy or move this chain to it.");

  route("GET", "/api/projects", { projects: ["/work/demo", "/work/other"], default: "/work/demo" });
  await page("loadProjects()");
  await open("#/chain/make-change");
  const target = $(".more-transfer select");
  assert.deepEqual([...target.options].map((o) => [o.value, o.textContent]), [["/work/other", "other"]]);

  const posted = [];
  let reply = { copied: ["chains/make-change.json", "chains/make-change/change.md"], same: ["tools/merge.json"], removed: [], kept: [] };
  route("POST", "/api/chains/make-change/transfer", (call) => {
    posted.push(body(call));
    return reply;
  });
  click(button("Copy to project"));
  await settle();
  assert.deepEqual(posted, [{ to: "/work/other", move: false }]);
  assert.equal(status(), "Copied to other: chains/make-change.json, chains/make-change/change.md. Already the same in other: tools/merge.json");

  type(control("Title"), "Changed");
  click(button("Move to project"));
  await settle();
  assert.deepEqual(dialogs.alert, ["Save or discard your changes first."]);
  assert.equal(posted.length, 1);

  await open("#/chain/make-change");
  answers.confirm = false;
  click(button("Move to project"));
  await settle();
  assert.deepEqual(dialogs.confirm, ["Move \"Make a change\" to other? It leaves the shared library, so every other project loses it too."]);
  assert.equal(posted.length, 1);

  answers.confirm = true;
  route("POST", "/api/chains/make-change/transfer", fail(422, "refused", ["chains/make-change.json is used by ship here, so it cannot move"]));
  click(button("Move to project"));
  await settle();
  assert.equal(dialogs.alert.at(-1), "Refused:\n- chains/make-change.json is used by ship here, so it cannot move");

  reply = { copied: ["chains/make-change.json"], same: [], removed: ["chains/make-change.json"], kept: ["tools/merge.json"] };
  route("POST", "/api/chains/make-change/transfer", (call) => {
    posted.push(body(call));
    return reply;
  });
  click(button("Move to project"));
  await settle(30);
  assert.deepEqual(posted.at(-1), { to: "/work/other", move: true });
  assert.equal(page("project"), "/work/other");
  assert.equal(calls.findLast((c) => c.path === "/api/library").query.project, "/work/other");
  assert.equal(status(), "Copied to other: chains/make-change.json. Removed from here: chains/make-change.json. Left here because other chains use them: tools/merge.json");
});
