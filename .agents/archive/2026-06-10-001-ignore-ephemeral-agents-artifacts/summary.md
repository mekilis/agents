# Ignore ephemeral .agents artifacts

status: archived
previous_status: active
archived_at: 2026-06-10T12:00:53Z

Summary:
- decisions: 0
- handoffs: 0
- verification records: 1
- findings open: 0
- findings fixed: 0
- findings verified: 4

## Scope

init's .agents/.gitignore only ignores runs/; tmp/ and active are committable. A sandboxed codex reviewer used .agents/tmp/gocache (only writable dot-dir), and 121MB of cache plus the active pointer were committed in a demo repo. Also steer agents away from scratch files in the repo.

## Final Status

Starting task.

Detailed transient chunks remain available in git history.
