# ADR 0017 阶段 2b cronkit 自主推进决策日志（2026-09-28）

授权背景：用户离开 12 小时，授权自主推进；非巨大变更自行决策，记录候选决策与实际决策，事后对齐。本日志承接 dec 阶段 2a 决策日志（同目录 2026-09-28-phase2a-autonomy-decision-log.md）的推进模式。

## 迁移前事实基线（全部实地核验）

- cronkit：GitHub 公网仓（shichao402/cronkit），Windows 托盘常驻工作目录编排器（Electron + Node 22 + TS）。
- 消费面：lock `relkit.consume/2` @ v0.4.19，scripts/host 32 文件（17 检入 + 15 运行时产物），CI 双 workflow（Release/Verify）入口 `python scripts/host/relkit_host.py`。
- 发布面：relkit.json 已有 agent.url（带 /v1 形态）+ release.packScript（package-release.mjs，写 `relkit.release-artifacts/1` manifest：单 install/payload + archives ci-only）；`directory.publishTo: [serve]`，agent serve 通道双 channel index 在线（dev sequence 1 / stable sequence 5，0.1.0+14 于 2026-09-25 经 serve 通道成功发布，run 36111164564）。
- 仓库 secrets：RELKIT_UPLOAD_TOKEN + RELKIT_AGENT_URL（站点根形态）均就位；vars 无上传调优项（Go stagedput 走默认 8MiB/4 并发，历史发版未配过，保持现状）。
- 签名私钥不在仓内（CLI.md 明示 privateKeyPath 是机器本地事务，gitignore 钉死 keys/）——签名发生在 agent 端 `/etc/relkit-agent/products/cronkit.json`，CI 侧无需私钥，与 dec 同构。

## 决策记录

### C1. 推进对象选择：cronkit（而非 SvnMergeTool / loom 两仓）
- 候选：cronkit（2b 批次）/ SvnMergeTool（2a 批次剩余）/ loom 两仓（2b「先盘后迁」）
- 实际：选 cronkit。理由：GitHub 公网仓（本机可完整执行 push→CI→RUP 全链）；与 dec 同构（Electron/Node + pack script + serve 通道 + 相同 secrets 形态），dec 迁移的全部经验直接复用；消费面形态最标准（/2 → /3 直升）。
- 弃选理由：SvnMergeTool 内网蓝盾 CI（git.woa.com + PAC 中央仓），本机不能直连生产环境网络，且历史卡点「ci release 仅支持单 install/payload」的解法（/2 manifest）虽已就位但双平台 drop 聚合仍需产品仓侧聚合脚本改造（见 relkit 决策日志 mem_fe723c_701444d），需用户拍板聚合位置；loom 两仓按计划明文「先盘后迁」，且 D:/workspace/Loom 工作区有用户进行中的 ADR-0050 工作（未提交），不动。
- 状态：已定（选 cronkit）

### C2-后记（2026-09-29 用户拍板，推翻原 C2）
- 用户对齐时明确表态：「我是希望完全结构化」「不希望有特例」。/1 的字符串 selectors 与 /2 并存属特例，撤销原「维持 /1」决策。
- 落地：cronkit package-release.mjs 升 `relkit.release-artifacts/2`（单 selectorGroup，selectors 为结构化 map，install/payload 显式 filename）；relkit 侧 loadReleaseArtifactsManifest 删除 /1 兼容分支（ciArtifactRefWire / parseSelectorsString / /1 归一化路径一并移除），/1 manifest 直接拒绝；测试 TestLoadReleaseArtifactsManifestSchema1Normalized 翻转为 RejectsSchema1。发布线全线唯一 /2，无 schema 分支。
- 依据：dec 侧 pack 脚本已是 /2（scripts/pack-release-artifacts.py），cronkit 升 /2 后两仓发布面同构，无特例；「完全结构化、消灭字符串解析」全线统一（2026-09-29 用户拍板记录见本文件 C2-后记）。

### C2. pack 脚本：保留 package-release.mjs 的 /1 manifest（不升级到 /2）
- 候选 A：维持 /1（Go CLI 已归一化读取）/ 候选 B：重写为 /2 结构化 selectorGroups
- 实际：选 A。理由：cronkit 是单组产物（win-x64 install + payload + ci-only archive），/1 的「单 install/payload」形状就是它的真实形状，/2 的多组能力无增益；Go CLI loadReleaseArtifactsManifest 对 /1 有显式兼容分支并归一化为单 selectorGroup；pack 脚本无 Python 依赖（.mjs 用 node 执行），runReleasePackScript 原生支持。少做改动 = 少做信息推测 = 迁移面最小。
- 状态：已定（选 A）

