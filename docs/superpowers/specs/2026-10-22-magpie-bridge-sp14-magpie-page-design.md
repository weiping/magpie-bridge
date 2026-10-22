# SP14：mbridge 的状态页，和 magpie 窗口里的用量速览

> 状态：草案，等作者确认。方案 A（状态、报表、重启长在 mbridge 自己的进程上，
> magpie 页面只做用量速览）。
>
> 依据：magpie `main` 的 `internal/gui/pluginpage.go`、
> `internal/gui/assets/pluginpages.js`、`internal/plugin/page.go`
> （2026-10-22 查）；社区现有的 `@magpie-community/page-usage`
> （`magpie-community/plugins` 的 `packages/usage`）。
>
> **前置（已完成）**：根目录有三个未跟踪的 magpie GUI 残留（`api.go`、`web.go`、
> `plugins.go`），让 `go build ./...` 报 23 个错。已移到仓库外的
> `../magpie-bridge-gui-reference/`，`go build ./...` 和 `go vet ./...` 现在都是绿的。

## 1. 问题

作者想在 magpie 里多一个 mbridge 的页面：看状态、看统计、重启 mbridge。

拆成两件事，因为它们的可行性和归属完全不同：

- **状态、报表、重启** —— 正主是 mbridge 进程，数据全在它本地
  （`router.json`、`router.jsonl`、`/v1/bridge/router`）。**mbridge 自己开一个网页
  最自然**：不碰 magpie 的配置格式，不违反 AGENTS.md 的契约，能显示 magpie 永远给不了
  的东西（档位配置、最近判档、路由报告）。
- **用量速览** —— 已经在 magpie 窗口里了，做成一个页面插件，给天天用 magpie 的人
  一个进出的 tab。

## 2. 约束：magpie 页面够不着 mbridge

magpie 页面插件跑在 `iframe sandbox="allow-scripts"` 里，独立 origin，CSP
（`pluginpage.go` 的 `pagePolicy`，全量）：

```
default-src 'none'; script-src <只允许自己的文件>; style-src <自己的> 'unsafe-inline';
img-src <自己的> data:; font-src <自己的>; media-src <自己的>;
connect-src 'none'; frame-src 'none'; worker-src 'none'; object-src 'none';
form-action 'none'; base-uri 'none'; frame-ancestors 'self'
```

- **没有网络**：`connect-src 'none'`，连不上 `127.0.0.1:3426`。
- **不能内嵌**：`frame-src 'none'`，连 iframe 都建不了。
- **只能读两种**：`plugin.PageReads = {"usage:summary", "providers:list"}`
  （`internal/plugin/page.go:77`），还得在 `magpie.permissions` 里声明。
- 唯一可写的本地状态是 `magpie.options`（≤1 次/秒，存 magpie 的 `plugins.json`）。

**结论**：magpie 页面永远拿不到 mbridge 的进程状态、`router.json`、判档记录，也点不动
任何东西。这是 magpie 的设计边界，不是实现问题。**所以状态和重启不进 magpie 页面。**

**一个例外，值得记住**：沙盒 iframe **导航自己是允许的**（`allow-scripts` 只禁顶层导航，
那要 `allow-top-navigation`）。所以 `<a href="http://127.0.0.1:3426/">` 点了之后，
插件那个 tab 会变成 mbridge 的状态页。

**但这是单程票**：`pluginpages.js` 的 `open(key)` 开头是
`if (p.frame?.isConnected) return p.ready`，frame 一旦导航走，sidebar 再点那个 tab 不会
重置 `src`，插件页这一局回不来（要恢复得把插件在 Plugins 里关掉再打开）。所以页面里
**URL 给可选择的文字，不做成链接**；链接只作为"知道自己在单程票上还要点"的备选。

## 3. mbridge 侧

### 3.1 状态页

`mbridge serve` 在监听地址的 **`/status`** 上服务一个页面（默认
`http://127.0.0.1:3426/status`）。**不占 `/`**：`/` 是反向代理的根，mbridge 自己要往那儿
过的请求不能被自己的页面截走。页面的脚本和它的 API 同源（`127.0.0.1:3426`），
所以它 `fetch` `/v1/bridge/router` 和 `POST /v1/bridge/restart` 都不受 §3.3 的同源检查
挡（那检查挡的是别人）。

页面自己不发请求去别处，数据全从 `/v1/bridge/router`（扩过，§3.2）拿，每 5 秒刷新。
下面 A–H 是它显示的全部内容，每一项都注明数据从哪来、是现成的还是要新写。

#### A · 进程与依赖

| 显示 | 来源 | 现成？ |
| --- | --- | --- |
| mbridge 版本、pid、`uptime_s` | `main.version` / `os.Getpid()` / serve 启动时刻 | 新写（三个字段） |
| 实际 `listen` | reloader 的 `r.listen` | 现成（`serve_cli.go`） |
| 配置里的 `listen`，以及**两者是否一致** | `cfg.Listen` vs `r.listen` | 新写一个布尔 |
| service：装没装 / 在不在跑 / 平台自己的说法 | `service.Manager.Status(ctx)` | **现成**（`service status` 在用） |
| magpie：地址、通不通、版本 | `magpie.Health` / `.Version` | **现成**（`statusCmd` 在用） |
| `router.json`：路径、能不能读、读不了时**原文** | `router.ConfigError()` + `router.Load` | 现成 |

`router.json` 改完之后 reloader 一秒内生效（只改 `listen` 要重启，reloader 自己 log 的
就是这个意思），所以 `listen` 不一致时页面顶上一条醒目提示：
**「配置里的 listen 要重启才生效」**。

#### B · 生效中的配置

