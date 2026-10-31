import test from "node:test";
import assert from "node:assert/strict";
import { $, $$, calls, click, fail, page, reset, route, settle } from "./page.mjs";
import { RUN, at, callRecord, has, head, isOpen, library, openRun, panel, planDone, press, row, runOf, testDone, texts } from "./run-fixtures.mjs";

const hash = () => globalThis.location.hash;
const callsTo = (log, part) => calls.filter((c) => c.path === `/api/runs/${RUN}/call` && c.query.log === log && c.query.part === part);
const callOf = (path) => panel(row(path), "Call");

test("the call panel shows the command, tool calls, output and stream, and keeps what was opened", async () => {
  reset();
  route("GET", `/api/runs/${RUN}/call`, (c) => (c.query.part === "stream" ? "{\"type\":\"result\"}\n" : callRecord));
  const run = runOf("done", { plan: planDone, test: testDone }, { ended_at: at(16) });
  const source = await openRun(run);
  click(head("plan"));
  click(callOf("plan").querySelector("summary"));
  await settle(20);

  const call = callOf("plan");
  const meta = texts(call, ".call-meta span");
  assert.match(meta[0], /^started /);
  assert.deepEqual(meta.slice(1), ["ran 42.0s", "exit 0", "in /work/demo"]);
  assert.equal(call.querySelector(".call-meta .good").textContent, "exit 0");
  assert.deepEqual(texts(call, ".cmd-line > span"), ["$ claude", "-p 'it'\\''s here'", "--output-format stream-json", "< stdin"]);

  const argument = call.querySelector(".cmd-arg");
  assert.equal(argument.parentElement.firstChild.textContent, "-p 'it'\\''s here'");
  assert.equal(argument.querySelector(".cmd-arg-start").textContent, `'${"x".repeat(200)}…`);
  assert.equal(argument.querySelector(".hint").textContent, "1.5 KB");
  assert.equal(argument.open, false);
  assert.equal(argument.querySelector("pre").textContent, callRecord.command[3]);
  assert.equal(panel(call, "stdin").querySelector("summary").textContent, "stdin 16 chars");
  assert.equal(panel(call, "stdin").open, true);
  assert.equal(panel(call, "result").querySelector("pre").textContent, "Planned the fix");
  assert.equal(panel(call, "stderr").querySelector("pre").textContent, "warning: slow disk");
  assert.equal(panel(call, "stdout") === undefined, true);

  assert.equal(call.querySelector(".actions-head").textContent, "Tool calls (3)");
  assert.deepEqual(texts(call, ".action-tool"), ["Bash", "Read", "Grep"]);
  assert.deepEqual(texts(call, ".action-input"), ["go test ./...", "not json", "{\"x\":1}"]);
  assert.deepEqual(texts(call, ".action-time"), ["0.3s", "1m 05s", "unfinished 39.0s"]);
  const actions = $$(".action");
  assert.ok(actions[1].classList.contains("failed"));
  assert.deepEqual(texts(actions[1], ".action-body .hint"), ["input", "output (failed)"]);
  assert.equal(actions[1].querySelectorAll(".action-body pre")[1].textContent, "(empty)");
  assert.deepEqual(texts(actions[2], ".action-body .hint"), ["input"]);

  const stream = panel(call, "full output stream");
  assert.equal(stream.open, false);
  click(stream.querySelector("summary"));
  await settle(20);
  assert.equal(stream.querySelector("pre").textContent, "{\"type\":\"result\"}\n");
  assert.equal(callsTo("plan", "stream").length, 1);

  click(argument.querySelector("summary"));
  click(panel(call, "stdin").querySelector("summary"));
  click($$(".action")[0].querySelector("summary"));
  source.send({ ...run, steps: run.steps.map((s) => (s.id === "plan" ? { ...s, actions: 4 } : s)) });
  await settle(20);
  const again = callOf("plan");
  assert.equal(callsTo("plan").length, 2);
  assert.equal(again.querySelector(".cmd-arg").open, true);
  assert.equal(panel(again, "stdin").open, false);
  assert.equal($$(".action")[0].open, true);
  assert.equal(panel(again, "full output stream").open, true);
  assert.equal(callsTo("plan", "stream").length, 2);
});

