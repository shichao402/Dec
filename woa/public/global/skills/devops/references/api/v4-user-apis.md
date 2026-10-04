# 蓝盾 v4 用户态（apigw-user）OpenAPI 端点字典

调任何蓝盾 v4 用户态 OpenAPI 前必须先在本文件查准确路径。禁止猜接口路径、禁止换常见 RESTful 形态试探、禁止动态发现（不存在该机制）。

- BASE_URL：`https://devops.apigw.o.woa.com/prod/v4/apigw-user`，下表路径为其后缀。
- 鉴权：`X-Bkapi-Authorization` 头（token 获取与续期见 `auth-setup.md` / `scripts/login.py`）。
- 来源：官方 `bkapi-devops` Go SDK（2.1.408）`bkapi.go` 全量提取，与 [APIGW 文档中心](https://bkapigw.woa.com/docs/api-docs/gateway/devops)（按 `?apiName=<接口名>` 逐个核对）一致；文档中心另有应用态 `v4_app_*` 镜像与 v3 存量接口，新代码一律用 v4 用户态。
- 流水线列表标准入口是 `v4_user_pipeline_view_pipelines`（`pipelineView/listViewPipelines`），不是常见 RESTful 形态；必带 query：`viewId`（默认 `allPipeline`）、`sortType`、`collation`。

## 项目

| 接口名 | 方法 | 路径 | 说明 |
|---|---|---|---|
| v4_user_project_create | POST | /projects/project_create | 创建项目(rbac) |
| v4_user_project_edit | PUT | /projects/{projectId} | 修改项目 |
| v4_user_project_get | GET | /projects/{projectId} | 获取项目信息 |
| v4_user_project_list | GET | /projects/project_list | 查询所有项目 |
| v4_user_project_name_validate | GET | /projects/project_name_validation | 校验项目名称和项目英文名 |
| v4_user_project_create_users | POST | /projects/{projectId}/project_user | 添加指定用户到指定项目用户组 |
| v4_user_get_projects_by_product_id | GET | /projects/project/get_projects_by_product_id | 根据运营产品ID获取项目列表 |
| v4_user_update_project_product | PUT | /projects/project/{projectId}/update_project_product | 更新项目关联产品 |

## 流水线：列表与编排

| 接口名 | 方法 | 路径 | 说明 |
|---|---|---|---|
| v4_user_pipeline_list | GET | /projects/{projectId}/pipelines/pipeline_list | 获取项目的流水线列表 |
| v4_user_pipeline_view_pipelines | GET | /projects/{projectId}/pipelineView/listViewPipelines | 获取视图流水线编排列表（列表标准入口） |
| v4_user_pipeline_create | POST | /projects/{projectId}/pipelines/pipeline | 新建流水线编排 |
| v4_user_pipeline_edit | PUT | /projects/{projectId}/pipelines/pipeline | 编辑流水线编排 |
| v4_user_pipeline_delete | DELETE | /projects/{projectId}/pipelines/pipeline | 删除流水线编排 |
| v4_user_pipeline_get | GET | /projects/{projectId}/pipelines/pipeline | 获取流水线编排 |
| v4_user_pipeline_batch_get | POST | /projects/{projectId}/pipelines/pipeline_batch_get | 批量获取流水线编排与配置 |
| v4_user_pipeline_paging_search_by_name | GET | /projects/{projectId}/pipelines/paging_search_by_name | 根据流水线名称搜索（分页） |
| v4_user_pipeline_search_by_name | GET | /projects/{projectId}/pipelines/search_by_name | 根据流水线名称搜索 |
| v4_user_pipeline_copy | POST | /projects/{projectId}/pipelines/pipeline_copying | 复制流水线编排 |
| v4_user_pipeline_rename | POST | /projects/{projectId}/pipelines/pipeline_name | 流水线重命名 |
| v4_user_pipeline_restore | PUT | /projects/{projectId}/pipelines/pipeline_restore | 还原流水线编排 |
| v4_user_pipeline_update | PUT | /projects/{projectId}/pipelines/pipeline_with_setting | 更新流水线编排和设置 |
| v4_user_pipeline_upload | POST | /projects/{projectId}/pipelines/pipeline_with_setting | 导入新流水线（含编排和设置） |
| v4_user_pipeline_lock | POST | /projects/{projectId}/pipelines/pipeline_lock | 启用/禁用流水线（并发设置） |
| v4_user_pipeline_status | GET | /projects/{projectId}/pipelines/pipeline_status | 获取流水线状态 |
| v4_user_pipeline_get_count | GET | /projects/{projectId}/pipelines/pipeline_count | 获取列表页列表相关的数目 |
| v4_user_pipeline_id_info | GET | /permission/move/projects/{projectId}/pipeline_id_list | 获取项目下pipelineId+自增id |
| v4_user_pipeline_visibility_list | GET | /projects/{projectId}/pipelines/visibility | 查询权限代持人公开的流水线 |
| v4_user_archived_pipeline_list | GET | /projects/{projectId}/archived/pipelines/list | 获取已归档流水线列表 |
| v4_user_pipeline_archive | POST | /projects/{projectId}/archived/pipelines/{pipelineId}/data/migrate | 迁移归档流水线数据 |
| v4_user_pipeline_stream_yaml | GET | /projects/{projectId}/pipelines/stream/yaml | 导出流水线yaml(gitci) |

## 流水线：视图 / 分组 / 标签

| 接口名 | 方法 | 路径 | 说明 |
|---|---|---|---|
| v4_user_pipeline_view_create | POST | /projects/{projectId}/pipelineView | 添加视图 |
| v4_user_pipeline_view_delete | DELETE | /projects/{projectId}/pipelineView | 删除视图 |
| v4_user_pipeline_view_get | GET | /projects/{projectId}/pipelineView | 获取视图 |
| v4_user_pipeline_view_update | PUT | /projects/{projectId}/pipelineView | 更改视图 |
| v4_user_pipeline_view_list | GET | /projects/{projectId}/pipelineView/list | 获取视图列表 |
| v4_user_pipeline_group_create | POST | /projects/{projectId}/pipelineGroups/group | 添加分组 |
| v4_user_pipeline_group_delete | DELETE | /projects/{projectId}/pipelineGroups/group | 删除分组 |
| v4_user_pipeline_group_get | GET | /projects/{projectId}/pipelineGroups/group | 获取所有分组信息 |
| v4_user_pipeline_group_update | PUT | /projects/{projectId}/pipelineGroups/group | 更改分组 |
| v4_user_pipeline_label_create | POST | /projects/{projectId}/pipelineGroups/label | 添加标签 |
| v4_user_pipeline_label_delete | DELETE | /projects/{projectId}/pipelineGroups/label | 删除标签 |
| v4_user_pipeline_label_update | PUT | /projects/{projectId}/pipelineGroups/label | 更改标签 |

## 流水线：版本与草稿

| 接口名 | 方法 | 路径 | 说明 |
|---|---|---|---|
| v4_user_pipeline_save_draft | POST | /projects/{projectId}/version/save_draft | 保存或创建流水线编排草稿 |
| v4_user_pipeline_rollback_draft | POST | /projects/{projectId}/version/rollback_draft | 回滚到指定历史版本并覆盖草稿 |
| v4_user_pipeline_release | POST | /projects/{projectId}/version/release_version | 将当前模板发布为正式版本 |
| v4_user_pipeline_release_prefetch | GET | /projects/{projectId}/version/release_prefetch | 发布为正式版本前的预览信息 |
| v4_user_pipeline_get_version | GET | /projects/{projectId}/version/get_version | 获取流水线指定版本的两种编排 |
| v4_user_pipeline_version_list | GET | /projects/{projectId}/version/version_list | 流水线编排版本列表（搜索、分页） |
| v4_user_pipeline_detail | GET | /projects/{projectId}/version/pipeline_detail | 获取流水线信息（含草稿） |
| v4_user_pipeline_preview_code | GET | /projects/{projectId}/version/preview_code | 触发前配置 |
| v4_user_pipeline_operation_log | GET | /projects/{projectId}/version/operation_log | 获取流水线操作日志列表（分页） |
| v4_user_pipeline_creator_list | GET | /projects/{projectId}/version/creator_list | 获取流水线编排创建人列表（分页） |
| v4_user_pipeline_operator_list | GET | /projects/{projectId}/version/operator_list | 获取流水线操作人列表（分页） |
| v4_user_pipeline_create_with_template | POST | /projects/{projectId}/version/create_with_template | 通过指定模板创建流水线 |
| v4_user_pipeline_export | GET | /projects/{projectId}/version/export | 导出流水线模板 |
| v4_user_pipeline_export_all | GET | /projects/{projectId}/version/export_all | 导出项目下所有有编辑权限的流水线 |
| v4_user_pipeline_reset_build_no | POST | /projects/{projectId}/version/reset_build_no | 重置流水线推荐版本号 |

## 构建操作

| 接口名 | 方法 | 路径 | 说明 |
|---|---|---|---|
| v4_user_build_start | POST | /projects/{projectId}/build_start | 启动构建 |
| v4_user_build_stop | POST | /projects/{projectId}/build_stop | 停止构建 |
| v4_user_build_restart | POST | /projects/{projectId}/build_restart | 取消并发起新构建 |
| v4_user_build_retry | POST | /projects/{projectId}/build_retry | 重试构建（重试或跳过失败插件） |
| v4_user_build_detail | GET | /projects/{projectId}/build_detail | 构建详情 |
| v4_user_build_status | GET | /projects/{projectId}/build_status | 查看构建状态信息（含stageStatus） |
| v4_user_batch_get_build_status | POST | /projects/{projectId}/batch_get_build_status | 批量获取构建详情 |
| v4_user_build_list | GET | /projects/{projectId}/build_histories | 获取流水线构建历史 |
| v4_user_build_list_simple | GET | /projects/{projectId}/build_histories/simple | 获取流水线轻量构建历史 |
| v4_user_build_failed_tasks | GET | /projects/{projectId}/build_failed_tasks | 获取指定构建的失败任务信息 |
| v4_user_build_startInfo | GET | /projects/{projectId}/build_manual_startup_info | 获取流水线手动启动参数 |
| v4_user_build_startOptions | POST | /projects/{projectId}/build_manual_startup_options | 获取流水线手动启动分页的参数 |
| v4_user_build_stage_start | POST | /projects/{projectId}/manual_start_build_stage | 手动审核启动阶段 |
| v4_user_build_variables_value | POST | /projects/{projectId}/build_variables | 获取构建中的变量值 |
| v4_user_manual_review | POST | /projects/{projectId}/manual_review | 人工审核插件进行审核 |
| v4_user_pause_build_execute | POST | /projects/{projectId}/build_execute_pause | 操作暂停插件 |
| v4_user_try_fix_stuck_builds | POST | /projects/{projectId}/try_fix_stuck_builds | 尝试修复异常中断的流水线 |
| v4_user_update_remark | POST | /projects/{projectId}/update_remark | 修改某次构建的备注 |
| v4_user_visibility_build_startInfo | GET | /projects/{projectId}/visibility_build_manual_startup_info | 获取可见性流水线手动启动参数 |
| v4_user_imate_build_start | POST | /projects/{projectId}/weMate_build_start | IMate会话提醒启动流水线 |
| v4_user_get_build_report | GET | /pipeline/reports/{projectId}/{pipelineId}/{buildId} | 获取构建下报告列表 |

## 构建日志

| 接口名 | 方法 | 路径 | 说明 |
|---|---|---|---|
| v4_user_log_init | GET | /projects/{projectId}/logs/init_logs | 根据构建ID获取初始化所有日志 |
| v4_user_log_after | GET | /projects/{projectId}/logs/after_line_logs | 获取某行后的日志 |
| v4_user_log_more | GET | /projects/{projectId}/logs/more_logs | 获取更多日志 |
| v4_user_log_line_num | GET | /projects/{projectId}/logs/last_line_num | 获取当前构建的最大行号 |
| v4_user_log_mode | GET | /projects/{projectId}/logs/log_mode | 获取插件的日志状态 |
| v4_user_log_download | GET | /projects/{projectId}/logs/download_logs | 下载日志接口 |
| v4_user_pipeline_webhook_list | GET | /{projectId}/webhook/pipeline_webhook_list | 获取流水线的webhook列表 |
| v4_user_pipeline_webhook_build_log | GET | /{projectId}/webhook/pipeline_webhook_build_log_detail | 获取流水线的webhook构建日志列表 |

## 制品 artifactory

| 接口名 | 方法 | 路径 | 说明 |
|---|---|---|---|
| v4_user_file_task_create | POST | /artifactory/projects/{projectId}/file_task | 创建文件托管任务 |
| v4_user_file_task_status | GET | /artifactory/projects/{projectId}/file_task | 查询文件托管任务状态 |
| v4_user_file_task_clear | DELETE | /artifactory/projects/{projectId}/file_task | 清理文件托管任务 |
| v4_user_artifactory_custom | GET | /projects/{projectId}/artifactories/custom | 查询自定义仓库文件列表 |
| v4_user_artifactory_list | GET | /projects/{projectId}/artifactories/file_info | 根据元数据获取文件 |
| v4_user_artifactory_appDownloadUrl | GET | /projects/{projectId}/artifactories/app_download_url | 获取APP跳转链接 |
| v4_user_artifactory_userDownloadUrl | GET | /projects/{projectId}/artifactories/user_download_url | 获取用户下载链接 |
| v4_user_artifactory_log_download | GET | /projects/{projectId}/artifactories/log | 下载熔断归档的全量日志 |

## 环境与节点

| 接口名 | 方法 | 路径 | 说明 |
|---|---|---|---|
| v4_user_env_list | GET | /environment/projects/{projectId}/envs/list | 获取环境列表 |
| v4_user_env_list_by_env_hashIds | POST | /environment/projects/{projectId}/envHashIds_to_envInfo | 根据hashId(多个)获取环境信息 |
| v4_user_env_list_env_by_env_names | POST | /environment/projects/{projectId}/envNames_to_envInfo | 根据环境名称获取环境信息 |
| v4_user_env_list_nodes_new | GET | /environment/projects/{projectId}/envs/{envHashId}/nodes_list | 获取环境的节点列表 |
| v4_user_env_list_usable_envs | GET | /environment/projects/{projectId}/usable_server_envs | 获取有权限使用的环境列表 |
| v4_user_env_list_usable_nodes | GET | /environment/projects/{projectId}/usable_server_nodes | 获取有权限使用的服务器列表 |
| v4_user_env_node_list_byEnvHashIds | POST | /environment/projects/{projectId}/envHashIds_to_nodes | 根据环境hashId获取节点列表 |
| v4_user_env_node_list_byNodeHashIds | POST | /environment/projects/{projectId}/nodeHashIds_to_nodes | 根据hashId获取项目节点列表 |
| v4_user_env_node_list_ext | GET | /environment/projects/{projectId}/ext_nodes | 获取构建节点信息（扩展接口） |
| v4_user_env_node_list_pipeline_ref | GET | /environment/projects/{projectId}/pipeline_ref_list | 获取构建节点信息（扩展接口） |
| v4_user_env_nodes | POST | /environment/projects/{projectId}/fetch_nodes | 获取项目节点列表 |
| v4_user_enable_env_node | PUT | /environment/projects/{projectId}/enable_env_node | 获取有权限使用的服务器列表 |
| v4_user_set_share_env | POST | /environment/projects/{projectId}/share_envs | 设置环境共享 |
| v4_user_third_party_env2nodes | GET | /environment/projects/{projectId}/third_party_env2nodes | 指定构建环境获取所有节点信息 |
| v4_user_node_list | GET | /projects/{projectId}/environment/third_part_agent_nodes | 获取项目下第三方构建机列表 |
| v4_user_node_status | GET | /projects/{projectId}/environment/third_part_agent_node_status | 获取指定构建机状态 |
| v4_user_node_third_part_agent_envvar | POST | /projects/{projectId}/environment/fetch_agent_env | 批量查询Agent环境变量 |
| v4_user_node_third_part_agent_tags | GET | /projects/{projectId}/environment/fetch_agent_tag | 查询项目标签和对应节点数 |
| v4_user_node_third_part_agent_update_agent_info | POST | /projects/{projectId}/environment/update_agent_info | 批量修改Agent环境变量 |
| v4_user_node_third_part_agent_update_envvar | POST | /projects/{projectId}/environment/batch_update_agent_env | 批量修改Agent环境变量 |
| v4_user_node_third_part_builds | GET | /projects/{projectId}/environment/third_part_agent_builds | 获取第三方构建机任务 |
| v4_user_node_third_part_detail | GET | /projects/{projectId}/environment/third_part_agent_node_detail | 获取指定第三方构建机详情 |

## 权限治理

| 接口名 | 方法 | 路径 | 说明 |
|---|---|---|---|
| v4_user_add_auth_group_members | POST | /auth/project/{projectId}/member_manage/members/add | 批量添加用户组成员 |
| v4_user_analyze_auth_member_permissions | GET | /auth/project/{projectId}/member_insight/members/{memberId}/analysis | 成员权限分析报告 |
| v4_user_apply_auth_group_handover | POST | /auth/project/{projectId}/member_manage/members/apply_handover | 成员自助申请交接用户组 |
| v4_user_apply_auth_member_renewal | POST | /auth/project/{projectId}/member_manage/members/apply_renewal | 普通用户申请续期权限 |
| v4_user_apply_join_auth_group | POST | /auth/project/{projectId}/member_manage/apply_to_join_group | 申请加入用户组 |
| v4_user_auth_member_exit_project | PUT | /auth/project/{projectId}/member_manage/members/exits_project | 用户主动退出项目 |
| v4_user_batch_handover_auth_members | PUT | /auth/project/{projectId}/member_manage/members/batch_handover | 批量交接用户组成员 |
| v4_user_batch_remove_auth_members | DELETE | /auth/project/{projectId}/member_manage/members/batch_remove | 批量移除用户组成员 |
| v4_user_batch_remove_auth_members_from_project | PUT | /auth/project/{projectId}/member_manage/members/batch_remove_from_project | 批量将用户移出项目 |
| v4_user_batch_renewal_auth_members | PUT | /auth/project/{projectId}/member_manage/members/batch_renewal | 批量续期用户组成员 |
| v4_user_check_auth_health | GET | /auth/project/{projectId}/member_insight/authorization/health_check | 项目授权健康检查 |
| v4_user_check_auth_member_exit | GET | /auth/project/{projectId}/member_manage/members/{targetMemberId}/exit_check | 检查成员退出/交接权限并推荐交接人 |
| v4_user_check_auth_member_exit_project | GET | /auth/project/{projectId}/member_manage/members/exits_project_check | 用户主动退出项目检查 |
| v4_user_check_auth_member_operate | POST | /auth/project/{projectId}/member_manage/members/{batchOperateType}/check | 批量操作成员检查 |
| v4_user_check_batch_remove_auth_members | POST | /auth/project/{projectId}/member_manage/members/batch_remove_from_project_check | 批量将用户移出项目检查 |
| v4_user_clone_auth_permissions | POST | /auth/project/{projectId}/member_manage/permissions/clone | 权限克隆 |
| v4_user_compare_auth_permissions | GET | /auth/project/{projectId}/member_insight/compare | 权限对比 |
| v4_user_diagnose_auth_permission | GET | /auth/project/{projectId}/member_insight/diagnose | 权限诊断 |
| v4_user_exit_auth_groups | DELETE | /auth/project/{projectId}/member_manage/members/exit_groups | 成员自助退出用户组 |
| v4_user_get_all_auth_member_groups | GET | /auth/project/{projectId}/member_manage/members/all_groups | 获取成员所有用户组详情 |
| v4_user_get_auth_group_permission_detail | GET | /auth/project/{projectId}/member_manage/groups/{groupId}/permission_detail | 查询用户组权限详情 |
| v4_user_get_auth_member_group_count | GET | /auth/project/{projectId}/member_manage/members/group_count | 获取成员用户组数量 |
| v4_user_get_auth_member_groups_detail | GET | /auth/project/{projectId}/member_manage/members/{resourceType}/groups | 获取成员用户组详情 |
| v4_user_list_auth_actions | GET | /auth/metadata/list_actions | 获取资源类型对应的操作列表 |
| v4_user_list_auth_group_members | GET | /auth/project/{projectId}/member_manage/list_group_members | 查询用户组成员详情列表 |
| v4_user_list_auth_groups | POST | /auth/project/{projectId}/member_manage/list_groups | 查询用户组列表 |
| v4_user_list_auth_project_members | GET | /auth/project/{projectId}/member_manage/list_project_members | 获取项目全体成员 |
| v4_user_list_auth_project_members_by_condition | GET | /auth/project/{projectId}/member_manage/list_project_members_by_condition | 根据条件获取项目全体成员 |
| v4_user_list_auth_resource_types | GET | /auth/metadata/list_resource_types | 获取资源类型列表 |
| v4_user_list_groups_for_apply | GET | /auth/project/{projectId}/member_manage/groups_for_apply | 查询可申请的用户组列表 |
| v4_user_recommend_auth_groups | POST | /auth/project/{projectId}/member_manage/recommend_groups | 智能推荐用户组 |
| v4_user_search_auth_users | GET | /auth/project/{projectId}/member_manage/users/search | 根据关键词搜索用户 |
| v4_user_permission_grant | POST | /auth/projects/{projectId}/instance_grant | 实例授权 |
| v4_user_permission_project_check | GET | /auth/validate/projects/{projectId}/check_project_users | 判断是否某项目中某组角色的成员 |
| v4_user_get_auth_resource_by_code | GET | /auth/metadata/projects/{projectId}/get_resource_by_code | 根据 Code 查询资源 |
| v4_user_get_auth_resource_by_name | GET | /auth/metadata/projects/{projectId}/get_resource_by_name | 根据名称查询资源 |
| v4_user_search_auth_resource | GET | /auth/metadata/projects/{projectId}/search_resource | 根据资源名称或 Code 搜索资源 |
| v4_user_get_auth_resource_permissions_matrix | GET | /auth/project/{projectId}/member_insight/resources/{resourceType}/{resourceCode}/permissions_matrix | 资源权限矩阵 |
| v4_user_get_resource_authorization | GET | /auth/authorization/{projectId}/{resourceType}/{resourceCode}/get_resource_authorization | 获取资源授予记录 |
| v4_user_list_pipeline_authorization | GET | /auth/authorization/{projectId}/pipelines/list_authorization | 获取流水线代持人列表 |
| v4_user_reset_pipeline_authorization | PUT | /auth/authorization/{projectId}/pipelines/{pipelineId}/reset_authorization | 重置流水线授权人 |

## 凭据

| 接口名 | 方法 | 路径 | 说明 |
|---|---|---|---|
| v4_user_credential_create | POST | /projects/{projectId}/credentials/credential | 新增凭据 |
| v4_user_credential_delete | DELETE | /projects/{projectId}/credentials/credential | 删除凭据 |
| v4_user_credential_edit | PUT | /projects/{projectId}/credentials/credential | 编辑凭据 |
| v4_user_credential_get | GET | /projects/{projectId}/credentials/credential | 获取凭据 |
| v4_user_credential_list | GET | /projects/{projectId}/credentials/credential_list | 获取有权限凭据列表 |

## 代码库

| 接口名 | 方法 | 路径 | 说明 |
|---|---|---|---|
| v4_user_repository_create | POST | /repositories/projects/{projectId}/repository | 关联代码库 |
| v4_user_repository_delete | DELETE | /repositories/projects/{projectId}/repository | 删除代码库 |
| v4_user_repository_edit | PUT | /repositories/projects/{projectId}/repository | 编辑关联代码库 |
| v4_user_repository_get | GET | /repositories/projects/{projectId}/repository/{repositoryId} | 获取代码库 |
| v4_user_repository_list | GET | /repositories/projects/{projectId}/repository_info_list | 代码库列表 |
| v4_user_repository_commit_list | GET | /projects/{projectId}/repositoryCommit/commit_data_list | 获取代码提交记录 |
| v4_user_repository_enable_pac | PUT | /repositories/projects/{projectId}/{repositoryHashId}/pac/enable | 开启代码库PAC |
| v4_user_repository_disable_pac | PUT | /repositories/projects/{projectId}/{repositoryHashId}/pac/disable | 关闭代码库PAC |
| v4_user_oauth_isOauth | GET | /repositories/oauth/isOauth | 校验用户是否已经OAUTH授权 |

## 模板

| 接口名 | 方法 | 路径 | 说明 |
|---|---|---|---|
| v4_user_template_create | POST | /projects/{projectId}/templates | 创建流水线模板 |
| v4_user_template_delete | DELETE | /projects/{projectId}/templates | 删除流水线模板 |
| v4_user_template_get | GET | /projects/{projectId}/templates/template_detail | 获取流水线模板详情 |
| v4_user_template_list | GET | /projects/{projectId}/templates | 模版管理-获取模版列表 |
| v4_user_template_update | PUT | /projects/{projectId}/templates | 更新流水线模板 |
| v4_user_template_version_delete | DELETE | /projects/{projectId}/templates/template_version | 删除流水线模板的版本 |
| v4_user_templateInstance_create | POST | /projects/{projectId}/templates/templateInstances | 批量实例化流水线模板 |
| v4_user_templateInstance_get | GET | /projects/{projectId}/templates/templateInstances | 获取流水线模板的实例列表 |
| v4_user_templateInstance_update | PUT | /projects/{projectId}/templates/templateInstances | 批量更新流水线模板实例 |
| v4_user_templateInstance_update_versionName | PUT | /projects/{projectId}/templates/templateInstances/update | 批量更新流水线模板实例 |
| v4_user_template_install | POST | /market/template_install_from_store | 安装研发商店模板到项目 |
| v4_user_template_install_new | POST | /market/template_install_from_store_new | 安装研发商店模板到项目（返回模板Id） |

## 回调

| 接口名 | 方法 | 路径 | 说明 |
|---|---|---|---|
| v4_user_callback_create | POST | /projects/{projectId}/callbacks | 创建callback回调（支持Stage事件） |
| v4_user_callback_batch_create | POST | /projects/{projectId}/callbacks/batch | 批量创建callback回调 |
| v4_user_callback_delete | DELETE | /projects/{projectId}/callbacks/callback | callback回调移除 |
| v4_user_callback_history_list | GET | /projects/{projectId}/callbacks/callback_history | callback回调执行历史记录 |
| v4_user_callback_history_retry | POST | /projects/{projectId}/callbacks/callback_retry | callback回调重试 |
| v4_user_callback_list | GET | /projects/{projectId}/callbacks/callback_list | callback回调列表 |

## 质量红线

| 接口名 | 方法 | 路径 | 说明 |
|---|---|---|---|
| v4_user_quality_rule_create | POST | /projects/{projectId}/quality/rule | 创建拦截规则 |
| v4_user_quality_rule_delete | DELETE | /projects/{projectId}/quality/rule | 删除拦截规则列表 |
| v4_user_quality_rule_list | GET | /projects/{projectId}/quality/rule | 获取拦截规则列表 |
| v4_user_quality_rule_update | PUT | /projects/{projectId}/quality/rule | 更新拦截规则列表 |
| v4_user_quality_intercepts_list | GET | /projects/{projectId}/quality/intercept_list | 获取拦截记录 |
| v4_user_quality_rule_build_history | GET | /projects/{projectId}/quality/rule_build_history_list | 获取Stream红线列表 |

## 插件

| 接口名 | 方法 | 路径 | 说明 |
|---|---|---|---|
| v4_user_atom_detail | GET | /atoms/atom_detail | 根据插件代码获取插件详细信息 |
| v4_user_atom_get | GET | /atoms/atom_info | 根据插件代码获取插件详细信息 |
| v4_user_atom_install | POST | /atoms/install_atom | 安装插件到项目 |
| v4_user_atom_list_all | GET | /atoms/atom_list | 获取所有流水线插件信息 |
| v4_user_atom_pipeline_list | GET | /atoms/atom_pipelines | 根据插件代码获取使用的流水线详情 |
| v4_user_atom_search | GET | /atoms/atom_search | 搜索插件信息 |
| v4_user_atom_statistic | GET | /atoms/atom_statistic | 根据插件代码获取插件统计信息 |
| v4_user_atom_yml_v2_detail | GET | /atoms/atom_yml_v2_detail | 查看插件的yml 2.0信息 |

## 签名

| 接口名 | 方法 | 路径 | 说明 |
|---|---|---|---|
| v4_user_sign_detail | GET | /sign/ipa/sign_detail | 签名任务详情 |
| v4_user_sign_downloadUrl | GET | /sign/ipa/sign_download_url | 获取签后IPA文件下载路径 |
| v4_user_sign_history_list | GET | /sign/ipa/sign_history_list | 获取签名任务历史 |
| v4_user_sign_status | GET | /sign/ipa/sign_status | 查看签名任务状态信息 |
| v4_user_sign_token | GET | /sign/ipa/projects/{projectId}/sign_token | 获取签名接口token |

## 编译加速

| 接口名 | 方法 | 路径 | 说明 |
|---|---|---|---|
| v4_user_turbo_new_plan | GET | /turbo/projectId/{projectId}/turbo_plan_detail | 新版编译加速获取方案详情 |
| v4_user_turbo_new_plan_list | GET | /turbo/projectId/{projectId}/turbo_plan_list | 新版编译加速获取加速方案列表 |
| v4_user_turbo_new_record_list | POST | /turbo/projectId/{projectId}/history_list | 新版编译加速获取加速历史列表 |

## Stream（工蜂 CI，同 apigw-user 车道）

| 接口名 | 方法 | 路径 | 说明 |
|---|---|---|---|
| v4_stream_user_builds_detail | GET | /stream/gitProjects/{gitProjectId}/build_detail | 查看项目下的指定构建详情 |
| v4_stream_user_builds_history | GET | /stream/gitProjects/{gitProjectId}/build_history | 可选条件检索Stream构建历史 |
| v4_stream_user_check_yaml | GET | /stream/check_yaml | yaml schema check |
| v4_stream_user_ci_enable | GET | /stream/gitProjects/{gitProjectId}/ci_enable | 查询工蜂项目CI开启状态 |
| v4_stream_user_reset_oauth | DELETE | /stream/gitProjects/{gitProjectId}/reset_oauth | 重置OAUTH授权 |
| v4_stream_user_setting_get | GET | /stream/gitProjects/{gitProjectId}/setting_get | 获取Stream设置 |
| v4_stream_user_setting_save | POST | /stream/gitProjects/{gitProjectId}/setting_save | 保存Stream设置 |
| v4_stream_user_name_to_pipelineInfo | GET | /stream/gitProjects/{projectId}/name_to_pipelineInfo | 流水线名称转流水线信息 |
| v4_stream_user_openapi_trigger | POST | /stream/gitProjects/{projectId}/openapi_trigger | openapi触发流水线 |
| v4_stream_user_pipeline_enable | PUT | /stream/gitProjects/{gitProjectId}/pipeline_enable | 启用/禁用流水线 |
| v4_stream_user_pipeline_info | GET | /stream/gitProjects/{gitProjectId}/pipeline_info | 获取流水线信息 |
| v4_stream_user_pipeline_list | GET | /stream/gitProjects/{gitProjectId}/pipeline_list | 获取流水线列表 |
| v4_stream_user_pipeline_listInfo | GET | /stream/gitProjects/{gitProjectId}/pipeline_listInfo | 获取流水线列表信息 |
| v4_stream_user_manual | POST | /stream/gitProjects/{projectId}/manual | 手动触发 |
| v4_stream_user_pipeline_startup | POST | /stream/gitProjects/{gitProjectId}/pipeline_startup | 启动流水线 |
| v4_stream_user_project_validate | GET | /stream/gitProjects/project_validate | 校验工蜂项目 |
| v4_stream_user_stream_list | GET | /stream/gitProjects/stream_list | 获取Stream项目列表 |
| v4_stream_user_projectName_transfer | GET | /stream/gitProjects/{gitProjectId}/projectName_transfer | 项目名转换 |

## 商店 / MCP / 其他

| 接口名 | 方法 | 路径 | 说明 |
|---|---|---|---|
| v4_user_market_event_start | POST | /market/event/projects/{projectId}/pipelines/{pipelineId}/{eventCode}/start | 通过触发器插件启动流水线 |
| v4_user_cds_webhook | POST | /market/event/{eventCode}/webhook/cds | 云桌面webhook事件推送 |
| v4_user_mcp_store_atom_add | POST | /mcp/store/market/desk/atom | 新增插件 |
| v4_user_mcp_store_atom_list | GET | /mcp/store/desk/atom/list | 获取有权限的插件列表 |
| v4_user_mcp_store_atom_test | PUT | /mcp/store/market/desk/atom/test | 插件测试版本升级 |
| v4_user_metrics_summary | GET | /metrics/projectId/{projectId}/summary | 获取看板 summary 数据 |
| v4_user_sg_project_workspace | GET | /v4/apigw/remotedev/project/workspace_sg | 提供获取云桌面信息（注意：此路径不在 apigw-user 下） |
| v4_user_ticket_validate | GET | /remotedev/ticket/validate | 云桌面校验用户登录是否有效 |

## 查证入口

本文件未覆盖或参数不确定时，按顺序查证：

1. [APIGW 文档中心](https://bkapigw.woa.com/docs/api-docs/gateway/devops)：`?apiName=<接口名>` 直达单个接口文档（路径、query、返回样例）。
2. [蓝盾 OpenAPI 入口说明（iWiki）](https://iwiki.woa.com/p/15108064)：全部 API 收口在 APIGW。
3. 应用态（`v4_app_*`）与 v3 存量接口同样在文档中心检索。
