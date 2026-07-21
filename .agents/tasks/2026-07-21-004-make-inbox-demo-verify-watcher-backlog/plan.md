# Plan

State: approved
Updated: 2026-07-21T22:03:53Z

# Make the inbox demo verify watcher backlog

Objective: turn the watcher portion of `tutorials/agent-inbox-demo.sh` into an observable release check rather than a best-effort background process.

Implementation:
- Start `agents watch --as codex-review` with the existing reviewer handoff already unconsumed.
- Wait with a bounded timeout for one JSONL event, validate `event`, `agent`, and the expected handoff text, then print the event into the demo transcript.
- Make missing or mismatched output fail the script, and reliably stop the watcher and remove temporary state on success or failure.
- Keep the rest of the demo flow and production code unchanged.

Validation:
- Run the demo from a temporary workspace with a freshly built binary.
- Run relevant focused tests and `go test ./...` with an isolated cache.
- Have the configured reviewer inspect the shell behavior and verify the finding.

Constraints: keep the change narrow, portable to the repository's supported shell environment, and free of machine-local details in task records.
