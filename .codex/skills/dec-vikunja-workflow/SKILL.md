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
- use `$vikunja-issue` when the main job is to file or triage a new issue
- use `$vikunja-project-bootstrap` when the main job is to create or normalize project structure

## Inputs To Confirm

Before acting, identify or confirm:

- the exact target Vikunja project name or ID for this round
- the exact Vikunja project ID once resolved
- any repository default project such as `Dec`
- the repository's working-plan policy
- any repository docs that define lifecycle, architecture, and completion rules

If any of these are unclear, resolve them before implementation.

## Non-Negotiables

- confirm the exact target project, then resolve the exact project ID, before any write action
- treat repository defaults such as `Dec` as defaults only, never as universal truth
- query and mutate tasks inside the confirmed project scope; do not treat a global task list as the current backlog
- keep the working plan in Vikunja by default; create `docs/tasks/VK-<id>.md` only when repository rules require it or the round needs durable repo-local design or verification notes
- use Vikunja's built-in `done` field as the completion truth; a visual done lane is presentation, not a second completion system

Operating on the wrong project is a hard failure.

## Canonical Tracker Model

Shared assets assume one canonical model.

- urgency comes from the built-in priority field
- buckets describe process stage, not work type
- active delivery buckets are `待分诊`, `待补充`, `待研判`, `待排期`, `执行中`, and `阻塞`
- canonical type labels are `type:bug`, `type:feature`, `type:improvement`, `type:research`, `type:decision`, and `type:follow-up`
- tasks in `待分诊` or `待补充` are clarification rounds, not coding rounds
- tasks in `待研判`, or tasks labeled `type:research` or `type:decision`, are analysis or decision rounds unless repository rules say otherwise
- tasks in `待排期` or `执行中` are the normal implementation candidates
- `阻塞` stays parked unless the blocking condition changed or the user explicitly wants to work it
- repository-local parked markers such as observation labels may exist, but they belong in repository rules, not in the shared asset

If a project still depends on legacy ready labels, plain `bug/feature/improvement` labels, or extra completion semantics, keep that as repository-local policy or normalize the project. Do not reintroduce those compatibility rules here.

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

## Default Execution Flow

1. Read any repository-specific advancement rules and instructions if they exist.
2. Resolve the exact Vikunja project for this round and confirm its project ID.
3. Query actionable items inside that confirmed project, sorted by priority.
4. Exclude blocked items, intake items, and repository-local parked items unless the user directs otherwise.
5. Analyze the top candidates, but lock only one item for execution.
6. Create or update the working plan in Vikunja first. Add a local task doc only when repository rules require it or the round clearly benefits from durable repo-local notes.
7. Mark the task as actively in progress in Vikunja if the workflow expects that.
8. Implement or analyze only the chosen increment.
9. Verify against the stop condition.
10. Review for regressions, missing coverage, and required doc updates.
11. Inspect `git status --short` and make sure only intended files are in scope.
12. Commit and push using the repository's traceability rules.
13. Write the commit hash, verification summary, and closeout note back to Vikunja.
14. Mark the task done only after write-back is complete.

## Query And Selection Rules

- prefer items already accepted into delivery, normally in `执行中` or `待排期`
- do not treat topic-local notes, checklists, or design docs as the project backlog
- use project-scoped endpoints or explicit `project_id` filters whenever possible
- if project lookup is ambiguous, stop and ask the user
- if an exact-name lookup misses, check nearby names or visibility differences before declaring the project missing
- analyze multiple items if needed, but execute only one item by default

Before the exact project ID is confirmed, do not perform write operations.

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

## Repository Traceability

- run `git status --short` before staging and again before closeout
- stage only files owned by the active task
- if the repository does not define a stricter commit convention, include the tracker ID in the commit subject, for example `feat(VK-47): ...`
- push before closing the tracker item when remote push is part of the normal workflow
- write the resulting commit hash back to Vikunja before marking the item fully closed
- if commit or push cannot be completed, leave the tracker item explicitly open with a note about what remains

## Issue Handling Rules

- file newly discovered bugs or improvements in the confirmed project before fixing them unless the repository defines a narrower exception
- search existing tracker items first to avoid duplicates
- use canonical `type:*` labels for new items
- fix immediately only if the issue is a critical blocker or clearly belongs to the active task

## Kanban Mutation Safety

- when a workflow needs a real kanban card move, use the dedicated project/view/bucket task-move endpoint for the confirmed project
- do not assume bulk task updates that write `bucket_id` will move the card correctly in board view

## Output Expectations

When the user asks to continue or summarize project work:

- state which Vikunja project was targeted, ideally with both name and project ID
- state which task was selected and why
- state where the working plan lives for this round
- state the round's stop condition
- report verification performed
- mention the commit hash or explicitly say that commit or push did not happen
- mention whether architecture docs or tracker state were updated

## Anti-Patterns

- do not guess the target Vikunja project from loose context
- do not infer the target project from the current repository name alone
- do not rely on global task search as a substitute for project confirmation
- do not create repository-local task docs by default when the tracker plan is enough
- do not reintroduce legacy compatibility rules into the shared asset
- do not batch unrelated workstreams into one round without an explicit reason
- do not close a tracker item before commit, write-back, and final state update are all complete
