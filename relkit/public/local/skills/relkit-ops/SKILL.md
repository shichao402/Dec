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

先运行结构化命令取得现场事实，只向用户确认会改变产品行为或风险归属的真实选择。
不要要求用户先理解或逐项拍板 root、fleet、Profile、Catalog 字段；将其翻译成
“目标 Operator、最终 distribution、受影响产品、回滚点”后再展示。安全且可机械验证的
内部默认由 core/ops 闭合。

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
  --asset <sha256:...=path> --out <bundle-dir>
relkit-ops bundle verify --project relkit.project.json --bundle <bundle-dir> --json
relkit-ops bundle install \
  --project relkit.project.json --project-root . \
  --target <os-arch> --bundle <bundle-dir> --json
```

Bundle 必须包含所选 distribution 的全部 component/release asset；缺一个摘要即失败。
不得只塞一份 CLI 冒充完整离线供给。安装会把目标平台的 `relkit` 与
`relkit-ops` 一并放入项目 `.relkit/bootstrap/`，后续产品 Job 使用这份受信
`relkit-ops` 传递 ArtifactSet，不继续依赖外部 bootstrap。

Operator 将 Release 组件同步到内容寻址 mirror 时使用 core 命令，不复制宿主脚本：

```text
relkit-ops distribution mirror \
  --distribution <relkit-distribution.json> \
  --release-assets-base <immutable-release-download-base> \
  --mirror-root <operator-mirror-root> --json
```

写凭据只从 `MIRRORS_USERNAME` / `MIRRORS_PASSWORD`（或显式改名后的 env 引用）
读取。命令必须同步 distribution 的全部 component 和 `releaseAssets`，逐项校验
上游摘要、写入 `blobs/<sha256>` 并读回校验；409 仅在
读回字节摘要相同后视为幂等成功。Operator Profile 的 `sources.mirrors` 必须指向
同一 root；不得在产品仓保存 mirror 地址或凭据。

安装 Candidate bundle 时还必须显式授权独立候选根：

```text
relkit-ops bundle install \
  --project relkit.project.json --project-root . \
  --target <os-arch> --bundle <bundle-dir> \
  --candidate-root-fingerprint <sha256:...> --json
```

缺少或不匹配该指纹必须失败；它不能从 bundle 自己推导，也不能复用为 stable
发布授权。Production bundle 不需要该参数。

## 发布

宿主 producer 只返回 `relkit.artifact-set/1`。产品 CI 固定到 merged ArtifactSet
为止：

```text
relkitw doctor
relkitw artifacts produce / merge
relkit-ops artifact-set push \
  --project relkit.project.json \
  --artifact-set <merged/artifact-set.json> \
  --url <build-unique-immutable-url> --json
```

`push` 返回的 `archiveSha256` 和 URL 必须作为成对证据传给独立 Operator Job；
URL 至少包含不可复用的 CI build identity，禁止覆盖另一次构建。Operator Job 先
按摘要拉回：

```text
relkit-ops artifact-set pull \
  --project relkit.project.json --url <url> \
  --sha256 <archiveSha256> --out-dir <new-dir> --json
relkitw stage --artifact-set <new-dir/artifact-set.json>
relkitw release --version <version>
relkitw release --version <version> --execute
relkitw verify released \
  --resolution <immutable-resolution.json> \
  --version <version> --channel <channel> \
  [--consumer <loopback-url>] [--directory <loopback-url>]
```

同一次发布只生成一次 immutable resolution，跨 Job 传递原文件。不得中途再次解析
`latest`。产品 Job 只持 ArtifactSet 传输凭据，不持 endpoint/agent token/签名私钥；
只有 Operator Job 可注入 `RELKIT_UPLOAD_TOKEN`，且必须设置
`RELKIT_RELEASE_VIA_CI=1`。Operator 不改产品业务选择。
关键 artifact/manifest/index/directory/Runtime Profile 任一步失败都不得报告成功；
网页投影失败可告警并重建。

正式场景必须让 bundle 安装后的 Runtime Bootstrap 提供 Product RUP Root；显式
`--product-root <product-rup-root.pb>` 只用于诊断旧环境变量或 mapping，不得成为唯一
验收证据。公开 SDK 还必须解析控制面实际生成的 bootstrap 字节，不能只解析手写 fixture。

`release --execute` 报错时不能直接假设“没有发布”。若日志显示已上传或可能已提交，
先用同一 version/channel、immutable resolution 和匹配
`runtimeProfilePublication.rootFingerprint` 的 Product RUP Root 从消费者入口执行
有界重试的 `verify released`：

- 验证通过：保留原发布，不重发同一 immutable version。
- 明确未提交：才允许重试 execute。
- 部分提交：按 `partial-commit` / fix-forward 处理，不覆盖历史。

Candidate verify 必须显式授权 Candidate Root；production verify 不得继续携带 Candidate
Root。Candidate Root 与 Product RUP Root 职责不同，禁止用其中一个替代另一个。

Candidate resolution 使用独立 Candidate Root 和 agent candidate lane，只能发非
stable 渠道。生产与 candidate 凭据、Catalog 和写权限不得共用。

## Candidate 认证与 Catalog 晋升

先创建仓库外的不可复用工作目录（CI 使用 `${RUNNER_TEMP}`）；impact、report、Catalog/Profile
快照不得写回源码 checkout，否则 clean-source 门禁必须失败。

先明确**认证域**：待晋升 Catalog、目标 Operator/发布责任边界与一份 fleet 共同定义。
fleet 是该域内的匿名场景、固定 canary、临时首用者及 `requiredCi` 登记，不是服务器集群，
也不是所有 Relkit 使用者仓库。公网与内网若是不同 Operator/责任边界，使用不同 Catalog
快照和 fleet；一个域的 report 不能晋升另一个域。

1. 用 production Catalog、candidate Catalog/Profile 和两套公开根生成 impact：

```text
relkit-ops impact generate ... \
  --escape-registry conformance/production-escapes --repo-root . \
  --risk <documentation|single-capability|contract|cross-cutting> \
  --out <outside-repo-work-dir>/impact.json
