# Dec 自更新架构（RUP + COS）

Dec 运行时的检查/下载使用 `cnb.cool/shichao402/relkit/sdk`，发布走腾讯云 COS 自有域名。
终端用户只下载 Console；四件套是 Console 管理的目标端运行时。

## 客户端

- 内嵌 `entryUrls`：`https://updates.firoyang.com/rup/directory/dec.pb`
- 公钥单 SSOT：人只改根目录 `relkit.json` → `signing.publicKeys`；`go generate ./internal/update` 复制到 `internal/update/embed/` 后由 `go:embed` 钉进二进制（运行时不读磁盘旁路文件）
- `CurrentCode` = `sdk.SemverCode(version)`（`v1.13.25` → `1013025`）
- 默认 channel：`dev`（目前仅个人使用；正式对外再切回 `stable`）
- selectors：`os` / `arch` / `component` / `audience=runtime`，component 为 `dec`、`dec-server`、`dec-mcp`、`dec-exec`
- Apply：先把同版本四个组件全部下载并校验，再用 `sdk/apply.ReplaceFile` 替换同一 `bin/` 下的程序（Windows rename-aside + 下次启动清理）
- Console bundle：每个安装包只带同 `os/arch` 四件套和 `runtime-manifest.json`；首次连接/升级从 resources 校验后以临时文件 + rename 释放到 `~/.dec/bin`，同时缓存到 `~/.dec/runtime-cache/<version>/<os>-<arch>/`
- SSH 置备：发起端按目标 `os/arch` 命中校验过的缓存则复用，否则请求签名 RUP；只有 RUP head 恰好等于 Console 钉死版本才下载。渠道已有更高版本时提示先更新 Console 或预置旧版本缓存

入口：

- Console **同步** 页（唯一用户面入口）
- `CheckBackground` 仍可供启动时读本地缓存

`CheckResult{CurrentVersion, LatestVersion, NeedUpdate}` 形状保持不变。

## 发布

GitHub Actions（`.github/workflows/release.yml`）按 **relkit 渠道 tag** 触发，不再靠改 `version.json` 推 main 自动发版：

| Git tag | RUP channel | 额外动作 |
|---------|-------------|---------|
| `dev/vX.Y.Z` | `dev` | 仅 COS / RUP |
| `stable/vX.Y.Z` | `stable` | 另推裸 tag `vX.Y.Z`、GitHub Release |

流程：

1. 改 `version.json` 为 `vX.Y.Z`，提交并推 `main`
2. 打渠道 tag 并推送（例：`git tag dev/vX.Y.Z && git push origin dev/vX.Y.Z`）
3. GitHub Actions：Ubuntu 交叉编全平台四件套；每个 Console 原生 runner 都准备 Go 与 relkit SDK，编译并内置同平台四件套
4. `relkit stage --channel <dev|stable>` 把两类产物写入同一次 staged 树（无私钥）
5. runtime 标 `audience=runtime`，Console 标 `audience=user`
6. 打包 `staged.tar.gz` → `PUT` `https://publish.firoyang.com/v1/staged/dec/{version}`
7. `POST /v1/publish` → 发布机签名并写 COS
8. `stable` 的 GitHub Release **只挂** `dec-console-*`；四件套不进人面附件
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
| 全新安装 | [发布页](https://update.firoyang.com/dec.html) 下载当前平台 **Dec Console**（安装包内含同平台四件套） |
| 本机首次连接/升级 | Console 从内置 resources 校验并原子释放四件套到 `~/.dec/bin/`，同时预热同平台缓存，不联网；较新运行时仍拒绝降级 |
| SSH 置备 | 发起端按目标 os/arch 复用经摘要校验的 `runtime-cache`，缺则 RUP 下载到发起端缓存，再通过系统 SSH 推送；目标机不需要 curl、bash 或公网 |
| GitHub Release | `stable` 上 Console 安装包的镜像备份，不挂四件套，也不是自更新逃生梯 |

更老的、尚无 RUP 的 Dec：靠历史版本链跳到第一个含 RUP 的版本，而不是在失败提示里推销重装。

离线边界：同平台套件由 Console 安装包保证；异平台在发起端无网且 `runtime-cache` 未命中时无法取得可信产物，置备会明确失败。缓存以 lock + 临时目录 + rename 防止并发读到半成品，并在每次命中时重验摘要。

RUP 限制：当前 SDK 的 chain 选择最高可达版本，不支持任意历史版本直取。因此旧 Console 在缓存未命中且渠道已前移时不会下载 head 冒充旧版本，而是失败并要求更新 Console 或预先准备对应缓存。SSH 传输后优先在目标用 `sha256sum` / `shasum -a 256` 逐件复验；无 hash 工具时降级为四组件 `--version`，并在激活失败时 best-effort 回滚旧套件。

## 本地依赖

开发 / CI 通过 sparse-checkout 拉取 relkit：

```
scripts/ensure_relkit_sparse.py --sdk-only
```

落到 `third_party/relkit/`，`go.mod` 使用：

```
replace cnb.cool/shichao402/relkit => ./third_party/relkit
```

上游 URL 默认 `https://cnb.cool/shichao402/relkit.git`。**ref 默认钉在已验证完整 commit `3d706f9a27aa37ab3dd7471ec4f48dfe638b5ba5`**（relkit `main` 上已推送；短 SHA 不能直接 `git fetch`），发布流水线（`.github/workflows/release.yml`）显式传同一完整 SHA，不要跟 `main` HEAD 漂。升级时先在本机验证该 commit，再同步改 `scripts/ensure_relkit_sparse.py` 的 `DEFAULT_REF` 与 workflow。临时覆盖可用 `--ref` / `RELKIT_URL` / `RELKIT_REF`。
