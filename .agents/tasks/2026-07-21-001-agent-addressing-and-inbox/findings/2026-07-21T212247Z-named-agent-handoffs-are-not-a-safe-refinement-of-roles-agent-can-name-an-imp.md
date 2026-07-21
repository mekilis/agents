# Finding: Named-agent handoffs are not a safe refinement of roles: --agent can name an imp

Status: verified
Severity: medium
Created by: supervisor

Named-agent handoffs are not a safe refinement of roles: --agent can name an implementer while --to says reviewer, and the direct inbox still accepts it. Validate that an addressed agent's registered role matches the handoff role, or infer the role from the named target, and replace the current mismatch-positive test with role-consistent addressing plus mismatch rejection.

## Fixed

By: implementer
Agent: agent (model gpt-5.5-low)
Recorded at: 2026-07-21T21:29:43Z
Evidence: addHandoff now rejects --agent targets whose registered role does not match --to, while role-only handoffs remain unchanged; TestAddressedHandoffRequiresRegisteredAgent covers unregistered rejection, mismatch rejection, and role-consistent success; full isolated-cache go test ./... passed.

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-07-21T21:33:43Z
Evidence: Reviewed addHandoff, parseHandoffArgs, registeredAgentByName, readRegisteredAgents, parseInboxHandoff, listInboxItems, and TestAddressedHandoffRequiresRegisteredAgent. Direct --agent handoffs now require registration and reject role mismatches before writeActiveTaskChunk; role-only handoffs still write without registry checks.
