# SP15：分类器走 magpie 路由组（mb-classifier）

> 状态：已实施（2026-10-24：`b307e663`、`93d8e8db`、`8d73cc44`、`ba479ce9`），
> 真机 init 未验（见 §8.5）。

## 1. 问题

`router.json` 的 `classifier` 现在是一个**模型 id**：`typesafe/jev-latest`（Jev 的 System One 两问）或任何 `provider/model`（plain 两问）。`mbridge router init` 默认写的是 `"local"`——一个无效哨兵，发到 magpie 的 chat 端点会 404，分类失败后静默 fallback 到 `default_tier`（`internal/router/classify.go` 只挡了空串，没挡 `"local"`）。

用户要换分类器、或给分类器加 failover，得手改 `router.json`，没法像路由组那样在 magpie 里加成员。要的是：

- `mbridge router init` 建一个 **`mb-classifier` 路由组**，默认成员 `typesafe/jev-latest`；
- `router.json` 默认 `classifier: "mb-classifier"`（组名，不再是模型 id）；
- `mb-classifier` 可由用户在 magpie 里加成员（换分类器、加 failover 都在 magpie 里做）。

## 2. Approaches

- **A：把 `group/mb-classifier` 发给 chat 端点**，让 magpie 的组路由在请求时解析成员。✗ 死路。magpie 的 chat 端点对 decision 组明确拒绝（`internal/gateway/gateway.go` 的 chat model 解析：`the routing group %s holds decision models … asked at /v1/systemone; it holds no conversation`，400）。而且就算组里换成 chat model，分类也退化成 plain 提示词，Jev 两问特化丢失。
- **B：mbridge 自己读组成员**（`magpie group` / `/v1/models`），解析出实际模型后仍按该模型的前缀走 System One 或 chat。✓ 可行，但 mbridge 得在 magpie 里改了成员之后重新读，存在滞后，复杂且多余。
- **C（选）：把 `group/mb-classifier` 原样发给 `/v1/systemone`**。magpie 的 System One 端点原生支持 decision 组（见 §3），请求时解析成员、逐个 failover、body 透传。既保留 Jev 两问特化，又白得组内 failover，mbridge 不用自己解析成员。

选 C：唯一不用自己解析成员、又不丢 Jev 特化的路，且 magpie 自己就把 `classifier=group/<id>` 当一等公民（`magpie group` 帮助里 `classifier=<provider/model>|group/<id>`）。

## 3. 调研：magpie 有 System One 端点，且原生支持 decision 组

本机 magpie GUI 未开（网关 3425 无监听，只有 `magpie mcp image` 子进程和 plugin-host），在线实测做不了；以下是从最新 magpie 源码（`github.com/yetone/magpie`）读到的白盒结论，覆盖全部版本、不依赖本机状态：

- **端点存在**：`POST /v1/systemone`（`gateway.go:712` 注册，`serveSystemOne`）。
- **接受组**：`serveSystemOne` 先调 `decisionGroupOf(q.Model)`——接受 `group/<id>` 前缀（`GroupPrefix = "group/"`）**或裸组名**（`GroupFor` 按名字找），命中即走 `serveDecisionGroup` → `serveDecideTries`：组内每个 decision model 的 keys/accounts 依次问，retryable 的失败 rest、下一个顶上，`withModel(body, c.model)` 只把 body 的 `model` 换成实际成员、**questions/state 原样透传**。
- **chat 端点是死路**：chat 的 model 解析对 `g.Decides()` 的组返回 400「holds no conversation」。
- **组的硬约束**：`g.Decides()`（`decide_group.go:38`）= `len(Members) > 0 && 每个成员 decidingMember`——**成员必须全是 decision model**（Jev/Clef/Nimble/Tev 等）。`typesafe/jev-latest` 是（`decidingMember` → `IsDecider` / provider `DecidesModel`）。混进一个 chat model，这个组就不再是 decision 组：`decisionGroupOf`（`decide_group.go:49-53`）对**显式** `group/<id>` 的请求回 400「the routing group %s holds models that hold conversations; System One asks a group of decision models, or a decision model as "provider/model"」；裸组名则落到「按模型解析」那条路，报「knows no model」。mbridge 发的是 `group/<id>`，所以踩的是前者（对着 magpie main 源码核对，2026-10-24）。
- **旁证**：magpie 组自身就支持 `classifier=group/<id>`。

