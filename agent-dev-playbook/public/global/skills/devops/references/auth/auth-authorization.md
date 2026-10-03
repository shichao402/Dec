# 授权与代持人命令

这组命令主要用于查询资源授权记录和流水线代持人治理。

## `get-resource-authorization`

查询某个资源的授权记录。

```bash
python scripts/auth_client.py get-resource-authorization \
  --project-id demo \
  --resource-type pipeline \
  --resource-code p-123
```

适合查看：
- 当前资源有哪些授权记录
- 谁在代持该资源

## `list-pipeline-authorization`

分页查询项目里的流水线代持人列表。

```bash
python scripts/auth_client.py list-pipeline-authorization \
  --project-id demo \
  --pipeline-name build \
  --handover-from alice \
  --page 1 \
  --page-size 20
```

适合先筛出目标流水线和当前代持人，再决定是否要重置。

## `reset-pipeline-authorization`

重置流水线授权人。

```bash
python scripts/auth_client.py reset-pipeline-authorization \
  --project-id demo \
  --pipeline-id p-123 \
  --handover-to bob
```

执行前必须先确认：
- 流水线 ID
- 当前代持人
- 新代持人

推荐流程：
1. `list-pipeline-authorization`
2. 展示变更前信息，等用户确认
3. `reset-pipeline-authorization`
4. 再次查询验证结果
