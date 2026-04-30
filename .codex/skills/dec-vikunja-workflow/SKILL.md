<!-- 本文件由 `dec pull` 从 .dec/cache/vikunja/ 渲染生成，请勿直接编辑。
     修改流程：编辑 .dec/cache/vikunja/... → dec push → dec pull 验证 -->

---
name: vikunja-workflow
description: >
  Execute one complete Vikunja-backed delivery round inside one confirmed project.
  Use when an AI agent needs to choose the next task, keep a working plan,
  implement or analyze one increment, verify it, write traceability back, and close the loop.
  Always confirm the exact target project before any write action.
---

# Vikunja Workflow

Use this skill to advance work through a reusable Vikunja workflow instead of a repository-local markdown board.

## Scope Boundary

- operate inside one explicitly confirmed Vikunja project for the current round
- advance one main item per round unless repository rules explicitly allow parallel work
- advance items within a single confirmed Epic per round; do not interleave work from different Epics in the same round
- use `$vikunja-issue` when the main job is to file or triage a new issue, including recognizing and proposing a new Epic
- use `$vikunja-project-bootstrap` when the main job is to create or normalize project structure
- never create an Epic in this skill; Epic creation is always handled by `$vikunja-issue` with user approval

## Inputs To Confirm

Before acting, identify or confirm:

- the exact target Vikunja project name or ID for this round
- the exact Vikunja project ID once resolved
- the target Epic for this round (its task ID and title)
- any repository default project such as `Dec`
- the repository's working-plan policy
- any repository docs that define lifecycle, architecture, and completion rules

If any of these are unclear, resolve them before implementation. Epic confirmation follows `Round Epic Lock`.

## Non-Negotiables

- confirm the exact target project, then resolve the exact project ID, before any write action
- confirm the exact target Epic for this round before selecting any card; a project with zero active Epics is not advanceable by this skill
- treat repository defaults such as `Dec` as defaults only, never as universal truth
- query and mutate tasks inside the confirmed project scope; do not treat a global task list as the current backlog
- once an Epic is locked for the round, select cards only from that Epic's subtasks; do not pick cards outside the locked Epic
- a delivery already committed and written back to Vikunja (the task carries a commit hash comment) is immutable; never rewrite that card's description, close state, or commit record
- keep the working plan in Vikunja by default; create `docs/tasks/VK-<id>.md` only when repository rules require it or the round needs durable repo-local design or verification notes
- use Vikunja's built-in `done` field as the completion truth; a visual done lane is presentation, not a second completion system

Operating on the wrong project is a hard failure. Operating outside the locked Epic is also a hard failure.

## Canonical Tracker Model

Shared assets assume one canonical model.

- urgency comes from the built-in priority field
- buckets describe process stage, not work type
- active delivery buckets are `待分诊`, `待补充`, `待研判`, `待排期`, `执行中`, and `阻塞`
- canonical type labels are `type:epic`, `type:bug`, `type:feature`, `type:improvement`, `type:research`, `type:decision`, and `type:follow-up`
- tasks in `待分诊` or `待补充` are clarification rounds, not coding rounds
- tasks in `待研判`, or tasks labeled `type:research` or `type:decision`, are analysis or decision rounds unless repository rules say otherwise
- tasks in `待排期` or `执行中` are the normal implementation candidates
- `阻塞` stays parked unless the blocking condition changed or the user explicitly wants to work it
- repository-local parked markers such as observation labels may exist, but they belong in repository rules, not in the shared asset

If a project still depends on legacy ready labels, plain `bug/feature/improvement` labels, or extra completion semantics, keep that as repository-local policy or normalize the project. Do not reintroduce those compatibility rules here.

## Epic As First Class

Epics represent long-running goals or persistent themes that span many regular cards.
Treat Epics as first-class citizens in the shared model, not as an ad-hoc convention.

### Identification

- an Epic is any task whose labels include `type:epic`
- `type:epic` is mutually exclusive with other `type:*` labels on the same card
- active Epic: an Epic task that is not `done` and is not in a closed-style bucket
- Epics are plain Vikunja tasks; they are not a bucket, column, or separate view

### Multiple Epics

