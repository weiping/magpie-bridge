import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent"
import { MbridgeClient } from "../src/client.js"
import { idsFrom } from "../src/session.js"

/**
 * pi-magpie-bridge: routes each Pi turn across mbridge's tiers (§6.8).
 *
 * before_agent_start asks the gateway which tier this prompt deserves and
 * switches Pi's model when the tier moved; every failure keeps the model
 * as-is, so the worst outcome is "the default tier served this turn".
 *
 * SP13: all of it happens only on a turn whose model is group/mbridge. Pi
 * has merged its settings (project over user, --model, /model) by then, so
 * the extension reads ctx.model and nothing else.
 */

const PROVIDER = "magpie"
const ROUTING_GROUP = "group/mbridge"

type ModelLite = { provider?: string; id?: string } | undefined

// "group/mbridge[1m]" is the routing group too (magpie's name for a 1M window)
const stripSuffix = (id: string): string => id.replace(/\[[^\]]*\]$/, "")

// modelKey names a model the way lastAutoModel remembers it
const modelKey = (m: ModelLite): string => (m?.id === undefined ? "" : `${m.provider ?? ""}/${m.id}`)

// isRoutingModel: the model is magpie's routing group, not one of its tier
// groups, which only we or the user choose.
const isRoutingModel = (m: ModelLite): boolean =>
  m?.provider === PROVIDER && typeof m.id === "string" && stripSuffix(m.id) === ROUTING_GROUP
const PR_LINK = /https:\/\/github\.com\/[\w.-]+\/[\w.-]+\/pull\/\d+/
const SUBAGENT_TOOLS = new Set(["subagent", "dispatch_agent"]) // S12

type AgentMessageLite = { role?: string; content?: unknown; stopReason?: string }

// lastAssistantText is S14's reading of the turn's final assistant text:
// a plain string, or the text parts of a content array joined.
function lastAssistantText(messages: AgentMessageLite[]): string | null {
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i]
    if (m?.role !== "assistant") continue
    if (typeof m.content === "string") return m.content
    if (Array.isArray(m.content)) {
      const parts = m.content as Array<{ type?: string; text?: string }>
      const text = parts
        .filter((p) => p?.type === "text" && typeof p.text === "string")
        .map((p) => p.text as string)
        .join("")
      return text === "" ? null : text
    }
    return null
  }
  return null
}

// finalAnswer is SP11's previous answer: the turn's final assistant text, or
// null when the turn ended without one (interrupted, an error, a last
// message with no text). The answer before it is not what the user now
// points at.
function finalAnswer(messages: AgentMessageLite[]): string | null {
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i]
    if (m?.role !== "assistant") continue
    if (m.stopReason === "aborted" || m.stopReason === "error") return null
    return lastAssistantText([m])
  }
  return null
}

