<!-- 本文件由 `dec pull` 从 .dec/cache/vikunja/ 渲染生成，请勿直接编辑。
     修改流程：编辑 .dec/cache/vikunja/... → dec push → dec pull 验证 -->

---
name: vikunja-project-bootstrap
description: >
  Bootstrap or normalize a single Vikunja project with a reusable issue-intake and delivery structure.
  Use when an AI agent needs to create or standardize project views, buckets, and labels
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
If the project needs a visual done lane, make it a proper done bucket instead of inventing a second completion label system.

### Labels

Use labels for item type, not process stage.

Recommended minimal set:

- `type:epic`
- `type:bug`
- `type:feature`
- `type:improvement`
- `type:research`
- `type:decision`
- `type:follow-up`

`type:epic` marks a long-running goal or persistent theme that spans many regular cards.
Normal cards are attached to an Epic through the Vikunja `subtask` relation, not through a separate bucket or view.
An Epic is a regular task with the `type:epic` label; it is mutually exclusive with the other `type:*` labels on the same card.

Optional additions such as `type:chore` are acceptable when the team already uses them consistently.

Do not add source labels like `source:user` or `source:dev` by default.
Source classification is often unstable and should usually stay in the description, comments, or reporter context unless the team has a stable rule.

## Build Order

1. Confirm the exact target project name or ID.
2. Resolve the project ID and inspect existing views, buckets, and labels.
3. Reuse or rename obviously compatible existing structures before creating duplicates.
4. Create the minimum view set.
5. Normalize buckets to the canonical process model.
6. Create missing canonical type labels, including `type:epic`.
7. Report the resulting structure and any intentional deviations.

## Migration Safety

- do not rename or delete buckets blindly when they already contain tasks
- when normalizing an old board, map old buckets to new buckets before moving cards
- if bulk updates appear to accept `bucket_id`, do not assume that means the kanban move is reflected correctly in board view
- use the dedicated project/view/bucket task-move endpoint for real kanban card migration
- do not preserve non-canonical labels or bucket semantics just for long-term compatibility; if migration is risky, stop and report the remaining cleanup instead of baking that ambiguity into the target structure

## Normalization Principle

The goal of this skill is a canonical structure, not indefinite mixed-mode compatibility.

- if the project can be normalized safely in one round, do it directly
- if migration is risky because buckets contain active work or the mapping is unclear, stop and report the exact follow-up migration work instead of leaving the shared shape ambiguous
- do not encode old ready labels, plain type labels, or duplicate completion semantics into the resulting standard

## Output Expectations

When finishing, state:

- which Vikunja project was normalized, with name and ID
- which views, buckets, and labels were created, reused, or skipped
- any non-canonical structure that still needs a dedicated cleanup round
- any follow-up migration work that should be done separately
- whether repo-local defaults were captured into `.dec/vars.yaml` for later asset reuse

## Anti-Patterns

- do not build one shared inbox for unrelated projects unless the user explicitly asks for a dedicated intake project
- do not use buckets to encode bug vs feature vs research
- do not duplicate priority as both field and label
- do not introduce source labels by default when the team cannot classify them consistently
- do not assume bulk task updates are a safe replacement for real kanban bucket moves
- do not keep plain `bug` / `feature` / `improvement` labels or extra completion semantics as the long-term target structure
- do not implement Epic as its own bucket, column, or view; Epic is only a `type:epic` label plus `subtask` relations, and introducing an "Epic bucket" will break the canonical process-stage meaning of buckets
