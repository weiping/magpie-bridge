import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

// The Pi object, mocked to the shapes S4/S12 recorded: ctx.sessionManager
// (getSessionId, getHeader), ctx.modelRegistry.find, pi.setModel,
// pi.on(event, handler). Events are driven by calling the stored handler.
type Handler = (event: any, ctx: any) => unknown

function fakePi() {
  const handlers = new Map<string, Handler>()
  const state = {
    sessionId: "s-pi-1" as string | undefined,
    header: {} as Record<string, unknown>,
    models: {
      "magpie/group/mb-fast": { provider: "magpie", id: "group/mb-fast" },
      "magpie/group/mb-balanced": { provider: "magpie", id: "group/mb-balanced" },
    } as Record<string, { provider: string; id: string }>,
    setModelCalls: [] as string[],
    setModelOk: true,
    model: { provider: "magpie", id: "group/mbridge" } as { provider: string; id: string } | undefined,
  }
  const pi = {
    on: vi.fn((name: string, handler: Handler) => {
      handlers.set(name, handler)
      return () => handlers.delete(name)
    }),
    setModel: vi.fn(async (model: { provider: string; id: string }) => {
      state.setModelCalls.push(`${model.provider}/${model.id}`)
      return state.setModelOk
    }),
  }
  const ctx = {
    get model() {
      return state.model
    },
    sessionManager: {
      getSessionId: () => state.sessionId,
      getHeader: () => state.header,
    },
    modelRegistry: {
      find: (provider: string, model: string) => state.models[`${provider}/${model}`],
    },
  }
  return {
    pi,
    ctx,
    state,
    fire: (name: string, event: any) => handlers.get(name)?.(event, ctx),
    registered: () => [...handlers.keys()],
  }
}

function gw(fetchMock: ReturnType<typeof vi.fn>, turns: Array<{ tier: string; group: string }>) {
  let n = 0
  fetchMock.mockImplementation(async (url: string) => {
    if (String(url).endsWith("/v1/bridge/turn")) {
      const t = turns[Math.min(n, turns.length - 1)]
      n++
      return new Response(JSON.stringify(t), { status: 200 })
    }
    return new Response("{}", { status: 200 })
  })
}

const bodies = (fetchMock: ReturnType<typeof vi.fn>, path: string) =>
  fetchMock.mock.calls
    .filter((c) => String(c[0]).endsWith(path))
    .map((c) => JSON.parse(String((c as unknown[])[1] ? (c[1] as RequestInit).body : "")) as Record<string, unknown>)

