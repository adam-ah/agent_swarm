const main = document.getElementById("main");
const lightbox = document.getElementById("lightbox");
let lib = { chains: [], tools: {}, prompts: {}, agent_tools: [] };
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
const itemURL = (kind, name, scope = "") => `/api/items/${kind}/${name}${EXT[kind]}${scope ? `?scope=${scope}` : ""}`;
const putItem = (kind, name, value, scope = "") =>
  api(itemURL(kind, name, scope), { method: "PUT", body: kind === "prompts" ? value : JSON.stringify(value, null, 2) });
const deleteItem = (kind, name) => api(itemURL(kind, name), { method: "DELETE" });
const clone = (v) => JSON.parse(JSON.stringify(v));

const KIND_LETTER = { tool: "T", agent: "A", approval: "U", chain: "C", root: "≡" };
const KIND_NAME = { tool: "tool", agent: "agent", approval: "user", chain: "chain" };
const STATE_ICON = { done: "✓", failed: "!", rejected: "✕", stopped: "■" };
const STATUS_LABEL = { running: "Running", waiting: "Waiting for you", done: "Done", failed: "Failed", stopped: "Stopped", rejected: "Rejected", interrupted: "Interrupted" };

function kindOf(step) {
  if (step.agent) return "agent";
  if (step.approval !== undefined && step.approval !== null && step.approval !== "") return "approval";
  if (step.chain) return "chain";
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

function ago(when) {
  const s = Math.round((Date.now() - new Date(when)) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return new Date(when).toLocaleDateString();
}

const money = (n) => (n ? `$${n.toFixed(2)}` : "");
const fileURL = (runID, path, kind = "file") => `/api/runs/${encodeURIComponent(runID)}/${kind}?path=${encodeURIComponent(path)}`;

/* data */

async function loadLibrary() {
  lib = await api("/api/library");
}

async function loadRuns() {
  runsCache = await api("/api/runs");
  renderSide();
}

/* projects */

async function loadProjects() {
  const res = await api("/api/projects");
  projects = res.projects || [];
  let saved = "";
  try { saved = localStorage.getItem("swarm.project") || ""; } catch {}
  project = projects.includes(saved) ? saved : res.default;
  renderProjects();
}

function renderProjects() {
  const select = document.getElementById("project-select");
  select.replaceChildren(...projects.map((p) => h("option", { value: p, selected: p === project, title: p }, p.split("/").filter(Boolean).pop() || p)));
}

document.getElementById("project-select").addEventListener("change", (e) => switchProject(e.target.value));
document.getElementById("add-project").addEventListener("click", async () => {
  const path = prompt("Full path of a project folder", "/mnt/e/Temp/");
  if (!path) return;
  try {
    const res = await send("POST", "/api/projects", { path });
    await loadProjects();
    switchProject(res.project);
  } catch (err) {
    alert(err.message);
  }
});

function switchProject(next) {
  if (editor?.dirty && !confirm("You have unsaved changes. Switch folders without saving?")) {
    renderProjects();
    return;
  }
  if (editor) editor.dirty = false;
  project = next;
  try { localStorage.setItem("swarm.project", next); } catch {}
  renderProjects();
  loadRuns().catch(() => {});
  if (location.hash === "#/chain/") refresh();
  else location.hash = "#/chain/";
}

/* sidebar */

const FROM = { shared: "your shared library (~/.swarm)", project: "this folder's .swarm" };
const TAG = { project: ["folder", "Lives in this folder's .swarm"] };

function renderSide() {
  const item = (kind, name, label, meta, source) =>
    h("a", { class: `side-item ${current.kind === kind && current.name === name ? "active" : ""}`, href: `#/${kind}/${encodeURIComponent(name)}` },
      h("span", { class: "text" }, label), TAG[source] ? h("span", { class: `tag ${source}`, title: TAG[source][1] }, TAG[source][0]) : null,
      meta ? h("span", { class: "meta" }, meta) : null);
  const list = (id, items, empty) => document.getElementById(id).replaceChildren(...(items.length ? items : [h("div", { class: "side-empty" }, empty)]));

  document.getElementById("memory-link").classList.toggle("active", current.kind === "memory");
  document.getElementById("settings-link").classList.toggle("active", current.kind === "settings");
  document.getElementById("settings-meta").textContent = lib.default_runner ? `agents: ${lib.default_runner}` : "";
  list("chains", lib.chains.map((c) => item("chain", c.name, c.title, c.problems?.length ? "!" : `${c.steps?.length ?? 0}`, c.source)), "No chains");
  list("tools", Object.keys(lib.tools).sort().map((n) => item("tool", n, n, null, lib.tools[n].source)), "No tools");
  list("prompts", Object.keys(lib.prompts).sort().map((n) => item("prompt", n, n, null, lib.prompts[n].source)), "No prompts");
  list("runs", runsCache.map((r) =>
    h("a", { class: `side-item ${current.kind === "run" && current.name === r.id ? "active" : ""}`, href: `#/run/${r.id}` },
      h("span", { class: `dot ${r.status}` }),
      h("span", { class: "text" }, r.subject || r.title, h("span", { class: "sub" }, `${r.title} · ${ago(r.started_at)}`)))), "No runs yet");
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

async function createItem(kind) {
  const name = prompt(`Name for the new ${kind} (lowercase letters, digits, - and _)`);
  if (!name) return;
  try {
    if (kind === "chain") await putItem("chains", name, { title: name, steps: [] }, "project");
    if (kind === "tool") await putItem("tools", name, { title: name, run: "echo hello" }, "project");
    if (kind === "prompt") await putItem("prompts", name, "Describe the job here. Use {{request}} or {{memory}} for values from the chain.\n", "project");
    await loadLibrary();
    location.hash = `#/${kind}/${name}`;
  } catch (err) {
    alert(err.message);
  }
}

/* routing */

function closeStream() {
  if (stream) { stream.close(); stream = null; }
}

async function refresh() {
  if (editor?.dirty && location.hash !== lastHash && !confirm("You have unsaved changes. Leave without saving?")) {
    history.replaceState(null, "", lastHash);
    return;
  }
  lastHash = location.hash;
  closeStream();
  try {
    await loadLibrary();
  } catch (err) {
    main.replaceChildren(h("div", { class: "page" }, h("div", { class: "eyebrow" }, "Cannot read the chains"), h("div", { class: "error-box" }, err.message)));
    return;
  }
  const [, kind = "chain", ...rest] = location.hash.split("/");
  const name = decodeURIComponent(rest.join("/"));
  current = { kind, name };
  editor = null;
  runView = null;
  if (kind === "run") showRun(name);
  else if (kind === "memory") showMemory();
  else if (kind === "settings") showSettings();
  else if (kind === "tool") showTool(name);
  else if (kind === "prompt") showPrompt(name);
  else {
    current = { kind: "chain", name: name || lib.chains[0]?.name };
    showChain(current.name);
  }
  renderSide();
}

/* form helpers */

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
      ...rows.map((row, i) => h("div", { class: `kv-row ${columns.length === 3 ? "three" : ""}` },
        columns.map((col) => textInput(row[col.key], (v) => { row[col.key] = v; onChange(rows); }, { placeholder: col.label, class: col.mono ? "code" : "" })),
        h("button", { class: "icon-btn", type: "button", title: "Remove", onclick: () => { rows.splice(i, 1); onChange(rows); draw(); } }, "×"))),
      h("button", { class: "linkish", type: "button", onclick: () => { rows.push(Object.fromEntries(columns.map((c) => [c.key, ""]))); onChange(rows); draw(); } }, "+ add"));
  };
  draw();
  return wrap;
}

