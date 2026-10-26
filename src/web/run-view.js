function showRun(id) {
  main.replaceChildren(h("div", { class: "page" }, h("div", { class: "empty" }, "Loading…")));
  runView = { id, entries: new Map(), status: null };
  stream = new EventSource(`/api/runs/${encodeURIComponent(id)}/events`);
  stream.onmessage = (event) => renderRun(JSON.parse(event.data));
}

function renderRun(run) {
  let page = main.querySelector(".page[data-run]");
  if (!page || page.dataset.run !== run.id) {
    page = h("div", { class: "page", "data-run": run.id },
      h("div", { class: "eyebrow" }, h("a", { href: `#/chain/${run.chain}` }, run.title)),
      h("h1", { id: "subject" }),
      h("div", { class: "run-meta", id: "meta" }),
      h("div", { id: "banner" }),
      h("div", { class: "split" },
        h("div", { class: "panel sticky" }, h("div", { class: "panel-head" }, "Tree"), h("div", { class: "tree", id: "run-tree" })),
        h("div", { class: "timeline", id: "steps" })));
    main.replaceChildren(page);
    runView.entries = new Map();
  }
  const label = run.subject || Object.entries(run.inputs || {}).map(([k, v]) => `${k} ${v}`).join(", ") || run.title;
  page.querySelector("#subject").replaceChildren(run.link ? h("a", { href: run.link, target: "_blank", rel: "noreferrer" }, label) : label);

  const live = run.status === "running" || run.status === "waiting";
  page.querySelector("#meta").replaceChildren(...[
    h("span", { class: "pill" }, h("span", { class: `dot ${run.status}` }), STATUS_LABEL[run.status] || run.status),
    h("span", {}, `started ${new Date(run.started_at).toLocaleTimeString()}`),
    h("span", { class: "sep" }, "·"),
    h("span", { "data-since": run.started_at, "data-until": run.ended_at || "" }, duration(run.started_at, run.ended_at)),
    run.cost ? h("span", { class: "sep" }, "·") : null,
    run.cost ? h("span", {}, money(run.cost)) : null,
    live ? h("button", { class: "btn danger", style: "margin-left:auto", onclick: () => stopRun(run.id) }, "Stop run") : null,
  ].filter(Boolean));

  const banner = page.querySelector("#banner");
  if (!live && run.reason) banner.replaceChildren(h("div", { class: `banner ${run.status}` }, h("strong", {}, STATUS_LABEL[run.status] || run.status), run.reason));
  else if (!live && run.status === "done") banner.replaceChildren(h("div", { class: "banner done" }, h("strong", {}, "Done"), "Every step finished."));
  else banner.replaceChildren();

  page.querySelector("#run-tree").replaceChildren(
    treeRow({ kind: "root", title: run.title, state: run.status, time: duration(run.started_at, run.ended_at), onclick: () => window.scrollTo({ top: 0, behavior: "smooth" }) }),
    runTreeRows(run.steps));
  syncCards(page.querySelector("#steps"), run.steps, run, run.chain);

  if (runView.status !== run.status) {
    runView.status = run.status;
    loadRuns();
  }
}

function runTreeRows(steps) {
  return h("div", { class: "tree-kids" }, steps.map((step) => [
    treeRow({
      kind: step.kind, title: step.title, state: step.status,
      time: step.status === "running" ? "" : step.started_at ? duration(step.started_at, step.ended_at) : "",
      onclick: () => focusCard(keyOf(step)),
    }),
    step.children?.length ? runTreeRows(step.children) : null,
  ]));
}

const keyOf = (step) => step.path || step.id;

function focusCard(path) {
  const card = document.getElementById(`card-${path}`);
  if (!card) return;
  card.scrollIntoView({ behavior: "smooth", block: "start" });
  card.classList.add("flash");
  setTimeout(() => card.classList.remove("flash"), 1200);
}

function syncCards(container, steps, run, chain) {
  steps.forEach((step, i) => {
    let entry = runView.entries.get(keyOf(step));
    if (!entry) {
      entry = { el: h("div"), sig: null, artSig: null, arts: h("div", { class: "artifacts" }), kids: h("div", { class: "timeline" }), chain };
      runView.entries.set(keyOf(step), entry);
      container.append(entry.el);
    }
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
    if (step.children?.length) syncCards(entry.kids, step.children, run, step.uses);
    const sig = JSON.stringify({ ...step, artifacts: undefined, children: undefined, earlier: undefined, cost: step.kind === "chain" ? undefined : step.cost });
    if (entry.sig !== sig) {
      const fresh = stepCard(run, step, i, entry);
      entry.el.replaceWith(fresh);
      entry.el = fresh;
      entry.sig = sig;
      if (entry.call?.el.open) entry.call.load();
    }
  });
}

