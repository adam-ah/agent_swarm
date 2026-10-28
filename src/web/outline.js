function notApproved(when, gateID) {
  const c = readCondition(when);
  return !!c && c.name === `${gateID}.decision` && ((c.op === "isnt" && c.value === "approve") || (c.op === "is" && c.value === "reject"));
}

function ifGroups(steps, inBranch = false) {
  const nodes = [];
  for (const step of steps) {
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
    return h("span", { class: "ol-from" }, "asked when the run starts: ", goLink(`“${from.input.label || from.input.name}”`, from, go),
      lib.input_values?.[from.input.name] ? [", this project's value is in ", h("a", { href: "#/settings" }, "Settings")] : null);
  }
  if (from.kind === "var") return h("span", { class: "ol-from" }, goLink("worked out when the run starts", from, go));
  if (from.kind === "builtin") return h("span", { class: "ol-from" }, "filled in by swarm", from.name === "memory" ? [" from ", h("a", { href: "#/memory" }, "Memory")] : null);
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
  const group = (cls, label, children, always = inAlways) => {
    const el = h("div", { class: `ol-block ${cls}` }, h("div", { class: "ol-label" }, label), h("div", { class: "ol-kids" }, renderOutline(children, view, blocks, always)));
    blocks.push({ el, steps: stepsIn(children) });
    return el;
  };
  const word = (text) => h("span", { class: "ol-word" }, text);
  const note = (...parts) => h("span", { class: "ol-note" }, parts);
  return nodes.map((node) => {
    if (node.type === "step") return view.step(node.step, inAlways);
    if (node.type === "if") return group("ol-if", [word("If"), conditionLabel(node.cond, view.chain, view.go, view.value)], node.children);
    if (node.type === "always") return group("ol-always", [word("Then, always"), note("even after a failure, a stop or a rejection")], node.children, true);
    const everyRejected = node.rejected.length && node.rejected.every((n) => readCondition(n.step.when)?.op === "isnt");
    const after = node.always ? "only the steps under THEN, ALWAYS run" : "nothing more runs";
    return [
      view.step(node.step, inAlways),
      h("div", { class: "ol-branches" },
        group("ol-approved", [word("Approved"), node.approved.length ? null : note("nothing more runs")], node.approved),
        node.back ? h("div", { class: "ol-block ol-revise" }, h("div", { class: "ol-label" }, word("Sent back"),
          note("⟲ runs again from ", goLink(`“${node.back.title}”`, { kind: "step", step: node.back }, view.go), ", with your note"))) : null,
        group("ol-rejected", [word("Rejected"), note(everyRejected ? "or an earlier step failed or stopped" : node.rejected.length ? "" : after)], node.rejected, true)),
    ];
  });
}
