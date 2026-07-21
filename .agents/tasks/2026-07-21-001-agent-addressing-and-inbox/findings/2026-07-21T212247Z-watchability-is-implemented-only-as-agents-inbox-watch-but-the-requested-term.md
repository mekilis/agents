# Finding: Watchability is implemented only as agents inbox --watch, but the requested term

Status: verified
Severity: medium
Created by: supervisor

Watchability is implemented only as agents inbox --watch, but the requested terminal surface requires agents watch to gain an agent inbox mode or flag. Add a scriptable agents watch --as NAME mode (an inbox alias may remain) that blocks for new unconsumed items and emits one stable machine-readable line per event, with focused command-level coverage and docs using the watch command.

## Fixed

By: implementer
Agent: agent (model gpt-5.5-low)
Recorded at: 2026-07-21T21:29:43Z
Evidence: agents watch --as NAME now delegates to inbox watch mode and emits stable JSON inbox_item lines for new unconsumed items; TestWatchAsEmitsInboxEvent covers the command-level surface and docs/demo now use agents watch --as; demo and full isolated-cache go test ./... passed.

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-07-21T21:33:44Z
Evidence: Reviewed runWatchCommand, parseInboxArgs, runInboxWatch, listInboxItems, inboxConsumed, and TestWatchAsEmitsInboxEvent. agents watch --as NAME sets inbox watch mode, preserves default run-activity watch when no args are passed, and emits stable inbox_item JSON lines for newly matching unconsumed items.
