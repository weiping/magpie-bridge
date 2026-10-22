# SP14：mbridge 的状态页，和 magpie 窗口里的用量速览 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 两件互不相干的东西，各归各家。① mbridge 自己服务一个网页 `http://127.0.0.1:3426/status`：进程与依赖、生效中的配置、正在选档的会话、最近判档、分类器、分档统计、两臂对比与校准，外加一个「重启 mbridge」按钮。② magpie 窗口里加一个页面插件，只显示 magpie 记下的用量（调用 / 错误 / Token / 费用 + 模型表）。

**Architecture:** ①走 mbridge 自己的 HTTP：`GET /v1/bridge/router`（已有的那个端点）扩字段，`GET /status` 服务一张同源的网页，网页每 5 秒拉一次那个端点；聚合（区间统计）由 serve 里一个后台聚合器每 30 秒增量算好。重启走 `POST /v1/bridge/restart`，**只认同源**。②走 magpie 的页面沙盒：`magpie.read("usage:summary")`，一个字都不从 mbridge 拿。

**Tech Stack:** Go 1.26（无新依赖）、magpie 页面沙盒 API（`_magpie/bridge.js`）、bun（页面测试）、纯静态 HTML/CSS/JS（无构建步骤）。

**Spec:** `docs/superpowers/specs/2026-10-22-magpie-bridge-sp14-magpie-page-design.md`

## Global Constraints

- **spec §2 是硬边界**：magpie 页面插件在 `iframe sandbox="allow-scripts"` 里，`connect-src 'none'`、`frame-src 'none'`，可读只有 `usage:summary` 和 `providers:list`。所以 **状态、报表、重启不进 magpie 页面**（spec §6）。magpie 页面里 mbridge 的状态只以「可选择的文字」给出地址，不做成链接（导航自己是允许的，但那是单程票，见 spec §2 末尾）。
- **spec §7 已经把 options 那条路记成「不走」**：本 SP 不写 options 通道、不写发布器、不往用户的 `plugins.json` 里塞快照。Task 1 撤掉此前按旧方案写的那些代码。
- **`POST /v1/bridge/restart` 必须卡同源**（spec §3.3）：`Sec-Fetch-Site: same-origin` 或 `Origin` 的主机等于 mbridge 自己的 `listen` 主机才算数，其它一律 `403`，**先回 403 再说要不要重启**，不许靠「没这个头」放行。本地非浏览器进程不受影响（它们可以自己跑 `mbridge service restart`）。
- **mbridge 与 magpie 的全部往来只能在 `internal/magpie`**（AGENTS.md 契约）。本 SP 只用现成的 `magpie.Client.Health` / `.Version` / `.Usage`。
- **页面上的数不许比实际多**：
  - `Event.LatencyMs` 是 Decide 全程（**含分类**）；分类耗时靠新加的 `Event.ClassifyMs`；两者都拿不到时显示「—」，不许把 `LatencyMs` 说成「分类耗时」。
  - `Classifier` 为空 = 这一轮**没问**分类器（R1 / R2 / 压缩提示 / `default`）。判「没问」只看 `Classifier` 空不空，不看那几个 `*float64` 是不是 nil。没问的轮次**不进**覆盖率、压缩率、不满意率的分母。
  - `Event.Source`（`jev`/`llm`/`rule`/`default`/`hint`/`held`）与 `Event.Classifier`（模型 id）是两个字段：「哪个模型判的」用 `Classifier`。
  - 校准表的分母是「分类器的档被采纳了」的轮次（`calibrate.go:88`）；空段画「—」，不是 `0%`。
  - PR 合并率**不含 `gh`**（页面进程内拿不到），输出必须带 `pr_states: "feedback-only"` 标记。
  - `Experiment.Salt` 是实验分组的私密量，**不显示**。
- **诚实边界写进页面**（spec §3.1 末）：harness 是客户端自报的（原样显示，别塞进「其它」）；会话列表只到 mbridge 记得住的为止（进程重启即空）；项目级 criteria 看不到；magpie 没价的模型计入 `UnpricedRequests`，不按 0 算进成本。
- **档位归因不做**（spec §4.3）：magpie 记的是最终模型，`usage:summary` 里分不出档位。
- TypeScript 优先；函数不超过 50 行；不留兼容代码；每个任务一个提交；测试命令 `go vet ./... && go test -race ./...`，页面测试 `bun test clients/magpie-page`。
- **Task 1 是删除**，执行前要作者点头（AGENTS.md：涉及删除重构必须先问）。

## Review Focus

1. **重启端点能被任意网页触发** → Task 8：`TestRestartRefusesCrossSite`（跨域一条 403，且**断言服务管理器一次都没被叫过**——LESSONS.md：断言「没做事」，不能只断言返回值）。
2. **卡片把两个 key 混成一张** → Task 3 / Task 5：`TestLiveKeepsBothCardsOfOneSession`、`TestLiveKeepsASessionIDWithAPipe`（session id 本身含 `|` 时不切成两半）。
3. **把「没测到」画成 0** → Task 6：`TestTheCoverageDenominatorIsTheTurnsThatAsked`、`TestTheConfidenceMeanSkipsTheTurnsThatDidNotAsk`；Task 7：页面把三态画出来（有值的格子 / `—` / 整行灰掉）。
4. **分类耗时被谎报** → Task 3：`TestClassifyMsIsZeroOnOldEvents`、`TestLatencyIsAtLeastClassifyMs`。
5. **页面插件被加了 `main`/`exports`** → Task 9：`TestThePackageHasNoMainOrExports`（magpie 的 `SetPageOptions` 只对纯窗口插件不重启 host；加了 `main`，用户每点一次周期切换就可能把所有插件重启一轮）。
6. **区间聚合被算成「此刻」** → Task 6：`TestTheWindowOnlyChangesTheAggregateSections`（A–E 不随 `window` 变）。

---

## File Structure

```
internal/router/
  events.go        + Event.ClassifyMs
  session.go       + LiveSession, Sessions.Live, SessionOfKey
  api.go           + Deps 的环境字段、LiveCards、router 端点扩字段
  status.go        （新）StatusView / PageConfig / ServiceStatus —— 页面与 status --json 共用
  window.go        （新）区间聚合器（window 块）
  live_test.go / status_test.go / window_test.go
internal/restart/  （新）POST /v1/bridge/restart + 同源判断
  restart.go
  restart_test.go
internal/status/   （新）GET /status 的网页（一张自包含 page.html）
  page.html
cmd/mbridge/
  service_cli.go   + service restart
  status_cli.go    + status --json
  serve_cli.go     + 挂 /status 与 /v1/bridge/restart、起聚合器
  main.go          + 用法文案
clients/magpie-page/     magpie 页面插件（只用 usage:summary）
  package.json  README.md  page/{index.html,app.js,view.js,style.css,icon.svg}  page.test.mjs
```

