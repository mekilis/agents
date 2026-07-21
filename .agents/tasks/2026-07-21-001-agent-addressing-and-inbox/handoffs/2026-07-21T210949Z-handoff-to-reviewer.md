# Handoff: reviewer

To: reviewer
From: implementer

Implemented task-scoped named agent registration, addressed handoffs with optional target agents, inbox list/consume/watch behavior, MCP parity, status per-agent unconsumed counts, focused tests, two-harness docs, and a scripted tutorial demo. Checks: GOCACHE=$(mktemp -d) go test ./... passed with 283 tests across 2 packages.
