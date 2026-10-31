/** Minimal mbridge /v1/bridge client for OpenClaw (fail-safe). */

export type TurnRequest = {
  session: string;
  prompt: string;
  agent?: string;
  cwd?: string;
  toolCalls?: number;
  toolFailures?: number;
  previousAnswer?: string;
};

export type TurnResponse = {
  tier: string;
  group: string;
  reason?: string;
};

const DEFAULT_BUDGET_MS = 3500;
const PROBE_MS = 500;

export function clampBudget(ms: unknown): number | null {
  return typeof ms === "number" && Number.isFinite(ms)
    ? Math.min(8000, Math.max(1500, Math.round(ms)))
    : null;
}

export class MbridgeClient {
  private turnBudgetMs = DEFAULT_BUDGET_MS;
  private budgetLearned = false;

  constructor(private readonly base: string) {}

  async learnBudget(): Promise<void> {
    const ctrl = new AbortController();
    const t = setTimeout(() => ctrl.abort(), PROBE_MS);
    try {
      const res = await fetch(`${this.base}/v1/bridge/router`, {
        method: "GET",
        signal: ctrl.signal,
      });
      if (!res.ok) return;
      const j = (await res.json()) as { turn_budget_ms?: unknown };
      const b = clampBudget(j.turn_budget_ms);
      this.turnBudgetMs = b ?? DEFAULT_BUDGET_MS;
      this.budgetLearned = true;
    } catch {
      // keep default
    } finally {
      clearTimeout(t);
    }
  }

  async ensureBudget(): Promise<void> {
    if (!this.budgetLearned) await this.learnBudget();
  }

  async turn(req: TurnRequest): Promise<TurnResponse | null> {
    await this.ensureBudget();
    const body: Record<string, unknown> = {
      harness: "openclaw",
      session: req.session,
      prompt: req.prompt,
      agent: req.agent ?? "main",
      store_hint: false,
    };
    if (req.cwd) body.cwd = req.cwd;
    if (req.toolCalls !== undefined) body.tool_calls = req.toolCalls;
    if (req.toolFailures !== undefined) body.tool_failures = req.toolFailures;
    if (req.previousAnswer) body.previous_answer = cutPrevious(req.previousAnswer);

    const ctrl = new AbortController();
    const t = setTimeout(() => ctrl.abort(), this.turnBudgetMs);
    try {
      const res = await fetch(`${this.base}/v1/bridge/turn`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body),
        signal: ctrl.signal,
      });
      if (!res.ok) return null;
      const out = (await res.json()) as TurnResponse;
      if (typeof out.tier !== "string" || typeof out.group !== "string") return null;
      return out;
    } catch {
      return null;
    } finally {
      clearTimeout(t);
    }
  }
}

function cutPrevious(s: string, max = 6000): string {
  if (s.length <= max) return s;
  return s.slice(0, max);
}

export function normalizeBase(url: string): string {
  return url.replace(/\/+$/, "");
}