### C3. CI 入口替换范围
- 实际：release.yml（install 入口 + publish 尾段 `ci release --execute` + secrets 注入 RELKIT_AGENT_URL）与 verify.yml（install 入口）两处全换 Go CLI；RELKIT_RELEASE_VIA_CI=1 + token 缺失时红（沿 dec 语义：渠道 tag 已推出，产物必须进 RUP）；Go 1.26.3 已在原 workflow 就位（Windows runner 上 setup-go 直装）。
- 注：dec 迁移时补回的 RELKIT_UPLOAD_PART_SIZE/CONCURRENCY vars 透传在 cronkit 不适用——cronkit 仓库从未配置过这两个 vars（dec 是 2026-09-22 为跨境 COS 专门调的），历史发版全走默认值成功，保持现状。
- 状态：已定

### C4. 文档同步范围
- 实际：package.json ensure-relkit script、ensure-relkit-bindings/package-versioned/relkit-smoke 三处报错提示、README 安装指引、ADR 0012 历史注记（不改写历史事实，加「2026-09-28 起 Python 消费面退役」现状注记）。工作区既有的 .cursor/skills 删除与 .dec/config.yaml 修改属用户本地 dec 工具变动，不掺入迁移提交。
- 状态：已定

### C5. 发布演练版本与验收门
- 实际：dev/v0.1.0+15（0.1.0+14 已于 09-25 发过 dev，沿用 +N 递增惯例）；验收门 = Release run 全绿 + RUP publish ok + serve index 更新。非 fake verify（CLI 无 fake 子命令，dry-run 已含五链）。
- 状态：已定，run 36441505235 进行中

## 终态与产物链（待 run 绿后补全）

### cronkit 仓（main）
| commit | 内容 |
|---|---|
| b3b5651 | 阶段 2b 主迁移：lock hostless consume/3 @ v0.5.7、CI 双入口全换 Go CLI、scripts/host 17 文件删除、五处文档/脚本同步（26 文件，+76/-7961） |
| 050765a | chore(release): prepare 0.1.0+15 for dev channel |

### RUP 侧
- `cronkit/0.1.0+15` dev 渠道发布成功：publish 响应 `published 0.1.0+15 (code 15) on channel dev, sequence 3`；`cas uploaded=2 skipped=0`，staged tar sha256=383df78b… bytes=920；serve directory sequence 13；dev index 329 → 505 字节（0.1.0+15 条目入索引）。
- 产物编组（与旧 Python 流程语义一致）：`artifact/cronkit/0.1.0+15/`（cronkit-0.1.0+15-win-x64-setup.exe + payload.zip）+ `manifest/cronkit/0.1.0+15.pb` + `index/cronkit/dev.pb` + `latest/cronkit/dev.json` + `site/cronkit.json` + `directory/cronkit.pb`。publish 响应中的「(cas hit, skipped upload)」为 agent serve 后端对本次 cas-put 已入库产物的摄取视图。
- 端到端客户端验证（真实线上 serve 平面）：`relkit smoke PASSED`——本地仅服务 directory 层，index/manifest/产物全走 update-internal.firoyang.com：fresh install → updateAvailable（code 15、artifact selected）→ download（size + sha256 验签通过、bytes>0）→ upToDate / 错 selectors / 错 key / 错 product / 不可达入口 / 节流全部符合预期。
- 演练顺带修掉 smoke 脚本两处历史债（`10e7988`）：download 步骤 oneof 赋值须用 `create(DownloadOpSchema, {planId})`（c4c4126 起潜伏，download 步骤首次被真正跑到）；Range 断言仅在 publishDir 承载完整产物树时成立，directory-only 形态降为信息性输出。

### 验收对照（migration plan 阶段 2b cronkit 行要求）
- lock 重写 consume/3：✅（hostless 形态，无 hostScriptsSha256，updater 走 module 通道 install 到 tools/bin）
- CI 入口替换：✅（release.yml + verify.yml）
- 删 scripts/host：✅（git rm 17 检入文件）
- 发布演练验收门：进行中（dev/v0.1.0+15，run 36441505235）

