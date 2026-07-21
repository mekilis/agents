# Fix inbox routing and watch backlog

status: archived
previous_status: active
archived_at: 2026-07-21T21:56:37Z

Summary:
- decisions: 0
- handoffs: 2
- verification records: 1
- run activity records: 2
- findings open: 0
- findings fixed: 0
- findings verified: 2

## Scope

Fix inbox handoff parsing so only the header block controls To, From, and Agent routing; change agents watch --as to emit every existing unconsumed backlog item once before polling; update focused tests and the two-harness tutorial/demo only as needed; preserve the handoff file format and avoid unrelated refactors.

## Final Status

Both inbox correctness defects are fixed. Focused parser/watch regressions, full isolated-cache Go suite, and tutorial demo pass; configured reviewer found no issues and verified both findings.

## Reviewer Verification

- Reviewed the full diff, inbox parser/watch sibling branches, write/read/consume parity, task plan and task chunks. Focused parser/watch regressions and full isolated-cache suite passed; no unresolved review findings. ## Command $ [cache env] ...

Detailed transient chunks remain available in git history.
