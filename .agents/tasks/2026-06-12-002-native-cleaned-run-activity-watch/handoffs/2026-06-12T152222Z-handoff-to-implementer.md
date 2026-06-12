# Handoff: implementer

To: implementer
From: codex

Fix reviewer block-list config parsing so roles.reviewer with list entries exposes the first configured reviewer through dispatch/configuredAgent while preserving the full reviewer pipeline. Add a regression for block-list reviewer syntax matching .agents/config.yml, then rerun go test ./... and hand back to reviewer.
