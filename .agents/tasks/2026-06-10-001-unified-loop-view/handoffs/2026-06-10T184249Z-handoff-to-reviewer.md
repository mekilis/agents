# Handoff: reviewer

To: reviewer
From: implementer

FINDING 2026-06-10T183932Z (split view attempted with non-TTY char devices) fixed: view gate extracted to loopViewTerminals requiring IsTerminal on both stdin and stdout; regression test TestLoopViewTerminalsRejectsNonTTYInputs; verified manually that agents go < /dev/null uses plain fallback. Please verify independently — if clean, M2 is done.
