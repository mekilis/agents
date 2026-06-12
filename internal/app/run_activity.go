package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type runActivityRecord struct {
	Event   string `json:"event"`
	Time    string `json:"time"`
	RunID   string `json:"run_id"`
	Role    string `json:"role,omitempty"`
	Agent   string `json:"agent,omitempty"`
	Status  string `json:"status,omitempty"`
	Text    string `json:"text,omitempty"`
	Error   string `json:"error,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Elapsed string `json:"elapsed,omitempty"`
}

type runActivitySink struct {
	mu      sync.Mutex
	dir     string
	starts  map[string]time.Time
	agents  map[string]string
	pending map[string][]string
}

func newRunActivitySink(e env) (loopEventSink, error) {
	active := readActiveTask(e.cwd)
	if active.Path == "" {
		return nil, nil
	}
	taskDir := filepath.Join(e.cwd, stateDir, active.Path)
	if _, err := os.Stat(filepath.Join(taskDir, "task.md")); err != nil {
		return nil, err
	}
	dir := filepath.Join(taskDir, "runs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	sink := &runActivitySink{
		dir:     dir,
		starts:  make(map[string]time.Time),
		agents:  make(map[string]string),
		pending: make(map[string][]string),
	}
	return sink.handle, nil
}

func (s *runActivitySink) handle(ev loopEvent) {
	if ev.RunID == "" {
		return
	}
	now := time.Now().UTC()
	rec := runActivityRecord{
		Event: ev.Event,
		Time:  now.Format(time.RFC3339),
		RunID: ev.RunID,
		Role:  ev.Role,
		Agent: ev.Agent,
		Error: strings.TrimSpace(ev.Error),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch ev.Event {
	case "run_started":
		s.starts[ev.RunID] = now
		s.agents[ev.RunID] = ev.Agent
		rec.Status = "started"
	case "output":
		text := cleanRunActivityText(s.agents[ev.RunID], ev.Chunk)
		if text == "" {
			return
		}
		text = s.coalesceMessageText(ev.RunID, text)
		if text == "" {
			return
		}
		rec.Event = "message"
		rec.Text = text
	case "run_finished":
		s.flushPendingMessage(ev.RunID, now.Format(time.RFC3339))
		rec.Status = "finished"
		if rec.Error != "" {
			rec.Status = "failed"
		}
		if started, ok := s.starts[ev.RunID]; ok {
			rec.Elapsed = formatElapsed(now.Sub(started))
		}
	case "stopped":
		rec.Status = "stopped"
		rec.Reason = ev.Reason
	default:
		return
	}
	_ = appendRunActivityRecord(filepath.Join(s.dir, ev.RunID+".jsonl"), rec)
}

func (s *runActivitySink) coalesceMessageText(runID, text string) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if isTinyRunActivityFragment(line) {
			s.pending[runID] = append(s.pending[runID], line)
			continue
		}
		pending := strings.Join(s.pending[runID], " ")
		if pending != "" {
			if !strings.Contains(normalizeRunActivityFragment(line), normalizeRunActivityFragment(pending)) {
				kept = append(kept, pending)
			}
			delete(s.pending, runID)
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func (s *runActivitySink) flushPendingMessage(runID, at string) {
	pending := strings.TrimSpace(strings.Join(s.pending[runID], " "))
	delete(s.pending, runID)
	if pending == "" {
		return
	}
	_ = appendRunActivityRecord(filepath.Join(s.dir, runID+".jsonl"), runActivityRecord{
		Event: "message",
		Time:  at,
		RunID: runID,
		Text:  pending,
	})
}

func isTinyRunActivityFragment(line string) bool {
	if len([]rune(line)) > 32 {
		return false
	}
	if strings.ContainsAny(line, ".!?:`/\\") {
		return false
	}
	fields := strings.Fields(line)
	return len(fields) == 1
}

func normalizeRunActivityFragment(line string) string {
	line = strings.ToLower(strings.TrimSpace(line))
	line = strings.Trim(line, " \t\r\n.,!?:;")
	return strings.Join(strings.Fields(line), " ")
}

func appendRunActivityRecord(path string, rec runActivityRecord) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	return encoder.Encode(rec)
}