直接显示 `router.json` **应用默认值之后**的值（`Config.defaults()`），不是文件里的原文：
用户要看到的是"现在真正在用什么"。逐项：

`router_group` · `default_tier` · `classifier` · `classify_timeout_ms` ·
`escalate_turns` · `cache_ttl_seconds` ·
`thresholds`（`tier_min` `dissatisfied_min` `review_min` `review_confidence_min`
`compact_min`，缺的那个显示默认）· `review`（`mode` `timeout_ms` `max_answer_chars`）·
`experiment`（`enabled` `router_percent` `control_tier`；**`salt` 不显示**，那是实验分组的
私密量）· `fixed_agents`（默认五条：`Explore` `statusline-setup` `claude-code-guide`
`Plan` `explorer`）· 每档的 `group` / `claude_alias` / `criteria`。

**页面给的是全局值。** 项目级 `<cwd>/.mbridge/router.json` 只能覆盖每档 `criteria`
（`config.go` 的 `mergeProjectCriteria`），页面看不到某个会话当时拿到的是哪一份 ——
所以这一行写「项目级文件可覆盖每档 criteria」，不假装页面显示的就是全部。

#### C · 正在选档的会话

这是作者要的那一块。**最近 5 分钟内有请求的会话**算"正在"，一条一张卡片，最新的在前。
这一块最容易写错：`Sessions` 的 map 里**同时装着两种 key**，而且大部分字段根本不在
`Sessions` 里、在事件里。四个坑按顺序踩完再写。

**坑 1 · 卡片的主键是 `key`，不是 `session`。** `DecideInput.Key` 在 harness（`/turn`）
路径上就等于 session（`api.go:309`），在 gateway 路径上是 `session + "|" + firstWords`
（`route.go:82`）；`firstWords` 是首条用户消息 sha256 前 12 字节的 24 位小写十六进制
（`wire.go:459`）。**所以一个 session 会有多张卡片** —— 同一段对话里每个子代理各一张
（`FirstWords` 的注释就是为这个写的：子代理和父代理共享 session，不共享首条消息）。

`Sessions` 的方法一半按 `key`、一半按 `session`，用错就是串味 —— 同一 session 的两张卡
会互相显示对方的 pin 和工具数：

| 按 `key` | 按 `session` |
| --- | --- |
| `Get` `Commit` `Turn` `UpdatedAt` `PutReview` `TakeReview` `InheritFrom` | `MarkPinned` `Pinned` `Observe` `Stats` `SinceLast` `MarkDerived` `ParentOf` |

**`key` → `session` 的换算规则写死**：从**最后一个** `|` 切，后缀是 24 位小写十六进制
才算 `firstWords`，否则 `session = key` 整串（harness 路径没有 `|`）。**不要图省事写
`strings.Split(key, "|")[0]`** —— session id 来自请求头、客户端可控、最长 128 字符
（`wire/session.go` 的 `SessionOf`），里面出现 `|` 是可能的。

**坑 2 · `/turn` 路径的子代理不会出现，`gateway` 路径的子代理会出现。**
`policy.go:40` 把子代理的 `prev` 强制成 nil，`api.go:187` 只让 `main` / `gateway`
写回状态 —— Claude Code 的 Task 子代理走 `/turn` 且 `agent != main`，判完即弃，
**不在这一块里**（§6.3 的子代理无状态，是有意的）。但 gateway 路径的 `Agent` 恒为
`"gateway"`（`route.go:115`），子代理照样提交状态，只是 `firstWords` 不同，表现为
"同一个 session 下多张卡"。**页面上不要写"子代理不出现"**，那是只对一半路径成立。

**坑 3 · `harness` / `agent` / `reason` / 置信度不在 `Sessions` 里，在事件里。**
`sessionState` 只有 `tier` / `toolCalls` / `toolFailures` / `lastAt` / `parent` /
`turns` / `pending` / `pinned`（`session.go:13`）。卡片要显示 harness 和 agent，得从这条
key **最近一条 `decide` 事件**里 join（`Event.Harness` / `Event.Agent`）。两个后果：

- `InheritFrom`（`api.go:118`）只搬状态、不发事件，所以继承来的档**可能比卡片上那条
  reason 更新得晚** —— 照实显示，不要假装 reason 描述的就是当前这一轮；
- 找不到事件的卡片（进程刚起、事件已滚出 `router.jsonl`）显示「未知」，**不是空白**。

**坑 4 · 工具调用 / 失败只有 gateway 路径有。** `Observe` 只在 gateway 路径被调
（`route.go:62`、`route.go:88`）；`/turn` 路径的工具数是请求里带来的（`api.go:316`），
进了策略但**没存进 `Sessions`**。所以 Claude Code / Codex 的卡片这一格是「—」。
本 SP 照实显示「—」并在灰字里说明；要两条路径都有，得在 `Commit` 里多存一份上一轮的
工具数，那是行为改动，另开一题（§6）。

卡片长这样：

```
┌────────────────────────────────────────────────────────────────┐
│ codex · main                    balanced → group/mb-balanced  ● 12s │
│ 3c8f9a12…a91f · 4e2b1d77           R6-adopt · conf 0.82 · 41ms     │
│ 轮 7 · 工具 24/2 · 升档还剩 2 轮 · 已手动 pin                     │
└────────────────────────────────────────────────────────────────┘
```

