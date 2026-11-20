# Dynamic adviser reputation

status: archived
previous_status: active
archived_at: 2026-11-20T21:49:29Z

Summary:
- decisions: 5
- handoffs: 15
- verification records: 2
- run activity records: 16
- findings open: 0
- findings fixed: 0
- findings verified: 25

## Scope

Ship dynamic adviser reputation tied to multi-adviser propose/critique outcomes. Durable ratings, automatic updates from findings/approvals, routing preference for higher-rated advisers. No human-set advice scores.

## Final Status

All 25 findings verified; implementation review passed. Plan re-approved after stale handoffs.

## Reviewer Verification

- Independently reviewed full adviser reputation diff against approved plan.md, verified all 5 fixed findings (25/25 verified, 0 open), confirmed live macOS Seatbelt write-boundary enforcement in TestAdviserSubprocessFilesystemBoundary (protected workspace/.agents/already-dirty writes blocked; external cache ...
- Reviewed draft plan.md against task scope, decisions, internal/app/app.go, internal/app/local_ai.go, internal/app/plan.go, internal/app/process_{unix,windows}.go, go.mod, and .agents/.gitignore; independently verified all 12 fixed plan findings (20/20 verified, 0 open, 0 fixed); confirmed baseline Go tests pass ...

Detailed transient chunks remain available in git history.
