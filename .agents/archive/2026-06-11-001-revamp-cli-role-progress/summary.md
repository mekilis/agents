# Revamp CLI role progress

status: archived
previous_status: active
archived_at: 2026-06-11T12:41:40Z

Summary:
- decisions: 1
- handoffs: 0
- verification records: 1
- findings open: 0
- findings fixed: 0
- findings verified: 0

## Scope

Implement the next UI revamp slice for the CLI-first workflow: add a structured progress protocol for roles, render progress updates in interactive /run without scraping raw provider output, keep raw stdout/stderr in logs, and preserve scriptable agents run output.

## Final Status

JSON agents-progress protocol implemented: interactive /run streams sentence-case progress lines from fenced JSON blocks, removes those blocks from final visible summaries, and prompts roles to emit JSON progress updates.

Detailed transient chunks remain available in git history.
