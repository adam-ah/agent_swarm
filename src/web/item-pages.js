function itemButtons(kind, name, source, status, payload, onSaved) {
  const save = async (scope = "") => {
    try {
      await putItem(kind, name, payload(), scope);
      await loadLibrary();
      status.className = "ok-text";
      status.textContent = scope ? "Saved as this project's copy" : "Saved";
      renderSide();
      onSaved?.();
      if (scope) refresh();
    } catch (err) {
      status.className = "error-text";
      status.textContent = err.message;
    }
  };
  const remove = async () => {
    const what = source === "project" ? "this project's copy" : "it from the shared library, for every project";
    if (!confirm(`Delete ${name}? This removes ${what}.`)) return;
    try {
      await deleteItem(kind, name);
      await loadLibrary();
      location.hash = "#/";
    } catch (err) {
      status.className = "error-text";
      status.textContent = err.message;
    }
  };
  return h("div", { class: "savebar" }, status,
    h("details", { class: "more" }, h("summary", { class: "btn small" }, "More"),
      h("div", { class: "more-menu" },
        source !== "project" ? h("button", { class: "linkish", type: "button", onclick: () => save("project") }, "Save as a copy for this project only") : null,
        h("button", { class: "linkish danger", type: "button", onclick: remove }, source === "project" ? "Delete this project's copy" : "Delete it for every project"))),
    h("button", { class: "btn small primary", type: "button", onclick: () => save() }, "Save"));
}

const unsaved = (status) => () => { status.className = "status"; status.textContent = "Unsaved"; };

function showTool(name) {
  const tool = clone(lib.tools[name] || { title: name, run: "" });
  const source = tool.source;
  delete tool.source;
  const status = h("span", { class: "status" });
  const dirty = unsaved(status);
  main.replaceChildren(h("div", { class: "page" },
    h("div", { class: "eyebrow" }, `Tool · tools/${name}.json · ${FROM[source]}`),
    h("h1", {}, name),
    h("p", { class: "lead" }, "A named action a chain can call. It runs as a bash script in the project folder. {{name}} is filled from the chain's inputs and values, the step's values, or an earlier step's output ({{step_id.field}}), already shell-quoted. If it prints a JSON object, its fields become the step's output."),
    h("div", { class: "split" },
      h("div", { class: "panel" }, h("div", { class: "panel-head" }, "Used by"), h("div", { class: "panel-body used-by" }, usedBy((s) => s.tool === name))),
      h("div", { class: "panel" },
        h("div", { class: "panel-head" }, h("span", { class: "grow" }, "Definition"), itemButtons("tools", name, source, status, () => tool)),
        h("div", { class: "panel-body form" },
          field("What it does", textInput(tool.title, (v) => { tool.title = v; dirty(); })),
          field("Command", textArea(tool.run, (v) => { tool.run = v; dirty(); }, { class: "code", rows: 12 })))))));
}

