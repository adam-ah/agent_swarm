let nextNotice = "";

function showEditor(name, stepID = "") {
  const chain = lib.chains.find((c) => c.name === name);
  if (!chain) {
    main.replaceChildren(h("div", { class: "page" }, h("div", { class: "empty" }, `There is no chain named ${name} in this project.`)));
    return;
  }
  const draft = clone(chain);
  for (const key of ["name", "problems", "warnings", "source", "prompts"]) delete draft[key];
  draft.steps = draft.steps || [];
  const prompts = Object.fromEntries(Object.entries(chain.prompts || {}).map(([k, v]) => [k, v.text]));
  const at = draft.steps.findIndex((s) => s.id === stepID);
  editor = {
    name, source: chain.source, draft, selected: at >= 0 ? at : "root", tools: clone(lib.tools), prompts, dirtyTools: new Set(), dirtyPrompts: new Set(),
    fresh: new Set(), openShared: new Set(), dirty: false, problems: chain.problems || [], warnings: chain.warnings || [], message: nextNotice,
  };
  nextNotice = "";
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
  const mine = editor;
  checkLater(editorDraft(), (verdicts) => {
    if (mine !== editor) return;
    editor.verdict = draftVerdict(verdicts);
    editor.refused = undefined;
    renderSaveBar();
  });
}

function cleanDraft() {
  const draft = clone(editor.draft);
  for (const step of draft.steps) {
    if (step.with && !Object.keys(step.with).length) delete step.with;
  }
  return draft;
}

function editorDraft() {
  return {
    chains: { [editor.name]: cleanDraft() },
    prompts: Object.fromEntries([...editor.dirtyPrompts].map((n) => [`${editor.name}/${n}`, editor.prompts[n]])),
    tools: Object.fromEntries([...editor.dirtyTools].map((n) => [n, { ...editor.tools[n], source: undefined }])),
  };
}

function draftVerdict(verdicts) {
  const own = `chains/${editor.name}.json`;
  const out = { problems: [], warnings: [] };
  for (const [path, v] of Object.entries(verdicts)) {
    const tag = (text) => (path === own ? text : `${path}: ${text}`);
    out.problems.push(...(v.problems || []).map(tag));
    out.warnings.push(...(v.warnings || []).map(tag));
  }
  return out;
}

function renderSaveBar() {
  const shared = editor.source !== "project";
  const draft = editor.dirty && editor.verdict ? chainProblems(editor.verdict.problems, editor.verdict.warnings, true) : "";
  document.getElementById("problems").replaceChildren(editor.refused?.length
    ? h("div", { class: "problems" }, h("strong", {}, "Not saved. swarm refused it because:"), h("ul", {}, editor.refused.map((p) => h("li", {}, p))))
    : editor.dirty ? draft : chainProblems(editor.problems, editor.warnings) || "");
  const status = editor.message ? h("span", { class: "status ok-text" }, editor.message)
    : editor.dirty ? h("span", { class: "status" }, shared ? "Unsaved. Saving changes it for every project." : "Unsaved changes") : null;
  document.getElementById("savebar").replaceChildren(
    h("span", { class: "editbar-title" }, "Edit the chain"),
    h("span", { class: "grow" }),
    status || "",
    h("details", { class: "more" }, h("summary", { class: "btn small" }, "More"),
      h("div", { class: "more-menu" },
        shared ? h("button", { class: "linkish", type: "button", onclick: () => saveChain("project") }, "Save as a copy for this project only") : null,
        transferControls(),
        h("button", { class: "linkish danger", type: "button", onclick: removeChain }, shared ? "Delete this chain for every project" : "Delete this project's copy"))),
    h("button", { class: "btn small", disabled: !editor.dirty, onclick: () => showEditor(editor.name, editor.draft.steps[editor.selected]?.id) }, "Discard"),
    h("button", { class: "btn small primary", disabled: !editor.dirty, onclick: () => saveChain() }, "Save"));
}

function transferControls() {
  const others = projects.filter((p) => p !== project);
  if (!others.length) return h("span", { class: "hint" }, "Add another project to copy or move this chain to it.");
  const target = selectBox(others.map((p) => [p, folderName(p)]), others[0], () => {});
  target.title = "The project to copy or move it to";
  return h("div", { class: "more-transfer" },
    h("span", { class: "hint" }, "To another project, with the tools, prompts and chains it uses from this one:"),
    target,
    h("div", { class: "row" },
      h("button", { class: "linkish", type: "button", onclick: () => transferChain(target.value, false) }, "Copy to project"),
      h("button", { class: "linkish", type: "button", onclick: () => transferChain(target.value, true) }, "Move to project")));
}

