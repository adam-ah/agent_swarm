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
  editor = { name, source: chain.source, draft, selected: at >= 0 ? at : "root", tools: clone(lib.tools), prompts, dirtyTools: new Set(), dirtyPrompts: new Set(), dirty: false, problems: chain.problems || [], message: "" };
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
      h("label", { class: `check${step.always ? " on" : ""}` },
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
