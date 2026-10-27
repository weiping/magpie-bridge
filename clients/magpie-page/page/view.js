// view.js provides the complete status UI renderer for the magpie plugin page.
// It shares the exact visual design and data representation with the mbridge /status page.

(() => {
"use strict";

// Capture-phase error shield: prevents WebKit sandboxed "Script error." / ResizeObserver
// loop notifications or transient data errors during tier switches from killing the page.
if (typeof window !== "undefined" && typeof window.addEventListener === "function") {
  window.addEventListener("error", (e) => {
    e.stopImmediatePropagation();
    e.preventDefault();
  }, true);
  window.addEventListener("unhandledrejection", (e) => {
    e.stopImmediatePropagation();
    e.preventDefault();
  }, true);
}

const REFRESH_MS = 5000;

// PAGE_VERSION is this page's own build: the sidebar keeps the iframe it
// built the first time, so a stale tab shows an old version here (SP14 §2:
// the plugin page is a one-way ticket — toggle the plugin off and on to
// get a new frame).
const PAGE_VERSION = "0.1.6";

const TEXT = {
  en: {
    title: "mbridge",
    subtitle: "per-turn model routing, on this machine",
    window: "window",
    unknown: "unknown",
    listenPending: "the listen in router.json takes effect only after a restart",
    turns: "turns",
    tools: "tools",
    coverage: "asked the classifier",
    failures: "failures",
    cost: "cost",
    topReason: "most common reason",
    process: "process",
    pid: "pid", uptime: "uptime", listen: "listen", magpie: "magpie",
    service: "service", running: "running", notRunning: "installed, not running",
    notInstalled: "not installed",
    live: "sessions running now",
    noLive: "no session is going through mbridge right now",
    justWent: "the last turns that went through it",
    decisions: "recent decisions",
    noDecisions: "no decision yet",
    classifier: "classifier",
    distributions: "distributions",
    arms: "two arms, and the calibration",
    config: "configuration in force",
    configNote: "a project-level .mbridge/router.json may override each tier's criteria",
    since: "since",
    tier: "tier", advised: "advised", reason: "reason", harness: "harness",
    agent: "agent", source: "source", conf: "confidence", ms: "ms",
    tierConf: "tier confidence", dissatisfied: "dissatisfied", compact: "compact",
    classifyMs: "classifier", latency: "decision (includes classification)",
    notAsked: "not asked", notMeasured: "—", failed: "failed",
    askHint: "not every turn asks it: pinned agents, plan mode and compaction never do, so a coverage below 100% is normal.",
    failureFix: { timeout: "raise classify_timeout_ms",
                  canceled: "the client gave up first: lower the timeout or use a faster classifier",
                  magpie: "magpie's own words; no fix is suggested" },
    sampleShort: "too few samples",
    feedbackOnly: "from feedback only, not gh",
    unpriced: "requests magpie has no price for",
    cacheWrite: "cache write (estimated)",
    merged: "merged PR", manual: "manual switch", sessions: "sessions",
    median: "median", p90: "p90",
    restart: "restart mbridge",
    restarting: "restarting…",
    restartAsked: "restart asked for — the pid changes when it comes back",
    restartRefused: "refused (not same-origin?)",
    foot: [
      "the harness is whatever the client says it is; mbridge does not check it, and an unknown one is shown as it came",
      "the session list is what mbridge still remembers: it is memory, and a restart empties it",
      "a project-level .mbridge/router.json may override each tier's criteria; which one a session used is not recorded",
      "the merged-PR rate here comes from the sessions' own feedback, not from gh: mbridge router report runs gh",
      "the tier on a card is where that turn went, not where it should have gone; the classifier's advice is shown next to it",
    ],
  },
  zh: {
    title: "鹊桥",
    subtitle: "本机的逐轮选档",
    window: "区间",
    unknown: "未知",
    listenPending: "router.json 里的 listen 要重启才生效",
    turns: "轮",
    tools: "工具",
    coverage: "问了分类器",
    failures: "失败",
    cost: "成本",
    topReason: "最常见的 reason",
    process: "进程",
    pid: "pid", uptime: "运行时长", listen: "listen", magpie: "magpie",
    service: "service", running: "在跑", notRunning: "装了，没在跑",
    notInstalled: "没装",
    live: "正在选档的会话",
    noLive: "现在没有会话在走 mbridge",
    justWent: "刚刚走过的几轮",
    decisions: "最近判档",
    noDecisions: "还没有判档",
    classifier: "分类器",
    distributions: "分布",
    arms: "两臂与校准",
    config: "生效中的配置",
    configNote: "项目级 .mbridge/router.json 可以覆盖每档的 criteria",
    since: "起",
    tier: "档", advised: "建议", reason: "reason", harness: "harness",
    agent: "agent", source: "来源", conf: "置信度", ms: "ms",
    tierConf: "档位置信度", dissatisfied: "不满意", compact: "压缩",
    classifyMs: "分类耗时", latency: "判档（含分类）",
    notAsked: "没问", notMeasured: "—", failed: "失败",
    askHint: "不是每一轮都问：被钉死的 agent、计划模式和压缩轮次都不问，所以覆盖率低于 100% 是正常的。",
    failureFix: { timeout: "调大 classify_timeout_ms",
                  canceled: "客户端先放弃了：调小超时，或换更快的分类器",
                  magpie: "这是 magpie 自己报的原文，不给建议" },
    sampleShort: "样本不足",
    feedbackOnly: "只来自会话自报的 feedback，不含 gh",
    unpriced: "magpie 没有价的请求",
    cacheWrite: "cache write（估算）",
    merged: "PR 合并", manual: "手动切模型", sessions: "会话数",
    median: "中位数", p90: "p90",
    restart: "重启 mbridge",
    restarting: "正在重启…",
    restartAsked: "已发出重启指令 —— pid 变了就是回来了",
    restartRefused: "被拒绝（不是同源？）",
    foot: [
      "harness 是客户端自报的，mbridge 不校验；没见过的值照原样显示",
      "会话列表只到 mbridge 记得住的为止：它在内存里，重启即空",
      "项目级 .mbridge/router.json 可以覆盖每档的 criteria；某个会话当时用的是哪一份，没有记录",
      "这里的 PR 合并率来自会话自报的 feedback，不是 gh：mbridge router report 才跑 gh",
      "卡片上的档是「这一轮送去了哪」，不是「该用哪」；分类器的建议就显示在旁边",
    ],
  },
};

const REASONS = {
  "R1-fixed": ["that agent is pinned by fixed_agents", "该 agent 被 fixed_agents 钉死"],
  "R2-plan-mode": ["plan mode: performance directly", "计划模式，直接 performance"],
  "R9-compact": ["a compaction or summary turn", "压缩 / 摘要轮次"],
  "R3-escalate": ["the last turn was judged unsatisfying: one tier up", "上一轮被判不满意 → 升一档"],
  "R3-tools": ["3+ tool calls, half of them failing: one tier up", "上一轮至少 3 次工具调用、一半以上失败 → 升一档"],
  "R3-review": ["the review said it was not resolved: one tier up", "复核说上一轮没解决 → 升一档"],
  "R4-escalation-hold": ["holding the escalated tier", "升档后保持"],
  "R6-adopt": ["took the classifier's answer", "采用分类结果"],
  "R6-lower": ["lower tier (cache expired, or lowered twice)", "降档（缓存过期，或连降两次）"],
  "R6-keep": ["not lowered", "不降档"],
  "R7-carry": ["classification failed or was unsure: kept the last tier", "分类失败或置信度不够，沿用上一档"],
  "R8-default": ["no last turn: the default tier", "没有上一轮，用 default_tier"],
  "hint": ["a tier a /turn stored", "用了 /turn 存下的判档"],
  "R-held": ["kept the tier the turn began with", "本轮内沿用开轮时那一档"],
  "codex-title": ["Codex naming the thread, not a turn", "Codex 给线程起标题，不是真的一轮"],
};

const TIERS = ["fast", "balanced", "performance"];

let lang = (navigator.language || "en").toLowerCase().startsWith("zh") ? "zh" : "en";
const T = () => TEXT[lang];

function el(tag, attrs, kids) {
  const n = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === undefined || v === null || v === false) continue;
    if (k === "text") n.textContent = v;
    else if (k === "class") n.className = v;
    else n.setAttribute(k, v);
  }
  for (const kid of [].concat(kids || [])) {
    if (kid === undefined || kid === null || kid === false) continue;
    n.appendChild(typeof kid === "object" ? kid : document.createTextNode(String(kid)));
  }
  return n;
}

