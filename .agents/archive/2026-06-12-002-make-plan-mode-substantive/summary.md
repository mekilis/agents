# Make plan mode substantive

status: archived
previous_status: active
archived_at: 2026-06-12T20:15:50Z

Summary:
- decisions: 3
- handoffs: 8
- verification records: 1
- run activity records: 7
- findings open: 0
- findings fixed: 0
- findings verified: 1

## Scope

Improve Agents plan mode so plans are substantive without being token-heavy. Base the design on current Agents behavior plus research from Cursor Plan Mode, Claude Code plan mode/ExitPlanMode, Codex CLI surfaces, IBM agent-planning guidance, and a local-model critique. The outcome should make coordinator-generated plans include compact discovery evidence, assumptions/questions, implementation slices, acceptance checks, risks, and requested action permissions or constraints, while keeping the canonical plan in .agents task state and preserving non-TTY/scriptable behavior.

## Final Status

Implemented section-aware bounded plan prompt compacting. Long middle sections are compacted with a clear marker while Risks and Execution Constraints remain visible in implementer and reviewer prompts. Focused tests and full Go suite pass.

## Reviewer Verification

- Reviewed plan prompt compacting, draft/create command surface, coordinator/reviewer prompt guidance, README updates, and regression coverage. Verified fixed bounded-plan finding; focused tests and full Go suite passed. ## Command $ [cache env] go ...

Detailed transient chunks remain available in git history.
