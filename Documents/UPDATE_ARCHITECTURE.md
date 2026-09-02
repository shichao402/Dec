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

入口：

- Console **同步** 页（唯一用户面入口）
- `CheckBackground` 仍可供启动时读本地缓存

`CheckResult{CurrentVersion, LatestVersion, NeedUpdate}` 形状保持不变。

## 发布

CNB（`.cnb.yml`）按 **relkit 渠道 tag** 触发，不再靠改 `version.json` 推 main 自动发版：

| Git tag | RUP channel | 额外动作 |
|---------|-------------|---------|
| `dev/vX.Y.Z` | `dev` | 仅 COS / RUP |
| `stable/vX.Y.Z` | `stable` | 另推裸 tag `vX.Y.Z`、GitHub Release |

流程：

1. 改 `version.json` 为 `vX.Y.Z`，提交并推 `main`
2. 打渠道 tag 并推送（例：`git tag dev/vX.Y.Z && git push origin dev/vX.Y.Z`）
3. CNB 构建四件套；原生 runner 构建 `dec-console-{os}-{arch}.<ext>`
4. `relkit stage --channel <dev|stable>` 固化 staged 树（无私钥）
5. runtime 标 `audience=runtime`，Console 标 `audience=user`；同版写入一个 staged tree
6. 打包 `staged.tar.gz` → `PUT` `https://publish.firoyang.com/v1/staged/dec/{version}`
7. `POST /v1/publish` → 发布机签名并写 COS；升级后的 relkit-serve 人类 browse 页只展示 `audience=user`

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
- `COS_SECRET_*` 只在发布机；CI 只有 `RELKIT_PUBLISH_TOKEN`（无私钥；可选 `RELKIT_PUBLISH_URL`，默认 `https://publish.firoyang.com`）。KeyStore 文件：`relkit_release.env.yml`。

## 与首次安装 / GitHub 的关系

| 场景 | 路径 |
|------|------|
| 日常自更新（已装 RUP 客户端） | 只走 `https://updates.firoyang.com/`；失败时排查网络/代理，**不要**改跑 install 脚本 |
| 全新安装 | **主路径 CNB raw**：`https://cnb.cool/shichao402/Dec/-/git/raw/main/scripts/install.sh`（Windows 换 `install.ps1`） |
| 安装脚本拉二进制 | 优先 COS RUP artifact：`https://updates.firoyang.com/rup/artifact/dec/{version}/{name}`；GitHub Release 仅脚本内 fallback |
| GitHub | 文档里的**镜像备份**（脚本 URL / Release asset），不是自更新逃生梯 |

更老的、尚无 RUP 的 Dec：靠历史版本链跳到第一个含 RUP 的版本，而不是在失败提示里推销重装。

## 本地依赖

开发 / CI 通过 sparse-checkout 拉取 relkit：

```
scripts/ensure_relkit_sparse.py --sdk-only
```

落到 `third_party/relkit/`，`go.mod` 使用：

```
replace cnb.cool/shichao402/relkit => ./third_party/relkit
```

上游 URL 默认 `https://cnb.cool/shichao402/relkit.git`。**ref 默认钉在已验证完整 commit `6c78d29fbd54efa87e6adf189fb9b7b277accd7c`**（relkit `main` 上已推送；短 SHA 不能直接 `git fetch`），发布流水线（`.cnb.yml`）显式传同一完整 SHA，不要跟 `main` HEAD 漂。升级时先在本机验证该 commit，再同步改 `scripts/ensure_relkit_sparse.py` 的 `DEFAULT_REF` 与 `.cnb.yml`。临时覆盖可用 `--ref` / `RELKIT_URL` / `RELKIT_REF`。
