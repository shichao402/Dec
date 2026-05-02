# Dec TUI 架构设计

状态：Draft

## 1. 背景与目标

Dec 当前是一个以 Cobra 为入口的命令行工具，核心能力已经完整，但交互体验仍然是“命令 + 文本输出 + 外部编辑器”的组合：

- `dec config init` 先生成 `.dec/config.yaml`，再打开外部编辑器，让用户手动把 `available` 复制到 `enabled`
- `dec push --remove` 直接从 `stdin` 读取确认
- `dec list` / `dec search` / `dec pull` / `dec push` 都把流程状态直接打印到终端

这套方式对熟悉命令行的用户足够有效，但对于高频使用场景，存在几个明显问题：

- 用户需要记忆命令和参数，发现路径长
- 资产选择、预览、启用、确认被拆散在多个命令和文件编辑里
- pull / push / remove 等关键动作缺少统一的状态视图和操作反馈
- 交互逻辑写在 `cmd/*` 中，后续想加 richer UI 复用成本高

本设计的目标是把 Dec 升级为“默认进入 TUI 的命令行工具”，保留 CLI 的自动化能力，但把日常交互迁移到内置文本界面中。

目标如下：

- `dec` 默认进入 TUI，不新增 `dec tui` 子命令
- 继续保留 `dec pull`、`dec push`、`dec version` 等脚本友好的子命令
- 用内置 TUI 替代 `config init` 对外部编辑器的强依赖
- 为 pull / push / search / remove 提供统一的键盘交互、预览、确认与进度反馈
- 不引入 Node.js 或前端运行时，继续保持单二进制分发

非目标：

- 不把 Dec 改造成图形桌面应用
- 不牺牲现有 CLI 自动化接口来换 TUI
- 不在第一阶段重写所有底层仓库/配置逻辑

## 2. 关键决策

### 2.1 入口决策

结论：`dec` 在交互式终端中默认启动 TUI；只有显式使用子命令或非 TTY 场景时，才走传统 CLI。

建议行为：

- `dec`：启动 TUI
- `dec pull`：继续执行 CLI 子命令
- `dec push --remove skill foo`：继续执行 CLI 子命令
- `dec --help` / `dec version`：继续显示 CLI 帮助与版本
- `dec` 运行在非 TTY 中：不启动 TUI，回退到帮助或经典 CLI 路径

建议保留一个隐藏逃生口，但不是新命令：

- `DEC_NO_TUI=1 dec`
- 或隐藏 flag：`dec --no-tui`

这不是为了暴露新入口，而是为了调试、CI、损坏终端环境下的应急回退。

### 2.2 架构决策

结论：TUI 不能直接包住当前 `cmd/*` 输出，必须先抽离应用服务层。

原因：

- 当前 `cmd/*` 里混合了参数解析、业务编排、磁盘操作、Git 事务、终端输出
- TUI 需要结构化状态，而不是 `fmt.Printf` 的行输出
- 如果 TUI 通过 shell out 调自己去跑 `dec pull`，后续状态同步、错误处理、测试都会变差

因此要把架构重构为：

- `cmd/*`：CLI 适配层
- `pkg/app/*`：可复用用例层
- `pkg/config` / `pkg/repo` / `pkg/ide` / `pkg/vars`：底层能力层
- `internal/tui/*`：TUI 展示层

### 2.3 交互决策

结论：TUI 不是“命令帮助页”，而是 Dec 的主工作台。

它应该覆盖这些高频行为：

- 查看仓库连接状态、当前项目状态、默认 IDE
- 浏览 vault / asset 列表
- 搜索与过滤资产
- 启用/禁用资产并保存配置
- 执行 pull / push / remove / update
- 查看变量缺失、仓库错误、IDE 警告

## 3. 技术栈调研

### 3.1 候选方案对比

