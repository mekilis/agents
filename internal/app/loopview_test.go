package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestActivityNameMapsRolesToActivities(t *testing.T) {
	for role, want := range map[string]string{
		"implementer": "Building",
		"reviewer":    "Checking",
		"coordinator": "Planning",
		"human":       "Ready for you",
		"other":       "other",
	} {
		if got := activityName(role); got != want {
			t.Fatalf("activityName(%q) = %q, want %q", role, got, want)
		}
	}
}

func TestTranslateStopSpeaksPlainLanguage(t *testing.T) {
	state := &loopJSON{NextRole: "implementer"}
	for _, tc := range []struct {
		reason string
		want   string
	}{
		{"human", "Done and independently checked"},
		{"no-progress", "The agents are stuck"},
		{"max-runs", "Stopped after 6 rounds"},
		{"unconfigured", "No agent is configured for Building"},
		{"quit", "Stopped at your request"},
		{"pause", "Paused"},
	} {
		got := translateStop(tc.reason, 6, state)
		if !strings.Contains(got, tc.want) {
			t.Fatalf("translateStop(%q) = %q, want substring %q", tc.reason, got, tc.want)
		}
		if strings.Contains(got, "implementer") && tc.reason != "unconfigured" {
			t.Fatalf("translateStop(%q) leaks role vocabulary: %q", tc.reason, got)
		}
	}
}

func TestViewLaneChunksHandlePartialsSpinnersAndANSI(t *testing.T) {
	var lane viewLane
	lane.appendChunk("\x1b[32mgreen\x1b[0m line\npartial")
	if len(lane.lines) != 1 || lane.lines[0] != "green line" {
		t.Fatalf("expected ANSI-stripped complete line, got %q", lane.lines)
	}
	if lane.partial != "partial" {
		t.Fatalf("expected partial carry, got %q", lane.partial)
	}
	lane.appendChunk(" done\n")
	if lane.lines[1] != "partial done" {
		t.Fatalf("expected joined partial line, got %q", lane.lines)
	}
	lane.appendChunk("spinner |\rspinner /\rspinner done\n")
	if got := lane.lines[len(lane.lines)-1]; got != "spinner done" {
		t.Fatalf("expected carriage-return overwrite to keep final state, got %q", got)
	}
	for i := 0; i < laneRingCapacity+50; i++ {
		lane.appendChunk("line\n")
	}
	if len(lane.lines) != laneRingCapacity {
		t.Fatalf("ring exceeded capacity: %d", len(lane.lines))
	}
	visible := lane.visibleLines(5)
	if len(visible) != 5 {
		t.Fatalf("expected 5 visible lines, got %d", len(visible))
	}
}

func TestLoopViewRendersLifecycleFrames(t *testing.T) {
	view := newLoopView("Add /history command", 6)
	frame := strings.Join(view.frame(80, 16), "\n")
	for _, want := range []string{"Building", "Checking", "Loop 0/6", "Task: Add /history command", "[q]uit [p]ause", "waiting for handoff"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("initial frame missing %q:\n%s", want, frame)
		}
	}
	if strings.Contains(frame, "implementer") || strings.Contains(frame, "reviewer") {
		t.Fatalf("frame leaks role vocabulary:\n%s", frame)
	}

	state := &loopJSON{NextRole: "implementer", Task: &taskJSON{Title: "Add /history command", Findings: findingsJSON{Open: 1, Fixed: 2}}}
	view.handle(loopEvent{Event: "state_changed", State: state})
	view.handle(loopEvent{Event: "run_started", RunID: "r1", Role: "implementer", Agent: "codex (model gpt-5.5)"})
	view.handle(loopEvent{Event: "output", RunID: "r1", Role: "implementer", Chunk: "editing main.go\ntests written\n"})
	frame = strings.Join(view.frame(80, 16), "\n")
	for _, want := range []string{"Building ●", "editing main.go", "tests written", "1 open · 2 fixed", "codex (model gpt-5.5)"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("running frame missing %q:\n%s", want, frame)
		}
	}

	view.handle(loopEvent{Event: "run_finished", RunID: "r1", Role: "implementer", State: state})
	view.handle(loopEvent{Event: "run_started", RunID: "r2", Role: "reviewer", Agent: "codex"})
	view.handle(loopEvent{Event: "output", RunID: "r2", Role: "reviewer", Chunk: "checking diff\n"})
	frame = strings.Join(view.frame(80, 16), "\n")
	for _, want := range []string{"Checking ●", "checking diff", "Loop 1/6"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("reviewer frame missing %q:\n%s", want, frame)
		}
	}
	if strings.Contains(frame, "Building ●") {
		t.Fatalf("builder lane should be idle while reviewer runs:\n%s", frame)
	}

	runs := 2
	view.handle(loopEvent{Event: "run_finished", RunID: "r2", Role: "reviewer", State: state})
	view.handle(loopEvent{Event: "stopped", Reason: "human", Runs: &runs, State: &loopJSON{NextRole: "human", Boundary: "human approval required", Task: state.Task}})
	frame = strings.Join(view.frame(80, 16), "\n")
	if !strings.Contains(frame, "Done and independently checked") {
		t.Fatalf("stopped frame missing translation:\n%s", frame)
	}
	if !strings.Contains(frame, "[q] close") {
		t.Fatalf("stopped frame should swap key hints:\n%s", frame)
	}
}

