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
- 本机模式 `scripts/acceptance.sh`：退出码 0，16 项全部 PASS，无残留
- 线上只读模式 `scripts/acceptance.sh --remote`：退出码 0，39 项全部 PASS
- 线上链路模式 `deploy/test-e2e.sh --reset`：退出码 0，链路 13 项 + 隔离核对 11 项全部 PASS
- 线上只读实测：`/mcp` 与 `/admin` 在公网均为 404（入口只在代理回环上）；15 个协议方法无证书一律 `unauthorized`；旧版本调用返回 `version_mismatch` 并带出要求版本

后续三个版本都由 CI 单一路径发布并部署，每个都修了一个真实缺陷：

- `v2.1.4`：CI 两个阻断问题。`actions/checkout` 默认不拉 tag，导致「上一个发布 tag」查不到、required 退化成当前 tag，同时守卫测试误报标签丢失；补 `fetch-tags: true`。其后 `legacy-final` 只存在本地、从未推远端，测试从「跳过」变成真正失败，补一次推送。
- `v2.1.5`：退出维护的并发竞态。`syncDeployFiles` 先读文件再写内存，两步之间若 `SetMaintenance(false)` 已删文件并置 false，就会被旧事实改回 true，表现为「退出维护偶发失效」。两者改用同一把 `deployMu`，并拆出 `consumeDeployRecordLocked` 防重入死锁；新增并发回归测试，去掉锁后第 0 次循环即失败。
- `v2.1.6`：`aligned` 用精确相等判定 required。只改服务端的发布会沿用上一个 required（需求 19.2），于是升级到新版本的代理反被判成不对齐、设备直接不可用。改为比较版本号，不低于 required 即对齐；非法格式返回 0 保持宽松。同步修正线上验收里「要求版本必须等于服务端版本」的旧断言——这条会在每次服务端发布后必然失败，把正常状态报成缺陷。

`v2.1.6` 发布后实测：生产 `pending.json` 为 `v2.1.6` / commit `eb614c6` / `result=ok` / `rolled_back=false`；`healthz` 报 `server_version=v2.1.6`、`required_client=v2.1.4`；本机代理升级到 `v2.1.6` 后 `aligned=true`、`last_error` 为空；本机模式 16 项、线上只读模式 38 项全部通过。

部署路径已收敛为唯一一条：正式发布只能 `git tag v2.x.y && git push origin v2.x.y` 走 CI。本地只做构建、测试与本机验收；`TestProductionDeployOnlyThroughCI` 会检查工作流里有 `refs/tags/v`、`StrictHostKeyChecking=yes`、`payload.tar`，部署入口有 tag 真实性核对，且 `scripts/`、`deploy/` 下没有任何脚本直接往生产机上装二进制。

三种模式验的是三件事：本机模式证明「这台机器能用」，线上只读模式证明「服务器那台机器是对的」，线上链路模式证明「线上真的能跑通完整链路」。MCP 入口只在代理里，云端不提供 `/mcp`。

测试环境与生产同机但完全隔离：独立数据目录 `/opt/dec-cloud-test/<slot>/data`、独立根密钥、端口只监听 `127.0.0.1:18443`、独立 systemd 单元。测试用 Bitwarden 专用账号（2FA 关闭，凭据在 `.secrets/dec/integration/bitwarden.yaml`），其根密钥冷备与生产互不可见。正式部署不用测试账号，生产初始化的主密码与 2FA 由本人当场输入。
