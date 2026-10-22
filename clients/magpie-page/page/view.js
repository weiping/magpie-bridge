// view.js is mbridge's magpie page: the words and the arithmetic, with
// no DOM in it, so the page and the tests can both use it.
//
// The numbers come from magpie's own ledger (`magpie.read("usage:summary")`),
// never from mbridge: the page runs in a sandbox that cannot reach mbridge
// at all (SP14 §2). Nothing here knows about tiers — magpie records the
// model that answered, and mbridge's group is not in there.

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
    period: { today: "today", "7d": "7 days", "30d": "30 days" },
    sort: { tokens: "tokens", calls: "calls", cost: "cost" },
    empty: "no calls in this period",
    error: "magpie's usage could not be read",
    retry: "try again in a minute",
    since: "since",
    total: "total",
    disclaimer:
      "This is everything magpie recorded. It is mbridge's usage only when every client goes through mbridge; whatever you use magpie for directly is in here too.",
    status: "mbridge's own status",
    statusWhere: "mbridge's status page:",
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
    period: { today: "今天", "7d": "7 天", "30d": "30 天" },
    sort: { tokens: "Token", calls: "调用", cost: "费用" },
    empty: "这个区间没有调用",
    error: "读不到 magpie 的用量",
    retry: "一分钟后自动重试",
    since: "起",
    total: "合计",
    disclaimer:
      "这是 magpie 记下的全部用量。客户端都走 mbridge 时它才等于 mbridge 的用量；你直接用 magpie 的那部分也在里面。",
    status: "mbridge 自己的状态",
    statusWhere: "mbridge 的状态页：",
    restart: "重启用",
  },
};

/** count is a number a person reads: 1,234 or 1.23M. */
export function count(n) {
  const v = Number(n) || 0;
  if (Math.abs(v) >= 1e9) return (v / 1e9).toFixed(2) + "B";
  if (Math.abs(v) >= 1e6) return (v / 1e6).toFixed(2) + "M";
  if (Math.abs(v) >= 1e3) return v.toLocaleString("en-US");
  return String(v);
}

/** money is a price, never NaN, and never "$0.00" for something real. */
export function money(n) {
  const v = Number(n);
  if (!isFinite(v)) return "$0";
  if (v === 0) return "$0";
  if (v > 0 && v < 0.01) return "<$0.01";
  if (v < 10) return "$" + v.toFixed(4).replace(/0+$/, "").replace(/\.$/, "");
  if (v < 1000) return "$" + v.toFixed(2);
  return "$" + v.toFixed(0);
}

/** share is a percentage, and never NaN. */
export function pct(part, whole) {
  const p = Number(part) || 0;
  const w = Number(whole) || 0;
  if (w <= 0) return "0%";
  const v = (100 * p) / w;
  if (!isFinite(v)) return "0%";
  return (Math.round(v * 10) / 10).toString() + "%";
}

function lang(l) {
  return l === "zh" ? "zh" : "en";
}

/** tiles are the four numbers at the top: calls, errors, tokens, cost. */
export function tiles(summary, l = "en") {
  const t = TEXT[lang(l)];
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

/** rows are the model table, sorted by tokens, calls or cost, with each
 * model's share of the total. */
export function rows(summary, sort = "tokens", l = "en") {
  const t = TEXT[lang(l)];
  const s = summary || {};
  const models = Array.isArray(s.models) ? s.models : [];
  const key = sort === "calls" ? "calls" : sort === "cost" ? "cost" : "tokens";
  const value = (m) =>
    key === "tokens" ? (m.input || 0) + (m.output || 0) : m[key] || 0;
  const sorted = models.slice().sort((a, b) => value(b) - value(a));
  const total = sorted.reduce((a, m) => a + value(m), 0);
  const labels = t.sort;
  return {
    sort: key,
    label: t.models,
    sortLabel: labels[key] || key,
    total,
    items: sorted.map((m) => ({
      model: m.model || "?",
      calls: count(m.calls),
      tokens: count((m.input || 0) + (m.output || 0)),
      cost: money(m.cost),
      share: pct(value(m), total),
      shareValue: total > 0 ? value(m) / total : 0,
    })),
  };
}

/** period labels, in the order magpie offers them. */
export function periods(l = "en") {
  const t = TEXT[lang(l)];
  return ["today", "7d", "30d"].map((k) => ({ key: k, label: t.period[k] }));
}

/** sorts, in the order the page offers them. */
export function sorts(l = "en") {
  const t = TEXT[lang(l)];
  return ["tokens", "calls", "cost"].map((k) => ({ key: k, label: t.sort[k] }));
}

/** header is what the page says above the numbers. */
export function header(summary, l = "en") {
  const t = TEXT[lang(l)];
  const s = summary || {};
  return {
    title: t.title,
    subtitle: t.subtitle,
    since: s.since ? t.since + " " + s.since : "",
    empty: !s.calls,
    emptyText: t.empty,
    error: t.error,
    retry: t.retry,
    disclaimer: t.disclaimer,
    status: t.status,
    statusWhere: t.statusWhere,
    restart: t.restart,
  };
}

globalThis.MbridgeView = {
  TEXT,
  count,
  money,
  pct,
  tiles,
  rows,
  periods,
  sorts,
  header,
};