| 字段 | 来源 | 缺了怎么显示 |
| --- | --- | --- |
| `harness` | 该 key 最近一条 `decide` 的 `Event.Harness` | 「未知」 |
| `agent` | 同上，`Event.Agent` | 「未知」 |
| 档 + 送去的组 | `Sessions.Get(key).Tier` + `cfg.Tiers[tier].Group` | 没档就不建卡 |
| `session` | key 换算（坑 1），只显示前 8 位 | — |
| reason | 最近一条 `decide` 的 `Event.Reason`，按下表翻译 | 「—」 |
| `tier_confidence` | 同上，`Event.TierConfidence`（`*float64`） | 「没测到」 |
| `latency_ms` | 同上，`Event.LatencyMs` = **Decide 全程**（含分类），见 §E | 「—」 |
| 已提交轮数 | `Sessions.Turn(key)` | 0 |
| 工具 / 失败 | `Sessions.Stats(session)`（**仅 gateway**，坑 4）；满足 `calls >= 3 && failures*2 >= calls`（`policy.go:98`，R3-tools 的判据）时标红 | 「—」 |
| 升档还剩几轮 | `Sessions.Get(key).EscalatedLeft` | 为 0 就不显示 |
| 连降计数 | `Sessions.Get(key).LowerStreak` | 为 0 就不显示；≥2 时标一下（下一轮 R6-lower 就会降档） |
| 手动 pin | `Sessions.Pinned(session)` | 否 |
| 父会话 | `Sessions.ParentOf(session, firstWords)` | 折叠放在末行，只有子代理有 |

行为：

- 按 `lastAt` 倒序，最多 20 张，多出来的写「还有 N 个」；
- 5 分钟是**显示窗口**，不是存储策略 —— `Sessions` 只在条目 ≥1024 时才淘汰闲置超 24 小时的
  那一些（`session.go:236`），过滤必须在截断之前做；
- `lastAt` 是零值的条目**跳过** —— `InheritFrom` 建出来的条目只写 `tier`、不写 `lastAt`
  （`session.go:231`），不跳过就会渲染出「1677 年前」；
- 一条都没有时显示「现在没有会话在走 mbridge」，**不是空的 `[]`**；
- 卡片是纯展示：不做悬停展开、不做假的可点区域（沙箱里也没有可用的 hover 提示）。

**要新写一个方法**：`Sessions` 现在**没有"列出全部"**。加
`Live(window time.Duration) []LiveSession`，遍历时**只认 `tier.Tier != ""` 的条目**
（这就是 `UpdatedAt` 的判据，`session.go:142`：光被 `Observe` 过、没有提交过档的条目
不是一张卡），按 §坑 1 换算出 session 去 join 工具数 / pin / 父会话，空结果返回 `[]`。
在 `serve` 里每 5 秒做一次快照供页面读，页面自己不碰 `Sessions` 的锁。

reason 的取值就是 `policy.go` 里那些，写死一份对照表给页面（中英文各一套）：

| reason | 含义 |
| --- | --- |
| `R1-fixed` | 该 agent 被 `fixed_agents` 钉死 |
| `R2-plan-mode` | 计划模式，直接 performance |
| `R9-compact` | 压缩 / 摘要轮次 |
| `R3-escalate` | 上一轮被判不满意 → 升一档 |
| `R3-tools` | 上一轮至少 3 次工具调用、一半以上失败 → 升一档 |
| `R3-review` | 复核说上一轮没解决 → 升一档 |
| `R4-escalation-hold` | 升档后保持（`escalate_turns` 轮内） |
| `R6-adopt` | 采用分类结果 |
| `R6-lower` | 降档（缓存过期，或连降两次） |
| `R6-keep` | 不降 |
| `R7-carry` | 分类失败或置信度不够，沿用上一档 |
| `R8-default` | 没有上一轮，用 `default_tier` |
| `hint` | 用了 `/turn` 存下的判档 |
| `R-held` | 本轮内沿用开轮时那一档 |
| `codex-title` | Codex 给线程起标题，不是真的一轮（记成 `aux`，**不计入统计**） |

#### D · 最近判档

`Deps.decisions` 那个 ≤20 的内存环（`/v1/bridge/router` 已经在返回）。每条一行：
时间、harness、agent、判定的档、**分类器建议的档**（`classified_tier`）、reason、
`source`、`classifier`（**两个都要、分开显示**，§E）、confidence、`latency_ms`。

**最终档和 `classified_tier` 不一样的那几条要能一眼看出来** —— 那是"分类器和策略分歧"
最直接的地方，比任何统计都说明问题。

#### E · 分类器（Jev）

这一块最容易把两个都叫 "source" 的字段搞混，也最容易把"没测到"画成 0。

**先分清两个字段**（`Event.Source` ≠ `Event.Classifier`）：

| 字段 | 取值 | 出处 |
| --- | --- | --- |
| `Source` | `jev` / `llm` / `rule` / `default` / `hint` / `held` | `classifySource`（`api.go:247`）只产两个值：`classifier` 正好是 `typesafe/jev-latest` 时给 `jev`，其余给 `llm`。另外 `rule` = 直接命中压缩提示（`api.go:136`）、`default` = 这一轮压根没问（`api.go:134`） |
| `Classifier` | 模型 id，形如 `typesafe/jev-latest`、`deepseek/deepseek-flash#plain`（回落到 plain prompt 时带 `#plain` 后缀） | `Verdict.Source`，写入在 `api.go:214` |

**页面上写"哪个模型判的"用 `Classifier`**，`Source` 只用来分组和配色（`jev` 一种、
`llm` 另一种、没问的灰掉）。写成 `llm: typesafe/jev-latest` 是错的 —— 那一条的
`Source` 是 `jev`。`Classifier` 为空 = 这一轮没问分类器，**这是唯一的判别依据**。

面板分五块：

