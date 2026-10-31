let nextNotice = "";

function showEditor(name, stepID = "") {
  const chain = lib.chains.find((c) => c.name === name);
  if (!chain) {
    main.replaceChildren(h("div", { class: "page" }, h("div", { class: "empty" }, `There is no chain named ${name} in this project.`)));
    return;
  }
  editor = {
    chains: {}, tools: clone(lib.tools), dirtyTools: new Set(), openShared: new Set(), dirty: false,
    problems: chain.problems || [], warnings: chain.warnings || [], message: nextNotice,
  };
  editor.root = editor.at = chainDraft(name);
  const at = editor.root.draft.steps.findIndex((s) => s.id === stepID);
  editor.selected = at >= 0 ? at : "root";
  nextNotice = "";
  main.replaceChildren(h("div", { class: "page" },
    h("div", { class: "eyebrow" }, `Chain · chains/${name}.json · ${FROM[chain.source]}`),
    h("h1", { id: "chain-title" }, editor.root.draft.title),
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

function chainDraft(name) {
  if (editor.chains[name]) return editor.chains[name];
  const chain = lib.chains.find((c) => c.name === name);
  if (!chain) return null;
  const draft = clone(chain);
  for (const key of ["name", "problems", "warnings", "source", "prompts"]) delete draft[key];
  draft.steps = draft.steps || [];
  draft.inputs = draft.inputs || [];
  draft.vars = draft.vars || [];
  const prompts = Object.fromEntries(Object.entries(chain.prompts || {}).map(([k, v]) => [k, v.text]));
  const open = { name, source: chain.source, draft, prompts, dirtyPrompts: new Set(), fresh: new Set() };
  open.saved = JSON.stringify(cleanDraft(open));
  editor.chains[name] = open;
  return open;
}

const draftSteps = (chain) => editor.chains[chain.name]?.draft.steps || chain.steps || [];

function cleanDraft(open) {
  const draft = clone(open.draft);
  for (const step of draft.steps) {
    if (step.with && !Object.keys(step.with).length) delete step.with;
  }
  return draft;
}

const editedSubchains = () => Object.values(editor.chains)
  .filter((c) => c !== editor.root && (c.dirtyPrompts.size || JSON.stringify(cleanDraft(c)) !== c.saved));

function editorDraft() {
  const chains = [editor.root, ...editedSubchains()];
  return {
    chains: Object.fromEntries(chains.map((c) => [c.name, cleanDraft(c)])),
    prompts: Object.fromEntries(chains.flatMap((c) => [...c.dirtyPrompts].map((n) => [`${c.name}/${n}`, c.prompts[n]]))),
    tools: Object.fromEntries([...editor.dirtyTools].map((n) => [n, { ...editor.tools[n], source: undefined }])),
  };
}

function draftVerdict(verdicts) {
  const own = `chains/${editor.root.name}.json`;
  const out = { problems: [], warnings: [] };
  for (const [path, v] of Object.entries(verdicts)) {
    const tag = (text) => (path === own ? text : `${path}: ${text}`);
    out.problems.push(...(v.problems || []).map(tag));
    out.warnings.push(...(v.warnings || []).map(tag));
  }
  return out;
}

function renderSaveBar() {
  const shared = editor.root.source !== "project";
  const subs = editor.dirty ? editedSubchains() : [];
  const alsoSaves = subs.length ? ` Also saves ${subs.map((c) => `chains/${c.name}.json${c.source === "project" ? "" : " (shared)"}`).join("; ")}.` : "";
  const draft = editor.dirty && editor.verdict ? chainProblems(editor.verdict.problems, editor.verdict.warnings, true) : "";
  document.getElementById("problems").replaceChildren(editor.refused?.length
    ? h("div", { class: "problems" }, h("strong", {}, "Save refused:"), h("ul", {}, editor.refused.map((p) => h("li", {}, p))))
    : editor.dirty ? draft : chainProblems(editor.problems, editor.warnings) || "");
  const status = editor.message ? h("span", { class: "status ok-text" }, editor.message)
    : editor.dirty ? h("span", { class: "status" }, shared ? "Unsaved; shared chain, saving affects all projects." : "Unsaved changes", alsoSaves) : null;
  document.getElementById("savebar").replaceChildren(
    h("span", { class: "editbar-title" }, "Edit chain"),
    h("span", { class: "grow" }),
    status || "",
    h("details", { class: "more" }, h("summary", { class: "btn small" }, "More"),
      h("div", { class: "more-menu" },
        shared ? h("button", { class: "linkish", type: "button", onclick: () => saveChain("project") }, "Save as project copy") : null,
        transferControls(),
        h("button", { class: "linkish danger", type: "button", onclick: removeChain }, shared ? "Delete (all projects)" : "Delete project copy"))),
    h("button", { class: "btn small", disabled: !editor.dirty, onclick: () => reopen() }, "Discard"),
    h("button", { class: "btn small primary", disabled: !editor.dirty, onclick: () => saveChain() }, "Save"));
}

function transferControls() {
  const others = projects.filter((p) => p !== project);
  if (!others.length) return h("span", { class: "hint" }, "No other project to copy or move to.");
  const target = selectBox(others.map((p) => [p, folderName(p)]), others[0], () => {});
  target.title = "Target project";
  return h("div", { class: "more-transfer" },
    h("span", { class: "hint" }, "Copy or move to another project, with the tools, prompts and sub-chains it uses from this one:"),
    target,
    h("div", { class: "row" },
      h("button", { class: "linkish", type: "button", onclick: () => transferChain(target.value, false) }, "Copy to project"),
      h("button", { class: "linkish", type: "button", onclick: () => transferChain(target.value, true) }, "Move to project")));
}

async function transferChain(to, move) {
  if (editor.dirty) {
    alert("Save or discard changes first.");
    return;
  }
  const there = folderName(to);
  const leaves = editor.root.source === "project" ? "this project" : "the shared library and every other project";
  if (move && !confirm(`Move "${editor.root.draft.title || editor.root.name}" to ${there}? It is removed from ${leaves}.`)) return;
  let done;
  try {
    done = await send("POST", `/api/chains/${encodeURIComponent(editor.root.name)}/transfer`, { to, move });
  } catch (err) {
    alert(err.message);
    return;
  }
  const said = (label, paths) => (paths.length ? `${label}: ${paths.join(", ")}` : "");
  const report = [said(`Copied to ${there}`, done.copied), said(`Already the same in ${there}`, done.same),
    said("Removed here", done.removed), said("Kept here, used by other chains", done.kept)].filter(Boolean).join(". ");
  if (!move) {
    editor.message = report;
    renderSaveBar();
    return;
  }
  nextNotice = report;
  switchProject(to, `#/chain/${encodeURIComponent(editor.root.name)}`);
}

async function removeChain() {
  const where = editor.root.source === "project" ? "the project copy" : "the shared chain, for all projects";
  if (!confirm(`Delete chain "${editor.root.draft.title || editor.root.name}"? Removes ${where}.`)) return;
  try {
    await deleteItem("chains", editor.root.name);
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
    for (const open of [...editedSubchains(), editor.root]) {
      for (const name of open.dirtyPrompts) {
        await putItem("prompts", `${open.name}/${name}`, open.prompts[name], scope);
        open.dirtyPrompts.delete(name);
      }
      await putItem("chains", open.name, cleanDraft(open), scope);
      open.saved = JSON.stringify(cleanDraft(open));
    }
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
  reopen({ openShared: editor.openShared, message: scope ? "Saved as project copy" : "Saved" });
  renderSide();
}

function reopen(keep = {}) {
  const { at, selected } = editor;
  const id = at.draft.steps[selected]?.id;
  showEditor(editor.root.name);
  const back = chainDraft(at.name) || editor.root;
  const i = back.draft.steps.findIndex((s) => s.id === id);
  Object.assign(editor, keep, { at: back, selected: i >= 0 ? i : "root" });
  renderEditorTree();
  renderSaveBar();
  renderInspector();
}

function treeRow({ kind, title, id, selected, sub, onclick }) {
  return h("div", { class: `tree-row ${selected ? "selected" : ""} ${sub ? "sub" : ""} ${kind === "root" ? "tree-root" : ""}`, onclick },
    h("span", { class: `kind ${kind}`, title: KIND_NAME[kind] || "chain" }, KIND_LETTER[kind]),
    h("span", { class: "title" }, title || "(untitled)"),
    id ? h("span", { class: "id" }, id) : null);
}

function subchainRows(chainName, depth) {
  const open = depth > 4 ? null : chainDraft(chainName);
  if (!open) return null;
  return h("div", { class: "tree-kids subchain" }, renderOutline(outlineOf(open.draft.steps), {
    chain: open.draft,
    step: (s) => stepRows(open, s, depth),
  }));
}

function stepRows(open, step, depth) {
  const steps = open.draft.steps;
  const i = steps.indexOf(step);
  const lists = step.chain ? listsPassed(step, steps, open.name) : [];
  return [
    treeRow({ kind: kindOf(step), title: step.title, id: step.id, sub: depth > 0, selected: editor.at === open && editor.selected === i, onclick: () => select(i, open) }),
    step.stop_if ? h("div", { class: "tree-note" }, `⤷ stops the run here if its ${describeCondition(step.stop_if)}`) : null,
    lists.length ? h("div", { class: "tree-note" }, `⤷ runs ${step.chain} once per item of ${lists.join(", ")}${step.parallel ? `, in parallel (${step.max_parallel || MAX_PARALLEL} at most)` : ""}${step.keep_going ? ", keep going on failure" : ""}`) : null,
    step.resume ? h("div", { class: "tree-note" }, `⤷ continues the session of ${steps.find((s) => s.id === step.resume)?.title || step.resume}`) : null,
    step.chain ? subchainRows(step.chain, depth + 1) : null,
  ];
}

function renderEditorTree() {
  const { root } = editor;
  const d = root.draft;
  const into = editor.at === root ? "" : ` to ${editor.at.draft.title || editor.at.name},`;
  document.getElementById("chain-title").textContent = d.title || root.name;
  document.getElementById("tree").replaceChildren(
    treeRow({ kind: "root", title: "Title, inputs and values", selected: editor.at === root && editor.selected === "root", onclick: () => select("root", root) }),
    h("div", { class: "tree-kids" }, renderOutline(outlineOf(d.steps), {
      chain: d,
      go: goIn(root),
      step: (step) => stepRows(root, step, 0),
    })));
  document.getElementById("tree-actions").replaceChildren(
    h("span", { class: "hint" }, typeof editor.selected === "number" ? `Add step${into} after the selected one:` : `Add step${into} at the end:`),
    h("div", { class: "row" }, [["tool", "Tool"], ["agent", "Agent"], ["user", "User"], ["chain", "Sub-chain"], ["repeat", "Repeat"]].map(([kind, label]) =>
      h("button", { class: `btn small add-kind ${kind}`, type: "button", disabled: kind === "repeat" && insertAt() === 0, onclick: () => addStep(kind) }, `+ ${label}`))));
}

const goIn = (open) => (from) => select(from.kind === "step" ? open.draft.steps.indexOf(from.step) : "root", open);
const editorGo = (from) => goIn(editor.at)(from);

function select(i, open = editor.at) {
  editor.at = open;
  editor.selected = i;
  renderEditorTree();
  renderInspector();
}

function slugOf(title) {
  const slug = title.toLowerCase().replace(/[^a-z0-9]+/g, "_").slice(0, 32).replace(/^_+|_+$/g, "");
  return /^[a-z]/.test(slug) ? slug : `step_${slug}`.replace(/_+$/, "");
}

function uniqueID(base, self) {
  const ids = new Set(editor.at.draft.steps.filter((s) => s !== self).map((s) => s.id));
  if (!ids.has(base)) return base;
  for (let n = 2; ; n++) if (!ids.has(`${base}_${n}`)) return `${base}_${n}`;
}

const insertAt = () => (typeof editor.selected === "number" ? editor.selected + 1 : editor.at.draft.steps.length);

function addStep(kind) {
  const otherChains = lib.chains.map((c) => c.name).filter((n) => n !== editor.at.name);
  const at = insertAt();
  const step = {
    tool: { title: "New tool step", tool: Object.keys(editor.tools).sort()[0] || "" },
    agent: { title: "New agent step", agent: Object.keys(editor.at.prompts).sort()[0] || "", tools: ["Read", "Grep", "Glob"] },
    user: { title: "Your approval", user: "Continue?" },
    chain: { title: "Sub-chain", chain: otherChains[0] || "" },
    repeat: { title: "Repeat", repeat: editor.at.draft.steps[at - 1]?.id },
  }[kind];
  step.id = uniqueID(slugOf(step.title));
  editor.at.fresh.add(step);
  editor.at.draft.steps.splice(at, 0, step);
  editor.selected = at;
  touch(true);
}

function move(delta) {
  const steps = editor.at.draft.steps;
  const i = editor.selected;
  [steps[i], steps[i + delta]] = [steps[i + delta], steps[i]];
  editor.selected = i + delta;
  touch(true);
}

function removeStep() {
  const steps = editor.at.draft.steps;
  const gone = steps[editor.selected];
  const after = steps.slice(editor.selected + 1);
  const rejectedOnly = kindOf(gone) === "user" ? after.filter((s) => s.always && notApproved(s.when, gone.id)) : [];
  const next = after.find((s) => !rejectedOnly.includes(s));
  const drop = [...rejectedOnly, ...after.filter((s) => s.repeat === gone.id && s === next)];
  const also = drop.length ? ` Also deletes ${drop.map((s) => `"${s.title}"`).join(" and ")}, which depend${drop.length > 1 ? "" : "s"} on it.` : "";
  if (!confirm(`Delete step "${gone.title}"?${also}`)) return;
  editor.at.draft.steps = steps.filter((s) => s !== gone && !drop.includes(s));
  forget(editor.at.draft.steps, gone, after.find((s) => !drop.includes(s)));
  editor.selected = editor.at.draft.steps.length ? Math.min(editor.selected, editor.at.draft.steps.length - 1) : "root";
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
  panel.replaceChildren(subchainNote() || "", editor.selected === "root" ? rootInspector() : stepInspector(editor.at.draft.steps[editor.selected]));
}

function subchainNote() {
  const open = editor.at;
  if (open === editor.root) return null;
  const runBy = lib.chains.filter((c) => draftSteps(c).some((s) => s.chain === open.name)).map((c) => c.title || c.name);
  return h("div", { class: "subchain-note" },
    `Sub-chain ${open.draft.title || open.name} (chains/${open.name}.json, ${FROM[open.source]}). Saving affects every chain that uses it: ${runBy.join(", ")}. `,
    h("a", { href: `#/chain/${open.name}` }, `Open ${open.name} →`));
}

function rootInspector() {
  const d = editor.at.draft;
  return h("div", { class: "form" },
    h("h2", {}, "The chain"),
    field("Title", textInput(d.title, (v) => { d.title = v; touch(); })),
    field("Description", textArea(d.description, (v) => { d.description = v; touch(); }, { rows: 5 })),
    field("Inputs", rowsEditor(d.inputs, [{ key: "name", label: "name", mono: true }, { key: "label", label: "label" }, { key: "default", label: "default" }, { key: "optional", label: "optional", check: true }], () => touch()),
      "set when a run starts"),
    field("Values", rowsEditor(d.vars, [{ key: "name", label: "name", mono: true }, { key: "value", label: "value, may use {{placeholders}}", mono: true }], () => touch()), "vars, computed once at run start"),
    h("div", { class: "row2" },
      field("Run heading", textInput(d.subject, (v) => { d.subject = v; touch(); }, { class: "code", placeholder: "e.g. {{request}}" }), "subject"),
      field("Run link", textInput(d.link, (v) => { d.link = v; touch(); }, { class: "code", placeholder: "e.g. {{read_issue.url}}" }), "URL for the run heading")));
}
