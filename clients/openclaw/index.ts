import { definePluginEntry } from "openclaw/plugin-sdk/plugin-entry";
import { MbridgeClient, normalizeBase } from "./mbridge-client.js";
import { groupToModelId, isRoutingModel, modelKey, stripModelSuffix } from "./routing.js";

type PluginConfig = {
  mbridgeUrl?: string;
  providerId?: string;
  routingModelId?: string;
};

type SessionState = {
  routed: boolean;
  lastTier: string | null;
  lastAutoModel: string;
  tools: { calls: number; failures: number };
  lastPrompt: string;
  lastAnswer: string | null;
};

function freshState(): SessionState {
  return {
    routed: false,
    lastTier: null,
    lastAutoModel: "",
    tools: { calls: 0, failures: 0 },
    lastPrompt: "",
    lastAnswer: null,
  };
}

function sessionId(ctx: { sessionId?: string; sessionKey?: string }): string {
  return ctx.sessionId ?? ctx.sessionKey ?? "";
}

export default definePluginEntry({
  id: "mbridge-openclaw",
  name: "Magpie Bridge routing",
  description: "Per-turn tier routing via mbridge for OpenClaw (方案 A).",
  register(api) {
    const cfg = (api.pluginConfig ?? {}) as PluginConfig;
    const mbridgeUrl = normalizeBase(cfg.mbridgeUrl ?? process.env.MBRIDGE_URL ?? "http://127.0.0.1:3426");
    const magpieProvider = cfg.providerId ?? "magpie";
    const routingModelId = cfg.routingModelId ?? "group/mbridge";
    const client = new MbridgeClient(mbridgeUrl);
    const sessions = new Map<string, SessionState>();

    void client.learnBudget();

    function stateFor(ctx: { sessionId?: string; sessionKey?: string }): SessionState | null {
      const id = sessionId(ctx);
      if (!id) return null;
      let st = sessions.get(id);
      if (!st) {
        st = freshState();
        sessions.set(id, st);
      }
      return st;
    }

    api.on("before_model_resolve", async (event, ctx) => {
      const prompt = (event.prompt ?? "").trim();
      if (!prompt) return;

      const st = stateFor(ctx);
      const sid = sessionId(ctx);
      if (!st || !sid) return;

      const onRoutingGroup = isRoutingModel(
        ctx.modelProviderId,
        ctx.modelId,
        magpieProvider,
        routingModelId,
      );
      const holdingAuto =
        st.lastAutoModel !== "" &&
        modelKey(ctx.modelProviderId ?? "", ctx.modelId ?? "") === st.lastAutoModel;

      st.routed = onRoutingGroup || holdingAuto;
      if (!st.routed) {
        st.lastTier = null;
        return;
      }

      st.lastPrompt = prompt;
      const turnReq: Parameters<typeof client.turn>[0] = {
        session: sid,
        prompt,
        agent: ctx.agentId ?? "main",
        cwd: ctx.workspaceDir,
        previousAnswer: st.lastAnswer ?? undefined,
      };
      if (st.tools.calls > 0) {
        turnReq.toolCalls = st.tools.calls;
        turnReq.toolFailures = st.tools.failures;
      }
      const out = await client.turn(turnReq);
      st.tools = { calls: 0, failures: 0 };

      if (!out) return;

      const modelId = groupToModelId(out.group);
      if (out.tier === st.lastTier && stripModelSuffix(ctx.modelId ?? "") === stripModelSuffix(modelId)) {
        return;
      }

      st.lastTier = out.tier;
      st.lastAutoModel = modelKey(magpieProvider, modelId);
      return {
        providerOverride: magpieProvider,
        modelOverride: modelId,
      };
    });

    api.on("after_tool_call", (_event, ctx) => {
      const st = stateFor(ctx);
      if (!st?.routed) return;
      st.tools.calls += 1;
      // OpenClaw after_tool_call does not always expose failure; increment failures only when result signals error.
    });

    api.on("agent_end", (event, ctx) => {
      const st = stateFor(ctx);
      if (!st?.routed || !st.lastPrompt) return;
      const messages = (event as { messages?: unknown[] }).messages;
      if (!Array.isArray(messages)) return;
      st.lastAnswer = lastAssistantText(messages);
    });
  },
});

type MsgLite = { role?: string; content?: unknown; stopReason?: string };

function lastAssistantText(messages: unknown[]): string | null {
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i] as MsgLite;
    if (m?.role !== "assistant") continue;
    if (m.stopReason === "aborted" || m.stopReason === "error") return null;
    if (typeof m.content === "string") return m.content;
    if (Array.isArray(m.content)) {
      const text = m.content
        .filter((p): p is { type?: string; text?: string } => typeof p === "object" && p !== null)
        .filter((p) => p.type === "text" && typeof p.text === "string")
        .map((p) => p.text as string)
        .join("");
      return text === "" ? null : text;
    }
  }
  return null;
}
