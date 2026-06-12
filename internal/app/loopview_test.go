package app

import (
	"bytes"
	"errors"
	"io"
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
	visible := lane.visibleLines(5, 40)
	if len(visible) != 5 {
		t.Fatalf("expected 5 visible lines, got %d", len(visible))
	}
}

func TestViewLaneRendersMarkdownForSplitView(t *testing.T) {
	var lane viewLane
	lane.appendChunk("## Status\n\n- **Task** routing\n\n")
	visible := lane.visibleLines(10, 60)
	text := strings.Join(visible, "\n")
	for _, want := range []string{"Status", "Task routing"} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered lane markdown missing %q:\n%s", want, text)
		}
	}
	for _, raw := range []string{"## Status", "**Task**"} {
		if strings.Contains(text, raw) {
			t.Fatalf("lane should render markdown instead of showing raw %q:\n%s", raw, text)
		}
	}
}

func TestViewLaneCleansProgressJSON(t *testing.T) {
	var lane viewLane
	lane.appendChunk("codex\n")
	lane.appendChunk(`{"status":"Running checks","detail":"Executing the unit suite"}` + "\n")
	lane.appendChunk("```agents-progress\n")
	lane.appendChunk(`{"status":"Reviewing diff","detail":"Checking files"}` + "\n")
	lane.appendChunk("```\n")
	text := strings.Join(lane.visibleLines(10, 80), "\n")
	for _, want := range []string{
		"codex",
		"Running checks: Executing the unit suite",
		"Reviewing diff: Checking files",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("cleaned progress missing %q:\n%s", want, text)
		}
	}
	for _, leaked := range []string{`"status"`, "```agents-progress"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("progress protocol leaked %q:\n%s", leaked, text)
		}
	}
}

func TestViewLaneDropsProviderStartupNoise(t *testing.T) {
	var lane viewLane
	lane.appendChunk("Reading additional input from stdin...\n")
	lane.appendChunk("2026-06-12T10:53:08Z  WARN codex_core_plugins::manifest: ignoring interface.defaultPrompt[0]: prompt must be at most 128 characters path=/Users/smart/.codex/.tmp/plugins/example/.codex-plugin/plugin.json\n")
	lane.appendChunk("failed to initialize MCP client during shutdown: MCP startup failed: handshaking with MCP server failed: connection closed: initialize response\n")
	lane.appendChunk("Useful review line\n")
	text := strings.Join(lane.visibleLines(10, 80), "\n")
	if !strings.Contains(text, "Useful review line") {
		t.Fatalf("expected useful line to remain:\n%s", text)
	}
	for _, leaked := range []string{"Reading additional input", "codex_core_plugins", "defaultPrompt", "MCP client"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("provider startup noise leaked %q:\n%s", leaked, text)
		}
	}
}

func TestViewLaneSkipsCodexPromptAndToolTranscript(t *testing.T) {
	var lane viewLane
	lane.appendChunk("OpenAI Codex v0.137.0\n")
	lane.appendChunk("workdir: /Users/smart/demos/tiny-habits\n")
	lane.appendChunk("You are the reviewer for this Agents task.\n")
	lane.appendChunk("Working agreement:\n")
	lane.appendChunk("- Do not edit implementation code, do not mark findings fixed, and do not commit.\n")
	lane.appendChunk("exec /bin/zsh -lc \"sed -n '241,360p' tiny_habits.py\" in /Users/smart/demos/tiny-habits succeeded in 0ms:\n")
	lane.appendChunk("def main(argv: list[str] | None = None) -> int:\n")
	lane.appendChunk("    return args.func(args)\n")
	lane.appendChunk("Verified:\n")
	lane.appendChunk("- Reviewed tiny_habits.py and tests.\n")
	text := strings.Join(lane.visibleLines(20, 80), "\n")
	if !strings.Contains(text, "Verified") || !strings.Contains(text, "Reviewed tiny_habits.py") {
		t.Fatalf("expected final review text to remain:\n%s", text)
	}
	for _, leaked := range []string{"OpenAI Codex", "Working agreement", "exec /bin/zsh", "def main", "return args.func"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("raw transcript leaked %q:\n%s", leaked, text)
		}
	}
}

func TestViewLaneSkipsRawTranscriptPartial(t *testing.T) {
	var lane viewLane
	lane.appendChunk("exec /bin/zsh -lc 'git status --short' in /Users/smart/demos/tiny-habits")
	text := strings.Join(lane.visibleLines(20, 80), "\n")
	if strings.Contains(text, "exec /bin/zsh") || strings.Contains(text, "git status") {
		t.Fatalf("raw partial transcript leaked:\n%s", text)
	}
	lane.appendChunk("\nVerified:")
	text = strings.Join(lane.visibleLines(20, 80), "\n")
	if !strings.Contains(text, "Verified") {
		t.Fatalf("expected user-facing partial to show:\n%s", text)
	}
}