const mapToRows = (m) => Object.entries(m || {}).map(([key, value]) => ({ key, value }));
const rowsToMap = (rows) => Object.fromEntries(rows.filter((r) => r.key).map((r) => [r.key, r.value]));

function usedBy(match) {
  const uses = [];
  for (const chain of lib.chains) {
    for (const step of chain.steps || []) {
      if (match(step)) uses.push(h("a", { href: `#/chain/${chain.name}` }, `${chain.title} → ${step.title}`));
    }
  }
  return uses.length ? uses : [h("span", { class: "hint" }, "Not used by any chain yet")];
}

/* chain editor */

function showChain(name) {
  const chain = lib.chains.find((c) => c.name === name);
  if (!chain) {
    main.replaceChildren(h("div", { class: "page" }, h("div", { class: "empty" },
      "No chains yet. Use + next to Chains to make one, or run ", h("code", {}, "swarm samples"), " in a terminal to add the sample chains.")));
    return;
  }
  const draft = clone(chain);
  delete draft.name;
  delete draft.problems;
  draft.steps = draft.steps || [];
  delete draft.source;
  const prompts = Object.fromEntries(Object.entries(lib.prompts).map(([k, v]) => [k, v.text]));
  editor = { name, source: chain.source, draft, selected: "root", tools: clone(lib.tools), prompts, dirtyTools: new Set(), dirtyPrompts: new Set(), dirty: false, problems: chain.problems || [], message: "" };
  renderChainPage();
}

function touch(redrawInspector = false) {
  editor.dirty = true;
  editor.message = "";
  renderEditorTree();
  renderSaveBar();
  if (redrawInspector) renderInspector();
}

function renderChainPage() {
  const saved = lib.chains.find((c) => c.name === editor.name);
  const inputs = Object.fromEntries((saved.inputs || []).map((i) => [i.name, i.name === "request"
    ? h("textarea", { rows: 3, placeholder: "Describe it in plain words" }, i.default || "")
    : h("input", { value: i.default || "", placeholder: i.label, autocomplete: "off" })]));
  const message = h("span", { class: "error-text" });
  const start = h("form", {
    class: "start",
    onsubmit: async (e) => {
      e.preventDefault();
      if (editor.dirty) { message.textContent = "Save your changes first."; return; }
      try {
        const values = Object.fromEntries(Object.entries(inputs).map(([k, el]) => [k, el.value]));
        const { id } = await send("POST", "/api/runs", { chain: editor.name, inputs: values });
        await loadRuns();
        location.hash = `#/run/${id}`;
      } catch (err) {
        message.textContent = err.message;
      }
    },
  }, (saved.inputs || []).map((i) => h("label", { class: i.name === "request" ? "wide" : "" }, i.optional ? `${i.label || i.name} (optional)` : i.label || i.name, inputs[i.name])), h("button", { class: "btn primary", type: "submit" }, "Start run"), message);

  main.replaceChildren(h("div", { class: "page" },
    h("div", { class: "eyebrow" }, `Chain · ${FROM[editor.source]} · chains/${editor.name}.json`),
    h("h1", { id: "chain-title" }, editor.draft.title),
    editor.draft.description ? h("p", { class: "lead" }, editor.draft.description) : null,
    start,
    h("div", { id: "problems" }),
    h("div", { class: "split" },
      h("div", { class: "panel sticky" },
        h("div", { class: "panel-head" }, h("span", { class: "grow" }, "Tree"), h("div", { class: "savebar", id: "savebar" })),
        h("div", { class: "tree", id: "tree" }),
        h("div", { class: "tree-actions", id: "tree-actions" })),
      h("div", { class: "panel" }, h("div", { class: "panel-body", id: "inspector" })))));
  renderEditorTree();
  renderSaveBar();
  renderInspector();
}

function renderSaveBar() {
  const refused = editor.refused?.length
    ? h("div", { class: "problems" }, h("strong", {}, "Not saved. swarm refused it because:"), h("ul", {}, editor.refused.map((p) => h("li", {}, p))))
    : editor.problems.length && !editor.dirty ? h("ul", { class: "problems" }, editor.problems.map((p) => h("li", {}, p))) : "";
  document.getElementById("problems").replaceChildren(refused);
  document.getElementById("savebar").replaceChildren(...[
    editor.message ? h("span", { class: "status ok-text" }, editor.message) : editor.dirty ? h("span", { class: "status" }, "Unsaved") : null,
    editor.source !== "project" ? h("button", { class: "btn small", title: "Save this chain into this folder's .swarm, so changes stay in this project", onclick: () => saveChain("project") }, "Copy to this folder") : null,
    h("button", { class: "btn small danger", onclick: removeChain }, "Delete chain"),
    h("button", { class: "btn small", disabled: !editor.dirty, onclick: () => showChain(editor.name) }, "Discard"),
    h("button", { class: "btn small primary", disabled: !editor.dirty, onclick: () => saveChain() }, "Save"),
  ].filter(Boolean));
}

async function removeChain() {
  const where = editor.source === "project" ? "this project's copy" : "the shared chain, for every project";
  if (!confirm(`Delete the chain "${editor.draft.title || editor.name}"? This removes ${where}.`)) return;
  try {
    await deleteItem("chains", editor.name);
  } catch (err) {
    alert(err.message);
    return;
  }
  editor.dirty = false;
  await loadLibrary();
  renderSide();
  location.hash = "#/chain/";
  if (location.hash === "#/chain/") refresh();
}

async function saveChain(scope = "") {
  try {
    for (const name of editor.dirtyTools) {
      await putItem("tools", name, { ...editor.tools[name], source: undefined });
      editor.dirtyTools.delete(name);
    }
    for (const name of editor.dirtyPrompts) {
      await putItem("prompts", name, editor.prompts[name]);
      editor.dirtyPrompts.delete(name);
    }
    const draft = clone(editor.draft);
    for (const step of draft.steps) {
      if (step.with && !Object.keys(step.with).length) delete step.with;
    }
    await putItem("chains", editor.name, draft, scope);
  } catch (err) {
    if (!err.problems?.length) {
      alert(err.message);
      return;
    }
    editor.refused = err.problems;
    editor.message = "";
    renderSaveBar();
    return;
  }
  await loadLibrary();
  const selected = editor.selected;
  showChain(editor.name);
  editor.selected = selected;
  editor.message = "Saved";
  renderEditorTree();
  renderSaveBar();
  renderInspector();
  renderSide();
}

