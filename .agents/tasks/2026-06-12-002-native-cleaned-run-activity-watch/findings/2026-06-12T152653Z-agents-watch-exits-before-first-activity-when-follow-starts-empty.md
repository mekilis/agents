# Finding: agents watch exits before first activity when follow starts empty

Status: verified
Severity: medium
Created by: reviewer
Agent: codex (model gpt-5.5, reasoning low)

agents watch exits before first activity when follow starts empty

## Fixed

By: implementer
Agent: agent (model gpt-5.5-low)
Recorded at: 2026-06-12T15:30:43Z
Evidence: Updated printRunActivity so TTY follow with no existing records prints the empty state and then enters follow polling at printed=0; added TestWatchFollowStartingEmptyKeepsPolling and TestFollowRunActivityPollsAfterStartingEmpty; reran GOCACHE="/tmp/agents-gocache.WBnYNT" go test ./... (233 passed).

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-06-12T15:33:28Z
Evidence: Reviewed printRunActivity: when follow=true, len(records)=0, and env.isTTY=true it prints the empty state then calls followRunActivityFunc(out, dir, 0) instead of returning. Reviewed followRunActivityUntilInterrupted polling from printed=0 and ran targeted regressions TestWatchFollowStartingEmptyKeepsPolling and TestFollowRunActivityPollsAfterStartingEmpty successfully with isolated GOCACHE.
