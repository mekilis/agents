package app

// The "Ready for you" screen: when the loop reaches the human boundary, the
// view flips to a plain-language inspection summary — what was asked, what
// changed, what was found, what evidence exists — and waits for one decision:
// accept, iterate, or abandon.

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type readyFinding struct {
	Status   string
	Severity string
	Title    string
}

type readyVerification struct {
	Result  string
	Command string
	Title   string
}

// readySummary is the data behind the Ready-for-you screen.
type readySummary struct {
	TaskTitle    string
	Scope        string
	DiffStat     string
	Findings     []readyFinding
	Verification []readyVerification
	Runs         int
}

// gatherReadySummary collects the human-boundary evidence: scope, working
// tree changes, findings history, and verification records.
func gatherReadySummary(e env, state loopState, runs int) readySummary {
	taskDir := filepath.Join(e.cwd, stateDir, state.Task.Path)
	summary := readySummary{
		TaskTitle: state.Task.Title,
		Scope:     readChunkBody(filepath.Join(taskDir, "scope.md")),
		DiffStat:  gitDiffStat(e.cwd),
		Runs:      runs,
	}
	summary.Findings = readReadyFindings(filepath.Join(taskDir, "findings"))
	summary.Verification = readReadyVerification(filepath.Join(taskDir, "verification"))
	return summary
}

// readChunkBody returns a chunk file's content without its leading heading.
func readChunkBody(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "#") {
		lines = lines[1:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// gitDiffStat summarizes working tree changes against HEAD. Absent git or a
// repository, the screen says so instead of guessing.
func gitDiffStat(cwd string) string {
	out, err := exec.Command("git", "-C", cwd, "diff", "--stat", "HEAD").CombinedOutput()
	if err != nil {
		return "(no git diff available)"
	}
	stat := strings.TrimRight(string(out), "\n")
	if strings.TrimSpace(stat) == "" {
		return "(working tree clean)"
	}
	return stat
}

func readReadyFindings(dir string) []readyFinding {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var findings []readyFinding
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		markdown := string(data)
		findings = append(findings, readyFinding{
			Status:   findingStatus(markdown),
			Severity: chunkField(markdown, "Severity:"),
			Title:    chunkTitle(markdown, "# Finding: "),
		})
	}
	return findings
}

func readReadyVerification(dir string) []readyVerification {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var records []readyVerification
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		markdown := string(data)
		record := readyVerification{
			Result: chunkField(markdown, "Result:"),
			Title:  chunkTitle(markdown, "# Verification: "),
		}
		for _, line := range strings.Split(markdown, "\n") {
			if strings.HasPrefix(line, "$ ") {
				record.Command = strings.TrimSpace(line)
				break
			}
		}
		records = append(records, record)
	}
	return records
}

func chunkField(markdown, prefix string) string {
	for _, line := range strings.Split(markdown, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, prefix) {
			return strings.TrimSpace(t[len(prefix):])
		}
	}
	return ""
}

func chunkTitle(markdown, prefix string) string {
	for _, line := range strings.Split(markdown, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(line[len(prefix):])
		}
	}
	return ""
}

func findingGlyph(status string) string {
	switch status {
	case "verified":
		return "✓"
	case "fixed":
		return "◐"
	case "deferred", "wontfix":
		return "—"
	default:
		return "●"
	}
}

// readySummaryLines renders the Ready-for-you screen as plain lines.
func readySummaryLines(summary readySummary) []string {
	lines := []string{
		"Ready for you — " + summary.TaskTitle,
		strings.Repeat("─", 60),
		"",
		fmt.Sprintf("Paused for your inspection after %d runs.", summary.Runs),
		"",
		"What was asked:",
	}
	lines = append(lines, indentLines(orPlaceholder(summary.Scope, "(no scope recorded)"))...)
	lines = append(lines, "", "What changed (git diff --stat):")
	lines = append(lines, indentLines(summary.DiffStat)...)
	lines = append(lines, "", "Findings history:")
	if len(summary.Findings) == 0 {
		lines = append(lines, "  (none recorded)")
	}
	for _, f := range summary.Findings {
		lines = append(lines, fmt.Sprintf("  %s %s · %s · %s", findingGlyph(f.Status), f.Status, f.Severity, f.Title))
	}
	lines = append(lines, "", "Verification evidence:")
	if len(summary.Verification) == 0 {
		lines = append(lines, "  (none recorded)")
	}
	for _, v := range summary.Verification {
		entry := "  " + v.Result + " · " + v.Title
		if v.Command != "" {
			entry += " · " + v.Command
		}
		lines = append(lines, entry)
	}
	lines = append(lines,
		"",
		"Your call:",
		"  [a] accept — archive the task and get a suggested commit message",
		"  [i] iterate — tell the implementer what to change and run another round",
		"  [x] abandon — leave everything as is",
		"",
	)
	return lines
}

func indentLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		out = append(out, "  "+line)
	}
	return out
}

func orPlaceholder(text, placeholder string) string {
	if strings.TrimSpace(text) == "" {
		return placeholder
	}
	return text
}

// suggestedCommitMessage drafts a commit message from the task title and
// scope. Agents never commit; the human reviews and commits.
func suggestedCommitMessage(summary readySummary) string {
	subject := strings.TrimSpace(summary.TaskTitle)
	if subject == "" {
		subject = "Complete agents task"
	}
	body := strings.TrimSpace(firstLine(summary.Scope))
	if body == "" || body == subject {
		return subject
	}
	return subject + "\n\n" + body
}

type readyAction int

const (
	readyAccept readyAction = iota
	readyIterate
	readyAbandon
)

// runReadyScreen prints the summary (raw-mode safe) and reads one decision
// from the key channel. Iterate also collects a feedback line.
func runReadyScreen(out io.Writer, keys <-chan byte, summary readySummary) (readyAction, string) {
	writeRawLines(out, readySummaryLines(summary))
	for key := range keys {
		switch key {
		case 'a', 'A':
			writeRawLines(out, []string{"", "Accepted — archiving the task..."})
			return readyAccept, ""
		case 'i', 'I':
			fmt.Fprint(out, "What should change? › ")
			feedback := readRawLine(out, keys)
			if strings.TrimSpace(feedback) == "" {
				writeRawLines(out, []string{"(nothing entered — still your call: [a]ccept [i]terate [x] abandon)"})
				continue
			}
			writeRawLines(out, []string{"Sent — routing your feedback to the implementer..."})
			return readyIterate, strings.TrimSpace(feedback)
		case 'x', 'X', 'q', 'Q':
			writeRawLines(out, []string{"", "Abandoned — leaving the task active."})
			return readyAbandon, ""
		case 3: // Ctrl-C
			writeRawLines(out, []string{"", "Cancelled — leaving the task active."})
			return readyAbandon, ""
		}
	}
	return readyAbandon, ""
}

// writeRawLines prints lines with explicit carriage returns so output stays
// aligned while the terminal is in raw mode.
func writeRawLines(out io.Writer, lines []string) {
	for _, line := range lines {
		fmt.Fprint(out, line, "\r\n")
	}
}

// readRawLine is a minimal raw-mode line reader with echo and backspace,
// fed from the shared key channel so it never competes with another stdin
// reader.
func readRawLine(out io.Writer, keys <-chan byte) string {
	var line []rune
	for key := range keys {
		switch {
		case key == '\r' || key == '\n':
			fmt.Fprint(out, "\r\n")
			return string(line)
		case key == 3 || key == 27: // Ctrl-C / Esc cancels
			fmt.Fprint(out, "\r\n")
			return ""
		case key == 127 || key == 8:
			if len(line) > 0 {
				line = line[:len(line)-1]
				fmt.Fprint(out, "\b \b")
			}
		case key >= 32:
			line = append(line, rune(key))
			fmt.Fprint(out, string(rune(key)))
		}
	}
	return string(line)
}

// acceptReadyTask archives the task and prints the suggested commit message.
func acceptReadyTask(out io.Writer, errOut io.Writer, e env, summary readySummary) int {
	record, err := archiveActiveTask(e, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(errOut, "agents: go: archive: %v\n", err)
		return 1
	}
	writeArchiveReport(out, record)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Suggested commit message (agents never commits — your call):")
	fmt.Fprintln(out)
	for _, line := range strings.Split(suggestedCommitMessage(summary), "\n") {
		fmt.Fprintf(out, "    %s\n", line)
	}
	return 0
}
