# 0002 — Secrets SyncTarget：`.secrets` 同步根镜像

- **状态**：已接受（实施中）
- **日期**：2026-08-09
- **取代**：[0001-secrets-landing-path.md](0001-secrets-landing-path.md)
- **影响范围**：`pkg/secrets/`、`pkg/app/secrets_*.go`、`pkg/app/delete.go`、`internal/tui/`、`Documents/BUNDLE-SECRETS-MODEL.md`、`schema/secrets/v1/`、`.cursor/rules/`

## 问题

0001 把 Secure Note 名定为「项目根消费者路径」，解决了 mise 直读问题，但带来：

1. 密文散落项目根，push **无法安全枚举**本地文件，新文件只能手工登记
2. project 与 bundle 在 Bitwarden 上虽共用 folder 协议，本地却没有统一边界
3. 环境变量注入与落地路径耦合，第三方 MCP 需要外部启动器

## 决策

**以 SyncTarget 为唯一同步单位：Bitwarden folder ↔ 本地 `.secrets` 下某个同步根；Note 名 = 相对该同步根的路径。**

```text
SyncTarget{kind: bundle, name: vikunja}
  folder: bundle/vikunja（默认带 bundle/ 前缀，与 project folder 区分）
  LocalRoot: .secrets/bundles/vikunja
  note "env/vikunja.env" → <project>/.secrets/bundles/vikunja/env/vikunja.env

SyncTarget{kind: project, name: Dec}
  folder: Dec（裸 project 名）
  LocalRoot: .secrets/project
  note "config/private.yaml" → <project>/.secrets/project/config/private.yaml
```

配套：

- 环境变量只认 `env/*.env`（dotenv，单行标量）；由 hidden `dec exec` 按 bundle 作用域注入；**不再**用外部 env 启动器
- SSH Key 仍落 `~/.ssh/`（机器平面例外）
- push **递归扫描** LocalRoot（可 create/update）；pull/push **不隐式删除**
- **folder 约定**：project = 实体名；bundle = `bundle/<name>`；可用 `secrets_bundle` 显式覆盖
- `.secrets/` 整树必须被 gitignore；已被跟踪则硬失败

## 理由

- **可枚举**：有边界才能安全 push 发现新文件
- **不散落**：密文有唯一明文目录，公开平面仍是 `.dec/`
- **同构**：project 与 bundle 在 Bitwarden 上协议完全一致
- **注入自主**：Dec 控制作用域，第三方 MCP 只读 `process.env`

## 被否方案

**A. 继续 0001 消费者路径直落。**  
否决：无法扫盘 push；密文边界不清。

**B. `.secrets` 暂存 + 再部署到消费者路径。**  
否决：双副本漂移；mise bridge / 符号链接增加 trust 与格式约束。改为 `dec exec` 直接读 `.secrets/**/env/*.env`。

**C. 保留 vikunja_workflow 硬编码别名。**  
否决：特例会无限增生；历史别名由用户手工整理或显式 binding。

## 参考

- `Documents/BUNDLE-SECRETS-MODEL.md`
- `.cursor/rules/bundle-secrets-mirror.mdc`
