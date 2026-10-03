# 查看构建状态

命令：`python scripts/pipeline_client.py status`

查看指定构建的详细状态信息，包括各阶段状态。

## 参数

### 必需参数

| 参数 | 说明 |
|------|------|
| --project-id | 项目英文名 |
| --pipeline-id | 流水线 ID（p-开头） |
| --build-id | 构建 ID（b-开头） |

### 可选参数

| 参数 | 说明 |
|------|------|
| --access-token | 可选；仅当用户本轮对话提供 token 时传入；日常由统一鉴权读取 |

## 返回信息

- 构建整体状态（status）
- 各 Stage 状态（stageStatus）
- 执行时间（executeTime, totalTime）
- 错误信息（如有，errorInfoList）
- 构建产物（artifactList）
- 构建参数（buildParameters）
- 代码库信息（material）

## 示例

### 基本用法

```bash
python scripts/pipeline_client.py status \
  --project-id myproject \
  --pipeline-id p-abc123 \
  --build-id b-xyz789
```

## 使用场景

1. **监控构建进度**：启动构建后轮询状态
2. **排查失败原因**：查看失败构建的详细信息
3. **确认构建完成**：等待构建结束后获取最终状态

## 状态枚举

| 状态 | 说明 |
|------|------|
| SUCCEED | 成功 |
| FAILED | 失败 |
| CANCELED | 已取消 |
| RUNNING | 运行中 |
| QUEUE | 排队中 |
| STAGE_SUCCESS | 阶段成功 |

## 轮询脚本示例

```bash
#!/bin/bash
PROJECT_ID="myproject"
PIPELINE_ID="p-abc123"
BUILD_ID="b-xyz789"

while true; do
  result=$(python scripts/pipeline_client.py status \
    --project-id $PROJECT_ID \
    --pipeline-id $PIPELINE_ID \
    --build-id $BUILD_ID)
  
  status=$(echo $result | python -c "import sys,json; print(json.load(sys.stdin)['data'][''])")
  echo "当前状态: $status"
  
  if [[ "$status" == "SUCCEED" ]]; then
    echo "构建成功！"
    break
  elif [[ "$status" == "FAILED" ]]; then
    echo "构建失败！"
    break
  elif [[ "$status" == "CANCELED" ]]; then
    echo "构建已取消！"
    break
  fi
  
  sleep 10
done
```
