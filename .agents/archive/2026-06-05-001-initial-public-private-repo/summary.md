# Initial Repository Preparation

status: archived
archived_at: 2026-06-05T08:00:00Z

Summary:
- Prepared Agents for a clean private GitHub first push.
- Added minimal GitHub Actions CI for formatting, build, vet, and race tests.
- Added a tracked `.githooks/pre-commit` for fast local gofmt and vet checks.
- Removed local demo and scratch QA state from the committed tree.
- Excluded local docs and QA notes from git.
- Kept root `.agents/` as public-safe self-coordination state.

Verification:
- `go test ./...` passed.
- `go vet ./...` passed.
- `go build ./...` passed.
