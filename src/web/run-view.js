function showRun(id) {
  main.replaceChildren(h("div", { class: "page" }, h("div", { class: "empty" }, "Loading…")));
  runView = { id, entries: new Map(), status: null, decisionKey: null, slots: new Map(), blocks: [], grouped: new Set(), values: [] };
  stream = new EventSource(`/api/runs/${encodeURIComponent(id)}/events`);
  stream.onmessage = (event) => renderRun(JSON.parse(event.data));
}

function findStepIn(steps, match) {
  for (const step of steps || []) {
    if (match(step)) return step;
    const inner = findStepIn(step.children, match);
    if (inner) return inner;
  }
  return null;
}

function renderRun(run) {
  let page = main.querySelector(".page[data-run]");
  if (!page || page.dataset.run !== run.id) {
    page = h("div", { class: "page narrow", "data-run": run.id },
      h("div", { class: "eyebrow" }, "Run of ", h("a", { href: `#/chain/${run.chain}` }, run.title)),
      h("h1", { id: "subject" }),
      h("div", { class: "run-meta", id: "meta" }),
      h("div", { id: "banner" }),
      h("div", { id: "decision" }),
      h("div", { class: "panel steps", id: "steps" }, skeleton(run.chain, "")));
    main.replaceChildren(page);
    Object.assign(runView, { entries: new Map(), decisionKey: null });
  }
  const label = run.subject || Object.entries(run.inputs || {}).map(([k, v]) => `${k} ${v}`).join(", ") || run.title;
  page.querySelector("#subject").replaceChildren(run.link ? h("a", { href: run.link, target: "_blank", rel: "noreferrer" }, label) : label);

  const live = run.status === "running" || run.status === "waiting";
  page.querySelector("#meta").replaceChildren(...[
    h("span", { class: "pill" }, h("span", { class: `dot ${run.status}` }), STATUS_LABEL[run.status] || run.status),
    h("span", {}, `started ${new Date(run.started_at).toLocaleTimeString()}`),
    h("span", { "data-since": run.started_at, "data-until": run.ended_at || "" }, duration(run.started_at, run.ended_at)),
    run.cost ? h("span", {}, money(run.cost)) : null,
    live ? h("button", { class: "btn small danger push-right", type: "button", onclick: () => stopRun(run.id) }, "Stop run") : null,
  ].filter(Boolean));

  page.querySelector("#banner").replaceChildren(outcomeBanner(run) || "");
  renderDecision(run, page.querySelector("#decision"));
  syncRows(page.querySelector("#steps"), run.steps, run, run.chain);
  markBlocks(run);

  if (runView.status !== run.status) {
    runView.status = run.status;
    loadRuns();
  }
}

function outcomeBanner(run) {
  if (["running", "waiting", "done"].includes(run.status)) return null;
  const step = findStepIn(run.steps, (s) => s.status === run.status);
  if (!step) return h("div", { class: `banner ${run.status}` }, h("strong", {}, STATUS_LABEL[run.status] || run.status), run.reason ? ` ${run.reason}` : "");
  const at = h("a", { href: `#/run/${run.id}`, onclick: (e) => { e.preventDefault(); focusRow(keyOf(step)); } }, step.title);
  const said = run.status === "stopped" ? step.ended : run.status === "rejected" ? step.output?.note : "";
  return h("div", { class: `banner ${run.status}` }, h("strong", {}, STATUS_LABEL[run.status]), " at ", at, said ? `: ${said}` : "");
}

function renderDecision(run, el) {
  const step = run.status === "waiting" ? findStepIn(run.steps, (s) => s.status === "waiting") : null;
  const key = step ? `${keyOf(step)}:${step.round || 0}:${JSON.stringify(step.artifacts || [])}` : "";
  if (key === runView.decisionKey) return;
  runView.decisionKey = key;
  el.replaceChildren(step ? h("section", { class: "decision" },
    h("div", { class: "decision-head" }, h("span", { class: "state waiting" }), step.title),
    h("div", { class: "question" }, step.question),
    h("div", { class: "artifacts" }, renderArtifacts(run.id, step.artifacts || [])),
    decideForm(run.id, step)) : "");
}

const keyOf = (step) => step.path || step.id;

