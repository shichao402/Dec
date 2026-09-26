# 管理员操作命令

这里的命令都是管理员治理命令。执行前先查、先预检、先确认，再执行。

## `recommend-groups`

根据资源和动作，为目标成员推荐最合适的用户组。

```bash
python scripts/auth_client.py recommend-groups \
  --project-id demo \
  --target-user-id alice \
  --resource-type pipeline \
  --resource-code p-123 \
  --action pipeline_execute
```

推荐路径：
1. 先 `search-resource` 或 `get-resource-by-name` 解析出 `resourceCode`
2. 再 `recommend-groups`
3. 展示推荐理由和 `relationId`
4. 用户确认后，再 `add-members`

## `add-members`

批量添加用户组成员。

```bash
python scripts/auth_client.py add-members \
  --project-id demo \
  --group-id 12345 \
  --target-user-ids alice,bob \
  --expired-days 365
```

注意：
- 优先传 `--group-id`
- 写操作前要先向用户展示目标成员、用户组、权限范围和有效期

## `renewal`

批量续期成员权限。

```bash
python scripts/auth_client.py renewal \
  --project-id demo \
  --group-ids 12345,23456 \
  --target-member-id alice \
  --renewal-days 180
```

推荐前置步骤：
1. `list-member-groups` 找出即将过期的用户组
2. 展示用户组列表和续期天数
3. 用户确认后执行

## `operate-check`

做基础预检查，支持 `RENEWAL`、`REMOVE`、`HANDOVER`。

```bash
python scripts/auth_client.py operate-check \
  --project-id demo \
  --operate-type REMOVE \
  --group-ids 12345,23456 \
  --target-member-id alice
```

适合：
- 在 `remove` / `handover` 前看基础阻塞项
- 续期前确认目标范围是否合理

## `exit-check`

检查成员退出/交接的可行性，并推荐交接人。

```bash
python scripts/auth_client.py exit-check \
  --project-id demo \
  --target-member-id alice \
  --group-ids 12345,23456 \
  --recommend-limit 5
```

如果要验证指定接收人：

```bash
python scripts/auth_client.py exit-check \
  --project-id demo \
  --target-member-id alice \
  --handover-to bob
```

这是移除、交接、移出项目之前的推荐预检查入口。

## `remove`

从指定用户组中移除成员。

```bash
python scripts/auth_client.py remove \
  --project-id demo \
  --group-ids 12345,23456 \
  --target-member-id alice
```

执行前必须先展示：
- 目标成员
- 用户组列表
- `exit-check` / `operate-check` 的阻塞项

## `handover`

把成员在指定用户组中的权限交接给另一人。

```bash
python scripts/auth_client.py handover \
  --project-id demo \
  --group-ids 12345,23456 \
  --target-member-id alice \
  --handover-to bob
```

执行前必须先确认：
- 接收人是否能接收全部授权
- 是否仍有代码库 / 环境节点 / 唯一管理员组阻塞

## `remove-from-project-check`

批量移出项目成员前的基础检查。

```bash
python scripts/auth_client.py remove-from-project-check \
  --project-id demo \
  --target-member-ids alice,bob
```

更推荐在复杂场景先跑 `exit-check` 看完整授权阻塞和推荐交接人。

## `remove-from-project`

批量将成员移出项目。

```bash
python scripts/auth_client.py remove-from-project \
  --project-id demo \
  --target-member-ids alice,bob \
  --handover-to charlie
```

执行前必须确认：
- 目标成员列表
- 授权阻塞项
- 指定交接人是否可接收

重要限制：
- 如果成员仍持有**代码库授权**、**流水线代持**、**环境节点授权**或**唯一管理员组**，
  通常不能直接移出项目。
- 这类场景下需要先通过 `exit-check` 明确阻塞项，并提供 `--handover-to` 作为移交人。
- 只有完成对应授权交接后，`remove-from-project` 才可能成功。

## `clone-permissions`

将来源用户的权限复制给目标用户。

预演：

```bash
python scripts/auth_client.py clone-permissions \
  --project-id demo \
  --source-user-id alice \
  --target-user-id bob \
  --dry-run true
```

执行：

```bash
python scripts/auth_client.py clone-permissions \
  --project-id demo \
  --source-user-id alice \
  --target-user-id bob \
  --dry-run false
```

建议始终先 `--dry-run true`，展示差异后再执行。