### Task 1: 撤掉走不通的那条路

**Files:**
- Delete: `internal/page/page.go`、`internal/page/page_test.go`
- Delete: `internal/magpie/plugin_options.go`、`internal/magpie/plugin_options_test.go`
- Delete: `cmd/mbridge/page_cli.go`
- Delete: `docs/superpowers/specs/2026-10-22-magpie-bridge-sp14-page-plugin-design.md`（旧方案的 spec，已被本 plan 的 spec 取代）
- Modify: `internal/router/api.go`（删掉 `RecentDecisions()`；`d.status` 自己拿锁读 `d.decisions`）

**Interfaces:**
- Consumes: 无。
- Produces: 仓库里只剩 spec §8 的那条路（mbridge 自己的 HTTP + magpie 用量页）。

- [ ] **Step 1: 确认作者点头**

把这四个文件要删的事说清楚（写的是什么、为什么 spec §7 决定不走），得到明确的「删」再动。

- [ ] **Step 2: 删文件并回退 `internal/router/api.go` 里的 `RecentDecisions()`**

```bash
git rm -r internal/page internal/magpie/plugin_options.go internal/magpie/plugin_options_test.go cmd/mbridge/page_cli.go
git rm docs/superpowers/specs/2026-10-22-magpie-bridge-sp14-page-plugin-design.md
```

- [ ] **Step 3: 确认仓库是绿的**

Run: `go vet ./... && go test ./...`
Expected: PASS（`internal/router/api.go` 里没有 `RecentDecisions` 的引用；`cmd/mbridge` 里没有 `page_cli.go` 的引用）。

- [ ] **Step 4: 提交**

```bash
git add -A
git commit -m "refactor: drop the options channel the spec does not take"
```

### Task 2: `mbridge service restart` 与 `mbridge status --json`

**Files:**
- Modify: `cmd/mbridge/service_cli.go`（`serviceCmd` 加 `case "restart"`）
- Modify: `cmd/mbridge/status_cli.go`（`statusCmd` 加 `--json`）
- Modify: `cmd/mbridge/main.go`（用法文案）
- Test: `cmd/mbridge/service_cli_test.go`
- Test: `cmd/mbridge/status_cli_test.go`

**Interfaces:**
- Consumes: `service.Manager.Restart(ctx context.Context) error`（`internal/service/service.go:189`）、`service.Manager.Status(ctx) (running bool, detail string, err error)`（同文件 `:230`）、`router.Load`、`magpie.Client.Health`/`.Version`/`.Groups`/`.ModelList`。
- Produces:
  - CLI `mbridge service install|uninstall|status|restart`；`restart` 打印 `mbridge restarts now`。
  - CLI `mbridge status --json`；输出是**一个 JSON 对象**，形状固定：
    ```go
    type statusReport struct {
        ConfigPath   string   `json:"config_path"`
        ConfigError  string   `json:"config_error,omitempty"`
        MagpieURL    string   `json:"magpie_url"`
        MagpieOK     bool     `json:"magpie_ok"`
        MagpieVersion string  `json:"magpie_version,omitempty"`
        MissingGroups []string `json:"missing_groups,omitempty"`
        Running      bool     `json:"running"`
        Listen       string   `json:"listen,omitempty"`
        Decisions    []struct {
            Session string `json:"session"`
            Tier    string `json:"tier"`
            Reason  string `json:"reason"`
        } `json:"decisions"`
    }
    ```
    `statusCmd` 现在的每一段结论（router.json 读没读出来、magpie 通不通、缺哪些组、mbridge 在不在跑、最近判档）在 JSON 里**一项一项对应**，不新增结论。

- [ ] **Step 1: 写失败测试**

`cmd/mbridge/service_cli_test.go`：

```go
func TestServiceRestartRunsTheSupervisor(t *testing.T) {
	home := t.TempDir()
	var calls [][]string
	old := newService
	newService = func() *service.Manager {
		return &service.Manager{GOOS: "darwin", Home: home, ConfigDir: home, UID: 501,
			Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				calls = append(calls, append([]string{name}, args...))
				return nil, nil
			}}
	}
	t.Cleanup(func() { newService = old })
	// 没装 unit 时 Restart() 自己是 no-op，所以先造一个 unit 文件
	path, _ := service.Unit("darwin", home, home, "")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("x"), 0o644)

	out := captureStdout(t, func() {
		if err := serviceCmd([]string{"restart"}); err != nil { t.Fatal(err) }
	})
	if len(calls) != 1 { t.Fatalf("calls %v", calls) }
	if !strings.Contains(out, "restarts now") { t.Fatalf("out %q", out) }
}
```

再加三条：三平台各一条 golden（同 `internal/service/testdata/*.golden` 的形状：darwin → `launchctl kickstart -k gui/<uid>/…`，linux → `systemctl --user restart …`，windows → `schtasks /End /TN …`）；`mbridge service` 拿到第三个子命令时报错并打用法。

`cmd/mbridge/status_cli_test.go`：`TestStatusJSONIsOneObject`——`statusCmd([]string{"--json"})` 的输出能被 `json.Unmarshal` 进 `statusReport`，且 `magpie_ok` / `running` 这两个键**存在**（不许用 `omitempty` 把 false 藏掉）；`TestStatusJSONAndTextAgree`——同一份假环境下，`--json` 的 `missing_groups` 与文本版打出的组数一致。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./cmd/mbridge/ -run 'TestServiceRestart|TestStatusJSON'`
Expected: FAIL —— `serviceCmd` 对 `"restart"` 落到用法；`statusCmd` 见到 `--json` 报「status takes no arguments」。

- [ ] **Step 3: 实现 `service restart`**

```go
case "restart":
	if err := m.Restart(ctx); err != nil { return err }
	fmt.Println(green.Render("✓"), "mbridge restarts now")
	return nil
```

- [ ] **Step 4: 实现 `status --json`**

`cmd/mbridge/status_cli.go`：把现在那段「收集结论」的逻辑抽成一个 `collectStatus(ctx, cfg) statusReport`，文本版和 JSON 版共用它；`--json` 时 `json.NewEncoder(os.Stdout).Encode(rep)`，**不打印任何别的东西**（一个 JSON 对象独占 stdout）。

- [ ] **Step 5: 运行确认通过**

Run: `go vet ./... && go test -race ./cmd/mbridge/`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add cmd/mbridge/service_cli.go cmd/mbridge/status_cli.go cmd/mbridge/main.go cmd/mbridge/service_cli_test.go cmd/mbridge/status_cli_test.go
git commit -m "feat: add mbridge service restart and status --json"
```

### Task 3: `Event.ClassifyMs` 与 `Sessions.Live()`

