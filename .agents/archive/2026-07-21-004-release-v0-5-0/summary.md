# Release v0.5.0

status: archived
previous_status: active
archived_at: 2026-07-21T22:44:48Z

Summary:
- decisions: 1
- handoffs: 3
- verification records: 1
- run activity records: 3
- findings open: 0
- findings fixed: 0
- findings verified: 2

## Scope

Release the addressed agent inbox feature as v0.5.0 after verifying the full change set since v0.4.3, documentation, demo, GoReleaser configuration, and clean release state. Push main, require CI success, then tag and verify published artifacts.

## Final Status

v0.5.0 preflight verified: fresh-binary demo, gofmt, build, vet, full tests, and race tests pass; GitHub Actions is authoritative for GoReleaser

## Reviewer Verification

- Reviewed full working tree diff, task plan/chunks, inbox watcher race fix, GoReleaser workflow/config, docs/demo coverage, and verified both fixed findings. Focused watcher tests, full tests, race tests, demo, gofmt check, and diff ...

Detailed transient chunks remain available in git history.
