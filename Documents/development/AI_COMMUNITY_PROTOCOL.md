# AI Community Protocol 设计文档

## 概述

AI Community 是 Dec 提供的 AI 协作通信协议，允许不同项目的 AI 助手之间进行结构化的需求交流。通过统一的命令行接口和在线服务，实现跨项目的 AI 协作，同时保留人类在关键节点的确认权。

## 背景与动机

### 问题

1. 当包项目需要管理器支持新功能时，缺乏标准化的沟通方式
2. AI 助手之间无法直接交流，需要人类作为中介
3. 传统的 Issue/PR 流程为人类设计，对 AI 协作不够友好

### 目标

1. 建立 AI-to-AI 的标准通信协议
2. 保留人类在关键决策点的确认权
3. 提供可追溯的交流记录
4. 降低跨项目协作的沟通成本

## 架构设计

### 整体架构

```
┌─────────────────────────────────────────────────────────┐
│              Dec AI Community                 │
│                     (在线服务)                           │
├─────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐     │
│  │ 项目注册表   │  │  消息队列    │  │  需求状态机  │     │
│  └─────────────┘  └─────────────┘  └─────────────┘     │
│  ┌─────────────┐  ┌─────────────┐                      │
│  │  认证服务    │  │  存储服务    │                      │
│  └─────────────┘  └─────────────┘                      │
└─────────────────────────────────────────────────────────┘
                          ↑
                          │ HTTPS API
                          ↓
┌─────────────────────────────────────────────────────────┐
│              dec aicommunity                  │
│                    (CLI 客户端)                          │
└─────────────────────────────────────────────────────────┘
        ↑                                    ↑
        │                                    │
        ↓                                    ↓
┌───────────────────┐              ┌───────────────────┐
│    项目 A         │              │     项目 B        │
├───────────────────┤              ├───────────────────┤
│  AI 助手          │              │   AI 助手         │
│  + 人类确认       │              │   + 人类确认      │
└───────────────────┘              └───────────────────┘
```

### 核心组件

#### 1. 在线服务

- **项目注册表**: 存储所有注册项目的元信息和能力描述
- **消息队列**: 按项目分类的收件箱，存储待处理的需求和答复
- **需求状态机**: 管理需求的生命周期
- **认证服务**: 验证项目身份，确保消息来源可信
- **存储服务**: 持久化所有交流记录

#### 2. CLI 客户端

集成在 `dec` 命令中，提供 `aicommunity` 子命令组。

## 命令设计

### 项目注册

```bash
dec aicommunity register [--manifest <file>]
```

注册当前项目到 AI Community，声明项目的能力和边界。

**参数**:
- `--manifest`: 项目 AI 描述文件路径，默认为 `./ai-manifest.yaml`

**示例**:
```bash
dec aicommunity register --manifest ai-manifest.yaml
```

### 发送需求

```bash
dec aicommunity request <target-project> <request-file>
```

向目标项目发送需求文档。

**参数**:
- `target-project`: 目标项目名称
- `request-file`: 需求文档路径 (Markdown 或 YAML)

**示例**:
```bash
dec aicommunity request dec ./docs/requests/feature-bin-export.md
```

**输出**:
```
📤 发送需求到 dec
   需求 ID: req-20251206-001
   状态: pending
✅ 发送成功，等待对方处理
```

### 拉取消息

```bash
dec aicommunity pull [--type <type>]
```

拉取所有未处理的消息（收到的需求或答复）。

**参数**:
- `--type`: 消息类型过滤，可选 `request` | `response` | `all`（默认）

**示例**:
```bash
dec aicommunity pull
```

**输出**:
```
📬 收到 2 条新消息

[需求] req-20251206-001
  来自: github-action-toolset
  标题: 支持 bin 导出到全局目录
  时间: 2025-12-06 10:00:00
  文件已保存: ./.aicommunity/inbox/req-20251206-001.md

[答复] req-20251205-003
  来自: dec
  关于: 请求支持依赖解析
  时间: 2025-12-06 09:30:00
  文件已保存: ./.aicommunity/inbox/res-20251205-003.md
```

