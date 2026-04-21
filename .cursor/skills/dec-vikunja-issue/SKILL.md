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
3. Search open tasks in that project using title keywords and short description phrases from the issue.
4. If an existing issue already covers the same underlying problem, append the new evidence there instead of creating a duplicate.
5. If no adequate issue exists, create a new task in that project.
6. Write the title and description around the observed problem, impact, evidence, and open questions.
7. Only classify into labels, bucket, or priority when the current evidence supports it. If classification is still weak, keep it in a neutral intake-style state.
8. If the reporter proposed a fix, record it as a candidate direction or hypothesis, not as an accepted plan.
9. Report the created or updated task ID, title, target project, and current classification back to the user.

## Classification Guidance

Prefer the narrowest safe claim.

- keep it as intake or unclassified when the main value is preserving the problem statement
- classify as `type:bug` when there is a concrete defect, reproduction, or strong failure evidence
- classify as `type:feature` or `type:improvement` when the desired behavior is clearer than the implementation shape
- classify as `type:research` when investigation is the primary next step
- classify as `type:decision` when the next step is selecting between directions, not coding
- classify as `type:follow-up` when it is a scoped continuation of an already completed or nearly completed task

Do not force a strong classification just to satisfy a template.

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

## Output Expectations

When finishing, state:

- which Vikunja project was targeted, with project name and ID when available
- whether you created a new issue or updated an existing one
- the task ID and title
- what canonical `type:*` label, bucket, or priority you applied, if any
- whether duplicate candidates were found
- whether the issue remains intake-style or is ready for a more specific execution workflow

## Anti-Patterns

- do not create an issue before the target project is confirmed
- do not file into the current repository's project just because it is convenient
- do not skip duplicate search when the issue is likely recurring
- do not treat a user-proposed solution as authoritative without validation
- do not turn issue filing into an implementation round unless the user explicitly asks for that or the existing workflow clearly requires it