describe("mbridge extension (SP7 end-of-turn review)", () => {
  let fetchMock: ReturnType<typeof vi.fn>
  beforeEach(() => {
    fetchMock = vi.fn()
    vi.stubGlobal("fetch", fetchMock)
    vi.stubEnv("MBRIDGE_URL", "http://gw")
  })
  afterEach(() => vi.unstubAllGlobals())

  it("agent_end posts one review carrying the turn's prompt and answer", async () => {
    const f = fakePi()
    gw(fetchMock, [{ tier: "fast", group: "group/mb-fast" }])
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "what license is this repo?" })
    await f.fire("agent_end", {
      type: "agent_end",
      messages: [
        { role: "user", content: "what license is this repo?" },
        { role: "assistant", content: [{ type: "text", text: "It is " }, { type: "text", text: "MIT." }] },
      ],
    })
    await vi.waitFor(() => expect(bodies(fetchMock, "/v1/bridge/review").length).toBe(1))
    expect(bodies(fetchMock, "/v1/bridge/review")[0]).toMatchObject({
      session: "s-pi-1",
      harness: "pi",
      prompt: "what license is this repo?",
      answer: "It is MIT.",
    })
  })

  it("the next /turn carries the previous answer, a performance turn's too (SP11)", async () => {
    const f = fakePi()
    gw(fetchMock, [{ tier: "performance", group: "group/mb-balanced" }])
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "CI 还要吗？" })
    await f.fire("agent_end", { type: "agent_end", messages: [{ role: "assistant", content: [{ type: "text", text: "提议：改三个工作流" }] }] })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "按这个思路修改" })
    const turns = bodies(fetchMock, "/v1/bridge/turn")
    expect(turns.length).toBe(2)
    expect(turns[0].previous_answer).toBeUndefined()
    expect(turns[1].previous_answer).toBe("提议：改三个工作流")
  })

  it("an interrupted or failed turn leaves no previous answer (SP11)", async () => {
    const f = fakePi()
    gw(fetchMock, [
      { tier: "fast", group: "group/mb-fast" },
      { tier: "fast", group: "group/mb-fast" },
      { tier: "fast", group: "group/mb-fast" },
      { tier: "fast", group: "group/mb-fast" },
    ])
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" })
    const end = (text: string, stopReason: string) =>
      f.fire("agent_end", { type: "agent_end", messages: [{ role: "assistant", stopReason, content: [{ type: "text", text }] }] })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "重构方案？" })
    await end("方案 A：大重构", "stop")
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "先看下 B" })
    await end("B 的半截", "aborted")
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "按这个思路做" })
    await f.fire("agent_end", { type: "agent_end", messages: [{ role: "assistant", content: [{ type: "toolCall", id: "c1" }] }] })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "继续" })
    const turns = bodies(fetchMock, "/v1/bridge/turn")
    expect(turns.length).toBe(4)
    expect(turns[1].previous_answer).toBe("方案 A：大重构")
    expect(turns[2].previous_answer).toBeUndefined()
    expect(turns[3].previous_answer).toBeUndefined()
  })

  it("a manually pinned session posts no review", async () => {
    const f = fakePi()
    gw(fetchMock, [{ tier: "fast", group: "group/mb-fast" }])
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "hello" })
    f.fire("model_select", { source: "user", model: { id: "other/m" }, previousModel: { id: "magpie/group/mb-fast" } })
    await f.fire("agent_end", { type: "agent_end", messages: [{ role: "assistant", content: "ok" }] })
    expect(bodies(fetchMock, "/v1/bridge/review").length).toBe(0)
  })

  it("a performance-tier turn posts no review", async () => {
    const f = fakePi()
    gw(fetchMock, [{ tier: "performance", group: "group/mb-balanced" }])
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "hello" })
    await f.fire("agent_end", { type: "agent_end", messages: [{ role: "assistant", content: "ok" }] })
    expect(bodies(fetchMock, "/v1/bridge/review").length).toBe(0)
  })

  it("an unreachable gateway does not throw out of agent_end", async () => {
    const f = fakePi()
    let down = false
    fetchMock.mockImplementation(async (url: string) => {
      if (String(url).endsWith("/v1/bridge/turn")) {
        return new Response(JSON.stringify({ tier: "fast", group: "group/mb-fast" }), { status: 200 })
      }
      if (down) throw new Error("connect ECONNREFUSED")
      return new Response("{}", { status: 200 })
    })
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "hello" })
    down = true // the gateway goes away between the turn and its review
    const rejections: unknown[] = []
    const onRejection = (reason: unknown) => rejections.push(reason)
    process.on("unhandledRejection", onRejection)
    expect(() =>
      f.fire("agent_end", { type: "agent_end", messages: [{ role: "assistant", content: "ok" }] }),
    ).not.toThrow()
    await new Promise((r) => setTimeout(r, 20))
    process.off("unhandledRejection", onRejection)
    expect(rejections).toEqual([])
    expect(fetchMock.mock.calls.some((c) => String(c[0]).endsWith("/v1/bridge/review"))).toBe(true)
  })
})

