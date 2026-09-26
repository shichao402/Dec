# 项目 API 实体

本文以 BK-CI 实际 Kotlin 实体为准，不以可能滞后的网关文档为准。核对基线：

- 仓库：`bk-ci-tencent`
- 分支：`hotfix/2026-08-12-11`
- 提交：`3741edb5b00`
- API：`ApigwProjectResourceV4`
- 请求实体：`ProjectCreateInfo`、`ProjectUpdateInfo`
- 返回实体：`ProjectVO`、`Result<T>`

字段名采用实际 JSON 属性名。`?` 表示可为 `null`；带默认值的字段可省略。

## ProjectCreateInfo

无默认值、创建时必须提供：

- `projectName: string`：项目名称
- `englishName: string`：项目英文名
- `description: string`：项目描述

有默认值、可省略：

- `projectType: integer = 0`
- `bgId: integer(int64) = 0`
- `bgName: string = ""`
- `businessLineId: integer(int64)? = null`
- `businessLineName: string? = ""`
- `deptId: integer(int64) = 0`
- `deptName: string = ""`
- `centerId: integer(int64) = 0`
- `centerName: string = ""`
- `secrecy: boolean = false`
- `hidden: boolean = false`
- `projectScope: integer = 0`：`0` 团队项目，`1` 个人项目
- `kind: integer = 0`
- `properties: ProjectProperties? = null`
- `subjectScopes: SubjectScopeInfo[]? = []`
- `logoAddress: string? = null`
- `authSecrecy: integer? = 0`
- `enabled: boolean = true`
- `productId: integer? = null`
- `productName: string? = null`
- `kpiCode: string? = null`
- `kpiName: string? = null`

最小请求：

```json
{
  "projectName": "示例项目",
  "englishName": "myproject",
  "description": "项目说明"
}
```

组织、项目性质和运营产品等字段不能根据默认值擅自推断；实际创建前仍须由用户确认。

## ProjectUpdateInfo

该实体用于完整更新，不是 JSON Merge Patch。

无默认值：

- `projectName: string`
- `description: string`
- `ccAppId: integer(int64)?`
- `ccAppName: string?`
- `kind: integer?`

其中后三项允许为 `null`。调用时应显式保留当前值或传 `null`，不要依赖 JSON 反序列化器对缺失 nullable 参数的兼容行为。

有默认值、可省略：

- `projectType: integer = 0`
- `bgId: integer(int64) = 0`
- `bgName: string = ""`
- `businessLineId: integer(int64)? = null`
- `businessLineName: string? = ""`
- `centerId: integer(int64)? = null`
- `centerName: string? = ""`
- `deptId: integer(int64)? = null`
- `deptName: string? = ""`
- `englishName: string = ""`
- `secrecy: boolean = false`
- `hidden: boolean? = null`
- `properties: ProjectProperties? = null`
- `subjectScopes: SubjectScopeInfo[]? = []`
- `logoAddress: string? = null`
- `authSecrecy: integer? = 0`
- `productId: integer? = null`
- `productName: string? = null`
- `kpiCode: string? = null`
- `kpiName: string? = null`

注意：`ProjectUpdateInfo` 没有 `enabled` 和 `projectScope` 字段，不能通过编辑请求修改它们。

## ProjectProperties

- `pipelineAsCodeSettings: PipelineAsCodeSettings = {"enable": false}`
- `remotedev: boolean? = false`
- `cloudDesktopNum: integer = 0`
- `remotedevManager: string? = null`
- `enableTemplatePermissionManage: boolean? = null`
- `dataTag: string? = null`
- `disableWhenInactive: boolean? = null`
- `buildMetrics: boolean? = null`
- `pipelineListPermissionControl: boolean? = null`
- `pluginDetailsDisplayOrder: string[]? = ["LOG", "ARTIFACT", "CONFIG"]`
- `pipelineDialect: string? = "CLASSIC"`
- `enablePipelineNameTips: boolean? = false`
- `pipelineNameFormat: string? = null`
- `loggingLineLimit: integer? = null`
- `remotedevObserver: boolean? = null`
- `enableShareArtifact: boolean? = true`

`PipelineAsCodeSettings`：

- `enable: boolean = false`
- `projectDialect: string? = null`
- `inheritedDialect: boolean? = true`
- `pipelineDialect: string? = null`

项目服务的用户编辑逻辑目前只复制 `pipelineDialect`、`enablePipelineNameTips`、`pipelineNameFormat`、`loggingLineLimit` 和 `enableShareArtifact`；其他由运营侧控制的属性即使出现在请求中，也不应假设能够被用户编辑接口修改。

## SubjectScopeInfo

- `id: string?`：无默认值
- `name: string`：无默认值
- `type: string? = "user"`
- `full_name: string? = ""`：Kotlin 属性名为 `fullName`，JSON 名为 `full_name`
- `username: string? = ""`

## ProjectVO

查询详情返回 `Result<ProjectVO?>`，列表返回 `Result<ProjectVO[]>`。`ProjectVO` 实际字段：

- 标识：`id: integer(int64)`、`projectId: string`、`projectName: string`、`projectCode: string`、`englishName: string`
- 类型与审批：`projectType: integer?`、`approvalStatus: integer?`、`approvalTime: string?`、`approver: string?`、`approvalMsg: string?`
- CC 与组织：`ccAppId: integer(int64)?`、`ccAppName: string?`、`bgId: string?`、`bgName: string?`、`businessLineId: string?`、`businessLineName: string?`、`centerId: string?`、`centerName: string?`、`deptId: string?`、`deptName: string?`
- 创建与更新：`createdAt: string?`、`creator: string?`、`updatedAt: string?`、`updator: string?`
- 基本信息：`description: string?`、`dataId: integer(int64)?`、`deployType: string?`、`extra: string?`、`kind: integer?`、`logoAddr: string?`、`remark: string?`
- 状态：`offlined: boolean?`、`secrecy: boolean?`、`hidden: boolean?`、`projectScope: integer?`、`helmChartEnabled: boolean?`、`useBk: boolean?`、`enabled: boolean?`、`gray: boolean`
- 构建配置：`hybridCcAppId: integer(int64)?`、`enableExternal: boolean?`、`enableIdc: boolean?`、`pipelineLimit: integer?`
- 路由与关联：`routerTag: string?`、`relationId: string?`、`channelCode: string?`
- 复合配置：`properties: ProjectProperties?`、`subjectScopes: SubjectScopeInfo[]?`
- 权限与展示：`authSecrecy: integer?`、`tipsStatus: integer?`、`managePermission: boolean?`、`showUserManageIcon: boolean?`、`canView: boolean?`、`pipelineTemplateInstallPerm: boolean?`
- 运营产品：`productId: integer?`、`productName: string?`、`kpiCode: string?`、`kpiName: string?`
- 兼容旧字段：`hybrid_cc_app_id`、`project_id`、`project_name`、`project_code`、`cc_app_id`、`cc_app_name`，均已废弃，不应在新逻辑中优先使用

组织 ID 在请求实体中是整数，在 `ProjectVO` 中是字符串；生成编辑请求时必须进行正确的字符串到整数转换，不能原样复制。

## Result<T>

所有四个接口都使用统一包装：

- `code: integer`
- `message: string?`
- `data: T?`
- `request_id: string?`
- `result: boolean?`

成功由 `code == 0` 判断，不应只检查 HTTP 状态码或顶层 `result`。

- 创建：`Result<boolean>`
- 编辑：`Result<boolean>`
- 获取：`Result<ProjectVO?>`
- 列表：`Result<ProjectVO[]>`
