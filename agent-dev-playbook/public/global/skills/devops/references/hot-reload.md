# 热更新

`devops` skill 通过 BK-Repo 上的完整 ZIP 更新：检查远端版本、下载并校验 ZIP、解压到系统临时目录，再逐文件覆盖当前安装目录。BK-Repo 的 `X-Checksum-Sha256` 响应头直接作为版本号，不需要额外的版本接口。

## 更新流程

1. 5 分钟 TTL 内不联网。
2. 向固定的 `devops.zip` 地址发送 `HEAD` 请求。更新源必须是 `https://`，其他协议直接报错。
3. 读取 `X-Checksum-Sha256`；若没有则兼容读取同值的 `ETag`。
4. 摘要与本地 `.skill_version` 相同，只刷新 TTL，不下载 ZIP。
5. 摘要不同，下载完整 ZIP，并重新计算 SHA256。
6. 校验通过后解压到系统临时目录，并取得安装锁。
7. 覆盖前把当前 Skill 自身文件打成 ZIP，保存到状态目录的 `backups/`。只保留最近 5 份。
8. 同时把当前安装目录快照到另一个临时目录，用于失败回滚。
9. 文件通过同目录 `.tmp` 逐个原子覆盖；按安装清单删除本 skill 自己的旧文件。
10. 更新成功后记录 SHA256 版本、文件清单和 TTL。

网络、下载、SHA256 校验或安装失败时不记录新版本。覆盖过程中出错会用快照把安装目录整体还原回更新前的状态，不会留下半新半旧的 Skill。普通会话初始化采用 fail-open：提示一次后继续处理用户需求，失败检查写入 5 分钟冷却，避免同一会话反复联网重试。用户明确执行 `--force` 时不写失败冷却，并通过非零退出码报告失败。

同一个安装目录同时只允许一个进程覆盖文件。抢不到锁时本次直接跳过（`action` 为 `skipped`，`reason` 为 `locked`），沿用本地版本；超过 10 分钟的残留锁会被自动清理。

## 本地配置保护

以下内容永远不会被 ZIP 覆盖或删除，也不会进入 `--pack` 生成的发布包：

- `config.json`：用户本地 token 和配置。
- `.knot/`：Knot 安装元数据。
- `__pycache__/`、`.pyc`、`.pyo`：Python 缓存。

删除只针对本 Skill 自己装进去的文件：状态目录里的 `.skill_files` 记录上一次安装的文件清单，只有清单里、而新 ZIP 不再提供的文件才会被删。还没有清单时（例如 Knot 刚装完的第一次更新）只清理 ZIP 同样提供的子目录（如 `scripts/`、`references/`），顶层陌生文件一律保留。

因此下载到安装目录里的制品、日志和二维码（`--output ./app-1.0.0.tgz`、`app-1.0.0_apk.png`、`full_log/` 等）不会被热更新删除，也不会打进备份包。Skill 自身的文件以远端 ZIP 为准，本地手工修改会在下一次成功更新时被覆盖；覆盖前的那一版会留在 `backups/`，成功时 JSON 的 `local_backup` 是这份 ZIP 的路径。`config.json` 含本地 token，不进入备份包。

本地安装不想自动热更新时，在 `config.json` 写入：

```json
{
  "hot_reload": false
}
```

缺省或 `true` 表示开启。该文件不会被 ZIP 覆盖，所以开关会一直生效。会话初始化执行 `python3 ./hot_reload.py` 时会跳过联网，`action` 为 `skipped`，`reason` 为 `config.json`。用户明确要求更新时，`--force` 仍会检查并覆盖（`config.json` 本身除外）。

本地开发或测试未发布版本时设置环境变量，硬关闭热更新，且 `--force` 也无法绕过：

```bash
export DEVOPS_NO_HOT_RELOAD=1
```

仓库源码目录通过外层同时存在 `CONTRIBUTING_GUIDE.md` 和 `.git` 自动识别并跳过。确实需要覆盖源码目录时：

```bash
export DEVOPS_HOT_RELOAD_IN_SOURCE=1
python3 ./hot_reload.py --force
```

## 更新源

默认地址：

```text
https://bkrepo.woa.com/generic/bkdevops/static/skill/devops.zip
```

需要临时灰度其他制品时可设置 `DEVOPS_SKILL_ARTIFACT_URL`。

制品库只需一个 `devops.zip`。无需额外上传 manifest、版本文件或 SHA256 文件，因为 BK-Repo 已在响应头提供摘要。若需要人工回滚审计，可以额外保存 `history/devops-{sha256}.zip`，客户端不会读取历史目录。

## Agent 怎么跑

每次会话首次加载只尝试一次：

```bash
python3 ./hot_reload.py
```

用户明确要求立即检查或更新：

```bash
python3 ./hot_reload.py --force
```

脚本 stdout 输出 JSON：

- `updated`：已更新并记录新 SHA256。`local_backup` 是覆盖前本地版本的 ZIP 路径；没有可备份的 Skill 文件时为空。
- `up_to_date`：远端 SHA256 与本地一致。
- `skipped`：因 TTL、`config.json` 的 `hot_reload: false`、环境变量、源码目录或安装锁被占用（`reason: locked`）而未更新。
- `error`：探测、校验或安装失败；`continue_with_local: true` 表示应继续使用本地 Skill，`retry_recommended: false` 表示不要在当前会话自动重试。

普通初始化即使 `action` 为 `error` 也返回退出码 0，避免热更新阻塞主任务；必须读取 JSON，不能仅凭退出码判断更新成功。用户明确执行 `--force` 时，失败仍返回非零。Agent 对初始化错误只提示一次并继续原任务，禁止围绕热更新反复排查或重试。

若 `python3 ./hot_reload.py` 报找不到文件，简短提示本地安装包可能不完整，但仍继续使用现有 Skill。只有当前任务确实因文件缺失无法完成时，才建议从 Knot 重新安装或完整下载新版 ZIP；不要手工拼单个文件。

## 发布与验证

在 `devops` 目录执行：

```bash
python3 ./hot_reload.py --self-test
python3 ./hot_reload.py --pack ../dist/devops.zip
```

`--pack` 会跳过输出文件自身，但仍建议把 ZIP 写到 `devops/` 目录之外，避免上一轮的产物被打进下一个包。

上传后检查：

```powershell
curl.exe -sS -IL "https://bkrepo.woa.com/generic/bkdevops/static/skill/devops.zip"
Get-FileHash .\devops.zip -Algorithm SHA256
```

响应头 `X-Checksum-Sha256` 必须与本地 ZIP 的 SHA256 一致。

## 状态目录

TTL、本地版本、安装清单和安装锁都存放在：

```text
~/.devops-skill-manager/hot_reload/{安装目录哈希}/
├── .update_cache   # 上次检查时间戳（TTL）
├── .skill_version  # 当前安装的 ZIP SHA256
├── .skill_files    # 上次安装的文件清单，决定哪些旧文件可以删
├── .install_lock   # 安装期间的互斥锁
└── backups/        # 覆盖前的本地 Skill 版本，文件名带时间和当时的版本前缀，只留最近 5 份
```

不同安装路径使用不同哈希，因此同一用户复制多份 Skill 时不会冲突。状态放在 Skill 目录之外，所以更新覆盖不会丢。可通过 `DEVOPS_SKILL_MANAGER_HOME` 覆盖状态根目录。
