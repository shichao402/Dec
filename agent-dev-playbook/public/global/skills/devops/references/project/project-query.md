# 项目查询

脚本：`scripts/project_client.py`

## 查询项目列表

```bash
python scripts/project_client.py list
```

实际 `ApigwProjectResourceV4.list` 支持以下查询参数：

- `--product-ids` → `productIds`：多个运营产品 ID 以逗号分隔
- `--channel-codes` → `channelCodes`：多个渠道号以逗号分隔
- `--sort` → `sort`：`PROJECT_NAME` 或 `ENGLISH_NAME`
- `--page` → `page`：服务端默认 `1`
- `--page-size` → `pageSize`：服务端默认 `10`

例如：

```bash
python scripts/project_client.py list \
  --sort PROJECT_NAME \
  --page 1 \
  --page-size 20
```

调用：

```text
GET /v4/apigw-user/projects/project_list
```

列表防截断：

- `list` 必须显式分页；脚本默认第 1 页、每页 20 条，并只输出常用字段。
- `pagination.returnedCount` 只表示当前页实际返回数，禁止将其描述为项目总数。
- 当 `pagination.nextPageRequired` 为 `true` 时，必须继续请求下一页；只有后续页返回数小于 `pageSize`，才能判定遍历结束。
- 声称「全部项目」前，必须汇总每一页的 `returnedCount`，并按 `projectId` 或 `projectCode` 去重校验。
- 禁止根据被平台截断、折叠或人工整理后的 YAML 行数推断项目数量。
- 仅在确实需要完整实体字段时使用 `--full-response`；不得从大段完整响应中手工计数。

返回实体为 `Result<List<ProjectVO>>`，不是分页包装对象。常用字段：

- `projectCode` / `englishName`：项目英文名，后续接口的 `projectId`
- `projectName`：项目中文名
- `enabled` / `offlined`：启用和下线状态
- `creator` / `createdAt`：创建信息
- `managePermission`：是否有项目管理权限
- `approvalStatus` / `approvalMsg`：审批状态与说明

## 获取项目详情

```bash
python scripts/project_client.py get --project-id {projectId}
```

调用：

```text
GET /v4/apigw-user/projects/{projectId}
```

`projectId` 是项目英文名。不要把数据库数字主键 `id` 当作 `projectId`。

返回实体为 `Result<ProjectVO?>`。项目不存在时 `data` 可以为 `null`。

完整 `ProjectVO` 和 `Result<T>` 字段见 [项目 API 实体](project-models.md#projectvo)。成功必须以 `code == 0` 判断，不能只检查 HTTP 状态码或顶层 `result`。

## 鉴权

日常命令无需传 token。缺少鉴权时按 [统一鉴权配置](../auth-setup.md) 处理。
