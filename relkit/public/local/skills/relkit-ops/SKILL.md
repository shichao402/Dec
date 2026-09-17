---
name: relkit-ops
description: >
  产品仓 relkit 开箱、发版、升 lock、在已有 serve/agent 上注册/列产品/轮换/吊销 token；
  以及两轨发布（--install / --payload）、InstallSpec.placement、内部更新接入。
  有 scripts/host/relkit_host.py（或 scripts/relkit_host.py）时使用。
---

# relkit 运维

## 入口

只跑 `python scripts/host/relkit_host.py`（无参数只分流，不替你确认）。该文件是 parser/dispatch 入口，具体实现位于同一 release 固定的 `scripts/host/hostlib/`；不要绕过入口直接调用内部模块。子命令、闸门、drift 以该脚本 `--help` / `onboard explain` / `status` 为准。

判断不了就问人。不要手拼 SSH 写配置，不要编造命令输出。

Go 的 `relkit` / `relkit-serve` / `relkit-agent` 不是人用的第二套运维 CLI。发布协议仍由它们实现；常驻进程仍要跑。产品 token 与开箱决策只经 host.py。

**两轨 / Placement / 内部更新**的产品接入合同在同目录 [`host-update.md`](host-update.md)。改发布脚本、填 `InstallSpec`、决定某平台开不开 payload 时先读它；不要去 relkit 仓 docs 里另找一份平行指南。协议实现细节才回 relkit ADR 0013 / 0014。

**箱子上的二进制**（空机 systemd、换 `relkit-agent` / `relkit-serve`）不在本 skill。那是 relkit 仓的 [`relkit-deploy`](../relkit-deploy/SKILL.md) 与 `python scripts/deploy/relkit.py`。现网箱 **agent + serve 固定配套**，缺 serve 不算升完。产品仓里若 `versionRelation=behind` 且 `onPublishRoute=true`，告诉用户先到 relkit 仓升远端，不要在本仓假装能 `upgrade --host`。

## 状态与缓存

- 决策记录：**提交** `.relkit/onboarding.json`。瞬时文件只在 `.relkit/cache/`（`onboarding.md`、`onboarding.local.json`、staged 树）。
- `ensure_gitignore` 保证 `.relkit/cache/`、`.relkit-keys/*.private.pb` 与 `__pycache__/`。不要把产品状态写进 skill cache / `docs/`。重置：`onboard reset --yes`。

## 批量决策优先

1. 先运行 `onboard start` / `onboard inspect` 完成现状检查，再单独向用户确认本次意图：`fresh`（新接入）、`reconfigure`（重新开箱）或 `upgrade`（沿用已确认决策升级）。现状与意图冲突时先解释冲突，不猜。
2. 运行 `onboard questions --intent <intent> --json`。先向用户展示其中的 `evidence.topology`、`evidence.remote`、`implications` 与 `blocked`，再提交这一波 `questions`。禁止脱离这些现场证据自行概括发布路径或 token 现状。
3. 将整批回答按输出的 `answerShape` 写入 `.relkit/cache/`，运行 `onboard apply --answers <file>`。它按 `revision` 检查现状是否变化，并原子校验全部答案；任何冲突都不会写入部分状态。
4. 批次按依赖分波次，不强求一次问完。`blocked` 中的决策本轮禁止询问；先完成它指出的前置项（例如确认 SSH Host），重新生成批次，拿到 live inventory 后再问 token。若脚本返回冲突，只重问报错项；若 revision 过期，重新生成问题批次并只问变化项。
5. 决策落盘后再执行 action steps。可以并行委派互不写同一文件、互不改同一远端状态的调查或实现；共享产品状态、同一配置文件、serve/agent 注册与 release 必须按依赖顺序经 `relkit_host.py` 执行和复核。

逐项 `onboard set` 只作为修改单个已知答案的兼容入口，不是默认开箱体验。脚本保持非交互；由 agent 使用结构化提问收集用户批量答案。

## 闸门短指针

