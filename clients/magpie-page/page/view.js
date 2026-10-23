// view.js is what the page shows, worked out from usage:summary without
// the DOM, so a test can check it.
//
// It is a classic script, not a module: a magpie page runs in a sandbox
// whose CSP serves the plugin's own files, and the community's usage
// page does it this way too (globalThis.UsageView). A module script
// would be a different thing to load, and the test below loads this file
// the same way the page does.

(() => {
  const TEXT = {
    en: {
      title: "mbridge",
      subtitle: "what magpie's ledger recorded",
      calls: "calls",
      errors: "errors",
      errorShare: "of the calls",
      tokens: "tokens",
      cost: "cost",
      models: "by model",
      model: "model",
      today: "today",
      "7d": "7 days",
      "30d": "30 days",
      sortTokens: "tokens",
      sortCalls: "calls",
      sortCost: "cost",
      empty: "no calls in this period",
      failed: "couldn't read usage",
      retry: "try again in a minute",
      since: "since",
      disclaimer:
        "This is everything magpie recorded. It is mbridge's usage only when every client goes through mbridge; whatever you use magpie for directly is in here too.",
      status: "mbridge's own status:",
      restart: "restart it with",
      mbridge: "mbridge",
      mbridgeNone: "mbridge has not published anything yet",
      mbridgeAge: "published",
      mbridgeLive: "sessions running now",
      mbridgeNoLive: "no session is going through mbridge right now",
      mbridgeDecisions: "recent decisions",
      mbridgeWindow: "the last",
      mbridgeTurns: "turns",
      mbridgeCoverage: "asked the classifier",
      mbridgeFailures: "failures",
      mbridgeAsk: "ask for fresh numbers",
      mbridgeAsking: "asking…",
      mbridgeRestart: "restart mbridge",
      mbridgeRestarting: "restarting…",
      mbridgeRestartAsked: "restart asked for",
      mbridgeRestartRefused: "refused",
      tier: "tier",
      advised: "advised",
      reason: "reason",
      conf: "confidence",
      unknown: "unknown",
      running: "running",
      notRunning: "installed, not running",
      notInstalled: "not installed",
      tools: "tools",
      noDecisions: "no decision yet",
    },
    zh: {
      title: "mbridge",
      subtitle: "magpie 记下的全部用量",
      calls: "调用",
      errors: "错误",
      errorShare: "占调用",
      tokens: "Token",
      cost: "费用",
      models: "按模型",
      model: "模型",
      today: "今天",
      "7d": "7 天",
      "30d": "30 天",
      sortTokens: "Token",
      sortCalls: "调用",
      sortCost: "费用",
      empty: "这个区间没有调用",
      failed: "读不到 magpie 的用量",
      retry: "一分钟后自动重试",
      since: "起",
      disclaimer:
        "这是 magpie 记下的全部用量。客户端都走 mbridge 时它才等于 mbridge 的用量；你直接用 magpie 的那部分也在里面。",
      status: "mbridge 自己的状态：",
      restart: "重启用",
      mbridge: "mbridge",
      mbridgeNone: "mbridge 还没有发布过任何东西",
      mbridgeAge: "发布于",
      mbridgeLive: "正在选档的会话",
      mbridgeNoLive: "现在没有会话在走 mbridge",
      mbridgeDecisions: "最近判档",
      mbridgeWindow: "最近",
      mbridgeTurns: "轮",
      mbridgeCoverage: "问了分类器",
      mbridgeFailures: "失败",
      mbridgeAsk: "要最新数字",
      mbridgeAsking: "正在要…",
      mbridgeRestart: "重启 mbridge",
      mbridgeRestarting: "正在重启…",
      mbridgeRestartAsked: "已发出重启指令",
      mbridgeRestartRefused: "被拒绝",
      tier: "档",
      advised: "建议",
      reason: "reason",
      conf: "置信度",
      unknown: "未知",
      running: "在跑",
      notRunning: "装了，没在跑",
      notInstalled: "没装",
      tools: "工具",
      noDecisions: "还没有判档",
    },
  };

  const PERIODS = ["today", "7d", "30d"];
  const SORTS = ["tokens", "calls", "cost"];
  const STATUS_URL = "http://127.0.0.1:3426/status";

  // text is the words for a language: zh for anything that starts with zh.
  function text(lang) {
    return String(lang || "").toLowerCase().startsWith("zh") ? TEXT.zh : TEXT.en;
  }

  // count is a number a person reads: 1,234 or 1.23M.
  function count(n) {
    n = Number(n) || 0;
    const a = Math.abs(n);
    if (a >= 1e9) return (n / 1e9).toFixed(2) + "B";
    if (a >= 1e6) return (n / 1e6).toFixed(2) + "M";
    if (a >= 1e3) return n.toLocaleString("en-US");
    return String(n);
  }

  // money is a price, never NaN, and never "$0.00" for something real.
  function money(n) {
    n = Number(n);
    if (!isFinite(n)) return "$0";
    if (n === 0) return "$0";
    if (n > 0 && n < 0.01) return "<$0.01";
    if (n < 10) return "$" + n.toFixed(4).replace(/0+$/, "").replace(/\.$/, "");
    if (n < 1000) return "$" + n.toFixed(2);
    return "$" + n.toFixed(0);
  }

  // share is a percentage, and never NaN.
  function pct(part, whole) {
    const p = Number(part) || 0;
    const w = Number(whole) || 0;
    if (w <= 0) return "0%";
    const v = (100 * p) / w;
    if (!isFinite(v)) return "0%";
    return (Math.round(v * 10) / 10).toString() + "%";
  }

  // tiles are the four numbers at the top: calls, errors, tokens, cost.
  function tiles(summary, lang) {
    const t = text(lang);
    const s = summary || {};
    return [
      { key: "calls", label: t.calls, value: count(s.calls) },
      {
        key: "errors",
        label: t.errors,
        value: count(s.errors),
        sub: t.errorShare + " " + pct(s.errors, s.calls),
      },
      { key: "tokens", label: t.tokens, value: count((s.input || 0) + (s.output || 0)) },
      { key: "cost", label: t.cost, value: money(s.cost) },
    ];
  }

  // rows are the model table, sorted by tokens, calls or cost, with each
  // model's share of the total.
  function rows(summary, sort, lang) {
    const s = summary || {};
    const models = Array.isArray(s.models) ? s.models : [];
    const key = sort === "calls" ? "calls" : sort === "cost" ? "cost" : "tokens";
    const value = (m) => (key === "tokens" ? (m.input || 0) + (m.output || 0) : m[key] || 0);
    const sorted = models.slice().sort((a, b) => value(b) - value(a));
    const total = sorted.reduce((a, m) => a + value(m), 0);
    return sorted.map((m) => ({
      model: m.model || "?",
      calls: count(m.calls),
      tokens: count((m.input || 0) + (m.output || 0)),
      cost: money(m.cost),
      share: pct(value(m), total),
      shareValue: total > 0 ? value(m) / total : 0,
    }));
  }

  // since is when the period started.
  function since(summary, lang) {
    const t = text(lang);
    const s = summary || {};
    return s.since ? t.since + " " + s.since : "";
  }

  // periodLabel is the name of a period.
  function periodLabel(period, lang) {
    return text(lang)[period] || period;
  }

  // sortLabel is the name of a sort.
  function sortLabel(sort, lang) {
    const t = text(lang);
    return t["sort" + sort.charAt(0).toUpperCase() + sort.slice(1)] || sort;
  }

  // mbridgeView is what the page shows of mbridge, from the snapshot
  // mbridge published into the plugin's options (SP14 §7).
  function mbridgeView(snapshot, lang) {
    const t = text(lang);
    if (!snapshot) {
      return { none: true, text: t.mbridgeNone };
    }
    const s = snapshot;
    const cov = s.window && s.window.coverage ? s.window.coverage : { asked: 0, total: 0 };
    const tiers = (s.window && s.window.tiers) || {};
    const reasons = (s.window && s.window.reasons) || {};
    const turnCount = Object.keys(tiers).reduce((a, k) => a + tiers[k], 0);
    const topReason = Object.keys(reasons).sort((a, b) => reasons[b] - reasons[a])[0] || "";
    const live = s.live || [];
    // the decisions this process made are in memory (a restart empties
    // them): the window's turns are the fallback
    const decisions = (s.decisions && s.decisions.length)
      ? s.decisions
      : ((s.window && s.window.turns) || []);
    const at = s.at ? new Date(s.at) : null;
    const age = at && !isNaN(at)
      ? Math.max(0, Math.round((Date.now() - at.getTime()) / 1000))
      : null;
    return {
      none: false,
      version: s.version || "",
      pid: s.pid || 0,
      uptime: s.uptime_s || 0,
      listen: s.listen || "",
      service: (s.service && s.service.running) ? t.running : ((s.service && s.service.installed) ? t.notRunning : t.notInstalled),
      magpie: s.magpie_ok ? (s.magpie_version || "") : "—",
      turnCount: turnCount,
      coverage: cov.total ? Math.round((100 * cov.asked) / cov.total) + "%" : "—",
      failures: (s.window && s.window.classify && s.window.classify.failures) || 0,
      topReason: topReason,
      age: age,
      ageText: age === null ? "" : (age < 60 ? age + "s" : (age < 3600 ? Math.round(age / 60) + "m" : Math.round(age / 3600) + "h")),
      live: live.map((c) => ({
        session: (c.session || "").slice(0, 8),
        harness: c.harness || t.unknown,
        agent: c.agent || "",
        tier: c.tier || "",
        group: c.group || "",
        reason: c.reason || "",
        conf: c.tier_confidence,
        turns: c.turns || 0,
        tools: c.agent === "gateway" ? (c.tools || 0) + "/" + (c.tool_failures || 0) : null,
        last: c.last_at || "",
      })),
      decisions: decisions.slice(0, 12).map((ev) => ({
        time: ev.t || "",
        clock: ev.t ? ev.t.slice(11, 19) : "",
        harness: ev.harness || t.unknown,
        agent: ev.agent || "",
        tier: ev.tier || "",
        advised: ev.classified_tier || "",
        differs: !!(ev.classified_tier && ev.classified_tier !== ev.tier),
        reason: ev.reason || "",
        source: ev.classifier || ev.source || "",
        conf: ev.tier_confidence,
        ms: ev.latency_ms || 0,
      })),
      emptyLive: live.length === 0,
      noDecisions: decisions.length === 0,
    };
  }

  globalThis.MbridgeView = {
    TEXT,
    PERIODS,
    SORTS,
    STATUS_URL,
    text,
    count,
    money,
    pct,
    tiles,
    rows,
    since,
    periodLabel,
    sortLabel,
    mbridgeView,
  };
})();
