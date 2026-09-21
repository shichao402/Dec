---
name: dec-extract-to-playbook
description: >
  把当前项目里已验证的能力直写沉淀进 AgentDevPlaybook 源仓 DecAssets，
  由 CI 发 Dec registry。仅在提供方 access=direct 且本机已登记该源仓时使用。
---

# 直写沉淀到 AgentDevPlaybook

默认目标提供方是 `agent-dev-playbook`。它与 `relkit` 同级：作者源是本仓
`DecAssets/`，分发面是 registry。personal 私仓只记 `access: direct`，不存放正文。

## 何时使用

1. 用户要把本地 Skill / Rule / MCP 抽进 playbook 给别的项目复用
2. `dec_list_provider_access` 显示该提供方 `access=direct` 且有 `AuthorRoot`

不要用 `dec_push`、不要写 `~/.dec/cache`、不要把正文推进 `dec-source-private`。

## 步骤

1. 确认来源目录、资产名、类型（默认 skill）。缺信息先问。
2. `dec_list_provider_access`：
   - `direct` 且有受管源仓：继续
   - `propose` 或没有源仓：改走官方 `dec-extract-asset` 的覆写 + `dec_propose_upstream`
3. 抽象：去掉仓库名、业务实体、密钥；跨项目会变的值用 `{{VAR_NAME}}`
4. 写入 `<AuthorRoot>/DecAssets/<type 目录>/<name>/`（skill 必须有 `SKILL.md`）
5. 在源仓 `.dec/config.yaml` 登记 `provides`
6. 提交并 push 源仓 `main`。CI `dec-publish.yml` 会 `publish-provides`
7. 消费仓 `requires.agent-dev-playbook: latest`，再 `dec_pull`

个人私密笔记才写 personal vault / cache。

## 禁止

- 把官方 `provides` 路径 `dec_push` 进私仓
- 只改 `.dec/cache/` 当作者源
- 把 playbook pin 成 `vault`
