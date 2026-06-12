# Human approval boundary display

status: archived
previous_status: active
archived_at: 2026-06-12T18:27:15Z

Summary:
- decisions: 0
- handoffs: 4
- verification records: 1
- run activity records: 4
- findings open: 0
- findings fixed: 0
- findings verified: 1

## Scope

Make the human approval boundary clearer in the visible terminal supervision experience, especially agents watch and/or agents status when the next role is human and approval/archive/commit/next-task action is required. Keep agents status scriptable and banner-free; avoid changing non-TTY/script output unexpectedly. Add focused tests for the displayed boundary behavior.

## Final Status

Fixed reviewer finding: the Ready for you cue is now watch-only, preserving plain agents runs output. Focused boundary tests and full go test ./... pass with the requested isolated cache.

Detailed transient chunks remain available in git history.
