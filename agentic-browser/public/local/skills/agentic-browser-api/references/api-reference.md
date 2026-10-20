# AgenticBrowser API 参考

> 本文件由脚本自动生成，请勿手工编辑。

生成命令：`npm run docs:api`

真源文件：

- `src/shared/contracts.ts`
- `src/preload/index.ts`
- `src/preload/strategy-bootstrap.ts`
- `src/main/strategy-runtime.ts`

生成器使用的 TypeScript 版本：5.9.3

## 渲染进程桥 `window.agenticBrowser`

### `BrowserShellApi` 全部方法

通过 `contextBridge.exposeInMainWorld` 暴露到渲染进程主 world，在 renderer
DevTools Console 中可直接调用。

共 32 个成员，按源码声明顺序排列。

#### `getSnapshot`

```typescript
getSnapshot(): Promise<BrowserSnapshot>
```

源码位置：`src/shared/contracts.ts:588`

无参数。

##### 字段表 `BrowserSnapshot`

| 字段         | 类型                   | 可选 |
| ------------ | ---------------------- | ---- |
| `url`        | `string`               | 否   |
| `title`      | `string`               | 否   |
| `loading`    | `boolean`              | 否   |
| `responsive` | `boolean`              | 否   |
| `generation` | `number`               | 否   |
| `lastEvent`  | `BrowserEvent \| null` | 否   |
| `health`     | `PageHealthSnapshot`   | 否   |
| `recovery`   | `RecoverySnapshot`     | 否   |

#### `getMonitorSnapshot`

```typescript
getMonitorSnapshot(): Promise<MonitorSnapshot>
```

源码位置：`src/shared/contracts.ts:589`

无参数。

##### 字段表 `MonitorSnapshot`

| 字段                   | 类型                             | 可选 |
| ---------------------- | -------------------------------- | ---- |
| `generatedAt`          | `string`                         | 否   |
| `browser`              | `BrowserSnapshot`                | 否   |
| `projectRuntime`       | `ProjectRuntimeSnapshot \| null` | 否   |
| `projectRuntimes`      | `ProjectRuntimeSnapshot[]`       | 否   |
| `activeProjectCount`   | `number`                         | 否   |
| `pausedProjectCount`   | `number`                         | 否   |
| `queuedProjectCount`   | `number`                         | 否   |
| `browserEventCount`    | `number`                         | 否   |
| `activePageCount`      | `number`                         | 否   |
| `activeStrategyCount`  | `number`                         | 否   |
| `strategies`           | `StrategyRuntimeSnapshot`        | 否   |
| `totalCpuPercent`      | `number`                         | 否   |
| `totalWorkingSetBytes` | `number`                         | 否   |
| `processes`            | `ProcessMetric[]`                | 否   |
| `downloads`            | `DownloadSummary`                | 否   |

#### `getCapabilityPolicy`

```typescript
getCapabilityPolicy(): Promise<CapabilityPolicySnapshot>
```

源码位置：`src/shared/contracts.ts:590`

无参数。

##### 字段表 `CapabilityPolicySnapshot`

| 字段            | 类型                 | 可选 |
| --------------- | -------------------- | ---- |
| `policy`        | `CapabilityPolicy`   | 否   |
| `currentOrigin` | `string \| null`     | 否   |
| `effective`     | `CapabilitySettings` | 否   |

#### `setCapabilityPolicy`

```typescript
setCapabilityPolicy(policy: CapabilityPolicy): Promise<CapabilityPolicySnapshot>
```

源码位置：`src/shared/contracts.ts:591`

| 参数     | 类型               | 可选 |
| -------- | ------------------ | ---- |
| `policy` | `CapabilityPolicy` | 否   |

#### `listTabs`

```typescript
listTabs(): Promise<BrowserTab[]>
```

源码位置：`src/shared/contracts.ts:594`

无参数。

#### `createTab`

```typescript
createTab(url?: string): Promise<BrowserTab[]>
```

源码位置：`src/shared/contracts.ts:595`

| 参数  | 类型     | 可选 |
| ----- | -------- | ---- |
| `url` | `string` | 是   |

#### `switchTab`

```typescript
switchTab(tabId: string): Promise<BrowserTab[]>
```

源码位置：`src/shared/contracts.ts:596`

| 参数    | 类型     | 可选 |
| ------- | -------- | ---- |
| `tabId` | `string` | 否   |

#### `closeTab`

```typescript
closeTab(tabId: string): Promise<BrowserTab[]>
```