function treeRow({ kind, title, id, selected, readonly, depth, onclick, state, time }) {
  return h("div", { class: `tree-row ${selected ? "selected" : ""} ${readonly ? "readonly" : ""} ${state || ""} ${kind === "root" ? "tree-root" : ""}`, onclick },
    state !== undefined ? h("span", { class: `state ${state}` }, STATE_ICON[state] || "") : null,
    h("span", { class: `kind ${kind}`, title: KIND_NAME[kind] || "chain" }, KIND_LETTER[kind]),
    h("span", { class: "title" }, title || "(untitled)"),
    time !== undefined ? h("span", { class: "time" }, time) : id ? h("span", { class: "id" }, id) : null);
}

function subchainRows(chainName, depth) {
  const sub = lib.chains.find((c) => c.name === chainName);
  if (!sub || depth > 4) return null;
  return h("div", { class: "tree-kids" }, (sub.steps || []).map((s) => [
    treeRow({ kind: kindOf(s), title: s.title, id: s.id, readonly: true }),
    s.chain ? subchainRows(s.chain, depth + 1) : null,
  ]));
}

function renderEditorTree() {
  const d = editor.draft;
  document.getElementById("chain-title").textContent = d.title || editor.name;
  document.getElementById("tree").replaceChildren(
    treeRow({ kind: "root", title: d.title || editor.name, id: editor.name, selected: editor.selected === "root", onclick: () => select("root") }),
    h("div", { class: "tree-kids" }, d.steps.map((step, i) => [
      treeRow({ kind: kindOf(step), title: step.title, id: step.id, selected: editor.selected === i, onclick: () => select(i) }),
      step.chain ? subchainRows(step.chain, 1) : null,
    ])));
  const i = editor.selected;
  const isStep = typeof i === "number";
  document.getElementById("tree-actions").replaceChildren(
    ...[["tool", "Tool"], ["agent", "Agent"], ["approval", "User approval"], ["chain", "Sub-chain"]].map(([kind, label]) =>
      h("button", { class: `btn small add-kind ${kind}`, onclick: () => addStep(kind) }, h("span", { class: `kind ${kind}` }, KIND_LETTER[kind]), `+ ${label}`)),
    h("span", { style: "flex:1" }),
    h("button", { class: "btn small", disabled: !isStep || i === 0, onclick: () => move(-1), title: "Move up" }, "↑"),
    h("button", { class: "btn small", disabled: !isStep || i === d.steps.length - 1, onclick: () => move(1), title: "Move down" }, "↓"),
    h("button", { class: "btn small danger", disabled: !isStep, onclick: removeStep }, "Delete step"));
}

function select(i) {
  editor.selected = i;
  renderEditorTree();
  renderInspector();
}

function uniqueID(base) {
  const ids = new Set(editor.draft.steps.map((s) => s.id));
  for (let n = 1; ; n++) if (!ids.has(`${base}_${n}`)) return `${base}_${n}`;
}

function addStep(kind) {
  const otherChains = lib.chains.map((c) => c.name).filter((n) => n !== editor.name);
  const step = {
    tool: { id: uniqueID("tool"), title: "New tool step", tool: Object.keys(editor.tools).sort()[0] || "" },
    agent: { id: uniqueID("agent"), title: "New agent step", agent: Object.keys(editor.prompts).sort()[0] || "", tools: ["Read", "Grep", "Glob"], returns: [{ name: "summary", value: "text" }] },
    approval: { id: uniqueID("approve"), title: "Your approval", approval: "Continue?" },
    chain: { id: uniqueID("sub"), title: "Sub-chain", chain: otherChains[0] || "" },
  }[kind];
  const at = typeof editor.selected === "number" ? editor.selected + 1 : editor.draft.steps.length;
  editor.draft.steps.splice(at, 0, step);
  editor.selected = at;
  touch(true);
}

function move(delta) {
  const steps = editor.draft.steps;
  const i = editor.selected;
  [steps[i], steps[i + delta]] = [steps[i + delta], steps[i]];
  editor.selected = i + delta;
  touch(true);
}

function removeStep() {
  const steps = editor.draft.steps;
  const step = steps[editor.selected];
  if (!confirm(`Delete the step "${step.title}"?`)) return;
  steps.splice(editor.selected, 1);
  editor.selected = steps.length ? Math.min(editor.selected, steps.length - 1) : "root";
  touch(true);
}

function renderInspector() {
  const panel = document.getElementById("inspector");
  if (editor.selected === "root") {
    panel.replaceChildren(rootInspector());
    return;
  }
  const step = editor.draft.steps[editor.selected];
  panel.replaceChildren(stepInspector(step));
}

function rootInspector() {
  const d = editor.draft;
  d.inputs = d.inputs || [];
  d.vars = d.vars || [];
  return h("div", { class: "form" },
    h("h2", {}, "The chain"),
    field("Title", textInput(d.title, (v) => { d.title = v; touch(); })),
    field("Description", textArea(d.description, (v) => { d.description = v; touch(); }, { rows: 2 })),
    h("div", { class: "row2" },
      field("Run heading", textInput(d.subject, (v) => { d.subject = v; touch(); }, { class: "code", placeholder: "e.g. #{{issue}} {{read_issue.title}}" })),
      field("Run link", textInput(d.link, (v) => { d.link = v; touch(); }, { class: "code", placeholder: "e.g. {{read_issue.url}}" }))),
    field("Inputs", rowsEditor(d.inputs, [{ key: "name", label: "name", mono: true }, { key: "label", label: "label" }, { key: "default", label: "default" }], () => touch()), "asked for when a run starts"),
    field("Vars", rowsEditor(d.vars, [{ key: "name", label: "name", mono: true }, { key: "value", label: "value, may use {{placeholders}}", mono: true }], () => touch()), "computed once at the start"));
}

