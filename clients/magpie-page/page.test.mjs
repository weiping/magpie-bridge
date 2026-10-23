// page.test.mjs is mbridge's magpie page under test (SP14 §5). It checks
// the things that are easy to break and expensive to break: the
// permissions, the package's shape (a window plugin must stay one), the
// page's own reading of mbridge's snapshot, and the channel to mbridge.
import { expect, test } from "bun:test";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

// The page loads view.js as a classic script (the magpie sandbox serves
// the plugin's own files; the community's usage page does it this way).
// The test loads it the same way and reads what it put on globalThis.
await import("./page/view.js");
const V = globalThis.MbridgeView;
const { mbridgeView } = V;

const dir = import.meta.dir;
const pageDir = join(dir, "page");
const read = (f) => readFileSync(join(pageDir, f), "utf8");
const pkg = JSON.parse(readFileSync(join(dir, "package.json"), "utf8"));

const SNAPSHOT = {
  version: "0.3.5+sp14",
  pid: 5574,
  uptime_s: 120,
  listen: "127.0.0.1:3426",
  service: { installed: true, running: true },
  magpie_ok: true,
  magpie_version: "0.1.1318",
  at: new Date().toISOString(),
  seq: 12,
  live: [
    { session: "3c8f9a12aabb", harness: "codex", agent: "main", tier: "balanced",
      group: "group/mb-balanced", reason: "R6-adopt", tier_confidence: 0.82, turns: 7,
      tools: 0, tool_failures: 0, last_at: new Date().toISOString() },
    { session: "4e2b1d77ccdd", harness: "gateway", agent: "gateway", tier: "performance",
      group: "group/mb-perf", reason: "R3-tools", turns: 3,
      tools: 24, tool_failures: 12, last_at: new Date().toISOString() },
  ],
  decisions: [
    { kind: "decide", t: "2026-10-22T23:20:41Z", harness: "pi", agent: "main", tier: "balanced",
      classified_tier: "fast", reason: "R6-adopt", source: "jev",
      classifier: "typesafe/jev-latest", tier_confidence: 0.61, latency_ms: 1766 },
    { kind: "decide", t: "2026-10-22T23:25:01Z", harness: "pi", agent: "main", tier: "balanced",
      reason: "R8-default", source: "default", latency_ms: 12 },
  ],
  window: {
    kind: "7d",
    coverage: { asked: 341, total: 349 },
    classify: { failures: 40 },
    tiers: { fast: 155, balanced: 104, performance: 82 },
    reasons: { "R6-adopt": 167, "R8-default": 44 },
    turns: [
      { kind: "decide", t: "2026-10-22T22:29:09Z", harness: "codex", agent: "main",
        tier: "fast", classified_tier: "fast", reason: "R6-adopt", latency_ms: 827 },
    ],
  },
};

/* 1. permissions: the page reads no magpie read at all — its data comes
   from the options, and options are not gated. Least privilege is an
   empty list. */
