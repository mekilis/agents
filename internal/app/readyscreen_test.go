package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGatherReadySummaryCollectsEvidence(t *testing.T) {
	withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"History"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := setTaskChunk(e, "scope", []string{"support /history in the terminal"}); err != nil {
			t.Fatal(err)
		}
		taskDir := filepath.Join(cwd, stateDir, task.Path)
		findingsDir := filepath.Join(taskDir, "findings")
		if err := os.MkdirAll(findingsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		finding := "# Finding: history misses edge case\n\nStatus: verified\nSeverity: medium\n\nhistory misses edge case\n"
		if err := os.WriteFile(filepath.Join(findingsDir, "2026-06-05T110000Z-history-misses-edge-case.md"), []byte(finding), 0o644); err != nil {
			t.Fatal(err)
		}
		verificationDir := filepath.Join(taskDir, "verification")
		if err := os.MkdirAll(verificationDir, 0o755); err != nil {
			t.Fatal(err)
		}
		verification := "# Verification: checks\n\nResult: pass\nBy: reviewer\n\nchecks\n\n## Command\n\n$ go test ./...\nexit: 0\n"
		if err := os.WriteFile(filepath.Join(verificationDir, "2026-06-05T120000Z-checks.md"), []byte(verification), 0o644); err != nil {
			t.Fatal(err)
		}

		state := loopState{Task: activeTask{Title: "History", Path: task.Path}}
		summary := gatherReadySummary(e, state, 4)
		if summary.TaskTitle != "History" || summary.Runs != 4 {
			t.Fatalf("unexpected summary header: %+v", summary)
		}
		if summary.Scope != "support /history in the terminal" {
			t.Fatalf("unexpected scope: %q", summary.Scope)
		}
		if len(summary.Findings) != 1 || summary.Findings[0].Status != "verified" ||
			summary.Findings[0].Severity != "medium" || summary.Findings[0].Title != "history misses edge case" {
			t.Fatalf("unexpected findings: %+v", summary.Findings)
		}
		if len(summary.Verification) != 1 || summary.Verification[0].Result != "pass" ||
			summary.Verification[0].Command != "$ go test ./..." {
			t.Fatalf("unexpected verification: %+v", summary.Verification)
		}
		// No git repository here: the screen says so instead of guessing.
		if summary.DiffStat != "(no git diff available)" {
			t.Fatalf("expected git placeholder, got %q", summary.DiffStat)
		}
		return 0
	})
}

func TestGitDiffStatSummarizesWorkingTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-m", "init")
	if got := gitDiffStat(dir); got != "(working tree clean)" {
		t.Fatalf("expected clean tree, got %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := gitDiffStat(dir); !strings.Contains(got, "a.txt") {
		t.Fatalf("expected a.txt in diff stat, got %q", got)
	}
}

func TestReadySummaryLinesRendersSections(t *testing.T) {
	summary := readySummary{
		TaskTitle: "History",
		Scope:     "support /history in the terminal",
		DiffStat:  " internal/app/app.go | 12 ++--",
		Findings: []readyFinding{
			{Status: "verified", Severity: "medium", Title: "history misses edge case"},
			{Status: "open", Severity: "high", Title: "unbounded buffer"},
		},
		Verification: []readyVerification{{Result: "pass", Title: "checks", Command: "$ go test ./..."}},
		Runs:         4,
	}
	text := strings.Join(readySummaryLines(summary), "\n")
	for _, want := range []string{
		"Ready for you — History",
		"Paused for your inspection after 4 runs.",
		"What was asked:",
		"support /history in the terminal",
		"What changed (git diff --stat):",
		"internal/app/app.go | 12 ++--",
		"Findings history:",
		"✓ verified · medium · history misses edge case",
		"● open · high · unbounded buffer",
		"Verification evidence:",
		"pass · checks · $ go test ./...",
		"[a] accept",
		"[i] iterate",
		"[x] abandon",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("ready summary missing %q:\n%s", want, text)
		}
	}

	empty := readySummary{TaskTitle: "Empty", DiffStat: "(working tree clean)"}
	text = strings.Join(readySummaryLines(empty), "\n")
	for _, want := range []string{"(no scope recorded)", "(none recorded)", "(working tree clean)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("empty summary missing %q:\n%s", want, text)
		}
	}
}

