/* app.js reads magpie's ledger and draws it. It never reaches mbridge:
   the sandbox a magpie page runs in has no network at all, and the only
   thing this page needs is what magpie already recorded (SP14 §2). */

import { tiles, rows, periods, sorts, header } from "./view.js";

const REFRESH_MS = 60000; // magpie's ledger moves slowly enough
const STATUS_URL = "http://127.0.0.1:3426/status";
const OPTIONS_KEY = "mbridge-page"; // magpie options keep the period and the sort

const state = { period: "7d", sort: "tokens", summary: null, error: null, lang: "en" };

function $(id) {
  return document.getElementById(id);
}

function segment(container, items, current, onPick) {
  container.textContent = "";
  for (const item of items) {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "seg" + (item.key === current ? " seg-on" : "");
    b.textContent = item.label;
    b.addEventListener("click", () => onPick(item.key));
    container.appendChild(b);
  }
}

function render() {
  const t = header(state.summary, state.lang);
  $("title").textContent = t.title;
  $("subtitle").textContent = state.summary ? t.subtitle : "";
  if (t.since) $("subtitle").textContent += " · " + t.since;

  segment($("periods"), periods(state.lang), state.period, (k) => {
    if (k === state.period) return;
    state.period = k;
    save();
    load();
  });
  segment($("sorts"), sorts(state.lang), state.sort, (k) => {
    state.sort = k;
    save();
    render();
  });

  const box = $("tiles");
  box.textContent = "";
  for (const tile of tiles(state.summary, state.lang)) {
    const cell = document.createElement("div");
    cell.className = "tile";
    const label = document.createElement("div");
    label.className = "tile-label";
    label.textContent = tile.label;
    const value = document.createElement("div");
    value.className = "tile-value";
    value.textContent = tile.value;
    cell.appendChild(label);
    cell.appendChild(value);
    if (tile.sub) {
      const sub = document.createElement("div");
      sub.className = "tile-sub";
      sub.textContent = tile.sub;
      cell.appendChild(sub);
    }
    box.appendChild(cell);
  }

  const r = rows(state.summary, state.sort, state.lang);
  $("models-label").textContent = r.label;
  $("models-sort").textContent = r.sortLabel;
  $("th-model").textContent = viewText("model");
  $("th-calls").textContent = viewText("calls");
  $("th-tokens").textContent = viewText("tokens");
  $("th-cost").textContent = viewText("cost");

  const body = $("rows");
  body.textContent = "";
  for (const item of r.items) {
    const tr = document.createElement("tr");
    const name = document.createElement("td");
    name.className = "model";
    name.textContent = item.model;
    const bar = document.createElement("span");
    bar.className = "bar";
    const fill = document.createElement("i");
    fill.style.width = Math.round(item.shareValue * 100) + "%";
    bar.appendChild(fill);
    name.appendChild(bar);
    tr.appendChild(name);
    for (const key of ["calls", "tokens", "cost"]) {
      const td = document.createElement("td");
      td.className = "num";
      td.textContent = item[key];
      tr.appendChild(td);
    }
    body.appendChild(tr);
  }
  $("empty").textContent = r.items.length ? "" : t.emptyText;

  $("disclaimer").textContent = t.disclaimer;
  $("status-label").textContent = t.status + " ";
  // the address is selectable text, not a link: the sandbox can navigate
  // itself, but that is a one-way ticket (SP14 §2)
  $("status-addr").textContent = t.statusWhere + " " + STATUS_URL;
  $("restart-label").textContent = " · " + t.restart + " ";
  $("error").textContent = state.error ? state.error + " · " + t.retry : "";
}

// viewText is one word from the page's vocabulary.
function viewText(key) {
  const v = globalThis.MbridgeView;
  const l = state.lang === "zh" ? "zh" : "en";
  return v.TEXT[l][key] || key;
}

async function load() {
  try {
    state.summary = await magpie.read("usage:summary", { period: state.period });
    state.error = null;
  } catch (err) {
    state.error = (err && err.message) || String(err);
  }
  render();
}

// save remembers the period and the sort in magpie's own options, so the
// page comes back the way it was left. magpie takes one write a second.
async function save() {
  try {
    const opts = (await magpie.options.get()) || {};
    opts[OPTIONS_KEY] = { period: state.period, sort: state.sort };
    await magpie.options.set(opts);
  } catch {
    // not being able to remember is not worth an error on the page
  }
}

async function restore() {
  try {
    const opts = (await magpie.options.get()) || {};
    const mine = opts[OPTIONS_KEY];
    if (mine && mine.period) state.period = mine.period;
    if (mine && mine.sort) state.sort = mine.sort;
  } catch {
    // the defaults stand
  }
}

(async function start() {
  await magpie.ready;
  const theme = magpie.theme || {};
  if ((theme.lang || "").toLowerCase().startsWith("zh")) state.lang = "zh";
  if (magpie.onTheme) magpie.onTheme((t) => {
    if ((t && t.lang || "").toLowerCase().startsWith("zh")) state.lang = "zh";
    render();
  });
  await restore();
  await load();
  setInterval(load, REFRESH_MS);
})();