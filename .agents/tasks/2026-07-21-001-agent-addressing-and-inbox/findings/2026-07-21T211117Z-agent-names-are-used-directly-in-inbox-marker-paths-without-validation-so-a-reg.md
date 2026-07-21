# Finding: Agent names are used directly in inbox marker paths without validation, so a reg

Status: verified
Severity: medium
Created by: reviewer
Agent: codex (model gpt-5.5, reasoning low)

Agent names are used directly in inbox marker paths without validation, so a registered name containing path separators or dot-dot segments can make consume state write outside the intended inbox/<agent>/ subtree; reject unsafe agent names before persisting them.

## Fixed

By: implementer
Agent: agent (model gpt-5.5-low)
Recorded at: 2026-07-21T21:14:29Z
Evidence: addRegisteredAgent now rejects names outside letters/digits/dot/underscore/hyphen before persistence, with TestAgentRegistrationRejectsUnsafeNames covering path separators, spaces, and dot-dot; GOCACHE=$(mktemp -d) go test ./... passed.

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-07-21T21:16:11Z
Evidence: Reviewed addRegisteredAgent, validateAgentName, consumeInboxItem, markInboxConsumed, and inboxConsumed: persisted agent names are constrained before agents.json writes, and consume paths are reachable only through registeredAgentByName/listInboxItems; TestAgentRegistrationRejectsUnsafeNames covers separators, spaces, dot-dot, and a safe name; full isolated-cache go test ./... passed.
