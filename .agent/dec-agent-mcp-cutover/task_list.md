# 任务跟踪：dec-agent MCP 切换与源码断代

计划见 [task-item.md](task-item.md)，需求见 [requirements.md](requirements.md)。本文件只跟踪执行状态，不替代计划。

## 前置

- [x] 把 `delete_asset` 按路径删除修复拆成独立提交（d5c47e1）
- [x] 把文档归档与合并拆成独立提交（25fb2dd）

## 1. 源码断代

- [x] 1.1 生成新一代依赖图并确定删除清单
- [x] 1.2 执行删除并收敛构建
- [x] 1.3 编写依赖守卫测试

## 2. dec-agent 的 MCP 协议层

- [x] 2.1 用官方 SDK 替换 `/mcp` 转发壳
- [x] 2.2 定义工具清单并统一转发到云端
- [x] 2.3 错误语义与测试

## 3. 本机 IDE 自动接入

- [x] 3.1 实现 `dec` 内置条目的写入与移除
- [x] 3.2 新增 `dec-agent mcp-config` 命令

## 4. 真实链路验收

- [x] 4.1 编写 MCP 验收客户端与 `scripts/acceptance.sh`
- [x] 4.2 发布新版本并对生产执行验收

## 验收标准对照

完整对照表见 [requirements.md](requirements.md) 末尾的「验收对照表」，每条标准都指向一项脚本检查或一个具名测试。

发布与验收记录（2026-10-23）：

- `v2.1.3` 经 CI 发布，`dec-cloud` 部署成功，`required_client` 同步到 `v2.1.3`
- 本机代理升级到 `v2.1.3`，`aligned: true`
- `scripts/acceptance.sh` 退出码 0，16 项检查全部 PASS，无残留
- `/mcp` 握手已本地完成（不再出现 `bootstrap_closed`），外部 Origin 返回 403
