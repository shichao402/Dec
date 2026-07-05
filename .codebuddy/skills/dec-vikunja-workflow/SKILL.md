<!-- 本文件由 `dec pull` 从 .dec/cache/vikunja/ 渲染生成，请勿直接编辑。
     修改流程：编辑 .dec/cache/vikunja/... → dec push → dec pull 验证 -->

---
name: vikunja-workflow
description: >
  Execute a complete Vikunja-backed project advancement workflow.
  Use when an AI coding agent needs to pick the next task from the correct Vikunja project,
  create a task plan, implement, verify, update docs, and close the loop safely.
  Always confirm the target project first; Vikunja tokens are usually not project-scoped.
  This skill is for advancing work inside a chosen project, not for filing standalone issues into arbitrary projects.
---

# Vikunja Workflow

Use this skill to advance software work through a Vikunja backlog with a repeatable lifecycle instead of ad hoc local work.

## Scope Boundary

This skill is for continuing delivery work inside one already-chosen Vikunja project for the current round.

- use this skill when the job is to choose the next task, implement it, verify it, and close it within that same confirmed project
- do not use this skill when the main goal is only to file a new issue into some project
- for cross-project issue filing or early triage, use a dedicated issue-creation or issue-triage skill so project selection and dedup checks stay explicit
- if the project structure itself must be created or normalized, use `$vikunja-project-bootstrap` before a normal delivery round

## Tracker Assumption

Assume the tracker is Vikunja unless the repository documents explicitly say otherwise.
This skill is reusable across different repositories and teams, but it currently assumes a Vikunja-based backlog.

## Inputs To Confirm

Before acting, identify or confirm:

- the exact target Vikunja project name or ID for this round
- the exact Vikunja project ID once resolved
- any repository-local default project such as `Dec`, if the current repository defines one
- the task plan directory and naming convention, preferably from repository docs or an optional default such as `Documents/tasks/VK-<id>.md`
- the repository documents that define lifecycle, architecture, and completion rules

If any of these are unclear, resolve them before implementation.

## Project Safety

Vikunja has a project concept, but tokens usually do not have project-scoped permissions.
A valid token may be able to read or mutate multiple projects.

Because of that:

- treat any configured project such as `Dec` as an optional repository default, not as universal truth
- if the user explicitly names another project for this round, that explicit target overrides the repository default
- never infer the target project from the repository directory name alone
- resolve the exact project by name or ID, then use its project ID for follow-up operations when possible
- restate the target project before write actions such as create, update, complete, comment, relate, or delete
- if search results are ambiguous or a similar project name appears, stop and ask the user
- if you need to switch to another project mid-round, ask explicitly before doing it

Operating on the wrong project is a hard failure, not an acceptable tradeoff.

## Source Of Truth

Read the repository's workflow documents before selecting work.
Typical sources include backlog rules, repository instructions, architecture docs, and existing task-plan files.

When instructions conflict, prefer executable code and tests first, then architecture and project rules, then longer-term planning docs.

## Default Execution Flow

1. Read the repository's advancement rules and instructions.
2. Resolve the exact Vikunja project for this round from explicit user input, repository docs, configured defaults, or clear tracker references.
3. Query that confirmed project for top-priority actionable items.
4. Exclude items that are explicitly parked, observation-only, or blocked unless the user directs otherwise.
5. Analyze the top candidates, but lock only one item for implementation unless the workflow explicitly allows parallel work.
6. Create or update the per-task plan document using the repository's naming convention, including the task title, tracker link, confirmed project name and ID, stop condition, implementation steps, test scope, and touched modules.
7. Mark the task as actively in progress in Vikunja if the workflow expects that.
8. Implement only the chosen increment.
9. Verify against the stop condition with targeted tests or checks.
10. Review the change for unintended side effects, missing coverage, and doc updates.
11. Update architecture docs, task docs, and tracker status as required by the project workflow.
12. Commit and push with the repository's required traceability format, then close the backlog item with the commit hash and summary when the workflow requires full closure.