function stepInspector(step) {
  const kind = kindOf(step);
  const setKind = (next) => {
    for (const key of ["tool", "agent", "approval", "revise", "chain", "tools", "returns", "save", "with"]) delete step[key];
    if (next === "tool") step.tool = Object.keys(editor.tools).sort()[0] || "";
    if (next === "agent") { step.agent = Object.keys(editor.prompts).sort()[0] || ""; step.tools = ["Read", "Grep", "Glob"]; step.returns = [{ name: "summary", value: "text" }]; }
    if (next === "approval") step.approval = "Continue?";
    if (next === "chain") step.chain = lib.chains.map((c) => c.name).find((n) => n !== editor.name) || "";
    touch(true);
  };
  return h("div", { class: "form" },
    h("div", { class: "card-head" }, h("span", { class: `chip ${kind}` }, KIND_NAME[kind]), h("span", { class: "card-title" }, step.title || "(untitled)")),
    h("div", { class: "row2" },
      field("Title", textInput(step.title, (v) => { step.title = v; touch(); })),
      field("ID", textInput(step.id, (v) => { step.id = v; touch(); }, { class: "code" }), "used as {{id.field}}")),
    field("Kind", selectBox([["tool", "Tool: run a named action"], ["agent", "Agent: an agent CLI with a prompt"], ["approval", "User approval: wait for a person"], ["chain", "Sub-chain: run another chain"]], kind, setKind)),
    kindFields(step, kind),
    h("div", { class: "subpanel" },
      h("h3", {}, "Flow"),
      h("div", { class: "row2" },
        field("Run only if", textInput(step.when, (v) => { step.when = v || undefined; touch(); }, { class: "code", placeholder: "e.g. triage.verdict == bug" })),
        field("Stop the chain if", textInput(step.stop_if, (v) => { step.stop_if = v || undefined; touch(); }, { class: "code", placeholder: "e.g. verdict != REPRODUCED" }))),
      field("Message when it stops", textInput(step.stop, (v) => { step.stop = v || undefined; touch(); }, { placeholder: "e.g. Could not reproduce. {{summary}}" })),
      field("Files to show", linesInput(step.show, (v) => { step.show = v.length ? v : undefined; touch(); }), "one per line; videos, stills and markdown display inline"),
      field("Headline", textInput(step.headline, (v) => { step.headline = v || undefined; touch(); }, { class: "code", placeholder: "defaults to the summary or last output line" })),
      h("label", { class: "check" + (step.always ? " on" : "") },
        h("input", { type: "checkbox", checked: !!step.always, onchange: (e) => { step.always = e.target.checked || undefined; touch(true); } }),
        "Always runs, even after a failure or stop")));
}

function kindFields(step, kind) {
  if (kind === "approval") {
    const earlier = editor.draft.steps.slice(0, editor.draft.steps.indexOf(step));
    return h("div", { class: "form" },
      field("Question for you", textArea(step.approval, (v) => { step.approval = v || "Continue?"; touch(); }, { rows: 2 }), "add the files to show below"),
      field("Revise goes back to", selectBox([["", "nowhere: only approve or reject"], ...earlier.map((s) => [s.id, s.title || s.id])], step.revise || "", (v) => { step.revise = v || undefined; touch(true); }),
        "the person's note reaches the steps that run again as {{feedback}}"));
  }
  if (kind === "chain") {
    const withRows = mapToRows(step.with);
    return h("div", { class: "form" },
      field("Chain to run", selectBox(lib.chains.filter((c) => c.name !== editor.name).map((c) => [c.name, c.title]), step.chain, (v) => { step.chain = v; touch(true); })),
      field("Values to pass in", rowsEditor(withRows, [{ key: "key", label: "name", mono: true }, { key: "value", label: "value", mono: true }], (rows) => { step.with = rowsToMap(rows); touch(); }), "it also sees everything this chain sees"),
      step.chain ? h("a", { href: `#/chain/${step.chain}` }, `Open the ${step.chain} chain →`) : null);
  }
  if (kind === "agent") {
    const name = step.agent;
    step.returns = step.returns || [];
    const toolChecks = h("div", { class: "checks" }, lib.agent_tools.map((t) => {
      const on = (step.tools || []).includes(t);
      return h("label", { class: `check ${on ? "on" : ""}` },
        h("input", { type: "checkbox", checked: on, onchange: () => {
          const set = new Set(step.tools || []);
          if (set.has(t)) set.delete(t); else set.add(t);
          step.tools = lib.agent_tools.filter((x) => set.has(x));
          touch(true);
        } }), t);
    }));
    return h("div", { class: "form" },
      field("Prompt", promptPicker(step)),
      field("Runs with", selectBox([["", `the default (${lib.default_runner})`], ...lib.runners.map((n) => [n, n])], step.runner || "", (v) => { step.runner = v || undefined; touch(true); }),
        step.runner === "codex" || (!step.runner && lib.default_runner === "codex") ? "Codex uses your own Codex config and ignores the list below" : "change the default under Settings"),
      field("What the agent may use", toolChecks),
      field("Fields it returns", rowsEditor(step.returns, [{ key: "name", label: "name", mono: true }, { key: "value", label: "text · number · bool · list · A | B | C", mono: true }], () => touch()), "summary becomes the step's headline"),
      field("Save the fields to", textInput(step.save, (v) => { step.save = v || undefined; touch(); }, { class: "code", placeholder: "e.g. {{run_dir}}/triage.json" })),
      name ? h("div", { class: "subpanel" },
        h("h3", {}, `Prompt · prompts/${name}.md`),
        textArea(editor.prompts[name], (v) => { editor.prompts[name] = v; editor.dirtyPrompts.add(name); touch(); }, { class: "code prompt" })) : null);
  }
  const name = step.tool;
  const tool = editor.tools[name];
  const withRows = mapToRows(step.with);
  return h("div", { class: "form" },
    field("Tool", toolPicker(step)),
    field("Values for the tool", rowsEditor(withRows, [{ key: "key", label: "name", mono: true }, { key: "value", label: "value", mono: true }], (rows) => { step.with = rowsToMap(rows); touch(); }), "filled into {{name}} in its command"),
    tool ? h("div", { class: "subpanel" },
      h("h3", {}, `Tool · ${name}`),
      field("What it does", textInput(tool.title, (v) => { tool.title = v; editor.dirtyTools.add(name); touch(); })),
      field("Command", textArea(tool.run, (v) => { tool.run = v; editor.dirtyTools.add(name); touch(); }, { class: "code", rows: 6 }), "runs in the project folder"),
      field("Files it shows", linesInput(tool.show, (v) => { tool.show = v; editor.dirtyTools.add(name); touch(); })),
      h("span", { class: "hint" }, "Changes here apply to every chain that uses this tool.")) : null);
}

function toolPicker(step) {
  const names = Object.keys(editor.tools).sort();
  return selectBox([...names.map((n) => [n, `${n} · ${editor.tools[n].title || ""}`]), ["__new", "+ New tool…"]], step.tool, (v) => {
    if (v === "__new") {
      const name = prompt("Name for the new tool");
      if (!name) { touch(true); return; }
      editor.tools[name] = { title: name, run: "echo hello", show: [] };
      editor.dirtyTools.add(name);
      v = name;
    }
    step.tool = v;
    touch(true);
  });
}

function promptPicker(step) {
  const names = Object.keys(editor.prompts).sort();
  return selectBox([...names.map((n) => [n, `prompts/${n}.md`]), ["__new", "+ New prompt…"]], step.agent, (v) => {
    if (v === "__new") {
      const name = prompt("Name for the new prompt");
      if (!name) { touch(true); return; }
      editor.prompts[name] = "Describe the job here.\n";
      editor.dirtyPrompts.add(name);
      v = name;
    }
    step.agent = v;
    touch(true);
  });
}

/* tool and prompt pages */

