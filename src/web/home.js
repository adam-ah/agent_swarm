function showHome() {
  const waiting = runsCache.filter((r) => r.status === "waiting");
  const subchains = lib.chains.filter((c) => callersOf(c).length);
  main.replaceChildren(h("div", { class: "page" },
    h("div", { class: "eyebrow" }, project),
    h("h1", {}, project.split("/").filter(Boolean).pop() || "swarm"),
    waiting.length ? h("section", { class: "section" }, h("h2", {}, "Waiting for you"), h("div", { class: "cards" }, waiting.map(waitingCard))) : null,
    h("section", { class: "section" }, h("h2", {}, "Start a chain"),
      lib.chains.length ? h("div", { class: "cards" }, lib.chains.filter((c) => !subchains.includes(c)).map(chainCard))
        : h("p", { class: "hint" }, "No chains. Add one with +, or run ", h("code", {}, "swarm samples"), ".")),
    subchains.length ? h("section", { class: "section" }, h("h2", {}, "Sub-chains"), h("div", { class: "cards" }, subchains.map(chainCard))) : null,
    h("section", { class: "section" }, h("h2", {}, "Runs"), runList(runsCache, "No runs."))));
}

function waitingCard(run) {
  return h("a", { class: "panel card-link waiting", href: `#/run/${run.id}` },
    h("span", { class: "card-link-title" }, h("span", { class: "dot waiting" }), run.subject || run.title),
    h("span", { class: "hint" }, `${run.title} · started ${ago(run.started_at)}`),
    run.question ? h("span", { class: "card-link-text clamp" }, run.question) : null,
    h("span", { class: "btn small primary" }, "Decide"));
}

function chainCard(chain) {
  const first = (chain.description || "").split(/(?<=\.)\s/)[0];
  return h("a", { class: "panel card-link", href: `#/chain/${chain.name}` },
    h("span", { class: "card-link-title" }, chain.title, TAG[chain.source] ? h("span", { class: "tag" }, TAG[chain.source][0]) : null),
    first ? h("span", { class: "card-link-text clamp" }, first) : null,
    h("span", { class: "hint" }, `${chain.steps?.length || 0} steps`, chain.problems?.length ? " · cannot run" : chain.warnings?.length ? " · warnings" : "",
      callersOf(chain).length ? ` · ${usedByLine(chain)}` : ""));
}

function runList(runs, empty) {
  if (!runs.length) return h("p", { class: "hint" }, empty);
  return h("div", { class: "panel run-list" }, runs.map((r) => h("a", { class: "run-line", href: `#/run/${r.id}` },
    h("span", { class: `dot ${r.status}` }),
    h("span", { class: "run-line-main" }, h("span", { class: "run-line-subject" }, r.subject || r.title), h("span", { class: "hint" }, r.title)),
    h("span", { class: `run-line-status ${r.status}` }, STATUS_LABEL[r.status] || r.status),
    h("span", { class: "run-line-when" }, ago(r.started_at), r.ended_at ? ` · ${duration(r.started_at, r.ended_at)}` : ""),
    r.status === "running" || r.status === "waiting" ? h("span", { class: "run-line-delete" }) : h("button", {
      class: "linkish danger run-line-delete", type: "button", title: "Delete run",
      onclick: (e) => { e.preventDefault(); e.stopPropagation(); deleteRun(r.id); },
    }, "Delete"))));
}