func cleanRunActivityText(agent, chunk string) string {
	text := strings.TrimSpace(cleanInteractiveAgentOutput(agent, chunk))
	if text == "" {
		return ""
	}
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		switch {
		case trimmed == "":
			continue
		case strings.HasPrefix(lower, "run id:"):
			continue
		case strings.HasPrefix(lower, "run log:"):
			continue
		case strings.Contains(lower, "still running"):
			continue
		case strings.Contains(lower, "heartbeat"):
			continue
		}
		kept = append(kept, trimmed)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func combineEventSinks(sinks ...loopEventSink) loopEventSink {
	var active []loopEventSink
	for _, sink := range sinks {
		if sink != nil {
			active = append(active, sink)
		}
	}
	if len(active) == 0 {
		return nil
	}
	return func(ev loopEvent) {
		for _, sink := range active {
			sink(ev)
		}
	}
}

func runWatchCommand(args []string, out, errOut io.Writer, e env) int {
	if len(args) != 0 {
		fmt.Fprintln(errOut, "agents: watch does not accept arguments yet")
		return 2
	}
	if err := printRunActivity(out, e, true); err != nil {
		fmt.Fprintf(errOut, "agents: watch: %v\n", err)
		return 1
	}
	return 0
}

func runRunsCommand(args []string, out, errOut io.Writer, e env) int {
	follow := false
	for _, arg := range args {
		switch arg {
		case "--follow":
			follow = true
		case "":
			continue
		default:
			fmt.Fprintf(errOut, "agents: runs: unknown flag %s\n", arg)
			return 2
		}
	}
	if err := printRunActivity(out, e, follow); err != nil {
		fmt.Fprintf(errOut, "agents: runs: %v\n", err)
		return 1
	}
	return 0
}

func printRunActivity(out io.Writer, e env, follow bool) error {
	active := readActiveTask(e.cwd)
	if active.Path == "" {
		return fmt.Errorf("no active task")
	}
	dir := filepath.Join(e.cwd, stateDir, active.Path, "runs")
	records, err := readRunActivityRecords(dir)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		fmt.Fprintf(out, "No run activity for %s yet.\n", active.Title)
		if follow {
			fmt.Fprintln(out, "Run `agents run` or `agents go` to create activity.")
			if e.isTTY {
				return followRunActivityFunc(out, dir, 0)
			}
		}
		return nil
	}
	fmt.Fprintf(out, "Task: %s\n", active.Title)
	for _, rec := range records {
		printRunActivityRecord(out, rec)
	}
	if follow && !e.isTTY {
		fmt.Fprintln(out, "Follow mode shows persisted activity; rerun to refresh.")
	}
	if follow && e.isTTY {
		return followRunActivityFunc(out, dir, len(records))
	}
	return nil
}

var followRunActivityFunc = followRunActivity

func followRunActivity(out io.Writer, dir string, printed int) error {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	return followRunActivityUntilInterrupted(out, dir, printed, ticker.C, interrupts)
}

func followRunActivityUntilInterrupted(out io.Writer, dir string, printed int, ticks <-chan time.Time, interrupts <-chan os.Signal) error {
	for {
		select {
		case <-interrupts:
			fmt.Fprintln(out, "Stopped watching.")
			return nil
		case <-ticks:
			records, err := readRunActivityRecords(dir)
			if err != nil {
				return err
			}
			if printed > len(records) {
				printed = 0
			}
			for _, rec := range records[printed:] {
				printRunActivityRecord(out, rec)
			}
			printed = len(records)
		}
	}
}

func readRunActivityRecords(dir string) ([]runActivityRecord, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var records []runActivityRecord
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		file, err := os.Open(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			var rec runActivityRecord
			if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
				_ = file.Close()
				return nil, err
			}
			records = append(records, rec)
		}
		if err := scanner.Err(); err != nil {
			_ = file.Close()
			return nil, err
		}
		_ = file.Close()
	}
	sort.SliceStable(records, func(i, j int) bool { return records[i].Time < records[j].Time })
	return records, nil
}

func printRunActivityRecord(out io.Writer, rec runActivityRecord) {
	switch rec.Event {
	case "run_started":
		fmt.Fprintf(out, "[%s] %s started", rec.RunID, titleRole(rec.Role))
		if rec.Agent != "" {
			fmt.Fprintf(out, " with %s", rec.Agent)
		}
		fmt.Fprintln(out)
	case "message":
		for _, line := range strings.Split(rec.Text, "\n") {
			if strings.TrimSpace(line) != "" {
				fmt.Fprintf(out, "  %s\n", strings.TrimSpace(line))
			}
		}
	case "run_finished":
		status := rec.Status
		if status == "" {
			status = "finished"
		}
		if rec.Elapsed != "" {
			fmt.Fprintf(out, "[%s] %s after %s\n", rec.RunID, status, rec.Elapsed)
		} else {
			fmt.Fprintf(out, "[%s] %s\n", rec.RunID, status)
		}
		if rec.Error != "" {
			fmt.Fprintf(out, "  error: %s\n", firstActivityLine(rec.Error))
		}
	}
}

func formatElapsed(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

func firstActivityLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
