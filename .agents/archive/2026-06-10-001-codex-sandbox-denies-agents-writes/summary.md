# Codex sandbox denies .agents writes

status: archived
previous_status: active
archived_at: 2026-06-10T11:51:12Z

Summary:
- decisions: 0
- handoffs: 0
- verification records: 1
- findings open: 0
- findings fixed: 0
- findings verified: 1

## Scope

codex exec --sandbox workspace-write blocks writes under .agents/ (protected like .git), so codex reviewers cannot record findings or verifications; fix the codex driver to pass sandbox_workspace_write.writable_roots with the absolute .agents path.

## Final Status

Starting task.

Detailed transient chunks remain available in git history.
