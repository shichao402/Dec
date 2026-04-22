---
name: vikunja-issue
description: >
  Capture and triage a newly discovered issue into the correct Vikunja project safely.
  Use when an AI agent needs to record a problem statement, search for duplicates,
  and decide whether it should remain intake or be classified with canonical
  `type:*` labels such as `type:bug`, `type:feature`, `type:improvement`,
  `type:research`, `type:decision`, or `type:follow-up`.
  Always resolve the exact target project first, then create or update the right item.
---

# Vikunja Issue

Use this skill when the main objective is to record and triage an issue in Vikunja rather than to advance an implementation task immediately.

## Primary Goal

Create or update the right backlog item in the right Vikunja project with enough context that a later discussion or execution round can continue without re-discovery.

## Issue-First Stance

Treat a newly reported issue as a problem statement first.
Do not assume the reporter already diagnosed the root cause correctly.
Do not assume a proposed fix is the solution you must implement.

A user may provide:

- a real bug report
- a desired capability or feature request
- a symptom without root-cause clarity
- a suggested solution that should be treated as input, not as a decision
- a request that really belongs in research or decision-making before implementation

Your job is to preserve the problem clearly, search for duplicates, and classify only as far as the current evidence safely supports.

## Required Inputs

Before any write action, identify or confirm:

- the exact target Vikunja project name or ID
- the exact target Vikunja project ID once resolved
- the issue summary, evidence, impact, and reporter context
- any proposed solution or next step, clearly marked as a hypothesis if not yet validated

If the user names a project explicitly, treat that as the starting point but still resolve the exact project ID before mutating anything.

## Project Safety

Vikunja tokens are usually not project-scoped.
One valid token may be able to change many unrelated projects.

Because of that:

- never infer the target project from the current repository just because it is convenient
- always resolve the project by exact name or explicit ID before create, update, comment, relate, or complete actions
- if multiple similar projects exist, stop and ask the user to confirm
- if the project cannot be resolved uniquely, do not create the issue

Operating on the wrong project is a hard failure.

## Default Flow

1. Confirm the target project name or ID.
2. Resolve the exact Vikunja project and capture its ID.
3. Check for Epic-shaped intent: if the request matches the Epic Recognition signals below, stop and ask the user whether to create an Epic before filing a regular issue. If the user approves, create the Epic first and treat the original request as a candidate subtask of that Epic.
4. Search open tasks in that project using title keywords and short description phrases from the issue.
5. If an existing issue already covers the same underlying problem, append the new evidence there instead of creating a duplicate.
6. If no adequate issue exists, create a new task in that project.
7. Write the title and description around the observed problem, impact, evidence, and open questions.
8. Only classify into labels, bucket, or priority when the current evidence supports it. If classification is still weak, keep it in a neutral intake-style state.
9. If the reporter proposed a fix, record it as a candidate direction or hypothesis, not as an accepted plan.
10. Run Epic Attachment on the newly created or updated issue: query active Epics, compute keyword overlap, and attach to the single matching Epic automatically, or ask the user when there are multiple candidates, or leave it unattached when there are none.
11. Report the created or updated task ID, title, target project, current classification, and Epic attachment outcome back to the user.

## Classification Guidance

Prefer the narrowest safe claim.

- keep it as intake or unclassified when the main value is preserving the problem statement
- classify as `type:bug` when there is a concrete defect, reproduction, or strong failure evidence
- classify as `type:feature` or `type:improvement` when the desired behavior is clearer than the implementation shape
- classify as `type:research` when investigation is the primary next step
- classify as `type:decision` when the next step is selecting between directions, not coding
- classify as `type:follow-up` when it is a scoped continuation of an already completed or nearly completed task
- use `type:epic` only for long-running goals or persistent themes that span many regular cards, and only after explicit user approval (see Epic Awareness)

`type:epic` is mutually exclusive with the other `type:*` labels on the same card. Never apply `type:epic` together with `type:bug`, `type:feature`, and so on.

Do not force a strong classification just to satisfy a template.

## Epic Awareness

Long-running goals and persistent themes live in the project as Epics, which are regular tasks carrying the `type:epic` label. Ordinary cards attach to an Epic through the Vikunja `subtask` relation. This section covers how to recognize a possible Epic during intake; attaching a newly filed issue to an existing Epic is covered in `Epic Attachment` below once that behavior ships.

### Epic Recognition

Intake requests sometimes describe a long-running direction rather than a single bounded change. Recognize these and propose an Epic to the user, but never create the Epic on your own.

A request is a likely Epic candidate when it does one or more of:

- describes scope with words such as `整体`, `整套`, `全部`, `所有`, `全面`, `通用`
- describes time or persistence such as `持续`, `长期`, `长线`, `一直`
- frames itself as a goal or theme rather than a bounded task, such as `目标`, `愿景`, `主题`, `方向` (especially when combined with the persistence words above)
- covers multiple modules, systems, or surfaces, such as `覆盖 X 方面`, `一系列`, `体系化`, `多个模块`, `跨模块`, `跨系统`
- proposes restructuring, governance, or standardization, such as `重构 XX 体系`, `治理 XX`, `规范 XX`, `统一 XX`

When the request matches any of the above, stop and ask the user:

> This looks like a long-running goal rather than a bounded issue. Do you want to create an Epic that captures this goal and lets specific work attach as subtasks? Or should I record it as a regular issue instead?

If the user approves:

