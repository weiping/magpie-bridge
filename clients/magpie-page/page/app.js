// app.js reads what mbridge published into the plugin's options, and
// draws it. It never reaches mbridge: the sandbox a magpie page runs in
// has no network at all (connect-src 'none'), so mbridge pushes its
// state into the options and the page reads them in-process (SP14 §7).
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

  // the page's own keys in the options (kept so mbridge's key survives
  // a write)
  let opts = {};
  try {
    opts = (await magpie.options.get()) || {};
  } catch {
    // no options yet: mbridge publishes them
  }
  const state = {
    mbridge: null,
    cmdSeq: 0,
  };

  // the address of mbridge's own page: selectable text, not a link (the
  // sandbox may navigate itself, but that is a one-way ticket, SP14 §2)
  function words() {
    const t = V.text(lang);
    document.querySelectorAll("[data-t]").forEach((e) => {
      e.textContent = t[e.dataset.t];
    });
    $("status").textContent = t.status;
    $("restart").textContent = t.restart;
    $("cmd").textContent = "mbridge service restart";
    $("addr").textContent = V.STATUS_URL;
    $("mbridge-ask").textContent = t.mbridgeAsk;
    $("mbridge-restart").textContent = t.mbridgeRestart;
  }

  // readOptions reads the options: what mbridge published. What we write
  // back must be what we just read, so mbridge's snapshot is not put back
  // stale.
  async function readOptions() {
    try {
      opts = (await magpie.options.get()) || {};
      state.mbridge = opts.mbridge || null;
    } catch {
      // the options are not readable: keep what we have
    }
  }

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
    paintMbridge();
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
    } catch {
      // mbridge will not hear it; the page says so
      return false;
    }
    return true;
  }

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
  await readOptions();
  paint();
  // mbridge's snapshot moves every fifteen seconds, and reading the
  // options is free
  setInterval(async () => {
    await readOptions();
    paint();
  }, 5000);
})();
