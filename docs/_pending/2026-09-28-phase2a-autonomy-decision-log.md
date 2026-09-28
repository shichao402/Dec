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
- 实际：待执行时定（倾向 A：这是一个原子迁移，拆开反而造成中间态不可构建）
- 状态：待决策

### D5. 发布演练 tag 命名
- 候选：`dev/v*` vs 直接用正式 `v*`
- 实际：按迁移计划用 `dev/v*`（RUP 渠道 tag，不污染正式 GitHub Release 链）
- 状态：待执行

### D6. relkit 仓 migration plan 文档阶段 2a 状态更新
- 候选：现在改 vs 演练通过后改
- 实际：演练通过后一并改（状态一次到位）
- 状态：待执行

## 进度清单
- [x] relkit v0.5.4：hostless consume/3 闸门
- [x] relkit v0.5.5：/2 manifest install-only 组
- [x] relkit v0.5.6：check module-channel updater
- [x] dec：relkit.json release 块 + pack-release-artifacts.py
- [x] dec：CI 入口 + publish 尾段替换（v0.5.6）
- [x] dec：scripts/host 全套删除（git rm）
- [x] dec：七处文档/技能同步
- [x] relkit v0.5.6 流水线 success
- [x] dec lock upgrade v0.5.6 → hostless consume/3
- [x] dec 本地验证：install/check/status/ci release dry-run 全绿（fake 为独立子命令不存在；dry-run 已含 install → pack → stage → simulate → fake 五链）
- [x] D3：upload vars 透传决策（补回，选 B）
- [ ] dec 提交 push
- [ ] 发布演练 dev/v* tag
- [ ] relkit migration plan 阶段 2a 状态更新
