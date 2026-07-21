# Finding: Inbox watch mode has no focused test coverage even though the scope calls for wa

Status: verified
Severity: low
Created by: reviewer
Agent: codex (model gpt-5.5, reasoning low)

Inbox watch mode has no focused test coverage even though the scope calls for watch emission tests; add a deterministic test that starts agents inbox --as NAME --watch, creates a new unconsumed matching handoff, and asserts one stable JSON line is emitted without replaying existing items.

## Fixed

By: implementer
Agent: agent (model gpt-5.5-low)
Recorded at: 2026-07-21T21:14:29Z
Evidence: TestInboxWatchEmitsOnlyNewMatchingItem deterministically starts watch after an existing inbox item, adds nonmatching and one matching direct handoff, and asserts the stable JSON event for the new item; GOCACHE=$(mktemp -d) go test ./... passed.

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-07-21T21:16:11Z
Evidence: Reviewed runInboxWatch and TestInboxWatchEmitsOnlyNewMatchingItem: test seeds existing implementer work, starts watch, adds nonmatching handoffs plus one matching direct handoff, and asserts one JSON inbox_item event for the new item; full isolated-cache go test ./... passed.
