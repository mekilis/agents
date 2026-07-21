# Agent addressing and inbox

status: archived
previous_status: active
archived_at: 2026-07-21T21:40:52Z

Summary:
- decisions: 1
- handoffs: 8
- verification records: 2
- run activity records: 13
- findings open: 0
- findings fixed: 0
- findings verified: 11

## Scope

Add task-scoped named agent registration with role and harness labels; CLI agent add/list/remove; addressed handoffs that target named agents while preserving existing role targets; explicit consumed state; banner-free agents inbox --as NAME with list, --consume, and JSON support; inbox includes handoffs to the named agent and its role; watch mode that blocks for new unconsumed items for an agent and emits one stable machine-readable line per event; MCP parity for agent registration/listing, inbox list/consume, and add_handoff target agent with sensible actor defaults; scriptable status showing registered agents and per-agent unconsumed counts; focused tests for agent-vs-role addressing, idempotent consume, inbox filtering, watch emission, and MCP round trips; a short two-harness documentation page with watcher snippet; and a scripted end-to-end demo under qa/ or tutorials showing handoff exchange, consumption, finding, fix, verification, and inbox state changes. Keep findings and decisions broadcast. No web UI, transport, auth, threading, or automatic orchestration. Run the full isolated-cache Go test suite. Keep committed records free of machine-local or private details.

## Final Status

Implemented named task agents, addressed handoffs with explicit inbox consumption, agents watch --as events, MCP parity, status counts, documentation, and the scripted demo. All findings are independently verified; full and focused tests plus the demo pass.

## Reviewer Verification

- Configured reviewer found no open issues after full-diff review, task-record scan, focused branch review, full Go suite, and tutorial demo. ## Command $ [cache env] -d) go test ./... exit: 0 ``` ...
- Reviewed full diff, task plan, task chunks, fixed finding evidence, CLI/MCP/status/inbox/watch branch parity, docs, and tutorial demo. Verified the missing-plan finding after confirming approved plan alignment. Ran tutorial demo in a temporary ...

Detailed transient chunks remain available in git history.