describe("mbridge extension", () => {
  let fetchMock: ReturnType<typeof vi.fn>
  beforeEach(() => {
    fetchMock = vi.fn()
    vi.stubGlobal("fetch", fetchMock)
    vi.stubEnv("MBRIDGE_URL", "http://gw")
  })
  afterEach(() => vi.unstubAllGlobals())

  it("subscribes to exactly the events of §6.8", async () => {
    const f = fakePi()
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    expect(f.registered().sort()).toEqual(
      ["agent_end", "before_agent_start", "before_provider_headers", "model_select", "session_start", "tool_call", "tool_result"].sort(),
    )
  })

  it("session_start keeps Pi's id; headers carry it to the gateway", async () => {
    const f = fakePi()
    f.state.header = { parentSession: "/home/u/.pi/agent/sessions/abc-123.jsonl" }
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" })
    // the handler mutates the event in place (Pi's contract)
    const ev = { type: "before_provider_headers", headers: {} as Record<string, string> }
    f.fire("before_provider_headers", ev)
    expect(ev.headers["X-Magpie-Session"]).toBe("s-pi-1")
  })

  it("session_start asks mbridge how long a turn may take (SP10)", async () => {
    fetchMock.mockResolvedValue(new Response("{}", { status: 200 }))
    const f = fakePi()
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" })
    await vi.waitFor(() =>
      expect(fetchMock.mock.calls.some((c) => String(c[0]) === "http://gw/v1/bridge/router")).toBe(true),
    )
  })

  it("a turn with no budget learned asks mbridge before /turn (SP10 review)", async () => {
    let routerCalls = 0
    fetchMock.mockImplementation(async (url: string) => {
      if (String(url).endsWith("/v1/bridge/router")) {
        routerCalls++
        return routerCalls === 1 ? Promise.reject(new Error("ECONNREFUSED")) : new Response(JSON.stringify({ turn_budget_ms: 3000 }), { status: 200 })
      }
      await new Promise((r) => setTimeout(r, 2000)) // a cold Jev
      return new Response(JSON.stringify({ tier: "fast", group: "group/mb-fast" }), { status: 200 })
    })
    const f = fakePi()
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" }) // mbridge not up yet
    await vi.waitFor(() => expect(routerCalls).toBe(1))
    vi.useFakeTimers()
    const turn = f.fire("before_agent_start", { type: "before_agent_start", prompt: "rename a variable" })
    await vi.advanceTimersByTimeAsync(2200)
    await turn
    vi.useRealTimers()
    expect(routerCalls).toBe(2)
    expect(f.state.setModelCalls).toEqual(["magpie/group/mb-fast"])
  })

  it("session_start falls back to a generated id and /lineage without a parent", async () => {
    const f = fakePi()
    f.state.sessionId = undefined
    f.state.header = {}
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "fork" }) // defensive: reason fork, no parent
    const lin = bodies(fetchMock, "/v1/bridge/lineage")
    expect(lin.length).toBe(1)
    expect(lin[0]).toMatchObject({ session: expect.any(String), source: "pi-fork" })
  })

  it("before_agent_start asks /turn and switches the model on a tier change", async () => {
    const f = fakePi()
    gw(fetchMock, [{ tier: "fast", group: "group/mb-fast" }])
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "what license?" })
    const t = bodies(fetchMock, "/v1/bridge/turn")
    expect(t[0]).toMatchObject({ harness: "pi", session: "s-pi-1", prompt: "what license?", agent: "main", store_hint: false, cwd: expect.any(String) })
    expect(f.state.setModelCalls).toEqual(["magpie/group/mb-fast"])
  })

  it("the same tier twice switches only once; a later change switches again", async () => {
    const f = fakePi()
    gw(fetchMock, [
      { tier: "fast", group: "group/mb-fast" },
      { tier: "fast", group: "group/mb-fast" },
      { tier: "balanced", group: "group/mb-balanced" },
    ])
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "one" })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "two" })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "three, this is much work" })
    expect(f.state.setModelCalls).toEqual(["magpie/group/mb-fast", "magpie/group/mb-balanced"])
  })

  it("empty prompts skip /turn; a failed /turn never touches the model", async () => {
    const f = fakePi()
    fetchMock.mockRejectedValue(new Error("down"))
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "" })
    expect(bodies(fetchMock, "/v1/bridge/turn").length).toBe(0)
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "hello" })
    expect(f.state.setModelCalls).toEqual([])
  })

  it("a missing model in the registry leaves the model alone", async () => {
    const f = fakePi()
    gw(fetchMock, [{ tier: "performance", group: "group/mb-perf" }]) // not in fake registry
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "hard thing" })
    expect(f.state.setModelCalls).toEqual([])
  })

  it("a derived session's first turn sends parent_session", async () => {
    const f = fakePi()
    f.state.header = { parentSession: "/sessions/parent-9.jsonl" }
    gw(fetchMock, [{ tier: "fast", group: "group/mb-fast" }])
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "go on" })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "and again" })
    const t = bodies(fetchMock, "/v1/bridge/turn")
    expect(t[0].parent_session).toBe("parent-9")
    expect(t[1]).not.toHaveProperty("parent_session")
  })

  it("a manual model_select reports and stops auto-switching; our own does not", async () => {
    const f = fakePi()
    gw(fetchMock, [
      { tier: "fast", group: "group/mb-fast" },
      { tier: "balanced", group: "group/mb-balanced" },
    ])
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "one" })
    expect(f.state.setModelCalls).toEqual(["magpie/group/mb-fast"])

    // our own switch (model_select mirroring lastAutoModel): no feedback
    f.fire("model_select", { type: "model_select", source: "set", model: { provider: "magpie", id: "group/mb-fast" }, previousModel: undefined })
    expect(bodies(fetchMock, "/v1/bridge/feedback").length).toBe(0)

    // the user switches by hand: feedback once, auto-switch off
    f.fire("model_select", { type: "model_select", source: "set", model: { provider: "other", id: "m1" }, previousModel: { provider: "magpie", id: "group/mb-fast" } })
    expect(bodies(fetchMock, "/v1/bridge/feedback")).toMatchObject([
      { session: "s-pi-1", kind: "manual_model_switch", value: "group/mb-fast→m1" },
    ])
    f.state.model = { provider: "other", id: "m1" } // Pi's model is the user's now
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "two would switch tiers" })
    expect(f.state.setModelCalls).toEqual(["magpie/group/mb-fast"]) // pinned now

    // restore (a resumed session reloading its model) is neither
    f.fire("model_select", { type: "model_select", source: "restore", model: { id: "x/y" }, previousModel: undefined })
    expect(bodies(fetchMock, "/v1/bridge/feedback").length).toBe(1)
  })
})