test("the page asks for no permissions at all", () => {
  expect(pkg.magpie.permissions).toEqual([]);
  const appSrc = read("app.js");
  expect(appSrc).not.toMatch(/\bfetch\(/);
  for (const f of readdirSync(pageDir).filter((f) => f.endsWith(".js"))) {
    const src = read(f);
    expect(src).not.toMatch(/XMLHttpRequest|WebSocket|sendBeacon|EventSource|importScripts|navigator\.sendBeacon/);
    // In text strings (such as installation guide scripts), github URLs appear as commands to run in terminal,
    // but no active network requests should be made
    expect(src).not.toMatch(/fetch\s*\(\s*["']https?:\/\/(?!127\.0\.0\.1)/);
  }
});

/* 2. shape: this must stay a window plugin (SP14 §4.4). Adding main or
   exports would make every options.set() restart magpie's plugin host. */
test("the package is a window plugin, so its writes never restart magpie", () => {
  expect(pkg.main).toBeUndefined();
  expect(pkg.exports).toBeUndefined();
  expect(pkg.magpie.page.file).toBe("page/index.html");
  expect(pkg.magpie.page.icon).toBe("page/icon.svg");
  expect(pkg.magpie.page.title).toBe("鹊桥");
  expect(pkg.magpie.page.titles).toMatchObject({ zh: "鹊桥", "zh-TW": "鵲橋" });
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
   there — the page would come up blank. */
test("the page's scripts are classic scripts, not modules", () => {
  for (const f of ["view.js", "app.js"]) {
    const src = read(f);
    expect(src).not.toMatch(/^\s*import\s/m);
    expect(src).not.toMatch(/^\s*export\s/m);
    expect(() => new Function(src)).not.toThrow();
  }
  expect(typeof globalThis.MbridgeView).toBe("object");
  expect(typeof globalThis.MbridgeView.render).toBe("function");
});

/* 5. the mbridge snapshot: what the page reads out of the options. */
test("the mbridge snapshot turnsOf extracts decisions and window turns", () => {
  const turns = V.turnsOf(SNAPSHOT);
  expect(turns.length).toBe(3);
  expect(turns[0].harness).toBe("pi");
});

test("the page speaks both languages", () => {
  expect(V.TEXT.en.title).toBe("mbridge");
  expect(V.TEXT.zh.title).toBe("鹊桥");
  expect(V.TEXT.en.noLive).toMatch(/no session is going through mbridge/);
  expect(V.TEXT.zh.noLive).toMatch(/现在没有会话在走 mbridge/);
});

test("formatters format numbers gracefully", () => {
  expect(V.money(0)).toBe("$0");
  expect(V.money(12.34)).toBe("$12.34");
  expect(V.pct(1, 4)).toBe("25%");
  expect(V.pct(0, 0)).toBe("0%");
});

/* 7. the channel to mbridge: the page writes its commands into the
   options (SP14 §7). */
test("the page asks mbridge for fresh numbers and for a restart", () => {
  const app = read("app.js");
  expect(app).not.toMatch(/command\("refresh"\)/);
  expect(app).toMatch(/command\("restart"\)/);
  expect(app).toMatch(/o\.cmd =/);
  // the page reads mbridge's snapshot out of the options
  expect(app).toMatch(/opts\.mbridge/);
  // and it does not fetch anything: the sandbox has no network
  expect(app).not.toMatch(/\bfetch\(/);
});

/* 8. error protection during tier switches: malformed or partial live
   cards and decisions never throw or crash the page. */
test("tier switch rendering is protected against malformed or partial data", () => {
  // Set up minimal DOM mock for V.render and V.liveCard
  const makeNode = () => ({
    textContent: "",
    className: "",
    style: {},
    children: [],
    appendChild(c) { this.children.push(c); return c; },
    setAttribute() {},
    addEventListener() {},
  });
  globalThis.document = {
    createElement: () => makeNode(),
    createTextNode: (t) => ({ textContent: String(t) }),
    getElementById: () => makeNode(),
  };

  expect(() => {
    V.render(null);
    V.render({});
    V.render({
      version: "0.4.0",
      pid: 1234,
      live: [
        null,
        undefined,
        { session: 12345, harness: null, tier: "fast", tier_confidence: "invalid-float", turns: 2 },
        { session: "abc", agent: "gateway", tier: "performance", tier_confidence: NaN, tools: "bad" },
      ],
      decisions: [
        null,
        { kind: "decide", t: "invalid-date", session: 999, tier: "balanced", classified_tier: "fast", tier_confidence: "bad", dissatisfied: "nan" },
      ],
      window: {
        tiers: null,
        reasons: "not-an-object",
        classify: { error_kinds: null, failures: 2 },
        calibrate: [null, { score: "tier", bands: [null, { lo: 0, hi: 1, n: "bad", under_rate: "bad" }] }],
      },
      config: { fixed_agents: null, tiers: null },
    });
  }).not.toThrow();
});
