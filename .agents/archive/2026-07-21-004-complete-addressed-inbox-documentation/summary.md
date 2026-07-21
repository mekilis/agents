# Complete addressed inbox documentation

status: archived
previous_status: active
archived_at: 2026-07-21T22:16:08Z

Summary:
- decisions: 0
- handoffs: 2
- verification records: 1
- run activity records: 2
- findings open: 0
- findings fixed: 0
- findings verified: 1

## Scope

Update public Markdown for the addressed-inbox release: README CLI and MCP coverage, plus the two-harness session workflow, handoff replacement, and fix-then-independent-verification lifecycle. No production-code changes.

## Final Status

README and two-harness documentation completed; independent review and isolated-cache tests passed

## Reviewer Verification

- Reviewed README.md and tutorials/agent-inbox-two-harnesses.md diff against the approved plan, task chunks, CLI top-level usage, inbox/handoff/watch/MCP implementation, and related tests; no unresolved issues found. ## Command $ [cache env] -d) go test ./... ...

Detailed transient chunks remain available in git history.
