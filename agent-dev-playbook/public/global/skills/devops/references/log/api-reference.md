# 蓝盾日志查询与插件编排 API 参考

本文档记录统一 `devops` Skill 日志模块涉及的蓝盾（BK-CI）API 接口、参数及返回格式，供按需查阅。

所有接口均通过 Header 携带鉴权信息：

```
X-Bkapi-Authorization: {"access_token":"xxx"}
```

`access_token` 由[统一鉴权配置](../auth-setup.md)读取。

---

## 1. 日志下载接口

- **接口地址**: `https://devops.apigw.o.woa.com/prod/v4/apigw-user/projects/{projectId}/logs/download_logs`
- **请求方法**: GET
- **返回格式**: `application/octet-stream`（二进制流）

当日志过大触发熔断时，返回内容末尾会包含 `【Please download logs to view.】` 标记，此时需通过下方接口 2 获取完整日志。

---

## 2. 完整日志 URL 获取接口（熔断处理）

- **接口地址**: `https://devops.apigw.o.woa.com/prod/v4/apigw-user/projects/{projectId}/artifactories/log`
- **请求方法**: GET
- **请求参数**:
  - `pipelineId`: 流水线 ID
  - `buildId`: 构建 ID
  - `elementId`: 步骤/插件 ID（即 tag）
  - `executeCount`: 执行次数（可选）
- **返回格式**:

```json
{
  "data": {
    "url": "https://xxx/xxx.zip"
  },
  "status": 0
}
```

拿到 `data.url` 后下载 zip 包并解压即可获得完整日志。

---

## 3. 构建详情接口（插件编排查询）

- **接口地址**: `https://devops.apigw.o.woa.com/prod/v4/apigw-user/projects/{projectId}/build_detail`
- **请求方法**: GET
- **请求参数**:
  - `pipelineId`: 流水线 ID
  - `buildId`: 构建 ID
  - `archiveFlag`: 是否查询归档数据（可选）
  - `executeCount`: 执行次数（可选）
- **返回格式**:

```json
{
  "status": 0,
  "data": {
    "id": "b-xxxxxxxx",
    "pipelineId": "p-xxxxxxxx",
    "pipelineName": "流水线名称",
    "status": "SUCCEED",
    "model": {
      "name": "流水线名称",
      "stages": [
        {
          "id": "stage-1",
          "name": "stage-1",
          "status": "SUCCEED",
          "containers": [
            {
              "@type": "vmBuild",
              "id": "1",
              "name": "构建环境",
              "jobId": "job_xxx",
              "elements": [
                {
                  "@type": "linuxScript",
                  "id": "e-xxxxxxxx",
                  "name": "Bash",
                  "atomCode": "linuxScript"
                }
              ]
            }
          ]
        }
      ]
    }
  }
}
```

关键字段说明：

| 字段 | 说明 |
|------|------|
| `data.model.stages` | Stage 列表 |
| `stages[].containers` | Job 列表，`@type` 标识 Job 类型（trigger/vmBuild 等） |
| `containers[].elements` | 插件列表，`atomCode` 为插件标识，`id`（e-开头）即下载步骤日志所需的 tag |

---

## 常见 HTTP 错误码排查

| 错误码 | 含义 | 排查建议 |
|--------|------|----------|
| 401 | 未授权 | 检查并更新[统一鉴权配置](../auth-setup.md) |
| 403 | 无权限 | 当前用户无该项目/流水线的访问权限，确认账号权限或更换有权限的令牌 |
| 404 | 资源不存在 | `projectId` / `pipelineId` / `buildId` 填写错误，核对 ID 格式与取值 |
