# Finding: CLI inbox consumption does not match the requested terminal contract: --consume

Status: verified
Severity: medium
Created by: supervisor

CLI inbox consumption does not match the requested terminal contract: --consume currently requires one item ID and consumes only that item, while agents inbox --as NAME --consume should list the agent's current unconsumed items and mark all returned items consumed idempotently. Preserve an optional single-item form only if useful, but the flag-alone behavior and tests are required.

## Fixed

By: implementer
Agent: agent (model gpt-5.5-low)
Recorded at: 2026-07-21T21:29:43Z
Evidence: agents inbox --as NAME --consume now lists and consumes all currently returned unconsumed items idempotently, while an optional item id still consumes one item; TestCLIInboxConsumeFlagAloneConsumesReturnedItems covers flag-alone and repeated consume behavior; full isolated-cache go test ./... and tutorials/agent-inbox-demo.sh passed.

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-07-21T21:33:43Z
Evidence: Reviewed runInboxCommand, parseInboxArgs, consumeInboxItems, consumeInboxItem, markInboxConsumed, listInboxItems, and TestCLIInboxConsumeFlagAloneConsumesReturnedItems. --consume without an item lists current unconsumed items for the named registered agent, marks each returned item consumed, returns consumed JSON count, and repeated flag-alone consume returns zero items without error.