function itemButtons(kind, name, source, status, payload) {
  const save = async (scope = "") => {
    try {
      await putItem(kind, name, payload(), scope);
      await loadLibrary();
      status.className = "ok-text";
      status.textContent = scope ? "Copied to this folder" : "Saved";
      renderSide();
      if (scope) refresh();
    } catch (err) {
      status.className = "error-text";
      status.textContent = err.message;
    }
  };
  const remove = async () => {
    const what = source === "project" ? "this folder's copy" : "it from the shared library, for every project";
    if (!confirm(`Delete ${name}? This removes ${what}.`)) return;
    try {
      await deleteItem(kind, name);
      await loadLibrary();
      location.hash = "#/chain/";
    } catch (err) {
      status.className = "error-text";
      status.textContent = err.message;
    }
  };
  return h("div", { class: "savebar" }, status,
    source !== "project" ? h("button", { class: "btn small", title: "Save a copy in this folder's .swarm that overrides this one here", onclick: () => save("project") }, "Copy to this folder") : null,
    h("button", { class: "btn small danger", onclick: remove }, "Delete"),
    h("button", { class: "btn small primary", onclick: () => save() }, "Save"));
}

function showTool(name) {
  const tool = clone(lib.tools[name] || { title: name, run: "", show: [] });
  const source = tool.source;
  delete tool.source;
  const status = h("span", { class: "status" });
  const dirty = () => { status.className = "status"; status.textContent = "Unsaved"; };
  main.replaceChildren(h("div", { class: "page" },
    h("div", { class: "eyebrow" }, `Tool · ${FROM[source]} · tools/${name}.json`),
    h("h1", {}, name),
    h("p", { class: "lead" }, "A named action a chain can call. It runs as a shell command in the project folder. Write {{name}} for a chain's inputs and vars, the step's values, or an earlier step's output ({{step_id.field}}); values are shell-quoted. If it prints a JSON object, its fields become the step's output."),
    h("div", { class: "split" },
      h("div", { class: "panel" }, h("div", { class: "panel-head" }, "Used by"), h("div", { class: "panel-body used-by" }, usedBy((s) => s.tool === name))),
      h("div", { class: "panel" },
        h("div", { class: "panel-head" }, h("span", { class: "grow" }, "Definition"), itemButtons("tools", name, source, status, () => tool)),
        h("div", { class: "panel-body form" },
          field("What it does", textInput(tool.title, (v) => { tool.title = v; dirty(); })),
          field("Command", textArea(tool.run, (v) => { tool.run = v; dirty(); }, { class: "code", rows: 12 })),
          field("Files it shows", linesInput(tool.show, (v) => { tool.show = v; dirty(); }), "one per line; globs work"))))));
}

async function showSettings() {
  const info = await api("/api/settings");
  let scope = info.project ? "project" : "shared";
  const status = h("span", { class: "status" });
  const body = h("div", { class: "panel-body form" });
  let layer;

  const draw = () => {
    layer = clone((scope === "project" ? info.project : info.shared) || {});
    layer.agents = layer.agents || {};
    const custom = Object.entries(layer.agents).filter(([n]) => n !== "claude" && n !== "codex").map(([name, cli]) => ({ name, command: cli.command || "" }));
    const argsField = (name) => textInput((layer.agents[name]?.args || []).join(" "), (v) => {
      const args = v.split(" ").filter(Boolean);
      if (args.length) layer.agents[name] = { args };
      else delete layer.agents[name];
      dirty();
    }, { class: "code", placeholder: name === "claude" ? "e.g. --model opus" : "e.g. --model gpt-5" });
    const names = () => [...new Set(["claude", "codex", ...lib.runners, ...custom.map((c) => c.name).filter(Boolean)])];
    const pick = selectBox([["", scope === "project" ? "not set here (use the shared default)" : "not set (claude)"], ...names().map((n) => [n, n])], layer.agent || "", (v) => { layer.agent = v || undefined; dirty(); });
    body.replaceChildren(
      field("Save these settings for", selectBox([["shared", "every project (~/.swarm/settings.json)"], ["project", "this project only (.swarm/settings.json)"]], scope, (v) => { scope = v; draw(); })),
      field("Default agent for agent steps", pick, `a step can override it with "Runs with"; now: ${info.default}`),
      h("div", { class: "subpanel" },
        h("h3", {}, "Built in"),
        h("div", { class: "used-by" }, ["claude", "codex"].map((n) => h("span", {}, info.installed[n] ? `✓ ${n} found at ${info.installed[n]}` : `✗ ${n} is not on PATH`))),
        field("Extra arguments for claude", argsField("claude"), "added to claude -p; the step's tool list still applies"),
        field("Extra arguments for codex", argsField("codex"), "added to codex exec; otherwise Codex uses your own config, sandbox and approvals")),
      h("div", { class: "subpanel" },
        h("h3", {}, "Your own agents"),
        h("span", { class: "hint" }, "A shell command run in the project folder. It gets the prompt on stdin (and in $SWARM_PROMPT_FILE) and must print the step's fields as one JSON object last."),
        rowsEditor(custom, [{ key: "name", label: "name, e.g. pi", mono: true }, { key: "command", label: "command, e.g. pi -p", mono: true }], (rows) => {
          for (const n of Object.keys(layer.agents)) if (n !== "claude" && n !== "codex") delete layer.agents[n];
          for (const row of rows) if (row.name) layer.agents[row.name] = { command: row.command };
          dirty();
        })));
  };
  const dirty = () => { status.className = "status"; status.textContent = "Unsaved"; };
  const save = async () => {
    if (!Object.keys(layer.agents).length) delete layer.agents;
    try {
      await api(`/api/items/settings.json?scope=${scope}`, { method: "PUT", body: JSON.stringify(layer, null, 2) });
      await loadLibrary();
      renderSide();
      status.className = "ok-text";
      status.textContent = "Saved";
      const fresh = await api("/api/settings");
      Object.assign(info, fresh);
    } catch (err) {
      status.className = "error-text";
      status.textContent = err.message;
    }
  };
  draw();
  main.replaceChildren(h("div", { class: "page" },
    h("div", { class: "eyebrow" }, "Settings"),
    h("h1", {}, "Which agent runs agent steps"),
    h("p", { class: "lead" }, "swarm drives agent CLIs in their headless mode: Claude Code, Codex, or one you add. Project settings override the shared ones."),
    h("div", { class: "panel" },
      h("div", { class: "panel-head" }, h("span", { class: "grow" }, "settings.json"), h("div", { class: "savebar" }, status, h("button", { class: "btn small primary", onclick: save }, "Save"))),
      body)));
}

