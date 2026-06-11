# Build a Tiny Todo CLI with Agents

This tutorial creates a small terminal project from scratch with Agents. The
goal is intentionally plain: a dependency-free todo command that can add, list,
complete, and remove items, with tests and a short README.

## Prerequisites

Install `agents` and at least one provider command used by your role config.
The default shared config uses:

- `agent` for implementation
- `codex` for review
- `codex` for coordination

You can change those commands in `.agents/config.yml` after initialization.

Check that the binary is available:

```bash
agents version
```

## Create the Project

Create a new folder and initialize git:

```bash
mkdir tiny-todo
cd tiny-todo
git init
```

Initialize Agents state:

```bash
agents init --share
agents trust
```

Use `--share` when you want `.agents/` task records committed with the project.
Use `--local` instead when the coordination state should stay private.

## Open the Interactive Terminal

Start Agents:

```bash
agents
```

Chat with the coordinator in plain text:

```text
I want to build a tiny dependency-free todo CLI from scratch.
It should run from the terminal.
It should support add, list, done, and remove.
Please include tests and a short README.
```

The coordinator can ask questions if the scope is unclear. Once the scope is
clear, it routes work to the implementer.

## Let the Loop Run

When the coordinator says the next step is implementation, use:

```text
/run
```

Agents runs the configured roles from `.agents/config.yml`. Implementer output
and reviewer output stream in the terminal, while durable task records are
written under `.agents/`.

If you already know the exact task and want to skip the chat step, use `go`:

```bash
agents go "Tiny todo CLI: build a dependency-free terminal todo app with add, list, done, remove, tests, and README"
```

## Review the Human Boundary

When the loop reaches the human boundary, Agents shows what was asked, what
changed, the findings history, and verification evidence.

Choose one:

```text
a  accept and archive the task
i  iterate with more feedback
x  abandon the task but leave files as-is
```

Agents does not commit for you. After accepting, inspect and commit normally:

```bash
git status
git diff --stat
git add .
git commit -m "Build tiny todo CLI"
```

## Useful Follow-Ups

Check task state:

```bash
agents status
```

Show scriptable status:

```bash
agents status --json
```

Archive a completed active task manually:

```bash
agents archive
```

Install zsh shell completion:

```bash
agents completion install-zsh
exec zsh
```
