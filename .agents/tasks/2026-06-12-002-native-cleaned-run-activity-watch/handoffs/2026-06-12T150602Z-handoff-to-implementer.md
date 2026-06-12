# Handoff: implementer

To: implementer
From: coordinator

Implement native cleaned run activity watch. Build on the existing managed loop events and agents-progress parsing. Persist cleaned structured run activity under the active task at .agents/tasks/<task>/runs/<run-id>.jsonl, not in transient .agents/runs/ and not as findings/status/verification duplicates. Add agents watch to follow the active task and role switches, synthesizing elapsed time without persisting heartbeat spam. Add agents runs for recent cleaned run history, with --follow sharing the watch behavior if that fits the CLI. Keep agents status scriptable and banner-free. Update README/help/completion as needed. Add focused tests for record writing, cleaned output/no raw progress leakage, failed run_finished records, watch/runs display, no persisted heartbeat events, and archive cleanup/summary implications.
