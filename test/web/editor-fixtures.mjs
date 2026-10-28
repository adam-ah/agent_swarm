import { $$, go, page, reset, route, settle } from "./page.mjs";

export const CHANGE_PROMPT = [
  "Change the code for {{request}}.",
  "What this project learned: {{memory}}",
  "",
  "## Return",
  "",
  "- `summary`: one plain sentence a person reads first",
  "- `files` (list): the files you changed",
  "- `verdict`: `DONE` or `STUCK`",
  "",
].join("\n");

export const REVIEW_PROMPT = [
  "Review the change {{change.summary}} in {{run_folder}}.",
  "",
  "## Return",
  "",
  "- `verdict`: `APPROVE` or `CHANGES`",
  "- `notes`: what to fix",
  "",
].join("\n");

const returns = {
  change: [{ name: "summary", value: "text" }, { name: "files", value: "list" }, { name: "verdict", value: "DONE | STUCK" }],
  review: [{ name: "verdict", value: "APPROVE | CHANGES" }, { name: "notes", value: "text" }],
};

export function makeChange() {
  return {
    name: "make-change",
    title: "Make a change",
    description: "Change the code, test it, review it and ask before merging.",
    subject: "{{request}}",
    link: "{{read_issue.url}}",
    inputs: [
      { name: "request", label: "What should change?" },
      { name: "test_command", label: "Test command", default: "./test.sh", optional: true },
    ],
    vars: [{ name: "branch", value: "swarm/{{run_id}}" }, { name: "issue_text", value: "{{read_issue.output}}" }],
    steps: [
      { id: "read_issue", title: "Read the issue", tool: "read_issue", with: { issue: "{{request}}" } },
      { id: "change", title: "Make the change", agent: "change", tools: ["Read", "Edit", "Bash"] },
      { id: "test", title: "Run the tests", tool: "run_tests", when: "change.verdict == DONE", stop_if: "output == FAIL", stop: "Tests failed: {{test.output}}" },
      { id: "review", title: "Review it", agent: "review-change", tools: ["Read", "Grep"], runner: "sonnet" },
      { id: "approve", title: "Approve the change", user: "Merge it? {{change.summary}}", show: ["{{run_folder}}/change.diff"], revise: "change" },
      { id: "merge", title: "Merge", tool: "merge", with: {} },
      { id: "ship", title: "Ship each changeset", chain: "ship-changeset", with: { changeset: "{{change.files}}" } },
      { id: "tell", title: "Tell the team", tool: "cleanup", when: "approve.decision != approve", always: true },
      { id: "cleanup", title: "Clean up", tool: "cleanup", always: true },
    ],
    prompts: {
      change: { text: CHANGE_PROMPT, source: "shared", returns: returns.change },
      "review-change": { text: REVIEW_PROMPT, source: "shared", returns: returns.review },
    },
    warnings: ["approve: nothing reads approve.note"],
    source: "shared",
  };
}

export function shipChangeset() {
  return {
    name: "ship-changeset",
    title: "Ship a changeset",
    inputs: [
      { name: "changeset", label: "Changeset to apply" },
      { name: "dry_run", label: "Dry run", optional: true },
      { name: "target", label: "", default: "main" },
    ],
    steps: [
      { id: "apply", title: "Apply one changeset", tool: "merge" },
      { id: "nested", title: "Run solo", chain: "solo" },
    ],
    source: "project",
  };
}

export function solo() {
  return {
    name: "solo",
    title: "Solo",
    steps: [
      { id: "only", title: "Only step", tool: "read_issue", when: "{{ready}} == yes", stop_if: "exit_code > 0" },
      { id: "back", title: "Back to shipping", chain: "ship-changeset" },
    ],
    problems: ["only: the tool read_issue needs a value for issue"],
    source: "project",
  };
}

export function tools() {
  return {
    read_issue: { title: "Read an issue", run: "gh issue view {{issue}} --json url,title", source: "shared" },
    run_tests: { title: "Run the tests", run: "{{test_command}} {{change.files}}", source: "project" },
    merge: { title: "Merge the branch", run: "git merge {{branch}} && echo {{run_folder}}", source: "shared" },
    cleanup: { title: "Clean up", run: "rm -rf {{run_folder}}/tmp", source: "shared" },
  };
}

export function library(chains = [makeChange(), shipChangeset(), solo()]) {
  return {
    chains,
    tools: tools(),
    agent_tools: ["Bash", "Read", "Grep", "Glob", "Write", "Edit", "WebFetch", "WebSearch"],
    builtins: ["project", "home", "swarm", "memory", "feedback", "run_id", "run_folder", "guide", "catalog"],
    input_values: { test_command: "make test" },
    project: "/work/demo",
    runners: ["claude", "codex", "sonnet"],
    default_runner: "claude",
  };
}

const ITEM = /^\/api\/items\/(chains|tools)\/(.+)\.(json|md)$/;

function store(lib, call) {
  const [, kind, name, ext] = ITEM.exec(call.path);
  const scope = call.query.scope;
  if (kind === "tools") {
    lib.tools[name] = { ...JSON.parse(call.body), source: scope || lib.tools[name]?.source || "shared" };
    return;
  }
  if (ext === "md") {
    const [chainName, promptName] = name.split("/");
    const chain = lib.chains.find((c) => c.name === chainName);
    chain.prompts = { ...chain.prompts, [promptName]: { text: call.body, source: chain.source } };
    return;
  }
  const old = lib.chains.find((c) => c.name === name);
  const next = { ...JSON.parse(call.body), name, prompts: old?.prompts, source: scope || old?.source || "project" };
  if (old) lib.chains.splice(lib.chains.indexOf(old), 1, next);
  else lib.chains.push(next);
}

export function start(lib = library()) {
  reset();
  const saved = [];
  route("GET", "/api/library", () => lib);
  route("PUT", ITEM, (call) => {
    saved.push(call);
    store(lib, call);
    return { ok: "true" };
  });
  return { lib, saved };
}

export async function open(hash) {
  page("if (editor) editor.dirty = false");
  if (globalThis.location.hash === hash) await page("refresh()");
  else await go(hash);
  await settle();
}

export function capture(method, path, reply = { ok: "true" }) {
  const got = [];
  route(method, path, (call) => {
    got.push(call);
    return typeof reply === "function" ? reply(call) : reply;
  });
  return got;
}

export const text = (el) => el?.textContent.replace(/\s+/g, " ").trim();

export const button = (label, root = globalThis.document) => [...root.querySelectorAll("button")].find((b) => b.textContent === label);

export const rows = () => $$("#tree .tree-row:not(.readonly)");

export const row = (title) => rows().find((r) => r.querySelector(".title").textContent === title);

export const titles = (selector) => $$(selector).map((el) => el.querySelector(".title").textContent);

export function labelled(label, root = globalThis.document) {
  return [...root.querySelectorAll("label.field")].find((l) => l.firstElementChild?.firstChild?.textContent === label);
}

export const control = (label, root) => labelled(label, root)?.querySelector("input, textarea, select");

export const options = (select) => [...select.options].map((o) => [o.value, o.textContent]);

export function tick(box) {
  box.dispatchEvent(new globalThis.window.MouseEvent("click", { cancelable: true }));
}

export const shown =(select) => (select.querySelector("option[selected]") ?? select.options[0]).value;

export const body = (call) => JSON.parse(call.body);