async function showSettings() {
  const info = await api("/api/settings");
  let scope = info.project ? "project" : "shared";
  const status = h("span", { class: "status" });
  const switcher = h("div", { class: "segmented" });
  const body = h("div", { class: "panel-body form" });
  let layer;
  let changed = false;
  const dirty = () => { changed = true; unsaved(status)(); };

  const draw = () => {
    layer = clone((scope === "project" ? info.project : info.shared) || {});
    layer.agents = layer.agents || {};
    const inherited = scope === "project" ? info.shared || {} : {};
    changed = false;
    status.textContent = "";
    switcher.replaceChildren(...[["project", "This project", ".swarm/settings.json"], ["shared", "Every project", "~/.swarm/settings.json"]].map(([value, label, path]) =>
      h("button", { class: scope === value ? "on" : "", type: "button", title: path, onclick: () => {
        if (value === scope || (changed && !confirm("Switch without saving your changes?"))) return;
        scope = value;
        draw();
      } }, label)));
    const custom = Object.entries(layer.agents).filter(([n]) => n !== "claude" && n !== "codex").map(([name, cli]) => ({ name, command: cli.command || "" }));
    const argsField = (name) => textInput((layer.agents[name]?.args || []).join(" "), (v) => {
      const args = v.split(" ").filter(Boolean);
      if (args.length) layer.agents[name] = { args };
      else delete layer.agents[name];
      dirty();
    }, { class: "code", placeholder: inherited.agents?.[name]?.args?.join(" ") || (name === "claude" ? "e.g. --model opus" : "e.g. --model gpt-5") });
    const names = () => [...new Set(["claude", "codex", ...lib.runners, ...custom.map((c) => c.name).filter(Boolean)])];
    const unset = scope === "project" ? `not set here: ${inherited.agent || "claude"}, from every project's settings` : "not set: claude";
    const shared = Object.entries(inherited.inputs || {}).filter(([k]) => !(layer.inputs || {})[k]);
    body.replaceChildren(
      h("p", { class: "hint" }, scope === "project" ? "These apply to this project only and win over the settings for every project." : "These apply to every project, unless a project sets its own."),
      field("Default agent for agent steps", selectBox([["", unset], ...names().map((n) => [n, n])], layer.agent || "", (v) => { layer.agent = v || undefined; dirty(); }), `a step can pick another under "Runs with"; used now: ${info.default}`),
      h("div", { class: "subpanel" },
        h("h3", {}, "Built in"),
        h("div", { class: "used-by" }, ["claude", "codex"].map((n) => h("span", {}, info.installed[n] ? `✓ ${n} found at ${info.installed[n]}` : `✗ ${n} is not on PATH`))),
        field("Extra arguments for claude", argsField("claude"), "added to claude -p; the step's tool list still applies"),
        field("Extra arguments for codex", argsField("codex"), "added to codex exec; otherwise Codex uses your own config, sandbox and approvals")),
      h("div", { class: "subpanel" },
        h("h3", {}, "Your own agents"),
        h("span", { class: "hint" }, "A shell command run in the project folder. It gets the prompt on stdin (and in $SWARM_PROMPT_FILE) and must print the prompt's fields as one JSON object last."),
        rowsEditor(custom, [{ key: "name", label: "name, e.g. pi", mono: true }, { key: "command", label: "command, e.g. pi -p", mono: true }], (rows) => {
          for (const n of Object.keys(layer.agents)) if (n !== "claude" && n !== "codex") delete layer.agents[n];
          for (const row of rows) if (row.name) layer.agents[row.name] = { command: row.command };
          dirty();
        })),
      h("div", { class: "subpanel" },
        h("h3", {}, "Starting values for chain inputs"),
        h("span", { class: "hint" }, `A chain input with this name starts with this value instead of the chain's own default, so a shared chain fits ${scope === "project" ? "this project" : "every project"} without a copy. Inputs the chains ask for: ${inputNames().join(", ") || "none"}.`),
        rowsEditor(mapToRows(layer.inputs), [{ key: "key", label: "input, e.g. test_command", mono: true }, { key: "value", label: "value, e.g. ./test.sh", mono: true }], (rows) => {
          layer.inputs = rowsToMap(rows);
          dirty();
        }),
        shared.length ? h("span", { class: "hint" }, "From every project's settings: ", shared.map(([k, v], i) => [i ? ", " : "", h("code", {}, `${k} = ${v}`)])) : null));
  };
  const inputNames = () => [...new Set(lib.chains.flatMap((c) => (c.inputs || []).map((i) => i.name)))].sort();
  const save = async () => {
    if (!Object.keys(layer.agents).length) delete layer.agents;
    if (!Object.keys(layer.inputs || {}).length) delete layer.inputs;
    try {
      await api(`/api/items/settings.json?scope=${scope}`, { method: "PUT", body: JSON.stringify(layer, null, 2) });
      await loadLibrary();
      renderSide();
      Object.assign(info, await api("/api/settings"));
      draw();
      status.className = "ok-text";
      status.textContent = "Saved";
    } catch (err) {
      status.className = "error-text";
      status.textContent = err.message;
    }
  };
  draw();
  main.replaceChildren(h("div", { class: "page" },
    h("div", { class: "eyebrow" }, "Settings"),
    h("h1", {}, "Agents and starting values"),
    h("p", { class: "lead" }, "swarm drives agent CLIs in their headless mode: Claude Code, Codex, or one you add. Starting values adapt shared chains, such as which command runs the full test suite."),
    h("div", { class: "panel" },
      h("div", { class: "panel-head" }, switcher, h("span", { class: "grow" }), h("div", { class: "savebar" }, status, h("button", { class: "btn small primary", type: "button", onclick: save }, "Save"))),
      body)));
}

