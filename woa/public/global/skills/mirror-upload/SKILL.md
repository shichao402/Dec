---
name: mirror-upload
description: >
  把制品上传（预置 / 补齐）到腾讯内网 generic mirror（mirrors.tencent.com），
  供内网 CI 构建直接命中内网、不再回落外网官方源。覆盖工具链供给盘点、
  缺口探测、写权限验证、大文件上传、上传后 sha256 读回校验、远端文件校验。
  当用户提及内网 mirror、generic mirror、mirrors.tencent.com、制品预置、
  供给盘点、工具链缺口、MANUAL-UPLOAD-REQUIRED、flutter/node/go SDK 上传、
  内网构建走外网慢、回填 mirrors 时使用。
  不处理流水线编排与构建日志（那是 devops skill 的职责）。
---

# 内网 generic mirror 制品上传

## 何时使用

消费端拉取顺序是 **mirrors GET → 腾讯源 → 官方源 → 回填**。本 skill 承担「回填 / 预置」这一头：

- CI 报 `[MANUAL-UPLOAD-REQUIRED]`——mirrors 与官方源都拿不到，必须人工预置
- 供给盘点发现某个 `mirror/<tool>/<version>/<file>` 是 404 缺口
- 内网构建走外网官方源太慢/不稳，想把大制品（flutter SDK 等 GB 级）预置进内网
- 需要确认上传后的制品与官方源是同一份（sha256 比对）

**判断标准**：目标是「往 mirrors 写东西」用本 skill；目标是「查流水线 / 读构建日志 / 改编排」用 devops skill。

## 环境要求

Python 3.8+，纯标准库（`urllib` / `hashlib` / `argparse`），零第三方依赖，全平台可用。

## 凭据（DEC 密钥仓库）

凭据**绝不硬编码、不写日志、不进仓库**。一律从 DEC 密钥仓库读：

1. `--env-file` 显式指定
2. 环境变量 `MIRRORS_USERNAME` / `MIRRORS_PASSWORD`
3. 默认 DEC 落地路径 `~/.dec/secrets/woa/.env/mirror.env`

env 文件格式（`KEY=value`，值可加引号）：

```ini
MIRRORS_USERNAME=your_user
MIRRORS_PASSWORD=your_token
```

密钥落地与远端 Bitwarden 同步由 Dec 管理（`dec_pull` / `dec_push`）。旧位置 `~/.dec/secrets/config.yaml` 已废弃。

## 镜像路径分区

```
mirror/<tool>/<version>/<filename>
```

完整 URL：`https://mirrors.tencent.com/repository/generic/relkit/mirror/<tool>/<version>/<filename>`

归档名要带平台前缀，且与消费端 `toolchain.json` 钉定完全一致。例：`flutter_macos_arm64_3.38.3-stable.zip`（arch 前缀带下划线）。

## 标准流程

### 1. 先验证写权限（务必先做）

大文件动辄 GB 级，先确认凭据有效再下载，别下了 2GB 才发现没权限。

```bash
python scripts/mirror_upload.py probe
```

返回「写权限验证通过」再继续。

### 2. 探测缺口

```bash
python scripts/mirror_upload.py check flutter 3.38.3 flutter_macos_arm64_3.38.3-stable.zip
```

`EXISTS` 表示已就绪，`MISSING` 表示需要补。

### 3. 下载官方源制品

在**能出公网的机器**上下载（内网办公机通常可以，CI runner 不一定）。URL 取消费端 `toolchain.json` 的钉定项，不要自己拼版本号。

### 4. 上传并校验

```bash
python scripts/mirror_upload.py put <local_file> <tool> <version> <filename>
```

脚本会打印本地 sha256 与大小，上传后**自动流式读回校验**并打印 `VERIFY MATCH`。GB 级文件加 `--no-verify` 可跳过读回（不推荐）。

### 5. 与官方源比对（可选但推荐）

```bash
python scripts/mirror_upload.py remote-sha <official_url>
python scripts/mirror_upload.py remote-sha <mirror_url>
```

两个 sha256 一致才证明内网这份与官方源是同一制品。**不要靠文件大小相同就下结论**。

## 关键约束

- **Content-Type 必须是 `application/octet-stream`**。默认的 `form-urlencoded` 会让 Spring FormContentFilter 把二进制 body 当表单解析炸 500（ADR 0018 T8.1 实证根因）。
- **制品不可覆盖**：PUT 已存在的路径返回 409，视为成功（不可变制品保护）。要换内容只能换版本号或文件名。
- **上传失败不挡构建**：这是 best-effort 动作。CI 里失败应降级走官方源，不要因此置红。
- **不要为了消缺而改消费端代码**：缺什么补什么，relkit 与产品仓 `scripts/` 的拉取逻辑不动。

## 腾讯源补充层

官方源前面插了一层腾讯源（仅对确实存在分区的工具生效，不做通用猜测）。实测盘点：

| 组件 | 腾讯源 | 状态 |
| --- | --- | --- |
| node | `mirrors.tencent.com/nodejs-release/` | 可用 |
| go | `mirrors.tencent.com/go/`（goproxy） | 本就是腾讯源 |
| flutter | — | 无分区，只能靠 mirror 预置 |
| innosetup | — | 无分区，回落 GitHub 官方源 |

**新增工具接腾讯源前必须实测探活**（HEAD + GET Range 双验），404 就是没有，不要凭印象写 URL。

## 参考

- [upload-workflow.md](references/upload-workflow.md) 完整实战案例与踩坑记录
