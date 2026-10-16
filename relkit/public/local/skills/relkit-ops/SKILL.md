---
name: relkit-ops
description: >
  为宿主接入 Relkit vNext，管理 Operator、Candidate 认证、Catalog 晋升、
  完整离线 bundle、原子升级和凭据轮换。项目存在 relkit.project.json，
  或用户要求接入、升级、认证、轮换 Relkit 时使用。
---

# Relkit vNext 运维

本 Skill 是薄编排，不实现第二份 schema、门禁或“完成步骤”账本。

- `relkit`：产品 core；负责 resolve、ArtifactSet、stage/publish、协议和 updater。
- `relkit-ops`：机器/Operator 面；负责 Profile、Catalog、供给、认证、升级和凭据。
- 本 Skill：读取结构化证据、向用户确认真正的选择、调用上述命令。

所有字段、错误码和后置条件以对应二进制当前 `--help` 与 JSON 输出为准。禁止在
Skill 中复制一份校验逻辑，禁止手改签名文档绕过失败。

## 宿主接入

1. 审查宿主只提交：
   - `relkit.project.json`
   - Relkit 生成的 `relkitw` / `relkitw.cmd`
   - 宿主自己的 producer / UI / 业务代码
2. 项目声明只含产品、逻辑 `operatorRef`、信任指纹、角色、能力、构建入口。
   不得含 Relkit 版本、真实服务器地址、Token、SSH、私钥或 mirror 凭据。
3. 先运行：

```text
relkit-ops status --inventory <operator-inventory> --json
relkit-ops bootstrap provision --inventory <operator-inventory> --json
./relkitw inspect --json
./relkitw doctor --resolution .relkit/resolution.json --project relkit.project.json --json
```

4. 缺少用户意图时，只问会实质改变声明或 Operator 的问题；不要把可从
   `status/inspect/doctor` 得到的事实反问用户。

## 首次 clone 的三条供给路径

- 公网：组织提供钉死 SHA-256 的 bootstrap URL；wrapper 校验后再运行。
- 仅 mirror：`bootstrap provision` 预置 Operator 映射和 bootstrap binary；
  resolution 中记录 mirror→公开源顺序，每个文件仍验 Catalog 摘要。
- 完全离线：由 ops 创建并安装完整 distribution bundle：

```text
relkit-ops bundle create \
  --project relkit.project.json \
  --resolution-bundle <resolution-bundle.json> \
  --asset <sha256:path> --out <bundle-dir>
relkit-ops bundle verify --project relkit.project.json --bundle <bundle-dir> --json
relkit-ops bundle install \
  --project relkit.project.json --project-root . \
  --target <os-arch> --bundle <bundle-dir> --json
```

Bundle 必须包含所选 distribution 的全部 component/release asset；缺一个摘要即失败。
不得只塞一份 CLI 冒充完整离线供给。

## 发布

宿主 producer 只返回 `relkit.artifact-set/1`。产品 CI 固定到 merged ArtifactSet
为止：

```text
relkitw doctor
relkitw artifacts produce / merge
上传 merged ArtifactSet
```

独立 Operator Job 才执行：

```text
stage → simulate → fake verify → publish → verify released
```

同一次发布只生成一次 immutable resolution，跨 Job 传递原文件。不得中途再次解析
`latest`。产品 Job 不持 endpoint/token/签名私钥；Operator 不改产品业务选择。
关键 artifact/manifest/index/directory/Runtime Profile 任一步失败都不得报告成功；
网页投影失败可告警并重建。

Candidate resolution 使用独立 Candidate Root 和 agent candidate lane，只能发非
stable 渠道。生产与 candidate 凭据、Catalog 和写权限不得共用。

## Candidate 认证与 Catalog 晋升

1. 用 production Catalog、candidate Catalog/Profile 和两套公开根生成 impact：

```text
relkit-ops impact generate ... --risk <documentation|single-capability|contract|cross-cutting> --out impact.json
```

2. 用 ops 私有 fleet 登记选择 cohort/canary 并运行反向 CI：

```text
relkit-ops certify --fleet <fleet.json> --impact impact.json --out certification.json --json
```

- 普通能力/合同变更：受影响 cohort 的固定 canary + 轮换样本。
- 新能力：首个声明该能力的宿主临时 mandatory。
- 签名、resolution、发布事务、协议、registry、IPC 等横切变更：`--full`，
  所有已登记活跃宿主。
- 某宿主声明的内网/外网 CI 任一路缺失，结果必须为 `skipped-unverified`，不能认证。

3. 只有通过的 `relkit.certification-report/1` 才能晋升：

```text
relkit-ops catalog transition \
  --state certified --certification-report certification.json ...
```

Candidate authority 本身不能签 certified Catalog。

## Operator 升级

先展示计划，再执行：

```text
relkit-ops plan --inventory <inventory> --to <version|latest> --out upgrade.json --json
relkit-ops upgrade <operator> --inventory <inventory> --plan upgrade.json --execute --json
```

计划必须列出服务、mirror、合同变化和精确 distribution。执行顺序固定为
candidate→sync→deploy→verify→activate。任一步失败运行 rollback，active Profile
保持旧内容；不得让项目看见半升级 Profile。

## 凭据轮换

```text
relkit-ops rotate <credential-name> --inventory <inventory> --execute --json
```

完成顺序固定为 mint→更新 secret store→服务用户可读→reload/restart→健康及写权限
验证→吊销旧凭据→原子激活新 Profile。任一步失败都不得标 active。输出只允许 opaque
secret reference，禁止打印或提交 token。

## 收尾

使用结构化输出复查：

```text
relkit-ops status --inventory <inventory> --json
./relkitw inspect --json
./relkitw doctor --resolution .relkit/resolution.json --project relkit.project.json --json
relkit verify released ...
```

失败按稳定 `code` 归因并修正式 core/ops/声明。禁止为单一宿主加仓库名特判，
禁止恢复 Python host/onboarding、复制 bootstrap 或平行门禁。