async function showMemory() {
  const res = await api("/api/memory");
  let text = res.text;
  const status = h("span", { class: "status" });
  const save = async () => {
    try {
      await send("PUT", "/api/memory", { text });
      status.className = "ok-text";
      status.textContent = "Saved";
    } catch (err) { alert(err.message); }
  };
  main.replaceChildren(h("div", { class: "page" },
    h("div", { class: "eyebrow" }, `Memory · ${res.path}`),
    h("h1", {}, "What this folder has learned"),
    h("p", { class: "lead" }, "Agents read this before they start (write {{memory}} in a prompt). The fix chain's last steps add what a run learned. It lives in the project folder, so it is shared with everyone who works on it once you commit it."),
    h("div", { class: "panel" },
      h("div", { class: "panel-head" }, h("span", { class: "grow" }, "memory.md"), h("div", { class: "savebar" }, status, h("button", { class: "btn small primary", onclick: save }, "Save"))),
      h("div", { class: "panel-body" }, textArea(text, (v) => { text = v; status.className = "status"; status.textContent = "Unsaved"; }, { class: "code prompt", placeholder: "Nothing remembered yet. Facts here reach every agent in this folder." })))));
}

function showPrompt(name) {
  let text = lib.prompts[name]?.text ?? "";
  const source = lib.prompts[name]?.source;
  const status = h("span", { class: "status" });
  const placeholders = [...new Set([...text.matchAll(/\{\{\s*([a-z][a-z0-9_.]*)\s*\}\}/g)].map((m) => m[1]))];
  main.replaceChildren(h("div", { class: "page" },
    h("div", { class: "eyebrow" }, `Prompt · ${FROM[source]} · prompts/${name}.md`),
    h("h1", {}, name),
    h("p", { class: "lead" }, "What an agent step tells Claude. {{placeholders}} are filled from the chain before the agent starts."),
    h("div", { class: "split" },
      h("div", { class: "panel" },
        h("div", { class: "panel-head" }, "Used by"), h("div", { class: "panel-body used-by" }, usedBy((s) => s.agent === name)),
        h("div", { class: "panel-head" }, "Placeholders it reads"), h("div", { class: "panel-body used-by" }, placeholders.length ? placeholders.map((p) => h("code", {}, `{{${p}}}`)) : h("span", { class: "hint" }, "none"))),
      h("div", { class: "panel" },
        h("div", { class: "panel-head" }, h("span", { class: "grow" }, "Text"), itemButtons("prompts", name, source, status, () => text)),
        h("div", { class: "panel-body" }, textArea(text, (v) => { text = v; status.className = "status"; status.textContent = "Unsaved"; }, { class: "code prompt" }))))));
}

/* run view */

function showRun(id) {
  main.replaceChildren(h("div", { class: "page" }, h("div", { class: "empty" }, "Loading…")));
  runView = { id, entries: new Map(), status: null };
  stream = new EventSource(`/api/runs/${encodeURIComponent(id)}/events`);
  stream.onmessage = (event) => renderRun(JSON.parse(event.data));
}

function renderRun(run) {
  let page = main.querySelector(".page[data-run]");
  if (!page || page.dataset.run !== run.id) {
    page = h("div", { class: "page", "data-run": run.id },
      h("div", { class: "eyebrow" }, h("a", { href: `#/chain/${run.chain}` }, run.title)),
      h("h1", { id: "subject" }),
      h("div", { class: "run-meta", id: "meta" }),
      h("div", { id: "banner" }),
      h("div", { class: "split" },
        h("div", { class: "panel sticky" }, h("div", { class: "panel-head" }, "Tree"), h("div", { class: "tree", id: "run-tree" })),
        h("div", { class: "timeline", id: "steps" })));
    main.replaceChildren(page);
    runView.entries = new Map();
  }
  const label = run.subject || Object.entries(run.inputs || {}).map(([k, v]) => `${k} ${v}`).join(", ") || run.title;
  page.querySelector("#subject").replaceChildren(run.link ? h("a", { href: run.link, target: "_blank", rel: "noreferrer" }, label) : label);

  const live = run.status === "running" || run.status === "waiting";
  page.querySelector("#meta").replaceChildren(...[
    h("span", { class: "pill" }, h("span", { class: `dot ${run.status}` }), STATUS_LABEL[run.status] || run.status),
    h("span", {}, `started ${new Date(run.started_at).toLocaleTimeString()}`),
    h("span", { class: "sep" }, "·"),
    h("span", { "data-since": run.started_at, "data-until": run.ended_at || "" }, duration(run.started_at, run.ended_at)),
    run.cost ? h("span", { class: "sep" }, "·") : null,
    run.cost ? h("span", {}, money(run.cost)) : null,
    live ? h("button", { class: "btn danger", style: "margin-left:auto", onclick: () => stopRun(run.id) }, "Stop run") : null,
  ].filter(Boolean));

  const banner = page.querySelector("#banner");
  if (!live && run.reason) banner.replaceChildren(h("div", { class: `banner ${run.status}` }, h("strong", {}, STATUS_LABEL[run.status] || run.status), run.reason));
  else if (!live && run.status === "done") banner.replaceChildren(h("div", { class: "banner done" }, h("strong", {}, "Done"), "Every step finished."));
  else banner.replaceChildren();

  page.querySelector("#run-tree").replaceChildren(
    treeRow({ kind: "root", title: run.title, state: run.status, time: duration(run.started_at, run.ended_at), onclick: () => window.scrollTo({ top: 0, behavior: "smooth" }) }),
    runTreeRows(run.steps));
  syncCards(page.querySelector("#steps"), run.steps, run);

  if (runView.status !== run.status) {
    runView.status = run.status;
    loadRuns();
  }
}

function runTreeRows(steps) {
  return h("div", { class: "tree-kids" }, steps.map((step) => [
    treeRow({
      kind: step.kind, title: step.title, state: step.status,
      time: step.status === "running" ? "" : step.started_at ? duration(step.started_at, step.ended_at) : "",
      onclick: () => focusCard(keyOf(step)),
    }),
    step.children?.length ? runTreeRows(step.children) : null,
  ]));
}

const keyOf = (step) => step.path || step.id;

function focusCard(path) {
  const card = document.getElementById(`card-${path}`);
  if (!card) return;
  card.scrollIntoView({ behavior: "smooth", block: "start" });
  card.classList.add("flash");
  setTimeout(() => card.classList.remove("flash"), 1200);
}

function syncCards(container, steps, run) {
  steps.forEach((step, i) => {
    let entry = runView.entries.get(keyOf(step));
    if (!entry) {
      entry = { el: h("div"), sig: null, artSig: null, arts: h("div", { class: "artifacts" }), kids: h("div", { class: "timeline" }) };
      runView.entries.set(keyOf(step), entry);
      container.append(entry.el);
    }
    const artSig = JSON.stringify(step.artifacts || []);
    if (entry.artSig !== artSig) {
      entry.arts.replaceChildren(...renderArtifacts(run.id, step.artifacts || []));
      entry.artSig = artSig;
    }
    if (entry.call?.log !== step.log) entry.call = step.log ? callPanel(run.id, step.log) : null;
    if (entry.earlierCount !== (step.earlier?.length || 0)) {
      entry.earlierCount = step.earlier?.length || 0;
      entry.earlier = entry.earlierCount ? earlierPanel(run, step.earlier) : null;
    }
    if (step.children?.length) syncCards(entry.kids, step.children, run);
    const sig = JSON.stringify({ ...step, artifacts: undefined, children: undefined, earlier: undefined, cost: step.kind === "chain" ? undefined : step.cost });
    if (entry.sig !== sig) {
      const fresh = stepCard(run, step, i, entry);
      entry.el.replaceWith(fresh);
      entry.el = fresh;
      entry.sig = sig;
      if (entry.call?.el.open) entry.call.load();
    }
  });
}

