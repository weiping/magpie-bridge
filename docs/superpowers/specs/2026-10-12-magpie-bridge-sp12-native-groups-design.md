# SP12：`router init --native` 建原生选档组

> 状态：作者 2026-10-12 确认（「按这个方案做」）。

## 1. 问题

magpie 的路由组自带「按意图选成员」的规则：新一轮开始时，分类器（Jev 或任何模型）判一次用户消息，命中的规则把对应成员排到最前。只要逐轮按难度选档，不经过 mbridge 进程也能做到。

现在这套组要用户照 README 手动执行八条 `magpie group …` 命令，还要自己处理一个坑：外层组展开子组时，多个子组共有的模型只留在最先出现的那个子组里，规则指向后面的子组时，它的第一个成员会被前面的子组拿走（实测：`mb-perf` 的 `k3` 被排在前面的 `mb-balanced` 占了，perf 档落到第二位的 opus-thinking）。

`mbridge router init` 已经在建 `mb-fast`、`mb-balanced`、`mb-perf`、`mbridge`，让它顺手建原生组更合适，也能自动处理去重。

SP8 §2.1 否决的是「用原生规则整体替代 mbridge」（它没有 R1–R8、降档迟滞、复核、校准）。本设计不替代任何东西：原生组是可选的，和 mbridge 的路由互不相干。

## 2. 设计

**开关**：`mbridge router init --preset <名> [--groups-only] [--force] [--native]`。默认不建，行为和以前完全一样。

**前提**：

- 四个档位相关的组都在 magpie 里（这次新建的，或者已有的）。
- `router.json` 的 `classifier` 是一个 magpie 能问的模型。规则里的意图需要分类器，magpie 在没有分类器时会拒绝保存规则。`classifier` 是 `local`（init 写的占位值）或空时，init 打印一句提示，跳过原生部分，其余照常完成，退出码仍是 0。设好分类器后再跑一次 `init --native` 即可，已有的组都会保留。

**建哪些组**（档位组名取自 `router.json` 的 `tiers.<档>.group`，下面用默认名）：

| 组 | 成员 |
| --- | --- |
| `mb-native-balanced` | `mb-balanced` 当前的成员，与 `mb-perf` 共有的加推理后缀（见下） |
| `mb-native` | `group/mb-perf`、`group/mb-native-balanced`、`group/mb-fast`，`routing=order`、`stays=turn` |

`mb-fast` 和 `mb-perf` 原样复用，改它们，原生组跟着变。perf 排在最前，它的成员原样保留。

**去重**：

- 成员的 id 连同推理后缀一起算。同一个模型带不同后缀是两个成员。
- 读 `magpie group` 得到 balanced 和 perf 当前的成员；balanced 里 id 与 perf 完全相同的成员，加上后缀。后缀依次试 `medium`、`low`、`high`、`xhigh`，取第一个不与 perf 的成员、也不与 balanced 里已有的成员相同的；成员本来带的后缀换掉，`:fast` 标记保留。
- 后缀会固定该成员的推理强度，不再跟随 agent 的请求。init 打印改了哪些成员。
- `mb-fast` 的成员与 perf 或 balanced 共有时不改 `mb-fast`（它被两边复用），只打印一条提示：这些成员在 `mb-native` 里只会出现在前面的组里。
- 模型 id 本身以 `:free`、`:7b` 这类结尾的，不是推理后缀，原样保留。只有 `none`、`minimal`、`low`、`medium`、`high`、`xhigh`、`max` 和 `fast` 算后缀。

**规则**（`mb-native` 的四条，顺序固定）：

| # | 条件 | 指向 |
| --- | --- | --- |
| 1 | 意图 = `tiers.fast.criteria`，并设分类器为 `router.json` 的 `classifier` | fast 档的组 |
| 2 | 意图 = `tiers.balanced.criteria` | `mb-native-balanced` |
| 3 | 意图 = `tiers.performance.criteria` | performance 档的组 |
| 4 | `tokens=1`（没有意图，所有请求都满足） | `default_tier` 对应的组（balanced 档用 `mb-native-balanced`） |

第 4 条是兜底：分类失败或没有意图命中时按 `default_tier` 落档，对应 mbridge 的 R8。意图文本取 `router.json` 的 criteria，所以用户改过的标准会被带进来；之后再改 `router.json`，原生规则不会自动跟着变；重建的办法是先 `magpie group rm mb-native mb-native-balanced`，再跑一次 `init --native`。

**已有的组**，沿用 init 的规矩（用户的值不被覆盖）：

- `mb-native-balanced` 或 `mb-native` 已在 magpie 里：保留，打印 `kept`。
- `mb-native` 在，但一条规则也没有（上次中途失败）：不动组，只补规则。
- `--force` 不用来重建原生组：它把四个档位组按预设放回去，并把 `router.json` 写回默认值，分类器又成了 `local`，所以 `--force --native` 一起用时，原生部分会因为没有分类器而跳过（已有的原生组原样留着）。要重建就用上面说的 `magpie group rm`。
- magpie 对同名 `group add` 是替换，会清掉原有规则和分类器（真 magpie 实测）。init 不依赖这一点，但契约测试每日核对它。
- 读不出 magpie 的组列表时，和现在一样报错停止，不当作「没有」。

**不做**：

- 不改 `mb-fast`、`mb-balanced`、`mb-perf`、`mbridge`。
- 不写任何 Agent 的配置。Codex 用 `codex -m group/mb-native`，init 在建好后打印这一行。
- 不同步 criteria（见上）。
- `router check` 不检查原生组。

## 3. `internal/magpie`

SP8 §5.3 的适配层多两项，都是公开 CLI：

```go
func (c *Client) GroupRuleAdd(ctx context.Context, id string, kv ...string) error // magpie group rule add <id> k=v…
// Group 多一个 Rules int：`magpie group <id>` 的 rules 段里编号行的个数
```

还用到已有的 `GroupAdd`、`GroupSet`（`stays=turn`）、`Group`。

`magpie group <id>` 的 rules 段读法：「rules」开头的行和它后面以编号开头的行，到第一个不是编号的行为止（`classifier …` 行）。只在 `Rules` 为 0 时补规则。magpie 把这一段改了格式的话，`Rules` 会误读成 0，init 就会给已有规则的组再补一份，造成重复；契约测试每日核对这一段的格式，变了第二天就知道。

## 4. 测试

| 层 | 内容 |
| --- | --- |
| 单元：去重 | 共有成员加后缀；本来有后缀的换掉；`:free` 不当后缀；`:fast` 保留；后缀被占时顺延；无共有成员时不变 |
| 单元：init | 默认不碰原生；`--native` 建两个组、四条规则，分类器、criteria、stays 都对；criteria 取用户改过的；`default_tier` 指别的档时兜底规则跟着变；分类器是 `local` 时跳过且退出码为 0；已有组保留；有组没规则时只补规则；`--force` 后因分类器回到 `local` 而跳过、已有的原生组不动；`group rm` 之后再跑会按现在的档位组和 `router.json` 重建；读不出组列表时停止 |
| 单元：magpie | `Group` 读出规则个数；没有规则段时为 0 |
| 契约 | 隔离 HOME 里的真 magpie：`group rule add` 带分类器与意图可以保存；`group <id>` 的规则个数是 4；同名 `group add` 清掉规则 |