| 方案 | 优点 | 缺点 | 结论 |
|---|---|---|---|
| `Bubble Tea` + `Bubbles` + `Lip Gloss` | Go TUI 生态最成熟；事件模型清晰；组件丰富；适合复杂交互与状态驱动；视觉质量上限高 | 需要建立状态管理和消息流，不是“拿来即用表单” | 推荐 |
| `tview` + `tcell` | 上手快，基础控件齐全，适合后台管理式界面 | API 偏命令式；样式系统弱；更容易做成传统表格式 UI；后续做细腻交互和品牌感成本高 | 不推荐作为主方案 |
| `promptui` / `survey` | 很适合单步 prompt、确认框、表单提问 | 不适合构建多页面、持续驻留的应用 shell | 不适合作为主架构 |
| `huh` | 表单体验比 survey 更现代，和 Bubble Tea 同生态 | 更偏 wizard/form，不适合承载整个应用框架 | 可选，不作为主框架 |
| 继续使用 Cobra + 外部编辑器 | 改动最小 | 无法实现“默认 TUI 工作台”的目标 | 否 |

### 3.2 推荐技术栈

推荐组合：

- `github.com/charmbracelet/bubbletea`
- `github.com/charmbracelet/bubbles`
- `github.com/charmbracelet/lipgloss`
- `golang.org/x/term`

可选补充：

- `github.com/creack/pty`：用于 TUI 集成测试
- `github.com/charmbracelet/huh`：仅在未来需要独立表单向导时引入

### 3.3 选择理由

Bubble Tea 适合 Dec 的原因，不在于“它最流行”，而在于它和 Dec 的产品目标匹配：

- Dec 是状态驱动工具：仓库状态、项目配置、可用资产、已启用资产、变量缺失、任务进度都天然适合消息驱动模型
- Dec 需要常驻工作台，而不是一次性 prompt
- Dec 的长任务较多：读仓库、fetch、copy、写配置、merge MCP，都需要异步任务和进度反馈
- Dec 需要把 CLI 专业感保留下来，Bubble Tea 可以让页面布局更像终端工作台，而不是问答脚本

## 4. 目标体验设计

### 4.1 首页工作台

`dec` 启动后的首页应该直接回答三个问题：

- 当前仓库是否已连接
- 当前目录是否已经初始化 `.dec/config.yaml`
- 下一步最合理的动作是什么

建议布局：

- 左侧：主导航
- 中间：当前页面主体
- 右侧：详情 / 预览 / 变量缺失提示
- 底部：快捷键帮助、任务状态、最近日志

主导航建议包含：

- `Home`
- `Assets`
- `Project`
- `Run`
- `外部应用`
- `Settings`

"外部应用"页是 Dec 的外部工具发射台：集中列出通过 `tea.ExecProcess` 挂起 TUI 运行的命令，默认提供 `pkv` 的两种用法：

- `pkv`：交互模式，把终端直接交给 pkv
- `pkv get all <project_name>`：针对当前项目批量操作；`<project_name>` 取自 `.dec/config.yaml` 的 `project_name` 字段，未配置时回退到当前目录 basename，不会自动写回 yaml
- pkv 不在 `$PATH` 时菜单项会被标记为不可用，但页面仍然可见，避免隐藏能力

### 4.2 资产浏览与启用

TUI 应直接替代 `config init` 中“打开 YAML 自己复制”的流程。

推荐流程：

1. 扫描远端仓库得到 `available`
2. 在 TUI 中按 vault / type / keyword 过滤
3. 用户用 `space` 切换启用状态
4. 右侧显示资产详情、安装目标 IDE、占位符摘要
5. 保存时自动写回 `.dec/config.yaml`
6. 保存后可直接触发 `pull`

这一步仍可保留“打开 YAML 原始编辑”的高级入口，但它应该是辅助能力，而不是主流程。

### 4.3 执行页

`pull` / `push` / `update` / `remove` 这类操作需要统一的执行页：

- 顶部显示当前任务、目标项目、目标 IDE
- 中间滚动显示结构化日志
- 底部显示当前阶段和进度
- 失败时给出可重试动作

不要在 TUI 中直接复用现有 stdout 文本；应改为结构化事件流。

