# 取消流水线构建

命令：`python scripts/pipeline_client.py stop`

别名：`python scripts/pipeline_client.py cancel`

取消指定构建，底层调用 API：`POST /projects/{projectId}/build_stop?pipelineId={pipelineId}&buildId={buildId}`，请求体固定为 `{}`。

## ⚠️ 重要：必须先获得用户确认

**在调用此命令前，必须向用户展示待取消的构建信息并获得明确确认。未经确认禁止执行。**

确认内容必须包括：
- 项目 ID（projectId）
- 流水线 ID（pipelineId）
- 构建 ID（buildId）

## 参数

### 必需参数

| 参数 | 说明 |
|------|------|
| --project-id | 项目英文名 |
| --pipeline-id | 流水线 ID（p-开头） |
| --build-id | 构建 ID（b-开头） |

### 可选参数

| 参数 | 类型 | 说明 |
|------|------|------|
| --access-token | string | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |

## 工作流

```
1. 使用 list 或 status 确认目标构建仍在运行中或排队中
2. ⚠️ 向用户展示待取消的 projectId、pipelineId、buildId，等待用户确认
3. 用户确认后调用 stop 或 cancel 取消构建
4. 再次调用 status，确认状态是否变为 CANCELED
```

## 示例

### 取消运行中的构建

```bash
python scripts/pipeline_client.py stop \
  --project-id myproject \
  --pipeline-id p-abc123 \
  --build-id b-xyz789
```

### 使用别名取消构建

```bash
python scripts/pipeline_client.py cancel \
  --project-id myproject \
  --pipeline-id p-abc123 \
  --build-id b-xyz789
```

## 返回说明

成功取消后通常返回接口标准响应，例如：

```json
{
  "status": 0,
  "message": "",
  "data": true
}
```

## 注意事项

- 通常仅对 `RUNNING` 或 `QUEUE` 状态的构建有意义
- 已结束构建再次取消，接口可能返回失败或无效操作提示
- 建议取消后立刻调用 `status` 复核最终状态
