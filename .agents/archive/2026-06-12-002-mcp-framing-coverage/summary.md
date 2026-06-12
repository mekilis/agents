# MCP framing coverage

status: archived
previous_status: active
archived_at: 2026-06-12T18:16:28Z

Summary:
- decisions: 1
- handoffs: 2
- verification records: 1
- run activity records: 1
- findings open: 0
- findings fixed: 0
- findings verified: 1

## Scope

Add focused tests for MCP transport framing. Cover both Content-Length framed JSON-RPC and newline-delimited JSON flows, including initialize plus tools/list behavior. Keep product behavior unchanged unless tests reveal a real bug. Preserve scriptable output and existing MCP protocol compatibility.

## Final Status

Implemented focused MCP framing coverage. Content-Length initialize/tools-list flow now asserts Content-Length response framing, line-delimited initialize/initialized/tools-list flow now asserts line response framing, and both paths assert expected tools/list contents. Checks passed: GOCACHE=/var/folders/_r/6vpq9cdn41nf2qznd89khv5m0000gn/T/tmp.JwkAsqifNk go test ./internal/app -run MCP; GOCACHE=/var/folders/_r/6vpq9cdn41nf2qznd89khv5m0000gn/T/tmp.JwkAsqifNk go test ./...

Detailed transient chunks remain available in git history.
