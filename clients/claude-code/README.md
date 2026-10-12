# magpie-bridge (Claude Code)

Route every Claude Code turn across mbridge's model tiers — `fast` for
questions and small changes, `balanced` for ordinary work, `performance`
for hard, multi-file or unknown-cause work — decided per turn by the
mbridge gateway's classifier.

## What it does

- `turn.start`: keeps the prompt. It does not ask the gateway yet: the
  model of the turn is not known here.
- `turn.step`: at the turn's first main step the model is known. On
  `group/mbridge` (a trailing `[1m]` is ignored) the plugin sends the prompt
  to the gateway (`POST /v1/bridge/turn`), records the tier it picks and
  rewrites the request to this turn's tier group (`group/mb-fast` /
  `mb-balanced` / `mb-perf`). It waits as long as mbridge's `turn_budget_ms`
  (`classify_timeout_ms` + 500, 1500–8000 ms; 1500 ms when mbridge doesn't
  say), learned from `GET /v1/bridge/router`. On any other model — a model
  you pinned with `/model`, a tier group a subagent already chose — the
  plugin does nothing.
- `agent.spawn`: on a routed turn, picks a tier for subagents started from
  scratch (type table first, then the classifier); agents that inherit the
  main model are pinned to the main session's tier at the moment they
  appear.
- On a routed turn, reports tool-call stats and PR links from Bash output.
  Leaving `group/mbridge` with `/model` is reported once as a manual switch.

**Routing is decided per turn by the model alone.** Claude Code merges its
settings (`--model`, `/model`, the project's `.claude/settings.json`, the
user's `~/.claude/settings.json`), and the plugin reads only the result. Set
`"model": "group/mbridge[1m]"` in a project to route it, or any other model
to leave it alone; the project wins over the user. Subagents follow
`ANTHROPIC_DEFAULT_{OPUS,SONNET,HAIKU}_MODEL`, which the project's `model`
does not change: override them in the project's `env` too when they should
match. With the main session off, the plugin never touches a subagent.

If the gateway is down, or any call fails, the mod gets out of the way:
requests go out as `group/mbridge` and the gateway's own fallback mode
routes them (or they fail like any other request would).

## Requirements

- Claude Code **v2.1.287 or newer** (mod hooks).
- Official magpie running (its gateway on `127.0.0.1:3425`), and mbridge
  running (`mbridge service install`, or `mbridge serve`) on this machine.
- Routing groups and `router.json` set up (`mbridge router init`).

Not required: Node, Python, or `mbridge` on `PATH` — the mod talks to
mbridge over HTTP only. Claude Code's own requests go straight to magpie,
the mod switching the model to `group/mb-<tier>` each turn.

## Install

```sh
claude plugin marketplace add weiping/magpie-bridge
claude plugin install magpie-bridge@magpie-bridge
```

The gateway URL defaults to `http://127.0.0.1:3426` (mbridge); change it in the
plugin's `gateway_url` setting if mbridge listens elsewhere.

## Known limitations

- **Agent SDK runs don't load this mod** on macOS: the SDK's local spawn
  fails with `errno -88` before Claude Code starts (observed with SDK
  0.3.289 / CLI 2.1.289). SDK-driven sessions fall back to the gateway's
  own routing. Under investigation.
- `classic.SessionStart` (used to detect `/fork` / `/branch` sessions)
  does not fire in `-p` mode; derived-session tier inheritance applies to
  interactive sessions.
- `--safe-mode`, `allowManagedModsOnly`, and org policies that disable
  mods mean this mod never loads; requests then route via the gateway's
  fallback mode.

## Development

```sh
claude plugin validate --strict clients/claude-code
claude plugin test clients/claude-code
```
