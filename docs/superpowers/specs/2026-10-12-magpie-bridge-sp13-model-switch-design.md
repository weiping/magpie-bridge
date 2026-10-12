# SP13：选了 mbridge 组才选档

> 状态：作者 2026-10-12 确认（「先写 spec 再按 Codex、Claude Code、Pi 的顺序实现」）。

## 1. 问题

作者把 Claude Code 的用户级模型改成了 `group/auto-claude-sonnet-5-5`，在本项目里启动后，状态栏仍显示 `mbridge: fast · …`，子代理也还在被挑档。原因是三个客户端的插件只要装着就会向 mbridge 判档，不看用户这一轮选的是不是 `group/mbridge`：

| 客户端 | 主轮次 | 子代理 | 上报 |
| --- | --- | --- | --- |
| Claude Code | `turn.start` 无条件判档、显示状态；改写模型只认 `group/mbridge` | `agent.spawn` 无条件挑档 | 工具统计、PR 链接无条件 |
| Codex | `UserPromptSubmit` 已按 `model` 判断 | `PreToolUse` 不看模型 | PR 链接不看模型；每个非 mbridge 的轮次都报 `manual_model_switch` |
| Pi | `before_agent_start` 无条件判档，靠会话内的 `manualPinned` 排除 | `tool_call` 无条件挑档 | 无条件 |

用户要的是：用户级、项目级的模型设置里选了 `group/mbridge` 就选档，选了别的就不选，项目级优先。

## 2. 设计

**开关只有一个：这一轮实际用的模型是不是 `group/mbridge`。**

- 比较时去掉末尾的 `[…]`（magpie 给 1M 窗口写成 `group/mbridge[1m]`）。
- 实际用的模型是客户端自己合并好的结果：命令行 `--model`、会话里的 `/model`、项目级设置、用户级设置，优先级由客户端处理。插件不读、不解析任何配置文件。
- 逐轮判断，不按会话判断。会话中途 `/model` 切换，下一轮生效，不用重开。
- 关闭时对 mbridge 完全静默：不发 `/turn`，不改写模型，不显示状态，不发工具统计、PR 链接、复盘和反馈。
- 不加 `auto|on|off` 之类的配置项。开关只有一个来源；要整个停掉，用客户端自己的插件开关（`claude plugin disable`）。

**记在一个值里：本轮是否开启（mode）。** 判档、子代理、上报都读它，不各自再判断。

### 2.1 Codex

- `UserPromptSubmit` 的输入带 `model`（已有）。`model == group/mbridge`（去后缀）为开启。
- 钩子是一次一进程，其他钩子读不到这一轮的结论，所以 `UserPromptSubmit` 把结果写进 `$TMPDIR/mbridge-codex-<session>-mode.json`：`{session, routed, model}`。写法和现有的 prompt、answer 状态文件一样。
- `PreToolUse`（子代理）、`PostToolUse`（PR 链接）读这个文件：不是本会话的、读不出的、`routed` 为假的，都按关闭处理，静默放行。不依赖这两种输入里有没有 `model`。
- `Stop` 的复盘依赖 prompt 状态文件，它只在开启的轮次写。关闭的轮次要把上一轮留下的 prompt 状态文件删掉，不让它和这一轮的回答配对。回答的状态文件（SP11）不变。
- `manual_model_switch` 的上报条件收紧：只在**上一轮是开启、这一轮不是**时报一次（读上一次的 mode 文件）。会话一开始就是非 mbridge 的模型是配置选择，不报；连续多轮都是非 mbridge 的，不重复报。
- 代理（`internal/proxy`）本来就只改写 `model == group/mbridge` 的请求，不改。

### 2.2 Claude Code

- `turn.start` 的事件只有 `turnId` 和 `text`，没有模型，所以这时不能判断。改成：
  - `turn.start` 只记下 prompt，把本轮的判档结果和「已判档」标记清空，不向网关发任何请求。
  - 主会话本轮**第一个** `turn.step`：`e.model`（去后缀）是 `group/mbridge` 则开启，此时才发 `/turn`、写状态栏、记下档位，再改写模型；不是则关闭，把 mode 记为关闭，直接放行。
  - 同一轮后面的 `turn.step` 沿用这一轮的结论，不再判档。
