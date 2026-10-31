let showIdle = false;
const NOT_RAN = new Set(["pending", "skipped"]);

function setShowIdle(on) {
  showIdle = on;
  document.getElementById("steps")?.classList.toggle("hide-idle", !on);
  const box = document.getElementById("show-idle");
  if (box) box.checked = on;
}

const idleNotes = new WeakMap();

function idleNote(container, steps, run) {
  const idle = steps.filter((s) => s.kind !== "item");
  const pending = idle.filter((s) => s.status === "pending").length;
  const skipped = idle.filter((s) => s.status === "skipped").length;
  const live = run.status === "running" || run.status === "waiting";
  const text = [pending ? `${pending} ${live ? "upcoming" : "not run"}` : "", skipped ? `${skipped} skipped` : ""].filter(Boolean).join(" · ");
  let note = idleNotes.get(container);
  if (!text) {
    note?.remove();
    return;
  }
  if (!note) {
    note = h("button", { class: "idle-note", type: "button", title: "Show pending and skipped steps", onclick: () => setShowIdle(true) });
    idleNotes.set(container, note);
  }
  note.textContent = text;
  container.append(note);
}

const perItem = (step) => step.children?.[0]?.kind === "item";

function activeStep(step) {
  const kid = (step.children || []).find((s) => s.status === "running" || s.status === "waiting");
  if (!kid || !holdsSteps(kid) || perItem(kid)) return kid || null;
  return activeStep(kid) || kid;
}

const COUNTED = ["done", "running", "waiting", "failed", "stopped", "rejected", "pending", "skipped"];

function itemCounts(step) {
  const statuses = step.children.map((s) => s.status);
  return COUNTED.map((status) => [statuses.filter((s) => s === status).length, status]).filter(([n]) => n).map(([n, status]) => `${n} ${status}`).join(" · ");
}

function progressOf(step) {
  if (perItem(step)) return itemCounts(step);
  const active = activeStep(step);
  return active && { ...active, children: perItem(active) ? itemCounts(active) : undefined, earlier: undefined, output: undefined };
}

function itemTitle(step) {
  if (step.label) return step.label;
  const inRun = `/runs/${runView.id}/`;
  const at = step.title.indexOf(inRun);
  return at >= 0 ? step.title.slice(at + inRun.length) : step.title;
}

function stepStats(step) {
  const stats = [];
  if (step.started_at && step.status !== "running") stats.push(seconds(step.started_at, step.ended_at));
  if (step.cost && !holdsSteps(step)) stats.push(money(step.cost));
  else if (step.tokens) stats.push(`${(step.tokens / 1000).toFixed(step.tokens < 10000 ? 1 : 0)}k tokens`);
  return stats.join(" · ");
}

function rowStats(step) {
  const stats = stepStats(step);
  if (!holdsSteps(step) || step.status !== "running" || !step.started_at) return h("span", { class: "srow-stats" }, stats);
  return h("span", { class: "srow-stats" }, h("span", { "data-since": step.started_at, title: "Elapsed" }, duration(step.started_at)), stats ? ` · ${stats}` : "");
}

function summaryLine(step, open) {
  if (holdsSteps(step) && (step.status === "running" || step.status === "waiting")) {
    if (perItem(step)) return h("span", { class: "srow-line" }, [itemCounts(step), step.headline].filter(Boolean).join(" · "));
    const active = activeStep(step);
    if (active) return h("span", { class: "srow-line" }, h("span", { class: "srow-at" }, active.title, active.rounds ? ` (round ${active.rounds})` : ""), " · ", activeLine(active));
  }
  if (step.status === "running" && !holdsSteps(step)) return h("span", { class: "srow-line working" }, activeLine(step));
  if (step.status === "waiting") return h("span", { class: "srow-line" }, activeLine(step));
  const text = step.headline || (open ? "" : firstLine(step.error) || step.ended);
  return text ? h("span", { class: "srow-line" }, text) : null;
}

function activeLine(step) {
  if (perItem(step)) return itemCounts(step);
  if (step.status === "waiting") return openAsk(step) ? "Waiting: questions above" : "Waiting: decision above";
  return [h("span", { class: "bars" }, h("i"), h("i"), h("i")),
    `${step.kind === "agent" ? `Working · ${step.actions || 0} actions` : "Running"} · `, h("span", { "data-since": step.started_at }, duration(step.started_at))];
}