源码位置：`src/shared/contracts.ts:597`

| 参数    | 类型     | 可选 |
| ------- | -------- | ---- |
| `tabId` | `string` | 否   |

#### `listProfiles`

```typescript
listProfiles(): Promise<ProfileSummary[]>
```

源码位置：`src/shared/contracts.ts:598`

无参数。

#### `createProfile`

```typescript
createProfile(name: string): Promise<ProfileSummary[]>
```

源码位置：`src/shared/contracts.ts:599`

| 参数   | 类型     | 可选 |
| ------ | -------- | ---- |
| `name` | `string` | 否   |

#### `activateProfile`

```typescript
activateProfile(profileId: string): Promise<ProfileSummary[]>
```

源码位置：`src/shared/contracts.ts:600`

| 参数        | 类型     | 可选 |
| ----------- | -------- | ---- |
| `profileId` | `string` | 否   |

#### `deleteProfile`

```typescript
deleteProfile(profileId: string): Promise<ProfileSummary[]>
```

源码位置：`src/shared/contracts.ts:601`

| 参数        | 类型     | 可选 |
| ----------- | -------- | ---- |
| `profileId` | `string` | 否   |

#### `getProjectWorkspace`

```typescript
getProjectWorkspace(): Promise<ProjectWorkspaceSnapshot>
```

源码位置：`src/shared/contracts.ts:602`

无参数。

##### 字段表 `ProjectWorkspaceSnapshot`

| 字段                  | 类型                          | 可选 |
| --------------------- | ----------------------------- | ---- |
| `activeProjectId`     | `string \| null`              | 否   |
| `projects`            | `ProjectDefinition[]`         | 否   |
| `runtimes`            | `ProjectRuntimeRecord[]`      | 否   |
| `strategyResolutions` | `ProjectStrategyResolution[]` | 否   |

#### `installAutomationPlayground`

```typescript
installAutomationPlayground(profileId: string): Promise<AutomationPlaygroundInstallResult>
```

源码位置：`src/shared/contracts.ts:603`

| 参数        | 类型     | 可选 |
| ----------- | -------- | ---- |
| `profileId` | `string` | 否   |

##### 字段表 `AutomationPlaygroundInstallResult`

| 字段                | 类型                        | 可选 |
| ------------------- | --------------------------- | ---- |
| `projectWorkspace`  | `ProjectWorkspaceSnapshot`  | 否   |
| `strategyWorkspace` | `StrategyWorkspaceSnapshot` | 否   |

#### `saveProject`

```typescript
saveProject(draft: ProjectDraft): Promise<ProjectWorkspaceSnapshot>
```

源码位置：`src/shared/contracts.ts:606`

| 参数    | 类型           | 可选 |
| ------- | -------------- | ---- |
| `draft` | `ProjectDraft` | 否   |

#### `setActiveProject`

```typescript
setActiveProject(projectId: string): Promise<ProjectWorkspaceSnapshot>
```

源码位置：`src/shared/contracts.ts:607`

| 参数        | 类型     | 可选 |
| ----------- | -------- | ---- |
| `projectId` | `string` | 否   |

#### `deleteProject`

```typescript
deleteProject(projectId: string): Promise<ProjectWorkspaceSnapshot>
```

源码位置：`src/shared/contracts.ts:608`

| 参数        | 类型     | 可选 |
| ----------- | -------- | ---- |
| `projectId` | `string` | 否   |

#### `startProjectRuntime`

```typescript
startProjectRuntime(projectId: string): Promise<ProjectWorkspaceSnapshot>
```

源码位置：`src/shared/contracts.ts:609`

| 参数        | 类型     | 可选 |
| ----------- | -------- | ---- |
| `projectId` | `string` | 否   |

#### `pauseProjectRuntime`

```typescript
pauseProjectRuntime(projectId: string): Promise<ProjectWorkspaceSnapshot>
```

源码位置：`src/shared/contracts.ts:610`

| 参数        | 类型     | 可选 |
| ----------- | -------- | ---- |
| `projectId` | `string` | 否   |

#### `resumeProjectRuntime`

```typescript
resumeProjectRuntime(projectId: string): Promise<ProjectWorkspaceSnapshot>
```

源码位置：`src/shared/contracts.ts:611`

| 参数        | 类型     | 可选 |
| ----------- | -------- | ---- |
| `projectId` | `string` | 否   |

#### `stopProjectRuntime`

