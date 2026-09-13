---
name: relkit-ops
description: >
  产品仓 relkit 开箱、发版、升 lock、在已有 serve/agent 上注册/列产品/轮换/吊销 token。
  有 scripts/host/relkit_host.py（或 scripts/relkit_host.py）时使用。
---

# relkit 运维

由 Dec 项目 `relkit` 的 `public/local` 下发。产品仓把 `relkit` 写入家项目 `requires` 再 pull，不要手拷 `.cursor/skills`。

## 入口

只跑 `python scripts/host/relkit_host.py`（无参数只分流，不替你确认）。子命令、闸门、drift 以该脚本 `--help` / `onboard explain` / `status` 为准。

判断不了就问人。不要手拼 SSH 写配置，不要编造命令输出。

Go 的 `relkit` / `relkit-serve` / `relkit-agent` 不是人用的第二套运维 CLI。发布协议仍由它们实现；常驻进程仍要跑。产品 token 与开箱决策只经 host.py。

**箱子上的二进制**（空机 systemd、换 `relkit-agent` / `relkit-serve`）不在本 skill。到 **relkit 仓**用 `relkit-deploy` skill 与 `python scripts/deploy/relkit.py`。产品仓里若 `versionRelation=behind` 且 `onPublishRoute=true`，告诉用户先到 relkit 仓升远端，不要在本仓假装能 `upgrade --host`。

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

- 开箱前先跑 `onboard start` / `onboard inspect`：脚本会列出 `relkit.json` backends、VERSION、lock、SSH Include/通配匹配主机。`http-put` / `local` 等陈旧类型是 error，挡住 `product.id`。不要用手写确认代替 inspect。
- `ssh.host`：问人之前脚本已展开 `~/.ssh/config` 的 Include 与通配，并列出 exact / patterns / matched。通配本身不是 SSH 别名。写入 `onboard set ssh.host <值>`。
- 发布拓扑：只认 `questions --json` 的 `evidence.topology`。`mode=direct` 表示 `publishTo` 只含 S3 等直连后端，serve/agent token 与注册不在发布链路上；不要因状态里残留 `ssh.host` 就把远端说成必需。
- `sidecar.layout`：只认 lock 装到 `tools/bin/relkit-updater`；可选 `relkit.json` `sidecar.packScript` 只校验接线，不硬编码 `.mjs`。真产物归 `pack.ci`。
- `fake.release`：只跑 `relkit_host.py fake verify`。缺 staged 树时脚本自己 dummy stage + simulate，禁止手调 `relkit.exe stage`。本机无 COS/S3 发布密钥时仍应能 simulate（对着空远端 index 合并 dummy staged）。
- `pack.ci`：GitHub Actions 里 `relkit_host.py install` 之后真正 `stage`/`cas-put`/`release --execute` 的工作流算已接线；只 `install` 不够。
- `updater.process`：封闭词 `rust` / `node` / `dart` / `go` / `other`。**不是** `rust-shell`。手写 DTO / `serde(default)` 吞缺键是 drift；以 `onboard explain updater.process` 为准。
- agent 发布：`release --execute` 必须由 CI 设 `RELKIT_RELEASE_VIA_CI=1`；本地不要发。
- `share-with`：只能从 `evidence.remote.products` 的真实产品 ID 中选择；`operatorTokenPresent=true` 不等于存在可继承的产品 token。远端不可读或 `blocked` 含 `token.isolation` 时禁止让用户猜。磁盘 token 仍是**已有 owner** 的 `{owner}.token`，不打印 token 内容。
- 远端版本：`versionRelation=behind` 且 `onPublishRoute=true` 时先升级远端（relkit 仓 `relkit-deploy`）；若 `onPublishRoute=false`，明确告诉用户它落后但不阻塞当前产品发布。
- 失败记账：带 `code=` 的 `Fail` 写入 `.relkit/cache/ops-journal.jsonl`（`unclassified` 不记）。`retrospect` 输出本次遇到 / 已修进脚本或 skill / 未消化；未消化非 0。

## 完成后

运行 `python scripts/host/relkit_host.py retrospect`，再运行 `status` 检查
`ops.retrospect` 已是 `verified`；只有前一命令退出码为 0，才能宣称开箱完成。
`RETROSPECT.md` 只是入口指针，阅读它本身不构成完成闸门。