test("a call record shows how it exited, its error, and what it printed", async () => {
  reset();
  route("GET", `/api/runs/${RUN}/call`, "");
  const view = (record) => {
    const box = globalThis.document.createElement("div");
    box.append(...page("callView")(RUN, "plan", record, new Map()).flat().filter(Boolean));
    return box;
  };
  const base = { command: ["make", "all"], dir: "/work/demo", started_at: at(15) };

  const killed = view({ ...base, ended_at: at(15, 0, 400), exit_code: -1 });
  assert.deepEqual(texts(killed, ".call-meta span").slice(1), ["ran 0.4s", "killed", "in /work/demo"]);
  assert.equal(killed.querySelector(".call-meta .bad").textContent, "killed");
  assert.deepEqual(texts(killed, ".cmd-line"), ["$ make all"]);
  assert.deepEqual(texts(killed, ".block-empty"), ["stdout printed nothing", "stderr printed nothing"]);

  const failed = view({ ...base, ended_at: at(17, 5), exit_code: 2, error: "make: no rule", stdout: "partial" });
  assert.deepEqual(texts(failed, ".call-meta span").slice(1), ["ran 2m 05s", "exit 2", "in /work/demo"]);
  assert.equal(failed.querySelector(".error-box").textContent, "make: no rule");
  assert.equal(panel(failed, "stdout").querySelector("pre").textContent, "partial");
  assert.deepEqual(texts(failed, ".block-empty"), ["stderr printed nothing"]);

  const going = view({ ...base, stream: true, actions: [{ id: "a1", tool: "Bash", input: "make", started_at: at(15) }] });
  assert.deepEqual(texts(going, ".call-meta span").slice(1), ["still running", "in /work/demo"]);
  assert.equal(has(going, ".block-empty"), false);
  const stream = panel(going, "stdout");
  click(stream.querySelector("summary"));
  await settle(20);
  assert.equal(stream.querySelector("pre").textContent, "(empty)");

  assert.match(going.querySelector(".action-time").textContent, /^running (\d+\.\ds|\d+[hm] \d+[ms])$/);
  const live = going.querySelector(".action-time [data-tick]");
  globalThis.document.body.append(going);
  live.textContent = "-";
  await settle(1100);
  assert.match(live.textContent, /^(\d+\.\ds|\d+[hm] \d+[ms])$/);
  going.remove();
});

test("a call that was not recorded says why", async () => {
  reset();
  route("GET", `/api/runs/${RUN}/call`, fail(404, "nothing recorded for this step yet"));
  await openRun(runOf("done", { plan: planDone, test: testDone }, { ended_at: at(16) }));
  click(head("test"));
  click(callOf("test").querySelector("summary"));
  await settle(20);
  assert.equal(callOf("test").querySelector(".call-body .hint").textContent, "nothing recorded for this step yet");
});

