# 鹊桥 Magpie Bridge

给编码 Agent 用的模型路由器：在 Agent 的 harness 里判断每一轮任务有多难，在本地网关里把请求派给合适的模型档位。简单的提问交给便宜的快模型，跨文件改动和难查的 bug 交给最强的模型。

Magpie Bridge 和官方 [magpie](https://github.com/yetone/magpie) 并排运行：magpie 是本地模型网关，Magpie Bridge 是它旁边的路由器，命令叫 `mbridge`，后台进程 `mbridge serve` 默认监听 `127.0.0.1:3426`。中文名“鹊桥”，英文名是它的直译：喜鹊（magpie）搭的桥，连起 Agent 的 harness 和模型网关。Magpie Bridge 不含、也不修改 magpie 的代码，只通过 magpie 的 HTTP 接口、命令行和用量 CSV 与它协作：先装官方 magpie，再装 Magpie Bridge。

## 要做什么

- **事前选档**：每一轮用户发话时，插件把用户原话、计划模式、Agent 和子代理类型交给分类器（默认用 TypeSafe 的 [Jev](https://docs.typesafe.ai/introduction)），选出 `fast`、`balanced`、`performance` 三档之一。
- **事后升档**：用户说上一轮不对，或者上一轮工具调用失败过半，下一轮自动升一档。
- **事后复核**（可选，默认关闭）：轮末把这一轮的请求和最终回复交给分类器判「到底有没有解决」，命中就下一轮升档；先把 `review.mode` 设 `shadow` 攒样本、用 `mbridge router calibrate` 看阈值站不站得住，再切 `act`。开启后回复会发给你配的分类器厂商，所以默认关。
- **子代理单独选档**：从零开始的子代理单独选档；继承父会话上下文的 fork 子代理跟随父会话的档位，保住 prompt cache。
- **档位与模型解耦**：每一档是网关里的一个路由组，组内有跨厂商的失败转移成员。换模型只改配置。
- **失败安全**：分类器、hook、网关任何一环出错，请求照常完成，只是少了路由。
- **自带验收**：按会话分组做线上 A/B，统计成本、合并 PR 的比例和手动换模型的次数；`mbridge router calibrate` 另外用「下一轮的不满 / 手动升档 / 工具失败」当标签，给出各分数分段的选低率与建议阈值。

## 组件

| 组件 | 用于 | 状态 |
| --- | --- | --- |
| 守护进程（`mbridge serve`）：`/v1/bridge/*` 与 Codex、网关模式的代理（`internal/router/`、`internal/proxy/`） | 所有 Agent | 默认监听 3426 |
| Claude Code 插件 `magpie-bridge`（`clients/claude-code/`） | Claude Code | 0.3.3，默认连 3426 |
| Pi 包 `@weiping/pi-magpie-bridge`（`clients/pi/`） | Pi | 0.3.1，自报工具统计 |
| Codex 插件 `magpie-bridge-codex`（`clients/codex/`） | Codex | 0.2.1，配合 `codex -p mbridge` |
| 状态页 `http://127.0.0.1:3426/status`（mbridge 自己服务） | 所有 Agent | 进程、生效中的配置、正在选档的会话、分类器、分档统计 |
| magpie 页面插件 `clients/magpie-page/` | magpie 窗口 | magpie 记下的用量速览 |
| 报表、复核与校准（`mbridge router report`、`calibrate`） | 所有 Agent | 成本取 magpie 的 `usage --csv` |

## 快速开始

1. **先装官方 magpie**（[yetone/magpie](https://github.com/yetone/magpie)），让它的网关跑在默认的 `127.0.0.1:3425`，加好厂商（分类器 Jev 也作为一个厂商加进去）：

   ```sh
   curl -fsSL https://usemagpie.ai/install.sh | sh   # macOS、Linux；Windows 到 usemagpie.ai 下载 App
   magpie autostart on                               # 有桌面时登录自启；服务器上用 magpie serve
   magpie provider add deepseek sk-…
   magpie provider add moonshot-cn sk-…              # Kimi 国内站；国际站用 moonshot
   magpie provider add typesafe <api-key>            # 分类器 Jev
   ```

   订阅类账号用 `magpie accounts add <agent>` 或 magpie App 登录。

2. **再装 mbridge**（下载经 SHA-256 校验，装进 `~/.local/bin`，并注册登录时启动的 mbridge）：

   ```sh
   curl -fsSL https://raw.githubusercontent.com/weiping/magpie-bridge/main/install.sh | sh
   ```

   Windows（PowerShell）：

   ```powershell
   irm https://raw.githubusercontent.com/weiping/magpie-bridge/main/install.ps1 | iex
   ```

   指定版本或目录：`… | sh -s -- --version v0.2.0 --bin-dir ~/bin`；不想开机启动加 `--no-service`，之后手动 `mbridge serve`。升级用 `mbridge update`。

3. **建档位并接上 Agent**：

   ```sh
   mbridge router init --preset cn --groups-only   # 在 magpie 里建四个路由组，写 router.json（还有 frontier、anthropic 预设）
   magpie claude group/mbridge                     # Claude Code 的 base URL 和模型指到 magpie
   cd ~/code/your-project
   mbridge router init --preset cn                 # 接好 Claude Code（本项目）、Codex 和 Pi
   mbridge status                                  # magpie、mbridge、分组是否齐全，最近的决策
   mbridge router check                            # 分组在不在、上下文窗口够不够（加 --yes 再发真实请求，会计费）
   ```

   `magpie claude group/mbridge` 放在两次 init 之间：`mbridge` 组建好后它会写成带 1M 窗口的 `group/mbridge[1m]`，第二次 init 看到它就不再往项目里写 `ANTHROPIC_MODEL`。Claude Code 的别名映射写在项目的 `.claude/settings.local.json`，每个要用的项目各跑一次。

4. **装插件**：

   ```sh
   # Claude Code（需要 ≥ v2.1.287）
   claude plugin marketplace add weiping/magpie-bridge
   claude plugin install magpie-bridge@magpie-bridge        # 默认连 http://127.0.0.1:3426

   # Codex（需要 mbridge 在 PATH 里，装完在 /hooks 里信任 hook）
   codex plugin marketplace add weiping/magpie-bridge --sparse .agents --sparse clients/codex   # 只取插件那两个目录：完整克隆约 96 MB，Codex 30 秒内拉不完会超时
   codex plugin add magpie-bridge-codex@magpie-bridge   # 以后升级：codex plugin marketplace upgrade magpie-bridge，再执行这一行
   codex -p mbridge                                     # 用 mbridge 的 profile 启动，请求经 mbridge 选档

   # Pi
   pi install npm:@weiping/pi-magpie-bridge                   # 升级：pi update
   ```

Claude Code 和 Pi 的请求直接发给 magpie，插件逐轮把模型切到 `group/mb-<档>`；Codex 的请求先到 mbridge，代理按轮把 `group/mbridge` 改写成某一档再转给 magpie。没有插件的 Agent 把 base URL 设成 `http://127.0.0.1:3426/v1`、模型设成 `group/mbridge`，就是网关模式。

**选了 `group/mbridge` 才选档（SP13）**。三个插件逐轮只看这一轮实际用的模型：是 `group/mbridge`（末尾的 `[1m]` 不算）就判档、改写、给子代理挑档、上报工具统计和 PR 链接；是别的（`group/auto-claude-*`、`group/mb-perf`、任何具体模型）就对 mbridge 完全静默。模型是客户端把命令行、`/model`、项目设置、用户设置合并之后的结果（项目优先），插件不读任何配置文件。所以用户级选 `group/auto-claude-*`、某个项目写 `group/mbridge`，这个项目就选档，反之亦然。

| 客户端 | 项目里怎么写 |
| --- | --- |
| Claude Code | `.claude/settings.json`：`{"model": "group/mbridge[1m]"}`；子代理跑在 `ANTHROPIC_DEFAULT_{OPUS,SONNET,HAIKU}_MODEL` 上，要一致的话在项目的 `env` 里一起覆盖 |
| Codex | `codex -p mbridge`，或项目的 `.codex/config.toml` 写 `model = "group/mbridge"` |
| Pi | 项目的 `.pi/settings.json`：`defaultProvider` 写 `magpie`、`defaultModel` 写 `group/mbridge`（项目要先被 Pi 信任，这个文件才会加载） |

其他命令：

```sh
mbridge router calibrate              # 三个分数各自的选低率与建议阈值（先跑 shadow 攒样本）
mbridge router report --since 14d     # 线上实验报表：成本、合并率、升档来源、未升档轮次的选低率
mbridge service install|uninstall|status
mbridge version                       # mbridge 和 magpie 的版本
```

## 配置

mbridge 的路由配置有两处：magpie 里的四个路由组（存在 magpie 自己的配置里），和 `~/.config/magpie-bridge/router.json`。两者都由 `mbridge router init --preset <frontier|anthropic|cn>` 生成，之后直接改文件即可。

### 路由组

| 组 | 用途 | 路由策略 |
| --- | --- | --- |
| `mb-fast` | 快速档候选组 | `usage`（优先使用剩余额度最多的账号/Key） |
| `mb-balanced` / `mb-native-balanced` | 均衡档候选组 | `pace`（按额度周窗口的恢复节奏分配） |
| `mb-perf` | 高性能候选组，成员按能力从强到弱排列 | `order`（依次尝试，强模型在前） |
| `mbridge` / `mb-native` | 档位路由组；`mbridge` 由 mbridge 按轮改写档位，`mb-native` 由 magpie 原生规则选档 | `order` |

#### 换档位组里的模型

每档用哪些模型，由 magpie 里这三个档位组的成员决定，直接用 magpie 的命令改。mbridge 只认 `mb-fast` 这些分组名，所以改完马上生效，不用重启 mbridge，也不用重跑 `router init`。

```sh
magpie models                         # 先查可用的模型名（provider/model）
magpie group mb-fast                  # 看一个组现在的成员和 routing
magpie group set mb-fast models=cursor/composer-2.5,zhipu/glm-5.3-flash:high routing=usage
magpie group set mb-balanced models=glm/glm-5.3:high,minimax-cn/MiniMax-M3 routing=pace
magpie group set mb-perf models=anthropic/claude-opus-5-5:high,deepseek/deepseek-v4-pro:max routing=order
magpie group set mb-perf models+=anthropic/claude-opus-5-5:high routing=order   # 追加成员时显式保留策略
magpie group set mb-perf models-=zhipu/glm-5.3:max routing=order                # 移除成员时也显式保留策略
magpie groups                         # 确认成员顺序和策略
mbridge router check                  # 检查分组是否齐全、上下文窗口够不够
```

- `models=` 会用给出的列表整个替换原来的成员。列表有顺序：第一个是主模型，后面的依次是失败转移。手工重设/替换档位组时也显式带上对应的 `routing=`：`mb-fast=usage`、`mb-balanced=pace`、`mb-perf=order`；性能组成员要按能力从强到弱排列。
- 成员可以带 `:low`、`:medium`、`:high` 这类后缀，指定推理强度。
- magpie 不认识的模型名会被拒绝（提示用 `magpie models` 查）。
- `mbridge` 这个路由组的成员就是三个档位组（`group/mb-balanced`、`group/mb-perf`、`group/mb-fast`），不要改成具体模型。
- 想改的是「什么样的问题进哪一档」，那是 `router.json` 里各档的 `criteria`，见下一节。

`router init` 的三套预设只决定第一次建分组时的成员。每档按顺序列几个候选模型，只有 magpie 里有厂商提供的才写进组（第一个是主模型，其余做失败转移），没有的跳过并提示；某档一个候选都没有时，先借用相邻档的成员并提示你换掉。之后分组归你管：再次执行 `router init` 不会改动它们，除非加 `--force`。

| 预设 | fast | balanced | performance |
| --- | --- | --- | --- |
| `cn` | `deepseek-flash`、`glm-5.3-flash:high`、`qwen3.8-flash:low` | `glm-5.3:high`、`kimi-k2.7-code`、`deepseek-v4-pro:high` | `kimi-k3:high`、`deepseek-v4-pro:max`、`glm-5.3:max`、`qwen3.8-max:xhigh` |
| `frontier` | `deepseek-flash`、`glm-5.3-flash:high`、`gemini-3.8-flash:low`、`gpt-6-luna:low` | `gpt-6.1-sol:medium`、`claude-sonnet-5-5:medium` | `gpt-6-astra:low`、`claude-opus-5-5:high` |
| `anthropic` | `anthropic/claude-haiku-5-5`、OpenRouter 同款 | `anthropic/claude-sonnet-5-5:medium`、OpenRouter 同款 | `anthropic/claude-opus-5-5:high`、OpenRouter 同款 |

`cn` 和 `frontier` 的候选不写厂商，哪个厂商提供该模型就用哪个（如 `moonshot`、`moonshot-cn`、`qwen-cn` 都提供 `kimi-k3`）。

`mb-fast`、`mb-balanced`、`mb-perf`、`mbridge` 这四个分组名归 Magpie Bridge 使用，别拿它们做别的用途。再次执行 `mbridge router init` 会保留已有的同名分组、`router.json`，以及当前项目 `.claude/settings.local.json` 里你已设过的 `ANTHROPIC_*` 值，只补上缺的部分，并接好各 Agent；加 `--force` 才会用预设覆盖它们。你的 Claude Code `model` 设置已经是 `group/mbridge`（含 `[1m]`）时，init 不写 `ANTHROPIC_MODEL`，以免环境变量盖过它。

### `router.json`

`mbridge router init` 生成的默认值如下，字段都可以改：

```json
{
  "version": 1,
  "router_group": "mbridge",
  "tiers": {
    "fast":        { "group": "mb-fast",     "claude_alias": "haiku",  "criteria": "…" },
    "balanced":    { "group": "mb-balanced", "claude_alias": "sonnet", "criteria": "…" },
    "performance": { "group": "mb-perf",     "claude_alias": "opus",   "criteria": "…" }
  },
  "default_tier": "balanced",
  "classifier": "local",
  "classify_timeout_ms": 2500,
  "thresholds": {
    "tier_min": 0.4,
    "dissatisfied_min": 0.7,
    "review_min": 0.7,
    "review_confidence_min": 0.5
  },
  "review": { "mode": "off", "timeout_ms": 5000, "max_answer_chars": 6000 },
  "escalate_turns": 2,
  "cache_ttl_seconds": 300,
  "fixed_agents": { "Explore": "fast", "Plan": "performance" },
  "experiment": { "enabled": false, "router_percent": 50, "control_tier": "performance", "salt": "…" }
}
```

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `router_group` | `mbridge` | 路由组名，Agent 的模型指到 `group/<这个名字>` |
| `listen` | `127.0.0.1:3426` | mbridge 的监听地址 |
| `magpie_url` | `http://127.0.0.1:3425` | 官方 magpie 网关的地址 |
| `tiers.<档>.group` | — | 该档对应的网关路由组 |
| `tiers.<档>.claude_alias` | — | Claude Code 侧的别名映射（`haiku`/`sonnet`/`opus`） |
| `tiers.<档>.criteria` | — | 交给分类器的选档标准（自然语言） |
| `default_tier` | `balanced` | 分类失败或不可信时的落档（规则 R8） |
| `classifier` | `local` | 分类器：`typesafe/jev-latest`，或任意 `provider/model`。普通模型用结构化输出给出档位置信度与不满分数（实测 Kimi 支持）；厂商拒绝 `response_format` 时（如 DeepSeek 返回 400）退回只回编号的提示词，此时置信度按回答是否合法记 1/0，事件里的 `classifier` 标 `#plain` |
| `classify_timeout_ms` | `2500` | 分类超时；超时该轮落默认档，下一轮补偿。三个 Agent 插件等 `/turn` 的时间随它走：本值加 500ms，限定在 1500–8000ms 之间 |
| `thresholds.tier_min` | `0.4` | 分类结果的最低置信度（R5） |
| `thresholds.dissatisfied_min` | `0.7` | 「用户在说上一轮不对」的判定阈值（R3 升档） |
| `thresholds.review_min` | `0.7` | 轮末复核判「没解决」的阈值（R3 升档，`review.mode` 非 `off` 时才有用） |
| `thresholds.review_confidence_min` | `0.5` | 复核自身的置信度下限，低于它不算命中 |
| `thresholds.overrides` | — | 按 harness 或 agent 覆盖上面几个阈值，例如 `[{"harness":"codex","tier_min":0.5}]`；同时写 harness 和 agent 的条目优先于只写一个的 |
| `review.mode` | `off` | 轮末复核：`off` 不问；`shadow` 只记 `would_review` 不影响路由；`act` 命中 R3 升档 |
| `review.timeout_ms` | `5000` | 后台复核的超时（不影响本轮回复，回复早已发出） |
| `review.max_answer_chars` | `6000` | 送审的回复上限，超出保留前 2000 和后 4000 字 |
| `escalate_turns` | `2` | 升档后保持的轮数（R4） |
| `cache_ttl_seconds` | `300` | 降档迟滞：距上次请求超过它才允许立即降档（R6） |
| `fixed_agents` | 见下 | 固定档位的子代理类型（R1）：`Explore`、`statusline-setup`、`claude-code-guide`、`explorer` → `fast`；`Plan` → `performance` |
| `experiment.enabled` | `false` | 开启线上 A/B |
| `experiment.router_percent` | `50` | 走路由的会话比例（0–100，其余走对照组） |
| `experiment.control_tier` | `performance` | 对照组钉死的档位 |
| `experiment.salt` | init 随机生成 | 分组哈希的盐，**写死后不要改**，否则实验前后不可比 |

配置无效时路由自动降级：mbridge 照常启动，代理只做透传，`group/mbridge` 由 magpie 按顺序落到 balanced。`mbridge router status` 会报告具体的配置错误。

### 分类器：TypeSafe Jev

分类器默认是 `"local"`——这是个占位值，要换成实际的 `provider/model`（如 `ollama/qwen3-4b`），否则分类一直失败，路由只能走默认档。推荐的分类器是 TypeSafe 的 [Jev](https://docs.typesafe.ai/introduction)：单请求返回档位选择和置信度，以及「用户在说上一轮不对」的概率，不用自己拼提示词。

```sh
# 1. 加 TypeSafe provider（key 从 https://console.typesafe.ai/keys 拿）
magpie provider add typesafe <api-key>
```

```jsonc
// 2. ~/.config/magpie-bridge/router.json
{
  "classifier": "typesafe/jev-latest",
  "classify_timeout_ms": 4000
}
```

不想直连 TypeSafe 还有两个中继预设：`vercel-jev`（Vercel AI Gateway，分类器写 `vercel-jev/typesafe-ai/jev`）和 `cloudflare-jev`（Cloudflare Workers AI，写 `cloudflare-jev/typesafe/jev`）。

时延提示：实测本机到 TypeSafe 的请求热的时候 300–450ms，空闲十几二十分钟后的第一轮要 1.5–2.4s（冷启动）。默认超时 2500ms 偶尔压线，建议设 4000。超时时该轮落默认档、下一轮自动补偿。如果网络到 TypeSafe 不稳定，用本地小模型更稳。

### 轮末复核与置信度校准

默认 `"review": {"mode": "off"}`——**默认关闭**，因为开启后每轮的回复会被发给分类器厂商（走你已配置的 classifier 渠道）。想用它，按这三步走：

1. `"mode": "shadow"`：复核照常发出，但只把 `would_review` 记进 `~/.config/magpie-bridge/router.jsonl`，路由行为一点不变。跑几天，攒样本。
2. `mbridge router calibrate`：看三个分数（档位置信度、不满分数、复核分数）各自的选低率和建议阈值。样本不足 30 的分段会标「样本不足」。标签只反映**用户表达出来的不满**，所以顶部有提示；要更准的判断用 `mbridge router calibrate --csv` 导出逐轮明细人工核对。
3. 数字站得住再切 `"mode": "act"`：复核命中时下一轮按 R3 升档，`router.jsonl` 的 decide 事件里 reason 会是 `R3-review`。

复核在后台跑，本轮回复不等它；结果晚到（下一轮已经开始）就丢弃，不会配错轮次。

### 项目级覆盖

在项目根目录放 `.mbridge/router.json`，只能覆盖各档的 `criteria`（比如告诉分类器「这个仓库的改动大多是跨服务的」），其余字段一律忽略：

```json
{
  "tiers": {
    "balanced": { "criteria": "本项目是普通前端仓库，单文件改动居多" }
  }
}
```

### 环境变量

| 变量 | 说明 |
| --- | --- |
| `MBRIDGE_URL` | 插件和 hook 访问 mbridge 的地址，默认 `http://127.0.0.1:3426`。Codex 的 hook 子进程不继承自定义环境变量，请让 mbridge 跑在默认端口 |

Claude Code 插件的地址另有 `gateway_url` 设置（`claude plugin install magpie-bridge@magpie-bridge --config gateway_url=…`）。

## 只用 magpie 原生规则选档（`mb-native`）

只需要逐轮按难度选档，不需要升档、降档迟滞、A/B 实验和校准时，可以不经过 mbridge，直接用 magpie 路由组自带的「按意图选成员」规则。Agent 直连 magpie（`127.0.0.1:3425`），模型设成 `group/mb-native`，不用 `mbridge serve`。

### 原生规则怎么选档

1. **只在新一轮开始时判一次。** 最后一条用户消息有文字、没有工具结果，就是新一轮；同一轮里的工具回合沿用这次的决定，不再问分类器。
2. **分类器只看最后一条用户消息。** 去掉 `<system-reminder>…</system-reminder>`，超过 4000 个字符时保留前 3000 和后 1000；图片不送；更早的对话不送，只额外带上上一轮被判成的意图。
3. **先收集候选意图。** 规则从上到下看，其他条件（长度、图片、推理、Agent 等）成立的规则，把它的意图收进来；遇到第一条没有意图的规则就停，它直接命中。收不到意图就不问分类器。
4. **分类器给出一个意图。**
   - 用 Jev 时，每组新的意图先多问一次，判断它们是「档位」（每条消息都在其中一档）还是「话题」（消息可以都不是），结果缓存在内存里。
   - 档位：Jev 总会选一档；置信度低于 0.4 且有上一轮时，沿用上一轮的档位。
   - 话题：选了「none of these」，或置信度低于 0.4，就没有意图命中。
   - 用普通聊天模型当分类器时，它按编号回答，`0` 表示都不是。
5. **失败只丢掉路由，不丢掉请求。** 超时 8 秒；失败后该分类器 30 秒内不再被问；分类器答不出来，就当没有意图命中。同一个分类器、同一组意图、同一条消息的答案在内存里缓存 10 分钟。
6. **规则从上到下匹配，第一条命中的赢。** 它指向的成员排到最前面，其余成员按原来的顺序做故障转移；规则优先于 `stays`。对话长到当前模型窗口的 95% 时，只会换到窗口更大的模型。

置信度阈值 0.4 是写死的，不能改。

### 搭建

先把 `router.json` 的 `classifier` 设成 magpie 能问的模型（见上文「分类器：TypeSafe Jev」），再让 init 建组：

```sh
mbridge router init --preset cn --groups-only --native   # 预设名照你当初 init 的写；已有的组都保留
magpie group mb-native                                   # 看成员、规则和分类器
```

`--native` 在 `mb-fast`、`mb-balanced`、`mb-perf` 已经在 magpie 里的前提下，另外建两个组，不改前三个：

| 组 | 成员 |
| --- | --- |
| `mb-native-balanced` | `mb-balanced` 当前的成员，使用 `routing=pace`；与 `mb-perf` 共有的，带上 `:medium` 之类的后缀（原因见下一节） |
| `mb-native` | `group/mb-perf`、`group/mb-native-balanced`、`group/mb-fast`，`routing=order`、`stays=turn` |

`mb-native` 上有四条规则：fast、balanced、performance 三条按意图选，意图文本取 `router.json` 里各档的 `criteria`；第四条没有意图（`tokens=1`），分类失败或没有意图命中时落到 `default_tier` 那一档。

- `classifier` 还是占位值 `local` 时，init 只提示，不建原生组，其余照常完成；设好分类器再跑一次即可。
- 已在 magpie 里的原生组原样保留；`mb-native` 在但一条规则都没有时，只补规则。
- `--force` 不用来重建原生组：它会把四个档位组按预设放回去，并把 `router.json` 写回默认值（分类器又成 `local`）。要重建，先 `magpie group rm mb-native mb-native-balanced`，再跑 `mbridge router init --preset … --groups-only --native`。改了 `criteria` 或档位组的成员之后，规则和 `mb-native-balanced` 不会自动跟着变，也这样重建。

规则里的分类器可以是任何 magpie 有的模型，建议用小而快、不带推理的；它的调用会作为 magpie 自己的请求出现在 Usage 里。

不想用 init 时，等价的手动命令（成员换成你自己 `mb-balanced` 里的模型）：

```sh
magpie group add mb-native-balanced models=zhipu/glm-5.3,deepseek/deepseek-v4-pro:medium,kimi-code-cn/k3:medium,… routing=pace
magpie group add mb-native models=group/mb-perf,group/mb-native-balanced,group/mb-fast routing=order stays=turn
magpie group rule add mb-native use=group/mb-fast classifier=typesafe/jev-latest intent="<fast 的 criteria>"
magpie group rule add mb-native use=group/mb-native-balanced intent="<balanced 的 criteria>"
magpie group rule add mb-native use=group/mb-perf intent="<performance 的 criteria>"
magpie group rule add mb-native use=group/mb-native-balanced tokens=1
```

### 嵌套组的成员去重

外层组把子组展开成成员时，同一个模型只保留在**最先出现**的子组里。`mb-balanced` 和 `mb-perf` 常常共有几个模型：外层把 balanced 排在前面，这些模型就归 balanced，规则指向 `mb-perf` 时，它的第一个成员会变成第二个，而不是你排在第一位的那个。

`init --native` 用两个办法避开，不需要自己动手：

- **外层顺序**：perf 排最前，它的成员原样保留。
- **推理后缀**：同一个模型带不同后缀算两个成员。`mb-native-balanced` 里与 perf 共有的模型，init 会按 `medium`、`low`、`high`、`xhigh` 的顺序挑一个没被占用的后缀加上（`:fast` 标记保留，`:free` 这类属于模型 id 的结尾不当后缀），并把改了哪些打印出来。代价是这些成员的推理强度被固定，不再跟随 Agent 的请求。

`mb-fast` 被两边复用，init 不改它；它的成员与 perf 或 balanced 共有时，init 会提示那个成员在 `mb-native` 里只算在前面的组里。以后往几个档位组里加同一个模型，会再遇到这件事，重建原生组就会重新处理。

### 和 mbridge 的差别

| 能力 | mbridge | 原生规则 |
| --- | --- | --- |
| 每轮按难度选 fast / balanced / performance | 是 | 是 |
| 置信度阈值 | `thresholds.tier_min`，默认 0.4 | 固定 0.4 |
| 「继续」「按这个做」这类追问沿用上一轮的档位 | 是 | 是 |
| 降档迟滞（`cache_ttl_seconds`，R6） | 有：距上次请求不到 300 秒不降档 | 没有：每轮按分类结果换档，换模型会让提示缓存失效 |
| 用户说上一轮不对时升档（R3、R4）、轮末复核 | 有 | 没有 |
| 子代理类型固定档位（`fixed_agents`） | 有 | 可用规则的 `agents=` 条件近似 |
| 分类依据 | 用户原话、上一轮回答、计划模式、Agent 和子代理类型 | 最后一条用户消息和上一轮的意图 |
| A/B 实验、报表、校准 | 有 | 没有 |
| 需要 `mbridge serve` | 是 | 否 |

### 在 Codex 里用

用原生组时，Codex **不用 `-p mbridge`**，也不需要 `magpie-bridge-codex` 插件。`-p mbridge` 让请求经 mbridge 的代理（3426）；原生组靠 magpie 自己路由，Codex 只要走默认的 magpie provider（`model_provider = "magpie"`，指向 3425）并把模型设成 `group/mb-native`：

```sh
codex -m group/mb-native             # 单次指定
# 或在 ~/.codex/config.toml 里写 model = "group/mb-native"
```

- Codex 的 `<environment_context>` 不会影响选档：它单独成一条用户消息（TUI），或和提示放在同一条消息里（`codex exec`），两种请求形状都按提示本身分类。
- 已装 `magpie-bridge-codex` 插件的话，**不要在 `/hooks` 里信任它的 hook**。模型不是 `group/mbridge` 时，hook 会把每次提示上报成 `manual_model_switch`，并给不带模型的 `spawn_agent` 钉上 `group/mb-<档>`，绕过原生规则。默认配置下这些 hook 不会触发（它们的信任记录目前只写在 `~/.codex/mbridge.config.toml` 里），只要不在默认配置里信任它们就不会有影响。
- 要回到 mbridge 的选档，照常 `codex -p mbridge`。两种方式互不影响。

## 分支与发版

| 分支 | 用途 |
| --- | --- |
| `main`（默认分支） | 开发主干 |
| `fix/…`、`feat/…`、`docs/…` | 功能分支，完成后 PR 合回 `main` |
| `v*` 标签 | 推送后构建六个二进制（darwin、linux、windows 的 amd64 和 arm64）和 `checksums.txt` 并发布 Release |

Magpie Bridge 和 magpie 之间只走公开接口（HTTP、CLI、usage CSV），全部集中在 `internal/magpie`。CI 每天拿官方最新的 magpie 跑契约测试（`contract/`），接口变了会自动开 issue。

## 构建与测试

```sh
make build                        # ./mbridge，纯 Go，不需要 cgo
make test                         # go vet、go test 和安装脚本的测试
cd clients/pi && npx vitest run   # Pi 扩展
bun test clients/magpie-page      # magpie 页面插件
```

## mbridge 自己的状态页

`mbridge serve` 在自己的地址上服务一个网页：

```sh
open http://127.0.0.1:3426/status     # 或直接在浏览器里打开
mbridge status                          # 同一批结论的终端版
mbridge status --json                   # 同一批结论，结构化输出
mbridge service restart                 # 重启（页面上也有一个按钮）
```

页面上有：进程与依赖（版本、pid、运行时长、实际 listen、service、magpie）、
**应用默认值之后**的 `router.json`（不显示实验分组的 salt）、最近 5 分钟在走
mbridge 的会话卡片、最近判档、分类器（配置 / 覆盖率 / 失败分类 / 耗时 / 校准表）、
分档与 reason 统计、两臂对比。区间 1 天 / 7 天 / 30 天，默认 7 天。

几处它不肯含糊的地方：

- 卡片上的档是「这一轮送去了哪」，不是「该用哪」；分类器的建议另起一列。
- `Classifier` 为空 = 那一轮**没问**分类器（被钉死的 agent、计划模式、压缩轮次），
  这一轮整行灰掉，不进覆盖率、压缩率、不满意率的分母。
- 分类耗时来自 `classify_ms`；拿不到（旧事件）显示「—」，**不会**拿判档总耗时冒充。
- 校准表空段画「—」而不是 0%；分母是「照分类器办的那些轮次」。
- PR 合并率在页面上是「不含 gh 补全」——页面在进程内算不出来 gh。

重启端点 `POST /v1/bridge/restart` 只认同源请求（`Sec-Fetch-Site: same-origin`
或 `Origin` 等于 mbridge 自己的主机），其余一律 403，否则任何网页都能反复重启
本机的 mbridge。本地进程要重启就跑 `mbridge service restart`。

## magpie 窗口里的 mbridge 页面

```sh
magpie plugin add @weiping/mbridge-page     # 已发布到 npm
magpie plugin add ./clients/magpie-page      # 或者从源码装：改完重开一个 magpie 就是新的
magpie plugin --json                         # 确认装上了（plugin list 这个子命令不存在）
```

**看到旧页面怎么办**：magpie 的侧边栏只建一次插件的 iframe，切回来不会重建（spec §2
的「单程票」）。升级后在 Plugins 里把 `mbridge-page` **关掉再打开**（或退出重开 magpie）
才会拿到新页面。页面右上角有 `page <版本>` 标记，一眼能看出是不是旧的。

页面里有两半：
- **magpie 记下的用量**：周期切换（今天 / 7 天 / 30 天）、四块数、模型表。
- **mbridge 自己的状态**：过去 7 天的轮数、分类器覆盖率与失败数、此刻在走 mbridge 的
  会话、最近判档（最终档与分类器建议的档分开显示），以及「要最新数字」「重启 mbridge」
  两个按钮。

magpie 的页面插件跑在 `iframe sandbox="allow-scripts"` 里，`connect-src 'none'`，
**够不着 mbridge**。所以这一半是 mbridge 主动把快照写进 magpie 为该插件保存的那块
options 里的（页面在进程内读，零成本；mbridge 写一次约 0.2 s，不影响 magpie 的其它插件）。
快照**不是实时的**：页面显示它的年龄，点「要最新数字」会让 mbridge 立刻发一次。

详见 [`clients/magpie-page/README.md`](clients/magpie-page/README.md) 与
spec 的 §7（这条路一开始被否掉过，后来实测推翻了否掉的理由）。

## 参考

- LangChain：[How to Build a Model Router in the Harness](https://www.langchain.com/blog/how-to-build-a-model-router-in-the-harness)
- OpenRouter：[Confidence Thresholds for Model Escalation Routing](https://openrouter.ai/blog/insights/confidence-thresholds-for-model-escalation-routing/)
- TypeSafe：[Jev 文档](https://docs.typesafe.ai/introduction)

## 许可

MIT，见 [LICENSE](LICENSE)。
