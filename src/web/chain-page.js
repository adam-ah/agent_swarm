function chainProblems(problems = [], warnings = []) {
  if (problems.length) return h("div", { class: "problems" }, h("strong", {}, "This chain cannot run:"), h("ul", {}, problems.map((p) => h("li", {}, p))));
  if (warnings.length) return h("div", { class: "problems warnings" }, h("strong", {}, "This chain runs, but these look wrong:"), h("ul", {}, warnings.map((w) => h("li", {}, w))));
  return null;
}

function startForm(chain) {
  const message = h("span", { class: "error-text" });
  const controls = (chain.inputs || []).map((i) => {
    const fromSettings = lib.input_values?.[i.name];
    const value = fromSettings || i.default || "";
    const el = i.optional
      ? h("input", { value, autocomplete: "off", placeholder: "leave empty to skip" })
      : h("textarea", { rows: 6, placeholder: "Describe it in plain words" }, value);
    return { input: i, el, fromSettings };
  });
  const submit = async (e) => {
    e.preventDefault();
    message.textContent = editor?.dirty ? "Save or discard your changes to the chain first." : "";
    if (editor?.dirty) return;
    try {
      const values = Object.fromEntries(controls.map((c) => [c.input.name, c.el.value]));
      const { id } = await send("POST", "/api/runs", { chain: chain.name, inputs: values });
      await loadRuns();
      location.hash = `#/run/${id}`;
    } catch (err) {
      message.textContent = err.message;
    }
  };
  return h("form", { class: "panel start", onsubmit: submit },
    h("div", { class: "editbar-title" }, "Start a run"),
    controls.length ? h("div", { class: "start-fields" }, controls.map(({ input, el, fromSettings }) =>
      h("label", { class: `field ${input.optional ? "" : "wide"}` },
        h("span", {}, input.label || input.name,
          input.optional ? h("span", { class: "hint" }, " optional") : null,
          fromSettings ? h("span", { class: "hint", title: "This project's starting value, set in Settings" }, " · from Settings") : null),
        el))) : null,
    h("div", { class: "row" }, h("button", { class: "btn primary", type: "submit", disabled: !!chain.problems?.length }, "Start run"), message));
}

function approvalBefore(step, steps) {
  return step.always ? null : steps.slice(0, steps.indexOf(step)).reverse().find((s) => kindOf(s) === "user") || null;
}
