import test from "node:test";
import assert from "node:assert/strict";
import { $, $$, answers, calls, click, dialogs, fail, route, settle, type } from "./page.mjs";
import { body, button, control, labelled, library, makeChange, open, options, shipChangeset, shown, solo, start, text } from "./editor-fixtures.mjs";

const status = () => $(".savebar > span");
const more = (label) => button(label, $(".savebar .more-menu"));
const heads = () => $$(".panel-head").map((head) => head.firstChild.textContent);

test("a tool page shows where the tool is used and saves its definition", async () => {
  const { saved } = start();
  await open("#/tool/merge");

  assert.equal(text($(".eyebrow")), "Tool · tools/merge.json · shared, all projects");
  assert.equal($("h1").textContent, "merge");
  const uses = $$(".used-by .use").map((u) => [u.querySelector(".use-chain").textContent, [...u.querySelectorAll(".use-steps a")].map((a) => a.getAttribute("href"))]);
  assert.deepEqual(uses, [["Make a change", ["#/chain/make-change/merge"]], ["Ship a changeset", ["#/chain/ship-changeset/apply"]]]);
  assert.equal(control("What it does").value, "Merge the branch");
  assert.equal(control("Command").value, "git merge {{branch}} && echo {{run_folder}}");
  assert.ok(more("Save as project copy"));
  assert.ok(more("Delete (all projects)"));

  type(control("What it does"), "Merge into main");
  assert.equal(status().className, "status");
  assert.equal(status().textContent, "Unsaved");
  type(control("Command"), "git merge {{branch}}");
  click(button("Save"));
  await settle();
  assert.deepEqual(saved.map((c) => [c.path, c.query.scope]), [["/api/items/tools/merge.json", undefined]]);
  assert.deepEqual(body(saved[0]), { title: "Merge into main", run: "git merge {{branch}}" });
  assert.equal(status().className, "ok-text");
  assert.equal(status().textContent, "Saved");
  assert.ok(calls.findLast((c) => c.path === "/api/library"));

  click(more("Save as project copy"));
  await settle(30);
  assert.deepEqual(saved.map((c) => c.query.scope), [undefined, "project"]);
  assert.equal(text($(".eyebrow")), "Tool · tools/merge.json · this project only");
  assert.equal(more("Save as project copy"), undefined);
  assert.ok(more("Delete project copy"));
  assert.equal($("#tools a[href=\"#/tool/merge\"] .tag").textContent, "project");
});

test("a tool that is not used anywhere says so and a refused save shows why", async () => {
  start();
  route("PUT", "/api/items/tools/lint.json", fail(422, "refused", ["run: the command is empty"]));
  await open("#/tool/lint");

  assert.equal($("h1").textContent, "lint");
  assert.equal(text($(".used-by")), "Unused");
  assert.equal(control("What it does").value, "lint");
  assert.equal(control("Command").value, "");
  click(button("Save"));
  await settle();
  assert.equal(status().className, "error-text");
  assert.equal(status().textContent, "Refused:\n- run: the command is empty");
});

test("a saved tool with a syntax error says it cannot run and saves without its problems", async () => {
  const { lib, saved } = start();
  lib.tools.merge.problems = ["line 2: python3: invalid syntax"];
  await open("#/tool/merge");

  assert.equal(status().className, "status error-text");
  assert.equal(status().textContent, "Cannot run: line 2: python3: invalid syntax");
  click(button("Save"));
  await settle();
  assert.deepEqual(body(saved[0]), { title: "Merge the branch", run: "git merge {{branch}} && echo {{run_folder}}" });
});

test("deleting a tool asks first, shows a refusal, and goes home once done", async () => {
  start();
  await open("#/tool/run_tests");
  assert.ok(more("Delete project copy"));

  answers.confirm = false;
  click(more("Delete project copy"));
  await settle();
  assert.deepEqual(dialogs.confirm, ["Delete run_tests? Removes the project copy."]);
  assert.equal(calls.some((c) => c.method === "DELETE"), false);

  answers.confirm = true;
  route("DELETE", "/api/items/tools/run_tests.json", fail(422, "refused", ["make-change uses it"]));
  click(more("Delete project copy"));
  await settle();
  assert.equal(status().className, "error-text");
  assert.equal(status().textContent, "Refused:\n- make-change uses it");

  route("DELETE", "/api/items/tools/run_tests.json", { ok: "true" });
  click(more("Delete project copy"));
  await settle(30);
  assert.ok(calls.some((c) => c.method === "DELETE" && c.path === "/api/items/tools/run_tests.json"));
  assert.equal(globalThis.location.hash, "#/");
});

