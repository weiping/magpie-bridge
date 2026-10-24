# SP14 真机验收记录（2026-10-24）

SP14 计划 Task 10 Step 5 要求「两条都不能省」的验收当时没有留下任何证据。这次补跑了一遍，
把结果记在这里。**每一条都有命令和输出**，没验的也写明没验。

## 环境

| 项 | 值 |
| --- | --- |
| 机器 | macOS（darwin arm64） |
| 浏览器 | Google Chrome Beta 156.0.8078.12（headless，用来渲染状态页） |
| 隔离实例 | `HOME=/tmp/sp14-acc/home`，`router.json` 抄正式那份并把 `listen` 改成 `127.0.0.1:3436` |
| 正式实例 | `127.0.0.1:3426`（`mbridge serve`，launchd service 在跑，pid 会随重启变） |
| mbridge | 本仓库 `cmd/mbridge` 现编（`go build ./cmd/mbridge`） |

正式实例上的 3426 端口那台是**你正在用的那个**，重启它会让 pid 变、会话列表清空（内存里的）。
本次为了验「点重启 pid 变」，在它上面重启了 4 次，每次都确认服务自己回来了。

## 1. 状态页（`http://127.0.0.1:3426/status`）

| 项 | 结果 | 证据 |
| --- | --- | --- |
| 页面出得来 | ✅ | `GET /status` → 200，32 808 bytes。Chrome Beta headless 渲染后的 DOM 是 49 361 bytes（源 HTML 32.0 KB），差出来的 17 KB 是页面自己的 JS 画出来的 |
| A–H 各块有内容 | ✅ | 渲染后的 DOM 抽出 184 段文字，每一块都有真实数据（下面逐块列） |
| 改 `router.json` 的档位成员，**5 秒内**更新 | ✅ | 隔离实例上把 `tiers.balanced.group` 改成 `mb-balanced-SECOND-EDIT`，**1 164 ms** 后 `GET /v1/bridge/router` 就返回新值，`criteria` 也一起变了；pid 没变（改配置不需要重启） |
| 点「重启 mbridge」，pid 变 | ✅ | POST `Sec-Fetch-Site: same-origin` → pid 38311 → 39272。服务自己回来了，`GET /status` 恢复 200 |

各块的实际内容（截图里的文字）：

| 块 | 内容 |
| --- | --- |
| 抬头 | 357 轮 / 97.8% 问了分类器 / 82 失败 / $208.46 成本 / R6-adopt 最常见的 reason |
| 进程行 | pid 39272 ↑ 2m · 127.0.0.1:3426 · magpie 0.1.1376 · service 在跑 |
| 正在选档的会话 | 「现在没有会话在走 mbridge」（空的时候就说空） |
| 刚刚走过的几轮 | 4 张卡片（pi / codex，带 harness·agent、这一轮的档、分类器的建议、reason） |
| 最近判档 | 10 行（时间、harness、agent、档、建议、reason、来源、置信度、判档耗时） |
| 分类器 | 97.8% (349/357)、失败 82/349、三类失败分列（canceled ×3 / magpie ×61 / timeout ×18）、一行「不是每一轮都问」的说明 |
| 分布 | tier / reason / harness / arm 四个直方图，全部有真实计数 |
| 两臂与校准 | arm 表（67 会话、中位数 $0.3114、p90 $11.13、PR 合并 0、手动切模型 0）+ 三张校准表 |
| 生效中的配置（折叠块） | router_group、default_tier、classifier=mb-classifier、thresholds、review、experiment、fixed_agents、tiers 全在 |
| 重启 mbridge | 按钮在 |
| 页脚 | 5 条诚实边界（harness 自报、会话列表重启即空、项目级 criteria 看不到、PR 合并率不含 gh、卡片上的档是「送去了哪」不是「该用哪」） |

### 顺带验到的：三态渲染（Review Focus #3）

「没测到」没有被画成 0：

- 没问分类器的轮次，置信度/不满意/压缩三列画的是 `—`（例如 `21:10:15 pi main balanced — R8-default default — —`）。
- 空的校准段画 `—`，不是 `0%`：第三张表五段全是 `0 0% —`。
- 样本不足的表在末尾写「样本不足」，不给建议阈值（`current 0.4 · 样本不足`）。

## 2. 同源（`POST /v1/bridge/restart`）

在正式实例（3426）上逐条打，每条之后都看 pid 有没有变：

