# Plan

State: approved
Updated: 2026-07-21T21:36:31Z

# Agent addressing and inbox

Objective: add task-scoped named agents and addressed handoff inbox semantics while preserving existing role routing and keeping findings and decisions broadcast.

Implementation shape:
- Extend the existing Markdown handoff records with an optional named-agent target and store task agent registration plus consumption markers under the active task.
- Add scriptable CLI commands for agent add/list/remove, flag-alone inbox consumption, and `agents watch --as NAME`; keep status banner-free and include per-agent unconsumed counts.
- Validate named targets against the registry and require the target agent's registered role to match the handoff role while preserving role-only handoffs.
- Expose equivalent MCP operations, default inbox identity and handoff attribution from a registered calling actor, and retain explicit overrides.
- Document the two-harness pattern and provide a runnable tutorial demo.

Validation:
- Focused coverage for agent-vs-role filtering, idempotent consumption, safe target/name validation, one-line watch emission without replay, status counts, and MCP round trips.
- Run the scripted demo and the full isolated-cache go test ./... suite.

Risks and constraints:
- Preserve backward-compatible role handoffs and archive/history behavior.
- Keep task paths safe and delivery explicit; do not add web UI, transport, auth, threads, or automatic agent orchestration.
- Keep machine-local and private details out of committed task records.
