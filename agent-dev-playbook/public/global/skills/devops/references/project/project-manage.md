# 项目创建与编辑

脚本：`scripts/project_client.py`

创建和编辑都是写操作。执行前必须向用户展示完整请求体和影响，并获得明确确认。

## 创建项目

接口：

```text
POST /v4/apigw-user/projects/project_create
```

命令：

```bash
python scripts/project_client.py create --body-file ./project-create.json
```

实际请求实体为 `ProjectCreateInfo`。根据 Kotlin 构造函数，只有以下字段无默认值：

- `projectName: string`
- `englishName: string`
- `description: string`

其他字段均有默认值，但组织、项目性质、项目类型、运营产品等业务信息不能因此省略或猜测，必须按用户实际情况提供。完整类型和默认值见 [项目 API 实体](project-models.md#projectcreateinfo)。

请求体示例：

```json
{
  "projectName": "示例项目",
  "englishName": "myproject",
  "description": "项目说明"
}
```

示例中的组织和类型字段不能猜测，必须由用户提供或确认。

## 编辑项目

接口：

```text
PUT /v4/apigw-user/projects/{projectId}
```

编辑接口接收完整 `ProjectUpdateInfo`，不是 JSON Merge Patch。必须先查询详情，基于当前值生成完整请求体，避免遗漏字段导致配置被重置。

推荐流程：

1. `get --project-id {projectId}` 获取当前项目。
2. 只修改用户要求的字段，生成完整更新 JSON。
3. 展示新旧差异和完整请求体，等待明确确认。
4. 执行：

```bash
python scripts/project_client.py edit \
  --project-id {projectId} \
  --body-file ./project-edit.json
```

5. 再次执行 `get` 验证结果。

实际请求实体为 `ProjectUpdateInfo`。无默认值的字段为：

- `projectName: string`
- `description: string`
- `ccAppId: integer?`
- `ccAppName: string?`
- `kind: integer?`

后三项允许为 `null`，但建议在完整更新请求中显式提供。`englishName` 有默认值 `""`，并不是实体层面的必填字段；更新路径中的 `projectId` 才是目标项目标识。完整类型和默认值见 [项目 API 实体](project-models.md#projectupdateinfo)。

特别注意：

- 更新实体不包含 `enabled` 和 `projectScope`，不能通过该接口修改。
- 查询返回的组织 ID 是字符串，更新请求中的组织 ID 是整数，不能直接原样复制。
- `logoAddr` 是返回字段，更新请求字段名是 `logoAddress`。

## properties

字段和默认值见 [ProjectProperties](project-models.md#projectproperties)。

当前服务端用户编辑逻辑只复制以下属性：

- `pipelineDialect`
- `enablePipelineNameTips`
- `pipelineNameFormat`
- `loggingLineLimit`
- `enableShareArtifact`

其他属性可能由运营侧控制，不要因为请求被接受就假设已修改。

## subjectScopes

用于限制项目最大可授权人员范围。每项的 `id` 和 `name` 在实体中无默认值；JSON 字段使用 `full_name`，不是 Kotlin 属性名 `fullName`。详见 [SubjectScopeInfo](project-models.md#subjectscopeinfo)。

## 错误处理

- HTTP 401：按 [统一鉴权配置](../auth-setup.md) 更新 token。
- HTTP 403：当前用户没有项目管理权限。
- 参数校验失败：根据 API 返回补齐组织、项目类型或完整更新字段，不要猜测。
- 写操作失败后不要自动重试，重新确认请求体后再执行。
