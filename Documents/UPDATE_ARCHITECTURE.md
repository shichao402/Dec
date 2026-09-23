# Dec 自更新架构（RUP + COS）

Console 自更新由 Tauri 壳调用 `third_party/relkit/sdk/rust` facade 与 lock-pinned `relkit-updater` sidecar；目标运行时套件的检查/下载仍使用 `go.firoyang.com/relkit/sdk`（`replace` 到 `third_party/relkit`）。发布走腾讯云 COS 自有域名。
终端用户只下载 Console；运行时套件是 Console 管理的目标端程序组。

## 两条客户端链

- 共同入口来自根目录 `relkit.json`：`https://raw.firoyang.com/rup/directory/dec.pb`
- Console 壳用 `include_str!` 编入同一份 `relkit.json`；Go 运行时套件链通过 `go generate ./internal/update` 复制到 `internal/update/embed/`。后者只服务 `audience=runtime`，不是 Console 自更新入口
- `CurrentCode` = `sdk.SemverCode(version)`（`v1.13.25` → `1013025`）
- 默认 channel：`stable`；Console 设置页可改并落盘到本机壳偏好（`preferences.json`），检查与安装都跟所选渠道。发版仍由 `dev/v*` / `stable/v*` tag 决定
- Console selectors：`os` / `arch` / `component=console` / `audience=user`；Tauri 直接 `Updater::open`、`check`、`download`，安装器启动、Windows `/S`、detach 与 relaunch 留在壳内
- Tauri 只输出 relkit canonical ProtoJSON；前端从 `third_party/relkit/bindings/ts` import 生成的 `CheckResultSchema` / `StatusSnapshotSchema`，按 `upToDate`、`updateAvailable`、`fallbackRequired`、`throttled`、`failed` 五变体渲染
- 运行时 selectors：`os` / `arch` / `component` / `audience=runtime`，component 为 `dec-server`、`dec-exec`、`dec-host-setup`
- Console bundle：每个安装包只带同 `os/arch` 运行时套件和 `runtime-manifest.json`；首次连接/升级从 resources 校验后以临时文件 + rename 释放到 `~/.dec/bin`，同时缓存到 `~/.dec/runtime-cache/<version>/<os>-<arch>/`
- SSH 置备：发起端按目标 `os/arch` 命中校验过的缓存则复用，否则请求签名 RUP；只有 RUP head 恰好等于 Console 钉死版本才下载。渠道已有更高版本时提示先更新 Console 或预置旧版本缓存

入口：

- Console **设置** 页（唯一用户面入口）
- 更新由本机 Console 壳及其内置 `relkit-updater` sidecar 执行，不经过当前目标 `dec-server`；连接与解锁页复用更新面板，因此未连接也能操作
- Console 启动时自动检查；`CheckPolicy.after_success=24h`、`after_failure=1h` 由引擎执行节流，只提示、不自动安装
- 手动检查忽略节流；用户确认后才下载并启动安装包

## 发布

GitHub Actions（`.github/workflows/release.yml`）按 **relkit 渠道 tag** 触发，不再靠改 `version.json` 推 main 自动发版：

| Git tag | RUP channel | 额外动作 |
|---------|-------------|---------|
| `dev/vX.Y.Z` | `dev` | 仅 COS / RUP |
| `stable/vX.Y.Z` | `stable` | 另推裸 tag `vX.Y.Z`、GitHub Release |

流程：

1. 改 `version.json` 为 `vX.Y.Z`，提交并推 `main`
2. 打渠道 tag 并推送（例：`git tag dev/vX.Y.Z && git push origin dev/vX.Y.Z`）
3. GitHub Actions：Ubuntu 交叉编全平台运行时套件；Console 只在 `windows-latest` 与 `macos-15-intel` 上原生编两套人面安装包并内置同平台套件（不发 Linux / darwin-arm64 Console）
4. `relkit stage --channel <dev|stable>` 把两类产物写入同一次 staged 树（无私钥）
5. runtime 标 `audience=runtime`，Console 标 `audience=user`
6. `relkit cas-put` 向 agent 申请唯一 ingest 的上传 URL：COS 已有同 sha256 则跳过，否则 CI 直接 PUT；随后只上传 `staged.pb` + `release-policy.json`
7. `POST /v1/publish` → 发布机从 CAS Promote、签名并写 COS
8. `stable` 的 GitHub Release **只挂** `dec-console-*`；运行时组件不进人面附件
9. 人类 browse 页按 audience 过滤依赖 **relkit-serve 发布端**升级，不能靠 Dec 本地 stage 单方面完成

## 签名密钥