注意：`update` 在本项目里专指 `dec` 二进制自更新（`pkg/update` 的 `Check` / `DoUpdate`），不是资产版本升级。**资产版本升级由 `dec pull --version` 承担**，与 `update` 是两条路径，不要复用。

### 4.4 搜索和命令面板

为了保留命令行的效率，建议加一个 command palette：

- `Ctrl+P` 或 `:` 打开动作面板
- 可以快速执行 `Pull assets`、`Push cache`、`Connect repo`、`Edit vars`、`Search assets`

这样既保留专业用户的速度，也不要求记住所有 CLI 命令。

## 5. 目标架构

### 5.1 模块分层

建议新增如下结构：

```text
cmd/
  root.go                 # CLI 入口与默认 TUI 路由
  *.go                    # 保留脚本友好子命令，但变薄

pkg/app/
  bootstrap.go            # 启动上下文、环境探测
  overview.go             # 首页状态聚合
  assets.go               # list/search/enable/disable/save
  project.go              # config init / project status / vars status
  operations.go           # pull / push / remove / update
  events.go               # 结构化事件与 reporter 接口

pkg/config/
pkg/repo/
pkg/ide/
pkg/vars/
pkg/types/

internal/tui/
  app.go                  # tea.Program 启动
  model.go                # 全局 model
  theme/
  pages/
    home.go
    assets.go
    project.go
    run.go
    settings.go
  components/
    sidebar.go
    statusbar.go
    logview.go
    filterinput.go
    confirm.go
```

### 5.2 分层职责

`pkg/app` 的职责：

- 编排现有底层能力
- 返回结构化数据，而不是直接打印文本
- 把操作过程以事件流形式暴露给 CLI/TUI

`internal/tui` 的职责：

- 管理页面状态和焦点
- 订阅长任务进度
- 渲染布局、颜色、键位帮助和确认框

`cmd/*` 的职责：

- 参数解析
- 调用 `pkg/app`
- 将结构化结果渲染为传统 CLI 文本

### 5.3 结构化事件模型

建议定义统一事件模型，替代当前散落在命令中的 `fmt.Printf`：

```go
type EventLevel string

const (
    EventInfo EventLevel = "info"
    EventWarn EventLevel = "warn"
    EventError EventLevel = "error"
)

type OperationEvent struct {
    Time      time.Time
    Level     EventLevel
    Scope     string
    Message   string
    Progress  *Progress
}

type Reporter interface {
    Emit(OperationEvent)
}
```

这样同一个 `PullAssets()` 用例可以：

- 在 CLI 中被渲染成文本日志
- 在 TUI 中被渲染成进度条、日志流、状态标签

### 5.4 启动路由

建议在入口层按下面的规则分流：

```text
if len(os.Args) == 1 && stdio 都是 TTY && TERM != dumb && DEC_NO_TUI != 1:
    run TUI
else:
    run Cobra CLI
```

这能满足两个目标：

- `dec` 默认就是 TUI
- 自动化脚本和现有命令不会被破坏

## 6. 对现有代码的重构建议

### 6.1 首先抽离 `cmd/config.go`

当前 `config init` 的主要问题不是功能缺失，而是流程硬编码在命令处理函数中：

- 扫描 repo
- 构建 `available`
- 保存项目配置
- 创建 vars 模板
- 打开编辑器
- 再次读取配置

这部分应抽成 `pkg/app/project.go` 中的用例，例如：

- `LoadProjectOverview()`
- `ScanAvailableAssets()`
- `SaveProjectSelection()`
- `EnsureProjectVarsTemplate()`

### 6.2 然后抽离 `pull` / `push`

`pull` 与 `push` 现在大量依赖顺序打印。TUI 落地前必须把这些操作变成：

- 接收参数
- 执行过程向 `Reporter` 发事件
- 返回结构化结果

尤其是 `push --remove`，当前直接读 `stdin`，在 TUI 下要改为统一 confirm modal。

### 6.3 保留现有底层包

以下包不需要因为 TUI 而整体重写：

- `pkg/repo`
- `pkg/config`
- `pkg/ide`
- `pkg/vars`

