# Finding: plan refine still uses ad hoc text surgery: durablePlanContent strips hardcoded

Status: verified
Severity: medium
Created by: smart

plan refine still uses ad hoc text surgery: durablePlanContent strips hardcoded scaffold strings, and refine appends feedback as a new bullet rather than actually refining the existing free-form plan. Replace this with a structured plan document model or an honest minimal command contract that does not pretend to synthesize. No revise alias/fallback should return.

## Fixed

By: implementer
Agent: agent (model gpt-5.5-low)
Recorded at: 2026-06-12T20:48:51Z
Evidence: Removed durablePlanContent and the feedback-appending refine path. /plan refine now validates an existing plan then fails honestly for free-form plans without mutating plan.md; regression coverage asserts no feedback is persisted and the plan remains unchanged. Checks passed: GOCACHE=/tmp/agents-gocache-refine go test ./internal/app and go test ./...

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-06-12T20:51:51Z
Evidence: Reviewed internal/app/plan.go and searched for durablePlanContent, refinement scaffold text, and feedback appending. refineActiveTaskPlan now validates feedback/existing plan and returns an unsupported free-form refinement error without mutating plan.md. Regression test TestPlanRefineRefusesAutomaticSynthesisAndPreservesPlan covers unchanged plan content; GOCACHE=/tmp/agents-gocache-review go test ./internal/app and go test ./... passed.
