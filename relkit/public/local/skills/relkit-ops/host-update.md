# 宿主更新接入合同

本文只讲宿主自己负责的部分：项目声明、取工具、出制品、内嵌 bootstrap、接引擎、
发布。版本选择、签名、发布事务、服务端点与密钥都在服务器与引擎里，宿主不复制。

权威依据是 [`docs/architecture.md`](../../../docs/architecture.md) 与
[ADR 0023](../../../docs/adr/0023-desktop-updater-epoch.md)。本文与它们冲突时以它们为准，
并回来修本文。

## 思路

上一代让每个宿主自己持有 resolution、`relkitw`、lock、指纹和 Candidate bundle，
再由各语言 SDK 各自解析协议。结果是同一套规则在五六个地方各实现一遍，任何一处
漂移都表现为“修了又坏”。

断代后反过来：服务器是唯一事实源，引擎 `relkit-updater` 是唯一状态机。宿主只回答
“我是谁、找哪台服务器、信哪把钥匙”，其余全部由服务器签名文档和引擎决定。所以下面
每一节都在描述宿主**不需要**做的事比需要做的事多，这是有意的。

## 1. 宿主持有什么

只有四样：服务器地址、服务器根公钥（公钥本身，OpenSSH 单行）、产品名、token。
前三样写在仓库根的 `relkit.project.json`；token 明文只在 CI 密钥里，仓库另提交一份
凭据清单 `relkit.custody.json`（schema `relkit.custody-manifest/1`），只登记 keyId 与
承载 token 的环境变量名，不含任何密钥。

`relkit.project.json`（schema `relkit.project/1`）允许的字段只有：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `product` | 是 | 产品 id |
| `server` | 是 | 服务器写面地址，例如 `http://publish.devcloud.woa.com` |
| `rootPublicKey` | 是 | 服务器根公钥，`/v1/tools/root.pub` 原样内容 |
| `channels` | 否 | 支持的渠道 |
| `selectorDimensions` | 否 | 产物选择维度 |
| `artifactProducer` | 否 | 出 ArtifactSet 的命令与 env allowlist |
| `versionProvider` | 否 | 出产品版本的命令，缺省读 `VERSION.json` |
| `site` | 否 | 人页声明，省略即不发 |

读到其他字段直接报错，没有兼容路径。`operatorRef`、`trustFingerprint`、`roles`、
`requiredContracts`、`runtimeProfileSubscriptions`、`facades`、`versionPolicy` 等
全部已删除。

宿主仓库**不提交**：`relkitw`、`relkit.lock.json`、`.relkit/` 下的 resolution 与指纹、
任何 relkit 二进制、版本号或组件摘要。

根公钥怎么拿：在面板注册产品后，用服务器的 `/v1/tools/root.pub` 内容。根公钥随
服务器实例而定，内网与外网各一把，不能混用。

## 2. 取工具

宿主与 CI 都不持有 relkit 二进制。需要工具时固定三步：

1. `GET <server>/v1/tools/current/<component>/<platform>` 与其 `/signature`；
2. 用构建机自带的 `ssh-keygen -Y verify -n relkit-tool` 对照根公钥验签；
3. 验签通过才执行。

失败立即失败，不降级到 GitHub、mirrors 或其他来源。

现役宿主用自己仓库里的 `scripts/relkit_tool.py` 实现这三步：

```text
python3 scripts/relkit_tool.py --component cli --platform <os-arch> --out-dir .relkit-tools
```

relkit 仓库里的同一三步参考实现，新宿主可以直接用，也可以照它写自己的版本：

- Windows：[`scripts/ci/relkit-tool-verify.ps1`](../../../scripts/ci/relkit-tool-verify.ps1)
- Linux / macOS：[`scripts/ci/relkit-tool-verify.sh`](../../../scripts/ci/relkit-tool-verify.sh)

参考实现的输入全是环境变量：

```text
RELKIT_SERVER            服务器地址（同 relkit.project.json 的 server）
RELKIT_ROOT_PUBLIC_KEY   根公钥行
RELKIT_TOOL              cli | updater | ops
RELKIT_PLATFORM          windows-amd64 | darwin-arm64 | linux-amd64
RELKIT_TOOL_DIR          可选，下载目录
```