**1 · 配置。** 分类器模型、`classify_timeout_ms`、`thresholds.tier_min`。
**并且写清楚"不是每一轮都问"**：R1（`fixed_agents` 钉死）、R2（计划模式）、直接命中
压缩提示的轮次都不问分类器（`api.go:135`）。所以"分类覆盖率 < 100%"是正常状态，
不是故障 —— 覆盖率本身就是这一块该显示的一个数（问了的轮次 / 全部 decide 轮次）。

**2 · 最近若干轮，每轮三格。**

```
  时间       tier 置信度            dissatisfied          compact
  12:04:31   ████████░░  0.82      ██░░░░░░░░░  0.21    ░░░░░░░░░  0.08
  12:04:58   ██████████  0.97      —                     —
  12:05:12   — 灰 —  没问（R2-plan-mode，这一轮不送分类器）
```

**三态必须画得出来**（LESSONS.md：读不出来的格子是"不知道"，不是"空"也不是"0"）：

| 显示 | 条件 | 为什么 |
| --- | --- | --- |
| 一个数（带条） | `Classifier` 非空且该分数有记 | 分类器真的答了 |
| `—` | 问了但没记这一项 | `dissatisfied` 只在 `prev != nil` 时记（`api.go:218`），`compact` 只在 `> 0` 时记（`api.go:222`）—— 首轮和"压缩分正好 0"在 `Event` 里长一样 |
| 整行灰掉 | `Classifier` 为空 | 这一轮没问（`api.go:134` 的 `default`、`api.go:136` 的 `rule`、R1 / R2） |

**所以：判别"没问"看 `Classifier` 空不空，不看那几个指针是不是 nil。** 想统计"压缩率"
或者"不满意率"时，分母是**问过且记到了的那部分**，不是全部 decide 轮次，也不要用
`Compact == nil` 去数"没压缩" —— 那会把"没问"算成"压缩了"。

**3 · 失败。** 区间内 `classify_error` 非空的轮次占**问过分类器的轮次**的比例（不是占全部
轮次）。失败原因照抄 `classifyFailure` 的三类（`api.go:433`），三类要给不同的建议：

| `classify_error` 开头 | 含义 | 页面上写 |
| --- | --- | --- |
| `timeout (classify_timeout_ms): …` | 分类器那边超时 | 调大 `classify_timeout_ms` |
| `canceled: the client stopped waiting: …` | 客户端先放弃了，请求不等了 | 分类慢让客户端先走 —— 调小超时或换更快的分类器 |
| 其它（magpie 自己报的原文） | magpie 侧报错 | 原样出，不要归到前两类 |

**4 · 耗时。这里有个现成的坑。** `Event.LatencyMs` 是 **Decide 全程**（`api.go:196` 从
`start` 量到返回），**包含分类**；分类自己的耗时只在失败时进了日志（`api.go:164`），
成功时**根本没记**。两个选择，本 SP 取第一个：

- **加 `Event.ClassifyMs int`（`json:",omitempty"`）**，在 `cstart := time.Now()` 那一行
  之后（`api.go:151`）成功和失败都记一次。这是给已有事件加一个可选字段，不改任何行为，
  旧 `router.jsonl` 读出来就是 0，页面对老数据这一格显示「—」；
- 不加的话，这一格必须标「判档 41ms（含分类）」，**不许**标成"分类耗时"。

**5 · 校准。** §G 的三张表搬过来。表头要写清楚 `tier` 那一列的分母是
**"分类器的档被采纳了"的那些轮次**（`calibrate.go:88`：`tierConf == nil ||
classifiedTier == "" || tier != classifiedTier` 一律不进），所以这张表**不是**
"分类器整体准不准"，是"照它办的那些轮次准不准"。落到 R2 / R6-lower 那些没照它办的
轮次上，是被刻意排除的，不是不准。

另外 `SampleShort` 的判据是**任一段样本 <30**，`calibrate.go:297` 把空段也算进去 ——
所以 1 天窗口下这三张表几乎必然标"样本不足"。**空段要画成「—」而不是「选低率 0%」**
（0 个样本的段，`UnderRate` 留在 0，画成 0% 就是把"没数据"说成"没有欠档"）。

#### F · 分档统计

全部从 `router.jsonl` 现算（`router.Decide` 每轮 `Append` 一条 `decide`）。
区间 1 天 / 7 天 / 30 天，默认 7 天（§9.2）。

| 统计 | 怎么来 |
| --- | --- |
| 各档轮次与占比 | `Report.TierDistribution`（现成） |
| **各 reason 的次数与占比** | `Event.Reason` 直方图，**新写** |
| **各 harness 的轮次与占比** | `Event.Harness` 直方图，**新写** |
| 主轮次 vs 子代理 | `Event.Agent == "main"` 分开 |
| router 臂 vs control 臂 | `Event.Arm`；实验关掉时全是 router，**全零的臂不画** |
| control 臂的影子档分布 | `Report.ShadowDistribution`（现成） |
| hint 命中数 | `Event.Kind == "hint_consumed"` 计数（现成字段） |

**只统计 `Kind == "decide"` 的轮次**：`aux`（Codex 线程标题）和 `proxy_passthrough`
不进去，`shadow` 单独算。

#### G · 两臂对比与校准

`mbridge router report` 的内容（`Report` / `ArmReport`），**进程内聚合，不 spawn 子进程**：

会话数 · 成本中位数 / 均值 / p90 + bootstrap 95% CI · 总成本 · cache write 成本
（**注明是估算**：magpie 的 CSV 没有按 token 种类分的成本，cache write 那份按它在输入侧
token 里占的比例摊，`report.go` 的算法）· 未计价请求数 · 开过 PR 的会话 / 已合并的会话 /
手动切过模型的会话 · 两臂合并率的两比例 z。