- create a new Vikunja task with `type:epic` label
- write a Markdown description that covers goal, success criteria, non-goals, and relevant references
- if the original request also has a concrete subtask attached to that goal, create that subtask separately and attach it to the new Epic through the `subtask` relation (put the relation on the Epic, with `other_task_id` pointing at the subtask)
- report the Epic task ID and title in the output

If the user declines:

- fall through to the normal issue flow
- do not apply `type:epic` label
- it is fine to record in the description that the user explicitly chose to file this as a regular issue

### Epic vs Normal Issue Boundary

To prevent trivial requests from being inflated into Epics:

- a concrete defect with a reproduction is a bug, not an Epic, no matter how the reporter worded it
- a single bounded feature request that can be implemented in one increment is a feature, not an Epic
- a scoped follow-up from a completed task is `type:follow-up`, not an Epic
- if no obvious persistence, scope, or structural wording appears, default to the narrowest safe `type:*` label and do not propose an Epic

Never apply `type:epic` without explicit user approval. Never apply `type:epic` alongside another `type:*` label on the same card.

### Epic Attachment

After you have created a regular issue (or decided to append evidence to an existing one), decide whether it should be attached to an active Epic in the same project.

Attachment procedure:

1. Query active Epics in the confirmed project (tasks with `type:epic` label and not `done`).
2. For each Epic, extract meaningful keywords from its title and description: noun phrases, system or module names, and functional domains. Ignore generic words such as `系统`, `功能`, `问题`, `任务`, `模块`.
3. Extract the same kind of keywords from the issue being filed.
4. Compute the overlap. An Epic is a candidate parent when the overlap contains at least two meaningful keywords.

Behavior based on candidate count:

- **0 candidates**: leave the issue unattached; do not create a `subtask` relation; note in the output that the issue is standalone and remains in intake.
- **1 candidate**: automatically attach the issue to that Epic. Write the `subtask` relation on the Epic task (parent = Epic, `other_task_id` = the new issue). Auto-attach is a tier-3 autonomous action, so record it in the output and (once Autonomy Audit Comment lands) leave an audit trail comment on the attached issue.
- **multiple candidates**: stop and ask the user to choose. Present each candidate with Epic task ID, title, and the overlapping keywords. Never guess between candidates.

Constraints:

- never attach to an Epic that is already `done` or closed
- never attach when the issue itself carries the `type:epic` label
- relation write failures are not silent: if the `subtask` relation write fails, report it and ask the user how to proceed; do not assume the attachment succeeded

## Generic Project Defaults

When a project adopts the generic Vikunja issue structure:

- use buckets to represent process state, not work type
- keep only truly local defaults in vars, such as the repository's default target project; stable process buckets and type labels should stay fixed in the shared asset
- default new issues to `待分诊`, move to `待补充` when evidence is missing, and move to `待研判` once the problem is clear enough for technical or product analysis
- use labels to represent task type, such as `type:bug`, `type:feature`, `type:improvement`, `type:research`, `type:decision`, or `type:follow-up`
- use the built-in priority field for urgency instead of inventing another urgency label set
- do not create new plain labels such as `bug`, `feature`, or `improvement` in the shared workflow
- do not require `source:*` labels unless the team already has a stable, low-ambiguity rule for them; reporter or origin context can live in the description or comments
- if the target project needs buckets, labels, or views standardized first, use `$vikunja-project-bootstrap` instead of expanding this skill


## Dedup Rules

- search within the confirmed target project before creating a task
- compare both title similarity and description scope, not title alone
- if one existing task is a clear superset, update that task instead of creating a narrower duplicate
- if two reports share a root problem but suggest different solutions, prefer one shared issue and record the alternatives there
- if two tasks share a root cause but track truly different user-visible outcomes, keep them separate and mention the related task IDs when useful

## Issue Shape

Prefer concise, actionable issues.

Title should state the concrete problem, request, or decision topic.

Description should usually include:

- what was observed or requested
- why it matters
- any concrete evidence, sample, error text, or affected path
- what is still uncertain
- any proposed solution, explicitly marked as a hypothesis if not yet validated
- what kind of next action seems most appropriate right now
- optionally, a **Parent Epic** field noting which Epic this issue was attached to (task ID and title), when Epic Attachment succeeded

## Output Expectations

When finishing, state:

- which Vikunja project was targeted, with project name and ID when available
- whether you created a new issue or updated an existing one
- the task ID and title
- what canonical `type:*` label, bucket, or priority you applied, if any
- whether duplicate candidates were found
- whether the issue remains intake-style or is ready for a more specific execution workflow
- whether a new Epic was proposed, approved by the user, and created during this round
- Epic Attachment outcome: attached to Epic `<task ID / title>`, asked the user to choose among multiple candidates, or left standalone in intake

## Anti-Patterns

- do not create an issue before the target project is confirmed
- do not file into the current repository's project just because it is convenient
- do not skip duplicate search when the issue is likely recurring
- do not treat a user-proposed solution as authoritative without validation
- do not turn issue filing into an implementation round unless the user explicitly asks for that or the existing workflow clearly requires it
- do not create an Epic task without explicit user approval, even if the request clearly looks like an Epic
- do not inflate a concrete bounded bug or feature into an Epic just to match a template
- do not attach a new issue to more than one Epic at a time; `subtask` relations are not the right place to express overlap across Epics
- do not attach to an Epic when keyword overlap is below the threshold; leave the issue standalone instead
