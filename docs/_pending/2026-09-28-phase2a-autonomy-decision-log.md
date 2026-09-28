# ADR 0017 阶段 2a 自主推进决策日志（2026-09-28）

授权背景：用户离开 12 小时，授权自主推进；非巨大变更自行决策，记录候选决策与实际决策，事后对齐。

## 决策记录

### D1. relkit v0.5.6 Release 就绪确认
- 候选：等流水线 vs 复查后直接推进
- 实际：`gh run watch 36431895128` 确认 success（test windows/ubuntu + release 全绿，Publish Dec provides / Publish GitHub Release 均通过）。直接进入 dec upgrade。
- 状态：已完成

### D2. dec lock upgrade 到 v0.5.6 hostless consume/3
- 候选：用 v0.5.5（check 修复前）vs v0.5.6（含 module-channel updater check）
- 实际：统一用 v0.5.6。upgrade 成功：lock 重写为 hostless consume/3（无 hostScriptsSha256、artifacts 无 host-scripts 行、updater 走 module channel install 到 tools/bin）。
- 状态：已完成

### D3. RELKIT_UPLOAD_PART_SIZE / RELKIT_UPLOAD_CONCURRENCY vars 透传
- 背景：旧 bash publish 尾段透传了这两个 vars；新 `ci release --execute` 尾段 env 块未保留。Go casput 实现读同名 env，不透传则用内置默认值。
- 核实结果：仓库 vars 实际配置为 `RELKIT_UPLOAD_PART_SIZE=1MiB`、`RELKIT_UPLOAD_CONCURRENCY=1`（2026-09-22 配置，为避开 COS UserNetworkTooSlow 专门调的跨境细管道值；旧尾段注释明确「跨境细管道未设置时仍保持 1」）。Go stagedput 默认 8MiB/4 并发。
- 候选 A：不补（用 Go 默认值）vs 候选 B：补回透传（保持上传行为一致）
- 实际：选 B。已在 release.yml publish 尾段 env 块补回两行透传，YAML 校验通过。
- 状态：已完成（选 B）


### D4. dec 提交策略
- 候选 A：一个大 commit vs 候选 B：拆分（host 删除 / CI 替换 / 文档同步 分开）
- 实际：选 A。单原子提交 `3b881ff`（31 文件，+268/-8599），避免中间态不可构建；后续修复各自独立成 commit（9d27720 / 7a6b473 / 09fd298）。
- 状态：已完成（选 A）

### D5. 发布演练 tag 命名
- 候选：`dev/v*` vs 直接用正式 `v*`
- 实际：`dev/v1.13.104`（沿用 dev/v1.13.99 → stable/v1.13.100 → … 103 版本链惯例，version.json 先行 bump 推 main）。三次迭代：run #1（36432968606）test job 测试债失败；run #2（36434753443）publish 405；run #3（36437241383）全绿。
- 状态：已完成 ✅

### D6. relkit 仓 migration plan 文档阶段 2a 状态更新
- 候选：现在改 vs 演练通过后改
- 实际：演练通过后改。relkit 提交 `519e723`：阶段 2a dec 行标记完成，记录三次迭代、两处修复与回退线依赖 git 历史的决策。
- 状态：已完成 ✅

## 终态与产物链

### relkit 仓（master，全部已推送）
| commit | 内容 |
|---|---|
| 7cdfde0 | v0.5.4：hostless consume/3 闸门（UpgradeLock omitHostScripts / LockDrift 放行 / upgrade --no-host-scripts + conformance） |
| 4e10a18 | v0.5.4 bump |
| 53a2883 | v0.5.5：/2 manifest install-only 组（console installer / manifest blob 无 payload） |
| 459396a | v0.5.5 bump |
| c8b589c | v0.5.6：check 对 module-channel updater 校验（checkModuleChannelUpdater 探针） |
| 4cb5c4a | v0.5.6 bump |
| f813fb3 | v0.5.7：PublishViaAgent agent URL 归一修复（405 根因，含 TestPublishEndpointNormalization） |
| b2b683d | v0.5.7 bump（tag v0.5.7，流水线 success） |
| 519e723 | migration plan 阶段 2a dec 行完成 |

### dec 仓（main，全部已推送）
| commit | 内容 |
|---|---|
| 3b881ff | 阶段 2a 主迁移：scripts/host 19 文件删除、CI 五入口 + publish 尾段 → relkit CLI、pack-release-artifacts.py、relkit.json release 块、七处文档同步、决策日志（+268/-8599） |
| 9d27720 | version.json → v1.13.104 |
| 7a6b473 | 测试债修复：embed SSOT 再生、sysproc 两处、trimProductRoot 跨平台归一 |
| 09fd298 | 入口/lock v0.5.6 → v0.5.7 |

### RUP 侧
- `dec/1.13.104` dev 渠道发布成功：publish 响应 `ok:true, product dec, sequence 16, version 1.13.104`；serve directory sequence 42。
- 产物编组 17 组（run #2/#3 一致）：runtime dec/exec/host-setup/server 全平台 ×（binary + payload zip）+ runtime-manifest + windows console installer。与旧 bash 版编组语义一致（结构化 /2 替代字符串解析）。
- CAS 内容寻址行为（run #3 实测）：`cas uploaded=32 skipped=0`——run #3 构建自 09fd298，与 run #2 的 7a6b473 构建产物字节不一致（staged sha 6acf0adf… bytes=3413 ≠ run #2 的 14007b0b… bytes=3414），故 32 blob 全部重新上传；publish 响应 JSON 中的「(cas hit, skipped upload)」是 agent serve 后端摄取阶段对本次 cas-put 已入库产物的视图，并非跨 run 去重。跨 run CAS 去重仅在重跑同一 sha 产物时才会命中。

