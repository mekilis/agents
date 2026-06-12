# Sandbox-friendly full tests

status: archived
previous_status: active
archived_at: 2026-06-12T18:12:59Z

Summary:
- decisions: 1
- handoffs: 4
- verification records: 1
- run activity records: 2
- findings open: 0
- findings fixed: 0
- findings verified: 2

## Scope

Make the full Go test suite friendlier in sandboxed environments. In particular, TestClassifyCoordinatorIntentWithOpenAICompat currently uses httptest.NewServer and can panic/fail when local port binding is unavailable in reviewer sandboxes. Adjust the test or helper behavior so sandbox port-binding limits result in a clear skip or safe fallback, while preserving meaningful coverage when sockets are available. Add focused coverage if appropriate. Keep changes small and do not alter product behavior.

## Final Status

Open reviewer finding was a no-op/no-problem note and has been marked fixed with evidence. Diff remains the small test-only local AI server helper change. Focused local AI tests and full go test ./... pass with isolated GOCACHE.

Detailed transient chunks remain available in git history.