- 开箱前先跑 `onboard start` / `onboard inspect`：脚本会列出 `relkit.json` backends、VERSION、lock、SSH Include/通配匹配主机。`http-put` / `local` / `static-http` 等陈旧类型是 error，挡住 `product.id`。不要用手写确认代替 inspect。
- `ssh.host`：问人之前脚本已展开 `~/.ssh/config` 的 Include 与通配，并列出 exact / patterns / matched。通配本身不是 SSH 别名。写入 `onboard set ssh.host <值>`。
- 发布拓扑：只认 `questions --json` 的 `evidence.topology`。`mode=direct` 表示 `publishTo` 只含 S3 等直连后端，serve/agent token 与注册不在发布链路上；不要因状态里残留 `ssh.host` 就把远端说成必需。
- `sidecar.layout`：只认 lock 装到 `tools/bin/relkit-updater`；可选 `relkit.json` `sidecar.packScript` 只校验接线，不硬编码 `.mjs`。真产物归 `pack.ci`。macOS 的 universal sidecar 只跑 `relkit_host.py sidecar universal --out <路径>`：`install` 每个目标都装成同一个文件名，只能放构建机自己的架构。**这条闸门说的是 sidecar 二进制打进发布树的位置，与客户端 `InstallSpec.placement` 无关**，同名不同事。
- 宿主接入：两轨、`InstallSpec.placement`、IPC `[3,3]`、payload 软链限制、闸门与禁止项见同目录 [`host-update.md`](host-update.md)。SPEC 附录 B 只分类。
- `relkit_consume.py` 是 release 内部件，随版本在 `scripts/host/` 里搬家。产品代码禁止 `import relkit_consume`，也禁止再写 `scripts/relkit_consume.py` 这个已退役路径；闸门 `consumer-entry-only` 会报，改法是走 `relkit_host.py` 的子命令。
- host 脚本要求 Python ≥ 3.9（hostlib 导入期就会求值 PEP 585 泛型）。产品入口脚本不要接受或安装 3.8，否则闸门 `host-python-floor` 报 drift；CI 容器只装 3.8 时要改成装 3.9 以上。
- `fake.release`：只跑 `relkit_host.py fake verify`。缺 staged 树时脚本自己 dummy stage + simulate，禁止手调 `relkit.exe stage`。本机无 COS/S3 发布密钥时仍应能 simulate（对着空远端 index 合并 dummy staged）。
- `pack.ci`：GitHub Actions 里 `relkit_host.py install` 之后真正 `stage`/`cas-put`/`release --execute` 的工作流算已接线；只 `install` 不够。
- `updater.process`：封闭词由组件 registry 派生（当前 `rust` / `node` / `dart` / `go` / `other`），**不是** `rust-shell`。手写 DTO / `serde(default)` 吞缺键是 drift。产品源码出现 `RupUpdater` 或 `sdk.Updater` 也是 drift：升级宿主只许走 facade + sidecar。选择 `other` 时，`relkit.json` 必须声明存在的 `updater.entry`；存在 WebView 还必须声明 `updater.projection`。sidecar 名只能出现在声明入口（及 `sidecar.packScript`），projection 仍按满强度形状检测。
- `updater.urlAllowlist` 是路径列表，只豁免这些路径中的 updater endpoint/base URL 文本；不豁免 sidecar 名、手写 `CheckResult` / `UpdateAvailable`、自声明 proto 或宽松反序列化。
- agent 发布：`release --execute` 必须由 CI 设 `RELKIT_RELEASE_VIA_CI=1`；本地不要发。
- 人页：产品 `relkit.json` 只写 `site.title/description/homepage`，禁止 `site.makers`。Makers projectId/region/tokenEnv 属于箱上 `relkit-agent.json` 顶层；产品发布只更新 `site/`、`latest/` 数据，agent 异步全量静态重建。人页落后时到 relkit 仓按 `relkit-deploy` 跑 `relkit-agent site-rebuild`，禁止靠重发产品版本救页。
- `share-with`：只能从 `evidence.remote.products` 的真实产品 ID 中选择；`operatorTokenPresent=true` 不等于存在可继承的产品 token。远端不可读或 `blocked` 含 `token.isolation` 时禁止让用户猜。磁盘 token 仍是**已有 owner** 的 `{owner}.token`，不打印 token 内容。
- 远端版本：`versionRelation=behind` 且 `onPublishRoute=true` 时先升级远端（relkit 仓 `relkit-deploy`）；若 `onPublishRoute=false`，明确告诉用户它落后但不阻塞当前产品发布。
- 失败记账：带 `code=` 的 `Fail` 写入 `.relkit/cache/ops-journal.jsonl`（`unclassified` 不记）。`retrospect` 输出本次遇到 / 已修进脚本或 skill / 未消化；未消化非 0。

## 完成后：先复盘会话，再跑机械闸门

`retrospect` 只能看文件与带错误码的命令失败；它看不到用户纠正、人工验收结果，
也看不到「命令成功但交付结果不对」。因此 **命令退出码为 0 不能替代会话复盘**。

先从本次运维意图开始重读对话与操作记录，逐项找出：

- 用户对目标、范围或前提的纠正；
- 人工验收与原先判断相反的结果；
- 绕过正式入口的临时处理；
- 当时显示成功、后来才证明不完整的步骤。

每项必须先分类，再决定落点：

1. **relkit 通用流程缺口**：换成任意无关产品、严格照本 Skill 操作仍会踩中。
   必须指出最早可拦截的 relkit 阶段（inspect / questions / apply / upgrade /
   install / fake verify / verify / release / retrospect），以及该阶段能读取的输入、
   应做的机械判断和失败结果。修复应落在 relkit 的脚本、Skill、测试或发布产物；
   不得拿当前产品的专用测试或配置当通用流程优化。
2. **产品自身缺口**：只在该产品的宿主、权限、UI 或打包结构中成立。留在产品仓，
   不冒充 relkit 运维优化。
3. **Agent 理解偏差**：例如用户问流程，却回答具体项目。先用一句话复述当前范围，
   再给建议；若这种误解可重复发生，就修本 Skill 的提示，不给产品加门阀。

复盘输出必须写清「现象 / 原流程为何没拦 / 最早拦截阶段 / 可机械化改动」。
只重复「应该有更新说明」「应该测试权限」这类已知目标，不算流程优化。
发现新的通用缺口但本次不能落地时，必须明确列为未消化，不能宣称运维流程完成。

会话复盘完成后，运行 `python scripts/host/relkit_host.py retrospect`，再运行
`status` 检查 `ops.retrospect` 已是 `verified`。若之后用户验收又暴露新问题，
前一次 verified 已过期，必须重新复盘并重跑。只有会话复盘没有未消化项、且命令
退出码为 0，才能宣称完成。`RETROSPECT.md` 只是入口指针，阅读它本身不构成完成闸门。
