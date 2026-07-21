# Plan

State: approved
Updated: 2026-07-21T21:51:16Z

# Fix inbox routing and watch backlog

Objective: close two correctness gaps in `internal/app/inbox.go` without changing the handoff file format or unrelated behavior.

Implementation order:
- First, constrain `parseInboxHandoff` routing-header recognition to the header block ending at the first blank line after `From:`. Body text and quoted handoffs must be inert. Preserve case-insensitive header names and compatibility with role-only and agent-addressed records.
- Add focused parser tests covering body `To:`, `From:`, and `Agent:` lines, a quoted full handoff, casing variants, and existing record forms.
- Then change `runInboxWatch` to emit every pre-existing unconsumed item once before polling, never emit consumed items, and suppress duplicates across later polls.
- Update focused watch tests, the two-harness tutorial wording, and the demo only if its expectations depend on the old startup behavior.

Validation:
- Run focused parser and watch tests.
- Run `go test ./...` with an isolated Go build cache.
- Have the configured reviewer inspect both fixes and independently verify both findings.

Constraints: keep the terminal event format and written handoff format unchanged; do not add orchestration or perform unrelated refactors; keep task records free of machine-local details.
