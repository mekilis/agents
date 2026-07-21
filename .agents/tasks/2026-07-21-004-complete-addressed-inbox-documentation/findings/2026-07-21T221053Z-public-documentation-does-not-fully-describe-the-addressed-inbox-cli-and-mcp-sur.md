# Finding: Public documentation does not fully describe the addressed-inbox CLI and MCP sur

Status: verified
Severity: medium
Created by: supervisor

Public documentation does not fully describe the addressed-inbox CLI and MCP surface or the required two-harness operating pattern

## Fixed

By: implementer
Agent: agent (model gpt-5.5-low)
Recorded at: 2026-07-21T22:12:54Z
Evidence: Updated README.md and tutorials/agent-inbox-two-harnesses.md with addressed-inbox CLI/MCP coverage, session-start inbox checks, handoff replacement, findings fix/verify lifecycle, and watch backlog behavior; validated agents help and ran GOCACHE=$(mktemp -d) go test ./...

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-07-21T22:14:52Z
Evidence: Reviewed full Markdown diff, task plan/state, CLI top-level usage, inbox/handoff/watch/MCP implementation and tests; ran GOCACHE=$(mktemp -d) go test ./... successfully.
