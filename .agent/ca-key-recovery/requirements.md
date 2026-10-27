# 需求文档：服务端 CA 私钥的 Bitwarden 备份与丢失恢复

## 引言

Dec Cloud 的服务端 CA 私钥（下称“CA 私钥”）用来签发和校验设备证书。现状有三个问题：

1. 它只以明文存放在生产机 `/var/lib/dec-cloud/certs/ca.key`，没有任何机外备份。服务器一丢，所有已登记设备都要重新登记。
2. [tls.go](/Users/firoyang/workspace/Dec/internal/cloudsvc/tls.go) 的 `loadOrCreateCA` 发现 `ca.key` 缺失时，会悄悄生成新 CA，既不提示也不备份，旧设备只会莫名其妙地断连。
3. 两份部署快照里各带一份明文 `ca.key`，回滚时会把旧 CA 带回来。

本次的决策（2026-10-27 用户确认）：CA 私钥不进 git、不进冷备仓库，改为首次生成后写入 Bitwarden；服务器连同磁盘都丢失时，从 Bitwarden 取回。这样替代原需求里“用根密钥加密后存进 git”的方案。理由是两种方案的安全性相同，而 Bitwarden 方案更简单：CA 私钥生成后不再变化，根密钥轮换时不用重新加密它，应急签发也不依赖根密钥。

验收方式：把生产上现在的 CA 私钥当作已丢失，走一遍正常流程。Bitwarden 主密码和 2FA 只在流程走到需要的那一步时由用户输入，不为这件事单独做一次手工补写。

不在本次范围：生产机遗留 GitHub token 的轮换（文件已删，轮换由用户手动完成）；根密钥本身的备份方式（维持现状）。

## 需求

### 需求 1：CA 私钥的存放与备份边界

**用户故事：** 作为 Dec 的唯一用户，我希望 CA 私钥只存在两个地方：服务器本机和 Bitwarden，以便在服务器彻底丢失时仍能恢复，同时不扩大泄露面。

#### 验收标准

1. 服务端 SHALL 把 CA 私钥存放在本机 `certs/ca.key`，权限 0600，属主为服务账户。
2. 服务端 SHALL 把 CA 私钥的唯一机外副本存放在 Bitwarden 条目 `_dec-cloud/server-ca/<CA 指纹>`。
3. CA 私钥 SHALL NOT 以任何形式（明文或密文）进入 SSOT 仓库、冷备仓库、审计仓库或部署快照。
4. 服务端 SHALL 把 CA 证书（公开部分）或其指纹记录在 SSOT 仓库中（例如 `config/server-ca.json`），作为恢复时校验 CA 私钥的依据。
5. 根密钥轮换（原需求 9.5）SHALL NOT 涉及 CA 私钥。

### 需求 2：CA 私钥缺失时不得静默重建

**用户故事：** 作为用户，我希望服务端发现 CA 私钥不见了时明确停下来，而不是悄悄换掉，以便已登记设备不会无声失效。

#### 验收标准

1. WHEN 服务端启动时 `ca.key` 不存在，且 SSOT 仓库中已有 CA 记录 THEN 服务端 SHALL 进入恢复模式，SHALL NOT 自动生成新 CA。
2. WHEN 服务端处于“CA 缺失”的恢复模式 THEN 需要设备证书的业务接口 SHALL 被拒绝，并返回能指明“服务端 CA 待恢复”的错误；登记与恢复相关的接口 SHALL 保持可用。
3. IF `ca.key` 存在，但与 SSOT 中记录的 CA 指纹不一致 THEN 服务端 SHALL 拒绝启动业务，并在日志中写明不一致，SHALL NOT 覆盖任何一方。
4. WHEN SSOT 仓库中没有任何 CA 记录（全新部署）THEN 服务端 SHALL 走首次初始化流程（需求 4），而不是恢复流程。
5. IF 根密钥与 CA 私钥同时缺失 THEN 服务端 SHALL 在同一次恢复流程中依次取回两者，用户 SHALL 只需输入一次 Bitwarden 主密码和 2FA。
6. WHEN 新版本首次启动时本机已有 `ca.key`，但 SSOT 中没有 CA 记录（即现有生产的存量状态）THEN 服务端 SHALL 只把 CA 证书和指纹写入 SSOT 并提交，标记为“Bitwarden 备份待补”，业务照常运行；SHALL NOT 把这种情况当作全新部署。
7. WHILE CA 处于“Bitwarden 备份待补”状态 THEN 服务端 SHALL 在健康检查和设备通道中持续提示这一状态，直到备份完成。

### 需求 3：从 Bitwarden 恢复 CA 私钥

**用户故事：** 作为用户，我希望服务器重建后从 Bitwarden 取回原来的 CA 私钥，以便已登记设备无需重新登记即可自动重连。

#### 验收标准

1. WHEN 用户在恢复流程中提供 Bitwarden 主密码和 2FA THEN 服务端 SHALL 按 SSOT 中记录的 CA 指纹精确读取 `_dec-cloud/server-ca/<指纹>`，SHALL NOT 按“最新”或模糊匹配选择条目。
2. WHEN 取回 CA 私钥 THEN 服务端 SHALL 校验它与 SSOT 记录的 CA 证书匹配，校验通过后才写入本机 `ca.key` 并退出恢复模式。
3. IF Bitwarden 中找不到对应指纹的条目，或校验失败 THEN 服务端 SHALL 保持恢复模式，并向用户给出两个明确选项：重试，或宣告 CA 丢失（需求 4）。
4. WHEN CA 从 Bitwarden 恢复成功 THEN 已登记设备 SHALL 无需重新登记即可自动重连。
5. 恢复成功、失败和宣告丢失 SHALL 都写入审计。

