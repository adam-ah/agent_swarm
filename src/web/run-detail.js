function stepFields(step) {
  const fields = Object.entries(step.output || {}).filter(([k, v]) => k !== "summary" && asText(v) !== "" && !(step.kind === "tool" && k === "output" && Object.keys(step.output).length > 1));
  if (!fields.length || step.kind === "user") return null;
  return h("details", { class: "output", open: step.kind === "agent" }, h("summary", {}, step.kind === "agent" ? "What the agent returned" : "Output"),
    h("dl", { class: "fields" }, fields.flatMap(([k, v]) => [h("dt", {}, k), h("dd", {}, Array.isArray(v) ? h("ol", {}, v.map((item) => h("li", {}, asText(item)))) : asText(v))])));
}

function readsOf(def, chainName = editor?.name) {
  const own = new Set(Object.keys(def.with || {}));
  const scoped = [def.tool ? lib.tools[def.tool]?.run : "", def.agent ? promptOf(chainName, def.agent)?.text : "", kindOf(def) === "user" ? [def.user, ...(def.show || [])].join("\n") : ""];
  const names = [...placeholdersIn(Object.values(def.with || {}).join("\n")), ...placeholdersIn(scoped.join("\n")).filter((n) => !own.has(n))];
  const when = readCondition(def.when);
  if (when) names.push(when.name);
  return [...new Set(names)];
}

function readsList(names, chain, go, valueOf) {
  return h("div", { class: "reads-list" }, names.map((name) => {
    const from = sourceOf(name, chain);
    const now = valueOf ? valueOf(name, from) : undefined;
    const text = String(now ?? "");
    return h("div", { class: "read" },
      h("code", { class: "ol-name" }, name),
      sourceNote(from, go),
      now === undefined ? null : h("span", { class: "ol-value", title: text }, text ? `value “${text.length > 80 ? `${text.slice(0, 80)}…` : text}”` : "value empty"));
  }));
}

function readsPanel(run, step, chainName) {
  const chain = lib.chains.find((c) => c.name === chainName);
  const def = chain?.steps?.find((s) => s.id === step.id);
  const names = def ? readsOf(def, chainName) : [];
  if (!names.length) return null;
  const prefix = keyOf(step).slice(0, -step.id.length);
  return h("details", { class: "output reads", open: true }, h("summary", {}, `What it read (${names.length})`),
    readsList(names, chain, runGo(chainName, prefix), (name, from) => valueNow({ name, from, prefix }, run)));
}

function earlierPanel(run, earlier) {
  return h("details", { class: "output earlier" }, h("summary", {}, `Earlier attempts (${earlier.length})`),
    earlier.map((step, n) => h("div", { class: "attempt" },
      h("div", { class: "attempt-head" }, `Attempt ${n + 1}`, h("span", { class: "srow-stats" }, step.started_at ? duration(step.started_at, step.ended_at) : step.status)),
      step.headline ? h("div", { class: "headline" }, step.headline) : null,
      step.error ? h("div", { class: "error-box" }, step.error) : null,
      stepFields(step),
      step.log ? callPanel(run.id, step.log).el : null,
      step.asked_again ? callPanel(run.id, step.asked_again, ASKED_AGAIN_TITLE).el : null)));
}

const ASKED_AGAIN = "Its answer had no JSON object with every field, so it was asked for them again in the same session";
const ASKED_AGAIN_TITLE = "How it was asked again for its fields";

const sizeOf = (text) => (text.length < 1024 ? `${text.length} chars` : `${(text.length / 1024).toFixed(1)} KB`);
const shellWord = (s) => (/^[\w@%+=:,./-]+$/.test(s) ? s : `'${s.replaceAll("'", `'\\''`)}'`);
const firstLine = (text) => (text || "").split("\n").find((line) => line.trim()) || "";

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
  return firstLine(input);
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
      h("span", { class: "hint" }, `in ${call.dir}`),
      call.limits ? h("span", { class: "hint" }, call.limits) : null),
    h("pre", { class: "command" }, `$ ${words.join(" ")}${call.stdin ? " < stdin" : ""}`),
    long.map(([i, arg]) => block(`argument ${i}`, arg, opened)),
    block("stdin", call.stdin, opened),
  ];
  if (call.error) out.push(h("div", { class: "error-box" }, call.error));
  if (call.stopped?.length) out.push(processList(`Processes swarm stopped when the step ended (${call.stopped.length})`, call.stopped, opened));
  if (call.kept?.length) out.push(processList(`Left running until ${call.kept_until} ends (${call.kept.length})`, call.kept, opened));
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

function processList(title, processes, opened) {
  return remembered(h("details", { class: "block" }, h("summary", {}, title), h("pre", {}, processes.join("\n"))), title, opened);
}

function callPanel(runID, log, title = "How it was called") {
  const opened = new Map();
  const body = h("div", { class: "call-body" }, "Loading…");
  const el = h("details", { class: "output call" }, h("summary", {}, title), body);
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
    const more = h("button", { class: "doc-more", type: "button", onclick: () => { doc.classList.toggle("open"); more.textContent = doc.classList.contains("open") ? "Show less" : "Show all"; } }, "Show all");
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