test("artifacts render by kind, and images open in a lightbox driven by keys", async () => {
  reset();
  const artifacts = [
    { kind: "video", path: "/runs/1/demo.mp4", name: "demo.mp4" },
    { kind: "image", path: "/runs/1/before.png", name: "before.png" },
    { kind: "image", path: "/runs/1/after.png", name: "after.png" },
    { kind: "markdown", path: "/runs/1/report.md", name: "report.md" },
    { kind: "diff", path: "/runs/1/change.diff", name: "change.diff" },
    { kind: "diff", path: "/runs/1/empty.diff", name: "empty.diff" },
    { kind: "json", path: "/runs/1/result.json", name: "result.json" },
    { kind: "json", path: "/runs/1/broken.json", name: "broken.json" },
    { kind: "text", path: "/runs/1/notes.txt", name: "notes.txt" },
  ];
  const files = {
    "/runs/1/report.md": "<h1>Report</h1><p>All good</p>",
    "/runs/1/change.diff": "diff --git a/auth.go b/auth.go\nindex 1..2\n--- a/auth.go\n+++ b/auth.go\n@@ -1 +1 @@\n-old\n+new\n same",
    "/runs/1/empty.diff": "  \n",
    "/runs/1/result.json": "{\"ok\":true}",
    "/runs/1/broken.json": "{not json",
    "/runs/1/notes.txt": "plain notes",
  };
  route("GET", /^\/api\/runs\/[^/]+\/(file|doc)$/, (c) => files[c.query.path]);
  const run = runOf("done", { plan: planDone, test: { ...testDone, artifacts } }, { ended_at: at(16) });
  const source = await openRun(run);
  click(head("test"));
  const arts = row("test").querySelector(".artifacts");
  const url = (path, kind = "file") => `/api/runs/${RUN}/${kind}?path=${encodeURIComponent(path)}`;

  const video = arts.querySelector("figure.video video");
  assert.equal(video.getAttribute("src"), `${url("/runs/1/demo.mp4")}#t=1`);
  assert.equal(video.hasAttribute("controls"), true);
  assert.equal(arts.querySelector("figure.video figcaption").textContent, "demo.mp4");

  const docs = [...arts.querySelectorAll(".doc")];
  assert.deepEqual(docs.map((d) => d.querySelector(".doc-head").textContent), ["report.md", "change.diff", "empty.diff", "result.json", "broken.json", "notes.txt"]);
  assert.ok(calls.some((c) => c.path === `/api/runs/${RUN}/doc` && c.query.path === "/runs/1/report.md"));
  assert.ok(calls.some((c) => c.path === `/api/runs/${RUN}/file` && c.query.path === "/runs/1/notes.txt"));
  assert.equal(docs[0].querySelector(".doc-body h1").textContent, "Report");
  assert.deepEqual([...docs[1].querySelectorAll(".diff span")].map((s) => s.className), ["meta", "meta", "meta", "meta", "hunk", "del", "add", ""]);
  assert.equal(docs[1].querySelector(".diff").textContent, `${files["/runs/1/change.diff"]}\n`);
  assert.equal(docs[2].querySelector(".doc-body").textContent, "No changes.");
  assert.equal(docs[3].querySelector("pre.plain").textContent, "{\n  \"ok\": true\n}");
  assert.equal(docs[4].querySelector("pre.plain").textContent, "{not json");
  assert.equal(docs[5].querySelector("pre.plain").textContent, "plain notes");
  assert.equal(has(arts, ".doc-more"), false, "short documents drop the Show all button");

  source.send({ ...run, steps: run.steps.map((s) => (s.id === "test" ? { ...s, artifacts: [{ kind: "text", path: "/runs/1/notes.txt", name: "notes.txt" }] } : s)) });
  const more = row("test").querySelector(".doc-more");
  const doc = row("test").querySelector(".doc");
  click(more);
  assert.equal(doc.classList.contains("open"), true);
  assert.equal(more.textContent, "Show less");
  click(more);
  assert.equal(doc.classList.contains("open"), false);
  assert.equal(more.textContent, "Show all");
  await settle(20);

  source.send(run);
  const lightbox = $("#lightbox");
  const image = () => lightbox.querySelector("img").getAttribute("src");
  const stills = [...row("test").querySelectorAll(".stills figure")];
  assert.deepEqual(stills.map((f) => f.querySelector("img").getAttribute("alt")), ["before.png", "after.png"]);
  assert.equal(lightbox.hidden, true);
  click(stills[1]);
  assert.equal(lightbox.hidden, false);
  assert.equal(image(), url("/runs/1/after.png"));
  press(globalThis.document, "ArrowRight");
  assert.equal(image(), url("/runs/1/before.png"));
  press(globalThis.document, "ArrowLeft");
  assert.equal(image(), url("/runs/1/after.png"));
  press(globalThis.document, "ArrowLeft");
  assert.equal(image(), url("/runs/1/before.png"));
  press(globalThis.document, "Enter");
  assert.equal(lightbox.hidden, false);
  press(globalThis.document, "Escape");
  assert.equal(lightbox.hidden, true);
  press(globalThis.document, "ArrowRight");
  assert.equal(image(), url("/runs/1/before.png"));

  click(stills[0]);
  assert.equal(lightbox.hidden, false);
  click(lightbox);
  assert.equal(lightbox.hidden, true);
  await settle(20);
});

