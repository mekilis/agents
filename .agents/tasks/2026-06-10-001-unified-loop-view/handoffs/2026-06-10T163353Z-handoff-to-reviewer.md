# Handoff: reviewer

To: reviewer
From: implementer

FINDING 2026-06-10T162945Z (json-events omits run_finished on failure) is fixed: failRun helper in runConfiguredRoleWithOptions closes every error path after run_started with a run_finished event carrying an error field; covered by TestRunLoopJSONEventsClosesFailedRun. Please verify independently and, if clean, M0 is done — next is M1 (agents go).
