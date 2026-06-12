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
	inBlock  bool
	block    []string
	skipRaw  bool
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
		l.appendLine(line)
	}
	if len(l.lines) > laneRingCapacity {
		l.lines = l.lines[len(l.lines)-laneRingCapacity:]
	}
}

func (l *viewLane) appendLine(line string) {
	trimmed := strings.TrimSpace(line)
	if shouldDropLaneLine(trimmed) {
		return
	}
	if l.inBlock {
		if trimmed == "```" {
			if progress, ok := parseProgressDirectiveLines(l.block); ok {
				l.lines = append(l.lines, formatProgressLine(progress))
			}
			l.inBlock = false
			l.block = nil
			return
		}
		l.block = append(l.block, line)
		return
	}
	switch {
	case trimmed == "```agents-progress":
		l.skipRaw = false
		l.inBlock = true
		l.block = nil
	case looksLikeBareProgressJSON(trimmed):
		if progress, ok := parseProgressDirectiveLines([]string{trimmed}); ok {
			l.skipRaw = false
			l.lines = append(l.lines, formatProgressLine(progress))
		}
	case startsRawTranscript(trimmed):
		l.skipRaw = true
	case l.skipRaw:
		if isUserFacingAgentLine(trimmed) {
			l.skipRaw = false
			l.lines = append(l.lines, line)
		}
	default:
		l.lines = append(l.lines, line)
	}
}

func startsRawTranscript(trimmed string) bool {
	lower := strings.ToLower(trimmed)
	switch {
	case trimmed == "OpenAI Codex v0.137.0":
		return true
	case strings.HasPrefix(trimmed, "--------"):
		return true
	case strings.HasPrefix(trimmed, "workdir:"):
		return true
	case strings.HasPrefix(trimmed, "model:"):
		return true
	case strings.HasPrefix(trimmed, "provider:"):
		return true
	case strings.HasPrefix(trimmed, "approval:"):
		return true
	case strings.HasPrefix(trimmed, "sandbox:"):
		return true
	case strings.HasPrefix(trimmed, "reasoning effort:"):
		return true
	case strings.HasPrefix(trimmed, "exec /bin/"):
		return true
	case trimmed == "exec":
		return true
	case strings.HasPrefix(trimmed, "/bin/"):
		return true
	case trimmed == "evidence:":
		return true
	case strings.Contains(lower, "placeholder evidence"):
		return true
	case strings.HasPrefix(trimmed, "You are the "):
		return true
	case strings.HasPrefix(trimmed, "Task:"):
		return true
	case strings.HasPrefix(trimmed, "Path:"):
		return true
	case strings.HasPrefix(trimmed, "Next action:"):
		return true
	case strings.HasPrefix(trimmed, "Working agreement:"):
		return true
	case strings.HasPrefix(trimmed, "To:") || strings.HasPrefix(trimmed, "From:"):
		return true
	case strings.Contains(lower, "agents process records this verification"):
		return true
	case strings.Contains(lower, "recording passing verification reaches"):
		return true
	case strings.Contains(lower, "summarize the review in your response"):
		return true
	case strings.Contains(lower, "keep build caches"):
		return true
	case strings.Contains(lower, "do not edit implementation code"):
		return true
	default:
		return false
	}
}

func isUserFacingAgentLine(trimmed string) bool {
	lower := strings.ToLower(trimmed)
	switch {
	case trimmed == "":
		return false
	case strings.HasPrefix(trimmed, "Issues"):
		return true
	case strings.HasPrefix(trimmed, "Findings"):
		return true
	case strings.HasPrefix(trimmed, "Verified"):
		return true
	case strings.HasPrefix(trimmed, "No findings"):
		return true
	case strings.HasPrefix(trimmed, "Review"):
		return true
	case strings.HasPrefix(trimmed, "Implemented"):
		return true
	case strings.Contains(lower, "recorded this as a finding"):
		return true
	default:
		return false
	}
}