```typescript
stopProjectRuntime(projectId: string): Promise<ProjectWorkspaceSnapshot>
```

源码位置：`src/shared/contracts.ts:612`

| 参数        | 类型     | 可选 |
| ----------- | -------- | ---- |
| `projectId` | `string` | 否   |

#### `getStartupSettings`

```typescript
getStartupSettings(): Promise<StartupSettings>
```

源码位置：`src/shared/contracts.ts:613`

无参数。

##### 字段表 `StartupSettings`

| 字段                | 类型      | 可选 |
| ------------------- | --------- | ---- |
| `supported`         | `boolean` | 否   |
| `openAtLogin`       | `boolean` | 否   |
| `managedByWatchdog` | `boolean` | 否   |

#### `setOpenAtLogin`

```typescript
setOpenAtLogin(enabled: boolean): Promise<StartupSettings>
```

源码位置：`src/shared/contracts.ts:614`

| 参数      | 类型      | 可选 |
| --------- | --------- | ---- |
| `enabled` | `boolean` | 否   |

#### `getStrategyWorkspace`

```typescript
getStrategyWorkspace(): Promise<StrategyWorkspaceSnapshot>
```

源码位置：`src/shared/contracts.ts:615`

无参数。

##### 字段表 `StrategyWorkspaceSnapshot`

| 字段                            | 类型                   | 可选 |
| ------------------------------- | ---------------------- | ---- |
| `strategies`                    | `StrategyDefinition[]` | 否   |
| `developmentAllowedStrategyIds` | `string[]`             | 否   |

#### `saveStrategy`

```typescript
saveStrategy(draft: StrategyDraft): Promise<StrategyWorkspaceSnapshot>
```

源码位置：`src/shared/contracts.ts:616`

| 参数    | 类型            | 可选 |
| ------- | --------------- | ---- |
| `draft` | `StrategyDraft` | 否   |

#### `setStrategyEnabled`

```typescript
setStrategyEnabled(strategyId: string, enabled: boolean): Promise<StrategyWorkspaceSnapshot>
```

源码位置：`src/shared/contracts.ts:617`

| 参数         | 类型      | 可选 |
| ------------ | --------- | ---- |
| `strategyId` | `string`  | 否   |
| `enabled`    | `boolean` | 否   |

#### `setStrategyAllowed`

```typescript
setStrategyAllowed(strategyId: string, allowed: boolean): Promise<StrategyWorkspaceSnapshot>
```

源码位置：`src/shared/contracts.ts:621`

| 参数         | 类型      | 可选 |
| ------------ | --------- | ---- |
| `strategyId` | `string`  | 否   |
| `allowed`    | `boolean` | 否   |

#### `deleteStrategy`

```typescript
deleteStrategy(strategyId: string): Promise<StrategyWorkspaceSnapshot>
```

源码位置：`src/shared/contracts.ts:625`

| 参数         | 类型     | 可选 |
| ------------ | -------- | ---- |
| `strategyId` | `string` | 否   |

#### `navigate`

```typescript
navigate(url: string): Promise<BrowserSnapshot>
```

源码位置：`src/shared/contracts.ts:626`

| 参数  | 类型     | 可选 |
| ----- | -------- | ---- |
| `url` | `string` | 否   |

#### `reload`

```typescript
reload(options: ReloadOptions): Promise<BrowserSnapshot>
```

源码位置：`src/shared/contracts.ts:627`

| 参数      | 类型            | 可选 |
| --------- | --------------- | ---- |
| `options` | `ReloadOptions` | 否   |

#### `setPageViewsVisible`

```typescript
setPageViewsVisible(visible: boolean): Promise<void>
```

源码位置：`src/shared/contracts.ts:628`

| 参数      | 类型      | 可选 |
| --------- | --------- | ---- |
| `visible` | `boolean` | 否   |

#### `onBrowserEvent`

```typescript
onBrowserEvent(listener: (event: BrowserEvent) => void): () => void
```

源码位置：`src/shared/contracts.ts:629`

| 参数       | 类型                            | 可选 |
| ---------- | ------------------------------- | ---- |
| `listener` | `(event: BrowserEvent) => void` | 否   |

## Strategy 宿主对象 `agenticStrategy`

### `StrategyHostApi` 全部成员

仅在 Strategy 脚本运行的页面隔离 world 中可用，与渲染进程主 world
不是同一上下文。

共 8 个成员，按源码声明顺序排列。

#### `config`

```typescript
readonly config: Record<string, JsonValue>
```

