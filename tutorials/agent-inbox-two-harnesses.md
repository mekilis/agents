# Agent Inbox With Two Harnesses

Use named agents when two harnesses are working the same task. The inbox lets
each harness pick up durable task state instead of relying on pasted prompts.

Named agents are task-scoped. Register each harness with the role it handles:

```bash
agents agent add cursor-one --role implementer --harness cursor
agents agent add codex-review --role reviewer --harness codex
agents agent list
```

Each harness should check its inbox at the start of every session or turn. The
inbox includes direct handoffs to the named agent and role handoffs for that
agent's registered role:

```bash
agents inbox --as cursor-one
agents inbox --as cursor-one --json
agents inbox --as cursor-one --consume
```

Handoffs are the replacement for pasted prompts between harnesses. They can
still target a role, which makes the item visible to all registered agents for
that role:

```bash
agents handoff --to implementer "Build the parser change"
```

They can also address one registered agent while preserving the role target:

```bash
agents handoff --to implementer --agent cursor-one "Only cursor-one should take this"
```

After a harness accepts an item, consume it so later turns do not replay stale
work. Consume all currently listed items or one item id:

```bash
agents inbox --as cursor-one --consume
agents inbox --as cursor-one --consume 2026-07-21T120000Z-handoff-to-implementer.md
```

Findings remain broadcast task state, not private inbox messages. Reviewers add
findings, implementers mark them fixed with evidence, and a different actor
performs independent verification:

```bash
agents finding add "Missing inbox regression test" --severity medium
agents finding fix tasks/.../findings/...md --evidence "added test and docs"
agents finding verify tasks/.../findings/...md --evidence "reviewed diff and ran tests"
```

Watcher snippet:

```bash
agents watch --as cursor-one |
while IFS= read -r event; do
  printf 'received %s\n' "$event"
done
```

Watch mode emits one JSON line for each existing unconsumed inbox item, then
continues emitting new unconsumed items as they arrive.
