> 本文件为人工维护内容。

# 快速开始

目标：15 分钟内完成第一个真实调用，而不是只看文档。

## 前置条件

Node.js 需要 22 或更高版本，`package.json` 的 `engines` 写明 `>=22`。

安装与构建是两件事，顺序不能颠倒：

1. `npm install` 安装依赖，只需在首次或依赖变更后执行。
2. `npm run dev` 构建并启动应用，带热重载。
3. `npm run build` 只产出构建文件，不启动应用，用于验证能否构建通过。

日常开发用 `npm run dev`。要确认产物能构建，用 `npm run build`。两者
不要同时跑，会争用输出目录。

## 主线流程

启动应用后，按以下顺序操作：

1. 在应用窗口打开 DevTools，macOS 用 `Cmd+Option+I`，Windows 或
   Linux 用 `Ctrl+Shift+I`。
2. 切换到 Console 标签页。
3. 调用 `window.agenticBrowser`，确认对象存在。
4. 订阅事件，观察输出是否随操作产生。

## 第一个调用

在 renderer 的 DevTools Console 中执行：

```javascript
const snapshot = await window.agenticBrowser.getSnapshot()
console.log(snapshot.url, snapshot.title, snapshot.loading)
```

返回 `BrowserSnapshot`，说明主进程与预加载桥链路通畅。若报
`Cannot read properties of undefined`，说明当前不在 renderer 主 world，
检查是否误在页面元素审查器里执行。

## 订阅事件

```javascript
const off = window.agenticBrowser.onBrowserEvent((event) => {
  console.log(event.sequence, event.type, event.url)
})

await window.agenticBrowser.navigate('https://example.com')
```

执行 `navigate` 后会看到 `navigation-started`、`load-started`、
`load-finished` 等事件依次输出。观察完毕后调用 `off()` 取消订阅，避免
监听器累积。

## 操作标签页

```javascript
const tabs = await window.agenticBrowser.createTab('https://example.com')
console.log(tabs.map((tab) => `${tab.id} ${tab.active} ${tab.url}`))
```

建标签页会返回全部标签页的快照数组，其中 `active` 为真的那条是当前页。

## 顺序约束示例

启停项目运行时必须满足前置状态，顺序错会失败：

```javascript
const workspace = await window.agenticBrowser.getProjectWorkspace()
const projectId = workspace.projects[0]?.id
if (!projectId) {
  throw new Error('工作区没有项目，先安装自动化演示项目')
}

await window.agenticBrowser.setActiveProject(projectId)
await window.agenticBrowser.startProjectRuntime(projectId)
```

`setActiveProject` 必须在 `startProjectRuntime` 之前，因为运行时绑定的
活动项目在启动时确定。反过来先启动再切换，已启动的运行时不会改绑。

## 安装演示项目

工作区为空时，先安装内置演示项目，它会同时写入示例 Strategy：

```javascript
const profiles = await window.agenticBrowser.listProfiles()
const profileId = profiles[0].id
const result = await window.agenticBrowser.installAutomationPlayground(
  profileId
)
console.log(result.projectWorkspace.projects.length)
console.log(result.strategyWorkspace.strategies.length)
```

安装后即可用上面的顺序约束示例启动运行时。

## 调试位置的区别

这是新人最常混淆的一点，务必分清：

- `window.agenticBrowser` 只在渲染进程主 world 可用，调试入口是
  renderer 的 DevTools。
- `agenticStrategy` 只在页面隔离 world 可用，调试入口是 Strategy
  脚本自身，通过 `agenticStrategy.log` 输出后到事件流里查看。

在 Strategy 脚本里访问 `window.agenticBrowser` 会失败，反之亦然。两者
不是同一上下文，也不是同一对象。

## 验证 Strategy 是否生效

```javascript
const strategies = await window.agenticBrowser.getStrategyWorkspace()
for (const strategy of strategies.strategies) {
  const allowed = strategies.developmentAllowedStrategyIds.includes(
    strategy.manifest.id
  )
  console.log(strategy.manifest.id, strategy.enabled, allowed)
}
```

`enabled` 为真且出现在 `developmentAllowedStrategyIds` 中，才会被注入。
两者缺一都不会运行。

## 示例只用公开 API

本文所有片段只调用公开 API，不依赖任何测试专用 flag 或 smoke 开关。
如果你需要绕开这些片段才能跑通，说明环境或前置状态有问题，应先排查而
不是改用测试入口。
