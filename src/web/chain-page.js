function chainProblems(problems = [], warnings = [], draft = false) {
  if (problems.length) {
    return h("div", { class: "problems" }, h("strong", {}, draft ? "Unsaved; swarm check fails:" : "Cannot run:"), h("ul", {}, problems.map((p) => h("li", {}, p))));
  }
  if (warnings.length) {
    return h("div", { class: "problems warnings" }, h("strong", {}, draft ? "Unsaved; warnings:" : "Warnings:"),
      h("ul", {}, warnings.map((w) => h("li", {}, w))));
  }
  if (draft) return h("div", { class: "hint" }, "Unsaved; swarm check passes.");
  return null;
}

function growing(el) {
  const fit = () => {
    el.rows = Math.max(1, el.value.split("\n").length);
    el.style.height = "auto";
    if (el.scrollHeight > el.clientHeight) el.style.height = `${el.scrollHeight + el.offsetHeight - el.clientHeight}px`;
  };
  el.addEventListener("input", fit);
  fit();
  return el;
}

function startForm(chain) {
  const message = h("span", { class: "error-text" });
  const controls = (chain.inputs || []).map((i) => {
    const fromSettings = lib.input_values?.[i.name];
    const value = fromSettings || i.default || "";
    const el = i.optional ? h("input", { value, autocomplete: "off" }) : growing(h("textarea", {}, value));
    return { input: i, el, fromSettings };
  });
  const command = h("pre", { class: "example" });
  const showCommand = () => { command.textContent = cliCommand(chain, controls); };
  showCommand();
  const submit = async (e) => {
    e.preventDefault();
    message.textContent = editor?.dirty ? "Unsaved chain changes: save or discard first." : "";
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
  return h("form", { class: "panel start", onsubmit: submit, oninput: showCommand },
    h("div", { class: "editbar-title" }, "Start a run"),
    controls.length ? h("div", { class: "start-fields" }, controls.map(({ input, el, fromSettings }) =>
      h("label", { class: `field ${input.optional ? "" : "wide"}` },
        h("span", {}, input.label || input.name,
          input.optional ? h("span", { class: "hint" }, " optional") : null,
          fromSettings ? h("span", { class: "hint", title: "Project input default (Settings)" }, " · from Settings") : null),
        el))) : null,
    h("div", { class: "row" }, h("button", { class: "btn primary", type: "submit", disabled: !!chain.problems?.length }, "Start run"), message),
    h("div", { class: "start-cli" }, h("span", { class: "hint" }, "CLI:"), command));
}

function cliCommand(chain, controls) {
  const words = ["swarm", "run", chain.name, "-project", shellWord(project)];
  for (const { input, el, fromSettings } of controls) {
    const value = el.value.trim();
    if (value) words.push("-input", `${input.name}=${shellWord(value)}`);
    else if (!input.optional && !fromSettings && !input.default) words.push("-input", `${input.name}='...'`);
  }
  return words.join(" ");
}

function approvalBefore(step, steps) {
  return step.always ? null : steps.slice(0, steps.indexOf(step)).reverse().find((s) => kindOf(s) === "user") || null;
}