**Files:**
- Modify: `internal/router/events.go`（`Event` 加一个字段）
- Modify: `internal/router/api.go`（`Decide` 里记录分类耗时）
- Modify: `internal/router/session.go`（`LiveSession`、`Live`、`SessionOfKey`）
- Test: `internal/router/events_test.go`、`internal/router/session_test.go`

**Interfaces:**
- Consumes: 现有 `Event`、`Sessions`（`internal/router/session.go`）、`sessionClock`（测试里挪时间用的包级变量）。
- Produces:
  - `Event.ClassifyMs int \`json:"classify_ms,omitempty"\`` —— 分类器那次调用自己的耗时，成功和失败都记；老 `router.jsonl` 里没有这个字段，读出来是 0。
  - `type LiveSession struct { Key, Session string; Tier Tier; Turns, Tools, ToolFailures, EscalatedLeft, LowerStreak int; Pinned bool; Parent string; LastAt time.Time }`
  - `type LiveCard struct { LiveSession; Harness string `json:"harness"`; Agent string `json:"agent"`; Group string `json:"group"`; Reason string `json:"reason"`; TierConfidence *float64 `json:"tier_confidence"`; LatencyMs int `json:"latency_ms"`; ClassifyMs int `json:"classify_ms"` }` —— **类型在这里定**（Task 4 的 `StatusView` 要用它），**填充在 Task 5**。
  - `func (s *Sessions) Live(window time.Duration) []LiveSession` —— 返回最近 `window` 内活动过、且判过档（`tier.Tier != ""`）的会话，按 `lastAt` 倒序，最多 20 条，空结果返回 `[]` 不是 `nil`。
  - `func SessionOfKey(key string) string` —— key → session 的换算（spec §3.1 坑 1）。

- [ ] **Step 1: 写失败测试**

`internal/router/session_test.go`：

```go
func TestLiveKeepsBothCardsOfOneSession(t *testing.T) {
	s := NewSessions()
	now := time.Now()
	s.Commit("sess|aaaaaaaaaaaaaaaabbbbbbbb", TurnState{Tier: TierBalanced})
	s.Commit("sess|ccccccccccccccccdddddddd", TurnState{Tier: TierFast})
	live := s.Live(time.Minute)
	if len(live) != 2 { t.Fatalf("live %+v", live) }
	if live[0].Session != live[1].Session { t.Fatalf("sessions %+v", live) }
}

func TestLiveKeepsASessionIDWithAPipe(t *testing.T) {
	s := NewSessions()
	s.Commit("sess|ion|pipe", TurnState{Tier: TierFast})
	live := s.Live(time.Minute)
	if len(live) != 1 || live[0].Session != "sess|ion|pipe" {
		t.Fatalf("live %+v", live)
	}
}

func TestLiveSkipsEntriesWithoutALastAt(t *testing.T) {
	// InheritFrom 造出来的条目没有 lastAt，不许渲染成「1677 年前」
}
```

`internal/router/events_test.go`：`TestClassifyMsIsZeroOnOldEvents`（一行没有 `classify_ms` 的 JSON 解出来是 0）；`TestLatencyIsAtLeastClassifyMs`（`LatencyMs >= ClassifyMs`，`LatencyMs` 是 Decide 全程）。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/router/ -run 'TestLive|TestClassifyMs|TestLatencyIs'`
Expected: FAIL —— `Live` / `LiveSession` / `SessionOfKey` 未定义；`Event.ClassifyMs` 未定义。

- [ ] **Step 3: 实现 `Event.ClassifyMs`**

`internal/router/events.go`：`Event` 加 `ClassifyMs int \`json:"classify_ms,omitempty"\``。

`internal/router/api.go`：`Decide` 里分类那次调用前后各取一次 `time.Now()`，成功和失败都把差值毫秒写进事件的 `ClassifyMs`。**只加字段，不改任何行为**（不因为耗时改变任何判定）。

- [ ] **Step 4: 实现 `Sessions.Live` 与 `SessionOfKey`**

`internal/router/session.go`：

- `SessionOfKey(key)`：从**最后一个** `|` 切，后缀是 24 位小写十六进制才算 `firstWords`，否则整串就是 session。**不许用 `strings.Split(key, "|")[0]`**（session id 客户端可控、最长 128 字符，里面可能有 `|`）。
- `Live(window)`：遍历 `s.m`，**只认 `tier.Tier != ""` 的条目**（这就是 `UpdatedAt` 的判据），`lastAt` 零值的跳过，`lastAt` 距今超过 `window` 的跳过；按 `lastAt` 倒序，最多 20 条；空结果返回 `[]`。
- 过滤发生在**截断之前**：先按窗口过滤，再取前 20（`Sessions` 只在条目 ≥1024 时才淘汰闲置超 24 小时的那些，spec §3.1 要求过滤在 `evictLocked` 的截断之前）。
- `Tools` / `ToolFailures` 来自 `Stats(session)`（**仅 gateway 路径有**，`/turn` 路径是 0，页面显示「—」）；`Pinned` 来自 `Pinned(session)`；`Parent` 来自 `ParentOf(session, firstWords)`；`EscalatedLeft` / `LowerStreak` 来自 `Get(key)`；`Turns` 来自 `Turn(key)`。

- [ ] **Step 5: 运行确认通过**

