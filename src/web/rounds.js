const roundOf = (step) => Number.parseInt(step.rounds, 10) || 1;

function roundsOf(steps) {
  const byRound = new Map();
  steps.forEach((step, order) => {
    for (const attempt of (step.earlier || []).filter((s) => s.started_at)) {
      const round = roundOf(attempt);
      byRound.set(round, [...(byRound.get(round) || []), { attempt, order }]);
    }
  });
  return [...byRound].sort(([a], [b]) => a - b).map(([round, list]) => ({
    round,
    steps: list.sort((a, b) => new Date(a.attempt.started_at) - new Date(b.attempt.started_at) || a.order - b.order).map((x) => x.attempt),
  }));
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
      asksPanel(run.id, step),
      step.log ? callPanel(run.id, step.log).el : null,
      step.asked_again ? callPanel(run.id, step.asked_again, ASKED_AGAIN_TITLE).el : null)),
  `${step.path}@${step.started_at}`, opened);
}
