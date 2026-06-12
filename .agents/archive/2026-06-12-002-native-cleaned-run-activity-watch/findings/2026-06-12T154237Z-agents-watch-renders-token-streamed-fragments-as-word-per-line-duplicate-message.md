# Finding: agents watch renders token-streamed fragments as word-per-line duplicate message

Status: fixed
Severity: medium
Created by: codex

agents watch renders token-streamed fragments as word-per-line duplicate messages instead of coalescing them into clean activity lines

## Fixed

By: implementer
Agent: agent (model gpt-5.5-low)
Recorded at: 2026-06-12T15:45:09Z
Evidence: Coalesced tiny token-stream fragments in run activity persistence, added TestRunActivityCoalescesTokenStreamFragments for observed word-per-line duplicate shape, and reran GOCACHE="/tmp/agents-gocache.yCS5vd" go test ./... (236 passed).