- a project can have more than one active Epic at the same time
- Epics may depend on other Epics; this shared skill uses the Vikunja task relation kind `blocked` to express "this Epic is blocked by that Epic". The kind name is `blocked`, not `blocked_by`
- dependency direction: put the relation on the source Epic with `relation_kind: "blocked"` and `other_task_id` pointing at the blocker
- ordinary tasks attach to an Epic through the `subtask` relation on the Epic (parent = Epic, subtask = regular task)

### No Epic Means Blocked

- a project with zero active Epics cannot be advanced by this workflow
- do not silently fall back to flat backlog selection
- do not create an Epic on your own; always ask the user to either promote an existing card to Epic or to draft a new Epic title and goal
- while the project is in this blocked state, do not select, modify, close, or comment on any card except to record the blocked state in the output

### Creating Epics Requires User Approval

- recognizing that a request looks like an Epic is fine to do proactively
- actually creating the Epic task with the `type:epic` label requires explicit user approval
- new issue capture that looks like a long-running goal should be handled by `$vikunja-issue`, which proposes the Epic for user approval; this workflow skill never creates Epics on its own

## Round Epic Lock

Every round locks exactly one target Epic before any card selection. This scoping prevents cross-Epic interleaving and makes the round's boundary explicit.

### Locking Procedure

1. After the project ID is resolved, query all active Epics in the confirmed project (tasks with `type:epic` label and not `done`).
2. Count the active Epics:
   - **0 active**: enter the no-Epic blocking flow (see below). Do not proceed to card selection.
   - **1 active**: automatically lock that Epic for the round and tell the user which Epic was locked (task ID and title).
   - **multiple active**: stop and ask the user to pick one. Present each candidate with task ID, title, and the number of active (non-done) subtasks. Do not guess.
3. Check cross-Epic dependencies on the locked Epic by reading its `blocked` relations (see `Epic As First Class > Multiple Epics`). If the locked Epic is blocked by another still-active Epic, surface that and ask the user whether to switch focus to the blocker before continuing.
4. Only after the Epic is locked and dependency state is explicit may the round select a card.

### Scope While Locked

- candidate cards are the subtasks of the locked Epic, filtered by the existing `Query And Selection Rules`
- do not pull cards from other Epics, even if they appear to share context
- do not compare or rank cards across Epics in the same round
- if the user wants to switch Epics mid-round, treat it as a new round: re-run the locking procedure from step 1

### Locked Epic Recovery Flow

When the locked Epic has no locally actionable candidate after filtering, do not treat the whole project as empty and do not silently execute a card outside the locked Epic.

This state can happen when every remaining subtask is blocked, parked, waiting for a specific external environment, or otherwise not runnable in the current agent context.

In that case:

- report the locked Epic and the exact reason no subtask is locally actionable
- keep all write operations paused; do not modify, close, comment on, or relate any candidate during discovery
- run an advisory project-scoped scan for active, non-done, non-`type:epic` tasks that appear ready but are not attached to any active Epic through a `parenttask`/`subtask` relation
- list those orphan ready candidates separately from the locked Epic's subtasks, including task ID, title, type labels, priority, and why they look actionable
- ask the user to choose a structural next step before implementation:
  1. attach a candidate to an existing active Epic
  2. promote an existing task to Epic after explicit approval
  3. draft a new Epic and create it through `$vikunja-issue` after explicit approval
  4. mark or move the blocking subtask according to the project's blocking policy
- after the user confirms the structure change, perform only that approved relation/label/bucket action, write the required audit comment, then restart the round from Epic lock

Orphan ready candidates are evidence that backlog structure needs cleanup. They are not execution candidates until they belong to the locked Epic of a fresh round.

### No-Epic Blocking Flow

When the confirmed project has zero active Epics:

- do not select any card
- do not modify, close, comment on, or relate any task in this round
- do not create an Epic autonomously
- report the blocked state explicitly, then offer the user two paths:
  1. promote an existing card to Epic (the user names which card; agent then adds the `type:epic` label after user approval)
  2. draft a new Epic from scratch (agent helps with title and goal; user approves before the Epic task is created; Epic creation itself is carried out by `$vikunja-issue`, not by this skill)
- end the round until the user takes one of those paths

## Source Of Truth Order

When instructions differ, use this order:

1. explicit user instruction for this round
2. repository-local workflow docs or agent instructions
3. project-local Dec vars such as `Dec` and `docs/tasks`
4. the generic defaults in this skill

