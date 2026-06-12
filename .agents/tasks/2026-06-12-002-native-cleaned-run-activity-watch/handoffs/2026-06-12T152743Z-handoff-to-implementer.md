# Handoff: implementer

To: implementer
From: codex

Fix the open medium finding: agents watch exits immediately when follow starts with no existing activity. In TTY follow mode, watch should print the empty-state message and then continue polling until interrupted or new run activity arrives; non-TTY behavior can remain bounded. Add a regression for empty follow behavior without hanging tests, rerun go test ./..., mark the finding fixed, and hand back to reviewer.