结论：mbridge 把 `group/mb-classifier` 发 `/v1/systemone` 即可，Jev 两问 body 完全兼容。

## 4. 设计

### 4.1 `classifier` 值的三类语义

| 值 | 含义 | 走法 |
| --- | --- | --- |
| `""` / `"local"` | 未配置（哨兵） | 现状不变：分类禁用/失败后 fallback `default_tier` |
| `group/<id>` 或裸组名（**不含 `/`**） | 分类器路由组 | **`/v1/systemone`** + Jev 两问 body，model 写 `group/<id>` |
| `provider/model`（含 `/`） | 模型 id | 现状不变：`typesafe/…` → System One；其它 → chat plain |

判定函数（新增，唯一事实来源）：

```go
// isClassifierGroup 是 classifier 指向一个 magpie 路由组而非模型。
// "local" 和 "" 是「未配置」哨兵，不是组名（它们不含 "/"，不挡会误判成组）。
func isClassifierGroup(c string) bool {
    if c == "" || c == "local" {
        return false
    }
    return strings.HasPrefix(c, "group/") || !strings.Contains(c, "/")
}
```

裸组名（`mb-classifier`，无 `/`）判为组；发给 magpie 时补成 `group/mb-classifier`，避开 `GroupFor` 按名字查的歧义。

### 4.2 分类器调用链

- **`NewClassifier`（`classify.go:71`）**：`jev` 的判断从 `cfg.Classifier == jevClassifier` 扩成 `cfg.Classifier == jevClassifier || isClassifierGroup(cfg.Classifier)`——组名走 Jev 的 System One 两问（`classifyJev`），因为组里默认是 Jev，且两问 body 是通用 System One 协议，任何 decider 都答。
- **`AskVia`（`ask.go`）**：分流从「`typesafe/` → System One，否则 Chat」扩成：
  1. `isClassifierGroup(model)` → 补 `group/` 前缀（若无），写进 body 的 `model`，发 `POST /v1/systemone`；
  2. `typesafe/` 前缀 → System One（现状）；
  3. 其它 → Chat（现状）。
- **`classifySource`（`api.go:348`）**：从 `== "typesafe/jev-latest"` 扩成 `== jevClassifier || isClassifierGroup(...)`，组也报 `"jev"`（走的是 Jev 两问）。事件的 `source` 字段、状态页的归类随之正确。

`classifyPlain`、`askPlain` 不动：组名不会走到 plain 路径。

### 4.3 `mbridge router init` 建组

`routerInit`（`router_cli.go`）的 `groups` 切片加第 5 项，成员硬编码（不走 `preset.Resolve`，因为它不是 `<p>/` 模式）：

```go
{"mb-classifier", []string{"typesafe/jev-latest"}, "order"},
```

`GroupAdd` 走 `magpie group add mb-classifier models=typesafe/jev-latest routing=order`。与 tier 组一致，`--force` 前已存在则保留（`kept`）。

**typesafe 没配时**（`served` 里没有 `typesafe/jev-latest`）：magpie 会拒绝组（`group add` 命名它不认识的 model）。这时**不阻断 init**，而是 amber warning 一句「mb-classifier 跳过：magpie 没有 provider 服务 typesafe/jev-latest」，并把 `router.json` 的 `classifier` 回退写 `"local"`（组都建不出来，写组名只会让分类 404 得更隐蔽）。tier 组照常建。

### 4.4 `router.json` 的默认 classifier

- `routerInit` 写的 `Config` 字面量：`Classifier: "local"` → `Classifier: "mb-classifier"`（typesafe 没配时按 §4.3 回退 `"local"`）。
- `Config.defaults()`（`config.go:121`）**保持 `"local"`**：那是「加载到没有 classifier 字段的老配置」时的兜底哨兵，语义是「未配置」，不该默认指向一个可能不存在的组。只有 `init` 写 `mb-classifier`。

## 5. 不做