这几个包本质上已经是可复用能力层。问题主要在 `cmd/*` 过厚，而不是底层实现方向错误。

## 7. 默认 TUI 交互流

### 7.1 首次使用

1. 用户运行 `dec`
2. TUI 检查是否已连接 repo
3. 若未连接，进入 repo connect 向导
4. 然后进入 global settings，选择 IDE
5. 如果当前目录未初始化，提示初始化项目
6. 进入资产选择页，完成启用
7. 立即执行 pull

### 7.2 已有项目

1. 用户运行 `dec`
2. 首页显示当前项目启用资产数、有效 IDE、最近 pull commit
3. 用户可直接进入 Assets 调整选择
4. 或在 Run 页执行 pull / push

### 7.3 删除资产

1. 用户在资产详情页触发 remove
2. TUI 打开 confirm modal，展示 vault / type / name
3. 确认后调用 remove 用例
4. 成功后自动刷新 `available` / `enabled` / cache 状态

## 8. 实施路线

### 阶段 1：铺底层

- 新增 `pkg/app` 用例层
- 把 `cmd/config.go`、`cmd/pull.go`、`cmd/push.go` 中的业务编排迁出
- 引入统一 `Reporter` / `OperationEvent`
- 让现有 CLI 先跑在新用例层之上

验收标准：

- 不改用户行为的前提下，CLI 仍可正常运行
- `cmd/*` 中的打印逻辑明显变薄

### 阶段 2：引入 TUI Shell

- 接入 Bubble Tea 根程序
- 做首页、导航、状态栏、日志面板
- 完成默认入口路由

验收标准：

- `dec` 在交互终端中进入 TUI
- `dec pull` 等命令保持原样可用

### 阶段 3：替换 `config init` 主流程

- 做资产树、筛选、启用切换、保存
- 在 TUI 中替代外部编辑器主路径
- 保留“打开 YAML”高级入口

验收标准：

- 大多数用户不再需要手动编辑 `.dec/config.yaml`

### 阶段 4：接管长任务与危险操作

- 将 pull / push / remove / update 全部接入执行页
- 提供统一确认框、错误重试、日志查看

当前进度：

- 阶段 4A（pull）与 4B（push）已落地，Run 页具备结构化执行骨架（Reporter / OperationEvent）
- 阶段 4C（remove）已落地：`pkg/app/remove.go` 提供独立用例层，输出 `remove.prepare / repo / ide / cache / config / finish` 事件；`cmd/push.go --remove` 已改为复用该用例层；TUI Run 页新增 `x` 触发的选择器 → 二次确认 → 执行闭环，遵循用例层 `Confirmed=true` 约束
- 阶段 4D（update，自更新）已落地：TUI Run 页新增 `u` 快捷键，触发 check → 版本对比 → 确认 → 替换二进制 的 `update` runMode，直接复用 `pkg/update` 的 `Check` / `DoUpdate` / `ManualInstallCommand`；不新增 `pkg/app/operations.go` 用例层，CLI `dec update` 与 TUI 共用同一套 `pkg/update` 代码

验收标准：

- 不再需要在交互流程中手写 `stdin` confirm
- remove 通过 `app.RemoveAsset` 一条路径驱动 CLI 与 TUI，不出现逻辑分叉
- update 通过 `pkg/update` 一条路径驱动 CLI 与 TUI，且在 TUI 中把下载失败路径指向 `ManualInstallCommand()` 作为 fallback

### 阶段 5：打磨与测试

- 响应式 terminal width 适配（5A，已落地）
- Windows / macOS / Linux 行为校验（5B）：macOS 已落地（见 §9.8），Linux / Windows 拆成独立跟踪卡后续推进
- 集成测试、快照测试、回退策略测试（5C，已落地，含 §9.6 PTY 集成测试）

## 9. 测试策略

这一节是已落地的测试清单，随阶段 5C 一并稳定。新增 TUI 逻辑时对照本表决定要加哪种测试，不要让 snapshot 成为默认兜底。

### 9.1 用例层单测