function stepFields(step) {
  const fields = Object.entries(step.output || {}).filter(([k, v]) => k !== "summary" && v !== "" && !(step.kind === "tool" && k === "output" && Object.keys(step.output).length > 1));
  if (!fields.length || step.kind === "approval") return null;
  return h("details", { class: "output" }, h("summary", {}, step.kind === "agent" ? "What the agent returned" : "Output"),
    h("dl", { class: "fields" }, fields.flatMap(([k, v]) => [h("dt", {}, k), h("dd", {}, v)])));
}

function earlierPanel(run, earlier) {
  return h("details", { class: "output earlier" }, h("summary", {}, `Earlier attempts (${earlier.length})`),
    earlier.map((step, n) => h("div", { class: "attempt" },
      h("div", { class: "attempt-head" }, `Attempt ${n + 1}`, h("span", { class: "card-stats" }, step.started_at ? duration(step.started_at, step.ended_at) : step.status)),
      step.headline ? h("div", { class: "headline" }, step.headline) : null,
      step.error ? h("div", { class: "error-box" }, step.error) : null,
      stepFields(step),
      step.log ? callPanel(run.id, step.log).el : null)));
}

const sizeOf = (text) => (text.length < 1024 ? `${text.length} chars` : `${(text.length / 1024).toFixed(1)} KB`);
const shellWord = (s) => (/^[\w@%+=:,./-]+$/.test(s) ? s : `'${s.replaceAll("'", `'\\''`)}'`);

function elapsed(from, to) {
  if (!from) return "";
  const ms = (to ? new Date(to) : new Date()) - new Date(from);
  if (ms < 1000) return `${Math.max(0, ms)}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return duration(from, to);
}

function remembered(el, key, opened) {
  el.open = opened.get(key) ?? el.open;
  el.addEventListener("toggle", () => opened.set(key, el.open));
  return el;
}

function block(label, text, opened, always = false) {
  if (!text && !always) return null;
  if (!text) return h("div", { class: "block-empty" }, label, h("span", { class: "hint" }, " printed nothing"));
  return remembered(h("details", { class: "block", open: text.length < 600 },
    h("summary", {}, label, h("span", { class: "hint" }, ` ${sizeOf(text)}`)), h("pre", {}, text)), label, opened);
}

function actionLabel(action) {
  let input = action.input;
  try {
    const parsed = JSON.parse(input);
    input = ["command", "file_path", "path", "pattern", "url", "query", "description", "prompt"].map((k) => parsed?.[k]).find((v) => typeof v === "string") ?? JSON.stringify(parsed);
  } catch {}
  return input.split("\n").find((line) => line.trim()) || "";
}

function callView(runID, log, call, opened) {
  const ended = call.ended_at;
  const exit = call.exit_code === undefined ? null : call.exit_code === 0 ? "exit 0" : call.exit_code === -1 ? "killed" : `exit ${call.exit_code}`;
  const long = [];
  const words = call.command.map((arg, i) => {
    if (arg.length <= 160 && !arg.includes("\n")) return shellWord(arg);
    long.push([i, arg]);
    return `<argument ${i}>`;
  });
  const out = [
    h("div", { class: "call-meta" },
      h("span", {}, `started ${new Date(call.started_at).toLocaleTimeString()}`),
      h("span", {}, ended ? `ran ${elapsed(call.started_at, ended)}` : "still running"),
      exit ? h("span", { class: call.exit_code === 0 ? "good" : "bad" }, exit) : null,
      h("span", { class: "hint" }, `in ${call.dir}`)),
    h("pre", { class: "command" }, `$ ${words.join(" ")}${call.stdin ? " < stdin" : ""}`),
    long.map(([i, arg]) => block(`argument ${i}`, arg, opened)),
    block("stdin", call.stdin, opened),
  ];
  if (call.error) out.push(h("div", { class: "error-box" }, call.error));
  if (call.actions?.length) {
    out.push(h("div", { class: "actions-head" }, `Tool calls (${call.actions.length})`),
      h("div", { class: "actions" }, call.actions.map((a, n) => remembered(
        h("details", { class: `action ${a.failed ? "failed" : ""}` },
          h("summary", {},
            h("span", { class: "action-tool" }, a.tool),
            h("span", { class: "action-input" }, actionLabel(a)),
            h("span", { class: "action-time" }, a.ended_at ? elapsed(a.started_at, a.ended_at) : "running")),
          h("div", { class: "action-body" }, h("div", { class: "hint" }, "input"), h("pre", {}, a.input),
            a.ended_at ? [h("div", { class: "hint" }, a.failed ? "output (failed)" : "output"), h("pre", {}, a.output || "(empty)")] : null)),
        `action ${a.id || n}`, opened))));
  }
  out.push(block("result", call.result, opened), block("stdout", call.stdout, opened, !call.stream && !!ended), block("stderr", call.stderr, opened, !!ended));
  if (call.stream) {
    const pre = h("pre", {}, "Loading…");
    const el = remembered(h("details", { class: "block" }, h("summary", {}, call.result ? "full output stream" : "stdout"), pre), "stream", opened);
    const fill = () => fetch(`/api/runs/${encodeURIComponent(runID)}/call?log=${encodeURIComponent(log)}&part=stream`).then((r) => r.text()).then((t) => { pre.textContent = t || "(empty)"; });
    el.addEventListener("toggle", () => { if (el.open) fill(); });
    if (el.open) fill();
    out.push(el);
  }
  return out;
}

function callPanel(runID, log) {
  const opened = new Map();
  const body = h("div", { class: "call-body" }, "Loading…");
  const el = h("details", { class: "output call" }, h("summary", {}, "How it was called"), body);
  const load = async () => {
    try {
      body.replaceChildren(...callView(runID, log, await api(`/api/runs/${encodeURIComponent(runID)}/call?log=${encodeURIComponent(log)}`), opened).flat().filter(Boolean));
    } catch (err) {
      body.replaceChildren(h("p", { class: "hint" }, err.message));
    }
  };
  el.addEventListener("toggle", () => { if (el.open) load(); });
  return { log, el, load };
}

