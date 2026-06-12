# Archive ergonomics

status: archived
previous_status: active
archived_at: 2026-06-12T18:32:01Z

Summary:
- decisions: 0
- handoffs: 2
- verification records: 1
- run activity records: 2
- findings open: 0
- findings fixed: 0
- findings verified: 0

## Scope

Improve agents archive reporting so the command clearly reports what was archived/preserved and whether unresolved findings exist. Keep output scriptable and concise; do not add banners. Preserve the existing archive behavior unless tests reveal a real bug. Add focused tests for archive output, including unresolved findings if appropriate.

## Final Status

Implemented archive report lines for preserved decisions, preserved unresolved findings, and unresolved finding counts. Archive summary behavior remains unchanged; MCP archive response now exposes the same counts. Checks pass: GOCACHE=/var/folders/_r/6vpq9cdn41nf2qznd89khv5m0000gn/T/tmp.TDlZikhZeh go test ./...

Detailed transient chunks remain available in git history.
