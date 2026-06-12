# Finding: Reviewer block-list config is parsed as unconfigured for dispatch/run. In readCo

Status: verified
Severity: high
Created by: codex

Reviewer block-list config is parsed as unconfigured for dispatch/run. In readConfig, 'reviewer:' creates an empty cfg.Roles["reviewer"], then appendReviewer does not replace it because the key exists, so configuredAgent/dispatch see an empty reviewer even though ReviewerPipeline has entries. This blocks the reviewer pipeline for config like roles.reviewer: [lmstudio, codex] in block-list form.

## Fixed

By: implementer
Agent: agent (model gpt-5.5-low)
Recorded at: 2026-06-12T15:24:22Z
Evidence: Updated appendReviewer to expose the first configured reviewer when roles.reviewer is a block list, added TestReadConfigParsesReviewerBlockListForConfiguredAgent, and reran GOCACHE="/tmp/agents-gocache.RZqgPk" go test ./... (231 passed).

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-06-12T15:26:53Z
Evidence: Reviewed appendReviewer/readConfig: an empty roles.reviewer placeholder is replaced by the first appended reviewer, and map/list parser paths keep cfg.Roles[reviewer] aligned with ReviewerPipeline[0]. Ran targeted regression TestReadConfigParsesReviewerBlockListForConfiguredAgent successfully.
