# mbridge's page for magpie

magpie 窗口侧栏 Plugins 组里的一个页面：magpie 记下的用量速览。

## 它显示什么

- **周期切换**（今天 / 7 天 / 30 天）与**排序**（Token / 调用 / 费用）
- 四块：调用、错误（占调用的比例）、Token、费用
- 模型表：按所选排序，每行一条占比条
- 每 60 秒刷新一次，周期与排序记在 magpie 的 options 里

## 它不显示什么

**mbridge 自己的状态不在这里。** magpie 的页面插件跑在 `iframe sandbox="allow-scripts"`
里，CSP 是 `connect-src 'none'`：它连不上 `127.0.0.1:3426`，也读不了 `router.json`。
所以状态、报表、重启都在 mbridge 自己的状态页：

    http://127.0.0.1:3426/status

页面上把那行地址做成**可选择的文字**而不是链接：沙盒允许导航自己，但那是单程票
（magpie 的 sidebar 不会把 frame 的 src 重置回来）。

**不做档位归因**：magpie 记下的是最终回答的模型，`usage:summary` 里没有档位。

## 装法

文件夹插件，改了源码在终端重开一个 `magpie` 命令就是新的：

    magpie plugin add ./clients/magpie-page

确认装上了（`plugin list` 这个子命令不存在，用 `--json`）：

    magpie plugin --json

## 这个包必须永远是纯页面插件

`package.json` 里**没有** `main`、**没有** `exports`，这不是偏好：
magpie 的 `magpie.options.set()` 对纯窗口插件（有 `page`、无 `main`/`exports`）
不重启插件宿主，而 CLI 写 options 每次都会。一旦给这个包加了 `main` 或 `exports`，
用户每点一次周期切换就可能把所有插件重启一轮。`page.test.mjs` 里有一条守着。

## 测试

    bun test clients/magpie-page