When behavior still conflicts, prefer executable code and tests over docs.

## Working Plan Location

Keep the working plan in Vikunja by default.
Create a repo-local task doc only when repository rules require it or when the round clearly needs durable repo-local design or verification notes.

## Autonomy Tier 3

This skill operates under a tier-3 autonomy contract: the agent may take certain recurring maintenance actions without asking, must propose structural changes for user approval, and must never touch immutable delivery records. Every autonomous action has to leave an audit trail (see `Autonomy Audit Comment`).

### Do Automatically (No Approval Required)

- append missing context to an existing task's description (acceptance criteria, evidence, clarifications); always read the current description first to avoid duplicate content
- set or adjust the `priority` field on a subtask of the locked Epic
- add missing canonical `type:*` labels (except `type:epic`) to a subtask of the locked Epic
- reorder the relative position of subtasks inside the locked Epic
- close a task that was executed this round and satisfies the stop condition (commit, push, and write-back all completed)
- advance to the next card in the same round within the `Continuation Window` budget
- attach a newly filed issue to exactly one matching active Epic via `subtask` relation, when the single-candidate rule in `$vikunja-issue > Epic Attachment` is satisfied

### Propose To User (Must Wait For Approval)

- create a new Epic (any `type:epic` task creation must be approved by the user and is carried out by `$vikunja-issue`)
- merge two Epics (moving subtasks from one Epic to another)
- delete an Epic or mark an Epic complete
- substantially rewrite an Epic's title or core goal statement
- create a cross-Epic `blocked` relation
- re-parent an existing task from one Epic to another
- pick among multiple candidate Epics during Epic Attachment
- promote an existing regular task to an Epic by adding the `type:epic` label

### Never Do

- modify a delivery task that already carries a commit hash write-back comment (the card is immutable from the autonomy standpoint)
- skip project confirmation or Epic lock for this round
- advance more than two cards in a single round, even if every card seems small
- select cards outside the locked Epic
- proceed in a project that has zero active Epics
- delete a task created by the user
- create a `type:epic` labeled task without explicit user approval

## Default Execution Flow

1. Read any repository-specific advancement rules and instructions if they exist.
2. Resolve the exact Vikunja project for this round and confirm its project ID.
3. Query active Epics in the confirmed project (tasks with `type:epic` label and not `done`).
4. Lock the round Epic per `Round Epic Lock`: 0 active → enter no-Epic blocking flow and exit the round; 1 active → auto-lock and tell the user; multiple active → ask the user to pick one.
5. Query actionable items inside the locked Epic's subtasks, sorted by priority.
6. Exclude blocked items, intake items, and repository-local parked items unless the user directs otherwise.
7. If no subtask remains locally actionable, enter `Locked Epic Recovery Flow` and stop before implementation.
8. Check cross-Epic `blocked` relations on the locked Epic; if it is blocked by another active Epic, surface the dependency and ask the user before continuing.
9. Analyze the top candidates, but lock only one item for execution.
10. Classify the locked item as a small card or a large card against the `Continuation Window` criteria.
11. Create or update the working plan in Vikunja first. Add a local task doc only when repository rules require it or the round clearly benefits from durable repo-local notes.
12. Mark the task as actively in progress in Vikunja if the workflow expects that.
13. Implement or analyze only the chosen increment.
14. Verify against the stop condition.
15. Review for regressions, missing coverage, and required doc updates.
16. Inspect `git status --short` and make sure only intended files are in scope.
17. Commit and push using the repository's traceability rules.
18. Write the commit hash, verification summary, and closeout note back to Vikunja.
19. Write a tier-3 audit comment on the task summarizing autonomous actions taken this round, per `Autonomy Audit Comment`.
20. Mark the task done only after write-back and audit comment attempts are complete.
21. Continuation decision: if the user asked to continue and the current item was a small card and fewer than two cards have been advanced this round, loop back to step 5 within the same locked Epic; otherwise stop and summarize.

## Query And Selection Rules

