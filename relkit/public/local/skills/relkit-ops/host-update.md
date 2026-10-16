# 宿主更新接入合同（Relkit vNext）

本文只说明宿主负责的声明、制品和运行时接线。版本选择、签名数据、发布事务、
endpoint 与密钥由 Relkit core / Operator 负责，宿主不得复制实现。

## 1. 项目声明

仓库根的 `relkit.project.json` 声明：

- product、channels、目标 `os-arch` 与 selector dimensions；
- 角色：publisher、updater-host、subscriber、updater-projection；
- `in-place` 或 `library` 版本策略；
- facade / projection 与所需协议合同；
- artifact producer 命令；
- Runtime Profile 发布或订阅的 product ref + root fingerprint。

不得声明 Relkit 版本、CLI/SDK URL、服务 endpoint、Token、SSH、机器目录、mirror
凭据或“已完成步骤”。这些来自已签名 Operator Profile 和 immutable resolution。

接入后只提交项目声明、生成的 `relkitw` / `relkitw.cmd`、producer 与产品代码。

## 2. Runtime Bootstrap

updater-host 或 subscriber 不手写 `ClientProfile` 中的入口 URL、公钥、渠道与恢复链接。
Resolver 验证签名 Runtime Profile 后，为 producer 提供
`relkit.runtime-bootstrap/1`：

- `resolutionDigest` 必须等于本次 resolution；
- profile product 必须等于订阅 product；
- trusted keys 必须非空且匹配项目钉定的 root fingerprint。

producer 把 verified bootstrap 注入应用；应用只通过各语言 canonical helper
`runtime_bootstrap_from_json`（或等价正式 API）构造 profile。缺失、摘要不符、产品不符
或签名不可信时构建失败，不允许回退到仓内常量或环境变量拼 URL。

首次自订阅产品尚无在线 Profile 时，由 Operator 用产品私钥创建并纳入 inventory；
私钥只在 Operator 私密配置中。

## 3. ArtifactSet producer

Core 向 producer 的 stdin 传 `relkit.artifact-producer-input/1`，至少包含：

- product、version、channel、target；
- immutable resolution digest；
- 独占 output directory；
- 已验证的 Runtime Bootstrap 集合。

producer 只构建文件并在 stdout 返回一个 `relkit.artifact-set/1`。它不得：

- 写 staged/index/directory、签名或 backend；
- 读取发布 endpoint、Token 或私钥；
- 再解析 latest 或下载另一套 Relkit；
- 修改 project、resolution 或 resolution bundle。

每个 artifact 声明 id、相对 path、kind、platform、selectors、摘要和大小。
内部更新 payload 通过 `relations.payloadOf` 指向同 selector 组的 installer；某平台只有
installer 合法，不为凑对称伪造 payload。

多平台 Job 各自产一个 ArtifactSet，汇总只执行 `relkit artifacts merge`。merge
要求 product、version、channel 与 resolution digest 完全一致，并校验文件摘要、
selector 维度与 payload 关系。

## 4. updater 运行时

- 唯一状态机是 resolution 物化的 `relkit-updater`。
- 宿主只调用正式 facade 的 open/check/download/apply/status。
- UI 只解析同一 distribution 物化的 projection。
- 禁止手写 CheckResult、InstallSpec、事件 DTO、payload 解压或第二套 apply。
- facade 与 sidecar 的 Updater IPC 窗口由 distribution 合同验证，不由宿主填版本。

版本策略：

- `in-place`：在 install root 内原子应用；是否需退出以 plan 的
  `requires_host_exit` 为准。
- `library`：写入 versions 目录并切换 active 指针；宿主 launcher 负责按项目 pin
  选择版本。retain、rollback、switch 必须在项目声明允许后才可达。

`preserve` 只保护用户数据；删除集合由前一基线、当前文件表和 preserve 计算。
payload 输入树不得含软链。macOS 完整 `.app` 的多版本切换必须经 LaunchServices，
不能直接 exec `Contents/MacOS`。

## 5. 产品 CI 与 Operator

产品 CI：

```text
安装签名 offline bundle / 使用 provisioned bootstrap
relkitw doctor
relkitw artifacts produce（每 target）
relkitw artifacts merge（汇总）
relkit-ops artifact-set push（上传 merged ArtifactSet，记录 URL + archiveSha256）
```

Candidate bundle 的安装命令必须显式传入 Operator 分发的
`--candidate-root-fingerprint`；缺少或不匹配即失败。该授权只允许 resolution
中的非 stable 渠道，不得用产品仓变量覆盖 bundle 内的身份、Operator 或 Catalog。

独立 Operator Job：

```text
relkit-ops artifact-set pull（URL + archiveSha256）
relkitw stage --artifact-set <artifact-set.json>
relkitw release --version <version>
relkitw release --version <version> --execute
relkitw verify released \
  --resolution <同一 immutable-resolution.json> \
  --version <version> --channel <channel> \
  [--consumer <loopback-url>] [--directory <loopback-url>]
```

产品 Job 只持 ArtifactSet 传输凭据，不持发布 endpoint/agent token；只有 Operator
Job 持 `RELKIT_UPLOAD_TOKEN`。Operator 不改产品业务选择。整条事务必须使用同一
resolution digest。candidate 使用独立身份与权限，不能发布 stable。

正式验收从 bundle 安装后的 Runtime Bootstrap 发现 Product RUP Root；显式
`--product-root` 只用于诊断，不能替代 bootstrap → wrapper → released-state 链路。
`release --execute` 后若立即读回失败，先按同一 resolution 有界重试 verify；已提交则
不得重发同一 immutable version，部分提交按 `partial-commit` / fix-forward 处理。

完整 Candidate → Production、Agent policy、stable 与回滚顺序以
[`cutover.md`](../../../docs/design/vnext/cutover.md) 为唯一 runbook。

## 6. 验收

接入完成至少证明：

1. `inspect --json` 能解释 Operator、distribution、resolution 与来源；
2. `doctor` 验证 project、bundle、Profile 与 anti-rollback watermark；
3. 缺 Runtime Profile、组件或 selector 时以稳定诊断码失败；
4. producer 输出可独立校验，跨平台 merge 后摘要仍正确；
5. facade conformance 与所属代表性场景（S1/S2/S3）通过；
6. 仓内没有 consume lock、Python host/onboarding、复制 wrapper、真实 secret，
   也没有产品名特判进入 Relkit core。

只组合既有能力的新宿主不得修改 Relkit core；真·新能力必须同时增加合同、独立测试、
组合测试和至少一个端到端场景。
