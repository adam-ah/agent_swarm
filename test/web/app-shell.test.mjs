import test from "node:test";
import assert from "node:assert/strict";
import { $, $$, answers, calls, click, dialogs, fail, go, page, reset, route, settle, sources, type } from "./page.mjs";
import { at, library } from "./run-fixtures.mjs";

const doc = globalThis.document;
const hash = () => globalThis.location.hash;
const texts = (selector) => $$(selector).map((e) => e.textContent);
const before = (seconds) => new Date(Date.now() - seconds * 1000).toISOString();
const change = (el) => el.dispatchEvent(new globalThis.Event("change", { bubbles: true }));

test("h builds elements from attributes, listeners and nested children", () => {
  const h = page("h");
  let clicks = 0;
  const el = h("span", { class: "chip", title: null, hidden: false, gone: undefined, open: true, "data-n": 3, onclick: () => clicks++ },
    "Go ", [h("b", {}, "now"), [null, undefined, false, 0]]);
  assert.equal(el.className, "chip");
  assert.equal(el.hasAttribute("title"), false);
  assert.equal(el.hasAttribute("hidden"), false);
  assert.equal(el.hasAttribute("gone"), false);
  assert.equal(el.getAttribute("open"), "");
  assert.equal(el.getAttribute("data-n"), "3");
  assert.equal(el.textContent, "Go now0");
  click(el);
  assert.equal(clicks, 1);
  assert.equal(h("input", { value: "typed" }).value, "typed");
});

test("api adds the project and reports refusals with their problems", async () => {
  reset();
  const api = page("api");
  route("GET", "/api/thing", { ok: 1 });
  route("GET", `/api/runs/1028-101500/call`, { command: [] });
  assert.deepEqual(await api("/api/thing"), { ok: 1 });
  assert.deepEqual(calls.at(-1).query, { project: "/work/demo" });
  await api("/api/thing?x=1");
  assert.deepEqual(calls.at(-1).query, { x: "1", project: "/work/demo" });
  await api("/api/projects");
  assert.deepEqual(calls.at(-1).query, {});
  await api("/api/runs/1028-101500/call?log=plan");
  assert.deepEqual(calls.at(-1).query, { log: "plan" });

  route("POST", "/api/thing", () => fail(400, "invalid chain", ["no title", "no steps"]));
  await assert.rejects(page("send")("POST", "/api/thing", { a: 1 }), { message: "Refused:\n- no title\n- no steps", problems: ["no title", "no steps"] });
  assert.equal(calls.at(-1).body, "{\"a\":1}");
  route("DELETE", "/api/thing", () => fail(500, "disk full"));
  await assert.rejects(api("/api/thing", { method: "DELETE" }), { message: "disk full", problems: [] });
  route("GET", "/api/thing", () => new Response("<html>", { status: 502, statusText: "Bad Gateway" }));
  await assert.rejects(api("/api/thing"), { message: "Bad Gateway" });
});