- 判档发生的时刻从 `turn.start` 推迟到第一个 `turn.step`，等待时间的上限不变（SP10 的 `turn_budget_ms`）。
- `agent.spawn`：主会话本轮 mode 不是开启（关闭，或还没有任何主轮次）则原样放行。子代理的模型由客户端的 `ANTHROPIC_DEFAULT_*_MODEL` 别名决定，插件不碰。开启时行为不变。
- 子代理的 `turn.step` 本来就只改写 `group/mbridge`，不变。
- `tool.call` 的工具统计和 PR 链接、`turn.complete` 的复盘：只在本轮 mode 开启时统计和上报。`lastAnswer`（SP11）照旧每轮都记，它只是本地状态。
- `classic.PostModelSwitch`（用户用 `/model` 或选择器切模型）：只在 `from_model`（去后缀）是 `group/mbridge`、也就是**从 mbridge 切走**时报 `manual_model_switch`。从别的模型切到别的、或从别的切回 mbridge，都不报。
- `$.state` 多一项 `mode: 'on' | 'off' | null`（`null`：还没有主轮次），同步更新 `types/index.d.ts`。
- 插件版本 0.2.3 → 0.3.0。

### 2.3 Pi

- `before_agent_start` 读 `ctx.model`：`provider == "magpie"` 且 `id`（去后缀）是 `group/mbridge`，**或者** `id` 是 mbridge 自己上一次 `setModel` 设的档位组（`lastAutoModel`），为开启。第二个条件必须有：第一轮判档后 Pi 的模型已经是 `group/mb-fast` 之类，不算它，第二轮就会误判成关闭。
- 关闭时：不判档、不调用 `setModel`、不显示状态、`lastTier` 清空。
- `manualPinned` 不再是会话内一直有效的标记，改成逐轮读 `ctx.model`。`model_select` 事件里：
  - `source == "restore"`、模型是 `lastAutoModel`：不处理（现有逻辑）。
  - 手动切到 `group/mbridge`：回到开启，`lastTier` 清空，下一轮重新判档。
  - 手动切到别的模型：`manual_model_switch` 只在这之前是开启时报一次。
- 子代理（`tool_call`）、`tool_result` 的统计和 PR 链接、`agent_end` 的复盘：只在本轮开启时做。
- `lastAnswer`（SP11）照旧每轮都记。

### 2.4 项目级怎么用

- 用户级设成 `group/auto-claude-*`（或任何别的模型）：三个客户端都不选档。
- 某个项目要选档：在项目的设置里把模型设成 `group/mbridge`。
  - Claude Code：项目的 `.claude/settings.json` 写 `{"model": "group/mbridge[1m]"}`。
  - Codex：用 `codex -p mbridge` 启动，或者项目的 `.codex/config.toml` 写 `model = "group/mbridge"`。
  - Pi：项目的 `.pi/settings.json` 里设默认模型为 `magpie/group/mbridge`。
- 反过来，用户级是 `group/mbridge`、某个项目要关：项目级写别的模型。
- Claude Code 子代理用的是 `ANTHROPIC_DEFAULT_{OPUS,SONNET,HAIKU}_MODEL`，项目级的 `model` 管不到它们，要一致的话在项目的 `env` 里一起覆盖。

## 3. 不做

- 不读、不解析任何客户端的配置文件。
- 不改 mbridge 网关的 `/v1/bridge/*` 接口和 Go 里的路由。
- 不改 `mbridge router init`。
- 不加配置项。
- 不改 `internal/proxy`。

## 4. 测试

三个客户端各自覆盖同一组用例，行为必须一致：

1. 模型是 `group/mbridge` 时：发 `/turn`，改写模型，状态栏有档位。
2. 模型带 `[1m]` 后缀时同上。
3. 模型是别的（`group/auto-claude-*`、`group/mb-perf`、具体模型）：不发 `/turn`、不改写、没有状态栏、不发任何上报。
4. 会话中途 `/model` 在两者之间切：下一轮按新模型判断。
5. 子代理：主轮次开启时挑档（现有），关闭时放行。
6. 工具统计、PR 链接、复盘：关闭时不发。
7. `/turn` 失败（网关没起）时和现在一样：模型不动。

各自另加：

- **Codex**：mode 文件是别的会话的，或读不出：按关闭处理；关闭的轮次删掉上一轮的 prompt 状态；`manual_model_switch` 只在「开启 → 非开启」时报一次，连续关闭不重复报，一开始就关闭不报。
- **Claude Code**：同一轮的多个 `turn.step` 只判一次档；`turn.start` 不发 `/turn`；子代理的 `turn.step` 不影响主会话的 mode；还没有主轮次时 `agent.spawn` 放行。
- **Pi**：mbridge 自己 `setModel` 之后的下一轮仍算开启；手动切到别的再手动切回 `group/mbridge`，重新判档。

录制用的输入沿用现有的真实输入（`internal/harness/codex/codex_test.go`、`clients/claude-code/hooks/register.test.ts`、`clients/pi/test/extension.test.ts` 里的），不另编。

## 5. 顺序

Codex → Claude Code → Pi。Codex 已经有一半，先把「开关」的写法定下来；Pi 要处理 `lastAutoModel` 和 `manualPinned` 的关系，风险最高，放最后。每个客户端一个提交，各自带测试和 README 的开关说明。
