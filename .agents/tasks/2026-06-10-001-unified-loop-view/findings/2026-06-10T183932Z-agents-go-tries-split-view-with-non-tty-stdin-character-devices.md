# Finding: agents go tries split view with non-TTY stdin character devices

Status: verified
Severity: medium
Created by: reviewer
Agent: codex (model gpt-5.5, reasoning low)

agents go tries split view with non-TTY stdin character devices

## Evidence

`runGoCommand` gates the split view on `e.isTTY`, `in.(*os.File)`,
`out.(*os.File)`, and `term.IsTerminal(outFile.Fd())`, but it never checks
`term.IsTerminal(inFile.Fd())` before calling `runLoopWithView`. `e.isTTY`
comes from `isTerminalReader`, which only checks `os.ModeCharDevice`, so
non-terminal character devices such as `/dev/null` pass the gate. In that
case `runLoopWithView` calls `term.MakeRaw` on stdin, prints
`agents: go: enable raw terminal: ...`, and returns 1 after already creating
the task, scope, and handoff. That violates the M2 contract that non-TTY
usage falls back to plain output automatically; the gate should require real
TTY stdin and stdout before entering the view.

## Fixed

By: implementer
Recorded at: 2026-06-10T18:42:49Z
Evidence: loopViewTerminals now requires term.IsTerminal on both stdin and stdout (e.isTTY's char-device check alone no longer admits /dev/null); gate extracted as a helper; TestLoopViewTerminalsRejectsNonTTYInputs covers /dev/null and non-file ends; manual check: agents go < /dev/null falls back to plain output. 133 tests pass, gofmt/vet clean

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-06-10T18:44:13Z
Evidence: Reviewed internal/app/loopview.go and internal/app/app.go: agents go now calls loopViewTerminals before runLoopWithView, and loopViewTerminals requires both stdin and stdout to be *os.File values backed by term.IsTerminal; /dev/null no longer passes the split-view gate even though isTerminalReader treats it as a character device. Regression TestLoopViewTerminalsRejectsNonTTYInputs covers /dev/null and non-file inputs. Focused tests passed with isolated cache: GOCACHE=/private/tmp/agents-review-gocache go test ./internal/app -run 'TestLoopViewTerminalsRejectsNonTTYInputs|TestGoRequiresTrustBeforeCreatingTask|TestGoRunsLoopToHumanBoundary|TestLoopControlStopsBetweenRuns|TestLoopViewFramesAreRectangular' -count=1.
