const KIND_CHOICES = [["tool", "Tool: run a named action"], ["agent", "Agent: an agent CLI with a prompt"], ["user", "User: wait for a person to approve"], ["chain", "Sub-chain: run another chain"],
  ["repeat", "Repeat: go back to an earlier step"]];

function fieldsOf(step) {
  const kind = kindOf(step);
  if (kind === "agent") {
    const returns = promptOf(editor.name, step.agent)?.returns || [];
    return returns.length ? returns.map((f) => f.name) : ["output"];
  }
  if (kind === "user") return ["decision", "note"];
  if (kind === "tool") return ["output"];
  return [];
}

function stepInspector(step) {
  const kind = kindOf(step);
  const at = editor.draft.steps.indexOf(step);
  const heading = h("span", { class: "card-title grow" }, step.title || "(untitled)");
  const idHint = h("span", { class: "hint" });
  const showID = () => { idHint.textContent = ` later steps read its fields as {{${step.id}.${fieldsOf(step)[0] || "field"}}}`; };
  const idInput = textInput(step.id, (v) => { step.id = v; editor.fresh.delete(step); showID(); touch(); }, { class: "code" });
  showID();
  const setTitle = (v) => {
    step.title = v;
    heading.textContent = v || "(untitled)";
    if (editor.fresh.has(step)) {
      step.id = uniqueID(slugOf(v), step);
      idInput.value = step.id;
      showID();
    }
    touch();
  };
  const setKind = (next) => {
    for (const key of ["tool", "agent", "user", "revise", "show", "chain", "tools", "runner", "with", "resume", "stall", "timeout", "keep_until", "repeat", "rounds", "fail"]) delete step[key];
    if (next === "tool") step.tool = Object.keys(editor.tools).sort()[0] || "";
    if (next === "agent") { step.agent = Object.keys(editor.prompts).sort()[0] || ""; step.tools = ["Read", "Grep", "Glob"]; }
    if (next === "user") step.user = "Continue?";
    if (next === "chain") step.chain = lib.chains.map((c) => c.name).find((n) => n !== editor.name) || "";
    if (next === "repeat") step.repeat = editor.draft.steps[at - 1].id;
    touch(true);
  };
  const what = kind === "repeat" ? `Repeat (${step.rounds || 3}× at most)` : "What it does";
  return h("div", { class: "form" },
    h("div", { class: "inspector-head" },
      h("span", { class: `chip ${kind}` }, KIND_NAME[kind]), heading,
      h("button", { class: "btn small", type: "button", disabled: at === 0, onclick: () => move(-1), title: "Move up" }, "↑"),
      h("button", { class: "btn small", type: "button", disabled: at === editor.draft.steps.length - 1, onclick: () => move(1), title: "Move down" }, "↓"),
      h("button", { class: "btn small danger", type: "button", onclick: removeStep }, "Delete")),
    h("div", { class: "row2" },
      field("Title", textInput(step.title, setTitle)),
      h("label", { class: "field" }, h("span", {}, "ID", idHint), idInput)),
    inspectorSection(what, "", kindFields(step, kind)),
    setsSection(step),
    flowFields(step),
    kind === "tool" || kind === "agent" ? inspectorSection("When swarm stops it", "", limitFields(step, kind)) : null,
    h("details", { class: "advanced" }, h("summary", {}, "Change what kind of step this is"), selectBox(KIND_CHOICES.filter(([k]) => k !== "repeat" || at > 0), kind, setKind)));
}

function repeatFields(step) {
  const earlier = editor.draft.steps.slice(0, editor.draft.steps.indexOf(step));
  const rounds = step.rounds || 3;
  const counts = [...new Set([2, 3, 4, 5, 6, 8, 10, rounds])].sort((a, b) => a - b);
  return h("div", { class: "form" },
    field("Go back to", selectBox(earlier.map((s) => [s.id, s.title || s.id]), step.repeat, (v) => { step.repeat = v; touch(true); }),
      "every step from there to here runs again"),
    field("At most", selectBox(counts.map((n) => [String(n), `${n}×${n === 3 ? " (the default)" : ""}`]), String(rounds), (v) => { step.rounds = v === "3" ? undefined : Number(v); touch(true); }),
      "rounds, counting the first"),
    field("When the loop ends", selectBox(Object.entries(LOOP_ENDS), step.fail || "", (v) => { step.fail = v || undefined; touch(true); })));
}

const LOOP_ENDS = {
  "": "the run goes on either way",
  out_of_rounds: "the run fails if it still wants another round after the last",
  ended_early: "the run fails if the loop ends before the last round",
};

