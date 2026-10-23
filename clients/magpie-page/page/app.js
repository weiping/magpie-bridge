// app.js powers the 鹊桥 plugin page inside magpie's window.
// It reads the snapshot mbridge published into magpie.options,
// renders the full dashboard with error boundaries, and dispatches user actions.

(async () => {
  try {
    const V = globalThis.MbridgeView;
    const theme = await magpie.ready;
    let lang = "en";
    if (theme && theme.lang) {
      if (String(theme.lang).toLowerCase().startsWith("zh")) {
        lang = "zh";
      }
    } else if (typeof navigator !== "undefined" && String(navigator.language || "").toLowerCase().startsWith("zh")) {
      lang = "zh";
    }

    let opts = {};
    let lastGoodSnap = null;
    let lastRenderedKey = "";

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

    async function readOptions() {
      try {
        const raw = await magpie.options.get();
        if (raw && typeof raw === "object") {
          opts = raw;
          if (opts.mbridge && typeof opts.mbridge === "object" && opts.mbridge.pid) {
            lastGoodSnap = opts.mbridge;
            return opts.mbridge;
          }
        }
        // If mbridge is concurrently writing a new tier-switch snapshot into plugins.json,
        // keep the last known good snapshot instead of flickering to the setup guide.
        return lastGoodSnap;
      } catch {
        return lastGoodSnap;
      }
    }

    function renderView(data) {
      try {
        const isLive = Boolean(
          data &&
          typeof data === "object" &&
          data.pid &&
          (data.service ? data.service.running !== false : true)
        );
        if (!isLive) {
          if (lastRenderedKey !== "GUIDE") {
            lastRenderedKey = "GUIDE";
            V.renderGuide(null, lang);
          }
          return;
        }

        // Only re-render the DOM when the snapshot sequence or content actually changed,
        // preventing unnecessary DOM teardown/rebuild and WebKit ResizeObserver loop traps.
        const renderKey = "LIVE:" + String(data.seq || "") + ":" + String(data.at || "") + ":" + String((data.live || []).length);
        if (renderKey === lastRenderedKey) {
          return;
        }
        lastRenderedKey = renderKey;

        V.render(data);

        // Wire up the restart button in the rendered view
        const restartBtn = document.querySelector(".restart");
        if (restartBtn) {
          restartBtn.onclick = async () => {
            try {
              restartBtn.disabled = true;
              restartBtn.textContent = "正在重启…";
              const ok = await command("restart");
              if (ok) {
                restartBtn.textContent = "已发出重启指令";
              } else {
                restartBtn.textContent = "指令发送失败";
                restartBtn.disabled = false;
              }
            } catch {
              restartBtn.disabled = false;
            }
          };
        }
      } catch (err) {
        console.warn("[mbridge-page] renderView caught error:", err);
      }
    }

    async function poll() {
      try {
        const snap = await readOptions();
        renderView(snap);
      } catch (err) {
        console.warn("[mbridge-page] poll caught error:", err);
      }
    }

    globalThis.pollMbridge = () => {
      lastRenderedKey = "";
      return poll();
    };

    await poll();
    setInterval(poll, 5000);
  } catch (err) {
    console.warn("[mbridge-page] startup caught error:", err);
  }
})();