describe("mbridge extension: feedback and subagents", () => {
  let fetchMock: ReturnType<typeof vi.fn>
  beforeEach(() => {
    fetchMock = vi.fn()
    vi.stubGlobal("fetch", fetchMock)
    vi.stubEnv("MBRIDGE_URL", "http://gw")
  })
  afterEach(() => vi.unstubAllGlobals())

  // SP13: PR links and subagent picks happen on a routed turn, so boot runs
  // one on the routing group (its /turn is answered, then the mock is reset)
  async function boot() {
    const f = fakePi()
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" })
    gw(fetchMock, [{ tier: "fast", group: "group/mb-fast" }])
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "boot turn" })
    fetchMock.mockClear()
    f.state.setModelCalls.length = 0
    return f
  }

  it("tool_result with a PR link in the text sends pr_created", async () => {
    const f = await boot()
    fetchMock.mockResolvedValue(new Response("{}", { status: 200 }))
    f.fire("tool_result", {
      type: "tool_result",
      isError: false,
      content: [{ type: "text", text: "Opened https://github.com/weiping/magpie-bridge/pull/7 for review" }],
    })
    await new Promise((r) => setTimeout(r, 0))
    expect(bodies(fetchMock, "/v1/bridge/feedback")).toMatchObject([
      { session: "s-pi-1", kind: "pr_created", value: "https://github.com/weiping/magpie-bridge/pull/7" },
    ])
  })

  it("tool_result without a link, or an error result, sends nothing", async () => {
    const f = await boot()
    fetchMock.mockResolvedValue(new Response("{}", { status: 200 }))
    f.fire("tool_result", { type: "tool_result", isError: false, content: [{ type: "text", text: "done" }] })
    f.fire("tool_result", { type: "tool_result", isError: true, content: [{ type: "text", text: "Opened https://github.com/a/b/pull/1" }] })
    await new Promise((r) => setTimeout(r, 0))
    expect(bodies(fetchMock, "/v1/bridge/feedback").length).toBe(0)
  })

  it("tool_call picks a tier for a fresh pi-subagents spawn and writes the model", async () => {
    const f = await boot()
    gw(fetchMock, [{ tier: "fast", group: "group/mb-fast" }])
    const input: Record<string, unknown> = { context: "fresh", task: "search the repo for todo markers" }
    await f.fire("tool_call", { type: "tool_call", toolName: "subagent", input })
    const t = bodies(fetchMock, "/v1/bridge/turn")
    expect(t[0]).toMatchObject({ session: "s-pi-1", prompt: "search the repo for todo markers", agent: "subagent", store_hint: false })
    expect(input.model).toBe("magpie/group/mb-fast")
  })

  it("tool_call uses the tier's real group id (performance is mb-perf, not mb-performance)", async () => {
    const f = await boot()
    gw(fetchMock, [{ tier: "performance", group: "group/mb-perf" }])
    const input: Record<string, unknown> = { context: "fresh", task: "design the migration" }
    await f.fire("tool_call", { type: "tool_call", toolName: "subagent", input })
    expect(input.model).toBe("magpie/group/mb-perf")
  })

  it("tool_call also handles dispatch_agent and the prompt/description fields", async () => {
    const f = await boot()
    gw(fetchMock, [{ tier: "balanced", group: "group/mb-balanced" }])
    const input: Record<string, unknown> = { context: "fresh", description: "fix the flaky test" }
    await f.fire("tool_call", { type: "tool_call", toolName: "dispatch_agent", input })
    expect(bodies(fetchMock, "/v1/bridge/turn")[0].prompt).toBe("fix the flaky test")
    expect(input.model).toBe("magpie/group/mb-balanced")
  })

  it("tool_call leaves fork-context spawns, preset models and other tools alone", async () => {
    const f = await boot()
    gw(fetchMock, [{ tier: "fast", group: "group/mb-fast" }])
    const forked: Record<string, unknown> = { context: "fork", task: "keep going in the fork" }
    await f.fire("tool_call", { type: "tool_call", toolName: "subagent", input: forked })
    const preset: Record<string, unknown> = { context: "fresh", task: "x", model: "magpie/some-model" }
    await f.fire("tool_call", { type: "tool_call", toolName: "subagent", input: preset })
    const other = { command: "ls" }
    await f.fire("tool_call", { type: "tool_call", toolName: "bash", input: other })
    expect(bodies(fetchMock, "/v1/bridge/turn").length).toBe(0)
    expect(forked).not.toHaveProperty("model")
    expect(preset.model).toBe("magpie/some-model")
  })

  it("tool_call with a failing /turn leaves the spawn without a model", async () => {
    const f = await boot()
    fetchMock.mockRejectedValue(new Error("down"))
    const input: Record<string, unknown> = { context: "fresh", task: "hard thing" }
    await f.fire("tool_call", { type: "tool_call", toolName: "subagent", input })
    expect(input).not.toHaveProperty("model")
  })
})