func TestLoopViewFailedRunShowsError(t *testing.T) {
	view := newLoopView("Task", 6)
	view.handle(loopEvent{Event: "run_started", RunID: "r1", Role: "implementer", Agent: "codex"})
	view.handle(loopEvent{Event: "run_finished", RunID: "r1", Role: "implementer", Error: "implementer command failed: exit status 1\ndetails"})
	frame := strings.Join(view.frame(120, 12), "\n")
	if !strings.Contains(frame, "failed: implementer command failed: exit status 1") {
		t.Fatalf("expected failure note in frame:\n%s", frame)
	}
	if !strings.Contains(frame, "Loop 0/6") {
		t.Fatalf("failed run must not count as a completed run:\n%s", frame)
	}
}

func TestLoopViewFramesAreRectangular(t *testing.T) {
	view := newLoopView("A very long task title that should be truncated rather than break the layout of the frame", 6)
	view.handle(loopEvent{Event: "run_started", RunID: "r1", Role: "implementer", Agent: "codex"})
	view.handle(loopEvent{Event: "output", RunID: "r1", Role: "implementer", Chunk: strings.Repeat("wide line ", 30) + "\n"})
	for _, size := range [][2]int{{80, 24}, {100, 30}, {40, 10}, {24, 8}, {10, 3}} {
		frame := view.frame(size[0], size[1])
		width := size[0]
		if width < 24 {
			width = 24
		}
		for i, line := range frame {
			if got := len([]rune(line)); got != width {
				t.Fatalf("frame %dx%d line %d width = %d, want %d: %q", size[0], size[1], i, got, width, line)
			}
		}
	}
}

func TestLoopViewTerminalsRejectsNonTTYInputs(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	// /dev/null is a character device, so the coarse e.isTTY check passes;
	// the view gate must still reject it on both ends.
	if !isTerminalReader(devNull) {
		t.Skipf("%s is not a character device on this platform", os.DevNull)
	}
	if _, _, ok := loopViewTerminals(devNull, devNull); ok {
		t.Fatal("expected /dev/null stdin+stdout to be rejected for the split view")
	}
	var buf bytes.Buffer
	if _, _, ok := loopViewTerminals(strings.NewReader(""), &buf); ok {
		t.Fatal("expected non-file reader/writer to be rejected for the split view")
	}
}

func TestLoopControlStopsBetweenRuns(t *testing.T) {
	withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		if _, err := openTask(e, []string{"Paused Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		control := &loopControl{}
		control.request("pause")
		control.request("quit") // first request wins
		var events []loopEvent
		stop, err := runManagedLoop(e, discardWriter{}, 6, false, func(ev loopEvent) {
			events = append(events, ev)
		}, control, loopDisplayPlain)
		if err != nil {
			t.Fatal(err)
		}
		if stop.Reason != "pause" {
			t.Fatalf("expected pause stop before any run, got %+v", stop)
		}
		if stop.Runs != 0 {
			t.Fatalf("expected no runs, got %d", stop.Runs)
		}
		last := events[len(events)-1]
		if last.Event != "stopped" || last.Reason != "pause" {
			t.Fatalf("expected stopped event with pause reason, got %+v", last)
		}
		return 0
	})
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
