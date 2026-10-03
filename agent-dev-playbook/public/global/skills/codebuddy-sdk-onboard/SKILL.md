---
name: codebuddy-sdk-onboard
description: >
  接入 @tencent-ai/agent-sdk 的经验手册，覆盖凭证环境、query/session、
  自定义 MCP 工具、多轮 resume，以及 React/Tauri 和 Flutter/Web 对话交互。
  新产品要接 CodeBuddy Agent、聊天面板或从其他 Agent 运行时迁移时使用。
---

# CodeBuddy Agent SDK 接入

这里接的是 Node 运行时 SDK `@tencent-ai/agent-sdk`，不是 CodeBuddy IDE，
也不是再安装一份 `codebuddy` CLI。

仓库里存在 `.codebuddy/` skills/rules，不代表 SDK 会自动加载它们。

## 一、先确定宿主

SDK 会启动 Agent 子进程、访问文件系统并读取密钥，不能放进浏览器。

| 产品宿主 | SDK 所在位置 | UI |
|---|---|---|
| Tauri / Electron | 原生侧或 Node sidecar | React 等现有 Web 技术栈 |
| Node 服务 + Web | 服务端 Core | 浏览器通过 HTTP/SSE |
| Flutter Web | Node 服务端 Core | 纯 Dart 对话 UI |

桌面发行物应捆绑 Node 和 SDK，不假设用户机器装有系统 Node 或 CodeBuddy CLI。
密钥不得进入渲染进程、Web 产物或项目仓库。

## 二、运行模式

接入前明确选择一种模式：

| 模式 | `settingSources` | 用途 |
|---|---|---|
| 一次性隔离任务 | `[]` | 批处理、不可信输入、禁止加载本机插件和 MCP |
| 项目感知 Agent | `["project"]` | 需要目标仓 skills、MCP、项目说明 |

项目感知模式下，`cwd` 必须指向目标仓库根，即项目配置真正所在的位置。
不要把“内容工作目录”误当“项目配置根”。

## 三、凭证、环境和模型

1. API Key 只放本机用户配置、系统钥匙串，或已忽略的服务端私密配置。
2. 国内/iOA 环境同时设置 SDK `environment` 和
   `env.CODEBUDDY_INTERNET_ENVIRONMENT`。漏传时内部 Key 可能落入 external login。
3. 产品配置可暴露 `public`、`internal`、`ioa`；public 映射到 SDK `external`，
   并删除进程继承的国内环境变量。
4. 模型目录以 SDK 实测为准：优先 `getAvailableModelsRaw()`，
   再退到 `getAvailableModels()` 或 `query.supportedModels()`。
5. 不使用 `codebuddy --help` 的静态模型列表判断 SDK 能力。

SDK 升级前先跑两个真实探针：列模型、最小 `query`。项目感知模式还要验证
skill/MCP 是否真的可见。

## 四、`query()` 的关键约束

### 自定义工具

- `options.tools` 只是白名单，不能注册工具。
- 自定义工具必须用 `createSdkMcpServer` + `tool`（zod schema）注册，
  再放进 `mcpServers`。
- 只把名字写进 `tools` 时，模型可能退化为在正文输出 JSON；
  整轮仍显示 completed，业务产物却为空。
- `canUseTool` 收到的名称常为 `mcp__<server>__<tool>`，而非裸工具名。
  白名单与权限判断必须兼容两种名称。
- system 消息展示的全量工具目录不代表白名单已生效。

结构化业务结果只通过工具回传，不解析 assistant 正文。

### 多轮会话

- 用 `resume` 延续同一时间线，不用 fork 冒充多轮。
- 尽早保存消息中的 `session_id`，客户端断线后仍能继续。
- resume 只恢复模型对话历史。用户在 UI 中手工修改过的暂存数据，
  必须在每一轮 prompt 中重新附上当前真值。

### 流、超时和取消