const IDLE = { tool: ["10m", "no output and no CPU use"], agent: ["15m", "nothing new from the agent"] };

function limitFields(step, kind) {
  const [stall, quiet] = IDLE[kind];
  const later = editor.draft.steps.slice(editor.draft.steps.indexOf(step) + 1);
  return h("div", { class: "form" },
    h("div", { class: "row2" },
      field("Idle limit", textInput(step.stall, (v) => { step.stall = v.trim() || undefined; touch(); }, { placeholder: stall, class: "code" }),
        `stopped after ${quiet} for this long; empty means ${stall}, 0 never`),
      field("Time limit", textInput(step.timeout, (v) => { step.timeout = v.trim() || undefined; touch(); }, { placeholder: "2h", class: "code" }),
        "stopped after this long in all; empty means 2h, 0 never")),
    kind === "tool" ? field("What it leaves running", selectBox([["", "is stopped when this step ends"], ...later.map((s) => [s.id, `keeps running until ${s.title || s.id} ends`])], step.keep_until || "", (v) => { step.keep_until = v || undefined; touch(true); }),
      "for a server that later steps need; whatever is left is stopped when the run ends") : null,
    h("div", { class: "hint" }, "Every process the step starts is stopped when it ends, including ones it leaves in the background."));
}

function inspectorSection(title, hint, ...children) {
  return h("section", { class: "subpanel" }, h("h3", {}, title, hint ? h("span", { class: "hint" }, ` ${hint}`) : null), ...children);
}

function readsStrip(namesOf, label) {
  const strip = h("div", { class: "reads-strip" });
  strip.draw = () => {
    const names = namesOf();
    strip.hidden = !names.length;
    strip.replaceChildren(h("span", { class: "reads-label" }, label), readsList(names, editor.draft, editorGo));
  };
  strip.draw();
  return strip;
}

function withReads(control, namesOf, label = "reads") {
  const strip = readsStrip(namesOf, label);
  control.addEventListener("input", strip.draw);
  control.addEventListener("change", strip.draw);
  return h("div", { class: "attached" }, control, strip);
}

function valuesGroup(label, body) {
  return h("div", { class: "group" }, h("span", { class: "group-label" }, label), h("div", { class: "group-body" }, body));
}

function readersOf(name) {
  const d = editor.draft;
  const out = d.steps.filter((s) => readsOf(s).includes(name)).map((s) => ({ step: s }));
  const others = [["the run heading", d.subject], ["the run link", d.link], ...(d.vars || []).map((v) => [`the value ${v.name}`, v.value])];
  for (const [label, text] of others) if (placeholdersIn(text).includes(name)) out.push({ label });
  return out;
}

function setsSection(step) {
  const kind = kindOf(step);
  if (kind === "chain") return null;
  const prefix = `${step.id}.`;
  const readByOthers = editor.draft.steps.flatMap(readsOf).filter((n) => n.startsWith(prefix)).map((n) => n.slice(prefix.length));
  const fields = [...new Set([...fieldsOf(step), ...readByOthers])];
  if (!fields.length) return null;
  const own = new Set([readCondition(step.stop_if)?.name, ...placeholdersIn(step.stop)]);
  return inspectorSection("What it sets", "and what reads each field",
    h("div", { class: "reads-list" }, fields.map((f) => {
      const parts = readersOf(prefix + f).map((r) => (r.step
        ? h("a", { href: "#", onclick: (e) => { e.preventDefault(); select(editor.draft.steps.indexOf(r.step)); } }, r.step.title) : r.label));
      if (own.has(f)) parts.push("its own stop condition");
      const shown = f === "summary" || f === "output";
      return h("div", { class: "read" }, h("code", { class: "ol-name" }, prefix + f),
        parts.length ? h("span", { class: "ol-from" }, "read by ", parts.flatMap((p, i) => (i ? [", ", p] : [p])))
          : h("span", { class: `ol-from ${shown || kind !== "agent" ? "" : "missing"}` }, shown ? "shown on the run page" : "nothing reads it"));
    })));
}

