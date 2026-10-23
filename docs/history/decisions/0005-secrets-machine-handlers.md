# 0005 — Secrets Machine Handlers：点类型目录路由

- **状态**：已接受
- **日期**：2026-08-13
- **影响范围**：`internal/secrets/sectype.go`、`internal/secrets/processor.go`、`internal/secrets/handler/`、`internal/app/secrets_pull.go`、`internal/app/remote_register.go`、`Documents/BUNDLE-SECRETS-MODEL.md`
- **修订**：
  - 2026-08-13 — 机器级 Note 默认落 `~/.dec/secrets/bundles/`（见 [0007](0007-machine-secrets-root.md)）
  - 2026-08-17 — **点类型目录**取代 `{实例}_{处理器}.yaml`；统一 `.gcm` / `.sshkey` / `.env`

## 问题

部分密钥不是「落到 `.secrets/` 文件就够」，还要写入**机器平面**副作用（例如 CNB 仅支持 HTTPS，需把 token 灌进 Git Credential Manager）。若继续散落脚本或硬编码特例，会无法水平扩展。旧 `*_gitgcm.yaml` 命名晦涩，且与 `env/`、SSH 识别方式不一致。

## 决策

### 点类型目录（统一识别）

```text
.<类型>/<实例>…
```

| 类型 | BW 侧标识 | 本机落点 |
|------|-----------|----------|
| `.gcm/<实例>…` | Secure Note 名 | 同步根 → gcm Handler Apply → GCM |
| `.env/<name>.env` | Secure Note 名 | 同步根 → `dec-exec` |
| `.sshkey/<实例>` | **SSH Key Item 名** | `~/.ssh/dec_<bundle>_<实例>`（剥前缀） |

- 框架只认**路径首段**选类型；正文格式 / 后缀由各处理器自定。
- 未知点目录（如 `.foo/`）**硬失败**。
- 存量一次性迁移（pull 前）：`*_gitgcm.yaml` → `.gcm/`，`env/` → `.env/`，裸 SSH 名 → `.sshkey/`；主路径不长期双认。

### SourceKind（有限闭集）

- `note` — Secure Note  
- `ssh_item` — SSH Key Item  

### Handler（开放注册）

按 SourceKind + 点目录 Match；首个 Note handler 为 `gcm`（目录 `.gcm`）。

### gcm 处理器契约（示例）

正文由 gcm 自定（当前为 YAML）：

```yaml
host: cnb.cool
username: cnb
password: "<token>"
# protocol: https
# provider: generic
```

`kind` 字段可选；若填写须为 `gcm`（兼容旧值 `gitgcm`）。

Apply：`git config --global credential…` + `git credential approve`。  
Revoke：删除 Note 前 `credential reject` + `--unset provider`。

### SSH

仍是 BW SSH Item，**不是** Secure Note。规范 Item 名为 `.sshkey/<实例>`；落地文件名只用实例。

### Remote 登记：同级 Processor

`note` / `.env` / `.gcm` / `.sshkey` 在 Remote `n`/`N` 里是**同级 Processor**：

| Processor | Bitwarden Writer | 登记来源 |
|-----------|------------------|----------|
| `note` | Secure Note Writer | temp / 路径 / 系统选文件 |
| `.env` | Secure Note Writer | 同上 |
| `.gcm` | Secure Note Writer | 同上；Pull 后才 GCM Apply |
| `.sshkey` | SSH Key Item Writer | 本机生成 / 路径 / 系统选文件 |

TUI 只跑统一状态机（归属 → Processor → 名称 → 来源 → 提交），不按类型开旁路。GCM Apply 属于 Pull 后处理，不属于创建链路。

Writer 契约对齐：两条 Writer（`PushBundle` / `CreateSSHKey`）都支持「目标 folder 不存在时按需创建」，供 Remote `N` 使用。

## 理由

- 点目录一眼可辨「特殊语义」，与普通 `config/` 内容目录区分。
- 识别层统一，存储层仍按 SourceKind 分叉。
- 迁存量一次到位，避免永久双认。

## 被否方案

**A. SSH 也改成 Secure Note 子目录。** 否决：丢失 BW SSH 字段。  
**B. 无点前缀 `gcm/`。** 否决：与内容目录混淆。  
**C. 长期双认旧路径。** 否决：心智负担；改为一次性迁移。  
**D. 仅 UI 展示 `.sshkey`、不改 BW Item 名。** 否决：标识不彻底。

## 参考

- [0002 SyncTarget](0002-secrets-synctarget-root.md)
- `Documents/BUNDLE-SECRETS-MODEL.md`
- 示例：`Documents/examples/.gcm/cnb.yaml.example`
