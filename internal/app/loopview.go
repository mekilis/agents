package app

// The unified loop view: two lanes (Building / Checking) rendered live from
// loopEvents, with a status bar that makes the loop the hero. Roles never
// lead the UI — they are mapped to activity language; role names stay in
// records and logs for auditability.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// activityName maps internal role vocabulary to the activity language the
// view speaks. Records and logs keep role names; the UI never leads with them.
func activityName(role string) string {
	switch role {
	case "implementer":
		return "Building"
	case "reviewer":
		return "Checking"
	case "coordinator":
		return "Planning"
	case "human":
		return "Ready for you"
	default:
		return role
	}
}

// translateStop turns a loop stop reason into plain language. Stops are
// honest endings, not hidden states.
func translateStop(reason string, runs int, state *loopJSON) string {
	switch reason {
	case "human":
		return "Done and independently checked — your call."
	case "no-progress":
		role := ""
		if state != nil {
			role = state.NextRole
		}
		return fmt.Sprintf("The agents are stuck — the last %s run recorded no progress.", activityName(role))
	case "max-runs":
		return fmt.Sprintf("Stopped after %d rounds with work remaining — review what's there, then rerun to continue.", runs)
	case "unconfigured":
		role := ""
		if state != nil {
			role = state.NextRole
		}
		return fmt.Sprintf("No agent is configured for %s (%s) — set it in .agents/config.yml.", activityName(role), role)
	case "quit":
		return "Stopped at your request."
	case "pause":
		return "Paused — rerun `agents run` to continue."
	case "coordinator":
		return "Waiting on Planning — continue in the interactive terminal."
	default:
		return "Stopped: " + reason
	}
}

// loopControl carries user intent into a running managed loop. It is checked
// between role runs only: the current run always finishes, so records stay
// consistent.
type loopControl struct {
	mu     sync.Mutex
	reason string // "" | "quit" | "pause"
}

func (c *loopControl) request(reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.reason == "" {
		c.reason = reason
	}
}

func (c *loopControl) requested() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reason
}

const laneRingCapacity = 200