func shouldDropLaneLine(trimmed string) bool {
	lower := strings.ToLower(trimmed)
	switch {
	case trimmed == "":
		return false
	case trimmed == "Reading additional input from stdin...":
		return true
	case strings.Contains(trimmed, " WARN codex_"):
		return true
	case strings.Contains(trimmed, "codex_core_plugins::manifest"):
		return true
	case strings.Contains(trimmed, "ignoring interface.defaultPrompt[0]"):
		return true
	case strings.Contains(trimmed, "ignoring interface.icon_"):
		return true
	case strings.Contains(trimmed, "prompt must be at most 128 characters"):
		return true
	case strings.HasPrefix(trimmed, "path=/Users/") && strings.Contains(trimmed, "/.codex/"):
		return true
	case strings.Contains(trimmed, "failed to initialize MCP client during shutdown"):
		return true
	case strings.Contains(trimmed, "handshaking with MCP server failed"):
		return true
	case strings.Contains(lower, "initialize response") && strings.Contains(lower, "connection closed"):
		return true
	default:
		return false
	}
}

func looksLikeBareProgressJSON(trimmed string) bool {
	return strings.HasPrefix(trimmed, "{") &&
		strings.HasSuffix(trimmed, "}") &&
		strings.Contains(trimmed, `"status"`) &&
		strings.Contains(trimmed, `"detail"`)
}

func formatProgressLine(progress progressDirective) string {
	status := strings.TrimSpace(progress.Status)
	if status == "" {
		return ""
	}
	detail := strings.TrimSpace(progress.Detail)
	if detail == "" {
		return "- " + sentenceCase(status)
	}
	return "- " + sentenceCase(status) + ": " + sentenceCase(singleLine(detail))
}

// visibleLines returns the lane content to render: the ring tail plus any
// unterminated partial line.
func (l *viewLane) visibleLines(height, width int) []string {
	lines := l.lines
	if partial := strings.TrimRight(l.partial, "\r"); partial != "" {
		if idx := strings.LastIndex(partial, "\r"); idx >= 0 {
			partial = partial[idx+1:]
		}
		if l.shouldShowPartial(partial) {
			lines = append(append([]string{}, lines...), partial)
		}
	}
	lines = scrubLaneDisplayLines(lines)
	lines = renderLaneMarkdown(lines, width)
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	return lines
}

func scrubLaneDisplayLines(lines []string) []string {
	var out []string
	skip := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case shouldDropLaneLine(trimmed):
			continue
		case startsRawTranscript(trimmed):
			skip = true
			continue
		case skip && isUserFacingAgentLine(trimmed):
			skip = false
			out = append(out, line)
		case skip:
			continue
		default:
			out = append(out, line)
		}
	}
	return out
}

func (l *viewLane) shouldShowPartial(partial string) bool {
	trimmed := strings.TrimSpace(partial)
	if shouldDropLaneLine(trimmed) || startsRawTranscript(trimmed) {
		return false
	}
	if l.skipRaw && !isUserFacingAgentLine(trimmed) && !looksLikeBareProgressJSON(trimmed) {
		return false
	}
	return true
}

func renderLaneMarkdown(lines []string, width int) []string {
	text := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	if text == "" || !looksLikeMarkdown(text) {
		return lines
	}
	rendered, err := renderTerminalMarkdown(text, width)
	if err != nil {
		return lines
	}
	rendered = ansiEscapes.ReplaceAllString(rendered, "")
	rendered = strings.TrimRight(rendered, "\n")
	if rendered == "" {
		return nil
	}
	return cleanRenderedLaneMarkdown(scrubLaneDisplayLines(strings.Split(rendered, "\n")))
}

func looksLikeMarkdown(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "> ") || strings.HasPrefix(trimmed, "```") {
			return true
		}
		if strings.Contains(trimmed, "|") && strings.Count(trimmed, "|") >= 2 {
			return true
		}
	}
	return false
}

