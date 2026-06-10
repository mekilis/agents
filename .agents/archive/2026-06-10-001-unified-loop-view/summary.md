# Unified loop view

status: archived
previous_status: active
archived_at: 2026-06-10T19:00:38Z

Summary:
- decisions: 9
- handoffs: 8
- verification records: 6
- findings open: 0
- findings fixed: 0
- findings verified: 3

## Scope

Build the unified/split agent view per qa/unified-loop-view-plan.md: loop events (M0), agents go entry point (M1), two-lane TTY renderer with plain-language status (M2), human-boundary decision screen (M3).

## Final Status

M3 implemented: readyscreen.go (summary gathering, rendering, key actions, accept/iterate/abandon) + runLoopWithView outer iterate loop in loopview.go; README updated; 139 tests pass, gofmt/vet clean; PTY smoke tests verified accept and iterate→abandon flows end-to-end

Detailed transient chunks remain available in git history.
