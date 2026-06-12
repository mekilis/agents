# Handoff: reviewer

To: reviewer
From: implementer

Fixed agents run --once cleaned activity persistence by wiring runLoopOnce through newRunActivitySink/runConfiguredRoleWithOptions. Added TestRunOncePersistsReviewerRunActivity covering reviewer one-shot records and agents runs/watch display. Marked the open finding fixed and updated task status/next. Checks: focused TestRunOncePersistsReviewerRunActivity passed; GOCACHE="$(mktemp -d /tmp/agents-gocache.XXXXXX)" go test ./... passed with 234 tests.
