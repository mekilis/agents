# Agents

Agents is a terminal for coordinating AI coding agents through task state,
handoffs, findings, decisions, verification, and human approval.

The current build is intentionally local-first: a small CLI, workspace trust,
scriptable task state, and an interactive terminal that routes the same
primitives you can call from scripts.

## Install

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

The completion script comes from the `agents` binary. If you prefer to install
it yourself, write `agents completion zsh` to a directory on your zsh `fpath`.

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
› I want to improve bookmark search, but I am not sure about the exact scope yet.
› It should probably handle uppercase titles and maybe tags too.
› /run
```

Plain text in the interactive terminal is coordinator chat. Slash commands are
reserved for controls, so `/run` asks the coordinator to continue planning,
ask for missing details such as a ref, or hand off to implementation when the
request is clear. If you already know the task, `/start` is still the fast path.

Hide the startup banner when you want a quieter interactive terminal:

```bash
AGENTS_NO_BANNER=1 agents
```

Show scriptable workspace status:

```bash
agents status
agents status --json
```

Open or switch tasks:

```bash
agents start "Fix checkout flow" --ref WEB-123 "Implement the checkout fix and add tests"
agents task open "Fix checkout flow" --ref WEB-123
agents task use tasks/2026-06-05-001-fix-checkout-flow--web-123
```

Record coordination state:

```bash
agents decision add "Reviewers record findings and do not edit code"
agents handoff --to reviewer "Checkout flow is ready for review"
agents finding add "Missing regression test for expired cards" --severity high
agents finding fix tasks/.../findings/2026-06-05T120000Z-missing-regression-test.md
agents finding verify tasks/.../findings/2026-06-05T120000Z-missing-regression-test.md
```

Inspect the loop and route role packets:

```bash
agents run
agents run --json
agents dispatch implementer
agents dispatch reviewer --json
```

When the next role has a configured agent, `agents run` writes transient provider
artifacts under `.agents/runs/`: prompts in `prompts/` and live logs in `logs/`.
It prints a run id and log path before launch, writes an immediate launch marker
to the log, streams provider output to the terminal, and writes the same output
to the log so you can tail it from another terminal. The run log launch marker
records the role, agent, model, and reasoning setting. Built-in agent names are
`cursor`, `cursor-agent`, `agent`, `codex`, and `claude`. Custom commands can
include `{prompt_file}`:

```bash
tail -f .agents/runs/logs/2026-06-05T120000Z-implementer.log
```

```yaml
roles:
  implementer: cursor
  reviewer: codex
  coordinator: codex
settings:
  # Codex CLI:
  #   models: use `codex -m <model>` / `/model`; examples: gpt-5.5, gpt-5.4,
  #           gpt-5-codex, codex-mini-latest
  #   reasoning: minimal, low, medium, high, xhigh
  codex_model: gpt-5.5
  codex_reasoning: low

  # Claude Code:
  #   model aliases: default, best, sonnet, opus, haiku, opusplan,
  #                  sonnet[1m], opus[1m]
  #   effort: low, medium, high, xhigh, max
  #   note: supported effort levels depend on the selected Claude model.
  claude_model: sonnet
  claude_effort: high

  # Cursor Agent:
  #   run `agent models` or `cursor-agent --list-models` for account-specific
  #   choices; current examples from CLI help include gpt-5,
  #   claude-4.6-sonnet-medium, claude-4.6-sonnet-medium-thinking.
  cursor_model: gpt-5
```

Provider-specific settings are passed only to that provider's built-in command:
`codex_*` to `codex`, `claude_*` to `claude`, and `cursor_model` to
`agent`/`cursor-agent`. Leave a setting empty to use the provider default, or
put provider flags directly in a custom role command.

```bash
agents archive
```

## Current Boundaries

Agents records and routes coordination state today, and `agents run` can launch
configured local agent commands for the next role. It does not automate commits,
push branches, or create pull requests. Natural-language top-level commands are
product direction; use the scriptable commands below in the current build.
`agents archive` is the local cleanup boundary for completed task state: it
writes a summary and prunes the active task chunks from `.agents/tasks/`.

Interactive input model:

- Type plain text to chat with the coordinator and build up task context.
- Chat messages are recorded as durable handoffs to `coordinator` under the
  active task, not as transient run logs.
- Pasted multi-line text is recorded as one chat message in the interactive
  composer.
- In an interactive terminal, chat immediately runs the configured coordinator
  so it can ask for constraints, refs, or approval before routing work.
- In scripts or pipes, chat is recorded and `/run` drives the next role.
- Type slash commands for internal controls.
- Type `/` to show the command menu, or a partial command such as `/st` to
  narrow suggestions.
- Unique slash prefixes execute, so `/q` and `/qu` resolve to `/quit`.
- Press Tab in the interactive composer to complete a unique slash command,
  such as `/qu` to `/quit`.
- Shell Tab completion is available through `agents completion install-zsh`.
- Press Ctrl-C to exit the composer with a continuation hint for the active
  session.
- Type `?` for shortcuts and the command menu.

Interactive slash commands available today:

```text
/start "title" [--ref REF] "what to do"
/status
/trust
/init [--share|--local]
/task open "title" [--ref REF]
/task use <task>
/decision add "text"
/handoff --to ROLE "text"
/finding add|fix|verify ...
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
