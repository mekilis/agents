# Plan

State: approved
Updated: 2026-07-21T22:44:36Z

Review every commit and user-facing change from v0.4.3 through HEAD, confirm v0.5.0 is the correct semantic version, verify README and tutorial coverage, run the scripted two-harness demo with a freshly built binary, and run gofmt, build, vet, test, and race checks with isolated caches where applicable. Inspect the checked-in GoReleaser configuration and release workflow locally; GitHub Actions is the authoritative GoReleaser execution environment, so no local GoReleaser installation or snapshot is required. The reviewer independently verifies the race fix and release evidence. After verification, the supervisor commits and archives the release task, reruns final gates, pushes main, waits for branch CI success, creates annotated tag v0.5.0 at the verified clean commit, pushes it, and confirms the GitHub release workflow and downloadable artifacts. Do not tag before branch CI succeeds.

Write this as concise, editable Markdown rather than filling out a form. Make it specific enough that an implementer and reviewer can act on it without guessing.

Include the substance naturally:
- what outcome the user wants and why this plan is in scope
- repo-relative evidence from docs, code, task state, or prior handoffs
- assumptions, open questions, or decisions still needed
- the implementation shape: files, behavior, and action-sized slices
- validation: focused tests, checks, or acceptance criteria
- risks, trade-offs, and constraints such as permissions, boundaries, or actions Agents must avoid