### 需求 4：首次生成与宣告丢失后重建

**用户故事：** 作为用户，我希望 CA 在首次生成或确认丢失后重建时，都先写进 Bitwarden 并校验，再正式启用，以便不会出现“已经启用但没有备份”的 CA。

#### 验收标准

1. WHEN 需要新建 CA（全新部署，或用户宣告 CA 丢失）THEN 服务端 SHALL 要求用户当场提供 Bitwarden 主密码和 2FA。
2. 服务端 SHALL 按“生成 → 写入 Bitwarden → 读回并比对 → 写入本机 `ca.key` → 更新 SSOT 中的 CA 记录并提交 → 启用”的顺序执行；任一步失败 SHALL 中止，SHALL NOT 启用新 CA。
3. 宣告 CA 丢失 SHALL 需要用户显式确认，确认提示中 SHALL 说明“所有已登记设备都需要重新登记”及受影响的设备数量。
4. WHEN 新 CA 启用 THEN 服务端 SHALL 把旧 CA 签发的所有设备标记为“需要重新登记”，SHALL NOT 直接删除设备记录。
5. Bitwarden 中旧 CA 的条目 SHALL 保留，不由系统自动删除。

### 需求 5：部署快照与回滚不携带 CA 私钥

**用户故事：** 作为用户，我希望部署快照里没有 CA 私钥，以便回滚不会把旧 CA 带回来，也不会多出明文副本。

#### 验收标准

1. 部署脚本生成快照时 SHALL 排除 `certs/ca.key`。
2. WHEN 回滚 THEN 部署脚本 SHALL NOT 覆盖当前的 `certs/ca.key`。
3. 本次上线 SHALL 清除生产机现有两份快照中的 `ca.key` 明文副本。
4. 服务端测试或部署测试 SHALL 断言快照中不含 `ca.key`。

### 需求 6：设备重新登记

**用户故事：** 作为用户，我希望在 CA 重建后，设备能得到清楚的提示并顺利重新登记，以便不丢失原有订阅。

#### 验收标准

1. WHEN 代理因设备证书被新 CA 拒绝而连接失败 THEN 代理 SHALL 给出“服务端身份已重置，需要重新登记”的明确提示，而不是笼统的连接错误。
2. 代理离线期间，已落到本机的资产文件 SHALL 保持不变（沿用离线降级行为）。
3. WHEN 同一台“我的设备”重新登记成功 THEN 它原来的订阅集合 SHALL 能恢复，用户不需要手动逐个重新订阅。
4. 重新登记 SHALL 沿用现有登记流程（含 Bitwarden 主密码和 2FA），不新增旁路。

### 需求 7：应急命令适配

**用户故事：** 作为用户，我希望服务器本机的应急命令在新存储方式下依然可用，以便 Bitwarden 不可用时仍有最后手段。

#### 验收标准

1. `dec-emergency issue-cert` SHALL 读取本机 `certs/ca.key` 签发证书，SHALL NOT 依赖根密钥。
2. IF 本机 `ca.key` 缺失 THEN `issue-cert` SHALL 拒绝执行，并提示“先走恢复流程取回 CA”。
3. `dec-emergency revoke` SHALL 不受 CA 存储方式变化影响。

### 需求 8：需求与决策文档同步

**用户故事：** 作为用户，我希望总需求文档和决策记录与新方案一致，以便后续不会再按旧文字实现。

#### 验收标准

1. `.agent/dec-cloud/requirements.md` 中关于“服务端身份私钥用根密钥加密后存入 git”（需求 9 第 5 条）、根密钥轮换重新加密服务端身份私钥（需求 9.5）、重建只需冷备仓库和根密钥（需求 11.6）三处 SHALL 改为本方案的表述；需求 3.5 SHALL 补充“同一份私钥从 Bitwarden 恢复”。
2. SHALL 新增一条 ADR，记录本次决策、理由和未采纳方案（加密入 git）。
3. `tls.go` 中“丢了就重新登记全部设备”的注释 SHALL 改为与实现一致的说明。

### 需求 9：生产演练（两次）

**用户故事：** 作为用户，我希望把现在的 CA 私钥当作已丢失，在生产上真实走一遍，并紧接着验证从 Bitwarden 恢复，以便确认两条路径都通。

#### 验收标准

1. 新版本 SHALL 只通过 CI（推 `v2.x.y` tag）部署到生产。
2. 演练一（最坏情况）：WHEN 删除生产机 `ca.key` 并重启服务 THEN 服务 SHALL 进入恢复模式；由于 Bitwarden 中尚无 CA 条目，用户宣告丢失后 SHALL 生成新 CA 并写入 Bitwarden；这台 Mac SHALL 完成重新登记且订阅恢复，`drift` 归零。
3. 演练二（预期路径）：WHEN 再次删除 `ca.key` 并重启服务 THEN 服务 SHALL 从 Bitwarden 恢复同一份 CA，这台 Mac SHALL 无需重新登记即自动重连。
4. 演练中需要 Bitwarden 主密码和 2FA 的步骤，SHALL 由流程本身在当时提示用户输入；Agent 在这些步骤暂停等待。
5. 演练结束后，生产机上 SHALL 只有一份 `ca.key`，快照中 SHALL 没有副本，审计中 SHALL 能看到两次演练的完整记录。
