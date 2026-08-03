# Add a supported one-command installer for Agents releases without requiring Git or Go. Implement a POSIX shell installer suitable for 'curl -fsSL https://raw.githubusercontent.com/mekilis/agents/main/install.sh | sh' that detects macOS/Linux and amd64/arm64, downloads the latest or AGENTS_VERSION-selected GitHub release, verifies it against checksums.txt, installs to AGENTS_INSTALL_DIR when set and otherwise chooses a sensible user-writable bin directory or /usr/local/bin with actionable output, and safely replaces an existing binary. Update README install and upgrade documentation. Add meaningful automated tests that do not depend on live network or mutate system directories. Keep Homebrew tap creation out of scope. Review independently and resolve findings.

status: archived
previous_status: active
archived_at: 2026-08-03T13:31:23Z

Summary:
- decisions: 0
- handoffs: 4
- verification records: 2
- run activity records: 4
- findings open: 0
- findings fixed: 0
- findings verified: 1

## Scope

Add a supported one-command installer for Agents releases without requiring Git or Go. Implement a POSIX shell installer suitable for 'curl -fsSL https://raw.githubusercontent.com/mekilis/agents/main/install.sh | sh' that detects macOS/Linux and amd64/arm64, downloads the latest or AGENTS_VERSION-selected GitHub release, verifies it against checksums.txt, installs to AGENTS_INSTALL_DIR when set and otherwise chooses a sensible user-writable bin directory or /usr/local/bin with actionable output, and safely replaces an existing binary. Update README install and upgrade documentation. Add meaningful automated tests that do not depend on live network or mutate system directories. Keep Homebrew tap creation out of scope. Review independently and resolve findings.

## Final Status

Fixed the upgrade-location finding. install.sh now replaces the PATH-resolved agents binary when AGENTS_INSTALL_DIR is unset, README documents upgrade behavior, and focused/full tests pass.

## Reviewer Verification

- Reviewed install.sh, install_test.go, README.md, .goreleaser.yaml, release workflow, and task chunks. Verified the upgrade-location finding independently. Checks passed: shell syntax, focused installer tests, and full Go suite with isolated GOCACHE. ## Command $ ...
- Reviewed install.sh, README.md, install_test.go, release configuration, and task chunks. sh -n install.sh passed; full Go test suite passed with isolated GOCACHE. No findings opened. ## Command $ sh -n install.sh && [cache ...

Detailed transient chunks remain available in git history.
