# Finding: Inbox watch tests race when replacing the package-level sleep hook during watche

Status: verified
Severity: high
Created by: supervisor

Inbox watch tests race when replacing the package-level sleep hook during watcher execution

## Fixed

By: implementer
Recorded at: 2026-07-21T22:40:04Z
Evidence: Guarded sleep-hook replacement and reads with a mutex-backed helper; go test -race ./... now passes

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-07-21T22:41:54Z
Evidence: Reviewed internal/app/inbox.go runInboxWatch plus sleepInboxWatch/setInboxWatchSleep and the two watcher tests; hook reads and replacements are mutex-guarded, focused watcher tests pass, and go test -race ./... passes.