源码位置：`src/shared/contracts.ts:574`

#### `log`

```typescript
log(message: string, data?: JsonValue): Promise<void>
```

源码位置：`src/shared/contracts.ts:575`

| 参数      | 类型        | 可选 |
| --------- | ----------- | ---- |
| `message` | `string`    | 否   |
| `data`    | `JsonValue` | 是   |

#### `debug`

```typescript
debug(message: string, data?: JsonValue): Promise<void>
```

源码位置：`src/shared/contracts.ts:576`

| 参数      | 类型        | 可选 |
| --------- | ----------- | ---- |
| `message` | `string`    | 否   |
| `data`    | `JsonValue` | 是   |

#### `warn`

```typescript
warn(message: string, data?: JsonValue): Promise<void>
```

源码位置：`src/shared/contracts.ts:577`

| 参数      | 类型        | 可选 |
| --------- | ----------- | ---- |
| `message` | `string`    | 否   |
| `data`    | `JsonValue` | 是   |

#### `error`

```typescript
error(message: string, data?: JsonValue): Promise<void>
```

源码位置：`src/shared/contracts.ts:578`

| 参数      | 类型        | 可选 |
| --------- | ----------- | ---- |
| `message` | `string`    | 否   |
| `data`    | `JsonValue` | 是   |

#### `emit`

```typescript
emit(name: string, payload?: JsonValue): Promise<void>
```

源码位置：`src/shared/contracts.ts:579`

| 参数      | 类型        | 可选 |
| --------- | ----------- | ---- |
| `name`    | `string`    | 否   |
| `payload` | `JsonValue` | 是   |

#### `request`

```typescript
request(request: StrategyNetworkRequest): Promise<StrategyNetworkResponse>
```

源码位置：`src/shared/contracts.ts:580`

| 参数      | 类型                     | 可选 |
| --------- | ------------------------ | ---- |
| `request` | `StrategyNetworkRequest` | 否   |

##### 字段表 `StrategyNetworkResponse`

| 字段      | 类型                     | 可选 |
| --------- | ------------------------ | ---- |
| `url`     | `string`                 | 否   |
| `status`  | `number`                 | 否   |
| `headers` | `Record<string, string>` | 否   |
| `body`    | `string`                 | 否   |

#### `evaluateMainWorld`

```typescript
evaluateMainWorld(source: string): Promise<unknown>
```

源码位置：`src/shared/contracts.ts:581`

| 参数     | 类型     | 可选 |
| -------- | -------- | ---- |
| `source` | `string` | 否   |

## 事件、权限与限额

### `BrowserEventType` 事件

源码位置：`src/shared/contracts.ts:42`

| 事件名                        | 源码行 |
| ----------------------------- | ------ |
| `'created'`                   | 43     |
| `'navigation-started'`        | 44     |
| `'navigation-finished'`       | 45     |
| `'load-started'`              | 46     |
| `'load-finished'`             | 47     |
| `'load-failed'`               | 48     |
| `'unresponsive'`              | 49     |
| `'responsive'`                | 50     |
| `'renderer-gone'`             | 51     |
| `'recreated'`                 | 52     |
| `'checkpoint-saved'`          | 53     |
| `'checkpoint-failed'`         | 54     |
| `'health-changed'`            | 55     |
| `'recovery-scheduled'`        | 56     |
| `'recovery-started'`          | 57     |
| `'recovery-succeeded'`        | 58     |
| `'circuit-open'`              | 59     |
| `'popup-blocked'`             | 60     |
| `'popup-routed'`              | 61     |
| `'download-blocked'`          | 62     |
| `'download-started'`          | 63     |
| `'download-progress'`         | 64     |
| `'download-completed'`        | 65     |
| `'download-failed'`           | 66     |
| `'strategy-started'`          | 67     |
| `'strategy-ready'`            | 68     |
| `'strategy-log'`              | 69     |
| `'strategy-event'`            | 70     |
| `'strategy-failed'`           | 71     |
| `'strategy-heartbeat-missed'` | 72     |
| `'strategy-quarantined'`      | 73     |
| `'strategy-reloaded'`         | 74     |
| `'project-runtime-started'`   | 75     |
| `'project-runtime-paused'`    | 76     |
| `'project-runtime-resumed'`   | 77     |
| `'project-runtime-stopped'`   | 78     |
| `'project-runtime-failed'`    | 79     |
| `'filter-hit'`                | 80     |
| `'filter-css-applied'`        | 81     |

