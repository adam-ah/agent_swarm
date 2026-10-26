function stepInspector(step) {
  const kind = kindOf(step);
  const setKind = (next) => {
    for (const key of ["tool", "agent", "user", "revise", "show", "chain", "tools", "with"]) delete step[key];
    if (next === "tool") step.tool = Object.keys(editor.tools).sort()[0] || "";
    if (next === "agent") { step.agent = Object.keys(editor.prompts).sort()[0] || ""; step.tools = ["Read", "Grep", "Glob"]; }
    if (next === "user") step.user = "Continue?";
    if (next === "chain") step.chain = lib.chains.map((c) => c.name).find((n) => n !== editor.name) || "";
    touch(true);
  };
  const idInput = textInput(step.id, (v) => { step.id = v; editor.fresh.delete(step); touch(); }, { class: "code" });
  const setTitle = (v) => {
    step.title = v;
    if (editor.fresh.has(step)) {
      step.id = uniqueID(slugOf(v), step);
      idInput.value = step.id;
    }
    touch();
  };
  return h("div", { class: "form" },
    h("div", { class: "card-head" }, h("span", { class: `chip ${kind}` }, KIND_NAME[kind]), h("span", { class: "card-title" }, step.title || "(untitled)")),
    h("div", { class: "row2" },
      field("Title", textInput(step.title, setTitle)),
      field("ID", idInput, "used as {{id.field}}")),
    field("Kind", selectBox([["tool", "Tool: run a named action"], ["agent", "Agent: an agent CLI with a prompt"], ["user", "User: wait for a person to approve"], ["chain", "Sub-chain: run another chain"]], kind, setKind)),
    kindFields(step, kind),
    h("div", { class: "subpanel" },
      h("h3", {}, "Flow"),
      h("div", { class: "row2" },
        field("Run only if", textInput(step.when, (v) => { step.when = v || undefined; touch(); }, { class: "code", placeholder: "e.g. triage.verdict == bug" })),
        field("Stop the chain if", textInput(step.stop_if, (v) => { step.stop_if = v || undefined; touch(); }, { class: "code", placeholder: "e.g. verdict != REPRODUCED" }))),
      field("Message when it stops", textInput(step.stop, (v) => { step.stop = v || undefined; touch(); }, { placeholder: "e.g. Could not reproduce. {{summary}}" })),
      h("label", { class: `check${step.always ? " on" : ""}` },
        h("input", { type: "checkbox", checked: !!step.always, onchange: (e) => { step.always = e.target.checked || undefined; touch(true); } }),
        "Always runs, even after a failure or stop")));
}

