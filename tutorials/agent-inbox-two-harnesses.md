# Agent Inbox With Two Harnesses

Named agents are task-scoped. Register each harness with the role it handles:

```bash
agents agent add cursor-one --role implementer --harness cursor
agents agent add codex-review --role reviewer --harness codex
agents agent list
```

Handoffs can still target a role:

```bash
agents handoff --to implementer "Build the parser change"
```

They can also address one registered agent while preserving the role target:

```bash
agents handoff --to implementer --agent cursor-one "Only cursor-one should take this"
```

Each harness reads its own inbox. The inbox includes direct handoffs to the
named agent and role handoffs for that agent's registered role:

```bash
agents inbox --as cursor-one
agents inbox --as cursor-one --json
agents inbox --as cursor-one --consume
```

Watcher snippet:

```bash
agents watch --as cursor-one |
while IFS= read -r event; do
  printf 'received %s\n' "$event"
done
```

Watch mode emits one JSON line per new unconsumed inbox item.
