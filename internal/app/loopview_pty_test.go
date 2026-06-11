//go:build !windows

package app

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

const ptyHelperEnv = "AGENTS_PTY_HELPER"

// TestHelperProcess re-runs the CLI inside the test binary so the PTY test
// can drive `agents go` as a real subprocess. It is inert in normal runs.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(ptyHelperEnv) != "1" {
		t.Skip("helper process only runs under the PTY e2e test")
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			args = args[i+1:]
			break
		}
	}
	os.Exit(Run(args, os.Stdin, os.Stdout, os.Stderr))
}

// TestGoUnifiedViewUnderPTY proves the TUI path end-to-end on a real
// pseudo-terminal: the split view renders both lanes live, the loop reaches
// the human boundary, the Ready-for-you screen shows the evidence, and
// accept archives the task with a suggested commit message.
func TestGoUnifiedViewUnderPTY(t *testing.T) {
	if testing.Short() {
		t.Skip("PTY e2e test skipped in -short mode")
	}
	home := t.TempDir()
	cwd := t.TempDir()
	// macOS resolves /var to /private/var; the workspace trust store records
	// the resolved path, so use it consistently.
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}

	writeStub := func(name, body string) string {
		path := filepath.Join(cwd, name)
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	// Stub records use far-future timestamps so they sort after go's own
	// handoff; the scripts locate the task dir by glob since go creates it.
	implementer := writeStub("implementer.sh", strings.Join([]string{
		"#!/bin/sh",
		"set -eu",
		"echo \"editing main.go\"",
		"task_dir=$(echo .agents/tasks/*)",
		"mkdir -p \"$task_dir/handoffs\"",
		"cat > \"$task_dir/handoffs/2099-01-01T000100Z-handoff-to-reviewer.md\" <<'EOF'",
		"# Handoff: reviewer",
		"",
		"To: reviewer",
		"",
		"Ready for review.",
		"EOF",
		"",
	}, "\n"))
	reviewer := writeStub("reviewer.sh", strings.Join([]string{
		"#!/bin/sh",
		"set -eu",
		"echo \"checking diff\"",
		"task_dir=$(echo .agents/tasks/*)",
		"mkdir -p \"$task_dir/verification\"",
		"cat > \"$task_dir/verification/2099-01-01T000200Z-checks.md\" <<'EOF'",
		"# Verification: checks",
		"",
		"Result: pass",
		"By: reviewer",
		"",
		"All checks pass.",
		"EOF",
		"",
	}, "\n"))
	if err := os.MkdirAll(filepath.Join(cwd, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "roles:\n  implementer: " + implementer + " {prompt_file}\n  reviewer: " + reviewer + " {prompt_file}\n  coordinator: codex\n"
	if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := trustWorkspace(env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--", "go", "History: support /history in the terminal")
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), ptyHelperEnv+"=1", "HOME="+home)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 30, Cols: 100})
	if err != nil {
		t.Skipf("cannot allocate pty: %v", err)
	}
	defer func() { _ = ptmx.Close() }()

	var mu sync.Mutex
	var output bytes.Buffer
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 4096)
		for {
			n, readErr := ptmx.Read(buf)
			if n > 0 {
				mu.Lock()
				output.Write(buf[:n])
				mu.Unlock()
			}
			if readErr != nil {
				return
			}
		}
	}()
	snapshot := func() string {
		mu.Lock()
		defer mu.Unlock()
		return output.String()
	}
	waitFor := func(marker string) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if strings.Contains(snapshot(), marker) {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %q in PTY output:\n%s", marker, snapshot())
	}

	// The loop runs to the human boundary, then the view flips to the
	// Ready-for-you screen; accept archives and suggests a commit message.
	waitFor("Your call:")
	if _, err := io.WriteString(ptmx, "a"); err != nil {
		t.Fatal(err)
	}

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("agents go exited with error: %v\n%s", err, snapshot())
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("agents go did not exit after accept:\n%s", snapshot())
	}
	_ = ptmx.Close()
	<-readDone

	got := snapshot()
	for _, want := range []string{
		"Building",                         // split view lane
		"Checking",                         // split view lane
		"editing main.go",                  // live implementer output
		"checking diff",                    // live reviewer output
		"Ready for you — History",          // boundary screen
		"support /history in the terminal", // scope section
		"Verification evidence:",           // evidence section
		"archived task: History",           // accept archived
		"Suggested commit message",         // accept suggested a message
		"[q]uit [p]ause",                   // key hints while running
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("PTY output missing %q:\n%s", want, got)
		}
	}
	if _, err := os.Stat(filepath.Join(cwd, ".agents", "archive")); err != nil {
		t.Fatalf("expected archive directory after accept: %v", err)
	}
	if active := readActiveTask(cwd); active.Path != "" {
		t.Fatalf("expected active task cleared after accept, got %+v", active)
	}
}