function kindFields(step, kind) {
  if (kind === "user") {
    const earlier = editor.draft.steps.slice(0, editor.draft.steps.indexOf(step));
    return h("div", { class: "form" },
      field("Question for you", textArea(step.user, (v) => { step.user = v || "Continue?"; touch(); }, { rows: 2 })),
      field("Files to show with the question", linesInput(step.show, (v) => { step.show = v.length ? v : undefined; touch(); }, { placeholder: "e.g. {{run_folder}}/change.diff" }),
        "what you decide on, written by an earlier step; one per line, videos, images, markdown and diffs display inline"),
      field("Revise goes back to", selectBox([["", "nowhere: only approve or reject"], ...earlier.map((s) => [s.id, s.title || s.id])], step.revise || "", (v) => { step.revise = v || undefined; touch(true); }),
        "your note is added to the prompt of every agent that runs again"));
  }
  if (kind === "chain") {
    const sub = lib.chains.find((c) => c.name === step.chain);
    return h("div", { class: "form" },
      field("Chain to run", selectBox(lib.chains.filter((c) => c.name !== editor.name).map((c) => [c.name, c.title]), step.chain, (v) => { step.chain = v; touch(true); })),
      field("Its inputs", valuesEditor(step, (sub?.inputs || []).map((i) => ({ name: i.name, label: i.label || i.name, fallback: i.default || (i.optional ? "optional" : "") })))),
      step.chain ? h("a", { href: `#/chain/${step.chain}` }, `Open the ${step.chain} chain →`) : null);
  }
  if (kind === "agent") {
    const name = step.agent;
    const returns = returnsText(lib.prompts[name]);
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
      picked("Prompt", promptPicker(step), name ? [
        textArea(editor.prompts[name], (v) => { editor.prompts[name] = v; editor.dirtyPrompts.add(name); touch(); }, { class: "code prompt" }),
        h("span", { class: "hint" }, returns ? `Returns ${returns}, read from its ## Return section when saved.` : `No ## Return section, so it returns free text, read as {{${step.id}.output}}.`),
        h("span", { class: "hint" }, `prompts/${name}.md · changes here apply to every step that uses this prompt`)] : null),
      field("Runs with", selectBox([["", `the default (${lib.default_runner})`], ...lib.runners.map((n) => [n, n])], step.runner || "", (v) => { step.runner = v || undefined; touch(true); }),
        step.runner === "codex" || (!step.runner && lib.default_runner === "codex") ? "Codex uses your own Codex config and ignores the list below" : "change the default under Settings"),
      field("What the agent may use", toolChecks));
  }
  const name = step.tool;
  const tool = editor.tools[name];
  const values = h("div");
  const drawValues = () => values.replaceChildren(valuesEditor(step, toolValues(tool)));
  drawValues();
  return h("div", { class: "form" },
    picked("Tool", toolPicker(step), tool ? [
      field("What it does", textInput(tool.title, (v) => { tool.title = v; editor.dirtyTools.add(name); touch(); })),
      field("Command", textArea(tool.run, (v) => { tool.run = v; editor.dirtyTools.add(name); touch(); drawValues(); }, { class: "code", rows: 6 }), "runs in the project folder"),
      h("span", { class: "hint" }, `tools/${name}.json · changes here apply to every chain that uses this tool`)] : null),
    field("Values for the tool", values));
}

function toolValues(tool) {
  return placeholdersIn(tool?.run).filter((n) => !n.includes(".") && !lib.builtins.includes(n)).map((n) => ({ name: n, label: `{{${n}}}`, fallback: "" }));
}

function valuesEditor(step, wanted) {
  const chainNames = new Set([...(editor.draft.inputs || []), ...(editor.draft.vars || [])].map((v) => v.name));
  const known = new Set(wanted.map((w) => w.name));
  const extra = Object.keys(step.with || {}).filter((k) => !known.has(k));
  const set = (key, value) => {
    step.with = { ...step.with, [key]: value };
    if (!value) delete step.with[key];
    touch();
  };
  if (!wanted.length && !extra.length) return h("span", { class: "hint" }, "It needs no values from this step.");
  return h("div", { class: "form" },
    wanted.map((w) => {
      const placeholder = chainNames.has(w.name) ? `empty: this chain's {{${w.name}}}` : w.fallback ? `empty: ${w.fallback}` : "needs a value, e.g. {{step_id.field}}";
      return field(w.label, textInput(step.with?.[w.name], (v) => set(w.name, v), { class: "code", placeholder }));
    }),
    extra.map((k) => h("div", { class: "hint" }, h("code", {}, `${k}: ${step.with[k]}`), " is not read any more ",
      h("button", { class: "linkish", type: "button", onclick: () => { set(k, ""); touch(true); } }, "remove"))));
}

function picked(label, picker, body) {
  return h("div", { class: `picked ${body ? "open" : ""}` }, field(label, picker), body ? h("div", { class: "picked-body" }, body) : null);
}

function toolPicker(step) {
  const names = Object.keys(editor.tools).sort();
  return selectBox([...names.map((n) => [n, `${n} · ${editor.tools[n].title || ""}`]), ["__new", "+ New tool…"]], step.tool, (v) => {
    if (v === "__new") {
      const name = prompt("Name for the new tool");
      if (!name) { touch(true); return; }
      editor.tools[name] = { title: name, run: "echo hello" };
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
      editor.prompts[name] = NEW_PROMPT;
      editor.dirtyPrompts.add(name);
      v = name;
    }
    step.agent = v;
    touch(true);
  });
}