- 不自己解析组成员（magpie 在请求时解析，§3）。
- 不做运行时「组是否存在 / 是否全 decision model」的预检；分类失败自然暴露（fallback `default_tier`，`classifyErr` 照记）。
- 不改 plain 路径（非 typesafe 的 `provider/model`）。
- 不改 review 的分类器调用（`classify.go` 的 `Review` 走同一套，`jev` 判定改了自动跟着走）。
- 不改 `/v1/bridge/*` 接口、三个客户端插件、`internal/proxy`。
- **例外（实现时补）**：`routerInit --native`（SP12）的 `classifier=` 规则原来照抄 `cfg.Classifier`；SP15 把默认值改成组名后，裸组名会被 magpie 拒绝（magpie 对非 `group/` 前缀的 classifier 走 `Resolve`，报「knows no model」）。所以 native init 构造 rule 时对裸组名补 `group/` 前缀——这是 SP15 引入的回归修复，不是扩展 native。

## 6. 测试

Go（`go vet ./... && go test -race ./...`）：

1. `isClassifierGroup`：`group/x`、`mb-classifier`（无 `/`）→ 组；`typesafe/jev-latest`、`openrouter/anthropic/claude`（含 `/`）→ 模型；`""`/`"local"` → 模型（`isClassifierGroup` 为 false，走现状 plain 路径，行为不变）。
2. `NewClassifier`：`classifier = "mb-classifier"` 时 `jev` 为真；`typesafe/jev-latest` 仍为真；`provider/model` 仍为假。
3. `AskVia`：组名发 `POST /v1/systemone`（非 chat），body `model` 是 `group/mb-classifier`（裸名补了前缀）；`typesafe/…` 仍 System One；`provider/model` 仍 chat。
4. `classifySource`：组名 → `"jev"`；`typesafe/jev-latest` → `"jev"`；`provider/model` → `"llm"`。
5. `routerInit`（复用现有 golden/断言）：建组列表含 `mb-classifier`（成员 `typesafe/jev-latest`、routing `order`）；`--force` 前已存在则 kept；typesafe 没配时 warning + 不建组 + router.json `classifier` 回退 `"local"`；写出的 router.json `classifier: "mb-classifier"`。

## 7. 顺序

单条链，一个提交：`isClassifierGroup` + 调用链（`classify.go`/`ask.go`/`api.go`）→ `routerInit` 建组 + 默认 classifier → 测试。改完回填本 spec 的实现决定（若 builder 有偏离，记进 spec 而非另立口子）。

## 8. 实现决定（2026-10-24 回填）

实现按 §4 走。§7 说的「单条链，一个提交」没照做：实际四个提交——
`b307e663`（判定 + 三处消费）、`93d8e8db`（init 建组 + 默认 classifier）、
`8d73cc44`（native init 补 `group/` 前缀）、`ba479ce9`（`ModelList` 带 deciders 头）。
后两个是执行中发现的问题修复，各自独立成提交。

1. **`isClassifierGroup` 与 §4.1 一字不差**，三处消费点（`NewClassifier`、
   `AskVia`、`classifySource`）都调它。`AskVia` 写进 body 的是**补过前缀的引用**
   （裸名 → `group/<name>`），magpie 靠 body 的 `model` 字段解析组；`typesafe/`
   那条路不变。测试：`TestIsClassifierGroup`、`TestAskViaRoutesGroupToSystemOne`、
   `TestClassifySourceGroupIsJev`。

2. **§4.3 的 typesafe 回退用「不追加」实现**：组列表里根本没有 `mb-classifier`
   （而不是追加之后再撞 magpie 的拒绝），所以不会有 `group add` 去失败；同时打印
   amber warning、`router.json` 的 `classifier` 写 `"local"`。效果与 §4.3 一致。
   测试：`TestRouterInitSkipsClassifierGroupWhenTypesafeMissing`。

3. **`served` 必须能看见 decision model，否则 SP15 静默失效（原稿没写，实现时发现）**：
   magpie 的 `/v1/models` 默认**不列 decision model**，`typesafe/jev-latest` 不在里面
   → 上一条永远走回退分支 → `mb-classifier` 永远不建、`classifier` 永远写 `"local"`，
   SP15 看起来什么都没发生。故 `magpie.Client.ModelList` 必须带 `X-Magpie-Deciders: 1`
   （`ba479ce9`，测试 `TestModelListSendsDecidersHeader`）。**改这段时别把这个头去掉**：
   去掉不会编译失败、也不会有测试变红，只会让 init 静默不建组。

