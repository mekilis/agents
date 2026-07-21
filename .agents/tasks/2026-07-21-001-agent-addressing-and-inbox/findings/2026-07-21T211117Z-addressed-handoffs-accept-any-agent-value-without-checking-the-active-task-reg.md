# Finding: Addressed handoffs accept any --agent value without checking the active task reg

Status: verified
Severity: medium
Created by: reviewer
Agent: codex (model gpt-5.5, reasoning low)

Addressed handoffs accept any --agent value without checking the active task registry, so typos or removed agent names create direct inbox items that no registered agent can receive; validate direct targets against agents.json or make the unregistered-target behavior explicit and tested.

## Fixed

By: implementer
Agent: agent (model gpt-5.5-low)
Recorded at: 2026-07-21T21:14:29Z
Evidence: addHandoff now resolves --agent through the active task registry before writing, with TestAddressedHandoffRequiresRegisteredAgent covering reject/success; GOCACHE=$(mktemp -d) go test ./... passed.

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-07-21T21:16:11Z
Evidence: Reviewed addHandoff and registeredAgentByName: --agent now resolves against agents.json before writeActiveTaskChunk; TestAddressedHandoffRequiresRegisteredAgent covers missing-target rejection and registered-target success; full isolated-cache go test ./... passed.