验证 `pkg/app/*` 中的状态编排与错误处理。

- 位置：`pkg/app/*_test.go`
- 原则：测结构化结果和事件序列，不测文本格式

### 9.2 TUI model 状态迁移测试

验证 `internal/tui/model.Update` 的状态迁移、关键快捷键、Reporter 接入。

- 位置：`internal/tui/model_test.go`
- 覆盖：Home 概览渲染、Assets 选择 / 筛选 / 切换 / dirty、Run 页 pull/push/remove/update 快捷键与消息流、Settings 编辑 / 保存（含显式空 IDE 选择）、suggestNextAction
- 原则：对业务操作使用包级可替换变量（`saveGlobalSettingsOperation` / `runRemoveOperation` / `updateCheckOperation` / `updateDoUpdateOperation` / `updateManualInstallCommand`）打桩，不发起真实 IO

### 9.3 响应式宽度回归

验证在基线宽度下渲染不溢出终端宽度，对应架构风险 3（中文宽字符与终端宽度）。

- 位置：`internal/tui/width_test.go`
- 基线宽度：60 / 80 / 100 / 140
- 断言：每行 `lipgloss.Width(line) <= 宽度`，状态栏在窄宽度下优先保留右侧页面状态

### 9.4 Snapshot 测试

固定基线宽度下的完整渲染内容，抓取布局级回归（不只是行宽）。

- 位置：`internal/tui/snapshot_test.go`
- Golden 文件：`internal/tui/testdata/snapshots/<page>_width_<n>.txt`
- 覆盖：Home / Assets / Run / Settings × 80 / 100 / 140 列
- 规范：`lipgloss.SetColorProfile(termenv.Ascii)` 固定色彩输出，`sanitizeView` 剥除行尾空白与尾空行
- 更新 golden：`go test ./internal/tui/ -run TestSnapshot -update`
- 60 列不入 snapshot：窄宽度下文案裁剪较激进，留给 `width_test.go` 的溢出守护而不是内容快照

### 9.5 默认入口与回退策略测试

验证 `Execute` / `decideEntryMode` 分流决策。

- 位置：`cmd/root_test.go`
- 已覆盖的分支：
  - 无参 + TTY + `TERM=xterm-256color` → TUI
  - 显式子命令 → CLI
  - `DEC_NO_TUI=1` → CLI
  - stdout 非 TTY → CLI
  - `--help` → CLI
  - `TERM=dumb` → CLI
- 原则：通过 `detectTTY` / `runCLIMode` / `runTUIMode` 包级变量打桩，不启动真实 Bubble Tea

### 9.6 PTY 集成测试

用伪终端验证 `dec` 无参能进入 TUI、完成一次最小导航、优雅退出，对应架构第 3.1 节中 `github.com/creack/pty` 的可选补充。

- 当前状态：已落地
- 位置：`internal/tui/pty_integration_test.go`
- 依赖：`github.com/creack/pty`
- 覆盖：构建 `dec` 可执行文件 → pty 启动 → 等待首屏（以状态栏 `q quit | tab switch` 为锚点，剥离 ANSI 后匹配）→ `tab` 循环 Home / Assets / Project / Run / 外部应用 / Settings → `shift+tab` 回退 → 发送 `q` → 断言退出码 0
- 平台限制：通过 `//go:build integration && !windows` 约束，默认不参与 `go test ./...`
- 本地运行：`go test -tags=integration ./internal/tui/...`
- CI：需要在 POSIX runner 上显式加 `-tags=integration`，Windows / 无 pty 环境通过 build tag 自动跳过

### 9.7 CI 执行

- 用例层、TUI model、width、snapshot、路由测试均通过 `go test ./...` 覆盖，纳入主 CI 流水线
- PTY 集成测试（§9.6，build tag `integration && !windows`）已在 `.github/workflows/build.yml` 与 `.github/workflows/release-smoke.yml` 的 test job 中启用：在 `ubuntu-latest` runner 上追加一步 `go test -tags=integration ./internal/tui/... -v`，与默认测试一起跑
- snapshot 与 PTY 相关的 golden / pty 设备依赖在非 Linux runner 上可能不可用，新增时需要在测试头部注明平台要求（`t.Skip` / build tag）