export default function (pi: ExtensionAPI): void {
  const client = new MbridgeClient(process.env.MBRIDGE_URL ?? "http://127.0.0.1:3426")
  // SP10: learn how long a turn may wait before the first one, when the
  // session_start probe found mbridge not up yet
  const learnedThenTurn = async (req: Parameters<typeof client.turn>[0]) => {
    if (!client.learned) await client.learnBudget()
    return client.turn(req)
  }

  // per-session memory; rebuilt at every session_start
  let session = ""
  let parent: string | null = null
  let parentSent = false
  let lastTier: string | null = null
  let lastPrompt = "" // this turn's words, for the end-of-turn review (SP7)
  let lastAnswer: string | null = null // the previous final answer, for the next /turn (SP11)
  let lastAutoModel = "" // our own setModel as "provider/id", told apart from a manual switch
  // SP13: whether the current turn is on group/mbridge; the first turn of a
  // session decides it from ctx.model, and our own setModel keeps it on
  let routed = false
  // SP8: the main session's tool results since the last /turn; mbridge no
  // longer sees Pi's requests, so Pi reports them (R3's tool rule)
  let tools: { calls: number; failures: number } | null = null
  let downShown = false

  pi.on("session_start", (event, ctx: ExtensionContext) => {
    const ids = idsFrom(
      () => ctx.sessionManager.getSessionId(),
      ctx.sessionManager.getHeader() as Record<string, unknown> | null,
    )
    session = ids.id
    parent = ids.parent
    // S12: reason stays "startup"; when no parent pointer shows up but Pi
    // reports a fork, mark the lineage and let the gateway match by words
    if (parent === null && (event as { reason?: string }).reason === "fork") {
      void client.lineage({ session, source: "pi-fork" })
    }
    parentSent = false
    void client.learnBudget() // SP10: how long a turn may wait
    lastTier = null
    lastPrompt = ""
    lastAnswer = null
    lastAutoModel = ""
    routed = false
    tools = null
  })

  pi.on("before_provider_headers", (event) => {
    // §6.8: the gateway reads this to stat the session's tools and spacing
    event.headers["X-Magpie-Session"] = session
  })

  // Compaction escalation: when compaction begins, if current tier is fast,
  // escalate immediately to balanced; if already balanced or performance, keep it.
  pi.on("session_before_compact", async (_event, ctx: ExtensionContext) => {
    if (!routed || session === "") return
    if (lastTier === "fast") {
      const target =
        ctx.modelRegistry.find(PROVIDER, "group/mb-balanced") ??
        ctx.modelRegistry.find(PROVIDER, "mb-balanced")
      if (target !== undefined) {
        lastAutoModel = modelKey(target)
        lastTier = "balanced"
        await pi.setModel(target)
      }
    }
  })

  pi.on("before_agent_start", async (event, ctx: ExtensionContext) => {
    const prompt = event.prompt ?? ""
    if (prompt === "" || session === "") return
    // SP13: this turn is routed when Pi's model is the routing group, or the
    // tier group an earlier routed turn of ours switched to
    const model = (ctx as { model?: ModelLite }).model
    routed = isRoutingModel(model) || (lastAutoModel !== "" && modelKey(model) === lastAutoModel)
    if (!routed) {
      lastTier = null
      return
    }
    lastPrompt = prompt
    const out = await learnedThenTurn({
      session,
      prompt,
      cwd: process.cwd(), // §4.1 project-level criteria
      parentSession: parent !== null && !parentSent ? parent : undefined,
      toolCalls: tools?.calls,
      toolFailures: tools?.failures,
      previousAnswer: lastAnswer ?? undefined, // SP11
    })
    tools = { calls: 0, failures: 0 }
    if (parent !== null) parentSent = true
    // mbridge down: the model stays as it is, and the status says why
    const status = (ctx as { ui?: { setStatus?: (k: string, t: string | undefined) => void } }).ui?.setStatus
    if (out === null) {
      if (!downShown) status?.("mbridge", "mbridge: 未运行")
      downShown = true
      return
    }
    if (downShown) status?.("mbridge", undefined)
    downShown = false
    if (out.tier === lastTier) return
    const target = ctx.modelRegistry.find(PROVIDER, out.group)
    if (target === undefined) return // tier group unknown to Pi: keep the model
    lastAutoModel = modelKey(target)
    lastTier = out.tier
    await pi.setModel(target)
  })

  // SP7 §3.5: after a routed main turn that did not run on performance,
  // ask the gateway to judge whether the answer left the request open.
  // Fire and forget: nothing here waits on the gateway.
  pi.on("agent_end", (event) => {
    const answer = lastAssistantText((event as { messages?: AgentMessageLite[] }).messages ?? [])
    // SP11: for the next /turn, whatever tier this turn was; none if it ended without one
    lastAnswer = finalAnswer((event as { messages?: AgentMessageLite[] }).messages ?? [])
    if (!routed || session === "" || lastTier === null || lastTier === "performance") return
    if (answer === null) return
    client.review({ session, prompt: lastPrompt, answer })
  })

  pi.on("model_select", (event) => {
    const e = event as { source: string; model: ModelLite & { id: string }; previousModel?: ModelLite }
    if (e.source === "restore") return // a resumed session reloading its model
    if (lastAutoModel !== "" && modelKey(e.model) === lastAutoModel) return // our own switch
    // SP13: leaving a routed turn's model is a manual switch, reported once;
    // the next turn reads its own mode from ctx.model. A session that never
    // routed chose its model in a settings file: nothing to report.
    const wasRouted = routed
    routed = false
    lastAutoModel = ""
    lastTier = null
    if (!wasRouted) return
    void client.feedback({
      session,
      kind: "manual_model_switch",
      value: `${e.previousModel?.id ?? ""}→${e.model?.id ?? ""}`,
    })
  })

  pi.on("tool_result", (event) => {
    const e = event as { isError?: boolean; content?: Array<{ type?: string; text?: string }> }
    if (!routed) return // SP13
    if (tools !== null) {
      tools.calls++
      if (e.isError === true) tools.failures++
    }
    if (e.isError === true) return
    for (const part of e.content ?? []) {
      if (typeof part.text !== "string") continue
      const m = PR_LINK.exec(part.text)
      if (m !== null) {
        void client.feedback({ session, kind: "pr_created", value: m[0] })
        return
      }
    }
  })

  pi.on("tool_call", async (event) => {
    const e = event as { toolName: string; input: Record<string, unknown> }
    if (!SUBAGENT_TOOLS.has(e.toolName)) return
    if (typeof e.input.model === "string") return // already chosen
    if (e.input.context === "fork") return // inherits the parent's context: gateway mode (S12)
    if (!routed || session === "") return // SP13
    const task = [e.input.task, e.input.prompt, e.input.description].find(
      (v): v is string => typeof v === "string" && v !== "",
    )
    if (task === undefined) return
    const out = await learnedThenTurn({ session, prompt: task, agent: "subagent", cwd: process.cwd() })
    if (out === null) return
    // out.group is the tier's real group id (mb-perf, not mb-performance)
    e.input.model = `${PROVIDER}/${out.group}`
  })
}
