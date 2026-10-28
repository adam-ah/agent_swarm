function showEditor(name, stepID = "") {
  const chain = lib.chains.find((c) => c.name === name);
  if (!chain) {
    main.replaceChildren(h("div", { class: "page" }, h("div", { class: "empty" }, `There is no chain named ${name} in this project.`)));
    return;
  }
  const draft = clone(chain);
  for (const key of ["name", "problems", "warnings", "source"]) delete draft[key];
  draft.steps = draft.steps || [];
  const prompts = Object.fromEntries(Object.entries(lib.prompts).map(([k, v]) => [k, v.text]));
  const at = draft.steps.findIndex((s) => s.id === stepID);
  editor = {
    name, source: chain.source, draft, selected: at >= 0 ? at : "root", tools: clone(lib.tools), prompts, dirtyTools: new Set(), dirtyPrompts: new Set(),
    fresh: new Set(), openShared: new Set(), dirty: false, problems: chain.problems || [], warnings: chain.warnings || [], message: "",
  };
  main.replaceChildren(h("div", { class: "page" },
    h("div", { class: "eyebrow" }, `Chain · chains/${name}.json · ${FROM[chain.source]}`),
    h("h1", { id: "chain-title" }, draft.title),
    chain.description ? h("p", { class: "lead" }, chain.description) : null,
    startForm(chain),
    h("div", { class: "editbar", id: "savebar" }),
    h("div", { id: "problems" }),
    h("div", { class: "split" },
      h("div", { class: "panel sticky" },
        h("div", { class: "panel-head" }, "Steps"),
        h("div", { class: "tree", id: "tree" }),
        h("div", { class: "tree-actions", id: "tree-actions" })),
      h("div", { class: "panel" }, h("div", { class: "panel-body", id: "inspector" })))));
  renderEditorTree();
  renderSaveBar();
  renderInspector();
}

function touch(redrawInspector = false) {
  editor.dirty = true;
  editor.message = "";
  renderEditorTree();
  renderSaveBar();
  if (redrawInspector) renderInspector();
}

function renderSaveBar() {
  const shared = editor.source !== "project";
  document.getElementById("problems").replaceChildren(editor.refused?.length
    ? h("div", { class: "problems" }, h("strong", {}, "Not saved. swarm refused it because:"), h("ul", {}, editor.refused.map((p) => h("li", {}, p))))
    : editor.dirty ? "" : chainProblems(editor.problems, editor.warnings) || "");
  const status = editor.message ? h("span", { class: "status ok-text" }, editor.message)
    : editor.dirty ? h("span", { class: "status" }, shared ? "Unsaved. Saving changes it for every project." : "Unsaved changes") : null;
  document.getElementById("savebar").replaceChildren(
    h("span", { class: "editbar-title" }, "Edit the chain"),
    h("span", { class: "grow" }),
    status || "",
    h("details", { class: "more" }, h("summary", { class: "btn small" }, "More"),
      h("div", { class: "more-menu" },
        shared ? h("button", { class: "linkish", type: "button", onclick: () => saveChain("project") }, "Save as a copy for this project only") : null,
        h("button", { class: "linkish danger", type: "button", onclick: removeChain }, shared ? "Delete this chain for every project" : "Delete this project's copy"))),
    h("button", { class: "btn small", disabled: !editor.dirty, onclick: () => showEditor(editor.name, editor.draft.steps[editor.selected]?.id) }, "Discard"),
    h("button", { class: "btn small primary", disabled: !editor.dirty, onclick: () => saveChain() }, "Save"));
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
  if (location.hash === "#/") refresh();
  else location.hash = "#/";
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
  const { selected, openShared } = editor;
  showEditor(editor.name);
  Object.assign(editor, { selected, openShared, message: scope ? "Saved as this project's copy" : "Saved" });
  renderEditorTree();
  renderSaveBar();
  renderInspector();
  renderSide();
}

function treeRow({ kind, title, id, selected, readonly, onclick }) {
  return h("div", { class: `tree-row ${selected ? "selected" : ""} ${readonly ? "readonly" : ""} ${kind === "root" ? "tree-root" : ""}`, onclick },
    h("span", { class: `kind ${kind}`, title: KIND_NAME[kind] || "chain" }, KIND_LETTER[kind]),
    h("span", { class: "title" }, title || "(untitled)"),
    id ? h("span", { class: "id" }, id) : null);
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
    treeRow({ kind: "root", title: "Title, inputs and values", selected: editor.selected === "root", onclick: () => select("root") }),
    h("div", { class: "tree-kids" }, renderOutline(outlineOf(d.steps), {
      chain: d,
      go: editorGo,
      step: (step) => {
        const i = d.steps.indexOf(step);
        const lists = step.chain ? listsPassed(step, d.steps) : [];
        return [
          treeRow({ kind: kindOf(step), title: step.title, id: step.id, selected: editor.selected === i, onclick: () => select(i) }),
          step.stop_if ? h("div", { class: "tree-note" }, `⤷ stops the run here if its ${describeCondition(step.stop_if)}`) : null,
          lists.length ? h("div", { class: "tree-note" }, `⤷ runs ${step.chain} once per item of ${lists.join(", ")}`) : null,
          step.chain ? subchainRows(step.chain, 1) : null,
        ];
      },
    })));
  document.getElementById("tree-actions").replaceChildren(
    h("span", { class: "hint" }, typeof editor.selected === "number" ? "Add a step after the selected one:" : "Add a step at the end:"),
    h("div", { class: "row" }, [["tool", "Tool"], ["agent", "Agent"], ["user", "User"], ["chain", "Sub-chain"]].map(([kind, label]) =>
      h("button", { class: `btn small add-kind ${kind}`, type: "button", onclick: () => addStep(kind) }, `+ ${label}`))));
}

const editorGo = (from) => select(from.kind === "step" ? editor.draft.steps.indexOf(from.step) : "root");

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
  panel.replaceChildren(stepInspector(editor.draft.steps[editor.selected]));
}

function rootInspector() {
  const d = editor.draft;
  d.inputs = d.inputs || [];
  d.vars = d.vars || [];
  return h("div", { class: "form" },
    h("h2", {}, "The chain"),
    field("Title", textInput(d.title, (v) => { d.title = v; touch(); })),
    field("Description", textArea(d.description, (v) => { d.description = v; touch(); }, { rows: 5 }), "what it does and when to use it"),
    field("Inputs", rowsEditor(d.inputs, [{ key: "name", label: "name", mono: true }, { key: "label", label: "question shown at the start" }, { key: "default", label: "default" }, { key: "optional", label: "optional", check: true }], () => touch()),
      "asked for when a run starts"),
    field("Values", rowsEditor(d.vars, [{ key: "name", label: "name", mono: true }, { key: "value", label: "value, may use {{placeholders}}", mono: true }], () => touch()), "worked out once at the start, for steps to read"),
    h("div", { class: "row2" },
      field("Run heading", textInput(d.subject, (v) => { d.subject = v; touch(); }, { class: "code", placeholder: "e.g. {{request}}" }), "shown at the top of each run"),
      field("Run link", textInput(d.link, (v) => { d.link = v; touch(); }, { class: "code", placeholder: "e.g. {{read_issue.url}}" }), "makes the heading a link")));
}