test("deleting a shared tool warns that every project loses it", async () => {
  start();
  route("DELETE", "/api/items/tools/merge.json", { ok: "true" });
  await open("#/tool/merge");
  click(more("Delete (all projects)"));
  await settle(30);
  assert.deepEqual(dialogs.confirm, ["Delete merge? Removes the shared copy, for all projects."]);
  assert.equal(globalThis.location.hash, "#/");
});

test("a prompt page lists its users, its returned fields and its placeholders, and saves its text", async () => {
  const { saved } = start();
  await open("#/prompt/make-change/change");

  assert.equal(text($(".eyebrow")), "Prompt of Make a change · chains/make-change/change.md · shared, all projects");
  assert.equal($(".eyebrow a").getAttribute("href"), "#/chain/make-change");
  assert.equal($("h1").textContent, "change");
  assert.deepEqual(heads(), ["Used by", "Returns", "Reads", "## Return syntax", "Text"]);
  const facts = $$(".panel-body.used-by");
  assert.equal(text(facts[0]), "Make a changeMake the change⚠ approve: nothing reads approve.note");
  assert.deepEqual([...facts[1].children].map(text), ["summary", "files list", "verdict DONE | STUCK"]);
  assert.deepEqual([...facts[2].children].map(text), ["{{request}}", "{{memory}}"]);
  assert.match($("pre.example").textContent, /^## Return\n\n- `verdict`/);
  assert.equal(text($(".more-menu .hint")), "For a project-only change, save a project copy of the chain; its prompts are copied with it.");
  assert.equal(more("Save as project copy"), undefined);
  assert.equal($("textarea.prompt").value, makeChange().prompts.change.text);

  type($("textarea.prompt"), "Fix {{request}} in {{project}} quickly.");
  assert.equal(status().textContent, "Unsaved");
  click(button("Save"));
  await settle();
  assert.deepEqual(saved.map((c) => c.path), ["/api/items/chains/make-change/change.md"]);
  assert.equal(saved[0].body, "Fix {{request}} in {{project}} quickly.");
  assert.equal(status().textContent, "Saved");
  const after = $$(".panel-body.used-by");
  assert.equal(text(after[1]), "No ## Return: free text in {{step_id.output}}");
  assert.deepEqual([...after[2].children].map(text), ["{{request}}", "{{project}}"]);
});

test("a prompt with a broken return section lists its problems, and one without placeholders says none", async () => {
  const chain = makeChange();
  chain.prompts["review-change"] = {
    text: "Review it.\n\n## Return\n\n- verdict: APPROVE or CHANGES\n- `notes`: what to fix\n",
    source: "shared",
    returns: [{ name: "notes", value: "text" }],
    problems: ["## Return: cannot read \"- verdict: APPROVE or CHANGES\"; write each field as - `name`: what it is, with a lowercase name"],
  };
  start(library([chain, shipChangeset(), solo()]));
  route("DELETE", "/api/items/chains/make-change/review-change.md", { ok: "true" });
  await open("#/prompt/make-change/review-change");

  const facts = $$(".panel-body.used-by");
  assert.deepEqual([...facts[1].children].map((el) => [el.className, text(el)]), [
    ["error-text", "## Return: cannot read \"- verdict: APPROVE or CHANGES\"; write each field as - `name`: what it is, with a lowercase name"],
    ["", "notes"],
  ]);
  assert.equal(text(facts[2]), "none");

  click(more("Delete prompt"));
  await settle(30);
  assert.deepEqual(dialogs.confirm, ["Delete make-change/review-change? Removes the shared copy, for all projects."]);
  assert.ok(calls.some((c) => c.method === "DELETE" && c.path === "/api/items/chains/make-change/review-change.md"));
  assert.equal(globalThis.location.hash, "#/");
});

test("a prompt of an unknown chain starts empty", async () => {
  start();
  await open("#/prompt/gone/plan");
  assert.equal(text($(".eyebrow a")), "gone");
  assert.equal($("textarea.prompt").value, "");
  assert.equal(text($$(".panel-body.used-by")[0]), "Unused");
});

function settingsView() {
  return {
    effective: { agent: "codex", agents: {}, inputs: { test_command: "./test.sh", lint: "make lint" } },
    default: "codex",
    runners: ["claude", "codex", "sonnet", "pi"],
    installed: { claude: "/usr/local/bin/claude" },
    shared: { agent: "codex", agents: { codex: { args: ["--model", "gpt-5"] } }, inputs: { test_command: "make test", lint: "make lint" } },
    project: {
      agents: { claude: { args: ["--model", "opus"] }, sonnet: { runs: "claude", args: ["--model", "sonnet"] }, pi: { command: "pi -p" } },
      inputs: { test_command: "./test.sh" },
    },
  };
}

function serveSettings(view = settingsView()) {
  const lib = library();
  lib.runners = ["claude", "codex", "sonnet", "pi"];
  const { saved } = start(lib);
  const puts = [];
  route("GET", "/api/settings", () => view);
  route("PUT", "/api/items/settings.json", (call) => {
    puts.push(call);
    view[call.query.scope] = JSON.parse(call.body);
    return { ok: "true" };
  });
  return { view, puts, saved };
}

const subpanel = (title) => $$(".subpanel").find((s) => s.querySelector("h3").textContent === title);
const rowsOf = (title) => [...subpanel(title).querySelectorAll(".kv-row")];
const rowValues = (title) => rowsOf(title).map((r) => [...r.querySelectorAll("input")].map((i) => i.value));

test("the settings page shows this project's agents and starting values over the shared ones", async () => {
  serveSettings();
  await open("#/settings");

  assert.equal($("h1").textContent, "Agents and input defaults");
  assert.deepEqual($$(".segmented button").map((b) => [b.textContent, b.className, b.title]),
    [["Project", "on", ".swarm/settings.json"], ["Shared", "", "~/.swarm/settings.json"]]);
  assert.equal(text($(".panel-body > .hint")), "Project settings (.swarm/settings.json); override the shared settings key by key.");
  const agent = control("Default agent");
  assert.deepEqual(options(agent), [["", "inherited: codex (shared settings)"], ["claude", "claude"], ["codex", "codex"], ["sonnet", "sonnet"], ["pi", "pi"]]);
  assert.equal(shown(agent), "");
  assert.equal(text(labelled("Default agent").querySelector(".hint")), "steps can override it with \"Runs with\"; current: codex");
  assert.deepEqual($$(".subpanel .used-by span").map(text), ["✓ claude: /usr/local/bin/claude", "✗ codex: not on PATH"]);
  assert.equal(control("claude args").value, "--model opus");
  assert.equal(control("codex args").value, "");
  assert.equal(control("codex args").placeholder, "--model gpt-5");
  assert.deepEqual(rowValues("Custom agents"), [["sonnet", "claude", "--model sonnet"], ["pi", "", "pi -p"]]);
  assert.deepEqual(rowValues("Input defaults"), [["test_command", "./test.sh"]]);
  const inputs = subpanel("Input defaults");
  assert.equal(text(inputs.querySelector(":scope > .hint")),
    "Overrides the chain's default for any input with this name, in this project. Chain inputs: changeset, dry_run, request, target, test_command.");
  assert.equal(text(inputs.querySelector(":scope > .hint:last-child")), "Inherited from shared settings: lint = make lint");
});

test("saving the project's settings writes only what this project sets", async () => {
  const { puts } = serveSettings();
  await open("#/settings");

  type(control("Default agent"), "sonnet");
  assert.equal(status().textContent, "Unsaved");
  type(control("claude args"), " ");
  type(control("codex args"), "--model  o3");
  click(button("+ add", subpanel("Custom agents")));
  const fresh = rowsOf("Custom agents")[2].querySelectorAll("input");
  type(fresh[0], "haiku");
  type(fresh[1], "claude");
  type(fresh[2], "--model haiku");
  click(rowsOf("Custom agents")[1].querySelector(".icon-btn"));
  click(button("+ add", subpanel("Input defaults")));
  const added = rowsOf("Input defaults")[1].querySelectorAll("input");
  type(added[0], "lint");
  type(added[1], "npm run lint");

  click(button("Save"));
  await settle(30);
  assert.equal(puts.length, 1);
  assert.equal(puts[0].query.scope, "project");
  assert.deepEqual(body(puts[0]), {
    agent: "sonnet",
    agents: { codex: { args: ["--model", "o3"] }, sonnet: { runs: "claude", args: ["--model", "sonnet"] }, haiku: { runs: "claude", args: ["--model", "haiku"] } },
    inputs: { test_command: "./test.sh", lint: "npm run lint" },
  });
  assert.equal(calls.filter((c) => c.path === "/api/settings").length, 2);
  assert.equal(status().className, "ok-text");
  assert.equal(status().textContent, "Saved");
  assert.deepEqual(rowValues("Custom agents"), [["sonnet", "claude", "--model sonnet"], ["haiku", "claude", "--model haiku"]]);
  assert.equal(control("claude args").value, "");
});

test("switching to every project's settings asks before dropping changes, and empty sections are left out", async () => {
  const { puts } = serveSettings();
  await open("#/settings");
  const every = () => $$(".segmented button")[1];

  click($$(".segmented button")[0]);
  assert.deepEqual(dialogs.confirm, []);
  type(control("claude args"), "--model sonnet");
  answers.confirm = false;
  click(every());
  assert.deepEqual(dialogs.confirm, ["Discard unsaved changes?"]);
  assert.equal($$(".segmented button")[0].className, "on");

  answers.confirm = true;
  click(every());
  assert.equal(every().className, "on");
  assert.equal(text($(".panel-body > .hint")), "Shared settings (settings.json in the swarm home); a project's settings override them.");
  const agent = control("Default agent");
  assert.equal(options(agent)[0][1], "unset: claude");
  assert.equal(shown(agent), "codex");
  assert.equal(control("codex args").value, "--model gpt-5");
  assert.equal(control("claude args").placeholder, "e.g. --model opus");
  assert.equal(status().textContent, "");
  assert.match(text(subpanel("Input defaults").querySelector(".hint")), /with this name, in every project\./);
  assert.deepEqual(rowValues("Input defaults"), [["test_command", "make test"], ["lint", "make lint"]]);

  type(control("codex args"), "");
  click(rowsOf("Input defaults")[0].querySelector(".icon-btn"));
  click(rowsOf("Input defaults")[0].querySelector(".icon-btn"));
  assert.deepEqual(rowValues("Input defaults"), []);
  click(button("Save"));
  await settle(30);
  assert.equal(puts[0].query.scope, "shared");
  assert.deepEqual(body(puts[0]), { agent: "codex" });

  click($$(".segmented button")[0]);
  assert.equal($$(".segmented button")[0].className, "on");
});

test("a project without its own settings opens on every project's settings, and a failed save shows why", async () => {
  const view = settingsView();
  view.project = null;
  serveSettings(view);
  route("PUT", "/api/items/settings.json", fail(422, "refused", ["agents: pi needs runs or command"]));
  await open("#/settings");

  assert.equal($$(".segmented button")[1].className, "on");
  click($$(".segmented button")[0]);
  assert.equal(options(control("Default agent"))[0][1], "inherited: codex (shared settings)");
  assert.deepEqual(rowValues("Custom agents"), []);
  click(button("Save"));
  await settle();
  assert.equal(status().className, "error-text");
  assert.equal(status().textContent, "Refused:\n- agents: pi needs runs or command");
});

test("the memory page edits what the project has learned", async () => {
  start();
  const puts = [];
  route("GET", "/api/memory", { text: "- Run make test before pushing.\n", path: "/work/demo/.swarm/memory.md" });
  route("PUT", "/api/memory", (call) => {
    puts.push(body(call));
    return { ok: "true" };
  });
  await open("#/memory");

  assert.equal(text($(".eyebrow")), "Memory · /work/demo/.swarm/memory.md");
  assert.ok($("#memory-link").classList.contains("active"));
  const area = $("textarea.prompt");
  assert.equal(area.value, "- Run make test before pushing.\n");
  type(area, "- Run make test before pushing.\n- The API lives in src/server.go.\n");
  assert.equal(status().textContent, "Unsaved");
  click(button("Save"));
  await settle();
  assert.deepEqual(puts, [{ text: "- Run make test before pushing.\n- The API lives in src/server.go.\n" }]);
  assert.equal(status().className, "ok-text");
  assert.equal(status().textContent, "Saved");

  route("PUT", "/api/memory", fail(500, "cannot write memory.md"));
  click(button("Save"));
  await settle();
  assert.deepEqual(dialogs.alert, ["cannot write memory.md"]);
});