| 材料 | 位置 | 说明 |
|------|------|------|
| 公钥 | `relkit.json` → `signing.publicKeys[]`；编译内嵌于 `internal/update/embed/relkit.json` | **不要**放进 secrets / Bitwarden |
| 私钥（本地 SSOT） | `.secrets/bundles/dec/keys/dec-2026.private.pb` | 相对 **含 `relkit.json` 的项目根**（relkit 用配置文件所在目录解析 `privateKeyPath`，与进程 cwd 无关）。仅本机应急 `relkit publish` 用 |
| Bitwarden | folder `bundle/dec`，Note 名 `keys/dec-2026.private.pb` | pull 后落到上述本地路径 |
| 私钥（发布机） | `/srv/relkit/dec/.relkit-keys/dec-2026.private.pb` | 产品自己的文件；`relkit-agent` 只读这一份。**不要**用共享环境变量 |
| `bundle/relkit` 里的产品私钥 | **已迁走** | 产品身份不进 relkit 工具 bundle |

- `.secrets/` 已 gitignore；**禁止**把私钥内容 commit 进 git。
- `.env` **不用于** relkit 私钥。
- **不要**设置 `RELKIT_PRIVATE_KEY` / `signing.privateKeyEnv`。
- `COS_SECRET_*` 只在发布机；CI 只有 **该产品** 的 `RELKIT_UPLOAD_TOKEN`（无私钥；可选 `RELKIT_AGENT_URL`，可写站点根或 `/v1`，默认 `https://publish.firoyang.com/v1`）。放到 GitHub 仓库 Secrets。同一 agent 上的每个产品各自一张 token；没有实例级 `RELKIT_AGENT_TOKEN`。

## 与首次安装 / GitHub 的关系

| 场景 | 路径 |
|------|------|
| 日常自更新（已装 RUP 客户端） | 只走 `https://updates.firoyang.com/`；失败时排查网络/代理，**不要**改跑 install 脚本 |
| 全新安装 | [发布页](https://update.firoyang.com/dec.html) 下载当前平台 **Dec Console**（安装包内含同平台运行时套件） |
| 本机首次连接/升级 | Console 从内置 resources 校验并原子释放运行时套件到 `~/.dec/bin/`，同时预热同平台缓存，不联网；较新运行时仍拒绝降级 |
| SSH 置备 | 发起端按目标 os/arch 复用经摘要校验的 `runtime-cache`，缺则 RUP 下载到发起端缓存，再通过系统 SSH 推送；目标机不需要 curl、bash 或公网 |
| GitHub Release | `stable` 上 Console 安装包的镜像备份，不挂运行时组件，也不是自更新逃生梯 |

更老的、尚无 RUP 的 Dec：靠历史版本链跳到第一个含 RUP 的版本，而不是在失败提示里推销重装。

离线边界：同平台套件由 Console 安装包保证；异平台在发起端无网且 `runtime-cache` 未命中时无法取得可信产物，置备会明确失败。缓存以 lock + 临时目录 + rename 防止并发读到半成品，并在每次命中时重验摘要。

RUP 限制：当前 SDK 的 chain 选择最高可达版本，不支持任意历史版本直取。因此旧 Console 在缓存未命中且渠道已前移时不会下载 head 冒充旧版本，而是失败并要求更新 Console 或预先准备对应缓存。SSH 传输后优先在目标用 `sha256sum` / `shasum -a 256` 逐件复验；无 hash 工具时降级为四组件 `--version`，并在激活失败时 best-effort 回滚旧套件。

## 本地依赖

开发 / CI 通过 checksum-pinned 的 release 附件安装 relkit：

```
python scripts/host/relkit_host.py install
```

升 lock（在 relkit 打出含 `bindings-ts` 的 GitHub Release 之后）：

```
python scripts/host/relkit_host.py upgrade v0.3.22
```

Go SDK 落到 `third_party/relkit/`，Rust facade 落到 `third_party/relkit/sdk/rust/`，TypeScript 绑定落到 `third_party/relkit/bindings/ts/`，CLI / updater 落到 `tools/bin/`。Tauri 与前端只 import 已 consume 的生成物；产品仓不生成 updater 协议代码。`go.mod` 使用：

```
replace go.firoyang.com/relkit => ./third_party/relkit
```

上游 release、commit 与附件哈希来自 `scripts/relkit.lock.json`（schema `relkit.consume/2`）。入口是 `scripts/host/relkit_host.py`，不要再跑根目录的 v1 `scripts/relkit_consume.py`。Go 模块路径是 `go.firoyang.com/relkit`（replace 到 `third_party/relkit`），不要 `go get` 该模块。