## 进度清单（终）
- [x] 前提核验：lock/CI/secrets/签名链路/pack 兼容性全链事实确认
- [x] lock 升级 v0.4.19 → v0.5.7 hostless consume/3（upgrade --no-host-scripts）
- [x] CI 双 workflow 入口替换 + publish 尾段（ci release --execute）
- [x] scripts/host 全套删除 + 五处文档/脚本同步
- [x] 本地验证：install/check/status 全绿 + ci release dry-run 五链全绿 + npm 三连（check-version/tsc/test 78 用例）
- [x] 提交 push（b3b5651 迁移主提交 + 050765a 版本准备 + 10e7988 smoke 修复）
- [x] 发布演练 tag dev/v0.1.0+15 推送
- [x] Release run 36441505235 全绿（约 7 分钟，与历史 run 同量级）
- [x] RUP publish ok：dev sequence 3、directory sequence 13、dev index 更新
- [x] 端到端 smoke：真实 serve 平面 check/download 全链 PASSED
- [x] 决策日志终稿（本文件）
- [ ] migration plan cronkit 行终态更新（relkit 仓，待提交）

## 2026-09-29 追记：/2 统一演练（dev/v0.1.0+16）

- 触发：用户拍板推翻 C2（「完全结构化、不希望有特例」），cronkit pack 升 /2 + relkit 侧 /1 兼容分支删除（详见 C2-后记）。
- 提交：cronkit `d8f53ab`（pack /2）+ `30b20b4`（版本准备）；relkit `eec6ab7`（/1 分支删除）+ `be21cf1`（v0.5.8）。relkit Release run 36508446924 全绿（5m3s）。
- 首轮 Release/Verify 双红：只改 VERSION.json 漏跑 `sync-version.mjs`，`check-version` 门禁拦下（Release 36508465811 / Verify 36508458151）——已提 issue shichao402/cronkit#1（bump 后需人工记得同步的结构性问题，建议 A bump 自动带同步 + B pre-commit 兜底 + C dist 清理旧 manifest）。
- 修复：`5af3cb4` 补 version.ts 同步，tag 重指后 Release run 36508948859 全绿（6m34s）、Verify run 36508942245 全绿（5m34s）。
- RUP 侧：publish `published 0.1.0+16 (code 16) on channel dev, sequence 4`；serve directory sequence 14；**serve 平面出现显式 filename 产物 `cronkit-0.1.0+16-win-x64-payload.zip`（/2 生效的直接证据，对比 +14 的 `…-relkit-payload.zip` 旧默认名）**；dev index 更新（+14 → +16 条目）；cas-put sha256 与 lock 校验链不变。
- 端到端 smoke：先按 09-28 同款（stable 渠道）PASSED；另用临时 dev-渠道 smoke（脚本按 8 组 13 项原样改渠道，未入库）验证 0.1.0+16 /2 产物 check/download 全链 PASSED。
- 遗留：cronkit `.dec/config.yaml` 的 `requires: relkit: latest` 与 `.cursor/skills/dec-relkit-*` 删除为用户本地 dec 工具变动，照旧未掺入本批提交。

## 2026-09-29 追记：cronkit#1 修复（version.syncScript + 旧 manifest 清理）

- 落地：relkit v0.5.9 新增可选 `relkit.json` `version.syncScript`（分派规则同 `release.packScript`），`relkit version set|bump` 写完 VERSION.json 自动执行；cronkit 侧声明 `scripts/sync-version.mjs`，另 `package-release.mjs` 开头清旧 `dist/release-artifacts.json`（防 staging 静默复用旧对应关系）。全仓 relkit 钉 v0.5.7→v0.5.9（7 文件）。
- 验证：relkit v0.5.9 Release run 36510268146 全绿；cronkit `dev/v0.1.0+17`（commit 4948cfd，含 d042d9d 用户 smoke 两模式提交）Release run 36510978539 全绿，bump→sync 一步完成（无人工同步）；lock `upgrade v0.5.9` 后 directory-only smoke stable/dev 双渠道 15/15 PASS（真 serve 平面）。
- issue shichao402/cronkit#1 已关闭（方案 A+C，B pre-commit 未做：bump 已自动化，忘跑环节不存在了）。
- 教训：smoke 端口参数误传 0 会导致 sidecar 连不上本地 directory（`no usable directory/index source`），报错形态与签名失败相同——排查先看端口参数。