```

2. 用 ops 私有 fleet 登记选择 cohort/canary 并运行反向 CI：

```text
relkit-ops certify \
  --fleet <fleet.json> --impact <outside-repo-work-dir>/impact.json \
  --escape-registry conformance/production-escapes --repo-root . \
  --out <outside-repo-work-dir>/certification.json --json
```

- 每个完整 distribution 先通过可移植的 S1–S3；目标认证域再执行与其产品相关的场景和 canary。
- 普通能力/合同变更：受影响 cohort 的固定 canary + 轮换样本。
- 新能力：首个声明该能力的宿主临时 mandatory。
- 签名、resolution、发布事务、协议、registry、IPC 等横切变更：`--full`，
  该认证域 fleet 的全部 mandatory 条目；不是未来所有客户仓库。
- 只有 Catalog 重算确认无组件或合同变化时才可使用 `documentation`；它默认可零宿主。
- 某域声明的内网/外网 CI 任一路缺失，结果必须为 `skipped-unverified`，不能认证。
  不属于该域的环境可以延期，但必须明确“未在该环境晋升”，不得复用其他域报告。

`impact` 固定 Candidate commit，以及包含测试源码摘要的 regression coverage digest；
`certify` 必须从同一干净 commit 重审、执行当前平台适用的回归并要求 digest 未漂移，
再把结果写入 `relkit.certification-report/2`。Catalog 晋升时当前干净源码也必须仍是
该 Candidate commit。报告绑定 fleet 摘要、宿主项目摘要和完整
`requiredCi` 选择；晋升必须传入同一 fleet 重算，替换或裁剪均失败。只有该报告与当前
registry 仍一致才能晋升：

```text
relkit-ops catalog transition \
  --state certified \
  --fleet <fleet.json> \
  --impact <outside-repo-work-dir>/impact.json \
  --certification-report <outside-repo-work-dir>/certification.json \
  --escape-registry conformance/production-escapes --repo-root . \
  ...
```

Candidate authority 本身不能签 certified Catalog。

## Agent policy 切换

二进制升级与 lane 切换是两件事。每个 deployment 都必须先只读计划，再用本次唯一的
回滚路径原子执行：

```text
relkit-ops agent policy \
  --config <relkit-agent.json> --policy <candidate-or-production-policy.json> --json
relkit-ops agent policy \
  --config <relkit-agent.json> --policy <candidate-or-production-policy.json> \
  --backup <unique-utc-backup.json> --execute --json
```

随后由 deploy adapter 执行 `restart/reload → GET /-/health`；任一步失败都恢复该备份、
再次重启并复验。公网与内网使用不同 policy、`deploymentId` 和证据，不能越域复用。
切 production 时，production bundle 和 policy 都不得继续携带 Candidate Root；CI 移除
`RELKIT_CANDIDATE_ROOT_FINGERPRINT` 后才可触发 stable。

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

如果本次问题没有被现有自动化提前发现，不能只补说明或把 code 记为
“已消化”。先分类：

- `generic` / `agent`：在 Relkit 中创建中央 production escape 记录，关联已经会执行的
  真实回归测试和 tracking issue/PR（已知 fix PR 可直接作为 tracking）；跨组件问题必须
  有真实场景证据。
- 产品专属：记录和回归留在宿主仓，不进入中央 registry。

普通开发/review 对目标分支运行审计并真实执行证据；它允许尚未知的 merge
commit/release，但不允许删除历史记录或缺测试：

```text
relkit-ops escape audit \
  --registry conformance/production-escapes --repo-root . \
  --baseline-ref origin/<base> --execute --json
```

release prep 在修复 merge 后补齐 closure；tag 构建对上一正式 tag 运行严格审计：

```text
relkit-ops escape audit \
  --registry conformance/production-escapes --repo-root . \
  --baseline-ref <previous-stable-tag> --release <vX.Y.Z> --json
```

中央记录的状态与校验均由 `relkit-ops` 推导，Skill 不复制 schema 或判定逻辑。
平台限定证据只能在声明的平台执行；其他平台报告为 `deferred`，不能冒充已执行。
登记 path 必须就是实际执行 symbol 的定义文件。Python/Node 是否通过由 runner 的结构化
结果/事件及来源文件判定，Go 要求 package 内唯一声明；Rust 只接受直接 `tests/*.rs`
中的无条件顶层测试，并以独立 target 精确执行。导入文件、同名 suite/测试或自行打印的
“通过”文本无效。
PR 的 Linux/Windows matrix 必须合起来覆盖全部声明平台。只有审计通过、回归随候选与
发布门禁执行，且同类问题会在下一个无关宿主暴露前被拦住，才算完成沉淀。

使用结构化输出复查：

```text
relkit-ops status --inventory <inventory> --json
./relkitw inspect --json
./relkitw doctor --resolution .relkit/resolution.json --project relkit.project.json --json
relkit verify released ...
```

失败按稳定 `code` 归因并修正式 core/ops/声明。禁止为单一宿主加仓库名特判，
禁止恢复 Python host/onboarding、复制 bootstrap 或平行门禁。
