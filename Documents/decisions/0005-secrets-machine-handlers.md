# 0005 — Secrets Machine Handlers：SourceKind + 名字路由

- **状态**：已接受（实施中）
- **日期**：2026-08-13
- **影响范围**：`internal/secrets/handler/`、`internal/app/secrets_pull.go`、`Documents/BUNDLE-SECRETS-MODEL.md`、`schema/secrets/v1/`（文档）
- **修订**：2026-08-13 — 机器级 Note 默认落 `~/.dec/secrets/bundles/`（见 [0007](0007-machine-secrets-root.md)）

## 问题

部分密钥不是「落到 `.secrets/` 文件就够」，还要写入**机器平面**副作用（例如 CNB 仅支持 HTTPS，需把 token 灌进 Git Credential Manager）。若继续散落脚本或硬编码特例，会无法水平扩展。

## 决策

分两层：

1. **SourceKind（有限闭集）**：对齐 Bitwarden 当前可同步的条目类型  
   - `note` — Secure Note  
   - `ssh_item` — SSH Key Item  
   以后若支持 Login 等，再增枚举；此层变更慢。
2. **Handler（开放注册）**：按 **源类型 + 名字约定** 路由；理论可无限扩展。

### Note 处理器命名

仅**结构化处理器 Note**使用：

```text
{实例}_{处理器}.yaml
```

例：`cnb_gitgcm.yaml` → 实例 `cnb`，处理器 `gitgcm`。

- 扩展名固定 `.yaml`（实现可兼容 `.yml`），便于 IDE 识别。
- 可放在 SyncTarget 同步根任意子路径；**路由看 basename**。
- 正文为 YAML；须含 `kind: <处理器>`，与文件名后缀双重校验，不一致则硬失败。
- **仍先按 0002 镜像落到 `.secrets/`**，再执行 `Handler.Apply`（机器平面副作用）。未匹配的 Note 只落盘。

### 普通 Note

`env/foo.env`、`config/x.json` 等**不**要求 YAML，走默认落盘；只有匹配 `_{处理器}.yaml` 的才进 Registry。

### SSH

继续走现有 SSH Key 落地（`~/.ssh/` + config）。在模型上它属于 `SourceKind=ssh_item` 的默认 handler；不必把私钥硬塞进 Secure Note。

### 首个 Handler：`gitgcm`

```yaml
kind: gitgcm
host: cnb.cool
username: cnb
password: "<token>"
protocol: https          # 可选，默认 https
provider: generic        # 可选，默认 generic
```

Apply：

1. `git config --global credential.<protocol>://<host>.provider <provider>`
2. `git credential approve` 写入 GCM / 当前 credential helper

适合用户级启用的 secrets-only bundle（见 0003），新机器：装 Dec → 解锁 BW → pull → 即可 `git push https://cnb.cool/...`。

## 理由

- **有限源类型**约束与 Bitwarden 能力对齐，避免假装「一切皆 Note」。
- **名字 + 扩展名**让磁盘与远端同构，IDE 可识别 YAML。
- **Registry** 把 CNB/GCM 等特例变成可插拔处理器，主 pull 流程不再堆 `if`。

## 被否方案

**A. 全部处理器都做成 Secure Note，SSH 也改成 Note。**  
否决：丢失 Bitwarden SSH 字段与现有落地；SourceKind 有限层失去意义。

**B. 仅靠 YAML 内 `kind`、文件名随意。**  
否决：列表不可扫、易漏路由。

**C. Note 名不带 `.yaml`（如 `cnb_gitgcm`）。**  
否决：落盘后 IDE 无法识别；已改为必须带后缀。

**D. 直接调 Windows Credential API，绕过 GCM。**  
否决：跨平台差；应走 `git credential`。

## 参考

- [0002 SyncTarget](0002-secrets-synctarget-root.md)
- [0003 用户级 Bundle 启用](0003-user-enabled-secret-bundles.md)
- `Documents/BUNDLE-SECRETS-MODEL.md`
