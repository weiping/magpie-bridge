# SP15：分类器走 magpie 路由组（mb-classifier）

> 状态：待作者确认。

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
- **组的硬约束**：`g.Decides()`（`decide_group.go:38`）= `len(Members) > 0 && 每个成员 decidingMember`——**成员必须全是 decision model**（Jev/Clef/Nimble/Tev 等）。`typesafe/jev-latest` 是（`decidingMember` → `IsDecider` / provider `DecidesModel`）。
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
