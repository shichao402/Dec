# 蓝盾访问令牌配置

## 获取 token

统一通过 <https://devops.woa.com/ms/auth/api/user/bkToken/get> 获取 `access_token`。

Agent 每次引导用户获取 token 时，都必须完整说明：

1. 先直接访问 <https://devops.woa.com/ms/auth/api/user/bkToken/get>。
2. 仅当该链接返回 401、无权限或提示未登录时，访问 <https://devops.woa.com/console/> 完成登录。
3. 登录后回到 token 链接重新获取。

禁止只提供 token 链接而省略第二步的条件说明；也不要把登录控制台描述为所有用户的固定前置步骤。

获取后可以按下面任一方式配置。

## 方式一：直接发给 AI

用户可以把 token 直接提供给 AI。Agent 必须：

1. 不在回复中复述 token。
2. 本次调用通过 `--access-token` 传入。
3. 同时执行 `python scripts/auth_cli.py login --access-token "<token>"` 写入 `config.json`，下次无需重复提供。

命令行参数可能出现在进程列表中，只用于用户本轮明确提供 token 的场景；日常调用不应反复携带。

## 方式二：终端交互输入

用户也可以在自己的终端登录。Agent 必须先写出当前 skill 根目录的绝对路径（含 `SKILL.md` 的目录），再给命令。禁止只写 `python3 scripts/auth_cli.py login`。

```bash
cd /实际的/skill根目录
python3 scripts/auth_cli.py login
```

不要加 `--access-token`。脚本会提示输入，输入时不回显，并写入该目录下的 `config.json`。token 不进入对话，也不进入进程参数。Agent 不要代为执行这条无参数命令。

## 方式三：自行配置

`config.example.json` 和 `config.json` 都在当前 skill 根目录，和 `SKILL.md` 同一层。Agent 必须写出该目录的绝对路径，并给出可直接执行的复制命令。禁止只说「复制 config.example.json」。

```bash
cd /实际的/skill根目录
cp config.example.json config.json
```

然后编辑该目录下的 `config.json`，填写：

```json
{
  "access_token": "你的 token",
  "user_id": "权限治理时填写企业微信英文名",
  "hot_reload": true,
  "pipelines": []
}
```

`config.json` 是本地密钥文件，已被 git 忽略。不要提交、分享或在回复中回显它。不想自动热更新时把 `hot_reload` 设为 `false`，详见 [热更新](hot-reload.md)。

可用以下命令检查配置，输出不会包含 token：

```bash
cd /实际的/skill根目录
python3 scripts/auth_cli.py status
```

## 旧环境变量兼容

为兼容已经完成旧配置的用户，统一 Skill 仍支持：

```bash
export BK_CI_ACCESS_TOKEN="你的 token"
export BK_CI_USER_ID="权限治理时使用的企业微信英文名"
```

环境变量是兼容方案，不作为新用户的推荐配置方式。凭证读取优先级为：

1. 本次命令显式参数
2. `config.json`
3. `BK_CI_ACCESS_TOKEN` / `BK_CI_USER_ID`

因此，`config.json` 已有值时不会使用同名环境变量。环境变量还必须存在于实际启动 Agent 或脚本的进程环境中；只在另一个终端执行 `export`，不会自动影响已启动的 IDE 或 Agent 进程。

## 401 / 令牌失效

1. 直接重新打开 <https://devops.woa.com/ms/auth/api/user/bkToken/get> 获取 token。
2. 仅当接口返回 401 或提示未登录时，登录 <https://devops.woa.com/console/> 后重试。
3. 把新 token 发给 AI，在 skill 根目录执行 `cd /实际的/skill根目录` 后再运行 `python3 scripts/auth_cli.py login`，或自行更新该目录下的 `config.json`。
4. 写操作失败后不要自动重试；重新展示参数并获得确认。

## 清理本地凭证

```bash
python scripts/auth_cli.py logout
python scripts/auth_cli.py logout --all
```

前者只清除 token，后者同时清除 `user_id`。
