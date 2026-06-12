# Handoff: implementer

To: implementer
From: smart

Implement native cleaned run activity so users can watch Agents work without tailing raw provider logs. Persist cleaned structured run activity under the active task, preferably .agents/tasks/<task>/runs/<run-id>.jsonl. Do not duplicate semantic records like findings/verification/status. Avoid recording noisy heartbeats; synthesize elapsed time in watch output. Add a native command such as agents watch or agents runs --follow that follows the active task and role switches. Keep agents status scriptable. Add tests.
