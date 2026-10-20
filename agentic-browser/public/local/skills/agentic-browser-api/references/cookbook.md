> 本文件为人工维护内容。

# 常见配方

本文件按场景收录可以直接照抄的组合写法。配方会随实践逐步补充，当前
内容覆盖接入时最常遇到的几类动作。

配方中的所有 API 调用都受 `npm run verify:api-docs` 校验，方法名与
参数中出现的不存在的成员会直接导致校验失败。新增配方时保持同样
习惯。

## 读取当前页面快照

快照是一次性的静态数据，不随页面变化自动更新。需要持续跟踪时，配合
`onBrowserEvent` 订阅。

```ts
const snapshot = await window.agenticBrowser.getSnapshot()
console.log(snapshot.url, snapshot.title)
```

## 订阅事件并正确退订

`onBrowserEvent` 返回取消订阅函数。忘记调用会持续累积监听器，页面
重建后旧监听器并不会自动清理。

```ts
const off = window.agenticBrowser.onBrowserEvent((event) => {
  console.log(event.type, event.sequence)
})

// 页面或组件销毁时
off()
```

## 建标签页并等待可用

`createTab` 返回后页面可能仍处于加载中。需要页面就绪再进行操作时，
应等待事件而不是立即读取内容。

```ts
await window.agenticBrowser.createTab('https://example.com')
const snapshot = await window.agenticBrowser.getSnapshot()
```

## 在 Strategy 内发事件给主进程

Strategy 无法写文件、无法执行命令，需要侧效应时只能发事件，由主进程
侧处理。事件名由脚本自定义，payload 必须是可 JSON 序列化的值。

```ts
agenticStrategy.emit('collect-finished', { count: 12 })
```

## 在 Strategy 内发受限网络请求

请求必须先命中 manifest 声明的 `networkMatches`，并且声明
`network.request` 权限，否则调用直接被拒绝。

```ts
const response = await agenticStrategy.request({
  url: 'https://example.com/api/list',
  method: 'GET'
})
agenticStrategy.log('status', response.status)
```

## 在 Strategy 内写日志

日志有长度上限，超长内容会被截断，不要用它传递大块数据。需要传输
结构化数据时用事件。

```ts
agenticStrategy.warn('selector missing', { selector: '#submit' })
```

## 跨 reload 保持状态

保存 Strategy、切换 allowlist 都会重建页面，页面内变量全部丢失。需要
保留的状态应在启动阶段重新获取，或通过事件交给主进程保存。

## 继续补充的位置

新增配方时追加到本文件末尾，保持「场景标题 + 一句说明 + 一个最小
示例」的结构。配方只写稳定成立的做法，不写依赖特定网页的选择器。
