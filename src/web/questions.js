const LEFT_TO_AGENT = "agent decides";

function openAsk(step) {
  const last = (step.asks || []).at(-1);
  return last && !last.answered_at ? last : null;
}

const answerOf = (ask, i) => (ask.answers?.[i] || "").trim() || LEFT_TO_AGENT;

function waitingSteps(steps, items = []) {
  return (steps || []).flatMap((step) => [
    ...(step.status === "waiting" ? [{ step, items }] : []),
    ...waitingSteps(step.children, step.kind === "item" ? [...items, step.title] : items),
  ]);
}

function renderDecision(run, el) {
  const sections = (run.status === "waiting" ? waitingSteps(run.steps) : []).map(({ step, items }) => {
    const key = `${keyOf(step)}:${step.round || 0}:${(step.asks || []).length}:${JSON.stringify(step.artifacts || [])}`;
    return [key, runView.decisions.get(key) || decisionSection(run.id, step, items)];
  });
  runView.decisions = new Map(sections);
  const keys = sections.map(([key]) => key).join("|");
  if (keys === runView.decisionKey) return;
  runView.decisionKey = keys;
  el.replaceChildren(...sections.map(([, section]) => section));
}

function decisionSection(runID, step, items) {
  const ask = openAsk(step);
  return h("section", { class: "decision" },
    h("div", { class: "decision-head" }, h("span", { class: "state waiting" }), [...items, ask ? `${step.title}: questions` : step.title].join(" · ")),
    ask ? questionsForm(runID, step, ask) : [stepShows(runID, step), decideForm(runID, step)]);
}

async function postDecision(runID, step, body, button) {
  button.disabled = true;
  try { await send("POST", `/api/runs/${encodeURIComponent(runID)}/decide`, { ...body, step: keyOf(step) }); } catch (err) { alert(err.message); button.disabled = false; }
}

function decideForm(runID, step) {
  const note = h("textarea", { rows: 2, placeholder: step.revise ? `Note (required to send back to ${step.revise})` : "Note (optional)" });
  const decide = (choice, button) => {
    if (choice === "revise" && !note.value.trim()) {
      note.placeholder = "Note required to send back";
      note.focus();
      return;
    }
    postDecision(runID, step, { choice, note: note.value }, button);
  };
  return h("div", { class: "decide" }, note, h("div", { class: "row" },
    h("button", { class: "btn approve", type: "button", onclick: (e) => decide("approve", e.target) }, "Approve"),
    step.revise ? h("button", { class: "btn", type: "button", onclick: (e) => decide("revise", e.target) }, `Send back to ${step.revise}`) : null,
    h("button", { class: "btn danger", type: "button", onclick: (e) => decide("reject", e.target) }, "Reject")));
}

function questionsForm(runID, step, ask) {
  const boxes = ask.questions.map(() => h("textarea", { rows: 2, placeholder: `Empty: ${LEFT_TO_AGENT}` }));
  return h("div", { class: "decide answers" },
    ask.questions.map((q, i) => h("label", { class: "ask" }, h("span", { class: "question" }, q), boxes[i])),
    h("div", { class: "row" },
      h("button", { class: "btn approve", type: "button", onclick: (e) => postDecision(runID, step, { choice: "answer", answers: boxes.map((b) => b.value) }, e.target) }, "Send answers"),
      h("button", { class: "btn", type: "button", title: "The agent answers every open question itself", onclick: (e) => postDecision(runID, step, { choice: "decide" }, e.target) }, "Let agent decide")));
}

function asksPanel(runID, step) {
  const asks = (step.asks || []).filter((a) => a.answered_at);
  if (!asks.length) return null;
  const count = asks.reduce((n, a) => n + a.questions.length, 0);
  return h("details", { class: "output asks", open: true }, h("summary", {}, `Questions (${count})`),
    asks.map((ask, n) => h("div", { class: "ask-round" },
      h("dl", { class: "fields" }, ask.questions.flatMap((q, i) => [h("dt", {}, q), h("dd", { class: answerOf(ask, i) === LEFT_TO_AGENT ? "hint" : "" }, answerOf(ask, i))])),
      ask.log ? callPanel(runID, ask.log, `Call after answers${asks.length > 1 ? ` (round ${n + 1})` : ""}`).el : null)));
}
