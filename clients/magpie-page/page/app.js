// app.js reads magpie's ledger and draws it. It never reaches mbridge:
// the sandbox a magpie page runs in has no network at all (connect-src
// 'none'), and the only thing this page needs is what magpie already
// recorded (SP14 §2).
//
// A classic script, like the community's usage page: it reads the view
// from globalThis.
(async () => {
  const V = globalThis.MbridgeView;
  const $ = (id) => document.getElementById(id);
  const theme = await magpie.ready;
  let lang = theme.lang;

  // the period and the sort live in this plugin's own options, so the
  // page opens where it was left
  let opts = {};
  try {
    opts = (await magpie.options.get()) || {};
  } catch {
    // no options yet: the defaults in package.json stand
  }
  const state = {
    period: V.PERIODS.includes(opts.period) ? opts.period : "7d",
    sort: V.SORTS.includes(opts.sort) ? opts.sort : "tokens",
    summary: null,
    error: null,
  };

  function words() {
    const t = V.text(lang);
    document.querySelectorAll("[data-t]").forEach((e) => {
      e.textContent = t[e.dataset.t];
    });
    $("status").textContent = t.status;
    $("restart").textContent = t.restart;
    $("cmd").textContent = "mbridge service restart";
  }

  function pick(group, attr, value) {
    $(group).querySelectorAll("button").forEach((b) => {
      b.setAttribute("aria-selected", String(b.dataset[attr] === value));
    });
  }

  function paint() {
    pick("periods", "period", state.period);
    pick("sorts", "sort", state.sort);
    const s = state.summary;
    if (!s) return;
    $("since").textContent = V.since(s, lang);

    $("tiles").replaceChildren(
      ...V.tiles(s, lang).map((tile) => {
        const cell = document.createElement("div");
        cell.className = "tile";
        for (const [cls, value] of [["k", tile.label], ["v", tile.value], ["s", tile.sub]]) {
          if (value === undefined) continue;
          const e = document.createElement("div");
          e.className = cls;
          e.textContent = value;
          cell.append(e);
        }
        return cell;
      })
    );

    const rows = V.rows(s, state.sort, lang);
    $("rows").replaceChildren(
      ...rows.map((r) => {
        const tr = document.createElement("tr");
        const name = document.createElement("td");
        name.className = "model";
        name.textContent = r.model;
        const bar = document.createElement("span");
        bar.className = "bar";
        const fill = document.createElement("i");
        fill.style.width = Math.max(2, Math.round(r.shareValue * 100)) + "%";
        bar.append(fill);
        name.append(bar);
        const calls = document.createElement("td");
        calls.className = "num";
        calls.textContent = r.calls;
        const tokens = document.createElement("td");
        tokens.className = "num";
        tokens.textContent = r.tokens;
        const cost = document.createElement("td");
        cost.className = "num";
        cost.textContent = r.cost;
        tr.append(name, calls, tokens, cost);
        return tr;
      })
    );
    $("empty").hidden = rows.length > 0;
    $("empty").textContent = V.text(lang).empty;
    $("error").hidden = !state.error;
    if (state.error) {
      $("error").textContent = V.text(lang).failed + ": " + state.error + " · " + V.text(lang).retry;
    }
  }

  async function load() {
    try {
      state.summary = await magpie.read("usage:summary", { period: state.period });
      state.error = null;
    } catch (e) {
      state.error = (e && e.message) || String(e);
    }
    paint();
  }

  // magpie takes one options write a second: a quick second click is
  // saved with the next one
  let saving = null;
  function save() {
    if (saving) return;
    saving = setTimeout(async () => {
      saving = null;
      try {
        opts = (await magpie.options.set({ ...opts, period: state.period, sort: state.sort })) || opts;
      } catch {
        // not remembering is not worth an error on the page
      }
    }, 1100);
  }

  $("periods").addEventListener("click", (e) => {
    const b = e.target.closest("button[data-period]");
    if (!b || b.dataset.period === state.period) return;
    state.period = b.dataset.period;
    save();
    load();
  });

  $("sorts").addEventListener("click", (e) => {
    const b = e.target.closest("button[data-sort]");
    if (!b || b.dataset.sort === state.sort) return;
    state.sort = b.dataset.sort;
    save();
    paint();
  });

  // the period and sort buttons carry their own words
  for (const b of $("periods").querySelectorAll("button[data-period]")) {
    b.textContent = V.periodLabel(b.dataset.period, lang);
  }
  for (const b of $("sorts").querySelectorAll("button[data-sort]")) {
    b.textContent = V.sortLabel(b.dataset.sort, lang);
  }
  $("addr").textContent = V.STATUS_URL;
  words();
  await load();
  setInterval(load, 60000);
})();
