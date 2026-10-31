const main = document.getElementById("main");
const lightbox = document.getElementById("lightbox");
let lib = { chains: [], tools: {}, agent_tools: [] };
let projects = [];
let project = "";
let runsCache = [];
let stream = null;
let current = { kind: null, name: null };
let editor = null;
let runView = null;
let lastHash = location.hash;

function h(tag, attrs = {}, ...children) {
  const el = document.createElement(tag);
  for (const [key, value] of Object.entries(attrs)) {
    if (value === null || value === undefined || value === false) continue;
    if (key.startsWith("on")) el.addEventListener(key.slice(2), value);
    else if (key === "class") el.className = value;
    else if (key === "value") el.value = value;
    else el.setAttribute(key, value === true ? "" : value);
  }
  for (const child of children.flat(Infinity)) {
    if (child === null || child === undefined || child === false) continue;
    el.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
  return el;
}

function withProject(path) {
  if (path.startsWith("/api/projects") || /^\/api\/runs\/[^/?]+/.test(path)) return path;
  return `${path}${path.includes("?") ? "&" : "?"}project=${encodeURIComponent(project)}`;
}

async function api(path, options = {}) {
  const res = await fetch(withProject(path), options);
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = new Error(body.problems?.length ? `Refused:\n- ${body.problems.join("\n- ")}` : body.error || res.statusText);
    err.problems = body.problems || [];
    throw err;
  }
  return body;
}

