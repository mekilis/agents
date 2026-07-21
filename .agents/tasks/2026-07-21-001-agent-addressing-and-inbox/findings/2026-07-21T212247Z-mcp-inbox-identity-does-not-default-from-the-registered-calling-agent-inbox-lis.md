# Finding: MCP inbox identity does not default from the registered calling agent: inbox_lis

Status: verified
Severity: medium
Created by: supervisor

MCP inbox identity does not default from the registered calling agent: inbox_list and inbox_consume require an explicit as field, and the round trip does not test actor attribution from registered identity. Make the MCP agent selector optional when AGENTS_ACTOR identifies a registered task agent, use that identity for inbox operations and add_handoff actor attribution by default, preserve explicit overrides where appropriate, and cover the default/override round trips.

## Fixed

By: implementer
Agent: agent (model gpt-5.5-low)
Recorded at: 2026-07-21T21:29:43Z
Evidence: MCP inbox_list and inbox_consume now default as from registered AGENTS_ACTOR, preserve explicit as overrides, and add_handoff uses AGENTS_ACTOR attribution by default; TestMCPAgentInboxRoundTrip covers default list/consume, explicit override, and From attribution; full isolated-cache go test ./... passed.

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-07-21T21:33:43Z
Evidence: Reviewed mcpTools, callMCPTool add_handoff/agent_add/agent_list/inbox_list/inbox_consume, mcpInboxAgent, withBy, resolveActor, and TestMCPAgentInboxRoundTrip. MCP inbox operations default to a registered AGENTS_ACTOR, explicit as overrides remain supported, and add_handoff attribution defaults through resolveActor while still honoring by.
