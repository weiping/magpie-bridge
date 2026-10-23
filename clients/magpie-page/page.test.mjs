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
    { t: "2026-10-22T23:20:41Z", harness: "pi", agent: "main", tier: "balanced",
      classified_tier: "fast", reason: "R6-adopt", source: "jev",
      classifier: "typesafe/jev-latest", tier_confidence: 0.61, latency_ms: 1766 },
    { t: "2026-10-22T23:25:01Z", harness: "pi", agent: "main", tier: "balanced",
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
  for (const f of readdirSync(pageDir).filter((f) => f.endsWith(".js"))) {
    const src = read(f);
    // the sandbox has no network at all; a page that tries is a page
    // magpie will refuse to load
    expect(src).not.toMatch(/\bfetch\(|XMLHttpRequest|WebSocket|sendBeacon|EventSource|importScripts|navigator\.sendBeacon/);
    expect(src).not.toMatch(/https?:\/\/(?!127\.0\.0\.1)/);
  }
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
   there — the page would come up blank. */
test("the page's scripts are classic scripts, not modules", () => {
  for (const f of ["view.js", "app.js"]) {
    const src = read(f);
    expect(src).not.toMatch(/^\s*import\s/m);
    expect(src).not.toMatch(/^\s*export\s/m);
    expect(() => new Function(src)).not.toThrow();
  }
  expect(typeof globalThis.MbridgeView).toBe("object");
  expect(typeof globalThis.MbridgeView.mbridgeView).toBe("function");
});

/* 5. the mbridge snapshot: what the page reads out of the options. */
test("the mbridge snapshot is read out of the options", () => {
  const v = mbridgeView(SNAPSHOT, "zh");
  expect(v.none).toBe(false);
  expect(v.version).toBe("0.3.5+sp14");
  expect(v.turnCount).toBe(341);
  expect(v.coverage).toBe("98%");
  expect(v.failures).toBe(40);
  expect(v.topReason).toBe("R6-adopt");
  expect(v.live.length).toBe(2);
  expect(v.live[0].session).toBe("3c8f9a12");
  expect(v.decisions.length).toBe(2);
  // the classifier's advice differs from the tier it went to: bold
  expect(v.decisions[0].differs).toBe(true);
  expect(v.decisions[0].clock).toBe("23:20:41");
  // and the age, so a stale snapshot says so
  expect(typeof v.ageText).toBe("string");
  const none = mbridgeView(null, "en");
  expect(none.none).toBe(true);
});

test("the decisions fall back to the window's turns", () => {
  // this process just restarted: its own decisions ring is empty, but
  // the window's turns are the history
  const v = mbridgeView({ ...SNAPSHOT, decisions: [] }, "zh");
  expect(v.decisions.length).toBe(1);
  expect(v.decisions[0].harness).toBe("codex");
});

test("nothing may break on data that is missing, zero or empty", () => {
  expect(() => mbridgeView({}, "en")).not.toThrow();
  expect(() => mbridgeView(null, "zh")).not.toThrow();
  expect(mbridgeView({}, "en").none).toBe(true);
  const text = JSON.stringify([mbridgeView({}, "en"), mbridgeView({ live: [], window: {} }, "zh")]);
  expect(text).not.toMatch(/NaN|undefined/);
});

/* 6. the page speaks both languages. */
test("the page speaks both languages", () => {
  const en = mbridgeView(SNAPSHOT, "en");
  const zh = mbridgeView(SNAPSHOT, "zh");
  expect(en.topReason).toBe("R6-adopt");
  expect(zh.turnCount).toBe(en.turnCount);
  // the words that say what the page cannot know
  expect(V.text("en").mbridgeNoLive).toMatch(/no session is going through mbridge/);
  expect(V.text("zh").mbridgeNoLive).toMatch(/现在没有会话在走 mbridge/);
});

/* 7. the channel to mbridge: the page writes its commands into the
   options (SP14 §7). */
test("the page asks mbridge for fresh numbers and for a restart", () => {
  const app = read("app.js");
  expect(app).toMatch(/command\("refresh"\)/);
  expect(app).toMatch(/command\("restart"\)/);
  expect(app).toMatch(/o\.cmd = cmd/);
  // the page reads mbridge's snapshot out of the options
  expect(app).toMatch(/state\.mbridge = opts\.mbridge/);
  // and it does not fetch anything: the sandbox has no network
  expect(app).not.toMatch(/\bfetch\(/);
});