### 联合类型与字面量

#### `StrategyRunAt`

源码位置：`src/shared/contracts.ts:435`

| 字面量             | 源码行 |
| ------------------ | ------ |
| `'document-start'` | 435    |
| `'document-end'`   | 435    |
| `'document-idle'`  | 435    |

#### `StrategyFrameScope`

源码位置：`src/shared/contracts.ts:437`

| 字面量  | 源码行 |
| ------- | ------ |
| `'top'` | 437    |
| `'all'` | 437    |

#### `StrategyPermission`

源码位置：`src/shared/contracts.ts:439`

| 字面量                 | 源码行 |
| ---------------------- | ------ |
| `'dom.read'`           | 440    |
| `'dom.write'`          | 441    |
| `'dom.click'`          | 442    |
| `'config.read'`        | 443    |
| `'log'`                | 444    |
| `'project.emit-event'` | 445    |
| `'network.request'`    | 446    |
| `'main-world'`         | 447    |

#### `StrategyLogLevel`

源码位置：`src/shared/contracts.ts:541`

| 字面量    | 源码行 |
| --------- | ------ |
| `'debug'` | 541    |
| `'info'`  | 541    |
| `'warn'`  | 541    |
| `'error'` | 541    |

#### `StrategyHostMethod`

源码位置：`src/shared/contracts.ts:557`

| 字面量              | 源码行 |
| ------------------- | ------ |
| `'log'`             | 558    |
| `'emit-event'`      | 558    |
| `'network-request'` | 558    |
| `'main-world-eval'` | 558    |

#### `StrategyRuntimeStatus`

源码位置：`src/shared/contracts.ts:489`

| 字面量        | 源码行 |
| ------------- | ------ |
| `'issued'`    | 490    |
| `'scheduled'` | 490    |
| `'starting'`  | 490    |
| `'running'`   | 490    |
| `'failed'`    | 490    |

#### `StrategyRuntimeSignalType`

源码位置：`src/shared/contracts.ts:532`

| 字面量        | 源码行 |
| ------------- | ------ |
| `'installed'` | 533    |
| `'started'`   | 533    |
| `'ready'`     | 533    |
| `'heartbeat'` | 533    |
| `'failed'`    | 533    |

#### `WorkflowFailureCategory`

源码位置：`src/shared/contracts.ts:242`

| 字面量             | 源码行 |
| ------------------ | ------ |
| `'network'`        | 243    |
| `'timeout'`        | 244    |
| `'authentication'` | 245    |
| `'page-changed'`   | 246    |
| `'renderer'`       | 247    |
| `'disk'`           | 248    |
| `'cancelled'`      | 249    |
| `'unknown'`        | 250    |

#### `ProjectRuntimeMode`

源码位置：`src/shared/contracts.ts:202`

| 字面量        | 源码行 |
| ------------- | ------ |
| `'manual'`    | 203    |
| `'autostart'` | 203    |
| `'resident'`  | 203    |
| `'schedule'`  | 203    |

#### `ProjectRuntimeStatus`

源码位置：`src/shared/contracts.ts:358`

| 字面量         | 源码行 |
| -------------- | ------ |
| `'queued'`     | 359    |
| `'starting'`   | 360    |
| `'running'`    | 361    |
| `'waiting'`    | 362    |
| `'recovering'` | 363    |
| `'paused'`     | 364    |
| `'failed'`     | 365    |
| `'completed'`  | 366    |
| `'cancelled'`  | 367    |

#### `DownloadRecordStatus`

源码位置：`src/shared/contracts.ts:400`

| 字面量          | 源码行 |
| --------------- | ------ |
| `'reserved'`    | 401    |
| `'downloading'` | 401    |
| `'completed'`   | 401    |
| `'failed'`      | 401    |
| `'cancelled'`   | 401    |

#### `PageHealthStatus`

源码位置：`src/shared/contracts.ts:105`

| 字面量           | 源码行 |
| ---------------- | ------ |
| `'starting'`     | 106    |
| `'healthy'`      | 106    |
| `'degraded'`     | 106    |
| `'unresponsive'` | 106    |
| `'stopped'`      | 106    |

#### `ContentFilterResourceType`

源码位置：`src/shared/contracts.ts:220`

| 字面量         | 源码行 |
| -------------- | ------ |
| `'stylesheet'` | 221    |
| `'image'`      | 221    |
| `'media'`      | 221    |
| `'font'`       | 221    |
| `'script'`     | 221    |
| `'xhr'`        | 221    |
| `'other'`      | 221    |