### 9.8 macOS 行为校验基线（阶段 5B）

macOS 下的 TUI 行为已在 2026-04-22 本地通过 pty 自动化脚本校验，作为阶段 5B 的 macOS 部分交付。Linux 与 Windows 部分拆出独立跟踪卡推进（见第 8 节阶段 5 表格）。

- 验证对象：`/tmp/dec`（`go build -o /tmp/dec .`，基于 `v1.11.15` 代码）
- 验证方式：用 `github.com/creack/pty` 启动子进程，驱动输入序列，抓取输出并剥离 ANSI 后断言锚点
- 验证矩阵：

| 场景 | TERM | 窗口大小 | 语言环境 | 结果 |
|------|------|----------|----------|------|
| Terminal.app 典型 | xterm-256color | 120×40 | zh_CN.UTF-8 | ✓ 首屏 + 6 页 tab 循环 + `q` 退出码 0 |
| iTerm2 典型 | xterm-256color | 180×50 | zh_CN.UTF-8 | ✓ 同上 |
| 窄宽度基线 | xterm-256color | 80×30  | zh_CN.UTF-8 | ✓ 同上，布局未溢出 |

- 入口分流（在 macOS `ttys00x` 真终端下复核）：
  - `dec`（无参 + TTY + `TERM=xterm-256color`）→ TUI
  - `echo '' \| dec`（非 TTY stdin）→ CLI 帮助
  - `DEC_NO_TUI=1 dec` → CLI 帮助
  - `dec --help` → CLI 帮助
  - `TERM=dumb dec` → CLI 帮助
  - `dec version` / `dec pull --help` → CLI 子命令保留，无 TUI 干扰
- TTY 状态：CLI 路径执行后 `stty -a` 首行未变化，未检测到残留设置
- PTY 集成测试：`go test -tags=integration ./internal/tui/...` 全部通过（含 §9.6 中的 `TestPTYStartupAndQuit`，现覆盖 tab 循环和 shift+tab 回退）

本轮未发现需登记的新 bug；如后续在其他 macOS 版本、其他终端模拟器（Alacritty / WezTerm / Ghostty）或 SSH 嵌套场景下出现差异，应在 Dec 项目中建 `type:bug` 单独跟踪，而不是扩充此节。

### 9.9 Linux 行为校验基线（阶段 5B-Linux）

Linux 下的 TUI 行为已在 2026-04-29 通过 Docker Linux 环境中的 pty 自动化校验，作为阶段 5B 的 Linux 部分交付。本基线覆盖容器 / CI runner 风格的 Linux PTY，不替代后续真实桌面终端或 SSH 环境中发现差异时的独立 bug 跟踪。

- 验证对象：测试内临时构建的 `dec` 可执行文件（`go build -o <temp>/dec .`）
- 验证方式：在 Linux 容器内运行 `go test -tags=integration ./internal/tui/... -run TestPTYStartupAndQuit -v`，由 `github.com/creack/pty` 启动子进程、驱动输入序列、抓取输出并剥离 ANSI 后断言锚点
- Docker 基线命令：

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.21-bookworm \
  bash -lc '/usr/local/go/bin/go test -tags=integration ./internal/tui/... -run TestPTYStartupAndQuit -v'
