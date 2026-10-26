function showChain(name, stepID = "") {
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
  const at = draft.steps.findIndex((s) => s.id === stepID);
  editor = {
    name, source: chain.source, draft, selected: at >= 0 ? at : "root", tools: clone(lib.tools), prompts, dirtyTools: new Set(), dirtyPrompts: new Set(),
    fresh: new Set(), dirty: false, problems: chain.problems || [], warnings: chain.warnings || [], message: "",
  };
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
    : editor.dirty ? ""
      : editor.problems.length ? h("ul", { class: "problems" }, editor.problems.map((p) => h("li", {}, p)))
        : editor.warnings.length ? h("div", { class: "problems warnings" }, h("strong", {}, "This chain runs, but these look wrong:"), h("ul", {}, editor.warnings.map((w) => h("li", {}, w)))) : "";
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

function treeRow({ kind, title, id, selected, readonly, onclick, state, time }) {
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
    ...[["tool", "Tool"], ["agent", "Agent"], ["user", "User"], ["chain", "Sub-chain"]].map(([kind, label]) =>
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

function slugOf(title) {
  const slug = title.toLowerCase().replace(/[^a-z0-9]+/g, "_").slice(0, 32).replace(/^_+|_+$/g, "");
  return /^[a-z]/.test(slug) ? slug : `step_${slug}`.replace(/_+$/, "");
}

function uniqueID(base, self) {
  const ids = new Set(editor.draft.steps.filter((s) => s !== self).map((s) => s.id));
  if (!ids.has(base)) return base;
  for (let n = 2; ; n++) if (!ids.has(`${base}_${n}`)) return `${base}_${n}`;
}

function addStep(kind) {
  const otherChains = lib.chains.map((c) => c.name).filter((n) => n !== editor.name);
  const step = {
    tool: { title: "New tool step", tool: Object.keys(editor.tools).sort()[0] || "" },
    agent: { title: "New agent step", agent: Object.keys(editor.prompts).sort()[0] || "", tools: ["Read", "Grep", "Glob"] },
    user: { title: "Your approval", user: "Continue?" },
    chain: { title: "Sub-chain", chain: otherChains[0] || "" },
  }[kind];
  step.id = uniqueID(slugOf(step.title));
  editor.fresh.add(step);
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
    field("Inputs", rowsEditor(d.inputs, [{ key: "name", label: "name", mono: true }, { key: "label", label: "label" }, { key: "default", label: "default" }, { key: "optional", label: "optional", check: true }], () => touch()),
      "asked for when a run starts; a run needs every input that is not optional and has no default"),
    field("Vars", rowsEditor(d.vars, [{ key: "name", label: "name", mono: true }, { key: "value", label: "value, may use {{placeholders}}", mono: true }], () => touch()), "computed once at the start"));
}
