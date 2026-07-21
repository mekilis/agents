# Finding: Inbox watch startup marks the existing unconsumed backlog seen instead of emitti

Status: verified
Severity: high
Created by: supervisor

Inbox watch startup marks the existing unconsumed backlog seen instead of emitting it, leaving a delivery race between list and watch startup

## Fixed

By: implementer
Recorded at: 2026-07-21T21:49:52Z
Evidence: runInboxWatch now emits all existing unconsumed items once before polling, skips consumed items, and tracks seen IDs; TestInboxWatchEmitsExistingAndNewMatchingItems passes.

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-07-21T21:52:04Z
Evidence: Reviewed runInboxWatch with list/consume sibling paths: startup list now emits each matching unconsumed item before marking it seen, skips consumed items, then suppresses duplicates while later polls emit new matching items. Focused watch regression and full go test ./... passed with isolated GOCACHE.
