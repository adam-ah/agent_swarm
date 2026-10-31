function showRun(id) {
  main.replaceChildren(h("div", { class: "page" }, h("div", { class: "empty" }, "Loading…")));
  runView = { id, entries: new Map(), status: null, decisionKey: null, decisions: new Map(), slots: new Map(), blocks: [], grouped: new Set(), values: [], rounds: [] };
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
      h("h1", { id: "subject", onclick: (e) => { if (!e.target.closest("a")) e.currentTarget.classList.toggle("whole"); } }),
      h("div", { class: "run-meta", id: "meta" }),
      h("div", { id: "banner" }),
      h("div", { id: "decision" }),
      h("div", { class: "panel steps", id: "steps" }, skeleton(run.chain, "")),
      h("div", { id: "stopped" }));
    main.replaceChildren(page);
    Object.assign(runView, { entries: new Map(), decisionKey: null, decisions: new Map() });
  }
  const label = run.subject || Object.values(run.inputs || {}).find((v) => v) || run.title;
  const subject = page.querySelector("#subject");
  subject.title = label;
  subject.replaceChildren(run.link ? h("a", { href: run.link, target: "_blank", rel: "noreferrer" }, label) : label);

  const live = run.status === "running" || run.status === "waiting";
  const unfinished = !live && run.status !== "done";
  const from = unfinished ? resumeStep(run.steps) : null;
  page.querySelector("#meta").replaceChildren(...[
    h("span", { class: "pill" }, h("span", { class: `dot ${run.status}` }), STATUS_LABEL[run.status] || run.status),
    h("span", {}, `started ${new Date(run.started_at).toLocaleTimeString()}`),
    run.resumed_at ? h("span", {}, `rerun from ${run.rerun_from || "its failed step"} at ${new Date(run.resumed_at).toLocaleTimeString()}`) : null,
    h("span", { "data-since": run.started_at, "data-until": run.ended_at || "" }, duration(run.started_at, run.ended_at)),
    run.cost ? h("span", {}, money(run.cost)) : null,
    h("span", { class: "row push-right" },
      live ? h("button", { class: "btn small danger", type: "button", onclick: () => stopRun(run.id) }, "Stop run") : null,
      unfinished ? h("button", { class: "btn small", type: "button", title: "Keep finished steps; rerun from the first unfinished one", onclick: () => rerunRun(run.id, "failed") }, from ? `Rerun from ${from.title}` : "Rerun from where it ended") : null,
      unfinished ? rerunPoints(run).map(({ step, level }) => h("button", { class: "btn small", type: "button", title: `Keep steps before ${step.title}; rerun from it`,
        onclick: () => rerunRun(run.id, "step", keyOf(step)) }, `Rerun from ${step.title} (${level})`)) : null,
      unfinished ? h("button", { class: "btn small", type: "button", title: "New run with the same inputs", onclick: () => rerunRun(run.id, "scratch") }, "Rerun from scratch") : null,
      live ? null : h("button", { class: "btn small danger", type: "button", onclick: () => deleteRun(run.id).then((gone) => { if (gone) location.hash = "#/"; }) }, "Delete run")),
  ].filter(Boolean));

  page.querySelector("#banner").replaceChildren(outcomeBanner(run) || "");
  renderDecision(run, page.querySelector("#decision"));
  syncRows(page.querySelector("#steps"), run.steps, run, run.chain);
  syncRounds(run);
  markBlocks(run);
  renderStopped(run, page.querySelector("#stopped"));

  if (runView.status !== run.status) {
    runView.status = run.status;
    loadRuns();
  }
}

function renderStopped(run, el) {
  const stopped = run.stopped || [];
  if (el.dataset.count === String(stopped.length)) return;
  el.dataset.count = String(stopped.length);
  el.replaceChildren(stopped.length ? h("details", { class: "output stopped-list", open: el.querySelector("details")?.open || false },
    h("summary", {}, `Killed outside a step (${stopped.length})`),
    h("pre", {}, stopped.join("\n"))) : "");
}