function focusRow(key) {
  const entry = runView.entries.get(key);
  if (!entry) return;
  if (!entry.open) toggleRow(entry);
  entry.el.scrollIntoView({ behavior: "smooth", block: "start" });
  entry.el.classList.add("flash");
  setTimeout(() => entry.el.classList.remove("flash"), 1200);
}

function skeleton(chainName, prefix) {
  const chain = lib.chains.find((c) => c.name === chainName);
  if (!chain?.steps?.length) return null;
  const blocks = [];
  const value = (name, from) => {
    const span = h("span", { class: "ol-value" });
    runView.values.push({ span, name, from, prefix });
    return span;
  };
  const el = renderOutline(outlineOf(chain.steps), {
    chain,
    value,
    go: runGo(chainName, prefix),
    step: (s, inAlways) => {
      const slot = h("div", { class: "slot" });
      runView.slots.set(prefix + s.id, slot);
      if (inAlways) runView.grouped.add(prefix + s.id);
      return slot;
    },
  }, blocks);
  runView.blocks.push(...blocks.map((b) => ({ el: b.el, paths: b.steps.map((s) => prefix + s.id) })));
  return el;
}

const runGo = (chainName, prefix) => (from) => {
  if (from.kind === "step") focusRow(prefix + from.step.id);
  else location.hash = `#/chain/${chainName}`;
};

const RAN = new Set(["done", "failed", "running", "waiting", "stopped", "rejected"]);

function valueNow({ name, from, prefix }, run) {
  if (from.kind === "input") return prefix ? undefined : run.inputs?.[name] ?? "";
  if (from.kind !== "step") return undefined;
  const entry = runView.entries.get(prefix + from.step.id);
  if (!entry || !RAN.has(entry.step.status)) return undefined;
  return asText(entry.step.output?.[from.field]);
}

function markBlocks(run) {
  for (const block of runView.blocks) {
    const states = block.paths.map((p) => runView.entries.get(p)?.step.status).filter(Boolean);
    const ran = states.some((s) => RAN.has(s));
    block.el.classList.toggle("ol-skipped", !ran && states.length > 0 && states.every((s) => s === "skipped"));
    block.el.classList.toggle("ol-ran", ran);
  }
  for (const value of runView.values) {
    const now = valueNow(value, run);
    const text = String(now ?? "");
    value.span.textContent = now === undefined ? "" : text ? `value “${text.length > 60 ? `${text.slice(0, 60)}…` : text}”` : "value empty";
    value.span.title = text;
  }
}

function syncRows(container, steps, run, chain) {
  for (const step of steps) {
    let entry = runView.entries.get(keyOf(step));
    if (!entry) {
      const nested = holdsSteps(step) ? skeleton(step.uses, `${keyOf(step)}.`) : null;
      entry = { el: h("div"), sig: null, artSig: null, arts: h("div", { class: "artifacts" }), kids: h("div", { class: "kids" }, nested), chain, open: false };
      runView.entries.set(keyOf(step), entry);
      (runView.slots.get(keyOf(step)) || container).append(entry.el);
    }
    Object.assign(entry, { run, step });
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
    if (step.children?.[0]?.kind === "item" && !entry.perItem) {
      entry.perItem = true;
      entry.kids.replaceChildren();
    }
    if (step.children?.length) syncRows(entry.kids, step.children, run, step.uses);
    const sig = JSON.stringify({ ...step, artifacts: undefined, children: undefined, earlier: undefined, cost: holdsSteps(step) ? undefined : step.cost });
    if (entry.sig !== sig) {
      if (step.status === "failed" && entry.status !== "failed") entry.open = true;
      Object.assign(entry, { sig, status: step.status });
      redrawRow(entry);
    }
  }
}

function redrawRow(entry) {
  const fresh = stepRow(entry);
  entry.el.replaceWith(fresh);
  entry.el = fresh;
  if (entry.open && entry.call?.el.open) entry.call.load();
}

function toggleRow(entry) {
  entry.open = !entry.open;
  redrawRow(entry);
}