function stepFields(step) {
  const fields = Object.entries(step.output || {}).filter(([k, v]) => k !== "summary" && v !== "" && !(step.kind === "tool" && k === "output" && Object.keys(step.output).length > 1));
  if (!fields.length || step.kind === "approval") return null;
  return h("details", { class: "output" }, h("summary", {}, step.kind === "agent" ? "What the agent returned" : "Output"),
    h("dl", { class: "fields" }, fields.flatMap(([k, v]) => [h("dt", {}, k), h("dd", {}, v)])));
}

function earlierPanel(run, earlier) {
  return h("details", { class: "output earlier" }, h("summary", {}, `Earlier attempts (${earlier.length})`),
    earlier.map((step, n) => h("div", { class: "attempt" },
      h("div", { class: "attempt-head" }, `Attempt ${n + 1}`, h("span", { class: "card-stats" }, step.started_at ? duration(step.started_at, step.ended_at) : step.status)),
      step.headline ? h("div", { class: "headline" }, step.headline) : null,
      step.error ? h("div", { class: "error-box" }, step.error) : null,
      stepFields(step),
      step.log ? callPanel(run.id, step.log).el : null)));
}

const sizeOf = (text) => (text.length < 1024 ? `${text.length} chars` : `${(text.length / 1024).toFixed(1)} KB`);
const shellWord = (s) => (/^[\w@%+=:,./-]+$/.test(s) ? s : `'${s.replaceAll("'", `'\\''`)}'`);

function elapsed(from, to) {
  if (!from) return "";
  const ms = (to ? new Date(to) : new Date()) - new Date(from);
  if (ms < 1000) return `${Math.max(0, ms)}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return duration(from, to);
}

function remembered(el, key, opened) {
  el.open = opened.get(key) ?? el.open;
  el.addEventListener("toggle", () => opened.set(key, el.open));
  return el;
}

function block(label, text, opened, always = false) {
  if (!text && !always) return null;
  if (!text) return h("div", { class: "block-empty" }, label, h("span", { class: "hint" }, " printed nothing"));
  return remembered(h("details", { class: "block", open: text.length < 600 },
    h("summary", {}, label, h("span", { class: "hint" }, ` ${sizeOf(text)}`)), h("pre", {}, text)), label, opened);
}

function actionLabel(action) {
  let input = action.input;
  try {
    const parsed = JSON.parse(input);
    input = ["command", "file_path", "path", "pattern", "url", "query", "description", "prompt"].map((k) => parsed?.[k]).find((v) => typeof v === "string") ?? JSON.stringify(parsed);
  } catch {}
  return input.split("\n").find((line) => line.trim()) || "";
}

function callView(runID, log, call, opened) {
  const ended = call.ended_at;
  const exit = call.exit_code === undefined ? null : call.exit_code === 0 ? "exit 0" : call.exit_code === -1 ? "killed" : `exit ${call.exit_code}`;
  const long = [];
  const words = call.command.map((arg, i) => {
    if (arg.length <= 160 && !arg.includes("\n")) return shellWord(arg);
    long.push([i, arg]);
    return `<argument ${i}>`;
  });
  const out = [
    h("div", { class: "call-meta" },
      h("span", {}, `started ${new Date(call.started_at).toLocaleTimeString()}`),
      h("span", {}, ended ? `ran ${elapsed(call.started_at, ended)}` : "still running"),
      exit ? h("span", { class: call.exit_code === 0 ? "good" : "bad" }, exit) : null,
      h("span", { class: "hint" }, `in ${call.dir}`)),
    h("pre", { class: "command" }, `$ ${words.join(" ")}${call.stdin ? " < stdin" : ""}`),
    long.map(([i, arg]) => block(`argument ${i}`, arg, opened)),
    block("stdin", call.stdin, opened),
  ];
  if (call.error) out.push(h("div", { class: "error-box" }, call.error));
  if (call.actions?.length) {
    out.push(h("div", { class: "actions-head" }, `Tool calls (${call.actions.length})`),
      h("div", { class: "actions" }, call.actions.map((a, n) => remembered(
        h("details", { class: `action ${a.failed ? "failed" : ""}` },
          h("summary", {},
            h("span", { class: "action-tool" }, a.tool),
            h("span", { class: "action-input" }, actionLabel(a)),
            h("span", { class: "action-time" }, a.ended_at ? elapsed(a.started_at, a.ended_at) : "running")),
          h("div", { class: "action-body" }, h("div", { class: "hint" }, "input"), h("pre", {}, a.input),
            a.ended_at ? [h("div", { class: "hint" }, a.failed ? "output (failed)" : "output"), h("pre", {}, a.output || "(empty)")] : null)),
        `action ${a.id || n}`, opened))));
  }
  out.push(block("result", call.result, opened), block("stdout", call.stdout, opened, !call.stream && !!ended), block("stderr", call.stderr, opened, !!ended));
  if (call.stream) {
    const pre = h("pre", {}, "Loading…");
    const el = remembered(h("details", { class: "block" }, h("summary", {}, call.result ? "full output stream" : "stdout"), pre), "stream", opened);
    const fill = () => fetch(`/api/runs/${encodeURIComponent(runID)}/call?log=${encodeURIComponent(log)}&part=stream`).then((r) => r.text()).then((t) => { pre.textContent = t || "(empty)"; });
    el.addEventListener("toggle", () => { if (el.open) fill(); });
    if (el.open) fill();
    out.push(el);
  }
  return out;
}

