# SP15：分类器走 magpie 路由组（mb-classifier）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `classifier` 从「模型 id」扩成「模型 id 或 magpie 路由组名」；`mbridge router init` 建一个 `mb-classifier` 决策组（成员 `typesafe/jev-latest`），并把 `router.json` 的 `classifier` 默认写成 `mb-classifier`，用户以后在 magpie 里加成员即可换分类器。

**Architecture:** 一个判定函数 `isClassifierGroup` 定「组 vs 模型」，三处消费它：`NewClassifier` 的 `jev` 开关、`AskVia` 的分流、`classifySource` 的归类。组名走 magpie 的 `/v1/systemone` 决策组端点（magpie 原生支持，请求时解析成员、逐个 failover、body 透传），body 的 `model` 补成 `group/<id>`。init 侧把 `mb-classifier` 加进建组列表，typesafe 没配时跳过建组并把 classifier 回退 `local`。

**Tech Stack:** Go 1.26（无新依赖）。不碰页面插件、`internal/proxy`、三个客户端。

**Spec:** `docs/superpowers/specs/2026-10-24-magpie-bridge-sp15-classifier-group-design.md`

## Global Constraints

- **`isClassifierGroup` 是「组 vs 模型」的唯一事实来源**（spec §4.1），且**必须排除 `""` 和 `"local"`**：`c == "" || c == "local"` 时返回 `false`（它们不含 `/`，不挡会误判成组）。三处消费点都调它，不许各自再判断。
- **组名发 `/v1/systemone`**（spec §4.2），body 的 `model` 写 `group/<id>`（裸组名补 `group/` 前缀）；`typesafe/` → System One（现状）；其它含 `/` 的 `provider/model` → chat（现状）。
- **`classifySource` 组名报 `"jev"`**（spec §4.2）：走的就是 Jev 的 System One 两问。
- **mb-classifier 成员硬编码 `typesafe/jev-latest`、routing `order`**（spec §4.3）；`served` 里没有 `typesafe/jev-latest` 时：amber warning「mb-classifier 跳过：magpie 没有 provider 服务 typesafe/jev-latest」、不建组、`classifier` 回退写 `"local"`。
- **`Config.defaults()` 保持 `"local"`**（spec §4.4）：只有 `init` 写 `mb-classifier`；加载老配置的兜底哨兵不变。
- **mbridge 不预检组是否存在/是否全 decision model**（spec §5）：分类失败自然暴露、fallback `default_tier`。mbridge 与 magpie 的全部往来只能在 `internal/magpie`（AGENTS.md 契约）。
- 每个任务一个提交；测试命令 `go vet ./... && go test -race ./...`。

## Review Focus

1. **`"local"`/`""` 被误判成组**（会让现状的 chat 404 变成 systemone 404）→ Task 1：`TestIsClassifierGroup` 断言 `isClassifierGroup("local") == false`、`isClassifierGroup("") == false`。
2. **裸组名没补 `group/` 前缀就发出去**（magpie `GroupFor` 按名字查有歧义）→ Task 1：`TestAskViaRoutesGroupToSystemOne` 断言 body 的 `model == "group/mb-classifier"`，不是 `"mb-classifier"`。
3. **组名走了 chat 端点**（chat 对 decision 组 400）→ Task 1：`TestAskViaRoutesGroupToSystemOne` 断言请求路径是 `/v1/systemone`，不是 `/v1/chat/completions`。
4. **typesafe 没配时 init 硬失败**（`magpie group add` 拒绝它不认识的 model）→ Task 2：`TestRouterInitSkipsClassifierGroupWhenTypesafeMissing` 断言 init 不报错、不建组。
5. **classifier 写了 `mb-classifier` 但组没建成**（分类 404 得更隐蔽）→ Task 2：同一测试断言回退时 `router.json` 的 `classifier == "local"`，不是 `"mb-classifier"`。

---

## File Structure