function kv(rows) {
  const box = el("div", { class: "kv" });
  for (const [k, v] of rows) {
    if (v === undefined || v === null) continue;
    box.appendChild(el("div", { text: k }));
    box.appendChild(typeof v === "object" ? el("div", {}, v) : el("div", { text: String(v) }));
  }
  return box;
}

function pct(part, whole) {
  if (!whole) return "0%";
  const v = (100 * part) / whole;
  return (Math.round(v * 10) / 10).toString() + "%";
}

function money(v) {
  v = Number(v) || 0;
  if (v === 0) return "$0";
  if (v > 0 && v < 0.01) return "<$0.01";
  if (v < 10) return "$" + v.toFixed(4).replace(/0+$/, "").replace(/\.$/, "");
  if (v < 1000) return "$" + v.toFixed(2);
  return "$" + v.toFixed(0);
}

function ago(iso) {
  if (!iso) return "";
  const t = new Date(iso);
  if (isNaN(t)) return "";
  const s = Math.max(0, Math.round((Date.now() - t) / 1000));
  if (s < 60) return s + "s";
  if (s < 3600) return Math.round(s / 60) + "m";
  if (s < 86400) return Math.round(s / 3600) + "h";
  return Math.round(s / 86400) + "d";
}

function clock(iso) {
  if (!iso) return "";
  const t = new Date(iso);
  if (isNaN(t)) return "";
  return t.toLocaleTimeString(lang === "zh" ? "zh-CN" : "en-GB", { hour12: false });
}

