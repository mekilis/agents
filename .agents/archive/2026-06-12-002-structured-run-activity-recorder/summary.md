# Structured run activity recorder

status: archived
previous_status: active
archived_at: 2026-06-12T18:49:02Z

Summary:
- decisions: 1
- handoffs: 6
- verification records: 1
- run activity records: 3
- findings open: 0
- findings fixed: 0
- findings verified: 4

## Scope

Replace ad hoc run-activity cleanup with a structured recorder pipeline. Goal: agents watch/runs should persist readable supervision events without token-spaced directive fences, broken commands, or raw JSON directive blocks. Design direction: classify cleaned role output before persistence into progress, command/check, directive, and summary/message events; suppress agents-handoff/agents-verify/agents-progress fenced directives from activity after they have been applied elsewhere; preserve short progress lines; normalize machine-local temp/cache paths in displayed commands to stable placeholders such as <isolated-cache>; keep non-TTY/script output stable except for removing previously noisy directive/token fragments from persisted activity. Add focused tests for directive suppression, command normalization, split command preservation, duplicate summary suppression, and short progress preservation. Avoid adding one-off string heuristics where a structured parser/classifier is feasible.

## Final Status

Fixed open structured run activity findings. Directive suppression now handles agents-* fenced directives generically, command classification uses explicit Agents-emitted prefixes only, local temp/cache redaction is centralized in a sanitizer, and command/progress allowlists were removed. Focused run activity tests and full go test ./... passed with isolated GOCACHE.

Detailed transient chunks remain available in git history.
