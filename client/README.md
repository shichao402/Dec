# Dec 管理客户端

独立 Tauri 2 桌面客户端，管理本机或远程 `dec-server`。不替代 `dec` TUI。

## 要求

- Node.js、Rust（stable）
- Windows 需 WebView2
- 本机连接会发现或拉起已安装的 `dec-server`

## 开发

```bash
cd client
npm install
npm run tauri dev
```

开发前端固定在 `127.0.0.1:59124`（避开 Vite 默认 5173）。debug 窗口在显示前会核对页面里的 `dec-console` 身份标记；对不上或端口上是别的项目，直接退出，避免把主密码框交给别人的前端。不要直接运行 `src-tauri/target/debug/app.exe`——那样不会启动本仓库的 Vite，只会去加载当时占着 `devUrl` 的任意页面。

启动后先选连接（本机 / 远程 gRPC / SSH 隧道），再用 Bitwarden 主密码解锁。`dec-server` 启动后全局锁定，解锁成功后控制权与 BW session 同为 1 小时内存态。

连接会保存 Bitwarden 邮箱。主密码默认不保存；用户明确勾选后才通过统一的系统凭据接口写入 Windows Credential Manager、macOS Keychain 或 Linux Secret Service，不会进入 `connections.json`。取消勾选或删除连接时会同时删除对应凭据。Linux 构建静态携带 D-Bus 客户端依赖，桌面会话仍需提供 Secret Service（如 GNOME Keyring 或 KWallet）。

远程直连必须使用由系统信任根校验的 TLS；若服务只监听 loopback，使用 SSH 隧道。连接成功后的完整流程是：

1. 初始化设备私仓、IDE 与 Global 资产；
2. 显式选择服务器目录接管项目，或手动选择一个范围扫描已有 Dec 项目；
3. 初始化项目并选择家项目 / requires 资产；
4. Pull 后在结果卡查看落地数量、跳过原因、缺失项与警告。

受管项目列表保存在目标设备，移除管理不会删除项目文件。Global 请求始终使用空项目路径；项目请求始终携带目标服务器上的绝对路径。

`dec-server` 是一机单例：换过二进制后仍在运行的旧实例不会加载新方法，调用会返回「未知服务方法」。控制台把这类错误翻译成重启提示，可在错误条或「设备设置 → 服务实例」重启服务并重连；本机连接会拉起新二进制。远端与本机生命周期一致——空闲即退出，连接时由连接方经 SSH 按需拉起，因此远端进程不在运行是正常状态；远端只需固定 `management_listen`（隧道要靠约定端口找到它），**不需要**配成常驻服务。自动置备见 [ADR 0019](../Documents/decisions/0019-remote-provisioning.md)。

## 界面结构

```
src/
  index.css                设计 token（字体栈、暗色调色板、滚动条、focus 环）
  App.tsx                  屏幕/视图状态机 + 动作编排，不含具体页面布局
  components/shell/        sidebar（设备 + 导航）、top-bar（面包屑 + 忙碌指示）、page（布局原语）
  components/ui/           button / input / panel / badge / checkbox / feedback 等基础件
  pages/                   连接、解锁、引导、概览、Global 资产、项目、项目详情、同步、设置
  lib/console.ts           视图枚举、资源锁常量、连接与路径展示的共享函数
```

布局原语只有两种页型，避免出现「短内容整页留白」和「长列表被外层滚动切断」：

- `PageScroll`：表单与说明类页面整页滚动，主动作放 `PanelFooter` 常驻底部。
- `PageFill`：列表类页面本身不滚动，内部 `ScrollArea` 撑满视口剩余高度；宽屏用 `SplitPane` 把主内容与上下文栏分列，窄屏自动退化为单列堆叠。

颜色、间距、字号一律走 token 与基础组件，页面里不直接写 `zinc-xxx` 之类的原始色值。新增页面优先复用 `Panel` / `SettingsSection` / `Stat` / `EmptyState`，保证密度与对齐一致。

面板与表格的高度策略不同，别混用：

- 有边框的卡片用 `fitBlock`（可缩不可长）：内容短就贴合内容，长了才吃满剩余高度并内部滚动。强行 `flex-1` 会得到「5 行内容 + 400px 空白」的面板。
- 表格类主体（资产选择）撑满高度：底部操作条贴住视口下沿，行的位置不随条数跳动。
- `SplitPane` 内的列表用 `<ScrollArea splitOnly>`：分栏时各自滚动，堆叠时把滚动交给外层，避免同一祖先链上出现两个滚动区。
- 截断文本要给 `title`，让省略号后面的内容仍可获取。

## 布局回归测试

布局不靠看截图判断，靠测量。`tests/` 下是 Playwright 驱动的布局断言，跑「页面 × 数据形态 × 视口」矩阵：

```bash
npm run test:layout                  # 75 个用例，约 25 秒
SHOTS=1 npx playwright test tests/shots.spec.ts   # 输出 .shots/*.png 供人工过目
```

- `tests/fixtures/data.ts`：五种数据形态（典型、全空、120 项、超长中文名与深路径、报错态）。只对着「中等数量 + 短名字」改布局，就会漏掉空列表留白与长文本溢出。
- `tests/fixtures/tauri-mock.ts`：通过 `addInitScript` 注入 Tauri IPC。生产代码里没有 mock 分支，`main.tsx` 也不需要开关。
- `tests/layout/probe.ts`：在页面里测量并给出结论——横向溢出、嵌套滚动、被裁掉点不到的控件、遮挡、实际被截断的文本、宽度利用率，以及「一边截断一边空着」的高度错配。纯粹的底部留白只记进 metrics，不判错：内容短时留白是常态。
- `tests/cases.ts`：用例矩阵与导航步骤，两个 spec 共用。有意为之的布局（居中解锁卡）在用例里显式 `ignore`，不放宽全局阈值。

视口基线取 `tauri.conf.json` 里声明的窗口下限（960×600）、默认尺寸与宽屏，思路与 `internal/tui` 的 golden 快照一致：先声明支持范围，再守住边界。改窗口下限时同步改 `tests/cases.ts` 的 `viewports`。

失败信息里直接给出像素数与元素路径。批量排查时用聚合视图：

```bash
PLAYWRIGHT_JSON_OUTPUT_NAME=.layout-report.json npx playwright test --reporter=json
node tests/layout-report.mjs .layout-report.json
```

## 异步交互

所有连接、读取、保存和长任务统一进入 Shell 级 action registry。动作按设备和工作区资源分域互斥：冲突按钮自动禁用，但侧栏导航与无冲突读取保持可用。任务切页不取消，运行进度在全局状态区持续可见；回到原页面后可继续读取结果。

`RunOperation` 会自动检查并旁观相同工作区的在飞任务。连接成功和受管项目变化后，Console 也会轮询 Global 与项目根，恢复本 Console 或 MCP 已发起的任务。页面组件不应自行增加 `busy` / `loading` / `saving` 状态；新增异步入口必须声明 action key、资源锁和读写类型。

## 构建

```bash
npm run tauri build
```