test("a question renders as markdown, shows the files it links in place and marks the ones the step does not show", async () => {
  reset();
  const shots = "/runs/1/screenshots";
  const artifacts = [
    { kind: "image", path: `${shots}/after-1.png`, name: "after-1.png" },
    { kind: "image", path: `${shots}/before-1.png`, name: "before-1.png" },
    { kind: "image", path: "/runs/1/extra.png", name: "extra.png" },
    { kind: "video", path: "/runs/1/clip.webm", name: "clip.webm" },
    { kind: "markdown", path: `${shots}/repro.md`, name: "repro.md" },
    { kind: "diff", path: "/runs/1/change.diff", name: "change.diff" },
  ];
  const question = "Open this PR?\n\n| Before | After |\n| --- | --- |\n| ![before](screenshots/before-1.png) | ![after](screenshots/after-1.png) |";
  route("POST", "/api/markdown", { html: "<p>Open this PR?</p><table><tr><td><img src=\"screenshots/before-1.png\" alt=\"before\"></td><td><img src=\"./screenshots/after%2D1.png\" alt=\"after\"></td></tr></table>"
    + "<p><img src=\"clip.webm\" alt=\"clip\"><a href=\"screenshots/repro.md\">steps</a><a href=\"https://github.com/o/r/pull/1\">pr</a><a href=\"#top\">top</a>"
    + "<img src=\"screenshots/after-9.png\" alt=\"gone\"><a href=\"nowhere.txt\">gone link</a></p>" });
  route("GET", /^\/api\/runs\/[^/]+\/(file|doc)$/, (c) => (c.query.path.endsWith("repro.md") ? "<h2>Bug</h2><p><img src=\"before-1.png\" alt=\"b\"><img src=\"../extra.png\" alt=\"e\"></p>" : ""));
  await openRun(runOf("waiting", { plan: planDone, test: testDone, review: { status: "waiting", started_at: at(16), question, artifacts } }));
  await settle(20);
  const url = (path) => `/api/runs/${RUN}/file?path=${encodeURIComponent(path)}`;

  assert.deepEqual(calls.filter((c) => c.path === "/api/markdown").map((c) => JSON.parse(c.body)), [{ text: question }, { text: question }]);
  const asked = $(".decision .question.md");
  assert.deepEqual([...asked.querySelectorAll("td img")].map((img) => img.getAttribute("src")), [url(`${shots}/before-1.png`), url(`${shots}/after-1.png`)]);
  assert.equal(asked.querySelector("video").getAttribute("src"), `${url("/runs/1/clip.webm")}#t=1`);
  const links = [...asked.querySelectorAll("a")];
  assert.deepEqual(links.map((a) => [a.textContent, a.getAttribute("href"), a.getAttribute("target")]), [
    ["steps", url(`${shots}/repro.md`), "_blank"],
    ["pr", "https://github.com/o/r/pull/1", "_blank"],
    ["top", null, null],
  ]);
  assert.deepEqual(texts(asked, ".missing"), ["screenshots/after-9.png is not in this step's show list", "gone link"]);
  assert.equal(asked.querySelector(".missing:last-child").title, "nowhere.txt is not in this step's show list");

  const below = $(".decision .question + .artifacts");
  assert.deepEqual([...below.querySelectorAll(".stills img")].map((img) => img.getAttribute("alt")), ["extra.png"]);
  assert.equal(has(below, "figure.video"), false);
  assert.deepEqual(texts(below, ".doc-head"), ["repro.md", "change.diff"]);
  assert.deepEqual([...below.querySelectorAll(".doc-body.md img")].map((img) => img.getAttribute("src")), [url(`${shots}/before-1.png`), url("/runs/1/extra.png")]);

  const lightbox = $("#lightbox");
  click(asked.querySelectorAll("td img")[1]);
  assert.equal(lightbox.hidden, false);
  assert.equal(lightbox.querySelector("img").getAttribute("src"), url(`${shots}/after-1.png`));
  press(globalThis.document, "ArrowRight");
  assert.equal(lightbox.querySelector("img").getAttribute("src"), url(`${shots}/before-1.png`));
  press(globalThis.document, "Escape");

  click(head("review"));
  assert.ok(has(row("review"), ".srow-body .question.md td img"), "the step row shows the same rendered question");

  route("POST", "/api/markdown", fail(500, "down"));
  await openRun(runOf("waiting", { plan: planDone, test: testDone, review: { status: "waiting", started_at: at(16), question, artifacts, round: 1 } }));
  await settle(20);
  assert.equal($(".decision .question").textContent, question);
  assert.equal(has($("#decision"), ".question.md"), false);
  assert.equal($$(".decision .stills img").length, 3, "without markdown every file shows below the question");
});

