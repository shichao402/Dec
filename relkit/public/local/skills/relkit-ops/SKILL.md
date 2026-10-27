---
name: relkit-ops
description: >
  为桌面宿主接入 Relkit 更新、在服务器上管理产品与 token、做 Operator 升级与凭据
  轮换。项目存在 relkit.project.json，或用户要求接入、发布、注册产品、签发/轮换
  token、升级 Relkit 时使用。
---

# Relkit 接入与运维

本 Skill 是薄编排：读现场事实、确认真正的选择、调用现有命令。不复制 schema、门禁
或判定逻辑。字段、错误码与后置条件以对应二进制 `--help` 和 JSON 输出为准。

权威依据：[`docs/architecture.md`](../../../docs/architecture.md)（现行架构）与
[ADR 0023](../../../docs/adr/0023-desktop-updater-epoch.md)。本 Skill 与它们冲突时以它们为准，
并回来修本 Skill。

## 思路

Relkit 只做 Windows 与 macOS 桌面应用的更新。服务器是唯一事实源，`relkit-updater`
是唯一引擎；宿主只声明“我是谁、找哪台服务器、信哪把钥匙”。

所以接入的工作量刻意很小：写一份四个必填字段的项目声明，在 CI 里用冻结脚本取工具，
产出制品，交给服务器发布。没有 resolution、`relkitw`、lock、bundle、Candidate 车道
或认证晋升。看到这些词说明读到的是上一代资料，不要照做。

## 组件分工

| 组件 | 谁用 | 做什么 |
| --- | --- | --- |
| `relkit` | 宿主 CI | `artifacts produce/merge`、`payload build`、`bootstrap build`、`stage`、`release`、`inspect`、`build`、`custody` |
| `relkit-updater` | 宿主应用 | 唯一更新状态机，宿主经 IPC 调用 |
| `relkit-agent` | 服务器 | 发布、签名、产品与 token 管理 API、工具分发 |
| `relkit-store` | 服务器 | 数据面 |
| `relkit-console` | 服务器 | 管理面板（产品、token、用户、角色） |
| `relkit-ops` | Operator | `status`、`plan`、`upgrade`、`rotate`、`key` |

`relkit-ops` 只有这五个子命令。`bootstrap provision`、`bundle *`、`distribution mirror`、
`artifact-set push/pull`、`impact generate`、`certify`、`catalog transition`、
`agent policy`、`escape audit` 都已删除，执行会报 `unknown relkit-ops command`。

## 宿主接入

完整合同见 [`host-update.md`](host-update.md)。步骤：

1. 在目标服务器的面板注册产品。注册时会自动签发第一个 token，明文只显示一次，
   直接存进宿主 CI 的密钥。
2. 取服务器根公钥：`GET <server>/v1/tools/root.pub`。内网与外网是不同服务器、不同
   根，不能混用。
3. 在宿主仓库根写 `relkit.project.json`：`product`、`server`、`rootPublicKey` 必填，
   其余字段按需。不要写 relkit 版本、摘要、token、SSH 或私钥。再写 `relkit.custody.json`
   登记 token 的 keyId 与环境变量名。
4. CI 取 `cli` 工具（下载、验签、执行；现役宿主用 `scripts/relkit_tool.py`，参考实现是
   relkit 的 `scripts/ci/relkit-tool-verify.ps1` / `.sh`），
   然后 `artifacts produce` → `merge` → `stage` → `release --execute`。
5. 自带更新面的宿主额外 `bootstrap build`，把产出原文内嵌进应用，交给引擎。

缺少用户意图时，只问会实质改变产品行为的问题（产品名、渠道、注册到哪台服务器）。
能从服务器或仓库查到的事实不要反问用户。

## 产品与 token 管理

全部在面板（`<server>/-/admin/`）或 agent 管理 API 上做：

- 注册产品：自带第一个 token；产品根留空时取 agent 配置的 `productRootParent/<id>`。
- 签发、轮换、吊销 token：轮换保持 id、换明文；吊销保留记录作审计。
- 下线产品：先预演，看清会删的注册与 token，以及残留的发布树目录；再确认下线。
  下线不删磁盘目录，残留目录按提示手工清理。

机上命令只剩首装引导：`relkit-agent init -config <cfg> -bootstrap`，生成一次性凭据，
用来创建第一个操作员账户。`-product`、`-token-only`、`-share-with` 已删除。

## 撤回版本

版本号不珍贵。发错的版本：

```text
relkit-agent unpublish -product <id> -version <v> --dry-run
relkit-agent unpublish -product <id> -version <v>
```

然后发更高版本修正。被撤的号不复用，已装的坏版本靠更高版本 fix-forward。

## Operator 升级

先计划，再执行：

```text
relkit-ops status --inventory <inventory> --json
relkit-ops plan --inventory <inventory> --to <version|latest> --out upgrade.json --json
relkit-ops upgrade <operator> --inventory <inventory> --plan upgrade.json --execute --json
```

升级只暴露完整旧 Profile 或完整新 Profile，失败保持旧内容。

服务二进制（agent / store / console）的替换走 [`relkit-deploy`](../relkit-deploy/SKILL.md)，
不在本 Skill 范围。

## 凭据轮换

```text
relkit-ops rotate <credential-name> --inventory <inventory> --execute --json
```

顺序固定：mint → 更新 secret store → reload/restart → 健康与写权限验证 → 吊销旧
凭据 → 激活。输出只允许 opaque secret reference，禁止打印或提交 token。

## 密钥

```text
relkit-ops key generate --key-id <id> --private <p> --public <p> --json
relkit-ops key fingerprint --public <p> --json
relkit-ops key rename --old-key-id <id> --new-key-id <id> --old-private <p> --private <p> --public <p>
```

每台服务器一把根，私钥只在该服务器与离线备份。任何时候“私钥找不到”，先跑托管
自检（见 [`docs/guide/trust-root-custody.md`](../../../docs/guide/trust-root-custody.md)），
自检说没有才算真的没有。不要因为找不到就生成新根：换根会让已装应用全部失去信任链。

## 收尾

失败按稳定 `code` 归因，修正 core、ops 或宿主声明。禁止：

- 为单一宿主加产品名特判；
- 在宿主里恢复 Python host、onboarding、`relkitw`，或在取工具脚本里钉版本、文件名、摘要；
- 在 CI 里加检查这些规矩的守卫步骤（规矩靠评审与文档，见架构文档第六节）。

本次问题若是 Skill 误导造成的，直接修本 Skill；若暴露了产品缺口，在对应仓库修复并补
回归测试。
