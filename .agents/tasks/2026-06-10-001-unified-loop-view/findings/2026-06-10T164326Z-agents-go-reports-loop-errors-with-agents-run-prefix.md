# Finding: agents go reports loop errors with agents run prefix

Status: verified
Severity: low
Created by: reviewer
Agent: codex (model gpt-5.5, reasoning low)

agents go reports loop errors with agents run prefix

## Evidence

`runGoCommand` now delegates loop execution to `runLoopToStop(out, errOut, e, maxRuns, nil)` in `internal/app/app.go`, but `runLoopToStop` still formats loop execution errors as `agents: run: %v`. Any loop-time failure after `agents go` creates the task (for example an invalid configured role command or another `runManagedLoop` error) is reported under the wrong command name, which is confusing for the new public entry point.

## Fixed

By: implementer
Recorded at: 2026-06-10T16:45:34Z
Evidence: runLoopToStop now takes the invoking command name and prefixes loop errors with it; agents go reports agents: go: ..., agents run unchanged. New TestGoReportsLoopErrorsAsGo asserts the go prefix and absence of the run prefix. go test ./... 125 pass, gofmt/vet clean

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-06-10T18:24:31Z
Evidence: Reviewed internal/app/app.go: runGoCommand calls runLoopToStop with cmd 'go', runLoopCommand calls it with cmd 'run', and loop errors format agents: <cmd>:. Regression TestGoReportsLoopErrorsAsGo exercises a failing configured implementer command and asserts agents: go: plus absence of agents: run:. Focused test passed with isolated cache: GOCACHE=/private/tmp/agents-review-gocache go test ./internal/app -run 'TestGoReportsLoopErrorsAsGo|TestGoOpensTaskScopeAndHandoffWithoutConfiguredRoles|TestGoRunsLoopToHumanBoundary|TestSplitGoText|TestRunLoopJSONEventsClosesFailedRun' -count=1.