**`SampleShort`（任一臂 <100 会话）时只显示成本和手动切模型率**，其余标"样本不足" ——
§9 本来就这么规定，页面照办，不显示一个没有依据的 z。

校准三张表（`router.Calibrate` 的 `tier` / `dissatisfied` / `review`）：每张是五个固定段
`[0.95,1] [0.85,0.95] [0.70,0.85] [0.50,0.70] [0,0.50]` 的样本数、占比、段内选低率，
加当前阈值、建议阈值（没有就说没有）、当前阈值的升档率与未升档中的选低率、以及换用
建议阈值的投影。**任一段样本 <30 就标 `SampleShort` 且不给建议**（`calMinSamples`）。

**PR 状态有一个诚实的缺口**：`ReportInput.PRStates` 是 **CLI 里调 `gh` 解析**的
（`router_cli.go`）。状态页在进程内做不出这个，只能用会话自报的 `pr_merged` feedback
兜底（`hasMergedFallback` 那条路）。所以页面上的合并率要标
**「不含 gh 补全」**；要全的看 `mbridge router report`。

#### H · 重启

按钮 + `mbridge service restart` 那一行，见 §3.3。

#### 诚实的边界（页面自己要说出来，不许猜）

- **harness 是客户端自报的。** `/turn` 的 `harness` 字段是客户端给的字符串，
  mbridge **不校验**（`api.go` 的 `turn`）。所以出现没见过的值就照原样显示，
  不要塞进"其它"里假装认识它。
- **会话列表只到 mbridge 记得住的为止。** `Sessions` 是内存态，进程重启即空，
  且只在 ≥1024 条时才淘汰。
- **项目级 criteria 看不到**（§B）。
- **PR 合并率不含 `gh`**（§G）。
- **magpie 没有价的模型计入 `UnpricedRequests`**，不按 0 算进成本。
- **页面上的档是"这一轮送去了哪"，不是"这个任务该用哪"** —— 后者是分类器的建议
  （`classified_tier`），两者分开显示（§D）。

### 3.2 扩 `/v1/bridge/router`，不开第二个端点

它已经返回 `{valid, router_group, turn_budget_ms, tiers, experiment, decisions}`
（`internal/router/api.go:406`），就是"mbridge 认为自己的配置是什么"那个端点。在它上面
补 §3.1 的 A–F，**不再加 `/v1/bridge/status`**：两个端点说同一件事，就要两份维护、
三处不一致。

加一个 `?window=1d|7d|30d`（默认 `7d`）和 `?since=<RFC3339>` 供 F、G 两节用；
**`window` 只影响聚合那几节，A–E 永远是"此刻"**。

新增的字段：

```
version, pid, uptime_s, listen, listen_pending, magpie_version, magpie_ok,
service: {installed, running, detail}, config_error,
live: [ {key, session, harness, agent, tier, group, reason,
         tier_confidence, latency_ms, turns, tools, tool_failures,
         escalated_left, lower_streak, pinned, parent, last_at} ],  // §C，5 分钟窗口
config: { …Config 应用默认值之后的全部… },              // §B
window: {kind, since, coverage: {asked, total}, classify: {…}, tiers: {…}, reasons: {…},
         harnesses: {…}, arms: {…}, report: {…}, calibrate: […] }
```

**`window` 那一大块由谁算、算多重，要在实现前定下来**：每 5 秒全量重扫
`router.jsonl` + `magpie usage --csv` 是浪费（一小时几千个事件、每次还要拉 magpie 的
CSV）。做法是 **serve 里一个后台聚合器，每 30 秒增量跑一次**（读 `router.jsonl` 的新增
行，`usage --csv` 按 `since` 拉一次），页面读的是它算好的三个窗口。

`mbridge router report` 的 CLI 继续自己算自己的（它要 `--since`、`--csv`、`gh`、
PR 状态，进程内那一套给不了）；**页面上的数会因此和 CLI 差一点**（页面的 PR 状态没有
`gh`），页面上标出来，CLI 不标。

顺手加 `mbridge status --json`，把 `statusCmd` 已经聚合的结论结构化输出。

### 3.3 重启按钮：端点要卡同源

`service.Manager.Restart()` 已经存在（`internal/service/service.go:189`），
`mbridge update` 在用（`cmd/mbridge/update_cli.go:126`）。两条路都接上：

- **`mbridge service restart`** —— `service` 现在只有 `install|uninstall|status`
  （`cmd/mbridge/service_cli.go`），加一个 case，零新逻辑。没装 unit 时 `Restart()`
  自己是 no-op。
- **状态页的按钮** → `POST /v1/bridge/restart`。

**这个端点必须卡同源，否则是能被任意网页触发的 DoS。**

mbridge **完全没有鉴权**，`proxy.Serve` 只强制监听 loopback，它自己在
`internal/proxy/proxy.go:138` 写明了："magpie asks no key of what comes from loopback"。
但 loopback 对浏览器不构成防护 —— 你访问的任何网页都能往 `127.0.0.1:3426` 发一个跨域
简单 POST（不需要 CORS 预检，请求照发），从而把你反复重启。

所以 `POST /v1/bridge/restart` 只在浏览器说是同源时才做：

- `Sec-Fetch-Site: same-origin`（`localhost` 页面）或 `Origin` 等于自己的 `listen` 主机；
- 其它一律 `403`，**先回 403 再说要不要重启，不要靠"没这个头"放行**；
- **本地非浏览器进程不受影响**：要重启的进程本来就能直接跑 `mbridge service restart`，
  这条限制只挡网页。

现有的 `/turn` 等端点有同样的暴露面，那是既有的、不在本 SP 范围；但**不要再往上加
一个廉价的 DoS**。

## 4. magpie 页面插件