function callPanel(runID, log) {
  const opened = new Map();
  const body = h("div", { class: "call-body" }, "Loading…");
  const el = h("details", { class: "output call" }, h("summary", {}, "How it was called"), body);
  const load = async () => {
    try {
      body.replaceChildren(...callView(runID, log, await api(`/api/runs/${encodeURIComponent(runID)}/call?log=${encodeURIComponent(log)}`), opened).flat().filter(Boolean));
    } catch (err) {
      body.replaceChildren(h("p", { class: "hint" }, err.message));
    }
  };
  el.addEventListener("toggle", () => { if (el.open) load(); });
  return { log, el, load };
}

function stepCard(run, step, i, entry) {
  const stats = [];
  if (step.started_at && step.status !== "running") stats.push(duration(step.started_at, step.ended_at));
  if (step.cost && step.kind !== "chain") stats.push(money(step.cost));
  else if (step.tokens) stats.push(`${(step.tokens / 1000).toFixed(step.tokens < 10000 ? 1 : 0)}k tokens`);
  const body = [];
  if (step.headline) body.push(h("div", { class: "headline" }, step.headline));
  else if (step.status === "pending" && step.kind !== "approval" && step.kind !== "chain") body.push(h("div", { class: "rule" }, step.kind === "agent" ? `prompt prompts/${step.uses}.md` : `tool ${step.uses}`));
  if (step.status === "running" && step.kind !== "chain") {
    body.push(h("div", { class: "working" }, h("span", { class: "bars" }, h("i"), h("i"), h("i")),
      `${step.kind === "agent" ? `Working · ${step.actions || 0} actions` : "Running"} · `, h("span", { "data-since": step.started_at }, duration(step.started_at))));
  }
  if (step.error) body.push(h("div", { class: "error-box" }, step.error));
  if (step.ended) body.push(h("div", { class: "ended-box" }, h("strong", {}, "Ended the run: "), step.ended));
  if (step.status === "waiting") body.push(h("div", { class: "decide" }, h("div", { class: "question" }, step.question)));
  body.push(entry.arts);
  if (step.status === "waiting") body.push(decideForm(run.id, step));
  body.push(stepFields(step));
  if (entry.call) body.push(entry.call.el);
  if (entry.earlier) body.push(entry.earlier);
  if (step.kind === "chain") body.push(entry.kids);
  return h("div", { class: `card ${step.status} ${step.kind}`, id: `card-${keyOf(step)}` },
    h("div", { class: "card-head" },
      h("span", { class: `state ${step.status}` }, STATE_ICON[step.status] || ""),
      h("span", { class: "card-title" }, step.title),
      h("span", { class: `chip ${step.kind}` }, step.runner ? `${KIND_NAME[step.kind]} · ${step.runner}` : KIND_NAME[step.kind]),
      step.round ? h("span", { class: "chip flag" }, `attempt ${step.round + 1}`) : null,
      step.always ? h("span", { class: "chip flag" }, "always runs") : null,
      h("span", { class: "card-stats" }, stats.join(" · ")),
      h("span", { class: "card-edit" }, editLinks(step, entry.chain))),
    ...body);
}