- 正常结果应等到 `type === "result"`，并检查成功 subtype。
- SDK 偶尔在 assistant 正文后不补 result；可保留最后一段正文作为受控兜底。
- abort 可能让流安静结束而不抛异常，必须先判断取消/超时状态。
- 使用 `AbortController`，并让上层 UI 能取消正在运行的 query。
- 模型有时把沙箱文件写成 `/asset.png`。可按 basename 回退到沙箱内文件，
  但最终路径仍必须经过沙箱边界校验。

### 权限

- 隔离任务：默认拒绝，只放行沙箱内必需工具。
- 项目内 Agent：可以允许工作工具，再用 git diff/文件策略做写后审计。
- 两者威胁模型不同，不要机械复用同一个 `permissionMode`。

## 五、交互和 UI

产品需要的是对话壳，不是把 CodeBuddy IDE 嵌进应用。

### 桌面 Web 技术栈

已有 React/Tauri/Electron 时，可用 `@assistant-ui/react`：

- 会话列表与续接
- 流式 assistant 文本
- 可折叠 reasoning / tool call / tool result
- artifact 卡片
- 取消运行

SDK 留在原生侧或 sidecar。WebView 通过应用 bridge/invoke 通信；
不要照搬库示例中的 `fetch('/api/chat')`。

### Flutter（尤其 Flutter Web）

使用纯 Dart 对话 UI（例如 `flutter_chat_ui`）和自有 SSE 客户端。

不要为了 assistant-ui 新建 Vite 子工程再 iframe 嵌入：

- 多一套构建和发布链
- iframe 会吞父层指针事件
- 父子窗口都要校验 origin 和维护加载/错误状态
- `webview_flutter` 不支持 Flutter Web

可以参考 assistant-ui 的流式处理思路，但按 Dart 技术栈实现。

### SSE 契约

发起 Agent 回合需要 POST body，因此不能使用只支持 GET 的浏览器 `EventSource`。
使用 POST 流式响应，并定义稳定事件：

```text
session
run_start
text_delta
tool_call
tool_result
artifact（或业务产物事件）
error
run_end
```

- `run_end` 在成功、失败、取消、超时时都必须发送，客户端靠它结束 busy。
- 未知事件应忽略，实现前向兼容。
- 流式消息使用“完整新文本替换”，不要依赖消息对象可变。
- 反向代理对 Agent SSE 路径关闭 buffering 和 gzip 缓冲，否则会等很久后一次性显示。

### UX 最低要求

- 未配置 Key 时显示设置入口，不发起假会话。
- 支持会话列表、同 session 续接和取消当前 run。
- 工具过程可折叠，不与最终正文混为一谈。
- 大型 artifact 用卡片打开，不塞进聊天气泡。
- 结构化产物先预览/确认，再进入产品已有审阅流程。
- 鉴权失败明确显示 error，不能以 HTTP 200 空结果假装成功。

## 六、验收清单

1. 明确宿主位置和运行模式。
2. Key、environment、model 配置不进入仓库或前端。
3. 真实运行列模型与最小 query 探针。
4. 验证 `settingSources` 下项目 skills/MCP 的可见性符合预期。
5. 自定义工具已注册，权限检查兼容带前缀工具名。
6. 结构化结果通过工具返回，并有失败回归测试。
7. 多轮同时验证 resume 与 UI 真值重贴。
8. 验证流式、取消、超时、断线续接和统一 `run_end`。
9. 在真实反向代理/桌面安装包中冒烟，不只在开发服务器测试。

## 七、明确不做

- 不把 SDK 打进 Web bundle。
- 不把 CodeBuddy CLI 当作产品运行依赖。
- 不把 `.codebuddy/` 的存在当作 SDK 已加载项目配置。
- 不解析模型散文充当结构化 API。
- 不为第二种 Agent 运输再复制一套业务 Job/REST。
- 不在 Flutter Web 中用 iframe 嵌 assistant-ui。
