<!-- 本文件由 `dec pull` 从 .dec/cache/vikunja/ 渲染生成，请勿直接编辑。
     修改流程：编辑 .dec/cache/vikunja/... → dec push → dec pull 验证 -->

---
name: vikunja-project-bootstrap
description: >
  Bootstrap or normalize a single Vikunja project with a reusable issue-intake and delivery structure.
  Use when an AI agent needs to create or standardize project views, buckets, labels, and optional saved filters
  without baking one repository or one project name into the workflow.
---

# Vikunja Project Bootstrap

Use this skill when the main objective is to create, normalize, or migrate the working structure of one Vikunja project.

## Scope Boundary

- operate on one explicitly confirmed Vikunja project at a time
- this skill defines project-local structure, not a global inbox across unrelated projects
- if the team wants a true cross-project intake, create a dedicated intake project deliberately instead of silently mixing normal project issues together

## Primary Goal

Make one Vikunja project usable for issue capture, triage, research, decision, and delivery without forcing premature implementation.

## Variable Policy

- in Dec, keep only truly project-local defaults as placeholders, such as the default target project and the task docs directory
- stable process buckets, type labels, and baseline views should stay fixed in the shared asset
- once the user confirms local defaults, write them into `.dec/vars.yaml` so `dec pull` can materialize the project-specific installed skill text

## Default Structure

Unless the user asks for another shape, recommend this baseline:

### Views

- `List` for sorting, batch review, and search
- `Kanban` for stage-based flow
- optional archive or read-only views only when there is a concrete need

### Buckets

Use buckets for process stage, not work type.

- `待分诊`: newly captured, not normalized yet
- `待补充`: problem is plausible but evidence, scope, reproduction, or ownership is incomplete
- `待研判`: the problem is clear enough for analysis, tradeoff discussion, or design selection
- `待排期`: direction is accepted and waiting for execution
- `执行中`: currently being advanced
- `阻塞`: blocked by dependency, missing decision, or external condition

Use Vikunja's `done` state for completion by default.
Do not create an extra done bucket unless the team explicitly needs a separate verification lane.

### Labels

Use labels for item type, not process stage.

Recommended minimal set:

- `type:bug`
- `type:feature`
- `type:improvement`
- `type:research`
- `type:decision`
- `type:follow-up`

Optional additions such as `type:chore` are acceptable when the team already uses them consistently.

Do not add source labels like `source:user` or `source:dev` by default.
Source classification is often unstable and should usually stay in the description, comments, or reporter context unless the team has a stable rule.

### Saved Filters

Saved filters are optional human-facing shortcuts, not a replacement for project selection.
Agents can query Vikunja directly through MCP, so do not create saved filters unless the user wants them.
If you do create them, every saved filter should explicitly limit results to the confirmed project.

Recommended baseline filters:

- `Intake`: current project, not done, in `待分诊`, `待补充`, or `待研判`
- `Needs Decision`: current project, not done, labeled `type:decision`
- `Needs Research`: current project, not done, labeled `type:research`
- `Ready / Active`: current project, not done, in `待排期` or `执行中`
- `Blocked`: current project, not done, in `阻塞`

## Build Order

1. Confirm the exact target project name or ID.
2. Resolve the project ID and inspect existing views, buckets, labels, and saved filters.
3. Reuse or rename compatible existing structures before creating duplicates.
4. Create the minimum view set.
5. Create or normalize buckets.
6. Create missing type labels.
7. Create saved filters scoped to that project only if the user wants them.
8. Report the resulting structure and any intentional deviations.

## Migration Safety

- do not rename or delete buckets blindly when they already contain tasks
- when normalizing an old board, map old buckets to new buckets before moving cards
- if bulk updates appear to accept `bucket_id`, do not assume that means the kanban move is reflected correctly in board view
- use the dedicated project/view/bucket task-move endpoint for real kanban card migration
- if the existing project already has a strong local convention, preserve it unless the user asked to standardize it

## Gradual Adoption

For an existing project, prefer incremental rollout:

- first rollout: list view, kanban view, and baseline buckets
- second rollout: type labels and neutral intake handling
- third rollout: saved filters and any project-local defaults

This keeps the structure reusable without forcing every team to adopt the full scheme at once.

## Output Expectations

When finishing, state:

- which Vikunja project was normalized, with name and ID
- which views, buckets, labels, and saved filters were created, reused, or skipped
- any risky legacy structure that was intentionally left in place
- any follow-up migration work that should be done separately
- whether repo-local defaults were captured into `.dec/vars.yaml` for later asset reuse

## Anti-Patterns

- do not build one shared inbox for unrelated projects unless the user explicitly asks for a dedicated intake project
- do not use buckets to encode bug vs feature vs research
- do not duplicate priority as both field and label
- do not introduce source labels by default when the team cannot classify them consistently
- do not assume bulk task updates are a safe replacement for real kanban bucket moves