#### `WorkflowNode` 的 kind 取值

工作流节点类型由可辨识联合的 `kind` 字段表达，没有独立的联合类型别名。

源码位置：`src/shared/contracts.ts:261`

| kind             | 源码行 |
| ---------------- | ------ |
| `navigate`       | 263    |
| `wait-for-event` | 268    |
| `delay`          | 273    |
| `complete`       | 278    |

### 宿主方法所需权限

权限来自 manifest 的 `permissions` 字段，未声明对应权限时调用会抛错。

| 宿主方法          | 所需权限             |
| ----------------- | -------------------- |
| `log`             | `log`                |
| `emit-event`      | `project.emit-event` |
| `network-request` | `network.request`    |
| `main-world-eval` | `main-world`         |

### 限额常量

下列数值由源码常量读取，数值变更会导致校验失败，需要重新生成。

| 常量                      | 数值   | 位置                     |
| ------------------------- | ------ | ------------------------ |
| `INSTALL_TIMEOUT_MS`      | 5000   | `strategy-runtime.ts:24` |
| `MAX_HOST_PAYLOAD_BYTES`  | 131072 | `strategy-runtime.ts:25` |
| `MAX_LOG_MESSAGE_LENGTH`  | 2000   | `strategy-runtime.ts:26` |
| `FIRST_STRATEGY_WORLD_ID` | 1001   | `strategy-runtime.ts:23` |

各常量含义：

- `INSTALL_TIMEOUT_MS`：bootstrap 安装超时
- `MAX_HOST_PAYLOAD_BYTES`：宿主调用 payload 字节上限
- `MAX_LOG_MESSAGE_LENGTH`：日志消息与失败原因长度上限
- `FIRST_STRATEGY_WORLD_ID`：Strategy 隔离 world 起始编号

### 显式错误文案

下列文案来自源码中的 `throw new Error(...)`，可用于识别失败原因。

含模板插值的文案无法逐字复述，仅给出位置与该文案的稳定前缀。

| 错误文案                                   | 位置                       |
| ------------------------------------------ | -------------------------- |
| `Strategy 宿主调用格式无效`                | `strategy-runtime.ts:199`  |
| `Strategy 执行令牌已失效`                  | `strategy-runtime.ts:203`  |
| `无法创建唯一 Strategy 执行令牌`           | `strategy-runtime.ts:349`  |
| `Strategy 日志参数无效`                    | `strategy-runtime.ts:391`  |
| `Strategy 日志级别或内容无效`              | `strategy-runtime.ts:402`  |
| `Strategy 领域事件名称无效`                | `strategy-runtime.ts:422`  |
| `Strategy 领域事件处理结果不是 JSON 值`    | `strategy-runtime.ts:440`  |
| `Strategy 网络请求格式无效`                | `strategy-runtime.ts:451`  |
| `Strategy 网络请求执行器不可用`            | `strategy-runtime.ts:461`  |
| `main-world 执行参数无效`                  | `strategy-runtime.ts:481`  |
| `Strategy main-world 执行未授权或源码无效` | `strategy-bootstrap.ts:87` |

#### 含插值的文案

- `strategy-runtime.ts:383`：以「Strategy」开头，中间含动态值
- `strategy-runtime.ts:458`：以「Strategy 网络请求未命中
allowlist：」开头，中间含动态值

### 覆盖率统计

| 项目                    | 数量 | 真源                      |
| ----------------------- | ---- | ------------------------- |
| `BrowserShellApi` 方法  | 32   | `src/shared/contracts.ts` |
| `StrategyHostApi` 成员  | 8    | `src/shared/contracts.ts` |
| `IPC_CHANNELS` 通道     | 32   | `src/shared/contracts.ts` |
| `BrowserEventType` 事件 | 39   | `src/shared/contracts.ts` |

渲染进程桥方法数与 IPC 通道数应相等，当前分别为 32 与 32。

### 注入对象交叉校验

`agenticStrategy` 由 `src/preload/strategy-bootstrap.ts` 的模板字符串注入，与
`StrategyHostApi` 的成员必须完全一致。

| 注入成员            | 接口已声明 |
| ------------------- | ---------- |
| `config`            | 是         |
| `log`               | 是         |
| `debug`             | 是         |
| `warn`              | 是         |
| `error`             | 是         |
| `emit`              | 是         |
| `request`           | 是         |
| `evaluateMainWorld` | 是         |
