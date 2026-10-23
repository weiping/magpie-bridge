// page.test.mjs is mbridge's magpie page under test (SP14 §5). It checks
// three things that are easy to break and expensive to break: the
// permissions, the package's shape (a window plugin must stay one), and
// the page's own arithmetic on data that can be empty.
import { expect, test } from "bun:test";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

// The page loads view.js as a classic script (the magpie sandbox serves
// the plugin's own files; the community's usage page does it this way).
// The test loads it the same way and reads what it put on globalThis.
await import("./page/view.js");
const V = globalThis.MbridgeView;
const { count, money, pct, tiles, rows, mbridgeView } = V;

const dir = import.meta.dir;
const pageDir = join(dir, "page");
const read = (f) => readFileSync(join(pageDir, f), "utf8");
const pkg = JSON.parse(readFileSync(join(dir, "package.json"), "utf8"));

const SUMMARY = {
  period: "7d",
  since: "2026-10-15T00:00:00Z",
  calls: 1200,
  errors: 30,
  input: 900000,
  output: 300000,
  cacheRead: 500000,
  cost: 12.3456,
  models: [
    { model: "deepseek/deepseek-chat", calls: 800, input: 400000, output: 100000, cost: 5 },
    { model: "kimi/kimi-k2", calls: 300, input: 400000, output: 150000, cost: 6 },
    { model: "openai/gpt-5-mini", calls: 100, input: 100000, output: 50000, cost: 1.3456 },
  ],
};

