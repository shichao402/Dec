# 查询命令

所有命令的 token 和用户 ID 默认由[统一鉴权配置](../auth-setup.md)读取：

```bash
python scripts/auth_client.py <command>
```

仅当用户本轮对话明确提供对应值时，才通过 `--access-token` 或 `--user-id` 传入。

## `list-groups`

查询项目用户组列表。

```bash
python scripts/auth_client.py list-groups \
  --project-id demo \
  --resource-type pipeline \
  --resource-code p-123 \
  --group-name 执行 \
  --page 1 \
  --page-size 20
```

常用参数：
- `--iam-group-ids`：按用户组 ID 列表过滤，逗号分隔
- `--resource-type` / `--resource-code`：按资源维度过滤
- `--group-name`：按用户组名称模糊匹配

## `get-group-permissions`

查询单个用户组的权限详情。

```bash
python scripts/auth_client.py get-group-permissions \
  --project-id demo \
  --group-id 12345
```

适合在 `add-members` 前确认该 `relationId` 实际对应哪些权限。

## `list-group-members`

查询用户组成员详情列表。

```bash
python scripts/auth_client.py list-group-members \
  --project-id demo \
  --iam-group-id 12345 \
  --page 1 \
  --page-size 20
```

常用参数：
- `--resource-type` / `--resource-code`
- `--iam-group-id`
- `--group-code`
- `--member-id`
- `--member-type`
- `--min-expired-at` / `--max-expired-at`

## `list-project-members`

查询项目成员列表。

```bash
python scripts/auth_client.py list-project-members \
  --project-id demo \
  --user-name alice \
  --page 1 \
  --page-size 20
```

常用参数：
- `--member-type`
- `--user-name`
- `--departed`

## `list-member-groups`

查询成员全部用户组详情。管理员治理场景里，这是查看成员权限的主入口。

```bash
python scripts/auth_client.py list-member-groups \
  --project-id demo \
  --member-id alice \
  --resource-type pipeline \
  --action pipeline_execute
```

常用参数：
- `--resource-type`
- `--group-name`
- `--iam-group-ids`
- `--min-expired-at` / `--max-expired-at`
- `--related-resource-type` / `--related-resource-code`
- `--action`

常见时间过滤：
- 已过期：`--max-expired-at <now>`
- 即将过期：`--min-expired-at <now> --max-expired-at <now + N*86400000>`
- 未过期：`--min-expired-at <now>`

## `search-users`

根据关键词搜索用户。

```bash
python scripts/auth_client.py search-users \
  --keyword alice \
  --project-id demo \
  --limit 10
```

适合确认用户 ID 拼写，或在当前项目成员里查找候选人。

