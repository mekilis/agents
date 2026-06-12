# Finding: plan refine currently appends a separate 'Refinement feedback to apply' block an

Status: verified
Severity: medium
Created by: smart

plan refine currently appends a separate 'Refinement feedback to apply' block and generic guidance instead of producing a refined durable plan. This preserves a meta-note/scaffold in plan.md and does not satisfy the intended behavior: refine should fold user feedback into the free-form plan content or fail honestly if it cannot do so; no fallback text-setter or note-appender behavior.

## Fixed

By: implementer
Agent: agent (model gpt-5.5-low)
Recorded at: 2026-06-12T20:45:27Z
Evidence: Updated plan refine to fail when no plan exists, strip draft/meta scaffolding from existing plan text, and fold feedback into durable free-form plan content. Regression test rejects persisted refinement/meta scaffold and confirms feedback remains in plan content. Checks passed: GOCACHE=/tmp/agents-gocache.* go test ./internal/app and go test ./...

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-06-12T20:51:51Z
Evidence: Reviewed internal/app/plan.go and app_test.go. /plan refine no longer writes a 'Refinement feedback to apply' block, generic scaffold guidance, or any feedback into plan.md; it refuses automatic synthesis for free-form plans and preserves the existing plan. GOCACHE=/tmp/agents-gocache-review go test ./internal/app and go test ./... passed.