describe("mbridge extension (SP8)", () => {
  let fetchMock: ReturnType<typeof vi.fn>
  beforeEach(() => {
    fetchMock = vi.fn()
    vi.stubGlobal("fetch", fetchMock)
    vi.stubEnv("MBRIDGE_URL", "http://gw")
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.unstubAllEnvs()
  })

  it("reports tool stats of the previous turn", async () => {
    const f = fakePi()
    gw(fetchMock, [{ tier: "fast", group: "group/mb-fast" }])
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "one" })
    f.fire("tool_result", { toolName: "bash", isError: false, content: [] })
    f.fire("tool_result", { toolName: "read", isError: true, content: [] })
    f.fire("tool_result", { toolName: "bash", isError: true, content: [] })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "two" })
    const t = bodies(fetchMock, "/v1/bridge/turn")
    expect(t[0].tool_calls).toBeUndefined()
    expect(t[1]).toMatchObject({ tool_calls: 3, tool_failures: 2 })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "three" })
    expect(bodies(fetchMock, "/v1/bridge/turn")[2]).toMatchObject({ tool_calls: 0, tool_failures: 0 })
  })

  it("defaults to mbridge on 3426", async () => {
    vi.unstubAllEnvs()
    const f = fakePi()
    gw(fetchMock, [{ tier: "fast", group: "group/mb-fast" }])
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "one" })
    const urls = fetchMock.mock.calls.map((c) => String(c[0]))
    expect(urls).toContain("http://127.0.0.1:3426/v1/bridge/router") // SP10's probe at session_start
    expect(urls).toContain("http://127.0.0.1:3426/v1/bridge/turn")
  })

  it("an unreachable mbridge keeps the model and says so in the status", async () => {
    const f = fakePi()
    const statuses: Array<[string, string | undefined]> = []
    ;(f.ctx as any).ui = { setStatus: (k: string, t: string | undefined) => statuses.push([k, t]) }
    fetchMock.mockRejectedValue(new Error("ECONNREFUSED"))
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" })
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "one" })
    expect(f.state.setModelCalls).toEqual([])
    expect(statuses).toContainEqual(["mbridge", "mbridge: 未运行"])
    // back up: the status clears
    gw(fetchMock, [{ tier: "fast", group: "group/mb-fast" }])
    await f.fire("before_agent_start", { type: "before_agent_start", prompt: "two" })
    expect(statuses[statuses.length - 1]).toEqual(["mbridge", undefined])
  })
})

// ---- SP13: only a turn on group/mbridge is routed ----