function outcomeBanner(run) {
  if (["running", "waiting", "done"].includes(run.status)) return null;
  const step = findStepIn(run.steps, (s) => s.status === run.status);
  if (!step) return h("div", { class: `banner ${run.status}` }, h("strong", {}, STATUS_LABEL[run.status] || run.status), run.reason ? ` ${run.reason}` : "");
  const at = h("a", { href: `#/run/${run.id}`, onclick: (e) => { e.preventDefault(); focusRow(keyOf(step)); } }, step.title);
  const said = run.status === "stopped" ? step.ended : run.status === "rejected" ? step.output?.note : "";
  return h("div", { class: `banner ${run.status}` }, h("strong", {}, STATUS_LABEL[run.status]), " at ", at, said ? `: ${said}` : "");
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
    rounds: (loop) => {
      const el = h("div", { class: "rounds" });
      runView.rounds.push({ el, paths: stepsIn(loop.children).map((s) => prefix + s.id), total: loop.step.rounds || 3, opened: new Map(), sig: null });
      return el;
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

const SETTLED = new Set(["done", "failed", "stopped", "rejected"]);

function valueNow({ name, from, prefix }, run) {
  if (from.kind === "input") return prefix ? undefined : { text: run.inputs?.[name] ?? "" };
  if (from.kind !== "step") return undefined;
  const step = runView.entries.get(prefix + from.step.id)?.step;
  if (!step) return undefined;
  if (SETTLED.has(step.status)) return { text: asText(step.output?.[from.field]) };
  const before = step.earlier?.findLast((s) => SETTLED.has(s.status));
  if (!before) return undefined;
  return { text: asText(before.output?.[from.field]), from: step.rounds ? `from round ${roundOf(before)}` : `from attempt ${(before.round || 0) + 1}` };
}

function valueLabel(now, max) {
  if (!now) return "";
  const shown = now.text ? `value “${now.text.length > max ? `${now.text.slice(0, max)}…` : now.text}”` : "value empty";
  return now.from ? `${shown} ${now.from}` : shown;
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
    value.span.textContent = valueLabel(now, 60);
    value.span.title = now?.text ?? "";
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
    const artSig = JSON.stringify([step.question || "", step.artifacts || []]);
    if (entry.artSig !== artSig) {
      entry.arts.replaceChildren(...stepShows(run.id, step));
      entry.artSig = artSig;
    }
    if (entry.call?.log !== step.log) entry.call = step.log ? callPanel(run.id, step.log) : null;
    if (entry.askedAgain?.log !== step.asked_again) entry.askedAgain = step.asked_again ? callPanel(run.id, step.asked_again, ASKED_AGAIN_TITLE) : null;
    if (entry.earlierCount !== (step.earlier?.length || 0)) {
      entry.earlierCount = step.earlier?.length || 0;
      entry.earlier = entry.earlierCount ? earlierPanel(run, step.earlier) : null;
    }
    if (step.children?.[0]?.kind === "item" && !entry.perItem) {
      entry.perItem = true;
      entry.kids.replaceChildren();
    }
    if (step.children?.length) syncRows(entry.kids, step.children, run, step.uses);
    const sig = JSON.stringify({ ...step, artifacts: undefined, children: undefined, earlier: undefined, cost: holdsSteps(step) ? undefined : step.cost, run: run.status });
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
  if (entry.open && entry.askedAgain?.el.open) entry.askedAgain.load();
}

function toggleRow(entry) {
  entry.open = !entry.open;
  if (entry.open && entry.call && ["running", "waiting"].includes(entry.run.status)) entry.call.el.open = true;
  redrawRow(entry);
}

function stepStats(step) {
  const stats = [];
  if (step.started_at && step.status !== "running") stats.push(seconds(step.started_at, step.ended_at));
  if (step.cost && !holdsSteps(step)) stats.push(money(step.cost));
  else if (step.tokens) stats.push(`${(step.tokens / 1000).toFixed(step.tokens < 10000 ? 1 : 0)}k tokens`);
  return stats.join(" · ");
}

function stepRow(entry) {
  const { run, step } = entry;
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
      step.rounds ? h("span", { class: "chip repeat" }, `round ${step.rounds}`) : step.round ? h("span", { class: "chip flag" }, `attempt ${step.round + 1}`) : null,
      step.resumed ? h("span", { class: "chip flag", title: `Resumes the session of ${step.resumed}` }, "resumed") : null,
      step.asked_again ? h("span", { class: "chip flag", title: ASKED_AGAIN }, "re-prompted") : null,
      step.asks?.length ? h("span", { class: "chip flag", title: "Asked questions before answering" }, "asked") : null,
      step.always && !runView.grouped.has(keyOf(step)) ? h("span", { class: "chip flag", title: "Also runs after failure, stop or reject" }, "always") : null),
    summaryLine(step, open)),
  h("span", { class: "srow-stats" }, stepStats(step)),
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
  if (step.status === "waiting") return h("span", { class: "srow-line" }, openAsk(step) ? "Waiting: questions above" : "Waiting: decision above");
  const text = step.headline || (open ? "" : firstLine(step.error) || step.ended);
  return text ? h("span", { class: "srow-line" }, text) : null;
}

