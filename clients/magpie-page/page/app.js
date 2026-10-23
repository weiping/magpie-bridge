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
  // el makes an element with text and class
  function el(tag, attrs, kids) {
    const n = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) {
      if (v === undefined || v === null) continue;
      if (k === "text") n.textContent = v;
      else if (k === "class") n.className = v;
      else n.setAttribute(k, v);
    }
    for (const kid of [].concat(kids || [])) {
      if (kid === undefined || kid === null) continue;
      n.appendChild(typeof kid === "object" ? kid : document.createTextNode(String(kid)));
    }
    return n;
  }
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
    // what mbridge published into the options (SP14 §7)
    mbridge: null,
    // the sequence number of the last command the page sent
    cmdSeq: 0,
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

  // paintMbridge draws what mbridge published: its numbers, the sessions
  // running now, and the decisions it made.
  function paintMbridge() {
    const t = V.text(lang);
    const v = V.mbridgeView(state.mbridge, lang);
    $("mbridge-age").textContent = v.none ? "" : (t.mbridgeAge + " " + v.ageText);
    $("mbridge-empty").textContent = v.none ? v.text : (v.noDecisions ? t.noDecisions : "");
    $("mbridge-empty").hidden = !(v.none || v.noDecisions);
    const stats = $("mbridge-stats");
    stats.textContent = "";
    if (!v.none) {
      const items = [
        [String(v.turnCount), t.mbridgeTurns],
        [v.coverage, t.mbridgeCoverage],
        [String(v.failures), t.mbridgeFailures],
        [v.topReason || "—", t.reason],
      ];
      for (const [value, label] of items) {
        const cell = document.createElement("div");
        cell.className = "stat";
        const b = document.createElement("b");
        b.textContent = value;
        const s2 = document.createElement("span");
        s2.textContent = label;
        cell.append(b, s2);
        stats.append(cell);
      }
    }
    const live = $("mbridge-live");
    live.textContent = "";
    if (v.none) {
      live.append(el("div", { class: "muted", text: t.mbridgeNone }));
    } else if (v.emptyLive) {
      live.append(el("div", { class: "muted", text: t.mbridgeNoLive }));
    } else {
      for (const c of v.live) {
        const bits = [];
        if (c.reason) bits.push(c.reason);
        if (c.conf !== undefined && c.conf !== null) bits.push("conf " + c.conf.toFixed(2));
        if (c.tools !== null && c.tools !== undefined) bits.push(t.tools + " " + c.tools);
        live.append(el("div", { class: "card" }, [
          el("div", { class: "row1" }, [
            el("span", { text: c.harness + " · " + c.agent }),
            el("span", { text: c.tier + (c.group ? " → " + c.group : "") }),
          ]),
          el("div", { class: "row2", text: c.session + " · " + bits.join(" · ") }),
        ]));
      }
    }
    const body = $("mbridge-decisions");
    body.textContent = "";
    for (const d of v.decisions || []) {
      const tr = document.createElement("tr");
      const cells = [
        [d.clock, "muted"],
        [d.harness, ""],
        [d.agent, ""],
        [d.tier, ""],
        [d.advised, d.differs ? "differs" : "muted"],
        [d.reason, "muted"],
        [d.conf === undefined || d.conf === null ? "—" : d.conf.toFixed(2), "num"],
      ];
      for (const [text, cls] of cells) {
        const td = document.createElement("td");
        if (cls) td.className = cls;
        td.textContent = text;
        tr.append(td);
      }
      body.append(tr);
    }
  }

  function paint() {
    pick("periods", "period", state.period);
    pick("sorts", "sort", state.sort);
    // mbridge's part does not depend on magpie's usage
    paintMbridge();
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

  // readOptions reads the plugin's options: the page's period and sort,
  // and what mbridge published there.
  async function readOptions() {
    try {
      const o = (await magpie.options.get()) || {};
      if (V.PERIODS.includes(o.period)) state.period = o.period;
      if (V.SORTS.includes(o.sort)) state.sort = o.sort;
      state.mbridge = o.mbridge || null;
    } catch {
      // the options are not readable: keep what we have
    }
  }

  async function load() {
    await readOptions();
    try {
      state.summary = await magpie.read("usage:summary", { period: state.period });
      state.error = null;
    } catch (e) {
      state.error = (e && e.message) || String(e);
    }
    paint();
  }

  // command asks mbridge to do something. The sequence number makes it
  // one-shot: the page repeats it until mbridge answers, and mbridge
  // runs it once.
  async function command(id) {
    state.cmdSeq += 1;
    const cmd = { id: id, seq: state.cmdSeq };
    try {
      const o = (await magpie.options.get()) || {};
      o.cmd = cmd;
      await magpie.options.set(o);
    } catch (e) {
      // mbridge will not hear it; the page says so
      return false;
    }
    return true;
  }

  // magpie takes one options write a second: a quick second click is
  // saved with the next one
  let saving = null;
  function save() {
    if (saving) return;
    saving = setTimeout(async () => {
      saving = null;
      try {
        // the whole object is replaced: mbridge's key goes with it, and
        // mbridge puts its snapshot back on its next round
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
  $("mbridge-ask").textContent = V.text(lang).mbridgeAsk;
  $("mbridge-restart").textContent = V.text(lang).mbridgeRestart;
  $("mbridge-ask").addEventListener("click", async () => {
    const b = $("mbridge-ask");
    const t = V.text(lang);
    b.disabled = true;
    b.textContent = t.mbridgeAsking;
    const ok = await command("refresh");
    if (!ok) { b.disabled = false; b.textContent = t.mbridgeAsk; }
    // mbridge publishes and the next poll picks it up
    setTimeout(() => { b.disabled = false; b.textContent = t.mbridgeAsk; load(); }, 3000);
  });
  $("mbridge-restart").addEventListener("click", async () => {
    const b = $("mbridge-restart");
    const t = V.text(lang);
    b.disabled = true;
    b.textContent = t.mbridgeRestarting;
    const ok = await command("restart");
    if (!ok) { b.disabled = false; b.textContent = t.mbridgeRestart; return; }
    b.textContent = t.mbridgeRestartAsked;
  });
  words();
  await load();
  setInterval(load, 60000);
})();