// Pi names a model { provider, id }: magpie's routing group is
// { provider: "magpie", id: "group/mbridge" }, and the tier groups the
// extension switches to are { provider: "magpie", id: "group/mb-fast" } and so on.
const onModel = (id: string, provider = "magpie") => ({ provider, id })

describe("mbridge extension (SP13)", () => {
  let fetchMock: ReturnType<typeof vi.fn>
  beforeEach(() => {
    fetchMock = vi.fn()
    vi.stubGlobal("fetch", fetchMock)
    vi.stubEnv("MBRIDGE_URL", "http://gw")
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.unstubAllEnvs()
  })

  async function boot(model: { provider: string; id: string } | undefined, turns = [{ tier: "fast", group: "group/mb-fast" }]) {
    const f = fakePi()
    ;f.state.model = model
    gw(fetchMock, turns)
    ;(await import("../extensions/mbridge.js")).default(f.pi as never)
    f.fire("session_start", { type: "session_start", reason: "startup" })
    return f
  }
  const turn = (f: ReturnType<typeof fakePi>, prompt: string) =>
    f.fire("before_agent_start", { type: "before_agent_start", prompt })
  const asked = () => bodies(fetchMock, "/v1/bridge/turn").length

  it("on group/mbridge a turn is routed", async () => {
    const f = await boot(onModel("group/mbridge"))
    await turn(f, "what license?")
    expect(asked()).toBe(1)
    expect(f.state.setModelCalls).toEqual(["magpie/group/mb-fast"])
  })

  it("a [1m] suffix still counts as group/mbridge", async () => {
    const f = await boot(onModel("group/mbridge[1m]"))
    await turn(f, "what license?")
    expect(asked()).toBe(1)
  })

  it("on any other model nothing is asked, switched or shown", async () => {
    for (const m of [onModel("group/auto-claude-sonnet-5-5"), onModel("group/mb-perf"), onModel("gpt-6-sol", "openai"), onModel("group/mbridge", "other"), undefined]) {
      fetchMock.mockReset()
      const f = await boot(m)
      const statuses: Array<[string, string | undefined]> = []
      ;(f.ctx as any).ui = { setStatus: (k: string, t: string | undefined) => statuses.push([k, t]) }
      await turn(f, "rename a variable")
      expect(asked()).toBe(0)
      expect(f.state.setModelCalls).toEqual([])
      expect(statuses).toEqual([])
    }
  })

  // After the first routed turn Pi's model is a tier group we set ourselves.
  // That is still the routing group's turn, not a user's choice of mb-fast.
  it("our own setModel keeps later turns routed", async () => {
    const f = await boot(onModel("group/mbridge"), [
      { tier: "fast", group: "group/mb-fast" },
      { tier: "balanced", group: "group/mb-balanced" },
    ])
    await turn(f, "one")
    f.fire("model_select", { type: "model_select", source: "set", model: onModel("group/mb-fast"), previousModel: onModel("group/mbridge") })
    ;f.state.model = onModel("group/mb-fast") // Pi's model now
    await turn(f, "two, more work")
    expect(asked()).toBe(2)
    expect(f.state.setModelCalls).toEqual(["magpie/group/mb-fast", "magpie/group/mb-balanced"])
    expect(bodies(fetchMock, "/v1/bridge/feedback").length).toBe(0)
  })

  it("picking a tier group by hand turns routing off, and says so once", async () => {
    const f = await boot(onModel("group/mbridge"))
    await turn(f, "one")
    f.fire("model_select", { type: "model_select", source: "set", model: onModel("group/mb-fast"), previousModel: onModel("group/mbridge") })
    ;f.state.model = onModel("group/mb-fast")
    // the user, not us, picks mb-perf
    f.fire("model_select", { type: "model_select", source: "user", model: onModel("group/mb-perf"), previousModel: onModel("group/mb-fast") })
    ;f.state.model = onModel("group/mb-perf")
    f.fire("model_select", { type: "model_select", source: "user", model: onModel("gpt-6-sol", "openai"), previousModel: onModel("group/mb-perf") })
    ;f.state.model = onModel("gpt-6-sol", "openai")
    await turn(f, "two")
    await turn(f, "three")
    expect(asked()).toBe(1)
    expect(bodies(fetchMock, "/v1/bridge/feedback")).toMatchObject([
      { session: "s-pi-1", kind: "manual_model_switch" },
    ])
  })

  it("a manual switch back to group/mbridge routes again", async () => {
    const f = await boot(onModel("group/mbridge"), [
      { tier: "fast", group: "group/mb-fast" },
      { tier: "balanced", group: "group/mb-balanced" },
    ])
    await turn(f, "one")
    f.fire("model_select", { type: "model_select", source: "user", model: onModel("gpt-6-sol", "openai"), previousModel: onModel("group/mb-fast") })
    ;f.state.model = onModel("gpt-6-sol", "openai")
    await turn(f, "two")
    expect(asked()).toBe(1)
    f.fire("model_select", { type: "model_select", source: "user", model: onModel("group/mbridge"), previousModel: onModel("gpt-6-sol", "openai") })
    ;f.state.model = onModel("group/mbridge")
    await turn(f, "three")
    expect(asked()).toBe(2)
    expect(f.state.setModelCalls).toEqual(["magpie/group/mb-fast", "magpie/group/mb-balanced"])
  })

  it("a session that starts on another model reports no manual switch", async () => {
    const f = await boot(onModel("group/auto-claude-sonnet-5-5"))
    f.fire("model_select", { type: "model_select", source: "restore", model: onModel("group/auto-claude-sonnet-5-5"), previousModel: undefined })
    f.fire("model_select", { type: "model_select", source: "user", model: onModel("group/mb-perf"), previousModel: onModel("group/auto-claude-sonnet-5-5") })
    ;f.state.model = onModel("group/mb-perf")
    await turn(f, "one")
    expect(bodies(fetchMock, "/v1/bridge/feedback").length).toBe(0)
  })

  it("on another model: no tool stats, PR link, review or subagent pick", async () => {
    const f = await boot(onModel("group/auto-claude-sonnet-5-5"))
    await turn(f, "one")
    f.fire("tool_result", { toolName: "bash", isError: false, content: [{ type: "text", text: "Opened https://github.com/weiping/magpie-bridge/pull/7" }] })
    await f.fire("agent_end", { type: "agent_end", messages: [{ role: "assistant", content: [{ type: "text", text: "done" }] }] })
    const input: Record<string, unknown> = { context: "fresh", task: "search the repo" }
    await f.fire("tool_call", { type: "tool_call", toolName: "subagent", input })
    await new Promise((r) => setTimeout(r, 10))
    expect(asked()).toBe(0)
    expect(bodies(fetchMock, "/v1/bridge/feedback").length).toBe(0)
    expect(bodies(fetchMock, "/v1/bridge/review").length).toBe(0)
    expect(input).not.toHaveProperty("model")
  })

  it("on group/mbridge a PR link, a review and a subagent pick still go out", async () => {
    const f = await boot(onModel("group/mbridge"), [{ tier: "fast", group: "group/mb-fast" }])
    await turn(f, "one")
    f.fire("tool_result", { toolName: "bash", isError: false, content: [{ type: "text", text: "Opened https://github.com/weiping/magpie-bridge/pull/7" }] })
    await f.fire("agent_end", { type: "agent_end", messages: [{ role: "assistant", content: [{ type: "text", text: "done" }] }] })
    const input: Record<string, unknown> = { context: "fresh", task: "search the repo" }
    await f.fire("tool_call", { type: "tool_call", toolName: "subagent", input })
    await vi.waitFor(() => expect(bodies(fetchMock, "/v1/bridge/review").length).toBe(1))
    expect(bodies(fetchMock, "/v1/bridge/feedback")).toMatchObject([{ kind: "pr_created" }])
    expect(input.model).toBe("magpie/group/mb-fast")
  })

  it("the previous answer is still kept on another model, for a later routed turn", async () => {
    const f = await boot(onModel("group/auto-claude-sonnet-5-5"), [{ tier: "fast", group: "group/mb-fast" }])
    await turn(f, "CI?")
    await f.fire("agent_end", { type: "agent_end", messages: [{ role: "assistant", content: [{ type: "text", text: "propose three workflows" }] }] })
    f.fire("model_select", { type: "model_select", source: "user", model: onModel("group/mbridge"), previousModel: onModel("group/auto-claude-sonnet-5-5") })
    ;f.state.model = onModel("group/mbridge")
    await turn(f, "do it")
    expect(bodies(fetchMock, "/v1/bridge/turn")[0].previous_answer).toBe("propose three workflows")
  })
})
