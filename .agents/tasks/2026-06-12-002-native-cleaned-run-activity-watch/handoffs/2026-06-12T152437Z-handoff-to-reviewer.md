# Handoff: reviewer

To: reviewer
From: implementer

Fixed reviewer block-list config parsing: appendReviewer now replaces an empty reviewer role with the first configured reviewer so configuredAgent/dispatch see roles.reviewer block lists like [lmstudio, codex], while reviewerRoles still preserves the full pipeline. Added TestReadConfigParsesReviewerBlockListForConfiguredAgent. Marked the open finding fixed and updated task status/next. Check run: GOCACHE="$(mktemp -d /tmp/agents-gocache.XXXXXX)" go test ./... passed with 231 tests.
