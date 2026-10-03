# 元数据、资源解析与校验命令

## `list-resource-types`

获取资源类型列表。

```bash
python scripts/auth_client.py list-resource-types
```

适合先确认某个权限领域的 `resourceType` 取值。

## `list-actions`

获取指定资源类型的动作列表。

```bash
python scripts/auth_client.py list-actions \
  --resource-type pipeline
```

适合确认 `action` 的准确取值，例如 `pipeline_execute`。

## `search-resource`

按名称或 Code 搜索资源。

```bash
python scripts/auth_client.py search-resource \
  --project-id demo \
  --resource-type pipeline \
  --keyword build
```

适合模糊查找资源，特别是用户只给了部分名称时。

## `get-resource-by-name`

按资源名称解析资源。

```bash
python scripts/auth_client.py get-resource-by-name \
  --project-id demo \
  --resource-type pipeline \
  --resource-name my-pipeline
```

适合在写操作前把展示名解析成 `resourceCode`。

## `get-resource-by-code`

按 `resourceCode` 查询资源详情。

```bash
python scripts/auth_client.py get-resource-by-code \
  --project-id demo \
  --resource-type pipeline \
  --resource-code p-123
```

## `check-project-user`

校验某用户是否是项目成员。

```bash
python scripts/auth_client.py check-project-user \
  --project-id demo \
  --target-user-id alice
```

可选参数：
- `--group`：检查是否属于特定项目组角色