Run: `go vet ./... && go test -race ./internal/router/`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/router/events.go internal/router/api.go internal/router/session.go internal/router/events_test.go internal/router/session_test.go
git commit -m "feat: record the classify time and list the live sessions"
```

### Task 4: `GET /v1/bridge/router` 扩字段（A · B）

**Files:**
- Create: `internal/router/status.go`
- Modify: `internal/router/api.go`（`Deps` 加环境字段；`d.status` 的响应扩字段）
- Modify: `cmd/mbridge/serve_cli.go`（`mbridgeDeps` 填环境字段）
- Test: `internal/router/status_test.go`

**Interfaces:**
- Consumes: Task 3 的 `Event.ClassifyMs`；`service.Manager.Status(ctx)`；`magpie.Client.Health/Version`。
- Produces:
  - `Deps` 新增字段（都可注入，测试里不碰真的 magpie / service）：
    ```go
    Version    string        // mbridge 的版本
    StartedAt  time.Time     // serve 起来的时刻
    Listen     string        // 真正在服务的地址
    Service    func(context.Context) (bool, string, error) // 装没装 / 在不在跑 / 平台自己的说法
    MagpieInfo func(context.Context) (string, bool)        // 版本 / 通不通
    ```
  - `internal/router/status.go`：
    ```go
    type ServiceStatus struct { Installed bool `json:"installed"`; Running bool `json:"running"`; Detail string `json:"detail"` }
    type StatusView struct {
        Version       string        `json:"version"`
        PID           int           `json:"pid"`
        UptimeS       int64         `json:"uptime_s"`
        Listen        string        `json:"listen"`
        ListenPending bool          `json:"listen_pending"`
        MagpieVersion string        `json:"magpie_version"`
        MagpieOK      bool          `json:"magpie_ok"`
        Service       ServiceStatus `json:"service"`
        ConfigError   string        `json:"config_error"`
        Live          []LiveCard    `json:"live"`
        Config        PageConfig    `json:"config"`
    }
    func (d *Deps) StatusView() StatusView
    ```
  - `PageConfig`：`router.json` **应用默认值之后**的全局值，字段固定为 `router_group`、`default_tier`、`classifier`、`classify_timeout_ms`、`escalate_turns`、`cache_ttl_seconds`、`thresholds`（`tier_min`/`dissatisfied_min`/`review_min`/`review_confidence_min`/`compact_min`）、`review`（`mode`/`timeout_ms`/`max_answer_chars`）、`experiment`（`enabled`/`router_percent`/`control_tier`，**没有 salt**）、`fixed_agents`、每档的 `group`/`claude_alias`/`criteria`。
    `func (c Config) PageView() PageConfig`

- [ ] **Step 1: 写失败测试**

`internal/router/status_test.go`：

```go
func TestTheStatusViewReportsTheConfigWithDefaults(t *testing.T) {
	cfg := Config{Tiers: map[Tier]TierCfg{TierFast: {Group: "group/fast"}}}
	d := &Deps{Config: cfg, Version: "0.3.2", StartedAt: time.Now().Add(-90 * time.Second),
		Listen: "127.0.0.1:3426",
		Service: func(context.Context) (bool, string, error) { return true, "running", nil },
		MagpieInfo: func(context.Context) (string, bool) { return "0.1.1318", true }}
	v := d.StatusView()
	if v.UptimeS < 89 || v.UptimeS > 95 { t.Fatalf("uptime %d", v.UptimeS) }
	if v.ListenPending { t.Fatal("listen pending") }
	if v.Config.DefaultTier != TierBalanced { t.Fatalf("default tier %q", v.Config.DefaultTier) }
	if v.Config.Experiment.Salt != "" { t.Fatal("the salt must not leave the process") }
}
```

再加：`TestTheStatusViewFlagsAListenThatNeedsARestart`（`Deps.Listen` 与 `Deps.Config.Listen` 不同时 `listen_pending` 为真）；`TestTheStatusViewReportsTheConfigError`（`router.ConfigError()` 非空时原样进 `config_error`）。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/router/ -run TestTheStatusView`
Expected: FAIL —— `StatusView` 未定义。

- [ ] **Step 3: 实现 `internal/router/status.go`**

- `Config.PageView()`：拷贝 `Config` 后**逐字段**挑出来填 `PageConfig`（不整个 marshal `Config`，否则 `experiment.salt` 会漏出去）。`salt` 永远不进 `PageConfig`。
- `Deps.StatusView()`：`PID` 用 `os.Getpid()`；`UptimeS` 用 `time.Since(d.StartedAt)`；`ListenPending` = `d.Listen != d.Config.Listen`；`Service` 调 `d.Service(ctx)`（装没装 = `detail != ""` 或 running，**装没装**由 `service.Manager` 的 unit 文件存在与否决定，实现里要能从 `detail` 判断）；`MagpieInfo` 同理；`ConfigError` 取 `router.ConfigError()`。

- [ ] **Step 4: 把 router 端点的响应扩上**

`internal/router/api.go` 的 `d.status`：现在的响应是 `{valid, router_group, turn_budget_ms, tiers, experiment, decisions}`，**保留这些键**，**加上** `StatusView` 里的字段（`version`/`pid`/`uptime_s`/`listen`/`listen_pending`/`magpie_version`/`magpie_ok`/`service`/`config_error`/`live`/`config`）。`decisions` 那个 ≤20 的环不变。

- [ ] **Step 5: 在 serve 里填环境字段**

`cmd/mbridge/serve_cli.go` 的 `mbridgeDeps`：`Deps{Version: version, StartedAt: time.Now(), Service: ..., MagpieInfo: ...}`。**`Listen` 用真正在服务的那一个**（不是 `cfg.Listen`）：reloader 的 `build()` 里拿到 deps 之后赋 `deps.Listen = r.listen`；`cfg.Listen` 留在 `deps.Config.Listen` 里。两者不同 → `listen_pending` 为真（配置改了要重启才生效，reloader 已经会 log 一句）。`Service` 用 `service.New().Status(ctx)`；`MagpieInfo` 用 `mc.Health(ctx)` + `mc.Version(ctx)`。

- [ ] **Step 6: 运行确认通过**

Run: `go vet ./... && go test -race ./internal/router/ ./cmd/mbridge/`
Expected: PASS。

- [ ] **Step 7: 提交**

```bash
git add internal/router/status.go internal/router/api.go internal/router/status_test.go cmd/mbridge/serve_cli.go
git commit -m "feat: report mbridge's process and config on the router endpoint"
```

### Task 5: 正在选档的会话（卡片，C 的四个坑）

**Files:**
- Modify: `internal/router/api.go`（`Deps` 记最近一条 decide 事件；加 `LiveCards`）
- Test: `internal/router/live_test.go`

**Interfaces:**
- Consumes: Task 3 的 `Sessions.Live`、`SessionOfKey`。
- Produces:
  - ```go
    func (d *Deps) LiveCards(window time.Duration) []LiveCard
    ```
  - `Deps` 新增一个内存索引：`live map[string]Event`（key → 最近一条 `decide` 事件），在 `d.log()` 里更新。

- [ ] **Step 1: 写失败测试**

`internal/router/live_test.go`：

```go
func TestTheCardTakesTheHarnessFromTheLastDecide(t *testing.T) {
	d := &Deps{Config: Config{Tiers: map[Tier]TierCfg{TierBalanced: {Group: "group/mb-balanced"}}}}
	d.Sessions = NewSessions()
	d.Sessions.Commit("sess|aaaaaaaabbbbbbbb", TurnState{Tier: TierBalanced})
	d.log(Event{Kind: "decide", Session: "sess", Harness: "codex", Agent: "main", Tier: TierBalanced,
		Reason: "R6-adopt", LatencyMs: 41})
	cards := d.LiveCards(time.Minute)
	if len(cards) != 1 { t.Fatalf("cards %+v", cards) }
	if cards[0].Harness != "codex" || cards[0].Group != "group/mb-balanced" { t.Fatalf("card %+v", cards[0]) }
}

func TestTheCardSaysUnknownWhenTheEventIsGone(t *testing.T) {
	// 找不到事件 → harness 是「未知」，不是空串（页面负责把空串画成「未知」）
}

func TestTheCardShowsNoToolsOnTheHarnessPath(t *testing.T) {
	// /turn 路径的卡片工具数是 0（页面显示「—」），gateway 路径才有 24/2
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/router/ -run 'TestTheCard'`
Expected: FAIL —— `LiveCards` 未定义。