function reasonText(r) {
  const both = REASONS[r];
  if (!both) return r || "";
  return lang === "zh" ? both[1] : both[0];
}

function tierChip(tier) {
  return el("span", { class: "chip tier-" + tier, text: tier });
}

function hasNum(v) {
  return v !== undefined && v !== null && v !== "" && Number.isFinite(Number(v));
}

function numFixed(v, d) {
  const n = Number(v);
  return Number.isFinite(n) ? n.toFixed(d === undefined ? 2 : d) : T().notMeasured;
}

function safeArr(a) {
  return Array.isArray(a) ? a.filter((x) => x !== null && x !== undefined) : [];
}

function safeObj(o) {
  return o && typeof o === "object" && !Array.isArray(o) ? o : {};
}

/* The turns to show: the window's last ones, plus the ones this process
   decided since it started, newest first. Without the window's turns a
   fresh process would show an empty page. */
function turnsOf(data) {
  if (!data || typeof data !== "object") return [];
  const out = [];
  const seen = new Set();
  const push = (ev) => {
    if (!ev || typeof ev !== "object" || (ev.kind !== "decide" && ev.kind !== "shadow")) return;
    const id = ev.turn_id || (String(ev.t || "") + "|" + String(ev.session || "") + "|" + String(ev.reason || ""));
    if (seen.has(id)) return;
    seen.add(id);
    out.push(ev);
  };
  for (const ev of safeArr(data.window && data.window.turns)) push(ev);
  for (const ev of safeArr(data.decisions)) push(ev);
  out.sort((a, b) => String(b.t || "").localeCompare(String(a.t || "")));
  return out;
}

function scoreCell(value, asked) {
  if (!asked) return el("div", { class: "off", text: T().notAsked });
  if (!hasNum(value)) return el("div", { text: T().notMeasured });
  const num = Number(value);
  return el("div", {}, [bar(num), el("div", { class: "muted", text: numFixed(num, 2) })]);
}

function bar(v) {
  const num = Number(v);
  const w = Number.isFinite(num) ? Math.max(0, Math.min(1, num)) * 100 : 0;
  const b = el("div", { class: "bar" });
  b.appendChild(el("i", { style: "width:" + w + "%" }));
  return b;
}

function liveCard(c) {
  c = safeObj(c);
  const bits = [];
  if (c.harness) bits.push(String(c.harness));
  if (c.agent) bits.push(String(c.agent));
  const row2 = el("div", { class: "row2", text: String(c.session || "").slice(0, 8) + " · " + (bits.join(" · ") || T().unknown) });
  const row3 = [];
  if (c.reason) row3.push(reasonText(c.reason));
  if (hasNum(c.tier_confidence)) {
    row3.push("conf " + numFixed(c.tier_confidence, 2));
  }
  row3.push(ago(c.last_at));
  const row4 = [];
  if (c.turns) row4.push(c.turns + " " + T().turns);
  row4.push(T().tools + " " + (c.agent === "gateway" ? (c.tools || 0) + "/" + (c.tool_failures || 0) : "—"));
  if (c.escalated_left) row4.push("↑" + c.escalated_left);
  if (c.lower_streak) row4.push("↓" + c.lower_streak);
  if (c.pinned) row4.push("📌");
  const tier = String(c.tier || "?");
  const row1Tier = [tierChip(tier)];
  if (c.classified_tier && c.classified_tier !== c.tier) {
    row1Tier.push(" ", el("span", { class: "chip differs", title: T().advised, text: c.classified_tier }));
  }
  return el("div", { class: "card" }, [
    el("div", { class: "row1" }, [
      el("span", { text: (c.harness || T().unknown) + " · " + (c.agent || "") }),
      el("span", {}, row1Tier),
    ]),
    row2,
    el("div", { class: "row3 muted", text: row3.join(" · ") }),
    el("div", { class: "row3 muted", text: row4.join(" · ") }),
  ]);
}

