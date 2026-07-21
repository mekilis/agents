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
watch_pid=""
cleanup_watch() {
	if [ -n "$watch_pid" ]; then
		kill "$watch_pid" 2>/dev/null || true
		wait "$watch_pid" 2>/dev/null || true
	fi
	rm -f "$watch_log"
}
trap cleanup_watch EXIT

agents watch --as codex-review > "$watch_log" &
watch_pid=$!
for _ in $(seq 1 50); do
	if [ -s "$watch_log" ]; then
		break
	fi
	sleep 0.1
done
python3 - "$watch_log" <<'PY'
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
lines = path.read_text().splitlines()
if not lines:
    raise SystemExit("agents watch --as codex-review did not emit the backlog event")
event = json.loads(lines[0])
if event.get("event") != "inbox_item":
    raise SystemExit(f"unexpected watcher event type: {event.get('event')!r}")
if event.get("agent") != "codex-review":
    raise SystemExit(f"unexpected watcher agent: {event.get('agent')!r}")
text = event.get("item", {}).get("text")
if text != "Review the consumed inbox and verification state":
    raise SystemExit(f"unexpected watcher handoff text: {text!r}")
print(lines[0])
PY
agents inbox --as codex-review --json
agents status --json
