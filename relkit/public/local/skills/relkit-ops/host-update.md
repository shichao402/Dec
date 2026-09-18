# 宿主接入合同（语言无关）

产品仓接入 relkit 更新时以本文为准。协议细节（payload 结构、基线差分、Placement 引擎行为）在 relkit 仓 ADR 0013 / 0014 与 SPEC §6.2 / §11 / §12.5 / 附录 B；`relkit stage` 标志在 CLI.md。那些是实现 SSOT，不是产品接入步骤。

不要在产品仓再写一份平行指南。

## 1. 两轨

同一版本可以（启用内部更新时必须）同时发布两类 artifact：

| 轨 | stage 入口 | `kind` | 用途 |
|---|---|---|---|
| 完整安装 | `relkit stage --install <路径>` | `archive` / `installer` / `binary` / `blob` | 冷启动、人页、`/-/latest/...`、内部更新不可用时回退 |
| 内部更新 | `relkit stage --payload <目录>` | `payload` | 已装客户端经 `relkit-updater` 事务化增/覆/删 |

`--install` 不要写成 `kind=payload`。人页与 `latest` **不暴露** payload；过滤在 relkit 发布/站点侧完成，产品仓不要再滤一层。

`--payload <目录>` 把输入树打成 zip（`files.pb` + `files/` + 可选 `scripts/`），并**注入** `apply=relkit-payload`。不要手写其它 `apply` 值。新引擎 check 会带同一键从而选中 payload；老客户端没有该键，仍只匹配同组完整安装包。

输入树可用 `.relkit-payload/manifest.json` 声明包内 `preserve` 与脚本；该配置目录不进入安装树。宿主不必解析 zip 内部。

**payload 输入树禁止软链。** 某平台分发物若本身是软链树（例如带 `Current` 的 `.app` / DMG），在产品打出一棵普通文件树之前，该平台只发 `--install`。这不是 relkit 的平台开关。

### 闸门

每个 payload **必须**另有一个去掉 `apply` 后 `selectors` 完全相同的完整安装 artifact。只发 payload 会被 stage/publish 拒绝。

产物内容由各平台打包 Job 决定；汇总 Job 只收集并 `--install` / `--payload`，不从安装包反推内部更新树。

## 2. 宿主合同

### 只 spawn sidecar

- 唯一引擎是安装树内的 `relkit-updater`（与 facade 同 SHA 锁定）。
- 宿主只通过**生成的 facade** 编排：`check` / `download` / `apply` / …。
- **禁止**在产品仓再实现 apply、解压 payload、或手写第二份 CheckResult / InstallSpec DTO。
- 本机协议是 `relkit.updater.v1`；线上签名对象仍是 `rup.v2`。InstallSpec **不进** index/manifest。
- 产品源码出现 `RupUpdater` / `sdk.Updater`、或 `import relkit_consume`，都是 drift。

### IPC 窗口

破坏性 **`[3, 3]`**。窗口是 facade 编译期常量，不写进 `ClientProfile`。混版本 sidecar 与 facade 会协商失败。发版 `manifest.json` 带 `minUpdaterIpc` / `maxUpdaterIpc`；产品仓 `relkit.lock.json` 的 `updaterIpc` 由 `relkit_host.py upgrade` 从这份 manifest 填写，不要手改、也不要沿用上一份 lock。

### `InstallSpec.placement`

| 值 | 原名（已废） | 行为 |
|---|---|---|
| `IN_PLACE` | `wholeRoot` | 按文件表写入 `install_root` |
| `LIBRARY` | `versionedDir` | 写入 `versions/<version>/`，成功后切换 `active.json` |

`LibraryPolicy`（仅 LIBRARY 有意义）：`retain` 保留几个已安装版本；`reserved_codes` 清理时保留的项目 pin。多实例跨更新至少 `retain=2`。

`preserve`：不覆盖、不删除的相对路径（与包内 preserve 取并集）。

**删除不是可配置轴。** 引擎固定：`delete = previous_baseline − current_table − preserve`。不要写 Ownership / `fullTree` / `declaredOnly`。

