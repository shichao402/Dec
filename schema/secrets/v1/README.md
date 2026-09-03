# secrets/v1 Schema

Protobuf 是 Bitwarden **secrets bundle** 绑定声明的 schema 真相源；运行时 wire format 为 YAML（`~/.dec/secrets/config.yaml`），secret 明文不进 Git。

相关文档：[BUNDLE-SECRETS-MODEL.md](../../Documents/BUNDLE-SECRETS-MODEL.md) · [0002 SyncTarget ADR](../../Documents/decisions/0002-secrets-synctarget-root.md) · [ARCHITECTURE.md](../../Documents/ARCHITECTURE.md)

**Bitwarden 认证状态不进 schema、不进磁盘**：session、vault/user key 与 2FA 中间态仅在
相关进程内存中保存。Console Authenticate 是唯一人工入口；本机交互 MCP 可拉起/聚焦
Console 并等待，远端无桌面与 CI 返回结构化错误。见
[ADR 0022](../../Documents/decisions/0022-console-bitwarden-unlock.md)。

## 核心模型：SyncTarget + 存储根分离

Dec Git Vault 以 **bundle** 组织公开资产，落地在 **`.dec/`**；Bitwarden 以 **secrets bundle**（folder，默认同名）存放私密文件，落地在 **`.secrets/`**：

```
SyncTarget{bundle: vikunja}
  folder: vikunja
  LocalRoot: .secrets/bundles/vikunja

Dec bundle（→ .dec/cache/vikunja/）:
  mcp/vikunja-mcp.json          # command: dec-exec，无 token

Bitwarden folder: vikunja
  Secure Note 名（相对 LocalRoot）:
  → .env/vikunja.env

  [SSH Key] .sshkey/deploy  Notes: vikunja.example.com
  → ~/.ssh/dec_vikunja_deploy
```

- **Secure Note 名称** = 相对 **SyncTarget.LocalRoot** 的路径（如 `.env/vikunja.env`）。
- Pull 后落到 `<project>/.secrets/bundles/vikunja/.env/vikunja.env`，**不进** `.dec/cache/`。
- 环境变量只认 `.env/*.env`；由独立 `dec-exec` 按 bundle 注入。
- **SSH Key**：Item 名 `.sshkey/<实例>`，落地 `~/.ssh/dec_<bundle>_<实例>`；有 hosts 时更新 Dec 管理 `~/.ssh/config` 区块。
- **点类型目录**（[0005](../../Documents/decisions/0005-secrets-machine-handlers.md)）：`.gcm/` / `.sshkey/` / `.env/` 决定识别与处理；如 `.gcm/cnb.yaml` → GCM handler。同一张表也是 Remote 登记的同级 Processor（各自声明来源与 Bitwarden Writer）。

**Invariant**：`.dec/` 树与 `.secrets/` 树 **不得相交**；`.secrets/` 须被 `.gitignore` 忽略。

## 消息概览

| 消息 | 用途 |
|------|------|
| `BundleBinding` | Dec bundle ↔ Bitwarden folder 可选别名 |
| `SecretsConfig` | bundle 侧 secrets 配置入口 |

## 没有本地同步状态文件

权威索引：**远端 folder 的 note 列表**（pull/delete/list）+ push 时 **递归扫描 LocalRoot**。不引入 `state.json`。

## 生成 Go 类型

```bash
cd schema
buf lint
buf generate
```

生成物输出到 `schema/gen/go/`。