function flowFields(step) {
  const earlier = editor.draft.steps.slice(0, editor.draft.steps.indexOf(step));
  const names = [...[...(editor.draft.inputs || []), ...(editor.draft.vars || [])].map((v) => v.name).filter(Boolean),
    ...earlier.flatMap((s) => fieldsOf(s).map((f) => `${s.id}.${f}`))];
  if (kindOf(step) === "repeat") {
    const back = conditionEditor(step.when, ["every round, until the rounds run out", "only when"], names, (v) => { step.when = v; touch(); });
    return inspectorSection("When it goes back", "",
      field("Goes back", withReads(back, () => [readCondition(step.when)?.name].filter(Boolean)), "otherwise the loop ends here and the run goes on"));
  }
  const message = field("Message when it stops the run", textInput(step.stop, (v) => { step.stop = v || undefined; touch(); }, { placeholder: "e.g. Nothing was changed. {{summary}}" }));
  message.hidden = !step.stop_if && !step.stop;
  const gate = approvalBefore(step, editor.draft.steps);
  const runs = conditionEditor(step.when, ["every time the chain gets here", "only when"], names, (v) => { step.when = v; touch(); });
  return inspectorSection("When it runs", "",
    field("Runs", withReads(runs, () => [readCondition(step.when)?.name].filter(Boolean)),
      gate ? `only once you approve "${gate.title}": a rejection ends the run before it` : ""),
    field("Stops the run after it", conditionEditor(step.stop_if, ["never", "when its"], fieldsOf(step), (v) => { step.stop_if = v; message.hidden = !v && !step.stop; touch(); })),
    message,
    h("label", { class: "toggle" },
      h("input", { type: "checkbox", checked: !!step.always, onchange: (e) => { step.always = e.target.checked || undefined; touch(); } }),
      h("span", {}, "Also runs after a failure, a stop or a rejection, for cleanup")));
}

const usesCount = (match) => editor.draft.steps.filter(match).length
  + lib.chains.filter((c) => c.name !== editor.name).reduce((n, c) => n + (c.steps || []).filter(match).length, 0);

function sharedEditor(path, uses, body) {
  return h("details", {
    class: "shared-edit", open: editor.openShared.has(path),
    ontoggle: (e) => { if (e.target.open) editor.openShared.add(path); else editor.openShared.delete(path); },
  }, h("summary", {}, `Edit ${path} here`, h("span", { class: "hint" }, uses > 1 ? ` · used by ${uses} steps, a change applies to all of them` : " · only this step uses it")), body);
}

function kindFields(step, kind) {
  if (kind === "user") {
    const earlier = editor.draft.steps.slice(0, editor.draft.steps.indexOf(step));
    return h("div", { class: "form" },
      field("Question", withReads(textArea(step.user, (v) => { step.user = v || "Continue?"; touch(); }, { rows: Math.max(3, step.user.split("\n").length + 1) }),
        () => placeholdersIn(step.user))),
      field("Files to show with it", withReads(linesInput(step.show, (v) => { step.show = v.length ? v : undefined; touch(); }, { placeholder: "e.g. {{run_folder}}/change.diff" }),
        () => placeholdersIn((step.show || []).join("\n"))), "one per line, written by an earlier step; videos, images, markdown and diffs show inline"),
      field("Send back to", selectBox([["", "nowhere: only approve or reject"], ...earlier.map((s) => [s.id, s.title || s.id])], step.revise || "", (v) => { step.revise = v || undefined; touch(true); }),
        "the person's note reaches every agent that runs again"));
  }
  if (kind === "chain") {
    const sub = lib.chains.find((c) => c.name === step.chain);
    const lists = listsPassed(step, editor.draft.steps, editor.name);
    return h("div", { class: "form" },
      field("Chain to run", selectBox(lib.chains.filter((c) => c.name !== editor.name).map((c) => [c.name, c.title]), step.chain, (v) => { step.chain = v; touch(true); })),
      valuesGroup("Its inputs", valuesEditor(step, (sub?.inputs || []).map((i) => ({ name: i.name, label: i.label || i.name, fallback: i.default || (i.optional ? "optional" : "") })))),
      h("div", { class: "hint" }, lists.length
        ? `Runs ${step.chain} once per item of ${lists.join(", ")}, one after another. A rejection or stop ends only that item's run; a failure fails this step once every item has run.`
        : "Runs it once. To run it once per item of a list, set one input to just the list, e.g. {{split.changesets}}; any value that is a list when the step runs works."),
      step.chain ? h("a", { href: `#/chain/${step.chain}` }, `Open the ${step.chain} chain →`) : null);
  }
  if (kind === "agent") return agentFields(step);
  if (kind === "repeat") return repeatFields(step);
  const name = step.tool;
  const tool = editor.tools[name];
  const ownReads = readsStrip(() => placeholdersIn(tool?.run).filter((n) => n.includes(".") || lib.builtins.includes(n)), "its command reads");
  const values = h("div");
  const drawValues = () => { values.replaceChildren(valuesEditor(step, toolValues(tool))); ownReads.draw(); };
  drawValues();
  return h("div", { class: "form" },
    h("div", { class: "picked" }, field("Tool", h("div", { class: "attached" }, toolPicker(step), ownReads)),
      tool ? h("span", { class: "hint" }, `${tool.title}. `, h("a", { href: `#/tool/${name}` }, `Open tools/${name}.json`)) : null,
      tool ? sharedEditor(`tools/${name}.json`, usesCount((s) => s.tool === name), h("div", { class: "form" },
        field("What it does", textInput(tool.title, (v) => { tool.title = v; editor.dirtyTools.add(name); touch(); })),
        field("Command", textArea(tool.run, (v) => { tool.run = v; editor.dirtyTools.add(name); touch(); drawValues(); }, { class: "code", rows: 6 }), "runs in the project folder"))) : null),
    valuesGroup("Values for the tool", values));
}