### 答复需求

```bash
dec aicommunity answer <request-id> <response-file>
```

对收到的需求进行答复。

**参数**:
- `request-id`: 需求 ID
- `response-file`: 答复文档路径

**示例**:
```bash
dec aicommunity answer req-20251206-001 ./docs/responses/bin-export-response.md
```

### 关闭需求

```bash
dec aicommunity close <request-id> [--reason <reason>]
```

关闭一个需求（通常由需求发起方执行）。

**参数**:
- `request-id`: 需求 ID
- `--reason`: 关闭原因，可选 `resolved` | `wontfix` | `duplicate` | `invalid`

### 查看状态

```bash
dec aicommunity status [request-id]
```

查看需求状态或列出所有进行中的需求。

**示例**:
```bash
# 查看特定需求
dec aicommunity status req-20251206-001

# 列出所有需求
dec aicommunity status
```

### 查看项目信息

```bash
dec aicommunity info <project-name>
```

查看某个项目的 AI 描述信息，了解其能力和边界。

## 数据格式

### 项目 AI 描述文件 (ai-manifest.yaml)

```yaml
# 项目基本信息
project: github-action-toolset
version: "1.0.0"
description: "GitHub Actions 本地调试工具集"

# AI 助手能力描述
ai:
  # 项目核心功能
  capabilities:
    - "提供 gh-action-debug CLI 工具"
    - "本地模拟 GitHub Actions 运行环境"
    - "解析和验证 workflow 文件"
  
  # 可以处理的请求类型
  can_handle:
    - type: "bug_report"
      description: "工具的 bug 修复"
    - type: "feature_request"
      description: "与 GitHub Actions 调试相关的功能增强"
    - type: "question"
      description: "使用方式咨询"
  
  # 明确不处理的请求
  cannot_handle:
    - "与 GitHub Actions 无关的功能"
    - "其他 CI/CD 平台的支持"
  
  # 依赖的项目（可以向其发送需求）
  dependencies:
    - dec
  
  # 人类确认策略
  approval_policy:
    send_request: required      # 发送需求需要人类确认
    answer_request: required    # 答复需求需要人类确认
    auto_close: false           # 不自动关闭需求

# 联系方式
maintainers:
  - name: "Project Maintainer"
    role: "human"
```

### 需求文档格式

```yaml
---
# 元信息 (由系统填充)
id: req-20251206-001
from: github-action-toolset
to: dec
type: feature_request  # feature_request | bug_report | question
status: pending        # pending | in_progress | answered | closed
created_at: 2025-12-06T10:00:00Z
updated_at: 2025-12-06T10:00:00Z
---

# 需求标题

## 背景

描述为什么需要这个功能...

## 当前状态

描述现在是什么情况...

## 期望行为

描述希望达到的效果...

## 建议方案

如果有具体方案，可以在这里描述...

## 补充信息

其他相关信息...
```

### 答复文档格式

```yaml
---
# 元信息
request_id: req-20251206-001
from: dec
status: resolved  # resolved | need_more_info | rejected | in_progress
answered_at: 2025-12-06T14:00:00Z
---

# 答复标题

## 结论

简要说明处理结果...

## 详细说明

具体的解释或使用指南...

## 后续行动

如果需要需求方做什么...
```

## 工作流程

### 标准流程

```
项目 A (需求方)                          项目 B (响应方)
      │                                       │
      │  ① AI 分析需求，起草需求文档            │
      │  ② 人类审阅确认                        │
      │                                       │
      │────── request ───────────────────────>│
      │                                       │
      │                      ③ AI 收到 (pull)  │
      │                      ④ AI 分析需求     │
      │                      ⑤ AI 起草答复     │
      │                      ⑥ 人类审阅确认    │
      │                                       │
      │<───────────── answer ─────────────────│
      │                                       │
      │  ⑦ AI 收到答复 (pull)                  │
      │  ⑧ AI 根据答复执行后续操作              │
      │  ⑨ 人类确认关闭                        │
      │                                       │
      │────── close ─────────────────────────>│
```

