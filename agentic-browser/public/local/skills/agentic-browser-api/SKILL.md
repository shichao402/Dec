---
name: agentic-browser-api
description: >-
  AgenticBrowser 的完整 API 说明书。当需要调用
  window.agenticBrowser 控制浏览器、编写 Strategy 隔离世界脚本、
  定义 Project 与 Workflow、订阅浏览器与运行时事件、排查 Strategy
  运行失败或网络请求被 allowlist 拦截时使用本说明。
---

> 本文件为人工维护内容。

# AgenticBrowser API

本目录是 AgenticBrowser 的 API 说明书，共三类读者：

- 刚接触项目、需要快速跑通第一个调用的开发者
- 需要按方法逐个查签名的使用者
- 需要驱动本项目的 AI Agent

签名层由类型自动生成，不会与代码不一致。概念、上手路径与限制清单为
人工维护。

## 三层进程模型

AgenticBrowser 是 Electron 应用，能力分布在三层。调用前必须清楚自己
站在哪一层。

- 主进程：运行在 Node.js，负责浏览器生命周期、项目调度、下载与恢复，
  属内部实现，不直接暴露。
- 预加载桥：运行在 contextBridge，把方法转发到主进程，入口是
  `window.agenticBrowser`。
- 页面隔离 world：运行在页面上下文，只能读配置、写日志、发事件、发
  请求，入口是 `agenticStrategy`。

主进程不对外开放任何编程接口。当前不存在 HTTP、CLI 或 MCP 形式的外部
接口。

## 上下文区分

这是最容易出错的地方：`window.agenticBrowser` 与 `agenticStrategy`
不是同一对象，也不在同一上下文。

`window.agenticBrowser` 在渲染进程主 world 中可用，调试位置是 renderer
的 DevTools Console。它负责控制浏览器的整体行为，例如建标签页、改设置、
启停项目运行时。

`agenticStrategy` 在页面隔离 world 中可用，只对 Strategy 脚本可见。它
作用于当前页面，能力受 manifest 的 `permissions` 与 `networkMatches`
限制。

在 Strategy 脚本里访问 `window.agenticBrowser` 会失败，反过来在 renderer
Console 里访问 `agenticStrategy` 也会失败。

## 最小上手步骤

1. 确认 Node.js 22 或更高版本，`package.json` 的 `engines` 要求 `>=22`。
2. 安装依赖：`npm install`。
3. 启动开发模式：`npm run dev`。该命令带热重载，修改源码后自动重建。
4. 在应用窗口按 `Cmd+Option+I` 或 `Ctrl+Shift+I` 打开 DevTools，切到
   Console。
5. 执行 `await window.agenticBrowser.getSnapshot()`，看到当前页面快照
   即表示桥接正常。

`npm run build` 只产出构建文件，不启动应用。`npm run dev` 会先构建再
启动并监听变更。两者不要混用。

## 顺序约束

部分方法依赖前置状态，顺序错会直接失败：

- 启停项目运行时前，该项目的 `id` 必须已存在于工作区，通常先调用
  `saveProject`。
- 运行时要绑定到当前活动项目时，先调用 `setActiveProject`。
- Strategy 只有在 manifest 的 `matches` 命中当前 URL、且已通过
  `setStrategyAllowed` 放行后才会注入。
- 修改 Strategy 源码后需要重新保存或重启运行时，页面会被重建。

## 子文档索引

- [api-reference.md](references/api-reference.md)：全部方法、类型、
  事件、权限、限额与错误清单，自动生成。
- [concepts.md](references/concepts.md)：核心概念与数据模型。
- [quickstart.md](references/quickstart.md)：从零到跑通的最短路径。
- [limits.md](references/limits.md)：限制与不支持项。
- [cookbook.md](references/cookbook.md)：常见配方，持续补充。
- [troubleshooting.md](references/troubleshooting.md)：排错指南，
  持续补充。

## 维护约定

生成文件只由 `npm run docs:api` 产出，不要手工编辑。校验通过
`npm run verify:api-docs` 执行，会重新生成并逐字节比对，同时校验文档
中出现的标识符是否都能在类型真源中找到。

手写文件中引用 API 时，一律用反引号包裹标识符，例如 `createTab`、
`onBrowserEvent`、`project.emit-event`，这样校验器才能提取并确认它们
真实存在。不要用「那个创建标签页的方法」这类自然语言指代。

校验器会扫描本目录下全部 Markdown 文件，因此新增子文档无需改动脚本
就会被自动纳入检查。

## 补充内容的边界

签名、类型、事件、权限、限额与错误清单属于金标准，只能由生成器产出，
不接受手写补充。配方与排错指南属于持续补充内容，按实践积累追加。

补充内容必须服从三条约束，否则校验会失败：

- 只使用真实存在的 API 名称，代码块中的 `window.agenticBrowser` 与
  `agenticStrategy` 下的成员都会被逐个核对。
- 不得在正文中记录限额数值，一律引用
  [api-reference.md](references/api-reference.md) 的限额常量一节，避免
  数值漂移。
- 保持行长与标题层级，文件末尾只保留一个换行。

补充配方写入 [cookbook.md](references/cookbook.md)，补充排错写入
[troubleshooting.md](references/troubleshooting.md)，两者都在文末预留了
追加位置。

## 固定命令与产物

- 生成：`npm run docs:api`，唯一产物是
  `references/api-reference.md`。
- 校验：`npm run verify:api-docs`，只读，不写文件。
- 全量门禁：`npm run check` 已包含上述校验。

生成器不接收参数，输出路径写死在脚本中，避免产物散落。

生成器与校验器位于仓库的 `scripts/api-docs/`，不在本目录内。本目录只放
文档，原因是它会被整体发布到 Dec registry 供他人安装，脚本依赖仓库内的
类型源码，发布出去无法运行。

## 分发

本目录已声明为 Dec 资产，配置在 `.dec/config.yaml`。推送到 main 分支且
改动落在本目录时，`.github/workflows/dec-publish.yml` 会自动发布。版本号
由发布内容的提交数派生（形如 v0.1.0+3），无需手工维护，与应用自身的
版本号无关。

安装到本机对应 AI 工具的方式是订阅 Dec 提供方 "agentic-browser"，资产
名为 "agentic-browser-api"，由 Dec 负责落盘到各工具目录。不要手工复制本
目录，手工副本不会随版本更新。
