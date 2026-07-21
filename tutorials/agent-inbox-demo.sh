#!/usr/bin/env bash
set -euo pipefail

agents init --local
agents task open "Agent inbox demo"
agents agent add cursor-one --role implementer --harness cursor
agents agent add codex-review --role reviewer --harness codex

agents handoff --to implementer --agent cursor-one "Implement the demo change"
agents inbox --as cursor-one --json

agents inbox --as cursor-one --consume
agents inbox --as cursor-one --json

agents finding add "Demo finding recorded by reviewer" --severity low --by codex-review
finding="$(agents status --json | python3 -c 'import glob,json,sys; task=json.load(sys.stdin)["active_task"]["path"]; print(sorted(path.removeprefix(".agents/") for path in glob.glob(f".agents/{task}/findings/*.md"))[0])')"
agents finding fix "$finding" --evidence "Demo fix recorded" --by cursor-one
agents finding verify "$finding" --evidence "Demo verification recorded" --by codex-review

agents handoff --to reviewer --agent codex-review "Review the consumed inbox and verification state"
watch_log="${TMPDIR:-/tmp}/agents-inbox-demo-watch.$$"
agents watch --as codex-review > "$watch_log" &
watch_pid=$!
sleep 0.2
kill "$watch_pid" 2>/dev/null || true
wait "$watch_pid" 2>/dev/null || true
rm -f "$watch_log"
agents inbox --as codex-review --json
agents status --json
