# Plan

State: draft
Updated: 2026-06-12T20:52:18Z

Replace /plan revise with /plan refine without fallback. The code change removes the old revise subcommand from CLI and interactive help, keeps /plan revise rejected, and documents /plan refine as an honest non-mutating command while free-form plans lack a structural rewrite path. Evidence: internal/app/plan.go owns plan subcommand dispatch; internal/app/app.go owns help and loop routing text; internal/app/coordinator.go owns coordinator planning wording; README.md documents public CLI usage; internal/app/app_test.go covers command behavior. Implementation shape already completed by Agents: update command names, reject revise, make refine refuse automatic free-form synthesis without mutating plan.md, and update docs/tests. Validation: reviewer ran GOCACHE=/tmp/agents-gocache-review go test ./internal/app and go test ./..., verified old refine findings, and confirmed no revise alias remains. Risks and constraints: do not add ad hoc phrase-based plan rewriting, do not preserve hidden compatibility fallback, keep plan Markdown free-form, and leave true AI-backed durable refinement as a future structural feature.

Write this as concise, editable Markdown rather than filling out a form. Make it specific enough that an implementer and reviewer can act on it without guessing.

Include the substance naturally:
- what outcome the user wants and why this plan is in scope
- repo-relative evidence from docs, code, task state, or prior handoffs
- assumptions, open questions, or decisions still needed
- the implementation shape: files, behavior, and action-sized slices
- validation: focused tests, checks, or acceptance criteria
- risks, trade-offs, and constraints such as permissions, boundaries, or actions Agents must avoid