function stepRow(entry) {
  const { run, step } = entry;
  const stats = [];
  if (step.started_at && step.status !== "running") stats.push(duration(step.started_at, step.ended_at));
  if (step.cost && !holdsSteps(step)) stats.push(money(step.cost));
  else if (step.tokens) stats.push(`${(step.tokens / 1000).toFixed(step.tokens < 10000 ? 1 : 0)}k tokens`);
  const expandable = !["pending", "skipped"].includes(step.status) && !holdsSteps(step);
  const open = expandable && entry.open;
  const toggle = () => toggleRow(entry);
  const head = h("div", {
    class: "srow-head", role: expandable ? "button" : null, tabindex: expandable ? "0" : null, "aria-expanded": expandable ? String(open) : null,
    onclick: expandable ? toggle : null, onkeydown: expandable ? (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); toggle(); } } : null,
  },
  h("span", { class: `state ${step.status}` }, STATE_ICON[step.status] || ""),
  h("span", { class: `kind ${step.kind}`, title: KIND_NAME[step.kind] }, step.kind === "item" ? step.id : KIND_LETTER[step.kind]),
  h("span", { class: "srow-text" },
    h("span", { class: "srow-title" }, step.title,
      step.round ? h("span", { class: "chip flag" }, `attempt ${step.round + 1}`) : null,
      step.always && !runView.grouped.has(keyOf(step)) ? h("span", { class: "chip flag", title: "Runs even after a failure, a stop or a rejection" }, "always") : null),
    summaryLine(step, open)),
  h("span", { class: "srow-stats" }, stats.join(" · ")),
  h("span", { class: "srow-caret" }, expandable ? (open ? "▾" : "▸") : ""));
  return h("div", { class: `srow ${step.status} ${step.kind} ${open ? "open" : ""}`, id: `row-${keyOf(step)}` },
    head,
    open ? h("div", { class: "srow-body" }, stepBody(run, step, entry)) : null,
    holdsSteps(step) ? entry.kids : null);
}

function summaryLine(step, open) {
  if (step.status === "running" && !holdsSteps(step)) {
    return h("span", { class: "srow-line working" }, h("span", { class: "bars" }, h("i"), h("i"), h("i")),
      `${step.kind === "agent" ? `Working · ${step.actions || 0} actions` : "Running"} · `, h("span", { "data-since": step.started_at }, duration(step.started_at)));
  }
  if (step.status === "waiting") return h("span", { class: "srow-line" }, "Waiting for your answer above");
  const text = step.headline || (open ? "" : firstLine(step.error) || step.ended);
  return text ? h("span", { class: "srow-line" }, text) : null;
}

function stepBody(run, step, entry) {
  return [
    step.error ? h("div", { class: "error-box" }, step.error) : null,
    step.ended ? h("div", { class: "ended-box" }, h("strong", {}, "Ended the run: "), step.ended) : null,
    step.kind === "user" && step.question ? h("div", { class: "question" }, step.question) : null,
    entry.arts,
    stepFields(step),
    readsPanel(run, step, entry.chain),
    entry.call?.el,
    entry.earlier,
    h("div", { class: "srow-links" }, editLinks(step, entry.chain)),
  ];
}

function editLinks(step, chain) {
  const target = { tool: ["tool", `Open the tool ${step.uses}`], agent: ["prompt", `Open the prompt ${step.uses}`] }[step.kind];
  return [
    target && step.uses ? h("a", { href: `#/${target[0]}/${encodeURIComponent(step.uses)}` }, target[1]) : null,
    chain ? h("a", { href: `#/chain/${encodeURIComponent(chain)}/${step.id}` }, "Edit this step") : null,
  ];
}

function decideForm(runID, step) {
  const note = h("textarea", { rows: 2, placeholder: step.revise ? `Note: what to change. Needed to send it back to ${step.revise}.` : "Optional note" });
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
    h("button", { class: "btn approve", type: "button", onclick: (e) => decide("approve", e.target) }, "Approve"),
    step.revise ? h("button", { class: "btn", type: "button", onclick: (e) => decide("revise", e.target) }, `Send back to ${step.revise}`) : null,
    h("button", { class: "btn danger", type: "button", onclick: (e) => decide("reject", e.target) }, "Reject")));
}

async function stopRun(id) {
  if (!confirm("Stop this run? Steps marked \"always\" still run.")) return;
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
