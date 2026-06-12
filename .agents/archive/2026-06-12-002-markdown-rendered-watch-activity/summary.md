# Markdown-rendered watch activity

status: archived
previous_status: active
archived_at: 2026-06-12T17:54:23Z

Summary:
- decisions: 0
- handoffs: 4
- verification records: 1
- run activity records: 4
- findings open: 0
- findings fixed: 0
- findings verified: 1

## Scope

make agents watch/runs readable for supervision by rendering cleaned activity in a Markdown-aware way for TTY output and by fixing token-fragment persistence so words, code fences, and commands do not appear split apart. Preserve short progress lines. Keep non-TTY/script output stable. Add focused tests for markdown-ish output and token fragments. Note: there are existing uncommitted MCP compatibility edits in internal/app/mcp.go and internal/app/app_test.go; do not revert them.

## Final Status

Fixed the low-severity formatting/readability finding by extracting shared run activity test helpers. Focused activity tests pass after gofmt. Existing MCP compatibility edits remain preserved.

Detailed transient chunks remain available in git history.