### 4.1 放哪、装哪

`clients/magpie-page/`，纯静态，无构建步骤：

```
clients/magpie-page/
  package.json      # magpie.page + magpie.permissions；没有 main / exports
  README.md
  page/index.html
  page/app.js       # 走 bridge、画、每分钟刷新
  page/view.js      # 文案（中英）+ 格式化 + 纯计算，无 DOM
  page/style.css    # 只用 --magpie-* 变量
  page/icon.svg
  page.test.mjs     # bun test
```

装法（见 §9.3 的决定）：**npm 上的 `@weiping/magpie-bridge-page`**，
`magpie plugin add @weiping/magpie-bridge-page`；没有 npm scope 时退回
`git clone` 之后 `magpie plugin add ./clients/magpie-page`。文件夹插件按现状加载，
不装进 `~/.config/magpie/plugins/`，改了源码在终端重开一个 `magpie` 命令就是新的。

不上 `registry.json`：Discover 那一栏是 `magpie-community/plugins` 仓库的
`registry.json`，那是社区的名单；走 GitHub `magpie-plugin` topic 那条路要求
`package.json` 在仓库根，而根是 Go 模块，不该为了一个插件长出一个 `package.json`。

### 4.2 读什么

`magpie.permissions` **只有 `["usage:summary"]`**。不加 `providers:list`：页面看不到
档位组成员的模型（§2），列出来的只是 magpie 的全部供应商，用户在 magpie 自己那一页
已经看得到，约等于零信息换一条 permission。

`usage:summary` 一个周期给：
`{period, since, calls, errors, input, output, cacheRead, cost, models[{model, calls, input, output, cost}]}`
（前 20 个模型）。

### 4.3 显示

照 `packages/usage` 的形状，不重新发明：周期切换（今天 / 7 天 / 30 天）+ 四块
（调用 / 错误占调用比 / Token / 费用）+ 模型表按 token·调用·费用排序带占比条 +
每 60 秒刷新 + 周期和排序记在 `magpie.options`。

两处必须写明：

1. **用量那节的免责**：「这些是 magpie 记下的全部用量。客户端都走 mbridge 时它才等于
   mbridge 的用量；你直接用 magpie 的那部分也在里面。」—— 这不是 mbridge 的流量。
2. **状态那节**：`mbridge status --json` 的完整版在 `http://127.0.0.1:3426/status`（把地址给成
   可选择的文字，见 §2 的单程票），终端里用 `mbridge status`。旁边给
   `mbridge service restart`。

**不做档位归因**：mbridge 改写成 `group/mb-<档>`，magpie 记的是最终的具体模型，
`usage:summary` 里分不出档位。

### 4.4 为什么这个包必须永远是纯页面插件

记一条踩坑，因为它是 **别碰** 的性质，不是"最好别"：

`magpie.options.set()` → `plugin.SetPageOptions`（`internal/plugin/page.go:261`）。
它对**纯窗口插件**（有 `page`/`panel`，无 `main`、无 `exports`）**不调**
`plugin.Restart()`；而 `magpie plugin options` CLI 走的是 `plugin.SetOptions`
（`internal/plugin/store.go:632`），**每次写都把当前 Bun host 置 nil 让它退休**。

所以：**只要给这个包加了 `main` 或 `exports`，用户每点一次周期切换就可能把所有插件
重启一轮。** 测试里有一条守着（§5）。

## 5. 测试

页面（`bun test clients/magpie-page`），照 `packages/usage/usage.test.mjs` 的形状：

1. **权限守恒**：`permissions` 恰好 `["usage:summary"]`；正则扫出所有
   `magpie.read("…")` 的名字，集合相等；源码里不出现 `fetch(`、`XMLHttpRequest`、
   `WebSocket`、`sendBeacon`、`EventSource`、`importScripts`。
2. **形态守恒**：**没有 `main` / `exports`**（§4.4 那个坑）；HTML 里有
   `<script src="_magpie/bridge.js">`；每个 `<script>` 都有 `src` 且内容为空
   （inline script 被 CSP 拒）；每个 `src` / `href` 都是 `page/` 里的相对路径，
   且文件真实存在。
3. **纯函数快照**：`view.js` 的 tiles / rows / `count` / `money` 的确切输出，
   `en` / `zh` 各一份。
4. **空数据不崩**：`calls: 0` 不产生 `NaN%` 或 `$NaN`；`models: []` 是空表；
   `usage:summary` 抛错时显示错误而不是白屏。

mbridge（Go）：

- `mbridge service restart` 三平台各一条 golden（同 `internal/service/testdata/*.golden`）；
  `service` 拿到第三个子命令时报错并打用法。
- `status --json` 的结构断言。
- **`Sessions.Live(window)`**（§C，四个坑各一条）：
  - 只返回 `lastAt` 在窗口内的；窗口外的一条都没有；
  - ≥1024 条时先塞满再过滤（证明过滤发生在 `evictLocked` 的截断之前）；
  - 一条都没有时是 `[]` 不是 `null`；
  - **坑 1**：同一 session 下两个不同 `firstWords` 的 key 各出一张卡，两张卡的
    `pinned` / 工具数来自**同一个 session 条目**；再造一个 session id 本身含 `|` 的
    `/turn` key，断言换算退回整串、没被切成两半；
  - **坑 2**：gateway 的子代理（`agent` 恒 `gateway`、`firstWords` 不同）**要出现**，
    `/turn` 的 `agent: "task"` **不出现**；
  - **坑 3**：`InheritFrom` 造出来的零值 `lastAt` 条目被跳过（不渲染成「1677 年前」）；
    查不到事件的卡片 harness 是「未知」不是空串；
  - **坑 4**：`/turn` 卡的工具格是「—」，gateway 卡才有 `24/2`。