function stepCard(run, step, i, entry) {
  const stats = [];
  if (step.started_at && step.status !== "running") stats.push(duration(step.started_at, step.ended_at));
  if (step.cost && step.kind !== "chain") stats.push(money(step.cost));
  else if (step.tokens) stats.push(`${(step.tokens / 1000).toFixed(step.tokens < 10000 ? 1 : 0)}k tokens`);
  const body = [];
  if (step.headline) body.push(h("div", { class: "headline" }, step.headline));
  else if (step.status === "pending" && step.kind !== "approval" && step.kind !== "chain") body.push(h("div", { class: "rule" }, step.kind === "agent" ? `prompt prompts/${step.uses}.md` : `tool ${step.uses}`));
  if (step.status === "running" && step.kind !== "chain") {
    body.push(h("div", { class: "working" }, h("span", { class: "bars" }, h("i"), h("i"), h("i")),
      `${step.kind === "agent" ? `Working · ${step.actions || 0} actions` : "Running"} · `, h("span", { "data-since": step.started_at }, duration(step.started_at))));
  }
  if (step.error) body.push(h("div", { class: "error-box" }, step.error));
  if (step.ended) body.push(h("div", { class: "ended-box" }, h("strong", {}, "Ended the run: "), step.ended));
  if (step.status === "waiting") body.push(h("div", { class: "decide" }, h("div", { class: "question" }, step.question)));
  body.push(entry.arts);
  if (step.status === "waiting") body.push(decideForm(run.id, step));
  body.push(stepFields(step));
  if (entry.call) body.push(entry.call.el);
  if (entry.earlier) body.push(entry.earlier);
  if (step.kind === "chain") body.push(entry.kids);
  return h("div", { class: `card ${step.status} ${step.kind}`, id: `card-${keyOf(step)}` },
    h("div", { class: "card-head" },
      h("span", { class: `state ${step.status}` }, STATE_ICON[step.status] || ""),
      h("span", { class: "card-title" }, step.title),
      h("span", { class: `chip ${step.kind}` }, step.runner ? `${KIND_NAME[step.kind]} · ${step.runner}` : KIND_NAME[step.kind]),
      step.round ? h("span", { class: "chip flag" }, `attempt ${step.round + 1}`) : null,
      step.always ? h("span", { class: "chip flag" }, "always runs") : null,
      h("span", { class: "card-stats" }, stats.join(" · "))),
    ...body);
}

function decideForm(runID, step) {
  const note = h("textarea", { placeholder: step.revise ? "Note: what to change, if you send it back" : "Optional note" });
  const decide = async (choice, button) => {
    if (choice === "revise" && !note.value.trim()) {
      note.placeholder = "Say what to change, then send it back";
      note.focus();
      return;
    }
    button.disabled = true;
    try { await send("POST", `/api/runs/${encodeURIComponent(runID)}/decide`, { choice, note: note.value }); } catch (err) { alert(err.message); button.disabled = false; }
  };
  return h("div", { class: "decide" }, note, h("div", { class: "row" },
    h("button", { class: "btn approve", onclick: (e) => decide("approve", e.target) }, "Approve"),
    step.revise ? h("button", { class: "btn", onclick: (e) => decide("revise", e.target) }, `Revise: back to ${step.revise}`) : null,
    h("button", { class: "btn", onclick: (e) => decide("reject", e.target) }, "Reject")));
}

function renderArtifacts(runID, artifacts) {
  const out = [];
  for (const a of artifacts.filter((x) => x.kind === "video")) {
    out.push(h("figure", { class: "video" },
      h("video", { src: `${fileURL(runID, a.path)}#t=1`, controls: true, preload: "metadata", playsinline: true }),
      h("figcaption", { class: "caption" }, a.name)));
  }
  const images = artifacts.filter((x) => x.kind === "image");
  if (images.length) {
    const urls = images.map((a) => fileURL(runID, a.path));
    out.push(h("div", { class: "stills" }, images.map((a, i) =>
      h("figure", { class: "still", onclick: () => openLightbox(urls, i) }, h("img", { src: urls[i], loading: "lazy", alt: a.name }), h("figcaption", {}, a.name)))));
  }
  for (const a of artifacts.filter((x) => x.kind !== "video" && x.kind !== "image")) {
    const body = h("div", { class: "doc-body" }, "Loading…");
    const doc = h("div", { class: "doc" }, h("div", { class: "doc-head" }, a.name), body);
    const more = h("button", { class: "doc-more", onclick: () => { doc.classList.toggle("open"); more.textContent = doc.classList.contains("open") ? "Show less" : "Show all"; } }, "Show all");
    doc.append(more);
    fetch(fileURL(runID, a.path, a.kind === "markdown" ? "doc" : "file")).then((r) => r.text()).then((text) => {
      if (a.kind === "markdown") body.innerHTML = text;
      else if (a.kind === "diff") body.replaceChildren(diffView(text));
      else body.replaceChildren(h("pre", { class: "plain" }, a.kind === "json" ? prettyJSON(text) : text));
      requestAnimationFrame(() => { if (body.scrollHeight <= body.clientHeight + 4) more.remove(); });
    });
    out.push(doc);
  }
  return out;
}

function diffView(text) {
  if (!text.trim()) return h("p", { class: "hint" }, "No changes.");
  return h("pre", { class: "diff" }, text.split("\n").map((line) => {
    const kind = line.startsWith("+++") || line.startsWith("---") || line.startsWith("diff ") || line.startsWith("index ") ? "meta"
      : line.startsWith("@@") ? "hunk" : line.startsWith("+") ? "add" : line.startsWith("-") ? "del" : "";
    return h("span", { class: kind }, `${line}\n`);
  }));
}

function prettyJSON(text) {
  try { return JSON.stringify(JSON.parse(text), null, 2); } catch { return text; }
}

let lightboxState = null;
function openLightbox(urls, index) {
  lightboxState = { urls, index };
  lightbox.querySelector("img").src = urls[index];
  lightbox.hidden = false;
}
lightbox.addEventListener("click", () => { lightbox.hidden = true; });
document.addEventListener("keydown", (e) => {
  if (lightbox.hidden || !lightboxState) return;
  if (e.key === "Escape") lightbox.hidden = true;
  if (e.key === "ArrowRight" || e.key === "ArrowLeft") {
    const n = lightboxState.urls.length;
    lightboxState.index = (lightboxState.index + (e.key === "ArrowRight" ? 1 : n - 1)) % n;
    lightbox.querySelector("img").src = lightboxState.urls[lightboxState.index];
  }
});

async function stopRun(id) {
  if (!confirm("Stop this run? Steps marked \"always runs\" still run.")) return;
  try { await api(`/api/runs/${encodeURIComponent(id)}/stop`, { method: "POST" }); } catch (err) { alert(err.message); }
}

setInterval(() => {
  for (const el of document.querySelectorAll("[data-since]")) {
    if (!el.dataset.until) el.textContent = duration(el.dataset.since);
  }
}, 1000);
setInterval(() => loadRuns().catch(() => {}), 5000);
window.addEventListener("beforeunload", (e) => { if (editor?.dirty) e.preventDefault(); });
window.addEventListener("hashchange", refresh);
loadProjects().then(loadRuns).catch(() => {}).finally(refresh);