test("an open step shows its fields, what it read, how it was asked again, and where to edit it", async () => {
  reset();
  route("GET", `/api/runs/${RUN}/call`, callRecord);
  const plan = { ...planDone, round: 1, resumed: "plan", session: "sess-2", asked_again: "plan.again", sent_back: "Check the expiry too" };
  const done = { status: "done", started_at: at(16), ended_at: at(16, 5) };
  const steps = {
    plan,
    test: { ...testDone, tokens: 900, ended: "Stopped: the tests are flaky", headline: "" },
    review: { ...done, question: "Ship this change?", output: { decision: "approve", note: "fine" } },
    learn: { ...done, tokens: 12000 },
    cleanup: done,
  };
  await openRun(runOf("done", steps, { ended_at: at(17) }));

  assert.deepEqual(texts(row("plan"), ".chip"), ["attempt 2", "resumed", "re-prompted"]);
  assert.equal(row("test").querySelector(".srow-line").textContent, "Stopped: the tests are flaky");
  assert.equal(row("test").querySelector(".srow-stats").textContent, "8.0s · 0.9k tokens");
  assert.equal(row("learn").querySelector(".srow-stats").textContent, "5.0s · 12k tokens");
  assert.deepEqual(texts(row("learn"), ".chip"), [], "an always step inside its outline block needs no chip");

  click(head("plan"));
  const body = row("plan").querySelector(".srow-body");
  const hints = [...body.children].filter((e) => e.classList.contains("hint")).map((e) => e.textContent);
  assert.deepEqual(hints.slice(0, 2), ["Sent back with: Check the expiry too", "Resumed the session of plan · session sess-2"]);
  assert.match(hints[2], /^The answer lacked a JSON object with every field/);
  const askedAgain = panel(body, "Re-prompt call");
  click(askedAgain.querySelector("summary"));
  await settle(20);
  assert.ok(calls.some((c) => c.path === `/api/runs/${RUN}/call` && c.query.log === "plan.again"));

  const reads = panel(body, "Reads (2)");
  const lines = [...reads.querySelectorAll(".read")];
  assert.deepEqual(lines.map((l) => l.querySelector(".ol-name").textContent), ["request", "memory"]);
  assert.equal(lines[0].querySelector(".ol-from").textContent, "input “What to fix”");
  assert.equal(lines[0].querySelector(".ol-value").textContent, "value “Login fails”");
  assert.equal(lines[1].querySelector(".ol-from").textContent, "built-in from Memory");
  assert.equal(lines[1].querySelector(".ol-from a").getAttribute("href"), "#/memory");
  assert.equal(has(lines[1], ".ol-value"), false);
  assert.deepEqual([...body.querySelectorAll(".srow-links a")].map((a) => [a.textContent, a.getAttribute("href")]),
    [["Open prompt plan", "#/prompt/fix/plan"], ["Edit step", "#/chain/fix/plan"]]);

  click(head("learn"));
  assert.equal(row("learn").querySelector(".srow-body > .hint").textContent, "New session · no session id, not resumable");

  click(head("test"));
  const testBody = row("test").querySelector(".srow-body");
  assert.equal(testBody.querySelector(".ended-box").textContent, "Ended the run: Stopped: the tests are flaky");
  const output = panel(testBody, "Output");
  assert.equal(output.open, false);
  assert.deepEqual(texts(output, "dt"), ["verdict"]);
  assert.deepEqual([...testBody.querySelectorAll(".srow-links a")].map((a) => a.getAttribute("href")), ["#/tool/test", "#/chain/fix/test"]);
  const read = panel(testBody, "Reads (1)").querySelector(".read");
  assert.equal(read.querySelector(".ol-from").textContent, "set by Plan the fix");
  assert.equal(read.querySelector(".ol-value").textContent, "value “auth.go, auth_test.go”");

  click(head("plan"));
  assert.equal(isOpen("plan"), false);
  click(read.querySelector(".ol-from a"));
  assert.equal(isOpen("plan"), true);

  click(head("review"));
  const reviewBody = row("review").querySelector(".srow-body");
  assert.equal(reviewBody.querySelector(".question").textContent, "Ship this change?");
  assert.equal(panel(reviewBody, "Output") === undefined, true);
  assert.deepEqual(texts(reviewBody, ".read .ol-name"), ["plan.summary", "request"]);
  assert.deepEqual(texts(reviewBody, ".srow-links a"), ["Edit step"]);

  click(head("cleanup"));
  const cleanup = row("cleanup").querySelector(".srow-body");
  assert.deepEqual(texts(cleanup, ".read .ol-from"), ["var, computed at start", "nothing sets it"]);
  assert.equal(has(cleanup, ".ol-from.missing"), true);

  click(panel(row("plan"), "Reads").querySelector(".read .ol-from a"));
  await settle(30);
  assert.equal(hash(), "#/chain/fix");
});