- **`/v1/bridge/router` 扩字段**（§3.2）：`version` / `listen` / `listen_pending` /
  `uptime_s` / `pid` / `service` / `config` 都在，`decisions` 环还是 ≤20。
- **reason / harness 直方图**（§F）：喂一段真的 `router.jsonl`（录一段，不要编），
  断言每个 reason 的计数和占比；`aux` 和 `proxy_passthrough` 不进 decide 的分母；
  没见过的 harness 原样出现、不被塞进"其它"。
- **`window` 的三档**（§3.2）：`1d` / `7d` / `30d` 各一条，同一份输入下三档的会话数
  满足 `1d ≤ 7d ≤ 30d`；`window` 只影响聚合，A–E 不随 `window` 变。
- **缺测不等于 0，没问更不等于 0**（§E）：一条 `tier_confidence` 为 `nil` 的事件，
  页面输出是"没测到"，且不进置信度均值（不是当 0 拉低）；一条 `Classifier` 为空的
  事件整行灰掉，且**不进**压缩率 / 不满意率的分母 —— 断言这两类事件都不改变那两个数。
- **`ClassifyMs`**（§E）：分类成功和超时两条都有值；老 `router.jsonl`（没这个字段）
  读出来是 0、页面显示「—」；断言 `LatencyMs ≥ ClassifyMs`（`LatencyMs` 是 Decide
  全程，`api.go:196`），免得实现把两个字段记反。
- **`classify_error` 三类**（§E）：超时 / canceled / magpie 原文各一条，页面原样出，
  第三类不被归进前两类（`classifyFailure` 只有两个 case，别在页面上补第三个）。
- **`tier` 校准的分母**（§E / §G）：造几条"分类器建议了但没采纳"的轮次
  （`R2-plan-mode`、`R6-lower`），断言它们不进 `tier` 表的任何一段，但**进**
  `dissatisfied` 表 —— 两张表的分母不是一回事。
- **空段画「—」**（§E / §G）：五段里某段 0 个样本时，该段选低率不是 `0%`；且
  `SampleShort` 因为这一段为 0 而为真（`calibrate.go:297` 把空段算进去）。
- **`SampleShort`**（§G）：一臂 99 个会话时只出成本和手动切模型率、`MergedZ` 不出现；
  两臂都到 100 时才出现。
- **PR 不含 `gh`**（§G）：只喂 feedback 里的 `pr_merged`，合并率照算，输出带
  `pr_states: "feedback-only"` 标记。
- **`POST /v1/bridge/restart` 的同源判断**：`Sec-Fetch-Site: same-origin` → 202；
  `cross-site` / `none` / 缺 `Origin` → 403 且**没有调用 `Restart()`**；
  跨域那条要有测试证明服务管理器一次都没被叫过（LESSONS.md：断言"没做事"，
  不能只断言返回值）。

端到端：隔离 HOME 里 `magpie plugin add ./clients/magpie-page`，`magpie plugin --json`
能看到这一行（`plugin list` 这个子命令不存在，用 `--json`）。

真机，两条都不能省（LESSONS.md：「"没在真机上试过"的意思是不要再往上堆，
也不要告诉用户能用」）：

- magpie 里点开页面：tab 在侧栏 Plugins 组、标题按窗口语言（`zh` / `zh-TW` / `ja` / `de`）、
  明暗两套 `--magpie-*` 变量都对；改 `router.json` 的档位成员，magpie 状态页 5 秒内更新。
- 从一个真实浏览器标签访问 `http://127.0.0.1:3426/`，点重启；再从 `localhost` 的页面
  跨域 POST 同一个端点，确认 403 且 mbridge 没重启。

## 6. 不做

- 状态、报表、重启**不进 magpie 页面**（§2）。
- 不开 `GET /v1/bridge/status`（§3.2）。
- 不加 `providers:list`。
- 不改 magpie 源码、不开网络、不绕沙盒。
- 不直接写 `~/.config/magpie/` 下的任何文件。
- **不在 `Commit` 里多存一份工具数**，好让 `/turn` 路径的卡片也显示工具调用
  （§C 坑 4）：那是行为改动，另开一题。这张卡这一格就是「—」。
- 不动 `Event.Kind` 那个过期的注释（它说只有 `decide|hint_consumed|feedback|shadow|aux`，
  代码里还有 `review` 和 `proxy_passthrough`）：改注释不影响行为，但它属于"顺手清理"，
  混在这个 SP 里会让 diff 变难读。**要改就单独一个 commit。**

## 7. 记下来：一条走不通的路，和它为什么会走不通

**mbridge 的状态能不能经 `magpie.options` 推进 magpie 页面、让页面点一下就重启？**
能，机制都在，但本 SP 不走。留着，因为结论不显然：

- 页面写 options（`/api/plugin-pages/options`，按 spec 查，别的插件读不到），
  mbridge 在进程外读得到 —— 数据和指令都是通的。
- 心跳可以走页面自己的 `magpie.options.set()`（**零进程**），mbridge 那边每几秒读一次
  `plugins.json` 合并写回；`magpie plugin options` CLI 是 mbridge 侧写 options 的正解。
- **唯一的坑**就是 §4.4：走 CLI 每次写都退休 host，所以心跳必须走页面那侧。

不走它，是因为 §1 的分工更干净：状态、报表、重启的"正主"是 mbridge 进程，
网页长在它身上能显示 magpie 永远给不了的东西；magpie 页面专心做用量速览，
不用每 5 秒推一次快照，也不用把快照塞进用户的 `plugins.json`。