function decisionTable(turns) {
  const t = el("table", { class: "wide" });
  t.appendChild(el("thead", {}, el("tr", {}, [
    el("th", { text: "" }), el("th", { text: T().harness }), el("th", { text: T().agent }),
    el("th", { text: T().tier }), el("th", { text: T().advised }), el("th", { text: T().reason }),
    el("th", { text: T().source }), el("th", { class: "num", text: T().conf }),
    el("th", { class: "num", text: T().latency }),
  ])));
  const body = el("tbody");
  for (const ev of safeArr(turns).slice(0, 20)) {
    const differs = ev.classified_tier && ev.classified_tier !== ev.tier;
    body.appendChild(el("tr", {}, [
      el("td", { class: "muted", text: clock(ev.t) }),
      el("td", { text: ev.harness || T().unknown }),
      el("td", { text: ev.agent || "" }),
      el("td", {}, tierChip(ev.tier || "?")),
      el("td", { class: differs ? "chip differs" : "muted",
                 text: differs ? ev.classified_tier : (ev.classified_tier || T().notMeasured) }),
      el("td", { class: "reason", title: reasonText(ev.reason), text: ev.reason || "" }),
      el("td", { class: "muted", text: ev.classifier || ev.source || "" }),
      el("td", { class: "num", text: hasNum(ev.tier_confidence) ? numFixed(ev.tier_confidence, 2) : T().notMeasured }),
      el("td", { class: "num", text: ev.latency_ms ? ev.latency_ms + "ms" : T().notMeasured }),
    ]));
  }
  t.appendChild(body);
  return t;
}

function distribution(title, counts, total) {
  const map = safeObj(counts);
  const keys = Object.keys(map).sort((a, b) => (Number(map[b]) || 0) - (Number(map[a]) || 0));
  const box = el("div", { class: "dist" });
  box.appendChild(el("h3", { text: title }));
  if (!keys.length) {
    box.appendChild(el("div", { class: "muted", text: "—" }));
    return box;
  }
  for (const k of keys) {
    const c = Number(map[k]) || 0;
    box.appendChild(el("div", { class: "distrow" }, [
      el("span", { class: "distname", text: k }),
      el("span", { class: "distbar" }, bar(c / (total || 1))),
      el("span", { class: "num distnum", text: c + " · " + pct(c, total) }),
    ]));
  }
  return box;
}