test("items are saved and deleted at their library paths", async () => {
  reset();
  route("PUT", /^\/api\/items\//, { ok: "true" });
  route("DELETE", /^\/api\/items\//, { ok: "true" });
  await page("putItem")("prompts", "fix/plan", "Plan it", "project");
  assert.deepEqual(calls.at(-1), { method: "PUT", path: "/api/items/chains/fix/plan.md", query: { scope: "project", project: "/work/demo" }, body: "Plan it" });
  await page("putItem")("tools", "test", { title: "Run tests" });
  assert.equal(calls.at(-1).path, "/api/items/tools/test.json");
  assert.equal(calls.at(-1).body, "{\n  \"title\": \"Run tests\"\n}");
  await page("deleteItem")("chains", "fix");
  assert.deepEqual([calls.at(-1).method, calls.at(-1).path], ["DELETE", "/api/items/chains/fix.json"]);
});

test("small helpers format kinds, times, money, paths and values", async () => {
  reset();
  const kindOf = page("kindOf");
  assert.deepEqual([{ agent: "a" }, { user: "Ok?" }, { user: "" }, { chain: "c" }, { tool: "t" }].map(kindOf), ["agent", "user", "tool", "chain", "tool"]);

  const duration = page("duration");
  assert.equal(duration(""), "");
  assert.equal(duration(at(15), at(15, 30)), "30s");
  assert.equal(duration(at(15), at(17, 5)), "2m 05s");
  assert.equal(duration("2026-10-28T10:00:00Z", "2026-10-28T13:07:00Z"), "3h 07m");
  assert.equal(duration(at(15, 30), at(15)), "0s");

  const seconds = page("seconds");
  assert.equal(seconds(""), "");
  assert.equal(seconds(at(15), at(15, 0, 50)), "0.1s");
  assert.equal(seconds(at(15), at(15, 1, 140)), "1.1s");
  assert.equal(seconds(at(15), at(17, 5)), "125.0s");
  assert.equal(seconds(at(15, 30), at(15)), "0.0s");

  const ago = page("ago");
  assert.equal(ago(before(5)), "just now");
  assert.equal(ago(before(150)), "2m ago");
  assert.equal(ago(before(7300)), "2h ago");
  assert.equal(ago("2020-01-02T12:00:00Z"), new Date("2020-01-02T12:00:00Z").toLocaleDateString());

  assert.equal(page("money")(0), "");
  assert.equal(page("money")(1.5), "$1.50");
  assert.equal(page("fileURL")("a b", "/x y.png"), "/api/runs/a%20b/file?path=%2Fx%20y.png");
  assert.equal(page("fileURL")("r", "/a.md", "doc"), "/api/runs/r/doc?path=%2Fa.md");
  const original = { a: [1, { b: 2 }] };
  const copy = page("clone")(original);
  copy.a[1].b = 3;
  assert.equal(original.a[1].b, 2);
  assert.equal(page("asText")(null), "");
  assert.equal(page("asText")(["a", { b: 1 }, 2]), "a, {\"b\":1}, 2");
  assert.equal(page("folderName")("/work/demo/"), "demo");
  assert.equal(page("folderName")("/"), "/");

  assert.deepEqual(page("placeholdersIn")("{{ a }} {{b.c}} {{a}} {{Bad}}"), ["a", "b.c"]);
  assert.deepEqual(page("placeholdersIn")(undefined), []);
  assert.equal(page("returnsText")({ returns: [{ name: "summary", value: "text" }, { name: "files", value: "list" }] }), "summary, files (list)");
  assert.equal(page("returnsText")(undefined), "");
  assert.deepEqual(page("mapToRows")({ a: "1" }), [{ key: "a", value: "1" }]);
  assert.deepEqual(page("mapToRows")(undefined), []);
  assert.deepEqual(page("rowsToMap")([{ key: "a", value: "1" }, { key: "", value: "x" }]), { a: "1" });

  route("GET", "/api/library", library);
  await page("loadLibrary()");
  const passed = page("listsPassed")({ with: { a: "{{plan.files}}", b: "x {{plan.files}}", c: "{{plan.summary}}", d: "{{test.output}}", e: "{{nowhere}}" } }, library.chains[0].steps, "fix");
  assert.deepEqual(passed, ["plan.files"]);
  assert.deepEqual(page("listsPassed")({}, [], "fix"), []);
});

test("the router opens each kind of page", async () => {
  reset();
  route("GET", "/api/library", library);
  route("GET", "/api/memory", { text: "Use tabs", path: "/work/demo/.swarm/memory.md" });
  route("GET", "/api/settings", { project: {}, shared: {}, default: "claude", installed: { claude: "/usr/bin/claude" } });

  await go("#/memory");
  assert.equal($("h1").textContent, "Project memory");
  assert.ok($("#memory-link").classList.contains("active"));
  await go("#/settings");
  assert.equal($("h1").textContent, "Agents and input defaults");
  assert.ok($("#settings-link").classList.contains("active"));
  assert.equal($("#settings-meta").textContent, "agents: claude");
  await go("#/tool/test");
  assert.equal($("h1").textContent, "test");
  assert.deepEqual($$("#tools .side-item.active").map((a) => a.getAttribute("href")), ["#/tool/test"]);
  await go("#/prompt/fix/plan");
  assert.equal($("h1").textContent, "plan");
  await go("#/chain/fix");
  assert.equal($("#chain-title").textContent, "Fix a bug");
  assert.deepEqual($$("#chains .side-item.active").map((a) => a.getAttribute("href")), ["#/chain/fix"]);
  await go("#/edit/fix/test");
  assert.equal($("#chain-title").textContent, "Fix a bug");
  assert.deepEqual({ ...page("current") }, { kind: "chain", name: "fix" });
  await go("#/run/1028-101500");
  assert.equal(sources.at(-1).url, "/api/runs/1028-101500/events");
  assert.equal($("#main .empty").textContent, "Loading…");
  await go("#/chain");
  assert.equal($("h1").textContent, "demo");
  assert.equal(sources.at(-1).closed, true);
  await go("#/nowhere");
  assert.equal($("h1").textContent, "demo");
  assert.ok($("#home-link").classList.contains("active"));
  assert.deepEqual({ ...page("current") }, { kind: "home", name: "" });
});

test("a library that cannot be read shows an error page", async () => {
  reset();
  route("GET", "/api/library", () => fail(500, "chains/fix.json is not valid JSON"));
  await go("#/tool/broken");
  assert.equal($("#main .eyebrow").textContent, "Cannot read the chains");
  assert.equal($("#main .error-box").textContent, "chains/fix.json is not valid JSON");
});

test("unsaved editor changes hold back navigation until confirmed", async () => {
  reset();
  route("GET", "/api/library", library);
  route("GET", "/api/memory", { text: "", path: "/work/demo/.swarm/memory.md" });
  await go("#/chain/fix");
  page("editor.dirty = true");
  answers.confirm = false;
  await go("#/memory");
  assert.deepEqual(dialogs.confirm, ["Discard unsaved changes?"]);
  assert.equal(hash(), "#/chain/fix");
  assert.equal($("#chain-title").textContent, "Fix a bug");

  page("editor.dirty = true");
  answers.confirm = true;
  await go("#/memory");
  assert.equal(dialogs.confirm.length, 2);
  assert.equal($("h1").textContent, "Project memory");
  assert.equal(page("editor"), null);
});

test("switching projects reloads runs and the page, and asks first about unsaved changes", async () => {
  reset();
  page("sessionStorage.clear()");
  route("GET", "/api/library", library);
  route("GET", "/api/projects", { projects: ["/work/demo", "/work/other"], default: "/work/demo" });
  await page("loadProjects()");
  const select = $("#project-select");
  assert.deepEqual($$("#project-select option").map((o) => [o.value, o.textContent, o.title]), [["/work/demo", "demo", "/work/demo"], ["/work/other", "other", "/work/other"]]);
  assert.equal(select.value, "/work/demo");

  await go("#/tool/test");
  type(select, "/work/other");
  await settle(20);
  assert.equal(page("project"), "/work/other");
  assert.equal(page("sessionStorage.getItem('swarm.project')"), "/work/other");
  assert.ok(calls.some((c) => c.path === "/api/runs" && c.query.project === "/work/other"));
  assert.equal(hash(), "#/");
  assert.equal($("#main .eyebrow").textContent, "/work/other");

  const reads = () => calls.filter((c) => c.path === "/api/library" && c.query.project === "/work/demo").length;
  const earlier = reads();
  type(select, "/work/demo");
  await settle(20);
  assert.equal(reads(), earlier + 1, "switching while home refreshes the page in place");
  assert.equal($("#main .eyebrow").textContent, "/work/demo");

  page("sessionStorage.setItem('swarm.project', '/work/other')");
  await page("loadProjects()");
  assert.equal(page("project"), "/work/other");
  page("sessionStorage.setItem('swarm.project', '/work/gone')");
  await page("loadProjects()");
  assert.equal(page("project"), "/work/demo");

  await go("#/chain/fix");
  page("editor.dirty = true");
  answers.confirm = false;
  type(select, "/work/other");
  assert.deepEqual(dialogs.confirm, ["Discard unsaved changes and switch project?"]);
  assert.equal(page("project"), "/work/demo");
  assert.equal($("#project-select").value, "/work/demo");
  assert.equal(hash(), "#/chain/fix");

  answers.confirm = true;
  type(select, "/work/other");
  await settle(20);
  assert.equal(dialogs.confirm.length, 2);
  assert.equal(page("project"), "/work/other");
  assert.equal(hash(), "#/");
  page("sessionStorage.clear()");
  await page("loadProjects()");
});

test("a project folder is added through a prompt", async () => {
  reset();
  page("sessionStorage.clear()");
  let added = false;
  route("GET", "/api/projects", () => ({ projects: added ? ["/work/demo", "/work/new"] : ["/work/demo"], default: "/work/demo" }));
  route("POST", "/api/projects", () => {
    added = true;
    return { project: "/work/new" };
  });
  await page("loadProjects()");

  click($("#add-project"));
  await settle();
  assert.deepEqual(dialogs.prompt, ["Project folder (absolute path)"]);
  assert.equal(calls.filter((c) => c.method === "POST").length, 0);

  answers.prompt = "/work/new";
  click($("#add-project"));
  await settle(30);
  assert.deepEqual(JSON.parse(calls.find((c) => c.method === "POST").body), { path: "/work/new" });
  assert.equal(page("project"), "/work/new");
  assert.deepEqual($$("#project-select option").map((o) => o.value), ["/work/demo", "/work/new"]);
  assert.equal($("#project-select").value, "/work/new");

  route("POST", "/api/projects", () => fail(400, "not a folder: /nope"));
  answers.prompt = "/nope";
  click($("#add-project"));
  await settle(20);
  assert.deepEqual(dialogs.alert, ["not a folder: /nope"]);
  assert.equal(page("project"), "/work/new");
  page("sessionStorage.clear()");
  route("GET", "/api/projects", { projects: ["/work/demo"], default: "/work/demo" });
  await page("loadProjects()");
});

test("the menu button opens the sidebar and navigation closes it", async () => {
  reset();
  const menu = $("#menu-btn");
  click(menu);
  assert.equal(doc.body.classList.contains("menu-open"), true);
  assert.equal(menu.getAttribute("aria-expanded"), "true");
  click(menu);
  assert.equal(doc.body.classList.contains("menu-open"), false);
  assert.equal(menu.getAttribute("aria-expanded"), "false");
  click(menu);
  await go("#/tool/menu");
  assert.equal(doc.body.classList.contains("menu-open"), false);
});

test("sidebar sections fold and remember it, but their + buttons do not fold them", async () => {
  reset();
  const section = $('[data-fold="chains"]');
  click(section.querySelector(".side-label"));
  assert.equal(section.classList.contains("folded"), true);
  assert.deepEqual(JSON.parse(page("localStorage.getItem('swarm.folded')")), ["chains"]);
  click(section.querySelector(".side-label"));
  assert.equal(section.classList.contains("folded"), false);
  assert.deepEqual(JSON.parse(page("localStorage.getItem('swarm.folded')")), []);

  click(section.querySelector("[data-new]"));
  await settle();
  assert.equal(section.classList.contains("folded"), false);
  assert.equal(dialogs.prompt.length, 1);
});

test("new chains and tools are named through a prompt and opened", async () => {
  reset();
  route("PUT", /^\/api\/items\//, { ok: "true" });
  const puts = () => calls.filter((c) => c.method === "PUT");

  click($('[data-new="chain"]'));
  await settle();
  assert.match(dialogs.prompt[0], /^New chain name/);
  assert.equal(puts().length, 0);

  answers.prompt = "deploy";
  click($('[data-new="chain"]'));
  await settle(30);
  assert.equal(puts()[0].path, "/api/items/chains/deploy.json");
  assert.deepEqual(puts()[0].query, { scope: "project", project: "/work/demo" });
  assert.deepEqual(JSON.parse(puts()[0].body), { title: "deploy", steps: [] });
  assert.equal(hash(), "#/chain/deploy");

  answers.prompt = "lint";
  click($('[data-new="tool"]'));
  await settle(30);
  assert.equal(puts()[1].path, "/api/items/tools/lint.json");
  assert.deepEqual(JSON.parse(puts()[1].body), { title: "lint", run: "echo hello" });
  assert.equal(hash(), "#/tool/lint");

  route("PUT", /^\/api\/items\//, () => fail(400, "invalid name", ["names use lowercase letters, digits, - and _"]));
  answers.prompt = "Bad Name";
  click($('[data-new="chain"]'));
  await settle(20);
  assert.deepEqual(dialogs.alert, ["Refused:\n- names use lowercase letters, digits, - and _"]);
  assert.equal(hash(), "#/tool/lint");
});

const withSubchain = () => [
  library.chains[0],
  { name: "broken", title: "Broken", steps: [], problems: ["no steps"], source: "shared" },
  { name: "odd", title: "Odd", steps: [{ id: "a", title: "A", tool: "t" }, { id: "b", title: "B", chain: "part" }], warnings: ["unused input"] },
  library.chains[1],
];

test("the sidebar lists chains, sub-chains, tools, waiting and recent runs", async () => {
  reset();
  const chains = withSubchain();
  route("GET", "/api/library", { ...library, chains });
  const run = (n, status) => ({ id: `1028-10${n}`, chain: "fix", title: "Fix a bug", subject: `Bug ${n}`, status, started_at: before(60 * n), inputs: {} });
  const runs = [run(0, "waiting"), ...[1, 2, 3, 4, 5, 6, 7, 8].map((n) => run(n, "done"))];
  route("GET", "/api/runs", runs);
  await page("loadRuns()");
  await go("#/tool/test");

  assert.equal($("#waiting-section").hidden, false);
  assert.deepEqual(texts("#waiting .side-item .text").map((t) => t.split("Fix a bug")[0]), ["Bug 0"]);
  assert.equal($("#home-meta").textContent, "1 waiting");
  assert.deepEqual(texts("#chains .side-item .text"), ["Fix a bug", "Broken", "Odd"]);
  assert.deepEqual(texts("#chains .side-item .meta"), ["7", "!", "⚠"]);
  assert.deepEqual(texts("#chains .side-item .tag"), ["project"]);
  assert.equal($("#subchains-section").hidden, false);
  assert.deepEqual($$("#subchains .side-item").map((a) => a.getAttribute("href")), ["#/chain/part"]);
  assert.deepEqual(texts("#subchains .side-item .sub"), ["used by Fix a bug, Odd"]);
  assert.deepEqual(texts("#subchains .side-item .meta"), ["1"]);
  assert.equal($$("#runs .side-item").length, 6);
  assert.equal($("#runs .side-more").textContent, "All 9 runs");
  assert.match($("#runs .side-item .sub").textContent, /^Fix a bug · 1m ago$/);
  assert.deepEqual(texts("#tools .side-item .text"), ["cleanup", "compile", "ship", "test"]);
  assert.deepEqual(texts("#tools .side-item .tag"), ["project", "project", "project"]);
  assert.ok($('#tools .side-item[href="#/tool/test"]').classList.contains("active"));

  reset();
  await page("loadRuns()");
  await go("#/tool/empty");
  assert.equal($("#waiting-section").hidden, true);
  assert.equal($("#home-meta").textContent, "");
  assert.equal($("#settings-meta").textContent, "");
  assert.deepEqual(texts("#chains .side-empty"), ["No chains"]);
  assert.equal($("#subchains-section").hidden, true);
  assert.deepEqual(texts("#runs .side-empty"), ["No runs"]);
  assert.deepEqual(texts("#tools .side-empty"), ["No tools"]);
});

test("the home page shows runs waiting for a decision and a card per chain", async () => {
  reset();
  route("GET", "/api/library", { ...library, chains: withSubchain() });
  const waiting ={ id: "1028-111111", chain: "fix", title: "Fix a bug", subject: "", status: "waiting", started_at: before(10), question: "Ship this change?", inputs: {} };
  const quiet = { ...waiting, id: "1028-121212", subject: "Quiet", question: "" };
  route("GET", "/api/runs", [waiting, quiet]);
  await page("loadRuns()");
  await go("#/tool/x");
  await go("#/");

  assert.equal($("#main .eyebrow").textContent, "/work/demo");
  assert.equal($("#main h1").textContent, "demo");
  const cards = $$(".card-link.waiting");
  assert.deepEqual(cards.map((c) => c.getAttribute("href")), ["#/run/1028-111111", "#/run/1028-121212"]);
  assert.equal(cards[0].querySelector(".card-link-title").textContent, "Fix a bug");
  assert.equal(cards[0].querySelector(".hint").textContent, "Fix a bug · started just now");
  assert.equal(cards[0].querySelector(".card-link-text").textContent, "Ship this change?");
  assert.equal(cards[1].querySelector(".card-link-text") === null, true);
  assert.equal(cards[0].querySelector(".btn").textContent, "Decide");

  const chainCards = $$('a.card-link[href^="#/chain/"]');
  assert.deepEqual(chainCards.map((c) => c.getAttribute("href")), ["#/chain/fix", "#/chain/broken", "#/chain/odd", "#/chain/part"]);
  assert.deepEqual(chainCards.map((c) => c.closest(".section").querySelector("h2").textContent), ["Start a chain", "Start a chain", "Start a chain", "Sub-chains"]);
  assert.equal(chainCards[0].querySelector(".tag").textContent, "project");
  assert.equal(chainCards[0].querySelector(".card-link-text").textContent, "Plans and tests a fix.");
  assert.deepEqual(chainCards.map((c) => c.querySelector(".hint").textContent), ["7 steps", "0 steps · cannot run", "2 steps · warnings", "1 steps · used by Fix a bug, Odd"]);
  assert.equal(chainCards[1].querySelector(".card-link-text") === null, true);
  assert.equal(chainCards[1].querySelector(".tag") === null, true);
  assert.deepEqual($$(".run-line .run-line-status").map((s) => s.textContent), ["Waiting for you", "Waiting for you"]);

  reset();
  await page("loadRuns()");
  await go("#/tool/x");
  await go("#/");
  assert.match($("#main .section .hint").textContent, /^No chains\. Add one with \+, or run swarm samples\./);
  assert.equal($$("#main .section").at(-1).querySelector(".hint").textContent, "No runs.");
});

test("the home list deletes a finished run only after confirming", async () => {
  reset();
  const done = { id: "1028-131313", chain: "fix", title: "Fix a bug", subject: "Old", status: "odd", started_at: before(30), ended_at: before(10), inputs: {} };
  route("GET", "/api/runs", [done]);
  await page("loadRuns()");
  await go("#/tool/x");
  await go("#/");
  assert.equal($(".run-line-status").textContent, "odd");
  assert.match($(".run-line-when").textContent, /^just now · 20s$/);
  answers.confirm = false;
  click($(".run-line-delete"));
  await settle();
  assert.equal(dialogs.confirm.length, 1);
  assert.equal(calls.filter((c) => c.method === "DELETE").length, 0);
  assert.equal(hash(), "#/");
});

test("form helpers wire inputs to their callbacks", () => {
  let got;
  const keep = (v) => { got = v; };

  const label = page("field")("Name", doc.createElement("input"), "lowercase");
  assert.equal(label.className, "field");
  assert.equal(label.querySelector("span").textContent, "Name lowercase");
  assert.equal(page("field")("Plain", doc.createElement("input")).querySelector(".hint") === null, true);

  const input = page("textInput")("a", keep, { placeholder: "name" });
  assert.equal(input.value, "a");
  assert.equal(input.placeholder, "name");
  type(input, "b");
  assert.equal(got, "b");
  assert.equal(page("textInput")(undefined, keep).value, "");

  const area = page("textArea")(null, keep, { rows: 3 });
  assert.equal(area.value, "");
  type(area, "text");
  assert.equal(got, "text");

  const select = page("selectBox")([["a", "A"], ["b", "B"]], "b", keep);
  assert.equal(select.value, "b");
  type(select, "a");
  assert.equal(got, "a");

  const lines = page("linesInput")(["x", "y"], keep);
  assert.equal(lines.value, "x\ny");
  assert.equal(lines.className, "code");
  type(lines, " c \n\n d ");
  assert.deepEqual(got, ["c", "d"]);
  assert.equal(page("linesInput")(undefined, keep, { rows: 4 }).getAttribute("rows"), "4");
});

test("rowsEditor edits, adds and removes rows", () => {
  const rows = [{ name: "a", value: "1", on: true }];
  const columns = [{ key: "name", label: "name", mono: true }, { key: "value", label: "value" }, { key: "on", label: "on", check: true }];
  let changes = 0;
  const editor = page("rowsEditor")(rows, columns, (r) => {
    assert.equal(r, rows);
    changes++;
  });
  const first = () => editor.querySelector(".kv-row");
  assert.equal(first().className, "kv-row three");
  const [name, value] = first().querySelectorAll("input:not([type])");
  assert.equal(name.className, "code");
  assert.equal(name.title, "name");
  assert.equal(value.value, "1");

  type(name, "b");
  assert.equal(rows[0].name, "b");
  const box = first().querySelector("input[type=checkbox]");
  assert.equal(box.checked, true);
  assert.equal(first().querySelector("label.check").className, "check on");
  box.checked = false;
  change(box);
  assert.equal(rows[0].on, undefined);
  assert.equal(first().querySelector("label.check").className, "check ");

  click(editor.querySelector("button.linkish"));
  assert.deepEqual(rows[1], { name: "", value: "" });
  assert.equal(editor.querySelectorAll(".kv-row").length, 2);
  click(first().querySelector("button.icon-btn"));
  assert.deepEqual(rows, [{ name: "", value: "" }]);
  assert.equal(editor.querySelectorAll(".kv-row").length, 1);
  assert.ok(changes >= 4);

  assert.equal(page("rowsEditor")([{}], columns.slice(0, 2), () => {}).querySelector(".kv-row").className, "kv-row ");
  assert.equal(page("rowsEditor")([{}], [...columns, { key: "x", label: "x" }], () => {}).querySelector(".kv-row").className, "kv-row four");
});

test("usedBy links each chain and step that matches", async () => {
  reset();
  route("GET", "/api/library", library);
  await page("loadLibrary()");
  const usedBy = page("usedBy");
  const tests = usedBy((s) => s.tool === "test");
  assert.equal(tests.length, 1);
  assert.equal(tests[0].querySelector(".use-chain").textContent, "Fix a bug");
  assert.equal(tests[0].querySelector(".use-chain").getAttribute("href"), "#/chain/fix");
  assert.deepEqual([...tests[0].querySelectorAll(".use-steps a")].map((a) => [a.textContent, a.getAttribute("href")]), [["Run the tests", "#/chain/fix/test"]]);
  assert.equal(usedBy((s) => s.agent)[0].querySelector(".use-steps").textContent, "Plan the fix, Learn from it");
  const none = usedBy(() => false);
  assert.equal(none.length, 1);
  assert.equal(none[0].textContent, "Unused");
});