**真要把状态放进 magpie 窗口**，上游给一条更好的路更省：给 `plugin.PageReads` 加一种
由插件宿主回答的 read（不是那个两项的封闭表）。写法照社区给 `usage:breakdown` 提需求的
那篇（yetone/magpie#2574）。本 SP 不等它。

## 8. 顺序

1. ~~把根目录那三个未跟踪的 magpie 文件挪出去~~ **已做**（§ 头部）。
2. `mbridge service restart` + `mbridge status --json`（独立、无依赖，先落地）。
3. `Event.ClassifyMs`（§E）+ `Sessions.Live()`（§C）—— 都是只加不改，先落地。
4. `/v1/bridge/router` 扩字段 + `/status` 状态页。
5. `POST /v1/bridge/restart` + 同源判断（连着它的跨域测试一起）。
6. `clients/magpie-page/`：`package.json`、`view.js` 和测试先落，再 `app.js`、`style.css`。
7. README 的 `clients/` 说明补一句怎么装；`mbridge install` 的文档里加一行。
8. 真机验收（两条）。
9. 发 npm（§9.3）。

## 9.1 实现时定下的（实现已落地，spec 回填）

1. **`Event.ClassifyMs`** 已加（`json:"classify_ms,omitempty"`），成功和失败都记，
   在 `Decide` 里分类前后各取一次时间。旧 `router.jsonl` 读出来是 0，页面显示「—」。
2. **`Sessions.Live(window time.Duration) []LiveSession`**：只认 `tier.Tier != ""` 的
   条目、跳过 `lastAt` 零值的（`InheritFrom` 造出来的）、**先按窗口过滤再截断到 20 条**
   （`Sessions` 只在 ≥1024 条时才淘汰闲置的，过滤必须在截断之前）。空结果返回 `[]`。
   卡片另带 `LiveTotal(window)`，页面才知道「还有 N 个」。
3. **`SessionOfKey(key)`**：从**最后一个** `|` 切，后缀是 24 位小写十六进制才算
   `firstWords`。**已按 §C 坑 1 实现**，配一条「session id 本身含 `|`」的测试。
4. **端点的 `experiment` 字段去掉了 `salt`**：端点只发 `config.experiment`
   （`enabled`/`router_percent`/`control_tier`）。谁读这个端点都不读 salt
   （Claude Code 的 hook 只读 `turn_budget_ms`），所以**私密量不出进程**。
5. **聚合器的形状**：`Aggregator.Run` 每 30 秒读 `router.jsonl` 的**新增行**
   （记偏移量；只取整行，半行留到下次），拉一次 `magpie usage 30d` 的 CSV，
   三个窗口（1d/7d/30d）各算一次存起来，页面只读。
6. **`/status` 是一张自包含的 `page.html`**（`go:embed`），内联 CSS 与 JS，
   无外部资源——mbridge 自己服务它，不受 magpie 沙盒那套 CSP 管。
7. **重启用 POST `POST /v1/bridge/restart`**：同源判断是
   `Host` 必须是 loopback（防 DNS rebinding）**且**
   （`Sec-Fetch-Site: same-origin` 或 `Origin` 的主机等于 `Host` 或 `listen`）。
   **本地非浏览器进程走端点也是 403** —— 它们该跑的是 `mbridge service restart`；
   spec §3.3 的「本地非浏览器进程不受影响」说的是「不受限制」，不是「也放行」。
8. **`proxy.Handler(target, listen, deps)`** 多收一个 `listen`：同源判断要有真实的地址，
   且 `router.json` 读不出来时（`deps` 为 nil）状态页与重启端点照样挂。

## 9. 已定

1. **状态页路径 `/status`**，不占 `/`：`/` 是反向代理的根，mbridge 自己要往那儿过的
   请求不能被自己的页面截走。页面和它的 API 同源，所以 §3.3 的同源检查挡不住它自己。
2. **报表区间 1 天 / 7 天 / 30 天**，默认 **7 天**。不是 1 天：`Report.SampleShort`
   （哪一臂不到 100 个会话）在一天的窗口里几乎总是成立，页面会一直在提示样本不足；
   也不是 14 天。**`mbridge router report` 的 CLI 默认值不动**，仍是 14 天 —— 那是已经
   有人用的行为，页面是新的，两者不需要一致。
3. **发 npm**，包名 `@weiping/magpie-bridge-page`，仓库里的 `clients/magpie-page/`
   是唯一真源（发布前从那里 `npm pack`）。理由：`make release` 只出
   `dist/mbridge-<os>-<arch>` 一个二进制，`install.sh` 也只下那一个二进制，**仓库文件
   不跟安装包走**；而 `magpie plugin add <名字>` 对 npm 包最省事，之后 magpie 自己
   每小时查更新、Plugins 上给一个更新点。零运行时依赖。
   兜底（永远可用、零新代码）：`git clone` 之后 `magpie plugin add ./clients/magpie-page`，
   README 里写这一条。
   要发 npm 就得有 `@weiping` 这个 scope；没有的话实现部分照做，分发退到兜底那条，
   `mbridge plugin install`（下载 release 里的插件 tarball）另开一题。
4. **卡片的 id 用 `key`，不用 `session`**（§C 坑 1）。不改成"给 `sessionState` 加一个
   session 字段"：那要动 `Commit` 的签名和 22 处调用（1 处生产 + 21 处测试），
   而换算规则只有一行、
   并且能用一条"session id 含 `|`"的测试钉住。
5. **给 `Event` 加 `ClassifyMs`，不加就不许标"分类耗时"**（§E）。`LatencyMs` 是 Decide
   全程，把它当分类耗时报出去是**错的数**，不是不精确 —— 所以要么补字段，要么改标签，
   不能含混过去。
