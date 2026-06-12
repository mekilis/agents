# Agent Instructions

Treat every committed file as public history.

## Product Language

- Product name: Agents.
- CLI name: `agents`.
- State directory: `.agents/`.
- Use plain nouns: task, agent, role, handoff, finding, decision,
  verification, approval, status, archive, history, run, loop.
- Do not introduce old experimental product names or metaphors as public
  product language.

## Repository State

- Root `.agents/` is committed on purpose. It is Agents working on itself.
- Local trust state must stay outside the repo in the user's home directory.
- Keep machine-local paths, temporary directories, private repo names, and
  personal environment details out of committed task records.

## Task State Cleanup

- Commit useful `.agents/` task chunks while work is active so humans and
  agents can share durable state.
- Before starting a new task, clean up the completed task state:
  summarize what matters, preserve unresolved findings, preserve decisions that
  still apply, and prune transient notes from the working tree.
- Rely on git history for detailed past chunks after they have been committed.
- Do not let live `.agents/` accumulate old handoffs, verification scratch, or
  status notes that no longer help the next task.

## Development

- Main binary: `cmd/agents/main.go`.
- App logic: `internal/app/`.
- Agents should build Agents. For implementation and review work in this repo,
  prefer opening a task and driving the configured roles with `agents go`,
  `agents run`, `agents watch`, and the task/finding/verification commands.
- The IDE or chat agent in use should act as the human supervisor: set scope,
  inspect status, add decisions, approve boundaries, and commit. It should not
  silently replace the configured Agents implementer/reviewer loop for normal
  product work.
- Direct IDE edits are acceptable for small instruction-only changes, emergency
  fixes, or when Agents itself is broken. Record the reason in task state when
  the work is more than trivial.
- Run tests with an isolated Go build cache when sandboxed:

```bash
GOCACHE=<isolated-cache> go test ./...
```

- After changing CLI behavior, reinstall the local development binary when
  needed:

```bash
GOCACHE=<isolated-cache> GOBIN=<user-go-bin> go install ./cmd/agents
```

## Product Rules

- Start from the visible terminal experience.
- Keep `agents status` scriptable and banner-free.
- Keep the interactive banner interactive-only.
- Do not add unsupported agent orchestration. Prefer honest missing behavior
  over speculative automation.
- Record meaningful work in `.agents/` chunks when it helps the next agent or
  human continue.
- Treat MCP, IDEs, and editor extensions as clients of Agents, not the core
  implementation surface. The durable source of coordination truth is
  `.agents/` plus git history.
