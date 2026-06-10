# Finding: json-events omits run_finished when a role command fails

Status: verified
Severity: medium
Created by: reviewer
Agent: codex (model gpt-5.5, reasoning low)

json-events omits run_finished when a role command fails

## Fixed

By: implementer
Recorded at: 2026-06-10T16:33:44Z
Evidence: runConfiguredRoleWithOptions now emits run_finished with an error field on every post-run_started error path (command failure, output directives, state analysis); new TestRunLoopJSONEventsClosesFailedRun asserts run-id pairing and error payload; go test ./... 119 pass, gofmt/vet clean

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-06-10T16:35:19Z
Evidence: Reviewed internal/app/app.go: runConfiguredRoleWithOptions emits run_started only after artifacts are ready, then all command/output-directive/state-analysis error paths return via failRun, which emits run_finished with the same run id and error. Regression TestRunLoopJSONEventsClosesFailedRun parses --json-events output and asserts failed run_started/run_finished pairing and error payload. Focused test passed: GOCACHE=/private/tmp/agents-review-gocache go test ./internal/app -run 'TestRunLoopJSONEventsClosesFailedRun|TestRunLoopJSONEvents|TestRunRejectsJSONEventsCombinations' -count=1.
