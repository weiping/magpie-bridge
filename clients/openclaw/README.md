# OpenClaw × mbridge（方案 A）

OpenClaw 插件：每轮用户话之前调用 `POST /v1/bridge/turn`，把模型改到 `group/mb-fast` / `group/mb-balanced` / `group/mb-perf` 之一。与 Claude Code / Pi 插件共用同一套 mbridge 规则与 `/status` 大盘（harness 记为 **`openclaw`**）。

## 前提

- 官方 [magpie](https://github.com/yetone/magpie) 在 `127.0.0.1:3425`
- `mbridge serve` 在 `127.0.0.1:3426`
- OpenClaw 里已配置 `models.providers.magpie`，且模型列表含三档组

## 启用选档（SP13）

把 **主模型** 设成路由组（不是 `mb-native`）：

```json
{
  "agents": {
    "defaults": {
      "model": {
        "primary": "magpie/group/mbridge"
      }
    }
  }
}
```

插件只在 primary 为 `group/mbridge`（可带 `[1m]` 后缀）时介入；失败时 **不改 model**（与 Pi 插件一致）。

## 安装

```bash
cd clients/openclaw
openclaw plugins install --link . --force
openclaw plugins enable mbridge-openclaw
```

在 `openclaw.json` 里允许会话 hook：

```json
{
  "plugins": {
    "entries": {
      "mbridge-openclaw": {
        "enabled": true,
        "hooks": { "allowConversationAccess": true },
        "config": {
          "mbridgeUrl": "http://127.0.0.1:3426",
          "providerId": "magpie",
          "routingModelId": "group/mbridge"
        }
      }
    }
  }
}
```

重启 Gateway 后发一条消息，在 `http://127.0.0.1:3426/status` 的「最近判档」里应出现 `openclaw · main`。

## 配置

| 字段 | 默认 | 说明 |
|------|------|------|
| `mbridgeUrl` | `http://127.0.0.1:3426` | 也可用 `MBRIDGE_URL` |
| `providerId` | `magpie` | OpenClaw provider id |
| `routingModelId` | `group/mbridge` | 仅当 session 用这个 routing 模型时才自动选档 |

## 后续（未做）

- `POST /v1/bridge/review` 轮末复核
- `after_tool_call` 失败计数 → R3-tools
- `manual_model_switch` feedback
- npm 发布 `@weiping/openclaw-mbridge`
