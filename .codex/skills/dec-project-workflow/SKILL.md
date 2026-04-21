---
name: project-workflow
description: Summarize and execute a Vikunja-backed project advancement method. Use when an AI coding agent needs to continue work in a project tracked in Vikunja, choose the next task from the correct Vikunja project, create a task plan, implement against the documented lifecycle, or explain how work should move from selection to closure without cross-project mistakes.
---

# Project Workflow

Use this skill to advance a software project through a tracked Vikunja backlog with a repeatable lifecycle instead of ad hoc local work.

## Tracker Assumption

Assume the tracker is Vikunja unless the repository documents explicitly say otherwise.
This skill is reusable across different projects, but it currently assumes a Vikunja-based backlog.

## Inputs To Confirm

Before acting, identify or confirm:

- the exact Vikunja project name
- the exact Vikunja project ID once resolved
- the task plan directory and naming convention
- the repository documents that define lifecycle, architecture, and completion rules

If any of these are unclear, resolve them before implementation.

## Project Safety

Vikunja has a project concept, but tokens usually do not have project-scoped permissions.
A valid token may be able to read or mutate multiple projects.

Because of that:

- treat the target project as mandatory input, not an assumption
- resolve the exact project by name, then use its project ID for follow-up operations when possible
- restate the target project before write actions such as create, update, complete, comment, relate, or delete
- if search results are ambiguous or a similar project name appears, stop and ask the user
- never operate on a task from a different project just because the title or keywords look relevant

Operating on the wrong project is a hard failure, not an acceptable tradeoff.

## Source Of Truth

Read the project's workflow documents before selecting work.
Typical sources include backlog rules, repository instructions, architecture docs, and existing task-plan files.

When instructions conflict, prefer executable code and tests first, then architecture and project rules, then longer-term planning docs.

## Default Execution Flow

1. Read the project's advancement rules and repository instructions.
2. Resolve the exact Vikunja project by name and project ID before reading or mutating backlog items.
3. Query that project for top-priority actionable items.
4. Exclude items that are explicitly parked, observation-only, or blocked unless the user directs otherwise.
5. Analyze the top candidates, but lock only one item for implementation unless the project's documented parallel criteria are fully satisfied.
6. Create or update the per-task plan document with the task title, tracker link, stop condition, implementation steps, test scope, and touched modules.
7. Mark the task as actively in progress in Vikunja if the workflow expects that.
8. Implement only the chosen increment.
9. Verify against the stop condition with targeted tests or checks.
10. Review the change for unintended side effects, missing coverage, and doc updates.
11. Update architecture docs, task docs, and tracker status as required by the project workflow.
12. Commit and push with the project's required traceability format, then close the backlog item with the commit hash and summary when the workflow requires full closure.

## Selection Rules

- Prefer tasks already marked as active or ready to start.
- Avoid observation-only or deferred tasks unless there is a concrete trigger.
- Do not switch to another Vikunja project without explicit confirmation.
- Do not treat topic-local notes or checklists as the primary source of project priority.
- Do not mix multiple main lines in one round unless the workflow explicitly allows it.
- Analyze multiple items if needed, but execute only one item by default.

## Stop Condition Rules

Stop when the current increment reaches one of these states:

- minimal runnable version that does not block the next step
- planning output with boundaries, interfaces, and acceptance documented
- bug fix with a real failing case covered by verification

Do not stay in a local optimization loop once the milestone is good enough.

## Issue Handling Rules

- File newly discovered bugs or improvements in the project's tracker before fixing them unless the workflow defines a narrower exception.
- Search existing tracker items first to avoid duplicates.
- Fix immediately only if the issue is a critical blocker or clearly belongs to the active task.

## Output Expectations

When the user asks to continue or summarize project work:

- state which Vikunja project was targeted, ideally with both name and project ID
- state which task was selected and why
- point to the plan document or planning location
- state the round's stop condition
- report verification performed
- mention whether architecture docs or tracker state were updated

## Anti-Patterns

- Do not guess the target Vikunja project from loose context.
- Do not keep polishing a completed local detail just because more optimization is possible.
- Do not batch unrelated workstreams into one round without an explicit reason.
- Do not silently fix side issues without creating or linking the corresponding tracker item.
- Do not close a coding round without both repository traceability and tracker closure when the workflow expects both.
