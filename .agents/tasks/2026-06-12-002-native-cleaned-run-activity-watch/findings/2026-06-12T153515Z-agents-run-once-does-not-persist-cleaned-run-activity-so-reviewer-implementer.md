# Finding: agents run --once does not persist cleaned run activity, so reviewer/implementer

Status: verified
Severity: high
Created by: codex

agents run --once does not persist cleaned run activity, so reviewer/implementer one-shot runs cannot be watched with agents watch even though they are real role runs

## Fixed

By: implementer
Agent: agent (model gpt-5.5-low)
Recorded at: 2026-06-12T15:37:25Z
Evidence: Wired runLoopOnce through newRunActivitySink/runConfiguredRoleWithOptions so agents run --once persists cleaned task run activity, added TestRunOncePersistsReviewerRunActivity covering reviewer records plus agents runs/watch display, and reran GOCACHE="/tmp/agents-gocache.p7sTT5" go test ./... (234 passed).

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-06-12T15:47:57Z
Evidence: Reviewed runLoopOnce wiring through newRunActivitySink and runConfiguredRoleWithOptions, checked TestRunOncePersistsReviewerRunActivity coverage for reviewer one-shot records plus watch/runs output, and ran the targeted regression successfully with isolated GOCACHE.