- candidate cards are always the subtasks of the round's locked Epic; do not look outside the locked Epic
- prefer items already accepted into delivery, normally in `执行中` or `待排期`
- do not treat topic-local notes, checklists, or design docs as the project backlog
- use project-scoped endpoints or explicit `project_id` filters whenever possible, and further filter by the locked Epic's subtasks
- if project lookup is ambiguous, stop and ask the user
- if an exact-name lookup misses, check nearby names or visibility differences before declaring the project missing
- if the locked Epic has no actionable subtasks that match the selection rules, use `Locked Epic Recovery Flow` rather than reaching across Epics
- analyze multiple items if needed, but execute only one item by default

Before the exact project ID and the round Epic are both confirmed, do not perform write operations.

## Round Type

Decide the round type before coding:

- tasks in `待分诊` or `待补充` are not implementation-ready; the round should clarify evidence, scope, or ownership first
- tasks in `待研判`, or tasks labeled `type:research` or `type:decision`, may end with analysis, options, recommendation, and tracker updates rather than code
- only tasks already accepted into delivery, usually in buckets like `待排期` or `执行中`, should default to implementation

If the selected item is still mostly problem discovery, stay in a research or decision round and do not force implementation just to keep momentum.

## Query Failures And Ambiguity

When the user-mentioned project, configured default project, or search results appear to be missing, do not jump straight to `not found`.

1. Check whether the miss is caused by naming differences such as case, abbreviations, prefixes, suffixes, spaces, hyphens, or parent-child project paths.
2. Check whether the miss is caused by visibility such as archived state, child-project placement, or an overly strict filter.
3. If you find close candidates, ask the user to confirm instead of guessing.
4. Only after those checks fail should you say that the target is not currently found.

Recommended phrasing:

- `I did not find that exact Vikunja project. It may be renamed, archived, or nested under a parent project.`
- `I found multiple similar projects. Confirm which one to operate on before I continue.`
- `I did not find this exact target. If you want, I can search nearby names or related keywords next.`

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

## Continuation Window

A single user `继续` (or equivalent) may advance at most two cards inside the locked Epic. The window is a safety budget, not a target: if something looks uncertain, stop early.

### Small Card Criteria

A card counts as a small card only when **all** of the following hold. If any one fails, treat it as a large card and stop after completing it.

- **Size**: the change can reasonably land in a single focused increment (code, docs, config, or tracker updates), without needing a plan across multiple sessions
- **Structure**: the round will not create a new Epic, re-parent a card, create a cross-Epic `blocked` relation, or redefine a bucket's meaning
- **External dependency**: no new information, secret, decision, or external service access has to come from the user mid-round
- **Risk**: the change does not involve data migration, production release steps, force-pushes, or other destructive or irreversible actions

### Budget Rules

- the budget resets at the start of each user-initiated round (each fresh `继续`)
- even if both cards are small, stop after the second card; never advance a third card in the same round
- after a large card, stop and summarize; do not advance another card regardless of remaining budget
- if the first card fails to reach its stop condition, do not advance to a second card; summarize what happened and wait

### Disclosure

When advancing more than one card in a round, report in the output:

- how many cards were advanced this round
- for each card, whether it was classified as small and why
- which card exhausted the budget (or which card was classified as large and ended the round)

## Repository Traceability

- run `git status --short` before staging and again before closeout
- stage only files owned by the active task
- if the repository does not define a stricter commit convention, include the tracker ID in the commit subject, for example `feat(VK-47): ...`
- push before closing the tracker item when remote push is part of the normal workflow
- write the resulting commit hash back to Vikunja before marking the item fully closed
- if commit or push cannot be completed, leave the tracker item explicitly open with a note about what remains

## Autonomy Audit Comment

Every tier-3 autonomous action has to leave a Markdown comment on the task it touched, using `vikunja_create_task_comment`. Comments are the shared audit trail and the data source for future Vikunja visualization work.

### Required Fields

Each audit comment is a Markdown fragment containing these fields, in this order:

- **Action**: a short action key such as `auto-close`, `auto-reprioritize`, `auto-append-description`, `auto-advance`, `auto-attach-to-epic`, `auto-relabel`, `auto-reorder`
- **Tier**: always `tier-3-autonomous` (future tooling filters on this key)
- **Round Epic**: the locked Epic's task ID and title
- **Trigger**: the proximate reason (quoted user instruction excerpt, or `continuation-window card #N` when stepping inside a continuation loop)
- **Change**: concrete summary of what changed (field name with old → new value, or a short action description)
- **Related**: related commit hash, related task IDs, or `none`
- **Timestamp**: ISO 8601 local-timezone timestamp

