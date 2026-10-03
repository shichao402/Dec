---
name: gongfeng
description: >
  通过工蜂（Gongfeng）MCP 操作代码仓库、分支、提交、文件、合并请求、
  Issue、代码审查和 SVN 数据。用户提到工蜂、git.woa.com、MR、CR、
  Issue、工蜂仓库或工蜂 SVN 时使用。
---

# 工蜂 MCP

## 来源

- 上游仓库：`https://git.woa.com/tgit/tgit-mcp-server.git`
- 提炼基线：`738c3845d89521d44e335b26c200ac3789b9b839`
- MCP：`https://mcpgw.knot.woa.com/gongfeng`

## 调用规则

1. 优先使用宿主直接暴露的 `gongfeng` MCP 工具。
2. 调用前先查看实际工具 schema，不根据示例猜参数。
3. 若宿主没有直接暴露工具，才使用 `mcporter-internal` 调用已配置的
   `gongfeng` 服务。
4. 查询操作可主动执行；创建、更新、关闭、评论、合并等写操作必须符合用户当前请求。
5. 不自行改用裸 HTTP API；只有 MCP 明确不可用且用户仍要求继续时，才说明降级方案。
6. 不在仓库、日志、Skill 或 MCP 配置中写入 token。统一认证由 MCP 网关和宿主完成。

## 项目标识

`project_id` 可使用数字 ID 或完整路径，例如：

```text
tgit/tgit-mcp-server
osgame-client/SvnMergeTool
```

注意区分：

- `id`：全局唯一 ID。
- `iid`：项目内用户可见编号。
- Issue 详情通常接收 `issue_iid`。
- Issue 评论相关接口通常接收全局 `issue_id`；先读详情取得它。
- `review` 是代码审查，`merge_request` / `mr` 是合并请求，不要混用 ID。

## 常用能力

### 项目和代码

- 搜索项目：`search_projects`
- 项目详情：`get_project_detail`
- 搜索代码：`search_project_code`
- 仓库树：`get_repository_tree`
- 文件内容：`get_blob_content`、`get_file_base64_content`
- 文件 blame：`get_file_blame`

### 提交、分支和标签

- 提交列表/详情：`get_commits_list`、`get_commit_info`
- 提交引用/diff：`get_commit_refs`、`get_commit_diff`
- 分支列表/设置：`get_branch_list`、`get_branch_settings`
- 标签：`get_tag_list`
- 比较：`compare`

### 合并请求和审查

- 查询 MR：`search_merge_request`、`search_merge_request_by_user`
- MR 变更/评论：`get_merge_request_changes`、`search_merge_request_notes`
- 创建/更新 MR：根据 MCP schema 选择 `create_merge_request`、`update_merge_request`
- 评审结果：`get_review_by_merge_request_iid`、`get_review_intelligent_result`
- 评审规则：`get_review_rule_config`、`get_review_rules_by_files`

### Issue

- 搜索：`search_project_issues`
- 详情：`get_issue_detail`
- 评论：`get_issue_notes`、`create_issue_note`、`update_issue_note`
- 创建：`create_issue`
- 更新、关闭、重开：`update_issue`

典型流程：

1. 用 `search_project_issues(project_id, iid=...)` 或 `get_issue_detail` 获取详情。
2. 需要评论时，从详情确认全局 `issue_id`。
3. 用 `create_issue_note` 写评论。
4. 关闭或重开用 `update_issue` 的 `state_event`。

### SVN

- 仓库树：`get_svn_repository_tree`
- 提交历史：`get_svn_commits`
- 文件差异：`get_svn_diff_files`

## 输出与安全

- 返回给用户时给出可点击的工蜂链接，并明确项目路径和 IID。
- 写操作后重新读取目标对象，验证状态或评论确实生效。
- 不输出认证头、token、完整凭据文件或可能含凭据的 URL。
- 遇到认证失败时报告认证状态，不尝试从应用私有凭据中解密 token。
