// app.js powers the 鹊桥 plugin page inside magpie's window.
// It reads the snapshot mbridge published into magpie.options,
// renders the full dashboard, and dispatches user actions (window tabs,
// restart, and refresh).

(async () => {
  const V = globalThis.MbridgeView;
  const theme = await magpie.ready;
  let lang = "en";
  if (theme && theme.lang) {
    if (theme.lang.toLowerCase().startsWith("zh")) {
      lang = "zh";
    }
  } else if (typeof navigator !== "undefined" && (navigator.language || "").toLowerCase().startsWith("zh")) {
    lang = "zh";
  }

  let opts = {};
  const current = { window: "7d" };

  function fail(message) {
    const root = document.getElementById("root");
    root.textContent = "";
    const h1 = document.createElement("h1");
    h1.textContent = "鹊桥";
    const banner = document.createElement("div");
    banner.className = "banner bad err";
    banner.textContent = message;
    root.append(h1, banner);
  }

  async function command(id) {
    try {
      const o = (await magpie.options.get()) || {};
      o.cmd = { id: id, seq: Date.now() };
      await magpie.options.set(o);
      return true;
    } catch {
      return false;
    }
  }

  async function setWindow(kind) {
    current.window = kind;
    try {
      const o = (await magpie.options.get()) || {};
      o.period = kind;
      await magpie.options.set(o);
      // Ask mbridge to immediately publish the updated window
      await command("refresh");
    } catch {}
  }

  async function readOptions() {
    try {
      opts = (await magpie.options.get()) || {};
      if (opts.period && (opts.period === "1d" || opts.period === "7d" || opts.period === "30d")) {
        current.window = opts.period;
      }
      return opts.mbridge || null;
    } catch (e) {
      return null;
    }
  }

  function renderView(data) {
    // When mbridge is not running or not installed (empty options or no valid snapshot with a running pid):
    // Display the setup and installation guide instead of empty dashboard!
    const isLive = data && data.pid && (data.service ? data.service.running !== false : true);
    if (!isLive) {
      V.renderGuide(null, lang);
      return;
    }

    V.render(data);

    // Wire up tab switching in the rendered view
    const tabButtons = document.querySelectorAll(".tabs button");
    tabButtons.forEach((btn) => {
      const kind = btn.textContent.trim();
      if (kind === "1d" || kind === "7d" || kind === "30d") {
        btn.onclick = async () => {
          await setWindow(kind);
          tabButtons.forEach((b) => b.setAttribute("aria-pressed", String(b.textContent.trim() === kind)));
        };
      }
    });

    // Wire up the restart button in the rendered view
    const restartBtn = document.querySelector(".restart");
    if (restartBtn) {
      restartBtn.onclick = async () => {
        restartBtn.disabled = true;
        restartBtn.textContent = "正在重启…";
        const ok = await command("restart");
        if (ok) {
          restartBtn.textContent = "已发出重启指令";
        } else {
          restartBtn.textContent = "指令发送失败";
          restartBtn.disabled = false;
        }
      };
    }
  }

  async function poll() {
    const snap = await readOptions();
    renderView(snap);
  }

  globalThis.pollMbridge = poll;

  await poll();
  setInterval(poll, 5000);
})();
