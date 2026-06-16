# reviewer branch parity discipline

status: archived
previous_status: active
archived_at: 2026-06-16T10:52:57Z

Summary:
- decisions: 1
- handoffs: 4
- verification records: 1
- run activity records: 4
- findings open: 0
- findings fixed: 0
- findings verified: 1

## Scope

Recent handoffs:
- Fixed reviewer full-diff packet untracked-file coverage. Reviewer prompts and local AI reviewer input now include bounded untracked-file diff content plus stat/list context. Added focused tests proving untracked source files appear in both ...
- Fix the open medium finding: reviewer full-diff packet omits untracked files. The new reviewer/local AI diff helpers use `git diff --no-ext-diff HEAD`, which excludes untracked additions. Preserve surgical scope. Include untracked files ...

Decisions:
- Scope is reviewer branch-parity discipline: add full-diff first-pass behavior, finding-scoped surrounding-function/sibling-branch verification, explicit branch checklist output, ordered reviewer config documentation, and fixture coverage for a sibling branch missing a guard.

Verification:
- Reviewed working diff, task chunks, fixed finding evidence, reviewer/local reviewer prompt paths, untracked diff handling, and tests. Verified fixed finding 2026-06-16T104338Z-reviewer-full-diff-packet-omits-untracked-files.md. No unresolved issues found. ## Command $ [cache env] go test ...

## Final Status

Implemented reviewer branch-parity discipline in reviewer packets and local first-pass review; focused tests, go vet ./..., and GOCACHE-isolated go test ./... pass.

## Reviewer Verification

- Reviewed working diff, task chunks, fixed finding evidence, reviewer/local reviewer prompt paths, untracked diff handling, and tests. Verified fixed finding 2026-06-16T104338Z-reviewer-full-diff-packet-omits-untracked-files.md. No unresolved issues found. ## Command $ [cache env] go test ...

Detailed transient chunks remain available in git history.