function agentFields(step) {
  const name = step.agent;
  const returns = returnsText(promptOf(editor.name, name));
  const path = `chains/${editor.name}/${name}.md`;
  const own = new Set(Object.keys(step.with || {}));
  const promptReads = readsStrip(() => placeholdersIn(editor.prompts[name]).filter((n) => !own.has(n)), "the prompt reads");
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
    h("div", { class: "picked" }, field("Prompt", h("div", { class: "attached" }, promptPicker(step), promptReads)),
      name ? h("span", { class: "hint" }, returns ? `It returns ${returns}, from the prompt's ## Return section. ` : `It has no ## Return section, so it answers in free text, read as {{${step.id}.output}}. `,
        h("a", { href: `#/prompt/${editor.name}/${name}` }, `Open ${path}`)) : null,
      name ? sharedEditor(path, editor.draft.steps.filter((s) => s.agent === name).length,
        textArea(editor.prompts[name], (v) => { editor.prompts[name] = v; editor.dirtyPrompts.add(name); touch(); promptReads.draw(); }, { class: "code prompt" })) : null),
    field("Runs with", selectBox([["", `the default (${lib.default_runner})`], ...lib.runners.map((n) => [n, n])], step.runner || "", (v) => { step.runner = v || undefined; touch(true); }),
      step.runner === "codex" || (!step.runner && lib.default_runner === "codex") ? "Codex uses your own Codex config and ignores the list below" : "change the default under Settings"),
    field("Context", selectBox([["", "a new context"], ...editor.draft.steps.slice(0, editor.draft.steps.indexOf(step)).filter((s) => s.agent)
      .map((s) => [s.id, `continue the session of ${s.title || s.id}`])], step.resume || "", (v) => { step.resume = v || undefined; touch(true); }),
    "continuing keeps all that agent saw and did; a blind review needs a new context. Sent back to by a person, an agent step continues its own last attempt with the note either way (claude and codex)"),
    h("div", { class: "field" }, h("span", {}, "What the agent may use"), toolChecks));
}

function toolValues(tool) {
  return placeholdersIn(tool?.run).filter((n) => !n.includes(".") && !lib.builtins.includes(n)).map((n) => ({ name: n, label: n, fallback: "" }));
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
      const readsNow = () => placeholdersIn(step.with?.[w.name] || (chainNames.has(w.name) ? `{{${w.name}}}` : ""));
      return field(w.label, withReads(textInput(step.with?.[w.name], (v) => set(w.name, v), { class: "code", placeholder }), readsNow));
    }),
    extra.map((k) => h("div", { class: "hint" }, h("code", {}, `${k}: ${step.with[k]}`), " is not read any more ",
      h("button", { class: "linkish", type: "button", onclick: () => { set(k, ""); touch(true); } }, "remove"))));
}

function toolPicker(step) {
  const names = Object.keys(editor.tools).sort();
  return selectBox([...names.map((n) => [n, `${n}: ${editor.tools[n].title || ""}`]), ["__new", "+ New tool…"]], step.tool, (v) => {
    if (v === "__new") {
      const name = prompt("Name for the new tool");
      if (!name) { touch(true); return; }
      editor.tools[name] = { title: name, run: "echo hello" };
      editor.dirtyTools.add(name);
      editor.openShared.add(`tools/${name}.json`);
      v = name;
    }
    step.tool = v;
    touch(true);
  });
}

function promptPicker(step) {
  const names = Object.keys(editor.prompts).sort();
  return selectBox([...names.map((n) => [n, `${n}.md`]), ["__new", "+ New prompt…"]], step.agent, (v) => {
    if (v === "__new") {
      const name = prompt("Name for the new prompt (lowercase letters, digits, - and _)", step.id);
      if (!name) { touch(true); return; }
      editor.prompts[name] = NEW_PROMPT;
      editor.dirtyPrompts.add(name);
      editor.openShared.add(`chains/${editor.name}/${name}.md`);
      v = name;
    }
    step.agent = v;
    touch(true);
  });
}
