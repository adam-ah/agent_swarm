const roundOf = (step) => Number.parseInt(step.rounds, 10) || 1;

function roundsOf(steps) {
  const attempts = steps.flatMap((step) => (step.earlier || []).filter((s) => s.started_at))
    .sort((a, b) => new Date(a.started_at) - new Date(b.started_at));
  const groups = [];
  for (const step of attempts) {
    const last = groups.at(-1);
    if (last?.round === roundOf(step) && !last.steps.some((s) => s.id === step.id)) last.steps.push(step);
    else groups.push({ round: roundOf(step), steps: [step] });
  }
  return groups;
}

function syncRounds(run) {
  for (const loop of runView.rounds) {
    const steps = loop.paths.map((p) => runView.entries.get(p)?.step).filter(Boolean);
    const groups = roundsOf(steps);
    const now = steps.length ? roundOf(steps[0]) : 1;
    const sig = JSON.stringify([now, groups.map((g) => g.steps.map((s) => `${s.path}@${s.started_at}`))]);
    if (sig === loop.sig) continue;
    loop.sig = sig;
    loop.el.replaceChildren(...groups.map((g) => pastRound(run, g, loop.total, loop.opened)),
      groups.length ? h("div", { class: "ol-label round-now" }, h("span", { class: "ol-word" }, `Round ${now} of ${loop.total}`)) : "");
  }
}

function pastRound(run, group, total, opened) {
  const first = group.steps[0];
  const last = group.steps.at(-1);
  const cost = group.steps.reduce((sum, s) => sum + (s.cost || 0), 0);
  const stats = [duration(first.started_at, last.ended_at || last.started_at), money(cost)].filter(Boolean).join(" · ");
  return remembered(h("details", { class: "round", open: true },
    h("summary", { class: "ol-label" }, h("span", { class: "ol-word" }, `Round ${group.round} of ${total}`), h("span", { class: "ol-note" }, stats)),
    group.steps.map((s) => pastStep(run, s, opened))), `round@${first.started_at}`, opened);
}

function pastStep(run, step, opened) {
  return remembered(h("details", { class: `srow past ${step.status} ${step.kind}` },
    h("summary", { class: "srow-head" },
      h("span", { class: `state ${step.status}` }, STATE_ICON[step.status] || ""),
      h("span", { class: `kind ${step.kind}`, title: KIND_NAME[step.kind] }, KIND_LETTER[step.kind]),
      h("span", { class: "srow-text" },
        h("span", { class: "srow-title" }, step.title),
        h("span", { class: "srow-line" }, step.headline || firstLine(step.error) || step.ended || "")),
      h("span", { class: "srow-stats" }, stepStats(step)),
      h("span", { class: "srow-caret" })),
    h("div", { class: "srow-body" },
      step.error ? h("div", { class: "error-box" }, step.error) : null,
      stepFields(step),
      step.log ? callPanel(run.id, step.log).el : null,
      step.asked_again ? callPanel(run.id, step.asked_again, ASKED_AGAIN_TITLE).el : null)),
  `${step.path}@${step.started_at}`, opened);
}
