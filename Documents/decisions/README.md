# 决策记录

记录 Dec 的架构决策：**决定了什么**、**为什么**、**否掉了哪些方案**。

设计文档（`ARCHITECTURE.md`、`BUNDLE-SECRETS-MODEL.md` 等）描述系统**当前**是什么样；
决策记录描述它**为什么**是这样。两者冲突时以决策记录为准，并应尽快把设计文档改平。

## 何时新增

- 定下一个后续会被反复追问「当初为什么这么选」的取舍
- 推翻既有做法
- 引入或删除兼容层

## 约定

- 文件名 `NNNN-短横线标题.md`，编号递增，不复用
- 决策一旦被取代，在原文件顶部标注 `已被 NNNN 取代`，不删除原文
- 必须写「被否方案」及其否决理由——这是记录里最容易被省掉、日后最有价值的部分

## 索引

| 编号 | 标题 | 状态 |
|------|------|------|
| [0001](0001-secrets-landing-path.md) | Secrets 落地路径：消费者路径即落地路径 | 已被 0002 取代 |
| [0002](0002-secrets-synctarget-root.md) | Secrets SyncTarget：`.secrets` 同步根镜像 | 已接受（实施中） |
| [0003](0003-user-enabled-secret-bundles.md) | 用户级 Bundle 启用（机器平面） | 已接受；并集语义被 0009 取代 |
| [0004](0004-remote-page.md) | Remote 页：上下文无关完整远端编辑器（方案 R） | 已接受（已实现） |
| [0005](0005-secrets-machine-handlers.md) | Secrets Machine Handlers：点类型目录（`.gcm` / `.sshkey` / `.env`） | 已接受 |
| [0006](0006-retire-pkg-for-internal.md) | 源码布局：废除 `pkg/`，统一到 `internal/` | 已接受（已实现） |
| [0007](0007-machine-secrets-root.md) | 机器级 bundle secrets 根 + 项目覆盖层 | 已接受；覆盖层被 0009 取代 |
| [0008](0008-service-facade-split.md) | Dec 服务 / 门面拆分 | 已接受（规划中） |
| [0009](0009-bundle-binary-scope.md) | Bundle 二元 scope（user \| project） | 已接受（实施中） |
| [0010](0010-pull-orphan-and-ops.md) | Pull 孤儿收敛、删除收敛与运维面修订 | 已接受（已实现） |