| 请求头 | 结果 | pid |
| --- | --- | --- |
| `Sec-Fetch-Site: cross-site` + `Origin: https://evil.example` | ✅ 403 | 38311 → 38311，**没重启** |
| 什么头都没有 | ✅ 403 | 38311 → 38311，**没重启** |
| `Sec-Fetch-Site: same-site` + `Origin: https://news.example` | ✅ 403 | 38311 → 38311，**没重启** |
| `Sec-Fetch-Site: cross-site` + `Origin: http://localhost:3426`（浏览器从 localhost 页发过来的样子） | ✅ 403 | 38311 → 38311，**没重启** |
| `Sec-Fetch-Site: same-origin` + `Origin: http://127.0.0.1:3426`（页面自己发的那条） | ✅ 202 / 连接被掐断 | 38311 → **39272**，重启了，服务自己回来了 |

**一个要说清楚的边界**：`internal/restart/restart.go` 的 `SameOrigin` 先看
`Sec-Fetch-Site == "same-origin"` 就放行，**不再回头核对 `Origin`**。所以一个自己
编头的非浏览器进程，只要写上 `Sec-Fetch-Site: same-origin` 就能重启 mbridge
（这次手工伪造 `Sec-Fetch-Site: same-origin` + `Origin: http://localhost:3426` 确实重启了）。

这不是浏览器能做的事：`Sec-Fetch-Site` 是 forbidden header name，页面的 JS 改不了，
浏览器只会按真实关系填 `same-origin` / `same-site` / `cross-site`。而且 SP14 计划的
Global Constraints 写明了「本地非浏览器进程不受影响（它们可以自己跑
`mbridge service restart`）」，这是**照计划实现的**，不是漏判。要收紧的话，是把
`Origin` 的主机也一起核对——那是改设计，不是这次验收的事。

另外：重启会掐断那条连接，页面上的按钮走的是 `catch` 分支，所以点下去按钮上显示的
是 `TypeError: Failed to fetch`，不是页面准备好的「已发出重启指令」。**pid 确实变了，
重启确实发生了**，只是按钮上的字不好看。

## 3. magpie 页面插件

| 项 | 结果 | 证据 |
| --- | --- | --- |
| 隔离 HOME 里 `magpie plugin add ./clients/magpie-page` | ✅ | 输出 `✓ added /Users/liuweiping/workspace/dev/magpie-bridge/clients/magpie-page`，`$HOME/.config/magpie/plugins.json` 写成 `{"plugins":[{"spec":"/Users/liuweiping/workspace/dev/magpie-bridge/clients/magpie-page"}]}` |
| 正式 magpie 里装的是 npm 上的那个 | ✅ | `~/.config/magpie/plugins/node_modules/@weiping/mbridge`，版本 **0.1.4**；`plugins.json` 里是 `@weiping/mbridge@latest` |
| options 通道是活的（spec §7） | ✅ | 同一份 `plugins.json` 里带着 mbridge 发的快照 `mbridge.at = 2026-10-24T13:18:55Z` 和页面给的指令 `cmd.id = "restart"` |
| `--magpie-*` 变量 | ✅ | 页面声明并消费 6 个：`--magpie-bg`、`--magpie-bg-subtle`、`--magpie-border`、`--magpie-fg`、`--magpie-fg-muted`、`--magpie-font`；浅色下有 fallback，深色下用页面自己的一套 |
| 在 magpie 窗口里点开这个页面 | ⛔ **没验** | 这台机器上 `magpie plugin --json` 不返回内容（真实 HOME 和隔离 HOME 都是空的），`magpie plugin add` 打完「✓ added」之后也不退出。所以「tab 落在侧栏 Plugins 组、标题按窗口语言、明暗两套变量对着 magpie 的主题真的对」这三条**没有真机证据** |

最后一行是这次验收唯一没做完的部分：它要在 magpie 的窗口里点，而 magpie 的 CLI 在这台
机器上不给列表。下次要补，就手动开 magpie 点一下，把三件事拍下来。

## 没做的（别当成做了）

- 没在 magpie 窗口里点开过页面（见上）。
- `magpie plugin --json` 在这台机器上是空的，`magpie plugin add` 打完也不退出——那一步
  的验收只能靠 `plugins.json` 本身，上面记的就是它。
- 隔离实例（3436）上没验「点重启 pid 变」：它没有装 service，点了也没有服务管理器
  可以叫。重启那一项是在正式实例（3426）上验的。