func cleanRenderedLaneMarkdown(lines []string) []string {
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		prefixLen := len(line) - len(strings.TrimLeft(line, " "))
		prefix := line[:prefixLen]
		trimmed := line[prefixLen:]
		if strings.HasPrefix(trimmed, "#") {
			markers := 0
			for markers < len(trimmed) && trimmed[markers] == '#' {
				markers++
			}
			if markers < len(trimmed) && trimmed[markers] == ' ' {
				trimmed = strings.TrimLeft(trimmed[markers+1:], " ")
				line = prefix + trimmed
			}
		}
		cleaned = append(cleaned, line)
	}
	return cleaned
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
		if ev.Error == "" && ev.State != nil && ev.State.NextRole != "" && ev.State.NextRole != ev.Role {
			lane.appendChunk(fmt.Sprintf("Done. Next: %s.\n", activityName(ev.State.NextRole)))
			if ev.State.NextRole == "reviewer" {
				v.check.note = "next"
			}
		}
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
	left := v.build.visibleLines(laneHeight-1, leftWidth-2)
	right := v.check.visibleLines(laneHeight-1, rightWidth-2)
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
		if v.notice != "" {
			return v.notice
		}
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
// buffer so the ending survives in scrollback; at the human boundary the
// summary is the Ready-for-you screen with one decision — accept, iterate,
// or abandon.
func runLoopWithView(in, out *os.File, errOut io.Writer, e env, taskTitle string, maxRuns int, forceFirstRole string) int {
	fd := int(in.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Fprintf(errOut, "agents: go: enable raw terminal: %v\n", err)
		return 1
	}
	restore := func() {
		_ = term.Restore(fd, oldState)
		fmt.Fprint(out, "\x1b[?25h\x1b[?1049l\r\033[2K")
	}
	defer restore()

	// Raw mode disables the terminal's SIGINT; the key reader forwards
	// Ctrl-C as an os.Interrupt so the active run's own signal handling
	// applies. Registering here keeps a stray SIGINT between runs from
	// killing the process while the screen is in the alternate buffer.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	keys := make(chan byte, 16)
	go func() {
		buf := make([]byte, 1)
		for {
			n, readErr := in.Read(buf)
			if readErr != nil {
				close(keys)
				return
			}
			if n > 0 {
				keys <- buf[0]
			}
		}
	}()

	// Each pass runs the loop inside the split view; iterate from the ready
	// screen records the feedback handoff and re-enters the loop.
	for {
		result := watchLoopInView(out, e, taskTitle, maxRuns, forceFirstRole, keys, interrupts)
		forceFirstRole = ""
		if result.err != nil || result.stop.Reason != "human" {
			restore()
			return printLoopViewEnding(out, errOut, e, result)
		}
		// Human boundary: leave the alternate screen so the summary lands
		// in scrollback; keep raw mode for single-key decisions.
		fmt.Fprint(out, "\x1b[?25h\x1b[?1049l")
		summary := gatherReadySummary(e, result.stop.State, result.stop.Runs)
		action, feedback := runReadyScreen(out, keys, summary)
		switch action {
		case readyAccept:
			restore()
			return acceptReadyTask(out, errOut, e, summary)
		case readyIterate:
			if _, handoffErr := addHandoff(e, []string{"--to", "implementer", feedback}, time.Now().UTC()); handoffErr != nil {
				restore()
				fmt.Fprintf(errOut, "agents: go: iterate: %v\n", handoffErr)
				return 1
			}
			writeRawLines(out, []string{"", "Sent — running another round…"})
		case readyAbandon:
			restore()
			fmt.Fprintln(out, "Left as is — the task stays active; rerun `agents run` or `agents` anytime.")
			fmt.Fprintln(out)
			printLoopState(out, result.stop.State)
			return 0
		}
	}
}

// watchLoopInView runs one managed-loop pass while rendering the view and
// handling keys, returning the loop result when it stops.
func watchLoopInView(out *os.File, e env, taskTitle string, maxRuns int, forceFirstRole string, keys <-chan byte, interrupts <-chan os.Signal) loopResult {
	fmt.Fprint(out, "\x1b[?1049h\x1b[?25l")
	view := newLoopView(taskTitle, maxRuns)
	control := &loopControl{}
	events := make(chan loopEvent, 256)
	loopDone := make(chan loopResult, 1)

	go func() {
		stop, loopErr := runManagedLoop(e, io.Discard, maxRuns, false, func(ev loopEvent) {
			events <- ev
		}, control, loopDisplayView, forceFirstRole)
		loopDone <- loopResult{stop: stop, err: loopErr}
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
		case key, ok := <-keys:
			if !ok {
				// Input closed: stop after the current run and block this
				// case (a nil channel never fires) instead of spinning.
				keys = nil
				control.request("quit")
				view.notice = "input closed — stopping after the current run…"
				break
			}
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
			// Drain events emitted before the loop returned and show the
			// final frame before the caller flips to the plain summary.
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
			if result.err == nil && result.stop.Reason == "human" {
				view.notice = "review complete — press enter or space for the inspection summary"
				draw()
				waitForReadySummary(keys)
			}
			return result
		}
		draw()
	}
}

func waitForReadySummary(keys <-chan byte) {
	for key := range keys {
		switch key {
		case '\r', '\n', ' ':
			return
		}
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
	fmt.Fprintln(out)
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
