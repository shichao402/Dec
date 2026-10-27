# 0052：服务端 CA 私钥只在本机与 Bitwarden，不进 git

- **状态**：已接受（实现中）
- **日期**：2026-10-27
- **影响范围**：`internal/cloudsvc/tls.go`、`internal/cloud/identity`、`internal/cloud/ssot`（新增 `config/server-ca.json`）、`deploy/dec-cloud-deploy.sh`、`dec-emergency issue-cert`
- **相关**：需求 1.5、3.5、9.5、11.6；[ca-key-recovery 需求](../../../.agent/ca-key-recovery/requirements.md)

## 背景

服务端 CA 私钥用来签发与校验设备证书。它原先只以明文躺在 `certs/ca.key`，没有任何机外备份；`loadOrCreateCA` 发现文件缺失时会静默生成新 CA，已登记设备随即全部无声失效；部署快照里还各带一份明文，回滚会把旧 CA 带回来。

原需求写的是"用根密钥加密后存入 git"。

## 决策

CA 私钥只存在两处：

1. 服务器本机 `certs/ca.key`（0600，属主为服务账户）。
2. Bitwarden 条目 `_dec-cloud/server-ca/<CA 指纹>`，首次生成时写入并读回比对，之后不再变化。

git 里只放 `config/server-ca.json`：CA 证书（公开部分）、指纹与备份状态。它是恢复时按指纹精确取条目、并校验取回内容的依据。

`ca.key` 缺失时服务端不再自动生成，而是进入恢复模式：先从 Bitwarden 取回；取不到时由用户显式宣告丢失，才生成新 CA，并把旧 CA 签发的设备标记为需要重新登记。

## 理由

- **安全性与"加密入 git"相同**：拿到 Bitwarden 里的根密钥加 git 就等于拿到 CA；拿到 Bitwarden 里的 CA 也一样。两者的泄露面都是"Bitwarden 账户"。
- **更简单**：CA 生成后不再变化，Bitwarden 只写一次；根密钥轮换不必重新加密它；应急签发不依赖根密钥。
- **"磁盘上不得有明文"在此场景意义有限**：根密钥本身就明文落在同一台机器上，能读 `ca.key` 的人也能读 `root.key`。

## 未采纳

- **用根密钥加密后存入 git**：多一层间接，轮换时要重新加密，应急命令需要根密钥才能签发，换来的安全收益为零。
- **维持现状（丢了就全部重新登记）**：设备一多代价不可接受，且静默重建会让问题拖到设备断连才暴露。

## 存量过渡

现有生产已有 `ca.key` 但 git 里没有记录。新版本首次启动时只补写公开记录，并标记 `backup_state=pending`，业务照常；健康检查持续提示，直到写入 Bitwarden。验收按正常流程：把现有 CA 当作已丢失演练一次，再删一次验证从 Bitwarden 恢复。