test("a project value for an input and a long value are both shown", async () => {
  reset();
  const request = "r".repeat(100);
  const run = runOf("running", { plan: { status: "running", started_at: at(15) } }, { inputs: { request } });
  await openRun(run, { ...library, input_values: { request: "Login fails" } });
  click(head("plan"));
  const read = panel(row("plan"), "Reads").querySelector(".read");
  assert.equal(read.querySelector(".ol-from").textContent, "input “What to fix”, project default in Settings");
  assert.equal(read.querySelector(".ol-value").textContent, `value “${"r".repeat(80)}…”`);
});

test("earlier attempts list each try with its own call", async () => {
  reset();
  route("GET", `/api/runs/${RUN}/call`, callRecord);
  const earlier = [
    { id: "plan", path: "plan", title: "Plan the fix", kind: "agent", uses: "plan", status: "failed", started_at: at(14), ended_at: at(14, 9), headline: "First plan", error: "no JSON in the answer", output: { reason: "timeout" }, log: "plan.1", asked_again: "plan.1.again" },
    { id: "plan", path: "plan", title: "Plan the fix", kind: "agent", uses: "plan", status: "stopped" },
  ];
  await openRun(runOf("done", { plan: { ...planDone, round: 2, earlier }, test: testDone }, { ended_at: at(16) }));
  click(head("plan"));
  const list = panel(row("plan"), "Earlier attempts (2)");
  const attempts = [...list.querySelectorAll(".attempt")];
  assert.deepEqual(attempts.map((a) => a.querySelector(".attempt-head").textContent), ["Attempt 19.0s", "Attempt 2stopped"]);
  assert.equal(attempts[0].querySelector(".headline").textContent, "First plan");
  assert.equal(attempts[0].querySelector(".error-box").textContent, "no JSON in the answer");
  assert.deepEqual(texts(attempts[0], "dt"), ["reason"]);
  assert.deepEqual(texts(attempts[0], "details.call > summary"), ["Call", "Re-prompt call"]);
  assert.equal(has(attempts[1], "details.call"), false);

  click(attempts[0].querySelector("details.call > summary"));
  await settle(20);
  assert.ok(calls.some((c) => c.path === `/api/runs/${RUN}/call` && c.query.log === "plan.1"));
});

test("a run of a chain that is gone still lists its steps", async () => {
  reset();
  const run = { ...runOf("done", { plan: planDone, test: testDone, cleanup: { status: "done" } }, { ended_at: at(16) }), id: "1028-120000", chain: "gone" };
  await openRun(run);
  assert.equal($$("#steps > .slot").length, 0);
  assert.equal(row("plan").parentElement.id, "steps");
  assert.deepEqual(texts(row("cleanup"), ".chip"), ["always"]);
  click(head("plan"));
  assert.equal(panel(row("plan"), "Reads") === undefined, true);
  assert.deepEqual([...row("plan").querySelectorAll(".srow-links a")].map((a) => a.getAttribute("href")), ["#/prompt/gone/plan", "#/chain/gone/plan"]);
});
