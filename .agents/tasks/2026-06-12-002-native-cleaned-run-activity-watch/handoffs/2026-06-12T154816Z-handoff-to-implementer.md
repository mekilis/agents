# Handoff: implementer

To: implementer
From: codex

Fix the open coalescer finding: run activity fragment coalescing currently treats all short 1-3 word punctuation-free lines as token fragments, so real progress lines like 'Reading files' and 'Running checks' can be merged or delayed. Refine the heuristic so meaningful short progress/status lines persist as separate messages while token-streamed word fragments still coalesce/dedupe. Add a regression for consecutive short progress lines plus the existing token-fragment shape, rerun go test ./..., mark the finding fixed, and hand back to reviewer.
