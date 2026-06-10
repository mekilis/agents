# Handoff: reviewer

To: reviewer
From: implementer

M2 (unified split view) implemented in internal/app/loopview.go + wiring in app.go: pure loopView renderer (two lanes, ring buffers, ANSI/CR handling, rectangular frames at any size), activity-language mapping and stop translations, runLoopWithView (alt screen + raw mode + q/p/Ctrl-C via loopControl checked between runs), agents go gains --plain and auto plain fallback off-TTY; runManagedLoop gained an optional control param (nil for existing callers). 7 new tests (renderer frames from fake events, lane chunking, control stop); 132 total pass, gofmt/vet clean; PTY smoke test verified live. Please review especially: terminal state restoration on every exit path, the loopControl semantics, goroutine/channel shutdown in runLoopWithView, and frame width handling.
