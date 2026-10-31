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
    const what = source === "project" ? "the project copy" : "the shared copy, for all projects";
    if (!confirm(`Delete ${name}? Removes ${what}.`)) return;
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
        source !== "project" && kind !== "prompts" ? h("button", { class: "linkish", type: "button", onclick: () => save("project") }, "Save as project copy") : null,
        kind === "prompts" ? h("span", { class: "hint" }, "For a project-only change, save a project copy of the chain; its prompts are copied with it.") : null,
        h("button", { class: "linkish danger", type: "button", onclick: remove }, kind === "prompts" ? "Delete prompt" : source === "project" ? "Delete project copy" : "Delete (all projects)"))),
    h("button", { class: "btn small primary", type: "button", onclick: () => save() }, "Save"));
}

const unsaved = (status) => () => { status.className = "status"; status.textContent = "Unsaved"; };

function showTool(name) {
  const tool = clone(lib.tools[name] || { title: name, run: "" });
  const source = tool.source;
  delete tool.source;
  const status = h("span", { class: "status" });
  const users = h("div", { class: "panel-body used-by" });
  const drawUsers = (verdicts) => users.replaceChildren(...usedBy((s) => s.tool === name, verdicts));
  drawUsers();
  const dirty = () => {
    unsaved(status)();
    checkLater({ tools: { [name]: tool } }, (v) => { drawUsers(v); draftStatus(status, v[`tools/${name}.json`]?.problems); });
  };
  main.replaceChildren(h("div", { class: "page" },
    h("div", { class: "eyebrow" }, `Tool · tools/${name}.json · ${FROM[source] || "unsaved"}`),
    h("h1", {}, name),
    h("p", { class: "lead" }, "Bash script, run in the project folder. {{name}} comes from the step's with, the chain's inputs and vars, or an earlier step's field ({{step_id.field}}); values are shell-quoted. A printed JSON object becomes the step's fields; otherwise stdout is {{step_id.output}}."),
    h("div", { class: "split" },
      h("div", { class: "panel" }, h("div", { class: "panel-head" }, "Used by"), users),
      h("div", { class: "panel" },
        h("div", { class: "panel-head" }, h("span", { class: "grow" }, "Definition"), itemButtons("tools", name, source, status, () => tool, () => drawUsers())),
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
    switcher.replaceChildren(...[["project", "Project", ".swarm/settings.json"], ["shared", "Shared", "~/.swarm/settings.json"]].map(([value, label, path]) =>
      h("button", { class: scope === value ? "on" : "", type: "button", title: path, onclick: () => {
        if (value === scope || (changed && !confirm("Discard unsaved changes?"))) return;
        scope = value;
        draw();
      } }, label)));
    const custom = Object.entries(layer.agents).filter(([n]) => n !== "claude" && n !== "codex")
      .map(([name, cli]) => ({ name, runs: cli.runs || "", command: cli.runs ? (cli.args || []).join(" ") : cli.command || "" }));
    const agentOf = (row) => (row.runs ? { runs: row.runs, args: row.command.split(" ").filter(Boolean) } : { command: row.command });
    const argsField = (name) => textInput((layer.agents[name]?.args || []).join(" "), (v) => {
      const args = v.split(" ").filter(Boolean);
      if (args.length) layer.agents[name] = { args };
      else delete layer.agents[name];
      dirty();
    }, { class: "code", placeholder: inherited.agents?.[name]?.args?.join(" ") || (name === "claude" ? "e.g. --model opus" : "e.g. --model gpt-5") });
    const names = () => [...new Set(["claude", "codex", ...lib.runners, ...custom.map((c) => c.name).filter(Boolean)])];
    const unset = scope === "project" ? `inherited: ${inherited.agent || "claude"} (shared settings)` : "unset: claude";
    const shared = Object.entries(inherited.inputs || {}).filter(([k]) => !(layer.inputs || {})[k]);
    body.replaceChildren(
      h("p", { class: "hint" }, scope === "project" ? "Project settings (.swarm/settings.json); override the shared settings key by key." : "Shared settings (settings.json in the swarm home); a project's settings override them."),
      field("Default agent", selectBox([["", unset], ...names().map((n) => [n, n])], layer.agent || "", (v) => { layer.agent = v || undefined; dirty(); }), `steps can override it with "Runs with"; current: ${info.default}`),
      h("div", { class: "subpanel" },
        h("h3", {}, "Built in"),
        h("div", { class: "used-by" }, ["claude", "codex"].map((n) => h("span", {}, info.installed[n] ? `✓ ${n}: ${info.installed[n]}` : `✗ ${n}: not on PATH`))),
        field("claude args", argsField("claude"), "appended to claude -p; step tool limits still apply"),
        field("codex args", argsField("codex"), "appended to codex exec; Codex otherwise uses your config, sandbox and approvals")),
      h("div", { class: "subpanel" },
        h("h3", {}, "Custom agents"),
        h("span", { class: "hint" }, "Either an alias for claude or codex with its own args (e.g. sonnet = claude --model sonnet); returns fields and resumes sessions like the built-in. Or a shell command run in the project folder: prompt on stdin and in $SWARM_PROMPT_FILE, must print the fields as one JSON object, cannot resume."),
        rowsEditor(custom, [
          { key: "name", label: "name, e.g. sonnet", mono: true },
          { key: "runs", label: "claude, codex, or empty for a command", mono: true },
          { key: "command", label: "args, e.g. --model sonnet, or a command, e.g. pi -p", mono: true },
        ], (rows) => {
          for (const n of Object.keys(layer.agents)) if (n !== "claude" && n !== "codex") delete layer.agents[n];
          for (const row of rows) if (row.name) layer.agents[row.name] = agentOf(row);
          dirty();
        })),
      h("div", { class: "subpanel" },
        h("h3", {}, "Input defaults"),
        h("span", { class: "hint" }, `Overrides the chain's default for any input with this name, in ${scope === "project" ? "this project" : "every project"}. Chain inputs: ${inputNames().join(", ") || "none"}.`),
        rowsEditor(mapToRows(layer.inputs), [{ key: "key", label: "input, e.g. test_command", mono: true }, { key: "value", label: "value, e.g. ./test.sh", mono: true }], (rows) => {
          layer.inputs = rowsToMap(rows);
          dirty();
        }),
        shared.length ? h("span", { class: "hint" }, "Inherited from shared settings: ", shared.map(([k, v], i) => [i ? ", " : "", h("code", {}, `${k} = ${v}`)])) : null));
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
    h("h1", {}, "Agents and input defaults"),
    h("p", { class: "lead" }, "Agent CLIs run headless (claude, codex, or custom), and per-project input defaults for shared chains, e.g. the test command."),
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
    h("h1", {}, "Project memory"),
    h("p", { class: "lead" }, "Injected into prompts via {{memory}}. make-change appends what each run learned. Lives in .swarm/memory.md; commit it to share."),
    h("div", { class: "panel" },
      h("div", { class: "panel-head" }, h("span", { class: "grow" }, "memory.md"), h("div", { class: "savebar" }, status, h("button", { class: "btn small primary", type: "button", onclick: save }, "Save"))),
      h("div", { class: "panel-body" }, textArea(text, (v) => { text = v; unsaved(status)(); }, { class: "code prompt", placeholder: "Empty" })))));
}

function returnsList(prompt) {
  const fields = prompt.returns || [];
  const problems = prompt.problems || [];
  if (!fields.length && !problems.length) return h("span", { class: "hint" }, "No ## Return: free text in {{step_id.output}}");
  return [
    ...problems.map((p) => h("span", { class: "error-text" }, p)),
    ...fields.map((f) => h("span", {}, h("code", {}, f.name), f.value === "text" ? null : h("span", { class: "hint" }, ` ${f.value}`))),
    fields.some((f) => f.name === "questions") ? h("span", { class: "hint" }, "Returns questions: the step blocks until they are answered; answers in {{step_id.answers}}") : null,
  ];
}

const RETURN_EXAMPLE = "## Return\n\n- `verdict`: `APPROVE` or `CHANGES`\n- `summary`: one plain sentence\n- `files` (list): the files you changed";

function showPrompt(chainName, name) {
  const chain = lib.chains.find((c) => c.name === chainName);
  let text = promptOf(chainName, name)?.text ?? "";
  const source = chain?.source;
  const status = h("span", { class: "status" });
  const facts = h("div", { class: "panel" });
  const drawFacts = (verdicts) => {
    const saved = promptOf(chainName, name) || {};
    const placeholders = placeholdersIn(saved.text);
    facts.replaceChildren(
      h("div", { class: "panel-head" }, "Used by"), h("div", { class: "panel-body used-by" }, usedBy((s, c) => c.name === chainName && s.agent === name, verdicts)),
      h("div", { class: "panel-head" }, "Returns"), h("div", { class: "panel-body used-by" }, returnsList(saved)),
      h("div", { class: "panel-head" }, "Reads"), h("div", { class: "panel-body used-by" }, placeholders.length ? placeholders.map((p) => h("code", {}, `{{${p}}}`)) : h("span", { class: "hint" }, "none")),
      h("div", { class: "panel-head" }, "## Return syntax"), h("div", { class: "panel-body" }, h("pre", { class: "example" }, RETURN_EXAMPLE),
        h("span", { class: "hint" }, "Backticked options form an enum. (list), (number) or (bool) after the name set the type; default is text.")));
  };
  drawFacts();
  main.replaceChildren(h("div", { class: "page" },
    h("div", { class: "eyebrow" }, "Prompt of ", h("a", { href: `#/chain/${chainName}` }, chain?.title || chainName), ` · chains/${chainName}/${name}.md · ${FROM[source] || "unsaved"}`),
    h("h1", {}, name),
    h("p", { class: "lead" }, "Agent prompt. {{placeholders}} are filled before the agent starts; ## Return lists the fields it must return."),
    h("div", { class: "split" },
      facts,
      h("div", { class: "panel" },
        h("div", { class: "panel-head" }, h("span", { class: "grow" }, "Text"), itemButtons("prompts", `${chainName}/${name}`, source, status, () => text, () => drawFacts())),
        h("div", { class: "panel-body" }, textArea(text, (v) => {
          text = v;
          unsaved(status)();
          checkLater({ prompts: { [`${chainName}/${name}`]: text } }, (verdicts) => { drawFacts(verdicts); draftStatus(status, verdicts[`chains/${chainName}/${name}.md`]?.problems); });
        }, { class: "code prompt" }))))));
}