async function showMemory() {
  const res = await api("/api/memory");
  let text = res.text;
  const status = h("span", { class: "status" });
  const save = async () => {
    try {
      await send("PUT", "/api/memory", { text });
      status.className = "ok-text";
      status.textContent = "Saved";
    } catch (err) { alert(err.message); }
  };
  main.replaceChildren(h("div", { class: "page" },
    h("div", { class: "eyebrow" }, `Memory · ${res.path}`),
    h("h1", {}, "What this project has learned"),
    h("p", { class: "lead" }, "Agents read this before they start (a prompt writes {{memory}}). The make-change chain's last steps add what a run learned. It lives in the project folder, so everyone who works on it shares it once it is committed."),
    h("div", { class: "panel" },
      h("div", { class: "panel-head" }, h("span", { class: "grow" }, "memory.md"), h("div", { class: "savebar" }, status, h("button", { class: "btn small primary", type: "button", onclick: save }, "Save"))),
      h("div", { class: "panel-body" }, textArea(text, (v) => { text = v; unsaved(status)(); }, { class: "code prompt", placeholder: "Nothing remembered yet. Facts here reach every agent in this project." })))));
}

function returnsList(prompt) {
  const fields = prompt.returns || [];
  const problems = prompt.problems || [];
  if (!fields.length && !problems.length) return h("span", { class: "hint" }, "No ## Return section, so the agent answers in free text, read as {{step_id.output}}");
  return [
    ...problems.map((p) => h("span", { class: "error-text" }, p)),
    ...fields.map((f) => h("span", {}, h("code", {}, f.name), f.value === "text" ? null : h("span", { class: "hint" }, ` ${f.value}`))),
  ];
}

const RETURN_EXAMPLE = "## Return\n\n- `verdict`: `APPROVE` or `CHANGES`\n- `summary`: one plain sentence\n- `files` (list): the files you changed";

function showPrompt(name) {
  let text = lib.prompts[name]?.text ?? "";
  const source = lib.prompts[name]?.source;
  const status = h("span", { class: "status" });
  const facts = h("div", { class: "panel" });
  const drawFacts = () => {
    const saved = lib.prompts[name] || {};
    const placeholders = placeholdersIn(saved.text);
    facts.replaceChildren(
      h("div", { class: "panel-head" }, "Used by"), h("div", { class: "panel-body used-by" }, usedBy((s) => s.agent === name)),
      h("div", { class: "panel-head" }, "Fields it returns"), h("div", { class: "panel-body used-by" }, returnsList(saved)),
      h("div", { class: "panel-head" }, "Placeholders it reads"), h("div", { class: "panel-body used-by" }, placeholders.length ? placeholders.map((p) => h("code", {}, `{{${p}}}`)) : h("span", { class: "hint" }, "none")),
      h("div", { class: "panel-head" }, "How to list the fields"), h("div", { class: "panel-body" }, h("pre", { class: "example" }, RETURN_EXAMPLE),
        h("span", { class: "hint" }, "Backticked options become the only allowed answers. Write (list), (number) or (bool) after a name for those kinds.")));
  };
  drawFacts();
  main.replaceChildren(h("div", { class: "page" },
    h("div", { class: "eyebrow" }, `Prompt · prompts/${name}.md · ${FROM[source]}`),
    h("h1", {}, name),
    h("p", { class: "lead" }, "What an agent step tells the agent. {{placeholders}} are filled from the chain before it starts. The fields listed under a ## Return heading are what the agent must answer with."),
    h("div", { class: "split" },
      facts,
      h("div", { class: "panel" },
        h("div", { class: "panel-head" }, h("span", { class: "grow" }, "Text"), itemButtons("prompts", name, source, status, () => text, drawFacts)),
        h("div", { class: "panel-body" }, textArea(text, (v) => { text = v; unsaved(status)(); }, { class: "code prompt" }))))));
}
