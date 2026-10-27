# 实施计划：服务端 CA 私钥的 Bitwarden 备份与丢失恢复

- [ ] 1. 同步需求与决策文档
   - 修改 `.agent/dec-cloud/requirements.md`：需求 9 第 5 条、9.5、11.6 改为“CA 私钥只在本机和 Bitwarden，不进 git”，需求 3.5 补充“同一份私钥从 Bitwarden 恢复”
   - 在 `docs/adr/` 新增 ADR，记录决策、理由和未采纳的“加密入 git”方案
   - 修改 `internal/cloudsvc/tls.go` 顶部注释，与新行为一致
   - _需求：1.5、8.1、8.2、8.3_

- [ ] 2. 定义 CA 记录与 CA 状态机
- [ ] 2.1 在 SSOT 中新增 CA 公开记录
   - 在 `internal/cloud/ssot/types.go` 定义 `config/server-ca.json`，字段包括 schema_version、CA 证书 PEM、指纹、`backup_state`（`pending` 或 `bitwarden`）、创建时间
   - 读写函数加上并发锁，写入走现有的提交与冷备推送
   - 单元测试：序列化往返；文件中不出现私钥
   - _需求：1.3、1.4、2.6_
- [ ] 2.2 用状态判定替换 `loadOrCreateCA`
   - 新函数根据“本机 `ca.key` 是否存在”和“SSOT 是否有记录”这两个条件，返回 `ready`、`needs_restore`、`fresh`、`mismatch`、`legacy_unbacked` 五种状态之一，不再自动生成 CA
   - `mismatch` 状态拒绝启动业务，日志写明两边的指纹；`legacy_unbacked` 状态补写 SSOT 记录（`backup_state=pending`），业务照常运行
   - 表驱动单元测试覆盖五种状态，并断言 `needs_restore` 下不生成新 CA
   - _需求：2.1、2.3、2.4、2.6_

- [ ] 3. 服务端恢复模式与门禁
   - `internal/cloudsvc/server.go` 和 `dispatch.go`：CA 不处于 `ready` 时，需要设备证书的方法统一返回新错误码 `server_ca_pending_restore`；登记、恢复、健康检查方法保持放行
   - 健康检查和设备通道带上 CA 状态与 `backup_state=pending` 提示
   - `internal/proto` 增加错误码和状态字段；单元测试验证门禁放行名单
   - _需求：2.2、2.7_

- [ ] 4. Bitwarden 读写 CA 私钥
   - 在 `internal/cloud/identity/bootstrap.go` 扩展同一会话内的操作：按指纹精确读取或写入 `_dec-cloud/server-ca/<指纹>`；写入后读回比对；完成后登出并清除凭据
   - 根密钥和 CA 私钥同时缺失时，在一次登录中依次取回两者
   - 使用假 Bitwarden 的单元测试覆盖：精确匹配（存在同前缀条目时不误选）、条目缺失、内容与证书不匹配、写入后读回不一致
   - _需求：1.2、2.5、3.1、3.2、4.5_

- [ ] 5. 恢复、首次初始化与宣告丢失三条流程
- [ ] 5.1 从 Bitwarden 恢复 CA
   - 在 `method_enroll.go` 一侧新增恢复方法：校验通过后写入 `ca.key`（权限 0600），切换到 `ready` 状态，并写入审计
   - 找不到条目或校验失败时保持恢复模式，返回“重试 / 宣告丢失”两个选项
   - _需求：3.2、3.3、3.4、3.5_
- [ ] 5.2 首次生成、宣告丢失与补备份
   - 共用一条流水线：生成 → 写入 Bitwarden → 读回比对 → 写入 `ca.key` → 更新 SSOT 并提交 → 启用；任一步失败即回滚，不启用新 CA
   - 宣告丢失需要显式确认参数，确认提示包含受影响的设备数；启用后把旧 CA 签发的设备标记为 `needs_reenroll`，不删除记录
   - `legacy_unbacked` 状态的补备份走同一条流水线，跳过“生成”这一步
   - 集成测试：中途任一步注入失败，都断言旧状态未被改变
   - _需求：4.1、4.2、4.3、4.4、3.5_

- [ ] 6. 代理端适配
   - `internal/agent/client.go`：识别证书被拒和 `server_ca_pending_restore`，分别提示“服务端身份已重置，需要重新登记”和“服务端 CA 待恢复”；离线降级逻辑不变
   - `loopback.go` 和 `adminjs.go`：管理页增加“恢复 CA”和“宣告丢失并重建”入口（含二次确认），主密码和 2FA 沿用现有输入框，不落盘
   - `enroll.go`：同一台设备重新登记时，服务端把 `needs_reenroll` 记录原有的订阅迁移到新证书上
   - 单元测试：错误提示映射；重新登记后订阅集合保持不变
   - _需求：6.1、6.2、6.3、6.4_

- [ ] 7. 应急命令适配
   - `internal/cloud/emergency/emergency.go` 和 `cmd/dec-emergency`：`issue-cert` 只读本机 `ca.key`，缺失时拒绝执行并提示先走恢复流程；`revoke` 行为不变
   - 补充对应的单元测试
   - _需求：7.1、7.2、7.3_

- [ ] 8. 部署快照排除 CA 私钥
   - `deploy/dec-cloud-deploy.sh`：生成快照时排除 `certs/ca.key`，回滚时不覆盖当前的 `ca.key`；部署后清理已有快照中的 `ca.key`
   - 在 `internal/guard` 增加守卫测试，断言部署脚本包含排除规则，并且仓库里不会写入 `ca.key`
   - _需求：1.3、5.1、5.2、5.3、5.4_

- [ ] 9. 发布并做生产演练
- [ ] 9.1 发布
   - 全量测试通过后推 `v2.x.y` tag，由 CI 部署；确认现有 CA 进入 `legacy_unbacked` 状态、SSOT 已写入记录、快照中的 `ca.key` 已清除
   - 扩展 `scripts/acceptance-prod.sh`，加入只读检查：CA 状态、`ca.key` 权限、快照中没有 `ca.key`
   - _需求：9.1、2.6、5.3_
- [ ] 9.2 演练一：把现有 CA 当作已丢失
   - 删除生产机的 `ca.key` 并重启服务 → 进入 `needs_restore` → 尝试恢复失败（Bitwarden 里没有条目）→ 宣告丢失并重建；暂停等用户输入主密码和 2FA
   - Mac 重新登记（再次暂停等用户输入），确认订阅恢复、`drift` 归零
   - _需求：9.2、9.4、4.3、4.4、6.3_
- [ ] 9.3 演练二：从 Bitwarden 恢复
   - 再次删除 `ca.key` 并重启服务 → 从 Bitwarden 恢复（暂停等用户输入）→ Mac 无需重新登记即自动重连
   - 用线上验收脚本确认：只有一份 `ca.key`，快照中没有副本，审计中两次演练记录完整
   - _需求：9.3、9.5、3.4、3.5_