function calibrationTable(c) {
  c = safeObj(c);
  const bands = safeArr(c.bands);
  const t = el("table");
  t.appendChild(el("thead", {}, el("tr", {}, [
    el("th", { text: "score" }), el("th", { class: "num", text: "n" }),
    el("th", { class: "num", text: "share" }), el("th", { class: "num", text: "under-tier" }),
  ])));
  const body = el("tbody");
  const total = bands.reduce((a, b) => a + (Number(b.n) || 0), 0);
  for (const b of bands) {
    const n = Number(b.n) || 0;
    body.appendChild(el("tr", {}, [
      el("td", { text: "[" + b.lo + ", " + b.hi + ")" }),
      el("td", { class: "num", text: String(n) }),
      el("td", { class: "num", text: pct(n, total) }),
      el("td", { class: "num", text: n ? pct((Number(b.under_rate) || 0) * 100, 100) : T().notMeasured }),
    ]));
  }
  t.appendChild(body);
  const foot = [c.current !== undefined ? "current " + c.current : null,
    c.suggested !== undefined && c.suggested !== null ? "suggested " + c.suggested : null,
    c.sample_short ? T().sampleShort : null].filter(Boolean).join(" · ");
  return el("div", { class: "cal" }, [t, el("div", { class: "muted", text: foot })]);
}

  function detectOS() {
    const nav = typeof navigator !== "undefined" ? navigator : {};
    const ua = (nav.userAgent || "").toLowerCase();
    const plat = (nav.platform || "").toLowerCase();
    if (plat.includes("win") || ua.includes("windows")) return "windows";
    if (plat.includes("linux") || ua.includes("linux") || ua.includes("x11")) return "linux";
    return "darwin"; // default mac
  }

  function renderGuide(osName, lang) {
    const isZh = String(lang || "").toLowerCase().startsWith("zh");
    const os = osName || detectOS();
    const root = document.getElementById("root");
    root.textContent = "";

    const box = el("div", { class: "setup-guide" });

    // Header
    const head = el("header", { class: "head" }, [
      el("h1", {}, [el("span", { text: isZh ? "鹊桥" : "mbridge" })]),
      el("p", { class: "sub", text: isZh ? "未检测到运行中的 mbridge 实例。请按以下步骤安装、配置并启动：" : "mbridge instance is not running or not installed. Follow the steps below to setup:" }),
    ]);
    box.appendChild(head);

    // Guide card 1: Service Install & Start
    const card1 = el("div", { class: "guide-card" });
    const osTitles = { darwin: "macOS", linux: "Linux", windows: "Windows" };
    card1.appendChild(el("h2", { text: (isZh ? "步骤一：安装与启动 · " : "Step 1: Install & Start · ") + (osTitles[os] || "macOS") }));
    card1.appendChild(el("p", { class: "guide-desc" }));
    const descBox = card1.querySelector(".guide-desc:last-of-type") || card1.querySelector("p:last-of-type");
    card1.appendChild(el("div", { class: "guide-desc", text: isZh
      ? "鹊桥 是独立于 magpie 的智能模型选档守护进程，服务跑在 127.0.0.1:3426。每轮自动按难度选档（fast / balanced / performance），支持分类器置信度、升档降档、A/B 实验和校准。"
      : "Magpie Bridge is a per-turn model router daemon on 127.0.0.1:3426. It routes each turn by difficulty (fast / balanced / performance), with classifier confidence, escalation, A/B experiments and calibration." }));
    const features = isZh ? [
      "逐轮自动选档：按任务难度在 fast / balanced / performance 三档之间切换",
      "TypeSafe Jev 分类器：用模型给每轮任务打置信度，低置信度自动升档",
      "升档降档与迟滞：上一轮不满意自动升档，缓存过期自动降档",
      "校准与 A/B 实验：阈值建议、两臂对比、成本统计",
      "状态页 http://127.0.0.1:3426/status：进程、会话、判档、分类器、分布、两臂与校准",
    ] : [
      "Per-turn routing: fast / balanced / performance by task difficulty",
      "TypeSafe Jev classifier: model-scored confidence per turn, auto-escalate on low confidence",
      "Escalation with hysteresis: unsatisfying turns escalate, cache expiry lowers",
      "Calibration & A/B experiments: threshold suggestions, two-arm comparison, cost stats",
      "Status page http://127.0.0.1:3426/status: process, sessions, decisions, classifier, distributions, two arms & calibration",
    ];
    const ul = el("ul", { class: "fixes" });
    for (const line of features) ul.appendChild(el("li", { text: line }));
    card1.appendChild(ul);

    const steps1 = el("div", { class: "guide-steps" });

    // 1. Install
    const s1 = el("div", { class: "guide-step" });
    s1.appendChild(el("div", { class: "step-title", text: isZh ? "1. 安装 mbridge 并注册自启服务" : "1. Install mbridge & register login service" }));
    const cmd1 = os === "windows"
      ? "irm https://raw.githubusercontent.com/weiping/mbridge/main/install.ps1 | iex"
      : "curl -fsSL https://raw.githubusercontent.com/weiping/mbridge/main/install.sh | sh";
    s1.appendChild(el("div", { class: "step-cmd" }, [el("pre", { text: cmd1 })]));
    steps1.appendChild(s1);

    // 2. Init groups
    const s2 = el("div", { class: "guide-step" });
    s2.appendChild(el("div", { class: "step-title", text: isZh ? "2. 在 magpie 中初始化路由分组与配置" : "2. Initialize routing groups & config in magpie" }));
    const cmd2 = "mbridge router init --preset cn --groups-only";
    s2.appendChild(el("div", { class: "step-cmd" }, [el("pre", { text: cmd2 })]));
    s2.appendChild(el("div", { class: "guide-tip", text: isZh ? "（也可选择 --preset frontier 或 anthropic 预设）" : "(or choose --preset frontier / anthropic)" }));
    steps1.appendChild(s2);

    // 3. Start / Verify
    const s3 = el("div", { class: "guide-step" });
    s3.appendChild(el("div", { class: "step-title", text: isZh ? "3. 启动与验证" : "3. Start & Verify" }));
    const cmd3 = "# 检查服务运行状态\nmbridge status\n\n# 或手动在前台启动\nmbridge serve";
    s3.appendChild(el("div", { class: "step-cmd" }, [el("pre", { text: cmd3 })]));
    steps1.appendChild(s3);

    card1.appendChild(steps1);
    box.appendChild(card1);

    // Guide card 2: Agent Plugins (Claude Code, Codex, Pi)
    const card2 = el("div", { class: "guide-card" });
    card2.appendChild(el("h2", { text: isZh ? "步骤二：安装 Agent 插件与使用" : "Step 2: Install Agent Plugins & Usage" }));
    card2.appendChild(el("p", { class: "guide-desc", text: isZh ? "主流编码 Agent 均有一键插件支持，按需安装对应客户端：" : "Supported coding agents have dedicated plugins. Install as needed:" }));

    const steps2 = el("div", { class: "guide-steps" });

    // Claude Code
    const pClaude = el("div", { class: "guide-step" });
    pClaude.appendChild(el("div", { class: "step-title", text: "Claude Code（≥ v2.1.287）" }));
    const cmdClaude = `# 1. 安装插件\nclaude plugin marketplace add weiping/mbridge\nclaude plugin install mbridge@mbridge\n\n# 2. 项目配置：在项目的 .claude/settings.json 中指定模型\n{\n  "model": "group/mbridge[1m]"\n}`;
    pClaude.appendChild(el("div", { class: "step-cmd" }, [el("pre", { text: cmdClaude })]));
    steps2.appendChild(pClaude);

    // Codex
    const pCodex = el("div", { class: "guide-step" });
    pCodex.appendChild(el("div", { class: "step-title", text: "Codex" }));
    const cmdCodex = `# 1. 安装插件（使用 --sparse 避免拉取庞大历史）\ncodex plugin marketplace add weiping/mbridge --sparse .agents --sparse clients/codex\ncodex plugin add mbridge@mbridge\n\n# 2. 启动使用：通过 mbridge 配置文件启动\ncodex -p mbridge\n# 或在项目的 .codex/config.toml 中指定\nmodel = "group/mbridge"`;
    pCodex.appendChild(el("div", { class: "step-cmd" }, [el("pre", { text: cmdCodex })]));
    steps2.appendChild(pCodex);

    // Pi
    const pPi = el("div", { class: "guide-step" });
    pPi.appendChild(el("div", { class: "step-title", text: "Pi" }));
    const cmdPi = `# 1. 安装扩展\npi install npm:@weiping/pi-mbridge\n\n# 2. 项目配置：在项目的 .pi/settings.json 中指定\n{\n  "defaultProvider": "magpie",\n  "defaultModel": "group/mbridge"\n}`;
    pPi.appendChild(el("div", { class: "step-cmd" }, [el("pre", { text: cmdPi })]));
    steps2.appendChild(pPi);

    card2.appendChild(steps2);
    box.appendChild(card2);

    // Actions & Live polling indicator
    const actions = el("div", { style: "margin: 1rem 0; display: flex; align-items: center; gap: 1rem;" });
    const refBtn = el("button", { type: "button", class: "guide-refresh", text: isZh ? "🔄 检查服务状态" : "🔄 Check Status" });
    refBtn.onclick = () => {
      refBtn.textContent = isZh ? "正在刷新…" : "Refreshing…";
      if (globalThis.pollMbridge) globalThis.pollMbridge();
    };
    actions.appendChild(refBtn);

    const tipLink = el("span", { class: "guide-tip", text: isZh ? "mbridge 服务启动后，本页面将自动实时切换为数据监控大盘。" : "Once mbridge starts, this page will automatically display the dashboard." });
    actions.appendChild(tipLink);

    box.appendChild(actions);
    root.appendChild(box);
  }

  function render(data) {
  try {
  data = safeObj(data);
  const root = document.getElementById("root");
  if (!root) return;
  root.textContent = "";
  const t = T();
  const w = safeObj(data.window);
  const turns = turnsOf(data);
  const turnsCount = Object.values(safeObj(w.tiers)).reduce((a, b) => a + (Number(b) || 0), 0);
  const cov = safeObj(w.coverage);
  const reasons = safeObj(w.reasons);
  const topReason = Object.keys(reasons).sort((a, b) => reasons[b] - reasons[a])[0] || "";
  const cost = (w.report && w.report.router && w.report.router.total_cost) || 0;

  // the headline: mbridge's own numbers, not the process table
  root.appendChild(el("header", { class: "head" }, [
    el("h1", {}, [el("span", { text: "mbridge" }),
      el("span", { class: "version", text: " " + (data.version || "") }),
      el("span", { class: "pagever", text: "page " + PAGE_VERSION })]),
    el("p", { class: "sub", text: t.subtitle }),
  ]));

  root.appendChild(el("div", { class: "pulse" }, [
    el("div", { class: "stat" }, [el("b", { text: String(turnsCount) }), el("span", { text: " " + t.turns })]),
    el("div", { class: "stat" }, [el("b", { text: pct(cov.asked, cov.total) }),
      el("span", { text: " " + t.coverage })]),
    el("div", { class: "stat" }, [el("b", { text: String((w.classify || {}).failures || 0) }),
      el("span", { text: " " + t.failures })]),
    el("div", { class: "stat" }, [el("b", { text: money(cost) }), el("span", { text: " " + t.cost })]),
    topReason ? el("div", { class: "stat" }, [el("b", { text: topReason }),
      el("span", { text: " " + t.topReason })]) : null,
  ]));

  if (data.listen_pending) {
    root.appendChild(el("div", { class: "banner", text: t.listenPending || "the listen in router.json takes effect only after a restart" }));
  }
  if (data.config_error) {
    root.appendChild(el("div", { class: "banner bad", text: "router.json: " + data.config_error }));
  }

  // the process, one line
  const svc = data.service || {};
  root.appendChild(el("div", { class: "procline" }, [
    el("span", { text: "pid " + (data.pid || "") }),
    el("span", { text: "↑ " + Math.round((data.uptime_s || 0) / 60) + "m" }),
    el("span", { text: data.listen || "" }),
    el("span", { text: (data.magpie_ok ? "magpie " + (data.magpie_version || "") : "magpie —") }),
    el("span", { text: "service " + (svc.installed ? (svc.running ? t.running : t.notRunning) : t.notInstalled) }),
  ]));

  // the sessions that are running now
  root.appendChild(el("h2", { text: t.live }));
  const live = safeArr(data.live);
  if (live.length) {
    const cards = el("div", { class: "cards" });
    for (const c of live) cards.appendChild(liveCard(c));
    root.appendChild(cards);
    const extra = (data.live_total || 0) - live.length;
    if (extra > 0) root.appendChild(el("div", { class: "muted", text: "+" + extra }));
  } else {
    root.appendChild(el("div", { class: "muted", text: t.noLive }));
    // nothing is running: show the turns that just went through, so the
    // section is not empty
    if (turns.length) {
      root.appendChild(el("h3", { class: "muted", text: t.justWent }));
      const cards = el("div", { class: "cards" });
      for (const ev of turns.slice(0, 4)) {
        const differs = ev.classified_tier && ev.classified_tier !== ev.tier;
        cards.appendChild(el("div", { class: "card" }, [
          el("div", { class: "row1" }, [
            el("span", { text: (ev.harness || t.unknown) + " · " + (ev.agent || "") }),
            el("span", {}, [
              tierChip(ev.tier || "?"),
              ...(differs ? [" ", el("span", { class: "chip differs", title: t.advised, text: ev.classified_tier })] : []),
            ]),
          ]),
          el("div", { class: "row2", text: clock(ev.t) + " · " + String(ev.session || "").slice(0, 8) }),
          el("div", { class: "row3 muted", text: reasonText(ev.reason) }),
        ]));
      }
      root.appendChild(cards);
    }
  }

  // the decisions
  root.appendChild(el("h2", { text: t.decisions }));
  if (!turns.length) {
    root.appendChild(el("div", { class: "muted", text: t.noDecisions }));
  } else {
    root.appendChild(decisionTable(turns));
  }

  // the classifier
  root.appendChild(el("h2", { text: t.classifier }));
  const cl = w.classify || { asked: 0, failures: 0, error_kinds: {} };
  root.appendChild(kv([
    [t.coverage, pct(cov.asked, cov.total) + "  (" + cov.asked + "/" + cov.total + ")"],
    [t.failures, cl.failures + " / " + cl.asked],
    ["", el("span", { class: "muted", text: t.askHint })],
  ]));
  if (cl.failures) {
    const ul = el("ul", { class: "fixes" });
    for (const [kind, n] of Object.entries(safeObj(cl.error_kinds))) {
      ul.appendChild(el("li", { text: kind + " × " + n + " — " + ((t.failureFix || {})[kind] || "") }));
    }
    root.appendChild(ul);
  }
  if (turns.length) {
    const gt = el("table", { class: "wide" });
    gt.appendChild(el("thead", {}, el("tr", {}, [
      el("th", { text: "" }), el("th", { text: t.harness }),
      el("th", { text: t.tierConf }), el("th", { text: t.dissatisfied }), el("th", { text: t.compact }),
      el("th", { class: "num", text: t.classifyMs }), el("th", { class: "num", text: t.latency }),
    ])));
    const body = el("tbody");
    for (const ev of turns.slice(0, 10)) {
      const asked = !!ev.classifier || !!ev.classify_error;
      body.appendChild(el("tr", { class: asked ? "" : "off" }, [
        el("td", { class: "muted", text: clock(ev.t) }),
        el("td", { text: ev.harness || t.unknown }),
        el("td", {}, scoreCell(ev.tier_confidence, asked)),
        el("td", {}, scoreCell(ev.dissatisfied, asked)),
        el("td", {}, scoreCell(ev.compact, asked)),
        el("td", { class: "num", text: asked && ev.classify_ms ? ev.classify_ms + "ms" :
          (ev.classify_error ? t.failed : t.notMeasured) }),
        el("td", { class: "num", text: ev.latency_ms ? ev.latency_ms + "ms" : t.notMeasured }),
      ]));
    }
    gt.appendChild(body);
    root.appendChild(gt);
  }

  // the distributions
  root.appendChild(el("h2", { text: t.distributions }));
  const dists = el("div", { class: "dists" });
  dists.appendChild(distribution("tier", w.tiers, turnsCount));
  dists.appendChild(distribution("reason", w.reasons, turnsCount));
  dists.appendChild(distribution("harness", w.harnesses, turnsCount));
  dists.appendChild(distribution("arm", w.arms, Object.values(safeObj(w.arms)).reduce((a, b) => a + (Number(b) || 0), 0)));
  root.appendChild(dists);

  // the two arms
  root.appendChild(el("h2", { text: t.arms }));
  const rep = w.report || {};
  const rows = [];
  for (const arm of ["router", "control"]) {
    const a = rep[arm];
    if (!a || !a.sessions) continue; // an arm with nothing in it is not drawn
    rows.push([arm, String(a.sessions), money(a.cost_median || 0), money(a.cost_p90 || 0),
      String(a.merged_sessions || 0), String(a.manual_switch_sessions || 0)]);
  }
  if (rows.length) {
    const at = el("table");
    at.appendChild(el("thead", {}, el("tr", {}, [
      el("th", { text: "arm" }), el("th", { class: "num", text: t.sessions }),
      el("th", { class: "num", text: t.median }), el("th", { class: "num", text: t.p90 }),
      el("th", { class: "num", text: t.merged }), el("th", { class: "num", text: t.manual }),
    ])));
    const body = el("tbody");
    for (const r of rows) {
      body.appendChild(el("tr", {}, r.map((cell, i) => el("td", { class: i ? "num" : "", text: cell }))));
    }
    at.appendChild(body);
    root.appendChild(at);
  } else {
    root.appendChild(el("div", { class: "muted", text: "—" }));
  }
  const notes = [t.feedbackOnly,
    t.unpriced + ": " + ((rep.router || {}).unpriced_requests || 0),
    t.cacheWrite + ": " + money((rep.router || {}).cache_write_cost || 0)];
  if (rep.sample_short) notes.push(t.sampleShort);
  root.appendChild(el("div", { class: "muted", text: notes.join(" · ") }));
  for (const c of safeArr(w.calibrate)) root.appendChild(calibrationTable(c));

  // the configuration, last: it is reference, not the story
  const cfg = data.config || {};
  const th = cfg.thresholds || {};
  const details = el("details", { class: "cfg" });
  details.appendChild(el("summary", { text: t.config }));
  details.appendChild(kv([
    ["router_group", cfg.router_group],
    ["default_tier", cfg.default_tier],
    ["classifier", cfg.classifier],
    ["classify_timeout_ms", String(cfg.classify_timeout_ms || "")],
    ["escalate_turns", String(cfg.escalate_turns || "")],
    ["cache_ttl_seconds", String(cfg.cache_ttl_seconds || "")],
    ["thresholds", "tier_min " + th.tier_min + " · dissatisfied " + th.dissatisfied_min +
      " · review " + th.review_min + " · compact " + th.compact_min],
    ["review", (cfg.review || {}).mode + " · " + (cfg.review || {}).timeout_ms + "ms"],
    ["experiment", ((cfg.experiment || {}).enabled ? "on " : "off ") +
      (cfg.experiment || {}).router_percent + "% → " + (cfg.experiment || {}).control_tier],
    ["fixed_agents", Object.entries(safeObj(cfg.fixed_agents)).map(([a, tier]) => a + " → " + tier).join(", ")],
    ["tiers", TIERS.map((name) => {
      const tc = (cfg.tiers || {})[name] || {};
      return name + " → " + (tc.group || "?") + " (" + (tc.claude_alias || "?") + ")";
    }).join(" · ")],
    ["", el("span", { class: "muted", text: t.configNote })],
  ]));
  root.appendChild(details);

  // the restart button
  const btn = el("button", { type: "button", class: "restart", text: t.restart });
  btn.addEventListener("click", async () => {
    btn.disabled = true;
    btn.textContent = t.restarting;
    try {
      const res = await fetch("/v1/bridge/restart", { method: "POST" });
      // the instruction is out, not the restart: the pid tells us when
      btn.textContent = res.status === 202 ? t.restartAsked : t.restartRefused;
      if (res.status !== 202) btn.disabled = false;
    } catch (err) {
      btn.textContent = String((err && err.message) || err);
      btn.disabled = false;
    }
  });
  root.appendChild(el("div", { class: "restartrow" }, btn));

  // the honest boundaries
  const foot = el("div", { class: "foot" });
  foot.appendChild(el("ul", {}, t.foot.map((line) => el("li", { text: line }))));
  root.appendChild(foot);
  } catch (err) {
    console.warn("[mbridge] render error caught:", err);
  }
}

  globalThis.MbridgeView = {
    PAGE_VERSION,
    TEXT,
    REASONS,
    TIERS,
    el,
    kv,
    pct,
    money,
    ago,
    clock,
    reasonText,
    tierChip,
    turnsOf,
    scoreCell,
    bar,
    liveCard,
    decisionTable,
    distribution,
    calibrationTable,
    render,
    renderGuide,
    detectOS,
  };
})();
