# Make plans free-form but substantive

status: archived
previous_status: active
archived_at: 2026-06-12T20:29:15Z

Summary:
- decisions: 1
- handoffs: 3
- verification records: 1
- run activity records: 2
- findings open: 0
- findings fixed: 0
- findings verified: 0

## Scope

Refine plan mode so generated and reviewed plans can be free-form Markdown while still being substantive. Avoid making users fill rigid hardcoded section headings. Preserve durable .agents plan state, plan approval routing, and prompt budget protection. Keep reviewer/coordinator guidance focused on substance: objective, evidence, open questions/assumptions, implementation shape, validation, risks, and constraints may appear naturally rather than as mandatory exact sections.

## Final Status

Implemented free-form plan draft scaffold, substance-focused coordinator/reviewer guidance, prompt-budget preservation for free-form risk/constraint lines, docs, and focused tests. Checks passing.

## Reviewer Verification

- Reviewed plan-mode scaffold, coordinator/reviewer guidance, prompt budget preservation, docs, and tests. No findings. Full suite passed with isolated Go build cache; focused plan/prompt tests and git diff --check also passed. ## Command ...

Detailed transient chunks remain available in git history.
