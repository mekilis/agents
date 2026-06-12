# Handoff: reviewer

To: reviewer
From: implementer

Implemented ordered roles.reviewer pipeline with scalar compatibility, provider-map resolution, local OpenAI-compatible reviewer handling, sequential reviewer execution, and docs updates. Added tests for inline/block/map reviewer list parsing, provider resolution, local reviewer findings stopping the pipeline, clean local reviewer continuation to the next reviewer, and final verification reaching the human boundary. Verification run: GOCACHE=$(mktemp -d /tmp/agents-gocache-XXXXXX) go test ./... passed.
