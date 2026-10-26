function itemButtons(kind, name, source, status, payload, onSaved) {
  const save = async (scope = "") => {
    try {
      await putItem(kind, name, payload(), scope);
      await loadLibrary();
      status.className = "ok-text";
      status.textContent = scope ? "Copied to this folder" : "Saved";
      renderSide();
      onSaved?.();
      if (scope) refresh();
    } catch (err) {
      status.className = "error-text";
      status.textContent = err.message;
    }
  };
  const remove = async () => {
    const what = source === "project" ? "this folder's copy" : "it from the shared library, for every project";
    if (!confirm(`Delete ${name}? This removes ${what}.`)) return;
    try {
      await deleteItem(kind, name);
      await loadLibrary();
      location.hash = "#/chain/";
    } catch (err) {
      status.className = "error-text";
      status.textContent = err.message;
    }
  };
  return h("div", { class: "savebar" }, status,
    source !== "project" ? h("button", { class: "btn small", title: "Save a copy in this folder's .swarm that overrides this one here", onclick: () => save("project") }, "Copy to this folder") : null,
    h("button", { class: "btn small danger", onclick: remove }, "Delete"),
    h("button", { class: "btn small primary", onclick: () => save() }, "Save"));
}

function showTool(name) {
  const tool = clone(lib.tools[name] || { title: name, run: "" });
  const source = tool.source;
  delete tool.source;
  const status = h("span", { class: "status" });
  const dirty = () => { status.className = "status"; status.textContent = "Unsaved"; };
  main.replaceChildren(h("div", { class: "page" },
    h("div", { class: "eyebrow" }, `Tool · ${FROM[source]} · tools/${name}.json`),
    h("h1", {}, name),
    h("p", { class: "lead" }, "A named action a chain can call. It runs as a shell command in the project folder. Write {{name}} for a chain's inputs and vars, the step's values, or an earlier step's output ({{step_id.field}}); values are shell-quoted. If it prints a JSON object, its fields become the step's output."),
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
  const body = h("div", { class: "panel-body form" });
  let layer;

  const draw = () => {
    layer = clone((scope === "project" ? info.project : info.shared) || {});
    layer.agents = layer.agents || {};
    const custom = Object.entries(layer.agents).filter(([n]) => n !== "claude" && n !== "codex").map(([name, cli]) => ({ name, command: cli.command || "" }));
    const argsField = (name) => textInput((layer.agents[name]?.args || []).join(" "), (v) => {
      const args = v.split(" ").filter(Boolean);
      if (args.length) layer.agents[name] = { args };
      else delete layer.agents[name];
      dirty();
    }, { class: "code", placeholder: name === "claude" ? "e.g. --model opus" : "e.g. --model gpt-5" });
    const names = () => [...new Set(["claude", "codex", ...lib.runners, ...custom.map((c) => c.name).filter(Boolean)])];
    const pick = selectBox([["", scope === "project" ? "not set here (use the shared default)" : "not set (claude)"], ...names().map((n) => [n, n])], layer.agent || "", (v) => { layer.agent = v || undefined; dirty(); });
    body.replaceChildren(
      field("Save these settings for", selectBox([["shared", "every project (~/.swarm/settings.json)"], ["project", "this project only (.swarm/settings.json)"]], scope, (v) => { scope = v; draw(); })),
      field("Default agent for agent steps", pick, `a step can override it with "Runs with"; now: ${info.default}`),
      h("div", { class: "subpanel" },
        h("h3", {}, "Built in"),
        h("div", { class: "used-by" }, ["claude", "codex"].map((n) => h("span", {}, info.installed[n] ? `✓ ${n} found at ${info.installed[n]}` : `✗ ${n} is not on PATH`))),
        field("Extra arguments for claude", argsField("claude"), "added to claude -p; the step's tool list still applies"),
        field("Extra arguments for codex", argsField("codex"), "added to codex exec; otherwise Codex uses your own config, sandbox and approvals")),
      h("div", { class: "subpanel" },
        h("h3", {}, "Your own agents"),
        h("span", { class: "hint" }, "A shell command run in the project folder. It gets the prompt on stdin (and in $SWARM_PROMPT_FILE) and must print the step's fields as one JSON object last."),
        rowsEditor(custom, [{ key: "name", label: "name, e.g. pi", mono: true }, { key: "command", label: "command, e.g. pi -p", mono: true }], (rows) => {
          for (const n of Object.keys(layer.agents)) if (n !== "claude" && n !== "codex") delete layer.agents[n];
          for (const row of rows) if (row.name) layer.agents[row.name] = { command: row.command };
          dirty();
        })));
  };
  const dirty = () => { status.className = "status"; status.textContent = "Unsaved"; };
  const save = async () => {
    if (!Object.keys(layer.agents).length) delete layer.agents;
    try {
      await api(`/api/items/settings.json?scope=${scope}`, { method: "PUT", body: JSON.stringify(layer, null, 2) });
      await loadLibrary();
      renderSide();
      status.className = "ok-text";
      status.textContent = "Saved";
      const fresh = await api("/api/settings");
      Object.assign(info, fresh);
    } catch (err) {
      status.className = "error-text";
      status.textContent = err.message;
    }
  };
  draw();
  main.replaceChildren(h("div", { class: "page" },
    h("div", { class: "eyebrow" }, "Settings"),
    h("h1", {}, "Which agent runs agent steps"),
    h("p", { class: "lead" }, "swarm drives agent CLIs in their headless mode: Claude Code, Codex, or one you add. Project settings override the shared ones."),
    h("div", { class: "panel" },
      h("div", { class: "panel-head" }, h("span", { class: "grow" }, "settings.json"), h("div", { class: "savebar" }, status, h("button", { class: "btn small primary", onclick: save }, "Save"))),
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
    h("h1", {}, "What this folder has learned"),
    h("p", { class: "lead" }, "Agents read this before they start (write {{memory}} in a prompt). The fix chain's last steps add what a run learned. It lives in the project folder, so it is shared with everyone who works on it once you commit it."),
    h("div", { class: "panel" },
      h("div", { class: "panel-head" }, h("span", { class: "grow" }, "memory.md"), h("div", { class: "savebar" }, status, h("button", { class: "btn small primary", onclick: save }, "Save"))),
      h("div", { class: "panel-body" }, textArea(text, (v) => { text = v; status.className = "status"; status.textContent = "Unsaved"; }, { class: "code prompt", placeholder: "Nothing remembered yet. Facts here reach every agent in this folder." })))));
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
      h("div", { class: "panel-head" }, "Placeholders it reads"), h("div", { class: "panel-body used-by" }, placeholders.length ? placeholders.map((p) => h("code", {}, `{{${p}}}`)) : h("span", { class: "hint" }, "none")));
  };
  drawFacts();
  main.replaceChildren(h("div", { class: "page" },
    h("div", { class: "eyebrow" }, `Prompt · ${FROM[source]} · prompts/${name}.md`),
    h("h1", {}, name),
    h("p", { class: "lead" }, "What an agent step tells the agent. {{placeholders}} are filled from the chain before it starts. The fields under ## Return, one per line like - `verdict`: `APPROVE` or `CHANGES`, are what it must return; write (list), (number) or (bool) after a name for those kinds."),
    h("div", { class: "split" },
      facts,
      h("div", { class: "panel" },
        h("div", { class: "panel-head" }, h("span", { class: "grow" }, "Text"), itemButtons("prompts", name, source, status, () => text, drawFacts)),
        h("div", { class: "panel-body" }, textArea(text, (v) => { text = v; status.className = "status"; status.textContent = "Unsaved"; }, { class: "code prompt" }))))));
}
