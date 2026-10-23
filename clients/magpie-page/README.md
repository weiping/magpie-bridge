# 鹊桥 Magpie Bridge · magpie 窗口页面插件

鹊桥（Magpie Bridge）在 magpie 窗口侧边栏中的交互大盘与引导插件。

## 功能说明

鹊桥是运行在官方 magpie 网关身旁的智能模型路由守护进程（默认监听 `127.0.0.1:3426`），本插件在 magpie 窗口内提供实时看板：

1. **自动状态感知与双态界面**：
   - **运行中**：呈现与 `http://127.0.0.1:3426/status` 实时对齐的完整大盘（核心指标、1d/7d/30d 区间切换、正在选档会话、判档流水、分类器置信度打分、分布直方图、两臂对比及一键重启）。
   - **未安装或未启动**：自动切换为环境自适应的安装与启动指引。
2. **逐轮智能选档**：按任务难度在 `fast` / `balanced` / `performance` 三档间自动调配模型。
3. **TypeSafe Jev 分类器**：对任务复杂度提供置信度评估，低置信度自动升档。
4. **升降档迟滞保护与会话继承**：上一轮不满意自动升档，连续低难度才平滑降档。
5. **校准与 A/B 实验**：内置两臂对照组与置信度阈值建议。

---

## 安装与初始化（mbridge）

### 1. 安装 mbridge 并注册自启服务

- **macOS / Linux**：
  ```bash
  curl -fsSL https://raw.githubusercontent.com/weiping/mbridge/main/install.sh | sh
  ```

- **Windows（PowerShell）**：
  ```powershell
  irm https://raw.githubusercontent.com/weiping/mbridge/main/install.ps1 | iex
  ```

### 2. 初始化 magpie 路由组与配置

```bash
mbridge router init --preset cn --groups-only
```
*(也可选择 `--preset frontier` 或 `anthropic` 预设)*

### 3. 启动与验证

```bash
# 检查状态
mbridge status

# 或手动前台启动
mbridge serve
```

---

## Agent 客户端插件安装与使用

### 1. Claude Code（≥ v2.1.287）

```bash
# 安装插件
claude plugin marketplace add weiping/mbridge
claude plugin install mbridge@mbridge
```

项目配置：在项目根目录 `.claude/settings.json` 中配置：
```json
{
  "model": "group/mbridge[1m]"
}
```

### 2. Codex

```bash
# 安装插件（推荐带 --sparse 避免克隆完整历史）
codex plugin marketplace add weiping/mbridge --sparse .agents --sparse clients/codex
codex plugin add mbridge@mbridge
```

启动与使用：
```bash
# 通过 mbridge 配置文件启动
codex -p mbridge
```
或在项目 `.codex/config.toml` 中配置：
```toml
model = "group/mbridge"
```

### 3. Pi

```bash
# 安装扩展
pi install npm:@weiping/pi-mbridge
```

项目配置：在项目根目录 `.pi/settings.json` 中配置：
```json
{
  "defaultProvider": "magpie",
  "defaultModel": "group/mbridge"
}
```

---

## 本插件安装

已发布至 npm，在 magpie 中直接安装即可：

```bash
magpie plugin add @weiping/mbridge-page
```

或从源码目录加载：
```bash
magpie plugin add ./clients/magpie-page
```

*(纯窗口页面插件，不启动 Bun 插件宿主，无额外常驻资源占用)*
