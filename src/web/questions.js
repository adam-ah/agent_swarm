const LEFT_TO_AGENT = "left to the agent";

function openAsk(step) {
  const last = (step.asks || []).at(-1);
  return last && !last.answered_at ? last : null;
}

const answerOf = (ask, i) => (ask.answers?.[i] || "").trim() || LEFT_TO_AGENT;

function questionsForm(runID, ask) {
  const boxes = ask.questions.map(() => h("textarea", { rows: 2, placeholder: `Your answer; leave it empty and it is ${LEFT_TO_AGENT}` }));
  const post = async (body, button) => {
    button.disabled = true;
    try { await send("POST", `/api/runs/${encodeURIComponent(runID)}/decide`, body); } catch (err) { alert(err.message); button.disabled = false; }
  };
  return h("div", { class: "decide answers" },
    ask.questions.map((q, i) => h("label", { class: "ask" }, h("span", { class: "question" }, q), boxes[i])),
    h("div", { class: "row" },
      h("button", { class: "btn approve", type: "button", onclick: (e) => post({ choice: "answer", answers: boxes.map((b) => b.value) }, e.target) }, "Send answers"),
      h("button", { class: "btn", type: "button", title: "It makes the call on every question itself and goes on", onclick: (e) => post({ choice: "decide" }, e.target) }, "Decide yourself")));
}

function asksPanel(runID, step) {
  const asks = (step.asks || []).filter((a) => a.answered_at);
  if (!asks.length) return null;
  const count = asks.reduce((n, a) => n + a.questions.length, 0);
  return h("details", { class: "output asks", open: true }, h("summary", {}, `Questions it asked you (${count})`),
    asks.map((ask, n) => h("div", { class: "ask-round" },
      h("dl", { class: "fields" }, ask.questions.flatMap((q, i) => [h("dt", {}, q), h("dd", { class: answerOf(ask, i) === LEFT_TO_AGENT ? "hint" : "" }, answerOf(ask, i))])),
      ask.log ? callPanel(runID, ask.log, `How it went on after your answers${asks.length > 1 ? ` (round ${n + 1})` : ""}`).el : null)));
}