```
internal/router/
  classify.go   + isClassifierGroup；NewClassifier 的 jev 判断扩成「jevClassifier 或组」
  ask.go        + AskVia 分流加「组 → System One + 补 group/ 前缀」
  api.go        + classifySource 扩成「jevClassifier 或组 → "jev"」
  classify_test.go / ask_test.go / api_test.go   + 三个测试
cmd/mbridge/
  router_cli.go          + init 建 mb-classifier 组 + 默认 classifier + typesafe 回退
  router_cli_test.go     + 更新现有断言 + 新增 typesafe 回退测试
```

### Task 1: 分类器认组名（isClassifierGroup + 三处消费）

**Files:**
- Modify: `internal/router/classify.go`（新增 `isClassifierGroup`；`NewClassifier` 的 `jev` 判断）
- Modify: `internal/router/ask.go`（`AskVia` 分流）
- Modify: `internal/router/api.go`（`classifySource`）
- Test: `internal/router/classify_test.go`（`TestIsClassifierGroup`）
- Test: `internal/router/ask_test.go`（`TestAskViaRoutesGroupToSystemOne`，复用现有 `fakeMagpie`）
- Test: `internal/router/api_test.go`（`TestClassifySourceGroupIsJev`）

**Interfaces:**
- Consumes: 无（第一个任务）。
- Produces: `isClassifierGroup(c string) bool` —— spec §4.1 的唯一事实来源；Task 2 的 `routerInit` 不直接调它，但 classifier 字段的语义由此定义。

- [ ] **Step 1: 写失败测试 `TestIsClassifierGroup`**（`internal/router/classify_test.go`）

```go
func TestIsClassifierGroup(t *testing.T) {
	for _, c := range []string{"group/mb-classifier", "mb-classifier", "mbridge"} {
		if !isClassifierGroup(c) {
			t.Errorf("isClassifierGroup(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"", "local", "typesafe/jev-latest", "openrouter/anthropic/claude-sonnet-5.5"} {
		if isClassifierGroup(c) {
			t.Errorf("isClassifierGroup(%q) = true, want false", c)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/router -run TestIsClassifierGroup`
Expected: FAIL — `undefined: isClassifierGroup`

- [ ] **Step 3: 实现 `isClassifierGroup(c string) bool`** 在 `internal/router/classify.go`（`jevClassifier` 常量旁边）

spec §4.1 的精确逻辑：

```go
func isClassifierGroup(c string) bool {
	if c == "" || c == "local" {
		return false
	}
	return strings.HasPrefix(c, "group/") || !strings.Contains(c, "/")
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/router -run TestIsClassifierGroup`
Expected: PASS

- [ ] **Step 5: 写失败测试 `TestAskViaRoutesGroupToSystemOne`**（`internal/router/ask_test.go`，复用 `fakeMagpie`）

```go
func TestAskViaRoutesGroupToSystemOne(t *testing.T) {
	f := &fakeMagpie{jev: `{"answers":{"tier":{"choice":"performance","confidence":0.9},"dissatisfied":{"noul":0.1}}}`}
	c := f.start(t)
	v, err := NewClassifier(testConfig("mb-classifier"), AskVia(c)).Classify(context.Background(),
		Question{Message: "redesign the storage layer", Criteria: testCriteria, PreviousTier: TierBalanced})
	if err != nil {
		t.Fatal(err)
	}
	if v.Tier != TierPerformance {
		t.Fatalf("verdict %+v", v)
	}
	if len(f.paths) != 1 || f.paths[0] != "/v1/systemone" {
		t.Fatalf("paths %v", f.paths)
	}
	var body struct {
		Model string `json:"model"`
	}
	json.Unmarshal([]byte(f.bodies[0]), &body)
	if body.Model != "group/mb-classifier" {
		t.Fatalf("model in body %q", body.Model)
	}
}
```

- [ ] **Step 6: 跑测试确认失败**

Run: `go test ./internal/router -run TestAskViaRoutesGroupToSystemOne`
Expected: FAIL — 组名现在走 plain → chat，`paths` 是 `/v1/chat/completions` 而非 `/v1/systemone`

