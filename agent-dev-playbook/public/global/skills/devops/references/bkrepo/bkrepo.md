# 蓝盾制品库（bk-repo）

通过网关 `https://bkrepo.apigw.o.woa.com/prod` 操作 Generic 仓库。认证走 `scripts/auth.py`（`Authorization: Bearer {token}`）。下载/上传先向网关申请临时 token，再访问 `https://bkrepo.woa.com`。

核心概念：`projectId`、`repoName`（如 `generic-local`）、`fullPath`（如 `/release/app-1.0.0.tgz`）。

脚本失败（非 0）时把 stderr 原样给用户，不要猜测原因、不要自动重试。

## 搜索

```bash
python scripts/bkrepo_search.py --project-id {projectId} --repo-name {repoName} --name "*.tgz"
python scripts/bkrepo_search.py --project-id {projectId} --repo-name {repoName} --path-prefix /release/
```

`--metadata key=value` 可重复。结果用 Markdown 表格展示。

## 下载

```bash
python scripts/bkrepo_download.py --project-id {projectId} --repo-name {repoName} --path /release/app-1.0.0.tgz --output ./app-1.0.0.tgz
```

支持断点续传。完成后告知本地保存路径。

## 上传

```bash
python scripts/bkrepo_upload.py --project-id {projectId} --repo-name {repoName} --path /release/app-1.0.0.tgz --local-file ./dist/app-1.0.0.tgz
python scripts/bkrepo_upload.py ... --overwrite
```

覆盖已有文件必须加 `--overwrite`，并先向用户确认。

## 详情

```bash
python scripts/bkrepo_detail.py --project-id {projectId} --repo-name {repoName} --path /release/app-1.0.0.tgz
python scripts/bkrepo_detail.py ... --format json
```