4. **native init 自己判「裸组名」，没调 `isClassifierGroup`（跨包，计划里那条约束在这里破例）**：
   `isClassifierGroup` 在 `internal/router` 里未导出，`cmd/mbridge` 够不着，所以
   `router_native.go` 自己判「不含 `/` 就是裸组名」并补 `group/` 前缀（`8d73cc44`，
   测试 `TestRouterInitNativeUsesGroupRefForClassifier`）。**这条路径只在
   `cfg.Classifier` 非空非 `local` 时可达**——`router_native.go:87` 先把 `""`/`"local"`
   早退提示掉，所以 `group/local` 不会被写进 magpie 规则
   （测试 `TestRouterInitNativeSkipsWhenClassifierIsLocal`）。改代码时别把那个早退挪到后面。

5. **真机 init 验过了（2026-10-24）**，但只验了一半：

   - `mbridge router init --preset cn --groups-only` 跑过。`~/.config/magpie-bridge/router.json`
     的 `classifier` 是 `mb-classifier`（文件 mtime `2026-10-24 13:35:24`，比最后一个提交
     `ba479ce9` 晚两分钟）；magpie 里 `mb-classifier` 组在，`routing order`，4 个成员
     （`typesafe/jev-preview`、`typesafe/jev-latest`、`cloudflare-jev/@cf/cloudflare/clef`、
     `cloudflare-jev/@cf/cloudflare/clef-flash`——全是 decision model，符合 §3 的硬约束）。
   - **但此刻组里没有可服务的成员**：magpie 把四个成员全标成 `not served now, skipped`，
     组本身是 `no member ready`；`mbridge router status` 最近三次决策是 `R8-default` +
     `R7-carry` ×2，也就是**分类这条路当前是断的**，轮子落回默认档。
   - 查到的原因：`magpie provider models typesafe` 直接报 `TLS handshake timeout`
     （api.typesafe.ai 拉不动，缓存的列表是 25 小时前的），两个 typesafe 成员因此不被服务；
     两个 `cloudflare-jev/@cf/cloudflare/clef*` 成员与 cloudflare-jev provider 暴露的 id
     完全一致，magpie 仍报不服务——**这一条没查清**（它的列表标的是
     `2 exposed of 3 from models.dev`，像是来自目录而不是实时探测）。

   结论：**「init 建组 + 默认写 classifier」这一步有真机证据了**；**「用这个组真的分类出档位」
   还没有**，别把它当成端到端通了。

6. **复核（同日第二遍，回答上面「这一条没查清」）**：

   - **`not served now, skipped` 是 magpie CLI 的显示问题，不是它服务不了。** `magpie groups`
     和 `magpie group <id>` 用的是「给 agent 的模型目录」（`provider.Served()`），而 decision
     model 和只做决策的 provider 是**故意**排除在那个目录外的（`entriesOf` 跳过
     `DecideOnly()` 的 provider 与 `isDecision` 的模型，`buildGroupEntries` 跳过 decision
     组）。于是**任何** decision 组都会把每个成员显示成 `not served now, skipped`、组显示
     `no member ready`——跟 Cloudflare 还是 TypeSafe、跟健康度都无关。证据：同一天
     `~/.config/magpie/routing/2026-10-24.jsonl` 里 `group/mb-classifier` 被
     `typesafe/jev-preview` 答出 200 共 16 次、`cloudflare-jev/@cf/cloudflare/clef` 1 次，
     而 CLI 同时说四个成员都不服务。上游 `main` 的 `internal/cli/groups_cli.go` 同样没有
     decision 组的分支，所以不是版本落后。
   - **Cloudflare Workers AI 到 Jev 是通的**：`magpie provider test cloudflare-jev` →
     `✓ decide 1038 ms · typesafe/jev`；直接 `POST /v1/systemone` 问
     `cloudflare-jev/typesafe/jev` → 200（`jev-1.13.0`），`@cf/cloudflare/clef` → 200（0.7s）。
   - **真正断的是分类链路：TypeSafe 慢 + 组的顺序。** 当时 `api.typesafe.ai` TLS 握手 17.8s，
     偶发 `unexpected EOF` / `http2: client connection lost`，单次 30–36s；组存的顺序是
     `typesafe/jev-preview → typesafe/jev-latest → cloudflare clef → clef-flash`，而
     `/v1/systemone` 的组是**严格按序、单次尝试没有自己的超时**（`serveDecideTries` →
     `postDecide` 只用调用方 ctx）。mbridge 的 `classify_timeout_ms`（当时 8000）一到就取消
     整轮，快的 Cloudflare 成员轮不上——22:56 那条 cloudflare try 的错就是 `context canceled`。
     TypeSafe「慢但成功」（9.8s / 18.8s）时不会 rest，所以每轮都先撞它；当天 37 次决策里
     18 次 `R8-default` + 7 次 `R7-carry`。
   - 要让这条链真通：把组里第一个成员换成够快的，例如
     `magpie group set mb-classifier models=cloudflare-jev/@cf/cloudflare/clef,cloudflare-jev/@cf/cloudflare/clef-flash,typesafe/jev-latest routing=order`；
     或把 `classify_timeout_ms` 提到 30000 以上（慢轮会明显变慢）。
   - 附带一条：`cloudflare-jev` provider 的 `models` 只勾了 clef 两个，`typesafe/jev`
     （Workers AI 上的 Jev 本体）只在 `pickedFrom` 里、没在 `models` 里，所以
     `magpie provider models cloudflare-jev` 显示 `not shown typesafe/jev · not picked`；
     要用得先 `magpie provider models cloudflare-jev +typesafe/jev`。

