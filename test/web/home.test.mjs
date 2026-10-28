import test from "node:test";
import assert from "node:assert/strict";
import { $, $$, calls, click, dialogs, go, page, reset, route } from "./page.mjs";

const finished = { id: "1028-101500", chain: "fix", title: "Fix a bug", subject: "Login fails", status: "failed", started_at: "2026-10-28T10:15:00Z", ended_at: "2026-10-28T10:16:00Z" };
const going = { ...finished, id: "1028-101600", subject: "Signup", status: "running", ended_at: undefined };

test("the home page lists runs and deletes a finished one", async () => {
  reset();
  let runs = [finished, going];
  route("GET", "/api/runs", () => runs);
  route("DELETE", `/api/runs/${finished.id}`, () => {
    runs = [going];
    return { ok: "true" };
  });
  await page("loadRuns()");
  await go("#/");

  const lines = $$(".run-line");
  assert.equal(lines.length, 2);
  assert.match(lines[0].textContent, /Login fails/);
  assert.equal(lines[1].querySelector("button"), null, "a running run cannot be deleted");

  click(lines[0].querySelector(".run-line-delete"));
  await page("Promise.resolve()");
  await go("#/");
  assert.equal(dialogs.confirm.length, 1);
  assert.ok(calls.some((c) => c.method === "DELETE" && c.path === `/api/runs/${finished.id}`));
  assert.deepEqual($$(".run-line").map((l) => l.querySelector(".run-line-subject").textContent), ["Signup"]);
  assert.equal($("#runs .side-item .text").textContent.startsWith("Signup"), true);
});
