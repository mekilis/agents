# Agents

Agents is a terminal for coordinating AI coding agents through task state,
handoffs, findings, decisions, verification, and human approval.

The current build is intentionally local-first: a small CLI, workspace trust,
scriptable task state, and an interactive terminal that routes the same
primitives you can call from scripts.

Agents is meant to be the coordination surface even when you launch it from an
IDE, another coding assistant, or a future MCP client. Those clients should set
scope, inspect state, and approve boundaries; the configured Agents roles
should do the implementation and independent review whenever the loop is
available.

## Install

Download archives for tagged releases from
[GitHub Releases](https://github.com/mekilis/agents/releases). Pick the archive
for your OS and CPU, unpack it, and put the `agents` binary somewhere on
`PATH`.

From this repository:

```bash
go install ./cmd/agents
```

If your Go bin directory is on `PATH`, this installs the `agents` command.
Check with:

```bash
agents help
```

To update the local command after pulling or editing this repo, run the same
install command again. If your shell cannot find `agents`, add Go's bin
directory to `PATH`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

Enable zsh completion:

```bash
agents completion install-zsh
exec zsh
```

Check the installed binary version:

```bash
agents version
```

The completion script comes from the `agents` binary. If you prefer to install
it yourself, write `agents completion zsh` to a directory on your zsh `fpath`.

## Release

Releases are published by GoReleaser from version tags:

```bash
git tag v0.4.1
git push origin v0.4.1
```

The `release` workflow builds archives for Linux, macOS, and Windows, attaches
checksums, and publishes them to GitHub Releases. The workflow uses Node 24
compatible GitHub Actions.

## Tutorial

Start with the short tutorial:

- [Build a tiny todo CLI with Agents](tutorials/tiny-todo-cli.md)

It shows the intended flow in a fresh project: initialize `.agents/`, trust the
workspace, chat with the coordinator, let implementer and reviewer roles work,
then accept or iterate at the human boundary.

## Use

Initialize task state in a project:

```bash
agents init
```

Use `agents init --local` if `.agents/` should stay private in that project.
The default `--share` mode is for teams or repos that want task records
committed.
New configs prefill role commands with `agent` for implementation and `codex`
for review/coordination. Edit `.agents/config.yml` if those commands are not on
your `PATH` or if you prefer another provider.

Trust the current workspace without opening the interactive terminal:

```bash
agents trust
agents trust --json
```

Open the interactive terminal:

```bash
agents
```

Build up a feature or bug fix conversationally:

```text
› I want to build a tiny todo CLI, but I am not sure about the exact scope yet.
› It should probably support add, list, done, and remove.
› Dependency-free is fine for the first version.
```

Plain text in the interactive terminal is coordinator chat. Slash commands are
reserved for controls. When a coordinator role is configured, chat immediately
runs the coordinator so it can answer, ask for missing details such as a ref,
or route a clear request to another role. If you already know the task, `/start`
is still the fast path. Use `/run` to drive queued roles until a stop condition,
or `agents run --once` in scripts to step the loop manually.

Hide the startup banner when you want a quieter interactive terminal:

```bash
AGENTS_NO_BANNER=1 agents
```

Show scriptable workspace status:

```bash
agents status
agents status --json
```

One command from intent to a running loop — `agents go` opens a task, records
the scope, hands off to the implementer, and drives the loop until a stop
condition. No role configuration questions are asked of the user:

```bash
agents go "Add /history command"                       # text is title and scope
agents go "History: support /history in the terminal"  # "title: instruction" splits
agents go "Fix flaky test" --ref BUG-7 --max-iterations 8
agents go "Add /history command" --plain               # force plain loop output
agents go --continue "Review the current state"        # continue the active task
agents go --reviewer "Inspect the latest changes"      # start with reviewer, then keep looping
```

On a terminal, `go` renders the unified loop view: two lanes — Building and
Checking — with live agent output, and a status bar tracking iterations and
the findings burn-down. The waiting lane shows why it waits; roles never lead
the UI (they stay in records and logs). Press `q` twice to stop after the
current run, `p` to pause after the current run; endings are translated to
plain language ("Done and independently checked — your call.", "The agents
are stuck — …"). When stdout is not a terminal — CI, pipes — or with
`--plain`, `go` falls back to today's plain loop output automatically.

When the loop reaches the human boundary, the view flips to the
**Ready for you** screen: what was asked (scope), what changed
(`git diff --stat`), the findings history, and the verification evidence —
then one decision. `a` accepts: the task is archived and a commit message is
suggested (agents never commit; that stays behind human approval). `i`
iterates: describe what should change, and the feedback becomes a handoff
that re-enters the loop in the same view. `x` abandons: everything is left
as is and the task stays active.

Like `agents run`, `go` executes commands from `.agents/config.yml`, so it
refuses untrusted workspaces before creating any task state.

Open or switch tasks step by step instead:

```bash
agents start "Fix checkout flow" --ref WEB-123 "Implement the checkout fix and add tests"
agents task open "Fix checkout flow" --ref WEB-123
agents task use tasks/2026-06-05-001-fix-checkout-flow--web-123
```

Maintain the active task's chunks through the CLI instead of hand-editing:

```bash
agents task scope "Checkout service and its API tests; no schema changes"
agents task status "Fix implemented; regression test added; awaiting review"
agents task next "Reviewer verifies the expired-card path"
```

Keep an approved implementation plan with the task when the work needs a
planning boundary:

```bash
agents plan revise "1. Add the CLI flag\n2. Update tests\n3. Refresh docs"
agents plan show
agents plan approve
agents plan clear
```

The canonical plan lives at `.agents/tasks/<task>/plan.md`. Runs may mention
plan updates, but durable plan content belongs to the task. A draft or stale
plan routes the loop to the coordinator for approval or revision before Agents
runs the implementer. Reviewers inspect the current task plan as part of review
and should record findings when the implementation does not match the plan, or
when the plan is stale, missing, or incorrect for the work under review.

Record coordination state:

```bash
agents decision add "Reviewers record findings and do not edit code"
agents handoff --to reviewer "Checkout flow is ready for review"
agents finding add "Missing regression test for expired cards" --severity high
agents finding fix tasks/.../findings/2026-06-05T120000Z-missing-regression-test.md --evidence "added regression test"
agents finding verify tasks/.../findings/2026-06-05T120000Z-missing-regression-test.md --evidence "reran the suite"
agents verify "go test ./... passes" --cmd 'go test ./...'
```

Every record carries an actor identity, resolved from `--by`, then the
`AGENTS_ACTOR` environment variable, then the OS user. When `agents run`
launches a role, it sets `AGENTS_ACTOR` to the role name in the agent's
environment, so records written by agents are attributed automatically.

Actor names alone are not enough: `cursor`, `codex`, and `claude` name tools,
not models. `agents run` also sets `AGENTS_AGENT` to a qualified descriptor —
for example `codex (model gpt-5.5, reasoning high)` — and records carry it as
an `Agent:` line, so it is always inspectable which model produced a decision,
finding, fix, or verification. Run output and `agents dispatch` show the same
qualified descriptor.

Independent verification is enforced, not suggested:

- A finding must be marked `fixed` before it can be `verified`.
- The actor that fixed a finding cannot verify it. `agents finding verify`
  refuses the same actor and asks for a different `--by`/`AGENTS_ACTOR`.
- `agents verify` records verification evidence under `verification/`. With
  `--cmd`, it runs the command and records the exit code and output; a failing
  command records `Result: fail` and exits non-zero.
- Evidence recorded by the `implementer` actor never satisfies the human
  approval boundary: implementers do not verify their own work.

Drive the loop and route role packets:

```bash
agents run                      # run queued roles until a stop condition
agents run --max-iterations 8   # raise the role-run budget (default 6)
agents run --once               # run exactly one role, then stop
agents run --json               # inspect loop state without running anything
agents run --json-events        # run the loop, streaming lifecycle events as JSONL
agents watch                    # show cleaned activity for the active task
agents runs                     # show recent cleaned run history
agents runs --follow            # same cleaned activity view as watch
agents dispatch implementer
agents dispatch reviewer --json
agents mcp                      # serve local Agents tools over MCP stdio
```

`agents run` manages the implementer/reviewer loop until a stop condition:

- the human approval boundary is reached (open and fixed findings are zero and
  independent verification evidence is newer than the latest handoff),
- the next role has no configured agent,
- a role run records no coordination progress (exit code 1, instead of
  churning) — including a run whose only "progress" is another handoff to the
  same target with no findings or verification movement,
- or the role-run budget is exhausted (exit code 1, rerun to continue).

Agents also writes cleaned structured run activity under the active task in
`.agents/tasks/<task>/runs/<run-id>.jsonl`. These records keep role starts,
cleaned agent messages, failures, and finish times without duplicating
findings, verification, decisions, status, or noisy heartbeat output. Use
`agents watch` while work is active, or `agents runs` to review recent cleaned
history.

`agents mcp` starts a local stdio MCP server for clients that want to inspect
and update the same task state without bespoke shell parsing. It exposes tools
for status, one loop step, recent run activity, handoffs, findings, decisions,
and archive; it does not add IDE orchestration or bypass existing Agents
boundaries.

For Agents development itself, prefer letting Agents drive the work:

```bash
agents go "Update run activity docs"
agents watch
agents status
```

The IDE or assistant you are using can still supervise: clarify scope, add
decisions or findings, inspect diffs, and approve commits. The normal
implementation/review path should stay inside the Agents loop so the repo keeps
durable task history and independent verification.

With `--json-events` the loop runs the same way but stdout becomes a JSONL
stream of lifecycle events for scripts and frontends: `state_changed` (loop
state payload), `run_started` / `run_finished` (run id, role, agent), `output`
(live provider output chunks), and `stopped` (reason, run count, final state).
Exit codes match the plain loop.

When the next role has a configured agent, `agents run` still writes transient
provider artifacts under `.agents/runs/`: prompts in `prompts/` and raw live
logs in `logs/`. Those logs are for debugging provider behavior; the native
watch path is the cleaned task run activity. The run log launch marker records
the role, agent, model, and reasoning setting. Built-in agent names are
`cursor`, `cursor-agent`, `agent`, `codex`, and `claude`. Custom commands can
include `{prompt_file}`:

```bash
tail -f .agents/runs/logs/2026-06-05T120000Z-implementer.log
```

```yaml
providers:
  lmstudio:
    type: openai_compatible
    base_url: http://127.0.0.1:1234/v1
    model: google/gemma-3-4b
    timeout_ms: 800
  codex:
    model: gpt-5.5
    reasoning: low
  claude:
    model: ""
    effort: high
  cursor:
    model: gpt-5
roles:
  implementer: cursor
  coordinator: lmstudio
  # A role can be a scalar, a map with per-role overrides, or for reviewer,
  # an ordered list of reviewer providers.
  reviewer:
    - lmstudio
    - provider: codex
      reasoning: high
```

Providers are named by who runs them or how Agents reaches them. Built-in
provider names are `cursor`, `cursor-agent`, `agent`, `codex`, and `claude`;
local OpenAI-compatible providers such as `lmstudio` and `ollama` can be used
for coordinator preflight and as reviewer list items. Models and
provider-specific settings live under `providers`; use `type:
openai_compatible` for local chat completion servers. Per-role
`model`/`reasoning` values override the named provider for that role only. Set
`cmd: my-agent {prompt_file}` as an advanced escape hatch, or put provider
flags directly in a custom role command.
Legacy `settings.local_ai_*`, `settings.codex_*`, `settings.claude_*`, and
`settings.cursor_model` keys still work, but new config should prefer
`providers` and `roles.reviewer`.

```bash
agents archive
```

## Current Boundaries

Agents records and routes coordination state, attributes every record to an
actor, enforces independent verification, and `agents run` manages the
implementer/reviewer loop across configured local agent commands until a stop
condition or the human approval boundary. It does not automate commits, push
branches, or create pull requests — those stay behind human approval.
Natural-language top-level commands are product direction; use the scriptable
commands below in the current build. `agents archive` is the local cleanup
boundary for completed task state: it writes a summary and prunes the active
task chunks from `.agents/tasks/`.

Interactive input model:

- Type plain text to chat with the coordinator and build up task context.
- Chat messages are recorded as durable handoffs to `coordinator` under the
  active task, not as transient run logs.
- Pasted multi-line text is recorded as one chat message in the interactive
  composer.
- In an interactive terminal, chat immediately runs the configured coordinator
  so it can ask for constraints, refs, or approval before routing work.
- Interactive coordinator and role replies render Markdown for headings, lists,
  code blocks, emphasis, and tables. Scriptable commands keep plain text output.
- When a provider reports token usage, Agents shows it as a small `Usage:` line
  after the reply and keeps the raw provider output in the run log.
- Coordinators do not write `.agents/` state directly; Agents records any
  requested role handoff from the parent process.
- In scripts or pipes, chat is recorded and `/run` drives the next role.
- Type slash commands for internal controls.
- Type `/` to show the command menu, or a partial command such as `/st` to
  narrow suggestions.
- Unique slash prefixes execute, so `/q` and `/qu` resolve to `/quit`.
- Press Tab in the interactive composer to complete a unique slash command,
  such as `/qu` to `/quit`.
- The interactive footer shows `Press ? for shortcuts · Type / for commands ·
  Tab completes slash commands`.
- Shell Tab completion is available through `agents completion install-zsh`.
- Press Ctrl-C in the composer or during a running role to stop with a
  continuation hint for the active session.
- Type `?` for shortcuts and the command menu.

Interactive slash commands available today:

```text
/start "title" [--ref REF] "what to do"
/status
/trust
/init [--share|--local]
/task open "title" [--ref REF]
/task use <task>
/task scope|status|next "text"
/plan show|revise|approve|clear ...
/decision add "text"
/handoff --to ROLE "text"
/finding add|fix|verify ...
/verify "text" [--cmd 'shell command']
/dispatch <role>
/run
/archive
/help
/quit
/exit
```

On first launch in a folder, Agents asks whether to trust that workspace. Trust
is local machine state and is not committed to the repo.

## Project State

This repository commits its root `.agents/` directory on purpose so Agents can
work on itself through visible task records. Other projects can choose whether
to commit `.agents/` for team-visible coordination or ignore it for private
scratch work.

For role labels, use `implementer`, `reviewer`, and `coordinator` in
`.agents/config.yml`:

```yaml
roles:
  implementer: agent
  reviewer: codex
  coordinator: codex
```