### Example Shape

```markdown
- **Action**: auto-attach-to-epic
- **Tier**: tier-3-autonomous
- **Round Epic**: #1 EPIC：TUI 化（所有功能在 TUI 内可用）
- **Trigger**: vikunja-issue Epic Attachment single-candidate rule
- **Change**: attached this task to Epic #1 via `subtask` relation
- **Related**: related task #1
- **Timestamp**: 2026-04-21T16:40:00+08:00
```

### Approval-Path Actions

For actions that live in the **propose to user** bucket of `Autonomy Tier 3`, still write an audit comment after the user approves. Prefix the `Action` value with `user-approved-` (for example `user-approved-merge-epics`) so that the record can be distinguished from unattended autonomous actions.

### Failure Policy (Action Priority)

- the primary action is executed first; the audit comment attempt follows
- if the audit comment call fails (network error, Vikunja rejection, permission issue), do **not** roll back the primary action; autonomous changes are already effective
- instead, record the missed audit in the round's final output: which task, what action, and the original fields that should have been written, so the user can manually post the missing comment
- repeated audit failures across a single round should stop the round, since running blind defeats the purpose of the audit trail

### Anti-Gaming

- do not skip the audit comment simply because "nothing important changed"; if an autonomous action ran, it has to be recorded
- do not bundle multiple distinct autonomous actions into a single comment to save API calls; write one comment per action
- do not retroactively edit a past audit comment to cover up a later change; write a new comment that references the earlier one

## Issue Handling Rules

- file newly discovered bugs or improvements in the confirmed project before fixing them unless the repository defines a narrower exception
- search existing tracker items first to avoid duplicates
- use canonical `type:*` labels for new items
- fix immediately only if the issue is a critical blocker or clearly belongs to the active task

## Kanban Mutation Safety

- when a workflow needs a real kanban card move, use the dedicated project/view/bucket task-move endpoint for the confirmed project
- do not assume bulk task updates that write `bucket_id` will move the card correctly in board view
- relation changes (`subtask`, `blocked`) mutate project structure; use the dedicated task-relation endpoint for the exact task; do not approximate relations with bucket moves or label tricks
- do not silently retry failed relation writes; surface the failure, report it, and ask the user before continuing

## Output Expectations

When the user asks to continue or summarize project work:

- state which Vikunja project was targeted, ideally with both name and project ID
- state which Epic was locked for the round (task ID and title), or state that no Epic was available and the round was blocked
- state which task was selected and why
- state where the working plan lives for this round
- state the round's stop condition
- report verification performed
- mention the commit hash or explicitly say that commit or push did not happen
- mention whether architecture docs or tracker state were updated
- report how many cards were advanced this round and why the round stopped (budget exhausted, large card, failure, no more actionable subtasks)
- if `Locked Epic Recovery Flow` ran, list the blocked subtasks and any orphan ready candidates discovered, and state that no candidate was executed until structure is confirmed
- list any audit comment writes that failed so the user can manually post the missing comment

## Anti-Patterns

- do not guess the target Vikunja project from loose context
- do not infer the target project from the current repository name alone
- do not rely on global task search as a substitute for project confirmation
- do not create repository-local task docs by default when the tracker plan is enough
- do not reintroduce legacy compatibility rules into the shared asset
- do not batch unrelated workstreams into one round without an explicit reason
- do not close a tracker item before commit, write-back, and final state update are all complete
- do not select cards outside the locked Epic once the round is locked
- do not let one blocked locked-Epic subtask make the whole project look empty; use `Locked Epic Recovery Flow` to surface orphan ready work without executing it
- do not auto-pick an Epic when the project has more than one active Epic; ask the user
- do not create an Epic inside this skill; Epic creation is always handled by `$vikunja-issue` with explicit user approval
- do not advance more than two cards in a single round, even if every card looks small
- do not skip the tier-3 audit comment for an autonomous action; the audit trail is the contract
- do not use a bucket or view to represent an Epic; Epics are tasks labeled `type:epic` with `subtask` relations
- do not silently retry or hide a failed `subtask` or `blocked` relation write; surface and ask the user
- do not modify a task that already has a commit-hash write-back comment; delivered cards are immutable
- do not advance into a project that has zero active Epics; enter the no-Epic blocking flow instead
