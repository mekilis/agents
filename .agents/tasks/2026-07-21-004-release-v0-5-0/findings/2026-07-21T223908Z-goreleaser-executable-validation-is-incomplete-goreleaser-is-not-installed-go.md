# Finding: GoReleaser executable validation is incomplete: goreleaser is not installed, go-

Status: verified
Severity: medium
Created by: smart

GoReleaser executable validation is incomplete: goreleaser is not installed, go-run attempts for latest and v2.7.0 were killed before reaching config validation, and mise download of goreleaser@2.7.0 was also killed before check output. README and .goreleaser.yaml were manually inspected, and go mod tidy is clean, but release should not proceed until `goreleaser check` and ideally `goreleaser release --snapshot --clean --skip=publish` pass.

## Fixed

By: supervisor
Recorded at: 2026-07-21T22:40:04Z
Evidence: Release requirement corrected by owner decision: GitHub Actions runs GoReleaser authoritatively after the tag; local configuration and workflow inspection are sufficient before push

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-07-21T22:41:54Z
Evidence: Reviewed approved plan and decision that GitHub Actions is authoritative for GoReleaser, inspected .goreleaser.yaml and .github/workflows/release.yml; local GoReleaser execution is no longer a prerequisite before push/tag.
