# Handoff: implementer

To: implementer
From: codex

Fix the open finding: agents run --once must persist cleaned task run activity for the role it runs, including reviewer runs. Wire the activity sink into the runLoopOnce path without breaking JSON/status behavior. Add a regression that a one-shot reviewer/implementer run writes .agents/tasks/<task>/runs/<run-id>.jsonl and appears in agents watch/runs. Rerun go test ./..., mark the finding fixed, and hand back to reviewer.
