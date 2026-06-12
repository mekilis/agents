# Fix archive placeholder scope fallback

status: archived
previous_status: active
archived_at: 2026-06-12T19:57:41Z

Summary:
- decisions: 0
- handoffs: 2
- verification records: 1
- run activity records: 2
- findings open: 0
- findings fixed: 0
- findings verified: 0

## Scope

Fix archive summary placeholder detection so weak bullet/list scope like '- define scope' is treated as placeholder and archive summaries fall back to durable handoffs/verification. Preserve sanitization guarantees, add focused regression tests, and update the already generated Improve archive summaries archive only through the normal archive behavior if needed.

## Final Status

Implemented archive scope placeholder normalization for Markdown list markers; focused archive regressions and full go test ./... pass with isolated GOCACHE. Existing improve-archive-summaries archive still has only summary.md, so there is no normal archive source task state left to refresh it.

## Reviewer Verification

- Reviewed archive placeholder fallback implementation, regression tests, task state, historical archive limitation, and whitespace/status. Focused archive tests and full Go suite passed with isolated GOCACHE. ## Command $ [cache env] -d [temp ...

Detailed transient chunks remain available in git history.
