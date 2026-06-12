# MCP v1

status: archived
previous_status: active
archived_at: 2026-06-12T16:33:45Z

Summary:
- decisions: 0
- handoffs: 4
- verification records: 1
- run activity records: 5
- findings open: 0
- findings fixed: 0
- findings verified: 1

## Scope

add agents mcp server command exposing status, run_once, recent_runs, watch_activity, add_handoff, add_finding, fix_finding, verify_finding, add_decision, and archive tools. Keep it local-first, scriptable, provider-agnostic, and covered by focused tests. Do not implement unsupported IDE orchestration; the MCP server should wrap existing Agents commands/state.

## Final Status

Implemented MCP stdio Content-Length framing fix; focused MCP tests and full go test ./... pass with isolated GOCACHE.

Detailed transient chunks remain available in git history.