`sidecar.layout` 闸门说的是 sidecar **打进发布树的位置**，与 `InstallSpec.placement` 同名不同事。

LIBRARY 要点：

- launcher **由产品自己提供**，放在 `install_root` 根；安装器快捷方式指向 launcher。
- 首装必须自己摆出 `versions/<version>/` 与 `active.json`。
- `executable_relpath` 相对版本目录填。
- `active.json` 只是默认指针；按项目选版靠产品 pin + `CheckOp.exact_code` + `ApplyOp.install_only` + list/switch/rollback。
- macOS 的 LIBRARY：拷完整 `.app` 到 `versions/<id>/<Product>.app`；launcher 必须用 LaunchServices / `open`，禁止 exec `Contents/MacOS/*`。`install_root` 不能放在签名 `.app` 内部。

### `apply_disposition` 与退出

| 值 | 含义 | 宿主动作 |
|---|---|---|
| `INTERNAL` | 选中了 `kind=payload` | 交给 `relkit-updater` apply；**禁止**自行解压 |
| `FULL_INSTALL` | 完整安装轨 | 把已校验本地路径交给平台安装流程 |

`requires_host_exit` 由本次 plan / 文件表判定（例如 in-place 触及当前 executable/sidecar），**不是** placement 的常量。LIBRARY 下新版本目录通常不属于当前运行树。收到 `true` 必须真的退出本进程。

### 禁止项

- 不要写 `meta.layout`（Layout API 已废）。
- 不要把 `placement` 放进 artifact `selectors`；落点只属于本机 `InstallSpec`。
- 不要只升级 SDK 而不 lock 同 SHA 的 `relkit-updater`（IPC `[3,3]`）。
- 不要自己填 `client_selectors.apply=relkit-payload`：引擎 check 时会注入。

## 3. 最小接入步骤

1. **锁定 sidecar + SDK**  
   经 `relkit_host.py` consume / upgrade，把 facade 与同版本 `relkit-updater` 升到支持 payload / Placement 的 release（IPC `[3,3]`）。

2. **填写 `ClientProfile` + `InstallSpec`**  
   Profile：`product`、`allowed_channels`、入口/index/fallback URL、信任公钥。  
   InstallSpec：`placement`、`install_root`、`executable_relpath`、`sidecar_relpath`，需要时加 `preserve` / `LibraryPolicy`。  
   Runtime 再带 `channel`、`current_code`、`data_dir`，以及本机 `client_selectors`（`os`/`arch` 等）。

3. **stage 两轨（同组 selectors）**

   ```bash
   relkit stage 1.5.0 --code 150 \
     --install dist/demoapp-win-x64.zip  kind=archive,os=windows,arch=x64 \
     --payload dist/demoapp-payload      os=windows,arch=x64
   ```

   第二条自动得到 `kind=payload` 与 `apply=relkit-payload`。多平台则每组 `os`/`arch`（及自定义键）各自成对。没有内部更新树的平台只写 `--install`。

4. **publish 后验人页**  
   人页与 `latest` 只应出现完整安装包。若仍看到 payload，查 publish/agent，不要在产品仓写过滤。

5. **宿主运行时**  
   facade `check` → `INTERNAL` 则 `download` + `apply`；`FULL_INSTALL` 则走平台安装。是否先退出看 `requires_host_exit`。

## 4. 最佳实践

- **人页 = 安装器。** 产品门户 / 下载页只挂完整安装轨（`--install`：exe / dmg / deb / 平台安装器），不要把内部更新包当给人点的下载项。
- **payload 不给人下。** `kind=payload` 只给已装客户端经 `relkit-updater` 消费；人页与公开目录不得列出。
- **`latest` id = 人工入口。** `/-/latest/<product>/<channel>/<artifact-id>` 面向人与安装脚本；id 应对应完整安装 artifact，不要指向 payload。
- **无内部更新的平台只 `--install`。** 某平台暂时没有可打包的 payload 树时，只发完整安装包即可；不要为了「凑两轨」伪造空 payload。
- **完整 zip 不是推荐人页形态。** 人页优先平台安装器；裸 `archive` zip 可以发版给机器或回退用，但不宜作为默认「给人点」的主下载形态。