func TestViewLaneScrubsStoredTranscriptLines(t *testing.T) {
	lane := viewLane{lines: []string{
		"Useful before",
		"exec /bin/zsh -lc 'git status --short' in /Users/smart/demos/tiny-habits exited 128 in 0ms:",
		"fatal: not a git repository",
		"test_tiny_habits.py",
		"Verified:",
		"- Tests passed.",
	}}
	text := strings.Join(lane.visibleLines(20, 80), "\n")
	for _, want := range []string{"Useful before", "Verified", "Tests passed"} {
		if !strings.Contains(text, want) {
			t.Fatalf("scrubbed lane missing %q:\n%s", want, text)
		}
	}
	for _, leaked := range []string{"exec /bin/zsh", "fatal: not a git repository", "test_tiny_habits.py"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("stored transcript leaked %q:\n%s", leaked, text)
		}
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

	reviewerState := &loopJSON{NextRole: "reviewer", Task: state.Task}
	view.handle(loopEvent{Event: "run_finished", RunID: "r1", Role: "implementer", State: reviewerState})
	view.handle(loopEvent{Event: "run_started", RunID: "r2", Role: "reviewer", Agent: "codex"})
	view.handle(loopEvent{Event: "output", RunID: "r2", Role: "reviewer", Chunk: "checking diff\n"})
	frame = strings.Join(view.frame(80, 16), "\n")
	for _, want := range []string{"Checking ●", "checking diff", "Loop 1/6", "Done. Next: Checking."} {
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

func TestLoopViewShowsQuietProviderHeartbeat(t *testing.T) {
	view := newLoopView("Tiny todo CLI", 6)
	view.handle(loopEvent{Event: "run_started", RunID: "r1", Role: "implementer", Agent: "agent"})
	view.handle(loopEvent{Event: "output", RunID: "r1", Role: "implementer", Chunk: "\rStill running... 20s"})
	frame := strings.Join(view.frame(80, 12), "\n")
	if !strings.Contains(frame, "Still running... 20s") {
		t.Fatalf("quiet provider heartbeat should be visible:\n%s", frame)
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

func TestLoopViewLineFiltersCoverUserFacingAndProviderNoise(t *testing.T) {
	for _, line := range []string{
		"OpenAI Codex v0.137.0",
		"--------",
		"workdir: /tmp/repo",
		"model: gpt",
		"provider: codex",
		"approval: never",
		"sandbox: workspace-write",
	} {
		if !startsRawTranscript(line) {
			t.Fatalf("expected raw transcript starter %q", line)
		}
	}
	if startsRawTranscript("regular output") {
		t.Fatal("regular output should not start raw transcript skip mode")
	}

	userFacing := []string{
		"Issues found",
		"Findings: none",
		"Verified fix",
		"No findings",
		"Review passed",
		"Implemented change",
		"I recorded this as a finding",
	}
	for _, line := range userFacing {
		if !isUserFacingAgentLine(line) {
			t.Fatalf("expected user-facing line %q", line)
		}
	}
	for _, line := range []string{"", "raw provider trace"} {
		if isUserFacingAgentLine(line) {
			t.Fatalf("expected non-user-facing line %q", line)
		}
	}
	for _, line := range []string{
		"Reading additional input from stdin...",
		"2026 WARN codex_core_plugins::manifest",
		"ignoring interface.defaultPrompt[0]",
		"ignoring interface.icon_small",
		"prompt must be at most 128 characters",
	} {
		if !shouldDropLaneLine(line) {
			t.Fatalf("expected provider noise to be dropped: %q", line)
		}
	}
	if shouldDropLaneLine("useful output") {
		t.Fatal("useful output should not be dropped")
	}
}

func TestPrintLoopViewEndingExitCodes(t *testing.T) {
	outFile, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer outFile.Close()
	state := loopState{NextRole: "human", NextAction: "approve", Boundary: "human approval required"}
	if code := printLoopViewEnding(outFile, io.Discard, env{}, loopResult{stop: loopStop{Reason: "human", State: state}}); code != 0 {
		t.Fatalf("human stop code=%d", code)
	}
	var errOut bytes.Buffer
	if code := printLoopViewEnding(outFile, &errOut, env{}, loopResult{err: errors.New("boom")}); code != 1 {
		t.Fatalf("error stop code=%d", code)
	}
	if !strings.Contains(errOut.String(), "agents: go: boom") {
		t.Fatalf("error output missing message: %q", errOut.String())
	}
	if code := printLoopViewEnding(outFile, io.Discard, env{}, loopResult{stop: loopStop{Reason: "max-runs", Runs: 1, State: state}}); code != 1 {
		t.Fatalf("max-runs stop code=%d", code)
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
		}, control, loopDisplayPlain, "")
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