- [ ] **Step 7: 实现两处**——`NewClassifier` 的 `jev`（`classify.go:72`）和 `AskVia` 分流（`ask.go`）

`NewClassifier`：`jev` 从 `cfg.Classifier == jevClassifier` 扩成 `cfg.Classifier == jevClassifier || isClassifierGroup(cfg.Classifier)`。

`AskVia`：在现有 `typesafe/` 判断之前加组分支——`isClassifierGroup(model)` 时，若 `model` 无 `group/` 前缀则补上，把 body 的 `model` 写成那个值，走 `c.SystemOne`（复用现有的 System One 分支：读 body 的 map、写 `m["model"]`、`json.Marshal`、`c.SystemOne`）。

- [ ] **Step 8: 跑测试确认通过**

Run: `go test ./internal/router -run TestAskViaRoutesGroupToSystemOne`
Expected: PASS

- [ ] **Step 9: 写失败测试 `TestClassifySourceGroupIsJev`**（`internal/router/api_test.go`）

```go
func TestClassifySourceGroupIsJev(t *testing.T) {
	if classifySource("mb-classifier") != "jev" {
		t.Errorf("classifySource(mb-classifier) = %q, want jev", classifySource("mb-classifier"))
	}
	if classifySource("typesafe/jev-latest") != "jev" {
		t.Errorf("classifySource(typesafe/jev-latest) = %q, want jev", classifySource("typesafe/jev-latest"))
	}
	if classifySource("deepseek/deepseek-chat") != "llm" {
		t.Errorf("classifySource(deepseek/deepseek-chat) = %q, want llm", classifySource("deepseek/deepseek-chat"))
	}
}
```

- [ ] **Step 10: 跑测试确认失败**

Run: `go test ./internal/router -run TestClassifySourceGroupIsJev`
Expected: FAIL — `classifySource("mb-classifier")` 现在返回 `"llm"`

- [ ] **Step 11: 实现 `classifySource`（`internal/router/api.go:348`）**

从 `classifier == "typesafe/jev-latest"` 扩成 `classifier == jevClassifier || isClassifierGroup(classifier)`。

- [ ] **Step 12: 跑测试确认通过**

Run: `go test ./internal/router -run TestClassifySourceGroupIsJev`
Expected: PASS

- [ ] **Step 13: 全量测试并提交**

```bash
go vet ./... && go test -race ./...
git add internal/router/classify.go internal/router/ask.go internal/router/api.go internal/router/classify_test.go internal/router/ask_test.go internal/router/api_test.go
git commit -m "feat: classifier accepts a magpie routing group name (mb-classifier)"
```

### Task 2: init 建 mb-classifier 组 + 默认 classifier

**Files:**
- Modify: `cmd/mbridge/router_cli.go`（`routerInit`：建组列表加 `mb-classifier`、typesafe 回退、`Config.Classifier` 写 `mb-classifier`）
- Test: `cmd/mbridge/router_cli_test.go`（更新 `TestRouterInitCnWritesGroupsAndConfig` 断言；新增 `TestRouterInitSkipsClassifierGroupWhenTypesafeMissing`）

**Interfaces:**
- Consumes: 无（`init` 只建组 + 写字符串，不调 `isClassifierGroup`）。
- Produces: init 后 magpie 有 `mb-classifier` 组（成员 `typesafe/jev-latest`、routing `order`），`router.json` 的 `classifier` 是 `mb-classifier`；typesafe 没配时是 `local`。

- [ ] **Step 1: 更新 `TestRouterInitCnWritesGroupsAndConfig`（`router_cli_test.go:31`）反映新行为**

