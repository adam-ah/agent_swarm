function notApproved(when, gateID) {
  const c = readCondition(when);
  return !!c && c.name === `${gateID}.decision` && ((c.op === "isnt" && c.value === "approve") || (c.op === "is" && c.value === "reject"));
}

function ifGroups(steps, inBranch = false) {
  const nodes = [];
  for (let i = 0; i < steps.length; i++) {
    const step = steps[i];
    const end = steps.findLastIndex((s, j) => j > i && s.repeat === step.id);
    if (end > i) {
      nodes.push({ type: "loop", step: steps[end], children: [...ifGroups(steps.slice(i, end), inBranch), { type: "step", step: steps[end] }] });
      i = end;
      continue;
    }
    const cond = inBranch ? "" : step.when || "";
    const last = nodes[nodes.length - 1];
    if (cond && last?.type === "if" && last.cond === cond) last.children.push({ type: "step", step });
    else if (cond) nodes.push({ type: "if", cond, children: [{ type: "step", step }] });
    else nodes.push({ type: "step", step });
  }
  return nodes;
}

function outlineOf(steps, all = steps) {
  const at = steps.findIndex((s) => kindOf(s) === "user");
  if (at < 0) {
    let end = steps.length;
    while (end > 0 && steps[end - 1].always) end--;
    const nodes = ifGroups(steps.slice(0, end));
    if (end < steps.length) nodes.push({ type: "always", children: ifGroups(steps.slice(end)) });
    return nodes;
  }
  const gate = steps[at];
  const rest = steps.slice(at + 1);
  const rejected = rest.filter((s) => s.always && notApproved(s.when, gate.id));
  const always = rest.filter((s) => s.always && !rejected.includes(s));
  const nodes = ifGroups(steps.slice(0, at));
  const back = gate.revise ? all.find((s) => s.id === gate.revise) || { id: gate.revise, title: gate.revise } : null;
  nodes.push({ type: "decision", step: gate, back, approved: outlineOf(rest.filter((s) => !s.always), all), rejected: ifGroups(rejected, true), always: always.length > 0 });
  if (always.length) nodes.push({ type: "always", children: ifGroups(always) });
  return nodes;
}

const BLOCK_START = { loop: "start of the loop", if: "start of the If block", always: "start of the steps that always run" };

function startsAround(nodes, id) {
  for (const node of nodes) {
    if (node.type === "step" || node.type === "decision") {
      if (node.step.id === id) return [];
      if (node.type === "step") continue;
    }
    const branches = node.type === "decision"
      ? [[node.approved, "start of the approved branch"], [node.rejected, "start of the rejected branch"]]
      : [[node.children, BLOCK_START[node.type]]];
    for (const [kids, level] of branches) {
      const inner = startsAround(kids, id);
      if (!inner) continue;
      const around = [...inner, { step: stepsIn(kids)[0], level }];
      return node.type === "decision" ? [...around, { step: node.step, level: "the approval" }] : around;
    }
  }
  return null;
}

function stepsIn(nodes) {
  return nodes.flatMap((n) => (n.type === "step" ? [n.step] : n.type === "decision" ? [n.step, ...stepsIn(n.approved), ...stepsIn(n.rejected)] : stepsIn(n.children)));
}

function sourceOf(name, chain) {
  const dot = name.indexOf(".");
  if (dot > 0) {
    const step = (chain.steps || []).find((s) => s.id === name.slice(0, dot));
    return step ? { kind: "step", step, field: name.slice(dot + 1) } : { kind: "missing" };
  }
  const input = (chain.inputs || []).find((i) => i.name === name);
  if (input) return { kind: "input", input };
  if ((chain.vars || []).some((v) => v.name === name)) return { kind: "var" };
  return lib.builtins?.includes(name) ? { kind: "builtin", name } : { kind: "missing" };
}

function goLink(text, target, go) {
  if (!go) return text;
  return h("a", { href: "#", onclick: (e) => { e.preventDefault(); e.stopPropagation(); go(target); } }, text);
}

function sourceNote(from, go) {
  if (from.kind === "step") return h("span", { class: "ol-from" }, "set by ", goLink(from.step.title, from, go));
  if (from.kind === "input") {
    return h("span", { class: "ol-from" }, "input ", goLink(`“${from.input.label || from.input.name}”`, from, go),
      lib.input_values?.[from.input.name] ? [", project default in ", h("a", { href: "#/settings" }, "Settings")] : null);
  }
  if (from.kind === "var") return h("span", { class: "ol-from" }, goLink("var, computed at start", from, go));
  if (from.kind === "builtin") return h("span", { class: "ol-from" }, "built-in", from.name === "memory" ? [" from ", h("a", { href: "#/memory" }, "Memory")] : null);
  return h("span", { class: "ol-from missing" }, "nothing sets it");
}

function conditionLabel(text, chain, go, value) {
  const c = readCondition(text);
  if (!c) return h("span", { class: "ol-note" }, text);
  const from = sourceOf(c.name, chain);
  return [
    h("code", { class: "ol-name" }, c.name),
    h("span", { class: "ol-note" }, { is: `is ${c.value}`, isnt: `is not ${c.value}`, empty: "is empty", set: "is not empty" }[c.op]),
    sourceNote(from, go),
    value ? value(c.name, from) : null,
  ];
}

function renderOutline(nodes, view, blocks = [], inAlways = false) {
  const group = (cls, label, children, always = inAlways, lead = null) => {
    const el = h("div", { class: `ol-block ${cls}` }, h("div", { class: "ol-label" }, label), h("div", { class: "ol-kids" }, lead, renderOutline(children, view, blocks, always)));
    blocks.push({ el, steps: stepsIn(children) });
    return el;
  };
  const word = (text) => h("span", { class: "ol-word" }, text);
  const note = (...parts) => h("span", { class: "ol-note" }, parts);
  return nodes.map((node) => {
    if (node.type === "step") return view.step(node.step, inAlways);
    if (node.type === "if") return group("ol-if", [word("If"), conditionLabel(node.cond, view.chain, view.go, view.value)], node.children);
    if (node.type === "always") return group("ol-always", [word("Then, always"), note("also after failure, stop or reject")], node.children, true);
    if (node.type === "loop") {
      const r = node.step;
      return group("ol-loop", [word("Repeat"), note(`(${r.rounds || 3}× at most)`),
        r.when ? [note("while"), conditionLabel(r.when, view.chain, view.go, view.value)] : note("every round"),
        note(`· ${LOOP_ENDS[r.fail || ""] || r.fail}`)], node.children, inAlways, view.rounds?.(node));
    }
    const everyRejected = node.rejected.length && node.rejected.every((n) => readCondition(n.step.when)?.op === "isnt");
    const after = node.always ? "only THEN, ALWAYS steps run" : "nothing more runs";
    return [
      view.step(node.step, inAlways),
      h("div", { class: "ol-branches" },
        group("ol-approved", [word("Approved"), node.approved.length ? null : note("nothing more runs")], node.approved),
        node.back ? h("div", { class: "ol-block ol-revise" }, h("div", { class: "ol-label" }, word("Sent back"),
          note("⟲ reruns from ", goLink(`“${node.back.title}”`, { kind: "step", step: node.back }, view.go), " with the note"))) : null,
        group("ol-rejected", [word("Rejected"), note(everyRejected ? "or an earlier step failed or stopped" : node.rejected.length ? "" : after)], node.rejected, true)),
    ];
  });
}