func TestFindingGlyphCoversStatuses(t *testing.T) {
	for status, want := range map[string]string{
		"verified": "✓",
		"fixed":    "◐",
		"deferred": "—",
		"wontfix":  "—",
		"open":     "●",
	} {
		if got := findingGlyph(status); got != want {
			t.Fatalf("findingGlyph(%q) got %q want %q", status, got, want)
		}
	}
}

func feedKeys(input string) chan byte {
	keys := make(chan byte, len(input))
	for i := 0; i < len(input); i++ {
		keys <- input[i]
	}
	close(keys)
	return keys
}

func TestRunReadyScreenActions(t *testing.T) {
	summary := readySummary{TaskTitle: "History"}

	var out bytes.Buffer
	action, _ := runReadyScreen(&out, feedKeys("a"), summary)
	if action != readyAccept {
		t.Fatalf("expected accept, got %v", action)
	}
	if !strings.Contains(out.String(), "Accepted — archiving the task") {
		t.Fatalf("expected accept acknowledgement, got:\n%s", out.String())
	}

	out.Reset()
	action, feedback := runReadyScreen(&out, feedKeys("ialso cover redo\r"), summary)
	if action != readyIterate || feedback != "also cover redo" {
		t.Fatalf("expected iterate with feedback, got %v %q", action, feedback)
	}
	if !strings.Contains(out.String(), "What should change?") {
		t.Fatalf("expected iterate prompt, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "routing your feedback to the implementer") {
		t.Fatalf("expected iterate acknowledgement, got:\n%s", out.String())
	}

	// Backspace edits, empty feedback re-prompts, x abandons.
	out.Reset()
	action, feedback = runReadyScreen(&out, feedKeys("iab\x7f\x7f\rx"), summary)
	if action != readyAbandon || feedback != "" {
		t.Fatalf("expected abandon after empty feedback, got %v %q", action, feedback)
	}

	out.Reset()
	action, _ = runReadyScreen(&out, feedKeys("q"), summary)
	if action != readyAbandon {
		t.Fatalf("expected abandon on q, got %v", action)
	}
	if !strings.Contains(out.String(), "leaving the task active") {
		t.Fatalf("expected abandon acknowledgement, got:\n%s", out.String())
	}

	// Closed input without a decision abandons rather than hanging.
	out.Reset()
	action, _ = runReadyScreen(&out, feedKeys(""), summary)
	if action != readyAbandon {
		t.Fatalf("expected abandon on closed input, got %v", action)
	}
}

func TestSuggestedCommitMessage(t *testing.T) {
	if got := suggestedCommitMessage(readySummary{TaskTitle: "History", Scope: "support /history in the terminal"}); got != "History\n\nsupport /history in the terminal" {
		t.Fatalf("unexpected message: %q", got)
	}
	if got := suggestedCommitMessage(readySummary{TaskTitle: "History", Scope: "History"}); got != "History" {
		t.Fatalf("expected subject only when scope repeats title, got %q", got)
	}
	if got := suggestedCommitMessage(readySummary{}); got != "Complete agents task" {
		t.Fatalf("expected fallback subject, got %q", got)
	}
}

func TestAcceptReadyTaskArchivesAndSuggestsCommit(t *testing.T) {
	withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"History"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		summary := readySummary{TaskTitle: "History", Scope: "support /history in the terminal"}
		var out, errOut bytes.Buffer
		if code := acceptReadyTask(&out, &errOut, e, summary); code != 0 {
			t.Fatalf("acceptReadyTask code=%d stderr=%s", code, errOut.String())
		}
		for _, want := range []string{"archived task: History", "Suggested commit message", "History", "support /history in the terminal"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("accept output missing %q:\n%s", want, out.String())
			}
		}
		if _, err := os.Stat(filepath.Join(cwd, stateDir, task.Path)); !os.IsNotExist(err) {
			t.Fatalf("expected task dir removed after archive, err=%v", err)
		}
		if active := readActiveTask(cwd); active.Path != "" {
			t.Fatalf("expected active task cleared, got %+v", active)
		}
		return 0
	})
}
