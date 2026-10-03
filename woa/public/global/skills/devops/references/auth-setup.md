# 鉴权配置

本 skill 的所有脚本统一通过 `scripts/auth.py` 读取凭据，入口顺序为：命令行参数 → `config.json` → 环境变量。

## 自动换票（推荐）

在 skill 根目录（含 `SKILL.md` 的目录）执行：

```bash
python scripts/login.py
```

流程：

1. 脚本自动拉起系统 Edge（专用 profile，与日常浏览器隔离），打开蓝盾控制台。
2. 未登录时窗口保持打开，等待在窗口内完成登录（扫码/账密均可）；已登录时通常数秒内完成。
3. 捕获到 `bk_ticket` 后自动完成 bkoauth 换票 + 校验 + 续期，并把 `access_token`/`refresh_token` 回写 `config.json`。
4. token 失效时重跑该脚本即可，profile 保存了登录态，多数情况无需再登录。

前提：`config.json` 已填好 `app_code`、`app_secret`、`user_id` 三个字段（蓝鲸开发者中心创建应用后获得；`user_id` 为你的 rtx 账号）。

`--headless` 可无窗口运行，但仅当 profile 内票据仍存活时有效。

## 手动 token（兜底）

自动换票不可用时（例如非 Windows 环境、无 Edge），从 <https://devops.woa.com/ms/auth/api/user/bkToken/get> 获取 access_token。仅当该链接返回 401、无权限或未登录时，先登录 <https://devops.woa.com/console/> 再重试。

获取后任选一种配置方式：

1. 在 skill 根目录执行 `python3 scripts/auth_cli.py login`，按提示粘贴（输入不回显，token 不进对话、不进进程参数）。
2. 直接编辑 `config.json` 写入 `access_token` 字段。

手动 token 只有 `access_token`，没有 `refresh_token`，到期（约 180 天）需重新获取。

## 环境要求

- Python 3.8+，Windows（自动换票需要系统 Edge；纯 API 脚本不限系统）。
- 第三方依赖仅 `PyYAML`，缺失时首次运行自动安装到 skill 私有 `lib/` 目录（不碰全局环境、零手动步骤）。`login.py`、`auth.py` 及全部网络请求为纯标准库。

## 字段说明

| 字段 | 用途 | 来源 |
| ---- | ---- | ---- |
| `app_code` / `app_secret` | bkoauth 应用凭据，换票与续期必需 | 蓝鲸开发者中心 |
| `user_id` | rtx 账号，换票与权限治理的操作者 | 本人 rtx |
| `access_token` | 用户态 API 令牌 | login.py 自动写入或手动获取 |
| `auth.refresh_token` | 续期原料，换票成功后自动写入 | login.py 自动写入 |
