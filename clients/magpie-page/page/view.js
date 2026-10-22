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
  };
})();