四处改动：组列表断言加 `mb-classifier`；`wantRouting` 加 `"mb-classifier": "order"`；加一行 `f.called("group", "add", "mb-classifier", "models=typesafe/jev-latest", "routing=order")` 断言；`cfg.Classifier != "local"` 改成 `cfg.Classifier != "mb-classifier"`。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./cmd/mbridge -run TestRouterInitCnWritesGroupsAndConfig`
Expected: FAIL — `mb-classifier` 组不存在、`cfg.Classifier` 仍是 `"local"`（Step 3 还没做）

- [ ] **Step 3: 实现「typesafe 配了」的路径**（`router_cli.go`）

`Config.Classifier` 从 `"local"` 改成 `"mb-classifier"`；`groups` 切片加 `{"mb-classifier", []string{"typesafe/jev-latest"}, "order"}`（成员硬编码，不走 `preset.Resolve`）。typesafe 没配时的回退留给 Step 7。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./cmd/mbridge -run TestRouterInitCnWritesGroupsAndConfig`
Expected: PASS

- [ ] **Step 5: 写失败测试 `TestRouterInitSkipsClassifierGroupWhenTypesafeMissing`**（`router_cli_test.go`）

```go
func TestRouterInitSkipsClassifierGroupWhenTypesafeMissing(t *testing.T) {
	f := withFakeMagpie(t, "deepseek/deepseek-flash", "deepseek/deepseek-v4-pro") // 无 typesafe
	if err := routerInit([]string{"--preset", "cn", "--groups-only"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := groupMembers(f, "mb-classifier"); ok {
		t.Fatal("mb-classifier 不该在 typesafe 没配时建")
	}
	cfg, err := router.Load(routerJSONPath(), "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Classifier != "local" {
		t.Fatalf("classifier 应回退 local，got %q", cfg.Classifier)
	}
}
```

- [ ] **Step 6: 跑测试确认失败**

Run: `go test ./cmd/mbridge -run TestRouterInitSkipsClassifierGroupWhenTypesafeMissing`
Expected: FAIL — 现状 typesafe 没配时 `GroupAdd` 会失败让 `routerInit` 报错（`magpie group add` 拒绝它不认识的 model）

- [ ] **Step 7: 实现 typesafe 回退**（`router_cli.go`）

在 `groups` 循环之前：`!slices.Contains(served, "typesafe/jev-latest")` 时，amber warning「mb-classifier 跳过：magpie 没有 provider 服务 typesafe/jev-latest」、不把 `mb-classifier` 加进 `groups`、`Classifier` 回退写 `"local"`。用 `slices.Contains`，不要为这一个判断导出 `router` 包里的 `servedID`。

- [ ] **Step 8: 跑测试确认通过**

Run: `go test ./cmd/mbridge -run TestRouterInitSkipsClassifierGroupWhenTypesafeMissing`
Expected: PASS

- [ ] **Step 9: 全量测试并提交**

```bash
go vet ./... && go test -race ./...
git add cmd/mbridge/router_cli.go cmd/mbridge/router_cli_test.go
git commit -m "feat: init makes mb-classifier group and defaults classifier to it"
```

## 完成后

- ✅ **2026-10-24（只验了一半）**：跑一遍真实 init 验证（magpie 网关起来时）：`mbridge router init --preset cn --groups-only`，确认 `magpie group mb-classifier` 显示成员 `typesafe/jev-latest`，`router.json` 的 `classifier` 是 `mb-classifier`。**做过了**：`router.json` 的 `classifier` 确实是 `mb-classifier`（mtime `2026-10-24 13:35:24`），magpie 里 `mb-classifier` 组在、`routing order`、4 个成员。**但此刻组里没有可服务的成员**（四个成员全是 `not served now, skipped`，组 `no member ready`），分类这条路还没验通。证据与原因见 spec §8.5。
- ✅ **2026-10-24**：回填 spec 的「实现决定」：见 spec §8（typesafe 回退的写法、`classifySource` 的取值、`ModelList` 的 deciders 头、native init 的跨包破例都在那里）。
- ✅ **2026-10-24**：在 README 补一句：`mb-classifier` 组里加成员必须都是 decision model（Jev/Clef/Nimble/Tev…），加进普通 chat model 会让 `/v1/systemone` 返回 400。见 README「换分类器组里的模型」。