var ansiEscapes = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\a]*(\a|\x1b\\)|\x1b.`)

// viewLane is one column of the split view.
type viewLane struct {
	activity string
	running  bool
	note     string
	lines    []string
	partial  string
}

func (l *viewLane) appendChunk(chunk string) {
	chunk = ansiEscapes.ReplaceAllString(chunk, "")
	chunk = strings.ReplaceAll(chunk, "\t", "    ")
	text := l.partial + chunk
	parts := strings.Split(text, "\n")
	l.partial = parts[len(parts)-1]
	for _, line := range parts[:len(parts)-1] {
		// Spinners redraw with carriage returns; keep the final state.
		if idx := strings.LastIndex(line, "\r"); idx >= 0 {
			line = line[idx+1:]
		}
		l.lines = append(l.lines, line)
	}
	if len(l.lines) > laneRingCapacity {
		l.lines = l.lines[len(l.lines)-laneRingCapacity:]
	}
}

// visibleLines returns the lane content to render: the ring tail plus any
// unterminated partial line.
func (l *viewLane) visibleLines(height int) []string {
	lines := l.lines
	if partial := strings.TrimRight(l.partial, "\r"); partial != "" {
		if idx := strings.LastIndex(partial, "\r"); idx >= 0 {
			partial = partial[idx+1:]
		}
		lines = append(append([]string{}, lines...), partial)
	}
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	return lines
}

// loopView consumes loopEvents and renders frames. It is pure with respect
// to the terminal: handle mutates state, frame returns lines, and the caller
// owns drawing — so tests assert on frames from a fake event stream.
type loopView struct {
	taskTitle string
	maxRuns   int
	runs      int
	findings  findingsJSON
	nextRole  string
	build     viewLane
	check     viewLane
	stopped   *loopEvent
	notice    string
}

func newLoopView(taskTitle string, maxRuns int) *loopView {
	view := &loopView{taskTitle: taskTitle, maxRuns: maxRuns}
	view.build.activity = "Building"
	view.build.note = "waiting"
	view.check.activity = "Checking"
	view.check.note = "waiting for handoff"
	return view
}

// laneFor routes events to a column: checking work right, everything else
// (building, planning) left.
func (v *loopView) laneFor(role string) *viewLane {
	if role == "reviewer" {
		return &v.check
	}
	return &v.build
}

func (v *loopView) handle(ev loopEvent) {
	switch ev.Event {
	case "state_changed":
		v.applyState(ev.State)
	case "run_started":
		lane := v.laneFor(ev.Role)
		lane.activity = activityName(ev.Role)
		lane.running = true
		lane.note = ev.Agent
		other := v.otherLane(lane)
		if !other.running {
			other.note = "waiting for handoff"
		}
	case "output":
		v.laneFor(ev.Role).appendChunk(ev.Chunk)
	case "run_finished":
		lane := v.laneFor(ev.Role)
		lane.running = false
		if ev.Error != "" {
			lane.note = "failed: " + firstLine(ev.Error)
		} else {
			lane.note = "finished"
			v.runs++
		}
		v.applyState(ev.State)
	case "stopped":
		stop := ev
		v.stopped = &stop
		if ev.Runs != nil {
			v.runs = *ev.Runs
		}
		v.build.running = false
		v.check.running = false
		v.applyState(ev.State)
	}
}

func (v *loopView) otherLane(lane *viewLane) *viewLane {
	if lane == &v.build {
		return &v.check
	}
	return &v.build
}

func (v *loopView) applyState(state *loopJSON) {
	if state == nil {
		return
	}
	v.nextRole = state.NextRole
	if state.Task != nil {
		v.findings = state.Task.Findings
		if state.Task.Title != "" {
			v.taskTitle = state.Task.Title
		}
	}
}

func firstLine(text string) string {
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		return text[:idx]
	}
	return text
}

// frame renders the full view as lines for a width x height terminal.
func (v *loopView) frame(width, height int) []string {
	if width < 24 {
		width = 24
	}
	if height < 8 {
		height = 8
	}
	inner := width - 3 // two borders + lane divider
	leftWidth := inner / 2
	rightWidth := inner - leftWidth
	laneHeight := height - 5 // top border, divider, two status lines, bottom border
	if laneHeight < 1 {
		laneHeight = 1
	}

	lines := make([]string, 0, height)
	lines = append(lines, "┌"+laneTitle(v.laneHeading(&v.build), leftWidth)+"┬"+laneTitle(v.laneHeading(&v.check), rightWidth)+"┐")
	left := v.build.visibleLines(laneHeight - 1)
	right := v.check.visibleLines(laneHeight - 1)
	leftStatus := padCell(" "+v.laneStatus(&v.build), leftWidth)
	rightStatus := padCell(" "+v.laneStatus(&v.check), rightWidth)
	lines = append(lines, "│"+leftStatus+"│"+rightStatus+"│")
	for i := 0; i < laneHeight-1; i++ {
		lines = append(lines, "│"+padCell(" "+laneLineAt(left, i, laneHeight-1), leftWidth)+"│"+padCell(" "+laneLineAt(right, i, laneHeight-1), rightWidth)+"│")
	}
	lines = append(lines, "├"+strings.Repeat("─", leftWidth)+"┴"+strings.Repeat("─", rightWidth)+"┤")
	lines = append(lines, "│"+padCell(" "+v.statusLine(), inner+1)+"│")
	lines = append(lines, "│"+padCell(" "+v.taskLine(inner), inner+1)+"│")
	lines = append(lines, "└"+strings.Repeat("─", inner+1)+"┘")
	return lines
}

func (v *loopView) laneHeading(lane *viewLane) string {
	if lane.running {
		return lane.activity + " ●"
	}
	return lane.activity
}

func (v *loopView) laneStatus(lane *viewLane) string {
	if lane.running {
		return "▸ " + lane.note
	}
	return "(" + lane.note + ")"
}

func (v *loopView) statusLine() string {
	if v.stopped != nil {
		return translateStop(v.stopped.Reason, v.runs, v.stopped.State)
	}
	active := "starting"
	if v.build.running {
		active = v.build.activity
	} else if v.check.running {
		active = v.check.activity
	} else if v.nextRole != "" {
		active = activityName(v.nextRole) + " next"
	}
	status := fmt.Sprintf("Loop %d/%d · %s · findings: %d open · %d fixed · %d verified",
		v.runs, v.maxRuns, active, v.findings.Open, v.findings.Fixed, v.findings.Verified)
	if v.notice != "" {
		status += " · " + v.notice
	}
	return status
}

func (v *loopView) taskLine(width int) string {
	hints := "[q]uit [p]ause"
	if v.stopped != nil {
		hints = "[q] close"
	}
	task := "Task: " + v.taskTitle
	gap := width - len([]rune(task)) - len([]rune(hints)) - 1
	if gap < 1 {
		gap = 1
	}
	return task + strings.Repeat(" ", gap) + hints
}

func laneTitle(title string, width int) string {
	label := "─ " + title + " "
	runes := []rune(label)
	if len(runes) > width {
		return string(runes[:width])
	}
	return label + strings.Repeat("─", width-len(runes))
}

func laneLineAt(lines []string, i, height int) string {
	// Bottom-align output like a terminal: the latest line hugs the status
	// bar and missing lines pad the top.
	offset := height - len(lines)
	if i < offset {
		return ""
	}
	return lines[i-offset]
}

func padCell(text string, width int) string {
	runes := []rune(text)
	if len(runes) > width {
		if width <= 1 {
			return string(runes[:width])
		}
		return string(runes[:width-1]) + "…"
	}
	return text + strings.Repeat(" ", width-len(runes))
}

// loopViewTerminals reports whether in and out can host the split view.
// Both ends must be real terminals: e.isTTY only checks for a character
// device, which lets /dev/null through, and the view needs raw-mode stdin
// plus a sized stdout. Anything less falls back to plain output.
func loopViewTerminals(in io.Reader, out io.Writer) (*os.File, *os.File, bool) {
	inFile, ok := in.(*os.File)
	if !ok {
		return nil, nil, false
	}
	outFile, ok := out.(*os.File)
	if !ok {
		return nil, nil, false
	}
	if !term.IsTerminal(int(inFile.Fd())) || !term.IsTerminal(int(outFile.Fd())) {
		return nil, nil, false
	}
	return inFile, outFile, true
}

// runLoopWithView drives the managed loop while rendering the unified split
// view on a TTY. The loop runs in a goroutine; events stream to the
// renderer; q (confirmed) and p stop after the current run. When the loop
// stops, the view flips to a plain-language summary in the normal screen
// buffer so the ending survives in scrollback.
func runLoopWithView(in, out *os.File, errOut io.Writer, e env, taskTitle string, maxRuns int) int {
	fd := int(in.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Fprintf(errOut, "agents: go: enable raw terminal: %v\n", err)
		return 1
	}
	fmt.Fprint(out, "\x1b[?1049h\x1b[?25l")
	restore := func() {
		_ = term.Restore(fd, oldState)
		fmt.Fprint(out, "\x1b[?25h\x1b[?1049l")
	}
	defer restore()

	// Raw mode disables the terminal's SIGINT; the key reader forwards
	// Ctrl-C as an os.Interrupt so the active run's own signal handling
	// applies. Registering here keeps a stray SIGINT between runs from
	// killing the process while the screen is in the alternate buffer.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	view := newLoopView(taskTitle, maxRuns)
	control := &loopControl{}
	events := make(chan loopEvent, 256)
	keys := make(chan byte, 16)
	loopDone := make(chan loopResult, 1)

	go func() {
		stop, loopErr := runManagedLoop(e, io.Discard, maxRuns, false, func(ev loopEvent) {
			events <- ev
		}, control)
		loopDone <- loopResult{stop: stop, err: loopErr}
	}()
	go func() {
		buf := make([]byte, 1)
		for {
			n, readErr := in.Read(buf)
			if readErr != nil {
				return
			}
			if n > 0 {
				keys <- buf[0]
			}
		}
	}()

	draw := func() {
		width, height, sizeErr := term.GetSize(int(out.Fd()))
		if sizeErr != nil || width <= 0 || height <= 0 {
			width, height = 100, 30
		}
		frame := view.frame(width, height)
		fmt.Fprint(out, "\x1b[H")
		for i, line := range frame {
			if i > 0 {
				fmt.Fprint(out, "\r\n")
			}
			fmt.Fprint(out, line, "\x1b[K")
		}
		fmt.Fprint(out, "\x1b[J")
	}

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	confirmQuit := false
	for {
		select {
		case ev := <-events:
			view.handle(ev)
		case <-ticker.C:
			// periodic redraw keeps the frame current across resizes
		case <-interrupts:
			control.request("quit")
			view.notice = "interrupted — stopping after the current run…"
		case key := <-keys:
			switch key {
			case 'q', 'Q':
				if confirmQuit {
					control.request("quit")
					view.notice = "stopping after the current run…"
				} else {
					confirmQuit = true
					view.notice = "press q again to stop after the current run"
				}
			case 'p', 'P':
				control.request("pause")
				view.notice = "pausing after the current run…"
			case 3: // Ctrl-C: interrupt the active run via its signal handling
				if proc, procErr := os.FindProcess(os.Getpid()); procErr == nil {
					_ = proc.Signal(os.Interrupt)
				}
			default:
				confirmQuit = false
				view.notice = ""
			}
		case result := <-loopDone:
			// Drain events emitted before the loop returned, show the final
			// frame briefly, then flip to the plain summary.
			for {
				select {
				case ev := <-events:
					view.handle(ev)
					continue
				default:
				}
				break
			}
			draw()
			restore()
			return printLoopViewEnding(out, errOut, e, result)
		}
		draw()
	}
}

type loopResult struct {
	stop loopStop
	err  error
}

// printLoopViewEnding writes the plain-language ending to the normal screen
// buffer after the view closes.
func printLoopViewEnding(out *os.File, errOut io.Writer, e env, result loopResult) int {
	if result.err != nil {
		if errors.Is(result.err, errRunInterrupted) {
			printInterruptHint(out, e)
			return 130
		}
		fmt.Fprintf(errOut, "agents: go: %v\n", result.err)
		return 1
	}
	stop := result.stop
	fmt.Fprintln(out, translateStop(stop.Reason, stop.Runs, statePayload(stop.State)))
	fmt.Fprintln(out)
	printLoopState(out, stop.State)
	switch stop.Reason {
	case "human", "unconfigured", "quit", "pause", "coordinator":
		return 0
	default:
		return 1
	}
}
