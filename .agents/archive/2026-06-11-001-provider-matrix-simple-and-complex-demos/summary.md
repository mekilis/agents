# Provider matrix: simple and complex demos

status: archived
previous_status: active
archived_at: 2026-06-11T21:21:58Z

Summary:
- decisions: 0
- handoffs: 0
- verification records: 0
- findings open: 0
- findings fixed: 0
- findings verified: 0

## Scope

Run real-provider role permutations for one simple demo task and one more complex demo task. Record which coordinator/implementer/reviewer combinations pass, where token usage appears, and any protocol/UI issues found.

## Final Status

Ran two real-provider matrices across Codex, Claude, and Cursor Agent: simple word counter --chars task and complex ledger summary task. All six simple permutations and all six complex permutations produced passing app tests. A harness log in one initial complex rerun was correctly flagged by Codex reviewer; rerunning with logs outside the repo passed. The matrix exposed and fixed one Agents bug: provider output could echo prompt placeholder directive blocks, causing duplicate/failed verification records. Agents now ignores placeholder handoff/verification directives and de-duplicates identical directives in one provider output.

Detailed transient chunks remain available in git history.