/* 1. permissions: exactly what the page needs, and nothing it cannot use. */
test("the page reads only usage:summary", () => {
  expect(pkg.magpie.permissions).toEqual(["usage:summary"]);
  const reads = new Set();
  for (const f of readdirSync(pageDir).filter((f) => f.endsWith(".js"))) {
    const src = read(f);
    for (const m of src.matchAll(/magpie\.read\(\s*["']([^"']+)["']/g)) reads.add(m[1]);
    // the sandbox has no network at all; a page that tries is a page
    // magpie will refuse to load
    expect(src).not.toMatch(/\bfetch\(|XMLHttpRequest|WebSocket|sendBeacon|EventSource|importScripts|navigator\.sendBeacon/);
    expect(src).not.toMatch(/https?:\/\/(?!127\.0\.0\.1)/);
  }
  expect([...reads].sort()).toEqual(["usage:summary"]);
});

/* 2. shape: this must stay a window plugin (SP14 §4.4). Adding main or
   exports would make every options.set() restart magpie's plugin host. */
test("the package is a window plugin, so its writes never restart magpie", () => {
  expect(pkg.main).toBeUndefined();
  expect(pkg.exports).toBeUndefined();
  expect(pkg.magpie.page.file).toBe("page/index.html");
  expect(pkg.magpie.page.icon).toBe("page/icon.svg");
  expect(pkg.magpie.page.titles).toMatchObject({ zh: expect.any(String), "zh-TW": expect.any(String) });
});

/* 3. the page loads only its own files, and no inline script (the CSP
   refuses inline scripts). */
test("the page loads only its own files", () => {
  const html = read("index.html");
  expect(html).toContain('<script src="_magpie/bridge.js">');
  for (const m of html.matchAll(/<script([^>]*)>([\s\S]*?)<\/script>/g)) {
    expect(m[1]).toMatch(/\ssrc="/);
    expect(m[2].trim()).toBe("");
  }
  for (const m of html.matchAll(/(?:src|href)="([^"]+)"/g)) {
    const ref = m[1];
    if (ref.startsWith("_magpie/")) continue;
    expect(ref).not.toMatch(/^(?:[a-z]+:|\/|\.\.)/i);
    expect(readdirSync(pageDir)).toContain(ref);
  }
  // the address of mbridge's own page is selectable text, not a link: the
  // sandbox may navigate itself, but that is a one-way ticket (SP14 §2)
  expect(html).not.toMatch(/href="http/);
});


/* 4. the page's scripts must be classic scripts: the magpie sandbox loads
   them with <script src>, and a module (import/export) is a SyntaxError
   there — the page would come up blank. This is the test that catches
   it. */
test("the page's scripts are classic scripts, not modules", () => {
  for (const f of ["view.js", "app.js"]) {
    const src = read(f);
    expect(src).not.toMatch(/^\s*import\s/m);
    expect(src).not.toMatch(/^\s*export\s/m);
    // what a classic script means: it parses as one
    expect(() => new Function(src)).not.toThrow();
  }
  // and the view is on globalThis, where the page looks for it
  expect(typeof globalThis.MbridgeView).toBe("object");
  expect(typeof globalThis.MbridgeView.tiles).toBe("function");
});

/* 5. the arithmetic, exactly, in both languages. */
test("the four tiles, in both languages", () => {
  expect(tiles(SUMMARY, "en")).toEqual([
    { key: "calls", label: "calls", value: "1,200" },
    { key: "errors", label: "errors", value: "30", sub: "of the calls 2.5%" },
    { key: "tokens", label: "tokens", value: "1.20M" },
    { key: "cost", label: "cost", value: "$12.35" },
  ]);
  expect(tiles(SUMMARY, "zh")).toEqual([
    { key: "calls", label: "调用", value: "1,200" },
    { key: "errors", label: "错误", value: "30", sub: "占调用 2.5%" },
    { key: "tokens", label: "Token", value: "1.20M" },
    { key: "cost", label: "费用", value: "$12.35" },
  ]);
  expect(V.PERIODS).toEqual(["today", "7d", "30d"]);
  expect(V.SORTS).toEqual(["tokens", "calls", "cost"]);
  expect(V.periodLabel("7d", "en")).toBe("7 days");
  expect(V.periodLabel("7d", "zh")).toBe("7 天");
  expect(V.sortLabel("cost", "zh")).toBe("费用");
});

test("the model table, sorted, with shares", () => {
  const byTokens = rows(SUMMARY, "tokens", "en");
  expect(byTokens.map((i) => i.model)).toEqual([
    "kimi/kimi-k2",            // 400k in + 150k out
    "deepseek/deepseek-chat",  // 400k in + 100k out
    "openai/gpt-5-mini",
  ]);
  expect(byTokens[0].tokens).toBe("550,000");
  expect(byTokens[0].share).toBe("45.8%");

  const byCalls = rows(SUMMARY, "calls", "en");
  expect(byCalls[0].calls).toBe("800");
  const byCost = rows(SUMMARY, "cost", "zh");
  expect(byCost[0].model).toBe("kimi/kimi-k2");
});

test("numbers are formatted the way a person reads them", () => {
  expect(count(0)).toBe("0");
  expect(count(999)).toBe("999");
  expect(count(1234)).toBe("1,234");
  expect(count(1234567)).toBe("1.23M");
  expect(money(0)).toBe("$0");
  expect(money(0.004)).toBe("<$0.01");
  expect(money(1.5)).toBe("$1.5");
  expect(money(12.3456)).toBe("$12.35");
  expect(money(undefined)).toBe("$0");
  expect(pct(0, 0)).toBe("0%");
  expect(pct(1, 3)).toBe("33.3%");
});

/* 6. nothing may break on data that is missing, zero or empty. */
test("empty data does not break the page", () => {
  const empty = tiles({}, "en");
  expect(empty.map((t) => t.value)).toEqual(["0", "0", "0", "$0"]);
  expect(empty[1].sub).toBe("of the calls 0%");
  expect(() => rows({ models: [] }, "tokens", "en")).not.toThrow();
  expect(rows({ models: [] }, "tokens", "en")).toEqual([]);
  expect(rows(null, "cost", "zh")).toEqual([]);
  // a model with no cost recorded shares nothing: 0%, not a fake 100%
  expect(rows({ models: [{ model: "x" }] }, "cost", "en")[0].share).toBe("0%");
  // no NaN anywhere, in any language
  const text = JSON.stringify([
    tiles({}, "en"), tiles({}, "zh"), rows({}, "tokens", "en"), V.since({}, "zh"),
  ]);
  expect(text).not.toMatch(/NaN|undefined/);
});

test("the page says what it does not know", () => {
  expect(V.text("en").disclaimer).toMatch(/everything magpie recorded/);
  expect(V.text("zh").empty).toBe("这个区间没有调用");
  expect(V.text("zh").disclaimer).toMatch(/magpie 记下的全部用量/);
  // no tier attribution in the usage part: magpie records the model that
  // answered, and which tier it was is mbridge's business. (mbridge's
  // own section does show tiers — it is mbridge's own data.)
  const view = read("view.js");
  const usage = view.slice(view.indexOf("function tiles"), view.indexOf("function since"));
  expect(usage).not.toMatch(/tier/i);
  // and the usage rows are models, not tiers
  expect(rows(SUMMARY, "tokens", "en")[0].model).toBe("kimi/kimi-k2");
});

/* 7. the mbridge snapshot the page shows: what mbridge published into
   the plugin's options (SP14 §7). */
test("the mbridge snapshot is read out of the options", () => {
  const snapshot = {
    version: "0.3.5+sp14", pid: 5574, uptime_s: 120, listen: "127.0.0.1:3426",
    service: { installed: true, running: true },
    magpie_ok: true, magpie_version: "0.1.1318",
    at: new Date().toISOString(), seq: 12,
    live: [{ session: "3c8f9a12aabb", harness: "codex", agent: "main", tier: "balanced",
             group: "group/mb-balanced", reason: "R6-adopt", tier_confidence: 0.82, turns: 7,
             tool_failures: 0, tools: 0, last_at: new Date().toISOString() }],
    decisions: [{ t: "2026-10-22T23:20:41Z", harness: "pi", agent: "main", tier: "balanced",
                  classified_tier: "fast", reason: "R6-adopt", source: "jev",
                  classifier: "typesafe/jev-latest", tier_confidence: 0.61, latency_ms: 1766 }],
    window: { kind: "7d", coverage: { asked: 341, total: 349 }, classify: { failures: 40 },
              tiers: { fast: 155, balanced: 104 }, reasons: { "R6-adopt": 167 } },
  };
  const v = mbridgeView(snapshot, "zh");
  expect(v.none).toBe(false);
  expect(v.turnCount).toBe(259);
  expect(v.coverage).toBe("98%");
  expect(v.failures).toBe(40);
  expect(v.topReason).toBe("R6-adopt");
  expect(v.live.length).toBe(1);
  expect(v.live[0].session).toBe("3c8f9a12");
  expect(v.decisions.length).toBe(1);
  expect(v.decisions[0].differs).toBe(true);
  expect(v.decisions[0].clock).toBe("23:20:41");
  // the age is shown, so a stale snapshot says so
  expect(typeof v.ageText).toBe("string");
  // no snapshot at all
  const none = mbridgeView(null, "en");
  expect(none.none).toBe(true);
});

test("the page asks mbridge for fresh numbers and for a restart", () => {
  const app = read("app.js");
  // the commands are written into the options, with a sequence number
  expect(app).toMatch(/command\("refresh"\)/);
  expect(app).toMatch(/command\("restart"\)/);
  expect(app).toMatch(/o\.cmd = cmd/);
  // the page reads mbridge's snapshot out of the options
  expect(app).toMatch(/state\.mbridge = o\.mbridge/);
  // and it does not fetch anything: the sandbox has no network
  expect(app).not.toMatch(/\bfetch\(/);
});
