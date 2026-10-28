import { $, go, route, settle, sources } from "./page.mjs";

export const RUN = "1028-101500";

export const at = (min, sec = 0, ms = 0) => `2026-10-28T10:${String(min).padStart(2, "0")}:${String(sec).padStart(2, "0")}.${String(ms).padStart(3, "0")}Z`;

export const library = {
  chains: [
    {
      name: "fix",
      title: "Fix a bug",
      description: "Plans and tests a fix. A person reviews it before it ships.",
      source: "project",
      inputs: [{ name: "request", label: "What to fix" }],
      vars: [{ name: "branch", value: "fix-{{run_id}}" }],
      steps: [
        { id: "plan", title: "Plan the fix", agent: "plan" },
        { id: "test", title: "Run the tests", tool: "test", with: { target: "{{plan.files}}" } },
        { id: "build", title: "Build each part", chain: "part", with: { part: "{{plan.files}}" } },
        { id: "review", title: "Review the change", user: "Is {{plan.summary}} right?", revise: "plan", show: ["{{request}}"] },
        { id: "ship", title: "Ship it", tool: "ship", when: "test.verdict == pass" },
        { id: "learn", title: "Learn from it", agent: "learn", always: true, when: "review.decision != approve" },
        { id: "cleanup", title: "Clean up", tool: "cleanup", always: true },
      ],
      prompts: {
        plan: { text: "Plan {{request}} using {{memory}}", source: "project", returns: [{ name: "summary", value: "text" }, { name: "files", value: "list" }] },
        learn: { text: "Learn from {{plan.summary}}", source: "project", returns: [{ name: "summary", value: "text" }] },
      },
    },
    {
      name: "part",
      title: "Build a part",
      source: "shared",
      inputs: [{ name: "part", label: "Part" }],
      steps: [{ id: "compile", title: "Compile it", tool: "compile", with: { file: "{{part}}" } }],
    },
  ],
  tools: {
    test: { title: "Run the tests", run: "go test {{target}}", source: "project" },
    ship: { title: "Ship it", run: "git push", source: "project" },
    cleanup: { title: "Clean up", run: "git branch -D {{branch}} {{nowhere}}", source: "project" },
    compile: { title: "Compile", run: "cc {{file}}", source: "shared" },
  },
  agent_tools: ["Bash", "Read", "Grep", "Glob", "Write", "Edit", "WebFetch", "WebSearch"],
  builtins: ["project", "home", "swarm", "memory", "feedback", "run_id", "run_folder", "guide", "catalog"],
  input_values: {},
  project: "/work/demo",
  runners: ["claude", "codex"],
  default_runner: "claude",
};

const STEPS = {
  plan: { id: "plan", path: "plan", title: "Plan the fix", kind: "agent", uses: "plan" },
  test: { id: "test", path: "test", title: "Run the tests", kind: "tool", uses: "test" },
  build: { id: "build", path: "build", title: "Build each part", kind: "chain", uses: "part" },
  review: { id: "review", path: "review", title: "Review the change", kind: "user", uses: "", revise: "plan" },
  ship: { id: "ship", path: "ship", title: "Ship it", kind: "tool", uses: "ship" },
  learn: { id: "learn", path: "learn", title: "Learn from it", kind: "agent", uses: "learn", always: true },
  cleanup: { id: "cleanup", path: "cleanup", title: "Clean up", kind: "tool", uses: "cleanup", always: true },
};

export const planDone = {
  status: "done", headline: "Change the session check", started_at: at(15), ended_at: at(15, 42),
  output: { summary: "Change the session check", files: ["auth.go", "auth_test.go"], notes: "" },
  cost: 0.1234, tokens: 5400, actions: 3, session: "sess-1", runner: "claude", log: "plan",
};

export const testDone = {
  status: "done", headline: "All tests pass", started_at: at(15, 42), ended_at: at(15, 50),
  output: { verdict: "pass", output: "ok  auth 0.2s" }, log: "test",
};

export function stepsWith(changes = {}) {
  return Object.entries(STEPS).map(([id, step]) => ({ ...step, status: "pending", ...changes[id] }));
}

export function runOf(status, changes = {}, extra = {}) {
  return {
    id: RUN, chain: "fix", title: "Fix a bug", project: "/work/demo", subject: "Login fails",
    inputs: { request: "Login fails" }, status, started_at: at(15), cost: 0, steps: stepsWith(changes), ...extra,
  };
}

export const callRecord = {
  command: ["claude", "-p", "it's here", "x".repeat(1500), "--output-format", "stream-json"],
  dir: "/work/demo",
  stdin: "Plan Login fails",
  started_at: at(15),
  ended_at: at(15, 42),
  exit_code: 0,
  result: "Planned the fix",
  stderr: "warning: slow disk",
  stream: true,
  actions: [
    { id: "a1", tool: "Bash", input: JSON.stringify({ command: "go test ./...", timeout: 60 }), output: "ok", started_at: at(15, 1), ended_at: at(15, 1, 250) },
    { id: "a2", tool: "Read", input: "not json\nsecond line", output: "", failed: true, started_at: at(15, 2), ended_at: at(16, 7) },
    { id: "a3", tool: "Grep", input: JSON.stringify({ x: 1 }), started_at: at(15, 3) },
  ],
};

export const row = (path) => $(`[id="row-${path}"]`);
export const head = (path) => row(path).querySelector(".srow-head");
export const has = (el, selector) => el.querySelector(selector) !== null;
export const isOpen = (path) => has(row(path), ".srow-body");
export const panel = (el, title) => [...el.querySelectorAll("details")].find((d) => d.querySelector("summary")?.textContent.startsWith(title));
export const texts = (el, selector) => [...el.querySelectorAll(selector)].map((e) => e.textContent);

export function press(target, key) {
  target.dispatchEvent(new globalThis.KeyboardEvent("keydown", { key, bubbles: true, cancelable: true }));
}

export async function openRun(run, lib = library) {
  route("GET", "/api/library", lib);
  const hash = `#/run/${run.id}`;
  if (globalThis.location.hash === hash) await go("#/");
  await go(hash);
  const source = sources.at(-1);
  source.send(run);
  await settle(20);
  return source;
}
