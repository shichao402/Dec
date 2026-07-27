# secrets/v1 Schema

Protobuf 是 Bitwarden **secrets bundle** 绑定声明的 schema 真相源；运行时 wire format 为 YAML（`~/.dec/secrets/config.yaml`），secret 明文不进 Git。

相关文档：[BUNDLE-SECRETS-MODEL.md](../../Documents/BUNDLE-SECRETS-MODEL.md) · [ARCHITECTURE.md](../../Documents/ARCHITECTURE.md) · [README.md](../../README.md)

**Bitwarden session 不进 schema、不进磁盘**：session 仅在 Dec/TUI 进程内存中保存；认证通过 TUI 触发的本地 HTTP web unlock（主密码 + 可选 2FA）完成。详见 [BUNDLE-SECRETS-MODEL.md — Bitwarden 认证](../../Documents/BUNDLE-SECRETS-MODEL.md#bitwarden-认证)。

## 核心模型：Bundle 同构 + 存储根分离

Dec Git Vault 以 **bundle** 组织公开资产，落地在 **`.dec/`**；Bitwarden 以 **secrets bundle**（同名/绑定）存放私密文件。Secure Note / mise env 落地 **项目根相对路径**；SSH Key 落地 **机器级 `~/.ssh/`**（OpenSSH/Git 默认只认该路径）。

```
Dec bundle（→ .dec/cache/vikunja/）:
  mcp/vikunja-mcp.json          # command: mise，无 token

Bitwarden folder: vikunja_workflow
  Secure Note 名 = 项目相对路径:
  → .config/mise/conf.d/vikunja.toml

  [SSH Key] deploy  Notes: vikunja.example.com
  → ~/.ssh/dec_vikunja_deploy
  → ~/.ssh/dec_vikunja_deploy.pub
  → ~/.ssh/config（Dec 管理区块）
```

- **Secure Note 名称** = 敏感文件在项目根的 **目标相对路径**（不是 bundle 内虚拟路径）。
- **Note 内容** = 该路径文件的完整正文。
- Pull 后落到 `<project>/.config/mise/conf.d/vikunja.toml`，**不进** `.dec/cache/`。
- **SSH Key**：Name = 逻辑名，Notes = hosts（可选；有内容时一行一个）；Pull 落地 `~/.ssh/dec_<bundle>_<name>`；有 hosts 时再更新 Dec 管理 `~/.ssh/config` 区块；**无** 项目根 `keys/`。

**Invariant**：`.dec/` 树与敏感落地路径 **不得相交**；冲突时 pull **报错**，不覆盖。

详细设计见 [Documents/BUNDLE-SECRETS-MODEL.md](../../Documents/BUNDLE-SECRETS-MODEL.md)。

## 消息概览

| 消息 | 用途 |
|------|------|
| `BundleBinding` | Dec bundle ↔ Bitwarden secrets bundle 绑定 |
| `SecretsConfig` | bundle 侧 secrets 配置入口 |

## 没有本地同步状态文件

曾有 `state.proto`（`SyncStateFile` / `SecretRef` / `NoteRef` / `EnvEntry`）打算用
`~/.dec/secrets/state.json` 记录「哪些本地文件由 Bitwarden 管」，作为 push 时的枚举依据。
它从未被实现，且方向是错的：本地状态文件会与远端漂移，漂移后 push 依据的是一份过期索引。

现在**权威索引是远端 folder 的 note 列表**，每次操作实时枚举，没有可漂移的本地副本。
需要知道某个文件是否被管，就查它所属 folder 的 note 列表。

## 生成 Go 类型

```bash
cd schema
buf lint
buf generate
```

生成物输出到 `schema/gen/go/`。
