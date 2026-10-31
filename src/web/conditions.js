const CONDITION_OPS = [["is", "is"], ["isnt", "is not"], ["empty", "is empty"], ["set", "is not empty"]];

function readCondition(text) {
  const m = /^\s*([a-z][a-z0-9_.]*)\s*(==|!=)\s*(.*?)\s*$/.exec(text || "");
  if (!m) return null;
  const value = m[3].replace(/^["']+|["']+$/g, "");
  const op = value ? (m[2] === "==" ? "is" : "isnt") : (m[2] === "==" ? "empty" : "set");
  return { name: m[1], op, value };
}

function writeCondition({ name, op, value }) {
  if (!name) return undefined;
  if (op === "empty") return `${name} == `;
  if (op === "set") return `${name} != `;
  return `${name} ${op === "is" ? "==" : "!="} ${value}`;
}

function describeCondition(text) {
  const c = readCondition(text);
  if (!c) return text;
  return { is: `${c.name} is ${c.value}`, isnt: `${c.name} is not ${c.value}`, empty: `${c.name} is empty`, set: `${c.name} is not empty` }[c.op];
}

function conditionEditor(text, [offLabel, onLabel], names, onChange) {
  const parsed = readCondition(text);
  const state = { on: !!text, raw: text && !parsed ? text : "", cond: parsed || { name: "", op: "is", value: "" } };
  const listID = `names-${Math.random().toString(36).slice(2, 9)}`;
  const wrap = h("div", { class: "cond" });
  const emit = () => onChange(state.on ? state.raw || writeCondition(state.cond) : undefined);
  const draw = () => {
    const parts = [selectBox([["off", offLabel], ["on", onLabel]], state.on ? "on" : "off", (v) => { state.on = v === "on"; emit(); draw(); })];
    if (state.on && state.raw) {
      parts.push(textInput(state.raw, (v) => { state.raw = v; emit(); }, { class: "code wide" }), h("span", { class: "hint" }, "name == value or name != value"));
    } else if (state.on) {
      const wantsValue = state.cond.op === "is" || state.cond.op === "isnt";
      parts.push(
        textInput(state.cond.name, (v) => { state.cond.name = v.trim(); emit(); }, { class: "code", list: listID, placeholder: names[0] || "name" }),
        selectBox(CONDITION_OPS, state.cond.op, (v) => { state.cond.op = v; emit(); draw(); }),
        wantsValue ? textInput(state.cond.value, (v) => { state.cond.value = v; emit(); }, { class: "code", placeholder: "e.g. APPROVE" }) : null,
        h("datalist", { id: listID }, names.map((n) => h("option", { value: n }))));
    }
    wrap.replaceChildren(...parts.filter(Boolean));
  };
  draw();
  return wrap;
}