### 验收对照（migration plan 阶段 2a dec 行要求）
- CI 入口替换：✅（test/runtime/console×2/publish + upload-bench）
- lock 重写 consume/3：✅（hostless 形态，无 hostScriptsSha256）
- 删 relkit_host.py 调用与 scripts/host：✅（19 文件 git rm）
- 发布演练验收门：✅（dev/v1.13.104 端到端，非 fake verify——CLI 无 fake 子命令，dry-run + 真实发布覆盖）
- 双跑节奏偏差说明：原计划「bash 尾段作 spec 标本并存至 Go 路径证明」，实际执行为直接替换（用户授权直接迁移），三次 run 迭代等价完成了「Go 路径完整跑通真实发布」的验证；回退线依赖 git 历史（3b881ff^ 可完整恢复 bash 尾段）。

## 进度清单（终）
- [x] relkit v0.5.4：hostless consume/3 闸门
- [x] relkit v0.5.5：/2 manifest install-only 组
- [x] relkit v0.5.6：check module-channel updater
- [x] dec：relkit.json release 块 + pack-release-artifacts.py
- [x] dec：CI 入口 + publish 尾段替换（最终 v0.5.7）
- [x] dec：scripts/host 全套删除
- [x] dec：七处文档/技能同步
- [x] relkit v0.5.6 流水线 success
- [x] dec lock upgrade → hostless consume/3（最终 v0.5.7）
- [x] dec 本地验证：install/check/status/ci release dry-run 全绿
- [x] D3：upload vars 透传（补回）
- [x] dec 提交 push（4 commits）
- [x] D7：演练 run #1 测试债修复
- [x] D8：publish URL 归一修复 → relkit v0.5.7
- [x] dec 入口/lock 升 v0.5.7
- [x] 发布演练 dev/v1.13.104：run #3 全绿，RUP publish ok
- [x] relkit migration plan 阶段 2a 状态更新


### D7. 演练 run #1 test job 四个失败的处理
- 背景：dev/v1.13.104 第一次 run 在 test job 失败，四个独立失败点：(1) internal/update embed relkit.json 与根 relkit.json 不一致（SSOT 测试，我们改根文件后未重新生成 embed）；(2) secrets passphrase 测试用了 Windows 专属 `syscall.SysProcAttr{HideWindow}` 字段（Linux 编译失败）；(3) 两处测试直接 `exec.Command` 违反 sysproc guard；(4) trimProductRoot 用 `filepath.ToSlash` 归一反斜杠（Linux 上 no-op），测试注释明确要求 Linux CI 同样命中，实现与意图不符。后三项均为 ADR 0036 凭据系列（dev/v1.13.103 后推入 main）自带测试债，首次过 CI 暴露。
- 候选 A：绕过（跳过测试/改 workflow）vs 候选 B：修复（embed 重新生成 + sysproc 替换 + 无条件反斜杠归一）
- 实际：选 B。四处修复提交 `7a6b473`，本机全量测试除既有 Windows 本地 serviceapi 债（issue #25 范畴，CI Linux 上通过）外全绿。run #2 的 test job 通过验证了修复有效。
- 状态：已完成（选 B）

### D8. 演练 run #2 publish step HTTP 405 的修复位置
- 背景：run #2 全链路走通（staging 17 组 → 32 个 cas blob 上传 + staged tar 上传全部成功），最后 `POST /publish` 返回 HTTP 405。根因：旧 bash 尾段把 `RELKIT_AGENT_URL` 归一到 `/v1` 结尾（casput.normalizeBase 也这么做），dec secret 配的是站点根形态；Go 的 `PublishViaAgent` 直接拼 `url + "/publish"`，POST 到了错误路径。
- 候选 A：dec 侧把 secret 改成带 /v1 的形态（掩盖 relkit 缺口，其他产品仓会再踩）vs 候选 B：修 relkit `PublishViaAgent` 补 URL 归一（泛化修复，对齐 casput.normalizeBase 与旧 bash 行为）
- 实际：选 B。relkit 提交 `f813fb3`（含 TestPublishEndpointNormalization 回归测试，9 个 URL 形态全钉），发 v0.5.7（流水线 success）。dec 入口/lock 升 v0.5.7（提交 `09fd298`），第三次演练进行中。
- 附注：run #2 的 staged 树已上传（cas 侧 32 blob + staged tar），但 publish 未达 → RUP 上无 1.13.104 manifest。run #3 实测：`cas uploaded=32 skipped=0`（run #3 构建自 09fd298，产物字节与 run #2 的 7a6b473 构建不一致，staged sha 6acf0adf… ≠ run #2 的 14007b0b…，故全部重新上传，无跨 run 去重命中）。publish 响应中的「(cas hit, skipped upload)」是 agent serve 后端对本次 cas-put 已入库产物的摄取视图，不是跨 run 去重。
- 状态：已完成（选 B），第三次演练全绿验证（run 36437241383）


## 进度清单
- 见上方「进度清单（终）」：全部完成，无遗留项。本段为初次写入时的工作清单，已完成历史使命，保留以记录当次推进视角。