- [ ] **Step 3: 实现 `Deps.live` 与 `LiveCards`**

- `Deps` 加 `live map[string]Event`；`d.log(ev)` 里，`ev.Kind == "decide"` 时按 `ev.Session + "|" + firstWords` 之外的既有 key 规则更新。**key 的来源用 `DecideInput.Key`**：`d.log` 现在只收 `Event`，所以在 `Decide` 里把 key 一起传进去（`d.logEvent(ev, key)` 之类），**不要从 `Event.Session` 反推 key**（gateway 路径一个 session 有多张卡）。
- `LiveCards(window)`：拿 `d.Sessions.Live(window)`，对每一条按 key join `d.live` 里的事件：`Harness`/`Agent`/`Reason`/`TierConfidence`/`LatencyMs`/`ClassifyMs`；找不到就留空（页面显示「未知」）。`Group` 来自 `d.Config.Tiers[tier].Group`。
- **继承来的档可能比事件新**：`InheritFrom` 只搬状态不发事件，所以卡上的 reason 不一定描述当前那一轮 —— 页面照实显示，不要在数据层「修正」。

- [ ] **Step 4: 运行确认通过**

Run: `go vet ./... && go test -race ./internal/router/`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/router/api.go internal/router/live_test.go
git commit -m "feat: join the last decision onto the live session cards"
```

### Task 6: 区间聚合（window 块，F · G）

**Files:**
- Create: `internal/router/window.go`
- Modify: `internal/router/api.go`（端点认 `?window=` 与 `?since=`；挂聚合器）
- Modify: `cmd/mbridge/serve_cli.go`（起聚合器）
- Modify: `cmd/mbridge/router_cli.go`（`readRouterEvents` 移到 `internal/router`，CLI 改调它）
- Test: `internal/router/window_test.go`

**Interfaces:**
- Consumes: `router.jsonl`（`fsutil.ConfigDir()/router.jsonl`）、`magpie.Client.Usage(ctx, period)`、`router.Aggregate`、`router.Calibrate`。
- Produces:
  - ```go
    type WindowView struct {
        Kind      string            `json:"kind"`      // 1d | 7d | 30d
        Since     time.Time         `json:"since"`
        Coverage  Coverage          `json:"coverage"`  // {asked, total}
        Classify  ClassifyStats     `json:"classify"`  // {errors, error_kinds, failures, asked}
        Tiers     map[string]int    `json:"tiers"`
        Reasons   map[string]int    `json:"reasons"`
        Harnesses map[string]int    `json:"harnesses"`
        Arms      map[string]int    `json:"arms"`
        Report    Report            `json:"report"`
        Calibrate []ScoreCalibration `json:"calibrate"`
        PRStates  string            `json:"pr_states"`  // 固定 "feedback-only"
    }
    type Aggregator struct { ... }
    func NewAggregator(eventsPath string, usage func(context.Context, string) ([]magpie.UsageRow, error), every time.Duration) *Aggregator
    func (a *Aggregator) Run(ctx context.Context)
    func (a *Aggregator) View(kind string) WindowView
    ```
  - `func ReadEvents(path string) ([]Event, error)`（从 `cmd/mbridge/router_cli.go` 的 `readRouterEvents` 搬过来，CLI 改调它，行为不变）。
  - 端点认 `?window=1d|7d|30d`（默认 `7d`）与 `?since=<RFC3339>`。**`window` 只影响聚合那几节**；A–E（进程、配置、live 卡片、判档）永远是「此刻」。

- [ ] **Step 1: 写失败测试**

`internal/router/window_test.go`：

```go
func TestTheWindowOnlyChangesTheAggregateSections(t *testing.T) {
	a := NewAggregator("", nil, time.Hour)
	a.set("1d", WindowView{Kind: "1d", Tiers: map[string]int{"fast": 1}})
	a.set("30d", WindowView{Kind: "30d", Tiers: map[string]int{"fast": 9}})
	if a.View("1d").Kind != "1d" { t.Fatal("kind") }
}

func TestTheReasonHistogramSkipsAuxAndPassthrough(t *testing.T) {
	// aux（Codex 线程标题）和 proxy_passthrough 不进 decide 的分母
}

func TestTheConfidenceMeanSkipsTheTurnsThatDidNotAsk(t *testing.T) {
	// 一条 tier_confidence 为 nil 的事件不进均值；一条 Classifier 为空的事件整行灰掉，
	// 也不进压缩率 / 不满意率的分母
}

func TestTheCoverageDenominatorIsTheTurnsThatAsked(t *testing.T) {
	// 覆盖率的分母是「问过分类器的轮次 ÷ decide 轮次」，
	// 不是「有分数的轮次」，也不是「全部轮次」
}