function stepBody(run, step, entry) {
  return [
    step.error ? h("div", { class: "error-box" }, step.error) : null,
    step.ended ? h("div", { class: "ended-box" }, h("strong", {}, "Ended the run: "), step.ended) : null,
    step.sent_back ? h("div", { class: "hint" }, h("strong", {}, "Sent back with: "), step.sent_back) : null,
    step.loop ? h("div", { class: "hint" }, step.loop) : null,
    step.kind === "agent" && step.ended_at ? h("div", { class: "hint" }, step.resumed ? `Resumed the session of ${step.resumed}` : "New session",
      step.session ? ` · session ${step.session}` : " · no session id, not resumable") : null,
    entry.arts,
    stepFields(step),
    asksPanel(run.id, step),
    readsPanel(run, step, entry.chain),
    entry.call?.el,
    step.asked_again ? h("div", { class: "hint" }, ASKED_AGAIN) : null,
    entry.askedAgain?.el,
    entry.earlier,
    h("div", { class: "srow-links" }, editLinks(step, entry.chain)),
  ];
}

function editLinks(step, chain) {
  const target = { tool: ["tool", `Open tool ${step.uses}`], agent: ["prompt", `Open prompt ${step.uses}`] }[step.kind];
  const page = step.kind === "agent" ? `${encodeURIComponent(chain)}/${encodeURIComponent(step.uses)}` : encodeURIComponent(step.uses);
  return [
    target && step.uses ? h("a", { href: `#/${target[0]}/${page}` }, target[1]) : null,
    chain ? h("a", { href: `#/chain/${encodeURIComponent(chain)}/${step.id}` }, "Edit step") : null,
  ];
}

async function stopRun(id) {
  if (!confirm("Stop run? \"always\" steps still run.")) return;
  try { await api(`/api/runs/${encodeURIComponent(id)}/stop`, { method: "POST" }); } catch (err) { alert(err.message); }
}

function resumeStep(steps) {
  const step = (steps || []).find((s) => s.status !== "done" && s.status !== "skipped");
  return step ? resumeStep(step.children) || step : null;
}

function rerunPoints(run) {
  const failed = resumeStep(run.steps);
  if (!failed) return [];
  const byPath = (path) => findStepIn(run.steps, (s) => keyOf(s) === path);
  const points = [];
  let path = keyOf(failed);
  while (path) {
    const dot = path.lastIndexOf(".");
    const prefix = path.slice(0, dot + 1);
    const parent = prefix ? byPath(prefix.slice(0, -1)) : null;
    const chain = lib.chains.find((c) => c.name === (parent ? parent.uses : run.chain));
    for (const start of startsAround(outlineOf(chain?.steps || []), path.slice(dot + 1)) || []) {
      points.push({ step: byPath(prefix + start.step.id), level: start.level });
    }
    if (parent) points.push({ step: parent, level: parent.kind === "item" ? "this item" : "the whole sub-chain" });
    path = parent ? keyOf(parent) : "";
  }
  const seen = new Set([keyOf(failed)]);
  return points.filter(({ step }) => {
    if (!step || !SETTLED.has(step.status) || seen.has(keyOf(step))) return false;
    seen.add(keyOf(step));
    return true;
  });
}

async function rerunRun(id, from, step) {
  try {
    const res = await send("POST", `/api/runs/${encodeURIComponent(id)}/rerun`, step ? { from, step } : { from });
    loadRuns().catch(() => {});
    if (res.id !== id) location.hash = `#/run/${res.id}`;
  } catch (err) {
    alert(err.message);
  }
}

async function deleteRun(id) {
  if (!confirm("Delete run and its logs?")) return false;
  try {
    await api(`/api/runs/${encodeURIComponent(id)}`, { method: "DELETE" });
  } catch (err) {
    alert(err.message);
    return false;
  }
  loadRuns().catch(() => {});
  return true;
}

setInterval(() => {
  for (const el of document.querySelectorAll("[data-since]")) {
    if (!el.dataset.until) el.textContent = (el.dataset.tick === "seconds" ? seconds : duration)(el.dataset.since);
  }
}, 1000);
setInterval(() => loadRuns().catch(() => {}), 5000);
window.addEventListener("beforeunload", (e) => { if (editor?.dirty) e.preventDefault(); });
window.addEventListener("hashchange", refresh);
loadProjects().then(loadRuns).catch(() => {}).finally(refresh);