function editLinks(step, chain) {
  const target = { tool: ["tool", "Edit tool"], agent: ["prompt", "Edit prompt"], chain: ["chain", "Edit chain"] }[step.kind];
  return [
    target && step.uses ? h("a", { href: `#/${target[0]}/${encodeURIComponent(step.uses)}`, title: `Open ${step.uses}` }, target[1]) : null,
    chain ? h("a", { href: `#/chain/${encodeURIComponent(chain)}/${step.id}`, title: `Open this step in the ${chain} chain` }, "Edit step") : null,
  ];
}

function decideForm(runID, step) {
  const note = h("textarea", { placeholder: step.revise ? "Note: what to change, if you send it back" : "Optional note" });
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
    h("button", { class: "btn approve", onclick: (e) => decide("approve", e.target) }, "Approve"),
    step.revise ? h("button", { class: "btn", onclick: (e) => decide("revise", e.target) }, `Revise: back to ${step.revise}`) : null,
    h("button", { class: "btn", onclick: (e) => decide("reject", e.target) }, "Reject")));
}

function renderArtifacts(runID, artifacts) {
  const out = [];
  for (const a of artifacts.filter((x) => x.kind === "video")) {
    out.push(h("figure", { class: "video" },
      h("video", { src: `${fileURL(runID, a.path)}#t=1`, controls: true, preload: "metadata", playsinline: true }),
      h("figcaption", { class: "caption" }, a.name)));
  }
  const images = artifacts.filter((x) => x.kind === "image");
  if (images.length) {
    const urls = images.map((a) => fileURL(runID, a.path));
    out.push(h("div", { class: "stills" }, images.map((a, i) =>
      h("figure", { class: "still", onclick: () => openLightbox(urls, i) }, h("img", { src: urls[i], loading: "lazy", alt: a.name }), h("figcaption", {}, a.name)))));
  }
  for (const a of artifacts.filter((x) => x.kind !== "video" && x.kind !== "image")) {
    const body = h("div", { class: "doc-body" }, "Loading…");
    const doc = h("div", { class: "doc" }, h("div", { class: "doc-head" }, a.name), body);
    const more = h("button", { class: "doc-more", onclick: () => { doc.classList.toggle("open"); more.textContent = doc.classList.contains("open") ? "Show less" : "Show all"; } }, "Show all");
    doc.append(more);
    fetch(fileURL(runID, a.path, a.kind === "markdown" ? "doc" : "file")).then((r) => r.text()).then((text) => {
      if (a.kind === "markdown") body.innerHTML = text;
      else if (a.kind === "diff") body.replaceChildren(diffView(text));
      else body.replaceChildren(h("pre", { class: "plain" }, a.kind === "json" ? prettyJSON(text) : text));
      requestAnimationFrame(() => { if (body.scrollHeight <= body.clientHeight + 4) more.remove(); });
    });
    out.push(doc);
  }
  return out;
}

function diffView(text) {
  if (!text.trim()) return h("p", { class: "hint" }, "No changes.");
  return h("pre", { class: "diff" }, text.split("\n").map((line) => {
    const kind = line.startsWith("+++") || line.startsWith("---") || line.startsWith("diff ") || line.startsWith("index ") ? "meta"
      : line.startsWith("@@") ? "hunk" : line.startsWith("+") ? "add" : line.startsWith("-") ? "del" : "";
    return h("span", { class: kind }, `${line}\n`);
  }));
}

function prettyJSON(text) {
  try { return JSON.stringify(JSON.parse(text), null, 2); } catch { return text; }
}

let lightboxState = null;
function openLightbox(urls, index) {
  lightboxState = { urls, index };
  lightbox.querySelector("img").src = urls[index];
  lightbox.hidden = false;
}
lightbox.addEventListener("click", () => { lightbox.hidden = true; });
document.addEventListener("keydown", (e) => {
  if (lightbox.hidden || !lightboxState) return;
  if (e.key === "Escape") lightbox.hidden = true;
  if (e.key === "ArrowRight" || e.key === "ArrowLeft") {
    const n = lightboxState.urls.length;
    lightboxState.index = (lightboxState.index + (e.key === "ArrowRight" ? 1 : n - 1)) % n;
    lightbox.querySelector("img").src = lightboxState.urls[lightboxState.index];
  }
});

async function stopRun(id) {
  if (!confirm("Stop this run? Steps marked \"always runs\" still run.")) return;
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
