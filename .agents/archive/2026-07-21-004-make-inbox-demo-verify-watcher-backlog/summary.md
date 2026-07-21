# Make inbox demo verify watcher backlog

status: archived
previous_status: active
archived_at: 2026-07-21T22:07:27Z

Summary:
- decisions: 0
- handoffs: 2
- verification records: 1
- run activity records: 2
- findings open: 0
- findings fixed: 0
- findings verified: 1

## Scope

Strengthen tutorials/agent-inbox-demo.sh so the scripted demo fails unless agents watch --as emits the expected pre-existing unconsumed reviewer handoff, and print the captured machine-readable event in the transcript. Keep the change narrow and preserve the current demo flow.

## Final Status

Demo now asserts and prints the watcher backlog event; independent review and all gates passed

## Reviewer Verification

- Reviewed full diff, task plan, watcher script success/failure cleanup paths, and related inbox watch tests. Verified the fixed finding by running a freshly built temp agents binary through tutorials/agent-inbox-demo.sh from a temp ...

Detailed transient chunks remain available in git history.
