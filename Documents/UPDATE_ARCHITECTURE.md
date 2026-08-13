# Dec 自更新架构（RUP + COS）

Dec CLI 的检查/下载已切换到 `github.com/shichao402/relkit/sdk`，发布走腾讯云 COS 自有域名。

## 客户端

- 内嵌 `entryUrls`：`https://updates.firoyang.com/rup/directory/dec.pb`
- 内嵌公钥 `dec-2026`（见 `internal/update` 与根目录 `relkit.json`）
- `CurrentCode` = `sdk.SemverCode(version)`（`v1.13.25` → `1013025`）
- 默认 channel：`dev`（目前仅个人使用；正式对外再切回 `stable`）
- selectors：`os` / `arch`（与 stage 时一致）
- Apply：`sdk/apply.ReplaceExecutable`（Windows rename-aside + 下次启动清理）

入口仍是：

- `dec update`（CLI，无确认）
- TUI Run 页 `u`（有确认）
- 启动时 `CheckBackground`（非阻塞，读本地缓存）

`CheckResult{CurrentVersion, LatestVersion, NeedUpdate}` 形状保持不变。

## 发布

1. CI 多平台构建产物 `dec-{os}-{arch}`  
2. `relkit stage` 固化 staged 树（无私钥）  
3. 打包 `staged.tar.gz` → `PUT` `https://publish.firoyang.com/v1/staged/dec/{version}`  
4. `POST /v1/publish` → 发布机签名并写 COS  

密钥：`RELKIT_PRIVATE_KEY` / `COS_SECRET_*` 只在发布机；CI 只有 `RELKIT_AGENT_TOKEN`。

## 与 GitHub Releases 的关系

首次安装脚本（`install.sh` / `install.ps1`）与 GitHub Release / `ReleaseLatest` **暂时保留**。  
日常自更新主路径已是 RUP/COS。安装脚本切 COS 是后续步骤。

## 本地依赖

开发时 `go.mod` 使用：

```
replace github.com/shichao402/relkit => ../relkit
```

CI 会 checkout `shichao402/relkit` 到同级目录以满足该 replace。