async function transferChain(to, move) {
  if (editor.dirty) {
    alert("Save or discard your changes first.");
    return;
  }
  const there = folderName(to);
  const leaves = editor.source === "project" ? "this project" : "the shared library, so every other project loses it too";
  if (move && !confirm(`Move "${editor.draft.title || editor.name}" to ${there}? It leaves ${leaves}.`)) return;
  let done;
  try {
    done = await send("POST", `/api/chains/${encodeURIComponent(editor.name)}/transfer`, { to, move });
  } catch (err) {
    alert(err.message);
    return;
  }
  const said = (label, paths) => (paths.length ? `${label}: ${paths.join(", ")}` : "");
  const report = [said(`Copied to ${there}`, done.copied), said(`Already the same in ${there}`, done.same),
    said("Removed from here", done.removed), said("Left here because other chains use them", done.kept)].filter(Boolean).join(". ");
  if (!move) {
    editor.message = report;
    renderSaveBar();
    return;
  }
  nextNotice = report;
  switchProject(to, `#/chain/${encodeURIComponent(editor.name)}`);
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
      await putItem("prompts", `${editor.name}/${name}`, editor.prompts[name], scope);
      editor.dirtyPrompts.delete(name);
    }
    await putItem("chains", editor.name, cleanDraft(), scope);
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
        const lists = step.chain ? listsPassed(step, d.steps, editor.name) : [];
        return [
          treeRow({ kind: kindOf(step), title: step.title, id: step.id, selected: editor.selected === i, onclick: () => select(i) }),
          step.stop_if ? h("div", { class: "tree-note" }, `⤷ stops the run here if its ${describeCondition(step.stop_if)}`) : null,
          lists.length ? h("div", { class: "tree-note" }, `⤷ runs ${step.chain} once per item of ${lists.join(", ")}`) : null,
          step.resume ? h("div", { class: "tree-note" }, `⤷ continues the session of ${d.steps.find((s) => s.id === step.resume)?.title || step.resume}`) : null,
          step.chain ? subchainRows(step.chain, 1) : null,
        ];
      },
    })));
  document.getElementById("tree-actions").replaceChildren(
    h("span", { class: "hint" }, typeof editor.selected === "number" ? "Add a step after the selected one:" : "Add a step at the end:"),
    h("div", { class: "row" }, [["tool", "Tool"], ["agent", "Agent"], ["user", "User"], ["chain", "Sub-chain"], ["repeat", "Repeat"]].map(([kind, label]) =>
      h("button", { class: `btn small add-kind ${kind}`, type: "button", disabled: kind === "repeat" && insertAt() === 0, onclick: () => addStep(kind) }, `+ ${label}`))));
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

const insertAt = () => (typeof editor.selected === "number" ? editor.selected + 1 : editor.draft.steps.length);

function addStep(kind) {
  const otherChains = lib.chains.map((c) => c.name).filter((n) => n !== editor.name);
  const at = insertAt();
  const step = {
    tool: { title: "New tool step", tool: Object.keys(editor.tools).sort()[0] || "" },
    agent: { title: "New agent step", agent: Object.keys(editor.prompts).sort()[0] || "", tools: ["Read", "Grep", "Glob"] },
    user: { title: "Your approval", user: "Continue?" },
    chain: { title: "Sub-chain", chain: otherChains[0] || "" },
    repeat: { title: "Repeat", repeat: editor.draft.steps[at - 1]?.id },
  }[kind];
  step.id = uniqueID(slugOf(step.title));
  editor.fresh.add(step);
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
  const gone = steps[editor.selected];
  const after = steps.slice(editor.selected + 1);
  const rejectedOnly = kindOf(gone) === "user" ? after.filter((s) => s.always && notApproved(s.when, gone.id)) : [];
  const next = after.find((s) => !rejectedOnly.includes(s));
  const drop = [...rejectedOnly, ...after.filter((s) => s.repeat === gone.id && s === next)];
  const also = drop.length ? ` ${drop.map((s) => `"${s.title}"`).join(" and ")} only make${drop.length > 1 ? "" : "s"} sense with it, so ${drop.length > 1 ? "they go" : "it goes"} too.` : "";
  if (!confirm(`Delete the step "${gone.title}"?${also}`)) return;
  editor.draft.steps = steps.filter((s) => s !== gone && !drop.includes(s));
  forget(editor.draft.steps, gone, after.find((s) => !drop.includes(s)));
  editor.selected = editor.draft.steps.length ? Math.min(editor.selected, editor.draft.steps.length - 1) : "root";
  touch(true);
}

function forget(steps, gone, next) {
  const about = (cond) => (readCondition(cond)?.name || "").startsWith(`${gone.id}.`);
  for (const s of steps) {
    if (s.repeat === gone.id) s.repeat = next.id;
    if (s.revise === gone.id && next && steps.indexOf(next) < steps.indexOf(s)) s.revise = next.id;
    else if (s.revise === gone.id) delete s.revise;
    if (s.resume === gone.id && gone.resume) s.resume = gone.resume;
    else if (s.resume === gone.id) delete s.resume;
    if (s.keep_until === gone.id && next) s.keep_until = next.id;
    else if (s.keep_until === gone.id) delete s.keep_until;
    if (about(s.when)) delete s.when;
    if (about(s.stop_if)) { delete s.stop_if; delete s.stop; }
  }
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
