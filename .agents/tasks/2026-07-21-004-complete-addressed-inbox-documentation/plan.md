# Plan

State: approved
Updated: 2026-07-21T22:15:31Z

Update README.md with concise examples for registering named agents, addressed handoffs, inbox list/consume, and agent-scoped watch, and update its MCP description to include agent registration and inbox operations. Expand tutorials/agent-inbox-two-harnesses.md to state that each harness checks its inbox at the start of every session or turn, handoffs replace pasted prompts, findings remain broadcast and follow fix then independent verification, and watch emits backlog before new arrivals. Keep the release documentation concise and consistent with actual CLI and MCP behavior. Validate commands against agents help and relevant implementation/tests, inspect Markdown diffs, and run go test ./... with an isolated cache before independent review.

Write this as concise, editable Markdown rather than filling out a form. Make it specific enough that an implementer and reviewer can act on it without guessing.

Include the substance naturally:
- what outcome the user wants and why this plan is in scope
- repo-relative evidence from docs, code, task state, or prior handoffs
- assumptions, open questions, or decisions still needed
- the implementation shape: files, behavior, and action-sized slices
- validation: focused tests, checks, or acceptance criteria
- risks, trade-offs, and constraints such as permissions, boundaries, or actions Agents must avoid