func TestTheCalibrationTableLeavesOutTheTurnsThatWereNotFollowed(t *testing.T) {
	// R2-plan-mode / R6-lower 不进 tier 表的任何一段，但进 dissatisfied 表
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/router/ -run 'TestTheWindow|TestTheReason|TestTheConfidence|TestTheCalibration'`
Expected: FAIL —— `Aggregator` 未定义。

- [ ] **Step 3: 实现 `internal/router/window.go`**

- `Aggregator` 每 `every`（serve 里给 30 秒）跑一次：读 `router.jsonl` 的**新增行**（记住上次读到哪一行，别整份重读），按 `Kind == "decide"` 的轮次算直方图（tiers / reasons / harnesses / arms）、覆盖率（`Classifier` 非空的轮次 ÷ decide 轮次）、分类失败（`classify_error` 非空的轮次 ÷ 问过分类器的轮次），再调 `router.Aggregate` 算两臂报告、`router.Calibrate` 算三张校准表。
- **失败原因分三类**：`timeout (classify_timeout_ms): …` / `canceled: the client stopped waiting: …` / 其它原文。第三类**原样出**，不要归进前两类。
- `Report.PRStates` 用会话自报的 `pr_merged` feedback 兜底（`hasMergedFallback` 那条路），`WindowView.PRStates` 固定写 `"feedback-only"`。
- `SampleShort` 为真时，页面只出成本与手动切模型率，`MergedZ` 不出现（`Report` 里本来就没有它被清掉的做法，页面按 `SampleShort` 决定画不画）。
- 空段（`N == 0`）的 `UnderRate` 是 0，页面画「—」；**后端不要把它变成一个假的百分数**。

- [ ] **Step 4: 把端点接上**

`internal/router/api.go` 的 `d.status`：读 `r.URL.Query().Get("window")`（默认 `7d`，只认 `1d`/`7d`/`30d`，别的当 `7d`）与 `?since=`；把 `window` 块放进响应（`window` 不可用时给 `null`）。`Deps` 加 `Windows func(kind string) WindowView`（可注入，测试里给假的）。

`cmd/mbridge/serve_cli.go`：起一个 goroutine 跑 `aggregator.Run(ctx)`，每 30 秒一次。

- [ ] **Step 5: 运行确认通过**

Run: `go vet ./... && go test -race ./...`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/router/window.go internal/router/api.go internal/router/window_test.go cmd/mbridge/serve_cli.go cmd/mbridge/router_cli.go
git commit -m "feat: aggregate the router's own windows for the status page"
```

### Task 7: `GET /status` 的状态页

**Files:**
- Create: `internal/status/page.html`
- Create: `internal/status/status.go`
- Modify: `internal/proxy/proxy.go`（挂 `GET /status`；`Handler` 多收一个 `listen`）
- Test: `internal/status/status_test.go`

**Interfaces:**
- Consumes: `GET /v1/bridge/router` 的响应（**同源**，所以不受 §3.3 的同源检查挡）。
- Produces:
  - `internal/status/status.go`：
    ```go
    package status
    //go:embed page.html
    var pageHTML []byte
    func Page() []byte
    ```
  - 路由：`GET /status` → `internal/status.Page()`（`Content-Type: text/html; charset=utf-8`）。
  - **不占 `/`**：`/` 是反向代理的根，mbridge 自己要往那儿过的请求不能被自己的页面截走。

- [ ] **Step 1: 写失败测试**

`internal/status/status_test.go`：

```go
func TestThePageFetchesOnlyTheRouterEndpoint(t *testing.T) {
	html := string(Page())
	for _, m := range regexp.MustCompile(`fetch\(\s*["']([^"']+)`).FindAllStringSubmatch(html, -1) {
		if !strings.HasPrefix(m[1], "/v1/bridge/router") { t.Fatalf("fetch %q", m[1]) }
	}
	if !strings.Contains(html, "/v1/bridge/router") { t.Fatal("no router fetch") }
}
```

再加两条：`TestThePageRefreshesEveryFiveSeconds`（源码里有 `5000` 的刷新间隔）；`TestThePageHasNoInlineEventHandlers`（没有 `onclick=` 这类属性；脚本是内联的也没关系——这张页面不是 magpie 沙盒里的那张）。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/status/`
Expected: FAIL —— 包不存在。

- [ ] **Step 3: 实现 `internal/status/page.html` 与 `status.go`**

一张自包含的 HTML（内联 CSS 与 JS，无外部资源）。内容按 spec §3.1 的 A–H：

- **A 进程与依赖**：版本、pid、uptime、实际 listen、配置里的 listen（不一致时顶部一条醒目提示「配置里的 listen 要重启才生效」）、service 装没装 / 在不在跑、magpie 地址与版本、`router.json` 路径与读不出来的原文。
- **B 生效中的配置**：全局值逐项；`experiment` 只显示 `enabled`/`router_percent`/`control_tier`；`fixed_agents` 默认五条；`thresholds` 缺的那项显示默认值。页面自己声明「项目级文件可覆盖每档 criteria」。
- **C 正在选档的会话**：最近 5 分钟有请求的会话，一会话一卡（**按 key，所以一个 session 可能有多张卡**），最新在前，最多 20 张，多出来写「还有 N 个」；一条都没有时显示「现在没有会话在走 mbridge」，不是空的 `[]`。
  - `harness`/`agent` 找不到事件时显示「未知」，不是空白。
  - 工具 / 失败：`/turn` 路径是「—」（灰字说明），gateway 路径才有 `24/2`；满足 `calls >= 3 && failures*2 >= calls` 时标红。
  - 升档还剩几轮、连降计数为 0 时不显示；手动 pin 显示「已手动 pin」；父会话折叠在末行。
  - reason 照 spec §3.1 那张表翻译（`R1-fixed`/`R2-plan-mode`/`R9-compact`/`R3-escalate`/`R3-tools`/`R3-review`/`R4-escalation-hold`/`R6-adopt`/`R6-lower`/`R6-keep`/`R7-carry`/`R8-default`/`hint`/`R-held`/`codex-title`），中英各一套。
- **D 最近判档**：每行时间、harness、agent、判定的档、**分类器建议的档**（分开两列）、reason、source、classifier、confidence、latency_ms；判定档与建议档不同的行要一眼能看出来。
- **E 分类器**：配置（模型、`classify_timeout_ms`、`thresholds.tier_min`）＋「不是每一轮都问」的说明 ＋ 最近若干轮每轮三格（tier 置信度 / dissatisfied / compact）＋ 覆盖率 ＋ 失败（三类分开给建议）＋ 耗时 ＋ 校准三张表。
  - 三态画得出来：**一个数（带条）** / **`—`（问了但没记这一项）** / **整行灰掉（没问）**。
  - 耗时那一格：`classify_ms` 有值显示它；没有（旧事件）显示「—」；**不许把 `latency_ms` 标成「分类耗时」**。
  - 校准表头写清楚分母是「照它办的那些轮次」；空段画「—」；`sample_short` 时不给建议。
- **F 分档统计**：各档轮次与占比、**各 reason 的次数与占比**、各 harness 的轮次与占比、主轮次 vs 子代理、router 臂 vs control 臂（全零的臂不画）、control 臂的影子档分布、hint 命中数。
- **G 两臂对比与校准**：会话数、成本中位数/均值/p90 + bootstrap 95% CI、总成本、cache write 成本（注明是估算）、未计价请求数、开过 PR 的会话、已合并的会话、手动切过模型的会话、两臂合并率的两比例 z；**PR 状态那行标「不含 gh 补全」**；`sample_short` 时只显示成本与手动切模型率。
- **诚实的边界**：spec §3.1 末尾那五条，原样写在页脚。
- 刷新：每 5 秒 `fetch("/v1/bridge/router")` 一次；`fetch` 失败时显示错误，**不白屏**。

- [ ] **Step 4: 挂上路由**

mux 在 `internal/proxy/proxy.go` 的 `Handler` 里建，所以路由挂在那里：`internal/proxy` 直接 import `internal/status`，`Handler` 里 `mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/html; charset=utf-8"); w.Write(status.Page()) })`。
**`Handler` 的签名多一个 `listen string`**：`func Handler(target *url.URL, listen string, deps *router.Deps) http.Handler` —— `router.json` 读不出来时（`deps` 为 nil）也要能把状态页和重启端点挂上，且同源判断要有真实的地址可比。调用方 `cmd/mbridge/serve_cli.go` 的 reloader 在 `build()` 里传 `r.listen`；`internal/proxy` 的测试同步改。

- [ ] **Step 5: 运行确认通过**

Run: `go vet ./... && go test -race ./internal/status/ ./internal/proxy/`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/status cmd/mbridge/serve_cli.go internal/proxy/proxy.go
git commit -m "feat: serve mbridge's status page"
```

### Task 8: `POST /v1/bridge/restart` 与同源判断

**Files:**
- Create: `internal/restart/restart.go`
- Test: `internal/restart/restart_test.go`
- Modify: `internal/proxy/proxy.go`（挂路由）
- Modify: `cmd/mbridge/serve_cli.go`（传 `service.Manager.Restart`）
- Modify: `internal/status/page.html`（按钮接上）

**Interfaces:**
- Consumes: `service.Manager.Restart(ctx context.Context) error`。
- Produces:
  - ```go
    package restart
    // Register mounts POST /v1/bridge/restart. listen is mbridge's own
    // address; do performs the restart.
    func Register(mux *http.ServeMux, listen string, do func(context.Context) error)
    func SameOrigin(r *http.Request, listen string) bool
    ```
  - 路由：`POST /v1/bridge/restart`；同源 → `202` + `{"restarting":true}`；不同源 → `403` + `{"error":"cross-origin request refused"}`，**而且一次都不调 `do`**。

- [ ] **Step 1: 写失败测试**

`internal/restart/restart_test.go`：

```go
func TestRestartRefusesCrossSite(t *testing.T) {
	called := 0
	mux := http.NewServeMux()
	restart.Register(mux, "127.0.0.1:3426", func(context.Context) error { called++; return nil })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, headers := range []map[string]string{
		{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"},
		{"Sec-Fetch-Site": "none"},
		{"Origin": "https://evil.example"},
		{}, // 缺头不放行
	} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/bridge/restart", nil)
		for k, v := range headers { req.Header.Set(k, v) }
		res, err := http.DefaultClient.Do(req)
		if err != nil { t.Fatal(err) }
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("%v → %d", headers, res.StatusCode)
		}
	}
	if called != 0 { t.Fatalf("the restart ran %d times", called) }
}

