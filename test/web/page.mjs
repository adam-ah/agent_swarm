import fs from "node:fs";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const installed = new URL("../../node_modules/happy-dom/package.json", import.meta.url);
const bundle = new URL("../../node_modules/.cache/happy-dom.mjs", import.meta.url);
const mtime = (url) => fs.statSync(url, { throwIfNoEntry: false })?.mtimeMs ?? 0;
if (mtime(bundle) < mtime(installed)) {
  const { build } = await import("esbuild");
  const result = await build({
    entryPoints: [fileURLToPath(new URL("lib/index.js", installed))],
    outfile: fileURLToPath(bundle),
    write: false,
    bundle: true,
    platform: "node",
    format: "esm",
    logLevel: "warning",
    banner: { js: "import { createRequire } from \"node:module\"; const require = createRequire(import.meta.url);" },
  });
  const partial = new URL(`happy-dom.${process.pid}.tmp`, bundle);
  fs.mkdirSync(new URL(".", bundle), { recursive: true });
  fs.writeFileSync(partial, result.outputFiles[0].contents);
  fs.renameSync(partial, bundle);
}
const { Window } = await import(bundle.href);

const web = new URL("../../src/web/", import.meta.url);
const html = fs.readFileSync(new URL("index.html", web), "utf8");
const scripts = [...html.matchAll(/<script src="\/([^"]+)"><\/script>/g)].map((m) => m[1]);
const window = new Window({ url: "http://swarm.test/", settings: { disableJavaScriptEvaluation: true, disableJavaScriptFileLoading: true, disableCSSFileLoading: true } });
window.document.write(html.replace(/<script[^>]*><\/script>/g, "").replace(/<link[^>]*>/g, ""));

export const calls = [];
export const sources = [];
export const dialogs = { confirm: [], alert: [], prompt: [] };
export const answers = { confirm: true, prompt: null };
const routes = [];

export function route(method, path, reply) {
  routes.unshift({ method, path, reply });
}

export function resetRoutes() {
  routes.length = 0;
  route("GET", "/api/projects", { projects: ["/work/demo"], default: "/work/demo" });
  route("GET", "/api/library", { chains: [], tools: {}, agent_tools: [] });
  route("GET", "/api/runs", []);
}

function matches(r, method, path) {
  return r.method === method && (r.path instanceof RegExp ? r.path.test(path) : r.path === path);
}

async function fakeFetch(url, options = {}) {
  const parsed = new URL(url, window.location.href);
  const method = options.method || "GET";
  const call = { method, path: parsed.pathname, query: Object.fromEntries(parsed.searchParams), body: options.body };
  calls.push(call);
  const found = routes.find((r) => matches(r, method, parsed.pathname));
  if (!found) return new Response(JSON.stringify({ error: `no fake for ${method} ${parsed.pathname}` }), { status: 404 });
  const reply = typeof found.reply === "function" ? await found.reply(call) : found.reply;
  if (reply instanceof Response) return reply;
  return new Response(typeof reply === "string" ? reply : JSON.stringify(reply), { status: 200 });
}

export function fail(status, error, problems) {
  return new Response(JSON.stringify({ error, problems }), { status });
}

class FakeEventSource {
  constructor(url) {
    this.url = url;
    this.closed = false;
    sources.push(this);
  }

  close() {
    this.closed = true;
  }

  send(data) {
    this.onmessage?.({ data: JSON.stringify(data) });
  }
}

const realInterval = setInterval;
Object.assign(globalThis, {
  window,
  document: window.document,
  location: window.location,
  history: window.history,
  Node: window.Node,
  HTMLElement: window.HTMLElement,
  Event: window.Event,
  KeyboardEvent: window.KeyboardEvent,
  MouseEvent: window.MouseEvent,
  localStorage: window.localStorage,
  sessionStorage: window.sessionStorage,
  requestAnimationFrame: (cb) => setTimeout(cb, 0),
  setInterval: (...args) => realInterval(...args).unref(),
  confirm: (text) => { dialogs.confirm.push(text); return answers.confirm; },
  alert: (text) => { dialogs.alert.push(text); },
  prompt: (text) => { dialogs.prompt.push(text); return answers.prompt; },
  EventSource: FakeEventSource,
  fetch: fakeFetch,
});
window.HTMLElement.prototype.scrollIntoView = function scrollIntoView() {};

resetRoutes();
for (const name of scripts) {
  const file = new URL(name, web);
  vm.runInThisContext(fs.readFileSync(file, "utf8"), { filename: file.href });
}

export const page = (code) => vm.runInThisContext(code);
export const $ = (selector) => window.document.querySelector(selector);
export const $$ = (selector) => [...window.document.querySelectorAll(selector)];
export const settle = (ms = 5) => new Promise((resolve) => setTimeout(resolve, ms));

export async function go(hash) {
  window.location.hash = hash;
  await settle(20);
}

export function click(el) {
  el.dispatchEvent(new window.MouseEvent("click", { bubbles: true, cancelable: true }));
}

export function type(el, value) {
  el.value = value;
  el.dispatchEvent(new window.Event("input", { bubbles: true }));
  el.dispatchEvent(new window.Event("change", { bubbles: true }));
}

export function reset() {
  calls.length = 0;
  sources.length = 0;
  for (const list of Object.values(dialogs)) list.length = 0;
  answers.confirm = true;
  answers.prompt = null;
  resetRoutes();
}

await settle(20);