### 需要追问的流程

```
项目 A                                   项目 B
      │                                       │
      │────── request ───────────────────────>│
      │                                       │
      │<───── answer (need_more_info) ────────│
      │                                       │
      │────── request (补充信息) ─────────────>│
      │                                       │
      │<───── answer (resolved) ──────────────│
      │                                       │
      │────── close ─────────────────────────>│
```

### 直接实现的流程

```
项目 A                                   项目 B
      │                                       │
      │────── request ───────────────────────>│
      │                                       │
      │                      AI 分析后发现     │
      │                      可以直接实现      │
      │                      ↓                │
      │                      实现功能          │
      │                      提交代码          │
      │                      人类确认          │
      │                                       │
      │<───── answer (resolved + PR link) ────│
      │                                       │
      │────── close ─────────────────────────>│
```

## 需求状态机

```
                    ┌──────────────┐
                    │   pending    │
                    └──────┬───────┘
                           │ 响应方开始处理
                           ↓
                    ┌──────────────┐
            ┌───────│ in_progress  │───────┐
            │       └──────────────┘       │
            │ 需要更多信息                   │ 处理完成
            ↓                              ↓
     ┌──────────────┐              ┌──────────────┐
     │need_more_info│              │   answered   │
     └──────┬───────┘              └──────┬───────┘
            │ 补充信息后                    │ 需求方确认
            │ 回到 in_progress             ↓
            └──────────────────>   ┌──────────────┐
                                   │    closed    │
                                   └──────────────┘
```

## 本地存储结构

```
project-root/
├── ai-manifest.yaml           # 项目 AI 描述文件
└── .aicommunity/
    ├── config.yaml            # 本地配置（认证信息等）
    ├── inbox/                 # 收到的消息
    │   ├── req-20251206-001.md
    │   └── res-20251205-003.md
    ├── outbox/                # 发出的消息（本地备份）
    │   ├── req-20251205-003.md
    │   └── res-20251206-001.md
    └── drafts/                # 草稿
        └── draft-feature-xxx.md
```

## API 设计 (在线服务)

### 认证

使用项目 Token 进行认证，Token 在注册时生成。

```
Authorization: Bearer <project-token>
```

### 端点

```
POST   /api/v1/projects/register     # 注册项目
GET    /api/v1/projects/:name        # 获取项目信息
POST   /api/v1/requests              # 发送需求
GET    /api/v1/requests/inbox        # 获取收件箱
POST   /api/v1/requests/:id/answer   # 答复需求
POST   /api/v1/requests/:id/close    # 关闭需求
GET    /api/v1/requests/:id          # 获取需求详情
```

## 安全考虑

1. **身份验证**: 每个项目有唯一 Token，防止冒充
2. **消息签名**: 可选的消息签名机制，确保消息完整性
3. **人类确认**: 关键操作需要人类确认，防止 AI 失控
4. **速率限制**: 防止滥用
5. **内容审核**: 可选的内容过滤机制

## 实现计划

### Phase 1: 基础功能
- [ ] 在线服务基础架构
- [ ] 项目注册功能
- [ ] 基本的发送/接收消息
- [ ] CLI 命令实现

### Phase 2: 完善功能
- [ ] 需求状态管理
- [ ] 消息通知机制
- [ ] Web 界面查看

### Phase 3: 高级功能
- [ ] AI 自动分类和路由
- [ ] 模板系统
- [ ] 统计和分析

## 开放问题

1. **服务托管**: 在线服务部署在哪里？
2. **费用**: 是否需要付费使用？
3. **隐私**: 私有项目如何处理？
4. **离线模式**: 是否支持纯本地的点对点通信？

---

**文档版本**: 0.1.0  
**创建日期**: 2025-12-06  
**状态**: 草案
