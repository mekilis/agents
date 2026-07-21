# Finding: The inbox tutorial demo discards its watcher log without asserting or printing t

Status: verified
Severity: medium
Created by: supervisor

The inbox tutorial demo discards its watcher log without asserting or printing the expected backlog event, so a broken watcher can still produce a passing demo

## Fixed

By: implementer
Agent: agent (model gpt-5.5-low)
Recorded at: 2026-07-21T22:03:30Z
Evidence: tutorials/agent-inbox-demo.sh now waits for, validates, and prints the codex-review backlog inbox_item JSON event; isolated demo, focused watcher tests, and go test ./... passed

## Verification

By: codex-review
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-07-21T22:05:32Z
Evidence: Reviewed full diff and watcher sibling paths; rebuilt agents to a temp GOBIN with isolated GOCACHE, ran tutorials/agent-inbox-demo.sh from a temp workspace and observed the codex-review inbox_item JSON event printed; ran focused watcher tests and go test ./... with isolated GOCACHE
