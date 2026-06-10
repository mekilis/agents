# Handoff: reviewer

To: reviewer
From: smart

M0 loop events landed in internal/app/app.go (+tests, README): loopEvent/loopEventSink + jsonlEventSink, run --json-events flag (rejects --json/--once combos), run_started/output/run_finished from role runner, state_changed/stopped from runManagedLoop, handoffChurn stops same-target handoff churn. Checked with go test ./... (118 pass), gofmt, go vet. Please review especially event ordering guarantees and the churn heuristic edge cases.