func TestRestartAllowsSameOrigin(t *testing.T) {
	called := 0
	// Sec-Fetch-Site: same-origin → 202，且 do 被叫了恰好一次
}
```

再加一条 `TestRestartRefusesANonLoopbackHost`：`Host` 不是 `127.0.0.1`/`localhost` 时一律 403（DNS rebinding）。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/restart/`
Expected: FAIL —— 包不存在。

- [ ] **Step 3: 实现 `internal/restart/restart.go`**

```go
func SameOrigin(r *http.Request, listen string) bool {
	host := r.Host
	if !isLoopback(host) { return false }
	if r.Header.Get("Sec-Fetch-Site") == "same-origin" { return true }
	origin := r.Header.Get("Origin")
	if origin == "" { return false }
	u, err := url.Parse(origin)
	if err != nil { return false }
	return u.Host == host || u.Host == listen
}
```

- [ ] **Step 4: 挂上路由并接线**

`internal/proxy/proxy.go` 的 `Handler(target, listen, deps)` 里 `restart.Register(mux, listen, func(ctx) error { return service.New().Restart(ctx) })`（`deps` 为 nil 也要挂）。
`cmd/mbridge/serve_cli.go`：把「重启用的 service manager」注进去（`service.New()` 每次新建即可，它自己找 unit 文件）。

- [ ] **Step 5: 状态页的按钮接上**

`internal/status/page.html`：一个「重启 mbridge」按钮 → `fetch("/v1/bridge/restart", {method:"POST"})` → 202 就显示「已发出重启指令」，403 显示「请求被拒绝（本页不是同源？）」。**按钮点了之后不假装已经重启**：显示的是「已发出指令」，pid 变了才显示「已重启」。

- [ ] **Step 6: 运行确认通过**

Run: `go vet ./... && go test -race ./internal/restart/ ./internal/proxy/`
Expected: PASS。

- [ ] **Step 7: 提交**

```bash
git add internal/restart internal/proxy/proxy.go cmd/mbridge/serve_cli.go internal/status/page.html
git commit -m "feat: add a same-origin restart endpoint"
```

### Task 9: `clients/magpie-page/`（magpie 窗口里的用量速览）

**Files:**
- Create: `clients/magpie-page/package.json`
- Create: `clients/magpie-page/README.md`
- Create: `clients/magpie-page/page/index.html`
- Create: `clients/magpie-page/page/app.js`
- Create: `clients/magpie-page/page/view.js`
- Create: `clients/magpie-page/page/style.css`
- Create: `clients/magpie-page/page/icon.svg`
- Test: `clients/magpie-page/page.test.mjs`

**Interfaces:**
- Consumes: magpie 页面 API：`magpie.ready`、`magpie.options.get()`、`magpie.options.set(obj)`、`magpie.read("usage:summary", {period})`、`magpie.theme` 的 `lang`。
- Produces: `usage:summary` 的形状 `{period, since, calls, errors, input, output, cacheRead, cost, models[{model, calls, input, output, cost}]}`（前 20 个模型）。

- [ ] **Step 1: 写失败测试**

`clients/magpie-page/page.test.mjs`（照 `magpie-community/plugins` 的 `packages/usage/usage.test.mjs` 形状，用 `bun:test`）：

```js
test("the page reads only usage:summary", () => {
  expect(pkg.magpie.permissions).toEqual(["usage:summary"])
  const reads = new Set()
  for (const f of readdirSync(page).filter((f) => f.endsWith(".js"))) {
    for (const m of read(f).matchAll(/magpie\.read\(\s*"([^"]+)"/g)) reads.add(m[1])
    expect(read(f)).not.toMatch(/\bfetch\(|XMLHttpRequest|WebSocket|sendBeacon|EventSource|importScripts/)
  }
  expect([...reads]).toEqual(["usage:summary"])
})

test("the package is a window plugin, so its writes never restart magpie", () => {
  expect(pkg.main).toBeUndefined()
  expect(pkg.exports).toBeUndefined()
})

test("the page loads only its own files", () => {
  const html = read("index.html")
  expect(html).toContain('<script src="_magpie/bridge.js">')
  for (const m of html.matchAll(/<script([^>]*)>([\s\S]*?)<\/script>/g)) {
    expect(m[1]).toMatch(/\ssrc="/)
    expect(m[2].trim()).toBe("")
  }
})
```

再加三条：`view.js` 的纯函数快照（`en`/`zh` 各一份：`tiles` 的四块文案与数值、`rows` 的排序与占比、`count`/`money` 的确切输出）；空数据不崩（`calls: 0` 不出 `NaN%`/`$NaN`；`models: []` 是空表；`usage:summary` 抛错时显示错误而不是白屏）；周期与排序记在 `magpie.options` 里。

- [ ] **Step 2: 运行确认失败**

Run: `bun test clients/magpie-page`
Expected: FAIL —— `clients/magpie-page/package.json` 不存在。