```

- 验证矩阵：

| 场景 | TERM | 窗口大小 | 语言环境 | 结果 |
|------|------|----------|----------|------|
| Linux CI / 容器宽屏 | xterm-256color | 120×40 | C.UTF-8 | ✓ 首屏 + 6 页 tab 循环 + shift+tab 回退 + `q` 退出码 0 |
| Linux console 窄宽度 | linux | 80×30 | C.UTF-8 | ✓ 同上，布局未溢出 |

- 本地 macOS 复核：`go test -tags=integration ./internal/tui/... -run TestPTYStartupAndQuit -v` 同样通过上述两个 TERM 场景
- 本轮未发现需登记的新 Linux bug。若后续在 GNOME Terminal / Konsole / xterm / SSH 嵌套等真实环境里出现颜色、边框、输入法或 Alt 序列差异，应在 Dec 项目中新建 `type:bug` 或 `type:improvement`，不要把真实环境差异混进本基线。

### 9.10 Project 页变量编辑（外部 editor 挂起）

阶段：EPIC TUI 化子卡 VK-15（task #80）。

- Project 页下方新增 "Project Variables" 只读区块，显示 `.dec/vars.yaml` 路径、有效 editor 命令、以及通过 `pkg/vars.ExtractPlaceholdersFromDir` 从 `.dec/cache/` 扫出的已用占位符及其解析来源（project / global / missing）
- 按 `e` 键触发 `openProjectVarsEditorCmd`：调用 `pkg/app.EnsureProjectVarsFile` 确保模板存在，再用 `pkg/editor.BuildCommand` 构造 `*exec.Cmd`，交给 `tea.ExecProcess` 挂起 TUI 执行外部编辑器，编辑器退出后通过 `projectVarsEditedMsg` 触发 `loadProjectVarsCmd` 重新拉一次视图
- 架构约束：
  - **禁止在 TUI 里直接调用 `editor.Open`**，会与 Bubble Tea 持有的 TTY 冲突，必须走 `tea.ExecProcess` 挂起路径
  - **不引入内置 YAML 编辑器**：`vars.yaml` 是任意嵌套 YAML，结构化表单覆盖不全；共用现有 `pkg/editor` 抽象（全局 / 项目 editor 字段）
  - **不提供写入 API**：`LoadProjectVarsView` 是纯读；所有写入都由用户的编辑器完成，TUI 不尝试自动修正 YAML
  - **CLI 不新增命令**：用户直接编辑 `.dec/vars.yaml` 是既有路径，TUI 的 `e` 入口就是包装器
- 用例层：`pkg/app/project_vars.go` 提供 `LoadProjectVarsView` / `EnsureProjectVarsFile`，解析来源常量 `PlaceholderSourceProject / Global / Missing`
- 当 `.dec/cache/` 不存在（未运行过 `dec pull`）时，区块显示提示语，不尝试从 bare repo 临时扫描占位符——那是 `dec pull` 的职责
- 当 `editor.BuildCommand` 返回错误（未配置且 `vim/vi/nano/notepad` 均查不到）时，通过 `projectVarsEditedMsg.err` 反馈给用户并在 UI 上显示"编辑器返回错误"，不静默失败

## 10. 风险与应对

### 风险 1：TUI 卡住长任务

应对：所有 Git / IO 操作都通过 `tea.Cmd` 异步执行，禁止在 `Update` 里直接做阻塞调用。

### 风险 2：CLI 与 TUI 双入口导致逻辑分叉

应对：强制所有业务动作都通过 `pkg/app`，禁止 TUI 直接调用 `cmd/*`。

### 风险 3：中文宽字符和终端宽度导致布局错位

应对：统一走 Lip Gloss 和 Bubble Tea 生态的宽度计算，不自己手写 rune 宽度逻辑。

### 风险 4：默认 TUI 影响现有用户习惯

应对：

- 只在交互式无参数启动时进入 TUI
- 所有原子命令继续保留
- 提供隐藏回退开关 `DEC_NO_TUI=1`

## 11. 最终建议

建议按下面的原则推进：

1. 先做架构分层，再做页面
2. 默认入口改为 TUI，但不新增 `dec tui`
3. 保留 CLI 子命令作为自动化接口，而不是兼容包袱
4. 技术栈选择 Bubble Tea 生态，不走 prompt 式方案拼装
5. 第一优先级不是“界面好看”，而是替代 `config init` 的 YAML 编辑链路

如果只选一个最关键的里程碑，应先完成：

- `config init` 的 TUI 化
- `dec` 默认进入 TUI
- `pull` 的执行页化

这三项完成后，Dec 的产品形态就会从“命令工具”升级为“终端中的资产工作台”。
