# Finding: run activity coalescer merges short meaningful progress lines

Status: verified
Severity: medium
Created by: reviewer
Agent: codex (model gpt-5.5, reasoning low)

The new coalescer treats any line up to 32 runes with 1-3 words and no punctuation as a token fragment, so real short progress/status lines such as "Reading files" or "Running checks" are held in `pending` instead of being persisted. If another short progress line follows, both are concatenated and only flushed at run finish as one synthetic message like "Reading files Running checks"; if a later full line does not contain the pending text, the stale pending text is emitted immediately before that unrelated line. This violates the requirement to coalesce tiny token-stream fragments while preserving meaningful progress/status lines.

Evidence: `internal/app/run_activity.go:116` sends every `isTinyRunActivityFragment` line to pending, and `internal/app/run_activity.go:146` classifies all 1-3 word, <=32-rune, punctuation-free lines as fragments. The regression only covers fragments followed by a full duplicate sentence, not consecutive short progress updates.

## Fixed

By: implementer
Agent: agent (model gpt-5.5-low)
Recorded at: 2026-06-12T15:50:00Z
Evidence: Refined run activity fragment detection to only hold single-word token chunks, added TestRunActivityPreservesShortProgressLines for consecutive short progress lines plus token-fragment coalescing, and reran focused tests plus go test ./... (237 passed).

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-06-12T15:52:15Z
Evidence: Reviewed run_activity coalescing: isTinyRunActivityFragment now only holds single-word punctuation-free chunks, so short multi-word progress lines persist immediately. Ran focused regressions TestRunActivityCoalescesTokenStreamFragments and TestRunActivityPreservesShortProgressLines successfully, and ran go test ./... -skip OpenAICompat with isolated GOCACHE successfully; unskipped go test ./... is blocked here by sandbox localhost bind denial in OpenAICompat httptest.