- [ ] **Step 3: 实现 `package.json` 与 `page/index.html`**

`package.json`：`name` 为 `mbridge-page`（发 npm 时是 `@weiping/mbridge-page`），`magpie.page` 为 `{title:"mbridge", titles:{zh:"mbridge","zh-TW":"mbridge",ja:"mbridge",de:"mbridge"}, file:"page/index.html", icon:"page/icon.svg"}`，`magpie.permissions` 为 `["usage:summary"]`，**没有 `main`、没有 `exports`**。

`page/index.html`：`<script src="_magpie/bridge.js">`、`<script src="view.js">`、`<script src="app.js" defer>`；无内联脚本与内联样式；骨架含周期切换（今天 / 7 天 / 30 天）、四块（调用 / 错误占调用比 / Token / 费用）、模型表（按 token·调用·费用排序，带占比条）。

- [ ] **Step 4: 实现 `page/view.js`（纯函数，无 DOM）**

导出（挂在 `globalThis` 上，页面与测试都能用）：`TEXT`（`en`/`zh`）、`tiles(summary, lang)`、`rows(summary, sort, lang)`、`count(n)`、`money(n)`、`pct(n)`。
`count(1234)` → `1,234`；`count(1234567)` → `1.23M`；`money(0)` → `$0`；`money(0.004)` → `<$0.01`；`pct(0)` → `0%`（**不是 `NaN%`**）。

- [ ] **Step 5: 实现 `page/app.js`**

- 每 60 秒 `magpie.read("usage:summary", {period})`；读之前 `await magpie.ready`。
- 周期与排序记在 `magpie.options` 里（`magpie.options.get()` / `set()`）；**只写自己的键**，读到的其它键原样带回。
- 周期和排序的变化要写到 `magpie.options`，**magpie 限每秒一次**（写太快会被拒）。
- 两处必须写明（spec §4.3）：①「这些是 magpie 记下的全部用量。客户端都走 mbridge 时它才等于 mbridge 的用量；你直接用 magpie 的那部分也在里面。」② 状态那节只给 `http://127.0.0.1:3426/status` 的**可选择的文字**（不做成链接），旁边给 `mbridge service restart`。
- **不做档位归因**（magpie 记的是最终模型，`usage:summary` 里分不出档位）。

- [ ] **Step 6: 实现 `page/style.css` 与 `page/icon.svg`**

只用 `--magpie-*` 变量；明暗两套都要对（社区 `packages/usage` 的做法）。

- [ ] **Step 7: 运行确认通过**

Run: `bun test clients/magpie-page`
Expected: PASS。

- [ ] **Step 8: 提交**

```bash
git add clients/magpie-page
git commit -m "feat: add the magpie page plugin for mbridge's usage"
```

### Task 10: README 与真机验收

**Files:**
- Modify: `README.md`（`clients/` 一节补「怎么装」；`mbridge status --json` 与 `/status` 的用法）
- Modify: `docs/superpowers/specs/2026-10-22-magpie-bridge-sp14-magpie-page-design.md`（把实现过程中确认下来的细节写回去：`Event.ClassifyMs` 已加、`Sessions.Live` 的签名、`window` 的聚合节奏、magpie 页面的安装方式）

**Interfaces:**
- Consumes: Task 1–9 定下的所有名字与形状。
- Produces: 无（文档）。

- [ ] **Step 1: 改 README**

- `clients/` 一节：写「`magpie plugin add ./clients/magpie-page`」，以及发布 npm 之后的 `magpie plugin add @weiping/mbridge-page`；写清楚这个页面只有用量速览。
- 新的一节讲 mbridge 自己的状态页：`http://127.0.0.1:3426/status`、`mbridge status`、`mbridge status --json`、`mbridge service restart`。

- [ ] **Step 2: 把 spec 回填**

在 spec 里补：Task 3 给 `Event` 加了 `ClassifyMs`（spec §9.5 已经预置了这条决定，现在它落地了）；`Sessions.Live(window)` 的确切签名与「过滤在截断之前」；聚合器 30 秒一次、页面 5 秒一次；页面路径 `/status`。

- [ ] **Step 3: 跑全部测试**

Run: `go vet ./... && go test -race ./... && bun test clients/magpie-page`
Expected: PASS。

- [ ] **Step 4: 提交**

```bash
git add README.md docs/superpowers/specs/2026-10-22-magpie-bridge-sp14-magpie-page-design.md
git commit -m "docs: document the status page and the magpie page plugin"
```

- [ ] **Step 5: 真机验收（两条都不能省，LESSONS.md）**

- **状态页**：跑 `mbridge serve`，浏览器打开 `http://127.0.0.1:3426/status`。Expected：页面出得来；A–H 各块有内容；改 `router.json` 的档位成员，**5 秒内**更新；点「重启 mbridge」，mbridge 的 pid 变。
- **同源**：在一个真实浏览器的标签里访问 `http://127.0.0.1:3426/status`，点重启 → mbridge 重启；再从 `localhost` 的页面跨域 POST 同一个端点 → **403**，且 mbridge **没有**重启。
- **magpie 页面**（隔离 HOME 装）：`magpie plugin add ./clients/magpie-page`，`magpie plugin --json` 能看到这一行（`plugin list` 这个子命令不存在，用 `--json`）；在 magpie 里点开页面：tab 在侧栏 Plugins 组、标题按窗口语言、明暗两套 `--magpie-*` 变量都对。

- [ ] **Step 6: 提交（若有改动）**

```bash
git add -A && git commit -m "test: verify the status page and the magpie page on a real machine"
```

### Task 11: 发 npm（可选，最后做）

**Files:**
- Modify: `clients/magpie-page/package.json`（`name` 改成 `@weiping/mbridge-page`，加 `version`、`repository`、`license`）

**Interfaces:**
- Consumes: Task 9 的 `clients/magpie-page/`。
- Produces: npm 上的 `@weiping/mbridge-page`；`magpie plugin add @weiping/mbridge-page` 可用。

- [ ] **Step 1: 先问作者有没有 `@weiping` 这个 scope**

没有就**不做这一步**：仓库里的 `clients/magpie-page/` 仍是唯一真源，README 里的兜底装法（`git clone` 之后 `magpie plugin add ./clients/magpie-page`）继续有效。

- [ ] **Step 2: 改 package.json 并 `npm pack`**

Run: `cd clients/magpie-page && npm pack`
Expected: 打出一个 tarball。

- [ ] **Step 3: 发布并验收**

Run: `npm publish --access public`，然后在隔离 HOME 里 `magpie plugin add @weiping/mbridge-page`，`magpie plugin --json` 能看到它。

- [ ] **Step 4: 提交**

```bash
git add clients/magpie-page/package.json
git commit -m "build: publish the magpie page plugin to npm"
```