## Selection Rules

Through Vikunja MCP tools, first resolve and confirm the exact project in scope for the round, then select the first item that matches the repository's priority rules.

- prefer tasks already accepted into delivery, such as `执行中` or `待排期`
- avoid observation-only, deferred, or blocked items unless there is a concrete trigger or the user directs otherwise
- do not switch to another Vikunja project without explicit confirmation
- do not treat topic-local notes or checklists as the primary source of project priority
- do not mix multiple main lines in one round unless the workflow explicitly allows it
- analyze multiple items if needed, but execute only one item by default

Before the exact project ID is confirmed, do not perform write operations.

## Round Type

Decide the round type before coding:

- tasks in `待分诊` or `待补充` are not implementation-ready; the round should clarify evidence, scope, or ownership first
- tasks in `待研判`, or tasks labeled `type:research` or `type:decision`, may end with analysis, options, recommendation, and tracker updates rather than code
- only tasks already accepted into delivery, usually in buckets like `待排期` or `执行中`, should default to implementation

If the selected item is still mostly problem discovery, stay in a research or decision round and do not force implementation just to keep momentum.


## Query Failures And Ambiguity

When the user-mentioned project, configured default project, or search results appear to be missing, do not jump straight to "not found".

1. Check whether the miss is caused by naming differences such as case, abbreviations, prefixes, suffixes, spaces, hyphens, or parent-child project paths.
2. Check whether the miss is caused by visibility such as archived state, child-project placement, or an overly strict filter.
3. If you find close candidates, ask the user to confirm instead of guessing.
4. Only after those checks fail should you say that the target is not currently found.

Recommended phrasing:

- "I did not find that exact Vikunja project. It may be renamed, archived, or nested under a parent project."
- "I found multiple similar projects. Confirm which one to operate on before I continue."
- "I did not find this exact target. If you want, I can search nearby names or related keywords next."

Forbidden behavior:

- do not conclude the backlog is empty just because an exact-name query missed
- do not switch to a similar-looking project without confirmation
- do not perform create, update, comment, complete, delete, or relation operations before the project is confirmed
- do not phrase a miss as certainty that the target does not exist

## Stop Condition Rules

Stop when the current increment reaches one of these states:

- minimal runnable version that does not block the next step
- planning output with boundaries, interfaces, and acceptance documented
- bug fix with a real failing case covered by verification

Do not stay in a local optimization loop once the milestone is good enough.

## Issue Handling Rules

- file newly discovered bugs or improvements in the currently confirmed target project before fixing them unless the repository workflow defines a narrower exception
- search existing tracker items first to avoid duplicates
- fix immediately only if the issue is a critical blocker or clearly belongs to the active task

## Variable Policy

- Dec assets should keep only truly project-local values as placeholders, such as the default target project and the task plan directory
- stable workflow vocabulary such as process buckets and `type:*` labels should stay fixed in the reusable asset
- after a user confirms local defaults, write those values into project vars so later extraction and reuse do not require another hard-coded rewrite

## Kanban Mutation Safety

- when a workflow needs a real kanban card move, use the dedicated project/view/bucket task-move endpoint for the confirmed project
- do not assume bulk task updates that write `bucket_id` will move the card correctly in board view

## Output Expectations

When the user asks to continue or summarize project work:

- state which Vikunja project was targeted, ideally with both name and project ID
- state how that target project was resolved if there was any ambiguity
- state which task was selected and why
- point to the plan document or planning location
- state the round's stop condition
- report verification performed
- mention whether architecture docs or tracker state were updated

## Anti-Patterns

- do not guess the target Vikunja project from loose context
- do not infer the target project from the current repository name alone
- do not keep polishing a completed local detail just because more optimization is possible
- do not batch unrelated workstreams into one round without an explicit reason
- do not silently fix side issues without creating or linking the corresponding tracker item
- do not close a coding round without both repository traceability and tracker closure when the workflow expects both