const send = (method, path, body) => api(path, { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
const EXT = { chains: ".json", tools: ".json", prompts: ".md" };
const itemURL = (kind, name, scope = "") => `/api/items/${kind === "prompts" ? "chains" : kind}/${name}${EXT[kind]}${scope ? `?scope=${scope}` : ""}`;
const promptOf = (chainName, name) => lib.chains.find((c) => c.name === chainName)?.prompts?.[name];
const putItem = (kind, name, value, scope = "") =>
  api(itemURL(kind, name, scope), { method: "PUT", body: kind === "prompts" ? value : JSON.stringify(value, null, 2) });
const deleteItem = (kind, name) => api(itemURL(kind, name), { method: "DELETE" });
const clone = (v) => JSON.parse(JSON.stringify(v));

const KIND_LETTER = { tool: "T", agent: "A", user: "U", chain: "C", repeat: "↻", root: "≡" };
const KIND_NAME = { tool: "tool", agent: "agent", user: "user", chain: "chain", repeat: "repeat", item: "item run" };
const holdsSteps = (step) => step.kind === "chain" || step.kind === "item";
const asText = (v) => (v === null || v === undefined ? "" : typeof v === "string" ? v : Array.isArray(v) ? v.map(asText).join(", ") : typeof v === "object" ? JSON.stringify(v) : String(v));
const LONE_PLACEHOLDER = /^\s*\{\{\s*([a-z][a-z0-9_.]*)\s*\}\}\s*$/;

const callersOf = (chain) => lib.chains.filter((c) => c !== chain && (c.steps || []).some((s) => s.chain === chain.name));
const usedByLine = (chain) => `used by ${callersOf(chain).map((c) => c.title).join(", ")}`;

function listsPassed(step, steps, chainName) {
  return Object.values(step.with || {}).map((v) => LONE_PLACEHOLDER.exec(v)?.[1]).filter((name) => {
    const [id, field] = (name || "").split(".");
    const source = steps.find((s) => s.id === id);
    return !!source?.agent && (promptOf(chainName, source.agent)?.returns || []).some((f) => f.name === field && f.value === "list");
  });
}
const STATE_ICON = { done: "✓", failed: "!", rejected: "✕", stopped: "■" };
const STATUS_LABEL = { running: "Running", waiting: "Waiting for you", done: "Done", failed: "Failed", stopped: "Stopped", rejected: "Rejected", interrupted: "Interrupted" };

function kindOf(step) {
  if (step.agent) return "agent";
  if (step.user !== undefined && step.user !== null && step.user !== "") return "user";
  if (step.chain) return "chain";
  if (step.repeat) return "repeat";
  return "tool";
}

function duration(from, to) {
  if (!from) return "";
  const s = Math.max(0, Math.round(((to ? new Date(to) : new Date()) - new Date(from)) / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${String(s % 60).padStart(2, "0")}s`;
  return `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, "0")}m`;
}

function seconds(from, to) {
  if (!from) return "";
  return `${(Math.max(0, (to ? new Date(to) : new Date()) - new Date(from)) / 1000).toFixed(1)}s`;
}

function ago(when) {
  const s = Math.round((Date.now() - new Date(when)) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return new Date(when).toLocaleDateString();
}

const money = (n) => (n ? `$${n.toFixed(2)}` : "");
const fileURL = (runID, path, kind = "file") => `/api/runs/${encodeURIComponent(runID)}/${kind}?path=${encodeURIComponent(path)}`;

async function loadLibrary() {
  lib = await api("/api/library");
}

async function loadRuns() {
  runsCache = await api("/api/runs");
  renderSide();
  if (current.kind === "home") showHome();
}

async function loadProjects() {
  const res = await api("/api/projects");
  projects = res.projects || [];
  let saved = "";
  try { saved = sessionStorage.getItem("swarm.project") || ""; } catch {}
  project = projects.includes(saved) ? saved : res.default;
  renderProjects();
}

const folderName = (path) => path.split("/").filter(Boolean).pop() || path;

function renderProjects() {
  const select = document.getElementById("project-select");
  select.replaceChildren(...projects.map((p) => h("option", { value: p, selected: p === project, title: p }, folderName(p))));
}

document.getElementById("project-select").addEventListener("change", (e) => switchProject(e.target.value));
document.getElementById("add-project").addEventListener("click", async () => {
  const path = prompt("Project folder (absolute path)", project.replace(/[^/]+\/?$/, ""));
  if (!path) return;
  try {
    const res = await send("POST", "/api/projects", { path });
    await loadProjects();
    switchProject(res.project);
  } catch (err) {
    alert(err.message);
  }
});
document.getElementById("menu-btn").addEventListener("click", () => {
  const open = document.body.classList.toggle("menu-open");
  document.getElementById("menu-btn").setAttribute("aria-expanded", String(open));
});

function switchProject(next, page = "#/") {
  if (editor?.dirty && !confirm("Discard unsaved changes and switch project?")) {
    renderProjects();
    return;
  }
  if (editor) editor.dirty = false;
  project = next;
  try { sessionStorage.setItem("swarm.project", next); } catch {}
  renderProjects();
  loadRuns().catch(() => {});
  if (location.hash === page) refresh();
  else location.hash = page;
}

const FROM = { shared: "shared, all projects", project: "this project only" };
const TAG = { project: ["project", "In this project's .swarm/"] };

function renderSide() {
  const active = (kind, name) => current.kind === kind && current.name === name;
  const item = (kind, name, label, meta, source) =>
    h("a", { class: `side-item ${active(kind, name) ? "active" : ""}`, href: `#/${kind}/${encodeURIComponent(name)}` },
      h("span", { class: "text" }, label), TAG[source] ? h("span", { class: `tag ${source}`, title: TAG[source][1] }, TAG[source][0]) : null,
      meta ? h("span", { class: "meta" }, meta) : null);
  const runItem = (r) =>
    h("a", { class: `side-item ${active("run", r.id) ? "active" : ""}`, href: `#/run/${r.id}` },
      h("span", { class: `dot ${r.status}` }),
      h("span", { class: "text" }, r.subject || r.title, h("span", { class: "sub" }, `${r.title} · ${ago(r.started_at)}`)));
  const list = (id, items, empty) => document.getElementById(id).replaceChildren(...(items.length ? items : [h("div", { class: "side-empty" }, empty)]));
  const waiting = runsCache.filter((r) => r.status === "waiting");
  const recent = runsCache.filter((r) => r.status !== "waiting").slice(0, 6);
  const more = runsCache.length > waiting.length + recent.length ? [h("a", { class: "side-more", href: "#/" }, `All ${runsCache.length} runs`)] : [];

  document.getElementById("home-link").classList.toggle("active", current.kind === "home");
  document.getElementById("home-meta").textContent = waiting.length ? `${waiting.length} waiting` : "";
  document.getElementById("memory-link").classList.toggle("active", current.kind === "memory");
  document.getElementById("settings-link").classList.toggle("active", current.kind === "settings");
  document.getElementById("settings-meta").textContent = lib.default_runner ? `agents: ${lib.default_runner}` : "";
  document.getElementById("waiting-section").hidden = !waiting.length;
  document.getElementById("waiting").replaceChildren(...waiting.map(runItem));
  const chainMeta = (c) => (c.problems?.length ? "!" : c.warnings?.length ? "⚠" : `${c.steps?.length ?? 0}`);
  const subchains = lib.chains.filter((c) => callersOf(c).length);
  list("chains", lib.chains.filter((c) => !subchains.includes(c)).map((c) => item("chain", c.name, c.title, chainMeta(c), c.source)), "No chains");
  document.getElementById("subchains-section").hidden = !subchains.length;
  document.getElementById("subchains").replaceChildren(...subchains.map((c) => item("chain", c.name, [c.title, h("span", { class: "sub" }, usedByLine(c))], chainMeta(c), c.source)));
  list("runs", [...recent.map(runItem), ...more], "No runs");
  list("tools", Object.keys(lib.tools).sort().map((n) => item("tool", n, n, null, lib.tools[n].source)), "No tools");
}

for (const button of document.querySelectorAll("[data-new]")) {
  button.addEventListener("click", (e) => { e.stopPropagation(); createItem(button.dataset.new); });
}

const folded = new Set((() => { try { return JSON.parse(localStorage.getItem("swarm.folded")) || []; } catch { return []; } })());
for (const section of document.querySelectorAll("[data-fold]")) {
  const name = section.dataset.fold;
  section.classList.toggle("folded", folded.has(name));
  section.querySelector(".side-label").addEventListener("click", () => {
    folded.has(name) ? folded.delete(name) : folded.add(name);
    section.classList.toggle("folded", folded.has(name));
    try { localStorage.setItem("swarm.folded", JSON.stringify([...folded])); } catch {}
  });
}

const NEW_PROMPT = "TODO: the task. {{request}}, {{memory}} and other placeholders are filled from the chain.\n\n## Return\n\n- `summary`: one-line result\n";

async function createItem(kind) {
  const name = prompt(`New ${kind} name ([a-z0-9_-])`);
  if (!name) return;
  try {
    if (kind === "chain") await putItem("chains", name, { title: name, steps: [] }, "project");
    if (kind === "tool") await putItem("tools", name, { title: name, run: "echo hello" }, "project");
    await loadLibrary();
    location.hash = `#/${kind}/${name}`;
  } catch (err) {
    alert(err.message);
  }
}

function closeStream() {
  if (stream) { stream.close(); stream = null; }
}

async function refresh() {
  if (editor?.dirty && location.hash !== lastHash && !confirm("Discard unsaved changes?")) {
    history.replaceState(null, "", lastHash);
    return;
  }
  lastHash = location.hash;
  closeStream();
  document.body.classList.remove("menu-open");
  try {
    await loadLibrary();
  } catch (err) {
    main.replaceChildren(h("div", { class: "page" }, h("div", { class: "eyebrow" }, "Cannot read the chains"), h("div", { class: "error-box" }, err.message)));
    return;
  }
  const [, kind = "", ...rest] = location.hash.split("/");
  const name = decodeURIComponent(rest[0] || "");
  const stepID = decodeURIComponent(rest[1] || "");
  current = { kind, name };
  editor = null;
  runView = null;
  if (kind === "run") showRun(name);
  else if (kind === "memory") showMemory();
  else if (kind === "settings") showSettings();
  else if (kind === "tool") showTool(name);
  else if (kind === "prompt") showPrompt(name, stepID);
  else if ((kind === "chain" || kind === "edit") && name) {
    current = { kind: "chain", name };
    showEditor(name, stepID);
  } else {
    current = { kind: "home", name: "" };
    showHome();
  }
  renderSide();
}

function field(label, input, hint) {
  return h("label", { class: "field" }, h("span", {}, label, hint ? h("span", { class: "hint" }, ` ${hint}`) : null), input);
}

function textInput(value, onInput, attrs = {}) {
  return h("input", { ...attrs, value: value ?? "", oninput: (e) => onInput(e.target.value) });
}

function textArea(value, onInput, attrs = {}) {
  return h("textarea", { ...attrs, oninput: (e) => onInput(e.target.value) }, value ?? "");
}

function selectBox(options, value, onChange) {
  return h("select", { onchange: (e) => onChange(e.target.value) },
    options.map(([v, label]) => h("option", { value: v, selected: v === value }, label)));
}

function linesInput(list, onChange, attrs = {}) {
  return textArea((list || []).join("\n"), (v) => onChange(v.split("\n").map((s) => s.trim()).filter(Boolean)), { class: "code", rows: 2, ...attrs });
}

function rowsEditor(rows, columns, onChange) {
  const wrap = h("div", { class: "kv" });
  const draw = () => {
    wrap.replaceChildren(
      ...rows.map((row, i) => h("div", { class: `kv-row ${["", "", "", "three", "four"][columns.length]}` },
        columns.map((col) => (col.check
          ? h("label", { class: `check ${row[col.key] ? "on" : ""}` },
            h("input", { type: "checkbox", checked: !!row[col.key], onchange: (e) => { row[col.key] = e.target.checked || undefined; onChange(rows); draw(); } }), col.label)
          : textInput(row[col.key], (v) => { row[col.key] = v; onChange(rows); }, { placeholder: col.label, title: col.label, class: col.mono ? "code" : "" }))),
        h("button", { class: "icon-btn", type: "button", title: "Remove", onclick: () => { rows.splice(i, 1); onChange(rows); draw(); } }, "×"))),
      h("button", { class: "linkish", type: "button", onclick: () => { rows.push(Object.fromEntries(columns.filter((c) => !c.check).map((c) => [c.key, ""]))); onChange(rows); draw(); } }, "+ add"));
  };
  draw();
  return wrap;
}

const placeholdersIn = (text) => [...new Set([...(text || "").matchAll(/\{\{\s*([a-z][a-z0-9_.]*)\s*\}\}/g)].map((m) => m[1]))];
const returnsText = (prompt) => (prompt?.returns || []).map((f) => (f.value === "text" ? f.name : `${f.name} (${f.value})`)).join(", ");
const mapToRows = (m) => Object.entries(m || {}).map(([key, value]) => ({ key, value }));
const rowsToMap = (rows) => Object.fromEntries(rows.filter((r) => r.key).map((r) => [r.key, r.value]));

function usedBy(match, verdicts) {
  const groups = lib.chains.map((chain) => [chain, (chain.steps || []).filter((s) => match(s, chain))]).filter(([, steps]) => steps.length);
  if (!groups.length) return [h("span", { class: "hint" }, "Unused")];
  return groups.map(([chain, steps]) => {
    const v = verdicts?.[`chains/${chain.name}.json`] || chain;
    return h("div", { class: "use" },
      h("a", { class: "use-chain", href: `#/chain/${chain.name}` }, chain.title),
      h("span", { class: "use-steps" }, steps.map((s, i) => [i ? ", " : "", h("a", { href: `#/chain/${chain.name}/${s.id}` }, s.title)])),
      (v.problems || []).map((p) => h("div", { class: "error-text" }, `cannot run: ${p}`)),
      (v.warnings || []).map((w) => h("div", { class: "hint" }, `⚠ ${w}`)));
  });
}

const CHECK_DELAY = 150;
let checkTimer;
let checkSeq = 0;

function checkLater(draft, show) {
  clearTimeout(checkTimer);
  const seq = ++checkSeq;
  checkTimer = setTimeout(async () => {
    const verdicts = await send("POST", "/api/check", draft).catch(() => null);
    if (verdicts && seq === checkSeq) show(verdicts);
  }, CHECK_DELAY);
}

function draftStatus(status, problems = []) {
  status.className = problems.length ? "status error-text" : "status";
  status.textContent = problems.length ? `Unsaved; invalid: ${problems.join("; ")}` : "Unsaved";
}