成功时 stdout 只有一行：已验签工具的路径。不管用哪种实现，取工具代码里都不能出现
版本号、文件名或摘要，这样 relkit 发版才不需要改宿主的 CI。

## 3. 出制品（ArtifactSet）

```text
<cli> artifacts produce --version <v> --channel <dev|stable> --target <os-arch> --out <dir>/artifact-set.json
<cli> artifacts merge --in <A> --in <B> --out <merged>/artifact-set.json
```

`<cli>` 是上一步取回的 `cli` 工具。`produce` 调用 `artifactProducer` 声明的命令，
producer 只构建文件并返回 `relkit.artifact-set/1`，不得写签名文档、不得读 token。

每个 artifact 声明 id、相对 path、kind、platform、selectors、摘要和大小。内部更新
payload 用 `relations.payloadOf` 指向同 selector 组的 installer；payload 文件表由
`<cli> payload build --tree <dir> --out <zip>` 产出（ADR 0013）。

`merge` 要求 product、version、channel 完全一致，任一不同即失败。

## 4. Runtime Bootstrap

自带更新面的宿主（例如 SvnMergeTool）构建时生成一份 bootstrap 并内嵌进应用：

```text
<cli> bootstrap build --project relkit.project.json --out <path>
```

产出 `relkit.runtime-bootstrap/2`：内嵌这台服务器的根公钥，以及由同一把根签名的
Runtime Profile。bootstrap 本身不签名，是构建输入，不是信任对象。

宿主只把 bootstrap 原文交给引擎，不解析它。只在启动器里做更新的产品（Loom）不产出
bootstrap。

## 5. 接引擎

`relkit-updater` 是唯一引擎。宿主与它之间唯一的契约是 `api/updater/v1` 帧 IPC
（4 字节大端长度前缀 + protobuf，当前基线 4）。各语言只有薄客户端：

| 宿主 | 语言 | 薄客户端 |
| --- | --- | --- |
| SvnMergeTool | Dart | `sdk/dart/lib/src/updater_facade.dart` |
| Loom-Launcher | Rust | `sdk/rust/src/lib.rs` |
| Loom | TypeScript | `bindings/ts`（仅生成类型） |

宿主只做 open / check / download / apply 这几个调用，并把事件交给 UI。安装放置由
`InstallSpec.placement` 声明，引擎负责原子替换、整包安装程序交接与 `DeleteInstalled`。

薄客户端**不**解析 bootstrap、index、manifest，不验签、不算摘要、不比较版本、不管理
安装目录。发现客户端开始做这些，就是架构被破坏，应删除而不是补齐。

## 6. 发布

产品 CI 每个平台只做到 ArtifactSet 为止，不持 token。平台之间用普通文件传输交换
ArtifactSet 目录（不引入任何 relkit 子命令）。

汇总后由持 token 的 job 在**同一工作区**完成：

```text
<cli> stage --artifact-set <merged>/artifact-set.json
<cli> release --version <v>              # 计划，只读
<cli> release --version <v> --execute    # 需要 RELKIT_RELEASE_VIA_CI=1
```

stage 以 `artifact-set.json` 所在目录为产物根逐字节校验，所以 stage 与 release 不能拆到
两个 job。`--execute` 的 token 从凭据清单（宿主仓库的 `relkit.custody.json`）按 keyId 读取：
清单路径由 `--custody <path>` 或环境变量 `RELKIT_CUSTODY_MANIFEST` 指定，清单里只登记
keyId 与承载 token 的环境变量名，不存 token 值；清单只有一个 token 条目时自动选用。
编排里不出现 token 值。

版本号不珍贵：发错的版本用面板或 `relkit-agent unpublish` 撤下，然后发更高版本；
被撤的号不复用。

## 7. 验收

接入完成至少证明：

1. 仓库里只有 `relkit.project.json`、`relkit.custody.json`（只含 keyId 与环境变量名）、
   取工具脚本、producer 与产品代码，没有 relkit 二进制、版本号、摘要、lock、resolution
   或真实 secret；
2. CI 从服务器取到工具，换错根公钥时验签失败；
3. `artifacts produce` / `merge` 的输出可独立校验；
4. 一次 `release --execute` 后，已装旧版本的应用经引擎检查到新版本并完成安装；
5. 宿主代码里没有解析协议文档或决定版本的逻辑。