7. **真机：把 `cloudflare-jev/typesafe/jev` 放在组首位后，还有一处不兼容（已修，`classify.go`）**

   用户 2026-10-24 23:30 左右把组改成
   `cloudflare-jev/typesafe/jev → typesafe/jev-latest → typesafe/jev-preview → clef → clef-flash`，
   并把 `cloudflare-jev` 的 `typesafe/jev` 勾进 `models`。组本身因此变快变稳（20 次实测里
   19 次 `cloudflare-jev/typesafe/jev` 答 200，p50 567ms，max 1.96s；1 次 Cloudflare 慢到
   8.0s 被 mbridge 的超时打断）。但**分类仍然失败**：`router.jsonl` 出现新错
   `router: jev chose "", not a tier`（2026-10-24T15:33:23Z）。

   原因：Workers AI 的响应是**两层信封**——
   `{"result":{"state":"Completed","result":{"answers":…}}}`——magpie 的
   `provider.DecideAnswer` 只拆一层（`decide_wire.go` 的 Cloudflare 分支单次
   `return env.Result`），所以到 mbridge 手里仍是
   `{"state":"Completed","result":{"answers":…}}`；而 `classifyJev` 只在**顶层**找
   `answers`，取到空 tier → 报错 → 落回默认档。`@cf/cloudflare/clef` 没这问题（它自己不带
   信封），所以此前只测 clef 时看不出来。

   修法（mbridge 侧，`internal/router/classify.go`）：新增 `jevAnswers(reply)`，顶层有
   `answers` 就用，否则跟着 `result` 往里走（最多 4 层），`jevReply` 改为直接描述 answers
   对象。测试 `TestJevClassifyThroughARelaysEnvelope`（用真机抓到的双层 body 与 Cloudflare
   原始 body）、`TestJevClassifyRefusesAReplyWithoutAnswers`。

   上游可一并修：`DecideAnswer` 的 Cloudflare 分支改成循环拆信封（`while result is an
   object and no answers yet`），这样任何客户端都不用知道中继套了几层。
   已写好并未推送：`/tmp/magpie-work` 分支 `fix/cloudflare-jev-answer-envelope`
   （基于 yetone/magpie `a6399dd`），测试 `TestCloudflareAnswerPeelsTheEnvelopes`
   在旧代码上红、新代码上绿，`go test ./internal/provider/ ./internal/gateway/` 全绿。
   推上游要先 fork；当前 weiping 账号下只有 mbridge 自己的仓库是 yetone/magpie 的 fork
   （`weiping/<repo>`，parent = yetone/magpie），往它推 magpie 分支会污染 mbridge 仓库，
   所以先征求用户意见再发 PR。

   还有一处天花板：8s 的 `classify_timeout_ms` 一到，mbridge 取消整轮，magpie 就**没法**
   轮到下一个成员（日志里那次的错是 Cloudflare 的 `context canceled`）。想让它真的
   failover，把超时提到 ≥10000，让慢的首个成员失败后 clef（≈0.7s）还来得及答。
