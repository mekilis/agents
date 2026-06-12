package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTrustWorkspaceRoundTrip(t *testing.T) {
	t.Setenv("USER", "agents-test-user")
	dir := t.TempDir()
	e := env{
		cwd:       filepath.Join(dir, "repo"),
		home:      dir,
		trustPath: filepath.Join(dir, ".agents", "trust.json"),
	}

	trusted, err := isTrusted(e)
	if err != nil {
		t.Fatal(err)
	}
	if trusted {
		t.Fatal("new workspace should not start trusted")
	}

	if _, err := trustWorkspace(e); err != nil {
		t.Fatal(err)
	}

	trusted, err = isTrusted(e)
	if err != nil {
		t.Fatal(err)
	}
	if !trusted {
		t.Fatal("workspace should be trusted after trustWorkspace")
	}

	store, err := readTrustStore(e.trustPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Workspaces) != 1 {
		t.Fatalf("expected one trusted workspace, got %+v", store.Workspaces)
	}
	got := store.Workspaces[0]
	if got.User != "agents-test-user" {
		t.Fatalf("expected trust user to be recorded, got %+v", got)
	}
	if got.Host == "" {
		t.Fatalf("expected trust host to be recorded, got %+v", got)
	}
}

func TestTrustWorkspaceRecordsAndChecksGitRemote(t *testing.T) {
	t.Setenv("USER", "agents-test-user")
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeGitConfig(t, repo, "git@example.com:agents/agents.git")
	e := env{
		cwd:       repo,
		home:      dir,
		trustPath: filepath.Join(dir, ".agents", "trust.json"),
	}
	if _, err := trustWorkspace(e); err != nil {
		t.Fatal(err)
	}
	store, err := readTrustStore(e.trustPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.Workspaces[0].GitRemote; got != "git@example.com:agents/agents.git" {
		t.Fatalf("expected git remote to be recorded, got %q", got)
	}
	trusted, err := isTrusted(e)
	if err != nil {
		t.Fatal(err)
	}
	if !trusted {
		t.Fatal("workspace should be trusted while remote matches")
	}

	writeGitConfig(t, repo, "git@example.com:someone-else/agents.git")
	trusted, err = isTrusted(e)
	if err != nil {
		t.Fatal(err)
	}
	if trusted {
		t.Fatal("workspace should not stay trusted after origin remote changes")
	}

	record, err := trustWorkspace(e)
	if err != nil {
		t.Fatal(err)
	}
	if record.GitRemote != "git@example.com:someone-else/agents.git" {
		t.Fatalf("expected trust refresh to record changed remote, got %+v", record)
	}
	trusted, err = isTrusted(e)
	if err != nil {
		t.Fatal(err)
	}
	if !trusted {
		t.Fatal("workspace should be trusted again after explicit trust refresh")
	}
}

func TestLegacyTrustRecordWithoutGitRemoteStillMatches(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeGitConfig(t, repo, "git@example.com:agents/agents.git")
	e := env{
		cwd:       repo,
		home:      dir,
		trustPath: filepath.Join(dir, ".agents", "trust.json"),
	}
	if err := writeTrustStore(e.trustPath, trustStore{Workspaces: []trustedWorkspace{{
		Path:      repo,
		TrustedAt: time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}}}); err != nil {
		t.Fatal(err)
	}
	trusted, err := isTrusted(e)
	if err != nil {
		t.Fatal(err)
	}
	if !trusted {
		t.Fatal("legacy trust record without git remote should remain trusted")
	}
}

func TestRunTrustCommandWritesTrustRecord(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		code := Run([]string{"trust"}, bytes.NewBuffer(nil), &out, &errOut)
		if code != 0 {
			return code
		}
		e, err := currentEnv(bytes.NewBuffer(nil))
		if err != nil {
			t.Fatal(err)
		}
		trusted, err := isTrusted(e)
		if err != nil {
			t.Fatal(err)
		}
		if !trusted {
			t.Fatal("workspace should be trusted after agents trust")
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("Run trust code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "trusted:") {
		t.Fatalf("expected trust output, got:\n%s", out.String())
	}
}

func TestRunTrustCommandJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		if err := os.MkdirAll(filepath.Join(cwd, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeGitConfig(t, cwd, "git@example.com:agents/agents.git")
		code := Run([]string{"trust", "--json"}, bytes.NewBuffer(nil), &out, &errOut)
		if code != 0 {
			return code
		}
		e, err := currentEnv(bytes.NewBuffer(nil))
		if err != nil {
			t.Fatal(err)
		}
		trusted, err := isTrusted(e)
		if err != nil {
			t.Fatal(err)
		}
		if !trusted {
			t.Fatal("workspace should be trusted after agents trust --json")
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("Run trust --json code=%d stderr=%s", code, errOut.String())
	}
	var got trustJSON
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("trust JSON should decode: %v\n%s", err, out.String())
	}
	if got.Trust != "trusted" || got.Workspace == "" || got.GitRemote != "git@example.com:agents/agents.git" {
		t.Fatalf("unexpected trust JSON: %+v", got)
	}
}

func TestRunTrustCommandRejectsUnknownFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run([]string{"trust", "--later"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatal("expected trust with unknown flag to fail")
	}
	if !strings.Contains(errOut.String(), "unknown flag --later") {
		t.Fatalf("expected unknown flag error, got %q", errOut.String())
	}
}

func TestRunStatusDoesNotPrintBanner(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run([]string{"status"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run status code=%d stderr=%s", code, errOut.String())
	}
	if strings.Contains(out.String(), "AGENTS") {
		t.Fatalf("status should not print interactive banner, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "active task: none") {
		t.Fatalf("status should show active task, got:\n%s", out.String())
	}
}

func TestRunUnknownCommandUsesPublicCLIError(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run([]string{"review", "this", "repo"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatal("expected unknown command to fail")
	}
	got := errOut.String()
	if !strings.Contains(got, "unknown command: review this repo") {
		t.Fatalf("expected unknown command error, got %q", got)
	}
	if strings.Contains(got, "wiring gap") || strings.Contains(got, "internal error") {
		t.Fatalf("unknown command should not expose implementation-detail wording, got %q", got)
	}
	if !strings.Contains(got, "agents help") {
		t.Fatalf("expected help hint, got %q", got)
	}
}

func TestRunVersionCommand(t *testing.T) {
	old := versionInfo
	t.Cleanup(func() { versionInfo = old })
	SetVersion("v1.2.3", "abc123", "2026-06-05")
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run([]string{"version"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run version code=%d stderr=%s", code, errOut.String())
	}
	if got := out.String(); !strings.Contains(got, "agents v1.2.3 (abc123, 2026-06-05)") {
		t.Fatalf("unexpected version output: %q", got)
	}
}

func TestHelpShowsTrustCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run([]string{"help"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run help code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "agents trust [--json]") {
		t.Fatalf("help should show trust command, got:\n%s", out.String())
	}
}

func TestStatusShowsActiveTaskSummary(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		taskDir := filepath.Join(cwd, ".agents", "tasks", "2026-06-04-001-demo--demo-1")
		if err := os.MkdirAll(taskDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "active"), []byte("tasks/2026-06-04-001-demo--demo-1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		task := "# Demo Task\n\nstatus: active\n\nA demo task.\n"
		if err := os.WriteFile(filepath.Join(taskDir, "task.md"), []byte(task), 0o644); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"status"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run status code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		"active task: Demo Task",
		"task status: active",
		"task path: tasks/2026-06-04-001-demo--demo-1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("status missing %q, got:\n%s", want, got)
		}
	}
}

func TestStatusShowsActiveTaskRecordSummary(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Record Summary"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := addDecision(e, []string{"Record summary should count decisions"}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := addHandoff(e, []string{"--to", "reviewer", "Please review status output"}, time.Date(2026, 6, 5, 10, 2, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		openFinding, err := addFinding(e, []string{"open issue"}, time.Date(2026, 6, 5, 10, 3, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		fixedFinding, err := addFinding(e, []string{"fixed issue"}, time.Date(2026, 6, 5, 10, 4, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := updateFindingStatus(e, []string{fixedFinding.Path}, "fixed", "Fixed", time.Date(2026, 6, 5, 10, 5, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		verifiedFinding, err := addFinding(e, []string{"verified issue"}, time.Date(2026, 6, 5, 10, 6, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := updateFindingStatus(e, []string{verifiedFinding.Path, "--by", "implementer"}, "fixed", "Fixed", time.Date(2026, 6, 5, 10, 6, 30, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := updateFindingStatus(e, []string{verifiedFinding.Path, "--by", "reviewer"}, "verified", "Verification", time.Date(2026, 6, 5, 10, 7, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(cwd, ".agents", task.Path, "verification"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cwd, ".agents", task.Path, "verification", "2026-06-05T100800Z-go-test.md"), []byte("# Verification\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if openFinding.Path == "" {
			t.Fatal("open finding should exist")
		}
		return Run([]string{"status"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run status code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		"next: decide next step",
		"findings: open=1 fixed=1 verified=1",
		"records: decisions=1 handoffs=1 verification=1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("status missing %q, got:\n%s", want, got)
		}
	}
}

func TestStatusJSONNoActiveTask(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run([]string{"status", "--json"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run status --json code=%d stderr=%s", code, errOut.String())
	}
	var got struct {
		Workspace  string          `json:"workspace"`
		Trust      string          `json:"trust"`
		ActiveTask json.RawMessage `json:"active_task"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("status JSON should decode: %v\n%s", err, out.String())
	}
	if got.Workspace == "" || got.Trust != "not trusted" {
		t.Fatalf("unexpected status JSON: %+v", got)
	}
	if string(got.ActiveTask) != "null" {
		t.Fatalf("expected active_task null, got %s", got.ActiveTask)
	}
}

func TestStatusJSONActiveTask(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"JSON Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := addDecision(e, []string{"Expose status as JSON"}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(cwd, ".agents", task.Path, "verification"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cwd, ".agents", task.Path, "verification", "2026-06-05T100200Z-json.md"), []byte("# Verification\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"status", "--json"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run status --json code=%d stderr=%s", code, errOut.String())
	}
	var got statusJSON
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("status JSON should decode: %v\n%s", err, out.String())
	}
	if got.ActiveTask == nil {
		t.Fatal("expected active task")
	}
	if got.ActiveTask.Title != "JSON Task" || got.ActiveTask.Records.Decisions != 1 || got.ActiveTask.Records.Verification != 1 {
		t.Fatalf("unexpected active task JSON: %+v", got.ActiveTask)
	}
}

func TestStatusJSONRejectsUnknownFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run([]string{"status", "--nope"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatal("expected status with unknown flag to fail")
	}
	if !strings.Contains(errOut.String(), "unknown flag --nope") {
		t.Fatalf("expected unknown flag error, got %q", errOut.String())
	}
}

func TestActiveTaskRejectsEscapingPath(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".agents", "active"), []byte("../outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readActiveTask(repo); got.Path != "" {
		t.Fatalf("expected escaping active path to be ignored, got %+v", got)
	}
}

func TestInitCreatesShareableWorkspaceState(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		code := Run([]string{"init"}, bytes.NewBuffer(nil), &out, &errOut)
		if code != 0 {
			return code
		}
		got := out.String()
		for _, want := range []string{
			"task state: share",
			"created: .agents/config.yml",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("init output missing %q, got:\n%s", want, got)
			}
		}
		config, err := os.ReadFile(filepath.Join(cwd, ".agents", "config.yml"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(config), "commit_chunks: true") {
			t.Fatalf("expected shared config, got:\n%s", config)
		}
		for _, want := range []string{
			"implementer: agent",
			"reviewer: codex",
			"coordinator: codex",
			`codex_model: ""`,
			`codex_reasoning: ""`,
			`claude_model: ""`,
			`claude_effort: ""`,
			`cursor_model: ""`,
		} {
			if !strings.Contains(string(config), want) {
				t.Fatalf("expected default role %q, got:\n%s", want, config)
			}
		}
		if _, err := os.Stat(filepath.Join(cwd, ".agents", "tasks")); err != nil {
			t.Fatal(err)
		}
		agentsIgnore, err := os.ReadFile(filepath.Join(cwd, ".agents", ".gitignore"))
		if err != nil {
			t.Fatal(err)
		}
		if string(agentsIgnore) != "runs/\ntmp/\nactive\n" {
			t.Fatalf("expected .agents/.gitignore to ignore runs, tmp, and active, got:\n%s", agentsIgnore)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("Run init code=%d stderr=%s", code, errOut.String())
	}
}

func TestInitKeepsExistingAgentsGitignore(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		if err := os.MkdirAll(filepath.Join(cwd, ".agents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cwd, ".agents", ".gitignore"), []byte("cache/\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		code := Run([]string{"init"}, bytes.NewBuffer(nil), &out, &errOut)
		if code != 0 {
			return code
		}
		got, err := os.ReadFile(filepath.Join(cwd, ".agents", ".gitignore"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "cache/\nruns/\ntmp/\nactive\n" {
			t.Fatalf("unexpected .agents/.gitignore:\n%s", got)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("Run init code=%d stderr=%s", code, errOut.String())
	}
}

func TestInitLocalAddsAgentsToGitignore(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		if err := os.WriteFile(filepath.Join(cwd, ".gitignore"), []byte("bin/\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		code := Run([]string{"init", "--local"}, bytes.NewBuffer(nil), &out, &errOut)
		if code != 0 {
			return code
		}
		config, err := os.ReadFile(filepath.Join(cwd, ".agents", "config.yml"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(config), "commit_chunks: false") {
			t.Fatalf("expected local config, got:\n%s", config)
		}
		ignore, err := os.ReadFile(filepath.Join(cwd, ".gitignore"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(ignore), ".agents/") {
			t.Fatalf("expected .agents/ in .gitignore, got:\n%s", ignore)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("Run init --local code=%d stderr=%s", code, errOut.String())
	}
}

func TestInitKeepsExistingConfig(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		if err := os.MkdirAll(filepath.Join(cwd, ".agents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte("custom: true\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		code := Run([]string{"init"}, bytes.NewBuffer(nil), &out, &errOut)
		if code != 0 {
			return code
		}
		if !strings.Contains(out.String(), "kept: .agents/config.yml") {
			t.Fatalf("expected kept config output, got:\n%s", out.String())
		}
		config, err := os.ReadFile(filepath.Join(cwd, ".agents", "config.yml"))
		if err != nil {
			t.Fatal(err)
		}
		if string(config) != "custom: true\n" {
			t.Fatalf("config should not be overwritten, got:\n%s", config)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("Run init code=%d stderr=%s", code, errOut.String())
	}
}

func TestInitRejectsUnknownFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run([]string{"init", "--maybe"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatalf("expected init with unknown flag to fail")
	}
	if !strings.Contains(errOut.String(), "unknown flag") {
		t.Fatalf("expected unknown flag error, got %q", errOut.String())
	}
}

func TestCompletionZshPrintsScript(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run([]string{"completion", "zsh"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run completion zsh code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		"#compdef agents",
		"'init:initialize Agents state'",
		"'completion:generate shell completion'",
		"'--local:ignore .agents in this repo'",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("completion script missing %q, got:\n%s", want, got)
		}
	}
}

func TestCompletionInstallZshWritesCompletionAndZshrc(t *testing.T) {
	var out, errOut bytes.Buffer
	var testHome string
	code := withTempProcessEnv(t, func(home, cwd string) int {
		testHome = home
		return Run([]string{"completion", "install-zsh"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run completion install-zsh code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "installed:") {
		t.Fatalf("install output should mention installed path, got:\n%s", out.String())
	}
	completion, err := os.ReadFile(filepath.Join(testHome, ".zsh", "completions", "_agents"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(completion), "#compdef agents") {
		t.Fatalf("completion file missing script:\n%s", completion)
	}
	zshrc, err := os.ReadFile(filepath.Join(testHome, ".zshrc"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".zsh/completions", "autoload -Uz compinit", "compinit"} {
		if !strings.Contains(string(zshrc), want) {
			t.Fatalf(".zshrc missing %q:\n%s", want, zshrc)
		}
	}
}

func TestStartOpensTaskAndHandoffToImplementer(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		code := Run([]string{"start", "Fix todo search", "--ref", "BUG-44", "Fix uppercase todo matching and add tests"}, bytes.NewBuffer(nil), &out, &errOut)
		if code != 0 {
			return code
		}
		active := readActiveTask(cwd)
		if active.Title != "Fix todo search" {
			t.Fatalf("unexpected active task: %+v", active)
		}
		handoffDir := filepath.Join(cwd, ".agents", active.Path, "handoffs")
		entries, err := os.ReadDir(handoffDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected one handoff, got %d", len(entries))
		}
		data, err := os.ReadFile(filepath.Join(handoffDir, entries[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		got := string(data)
		for _, want := range []string{"To: implementer", "Fix uppercase todo matching and add tests"} {
			if !strings.Contains(got, want) {
				t.Fatalf("handoff missing %q, got:\n%s", want, got)
			}
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("Run start code=%d stderr=%s", code, errOut.String())
	}
	for _, want := range []string{"started task: Fix todo search", "handoff to: implementer"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("start output missing %q, got:\n%s", want, out.String())
		}
	}
}

func TestStartRequiresInstruction(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run([]string{"start", "Fix todo search"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatal("expected start without instruction to fail")
	}
	if !strings.Contains(errOut.String(), "instruction text is required") {
		t.Fatalf("expected instruction error, got %q", errOut.String())
	}
}

func TestSplitGoText(t *testing.T) {
	for _, tc := range []struct {
		text, title, instruction string
	}{
		{"Add /history command", "Add /history command", "Add /history command"},
		{"History: support /history in the terminal", "History", "support /history in the terminal"},
		{"Fix http://example.com link", "Fix http://example.com link", "Fix http://example.com link"},
		{"Trailing colon: ", "Trailing colon: ", "Trailing colon: "},
	} {
		title, instruction := splitGoText(tc.text)
		if title != tc.title || instruction != tc.instruction {
			t.Fatalf("splitGoText(%q) = (%q, %q), want (%q, %q)", tc.text, title, instruction, tc.title, tc.instruction)
		}
	}
}

func TestGoRequiresTrustBeforeCreatingTask(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		code := Run([]string{"go", "Add /history command"}, bytes.NewBuffer(nil), &out, &errOut)
		if _, err := os.Stat(filepath.Join(cwd, ".agents", "tasks")); !os.IsNotExist(err) {
			t.Fatalf("untrusted go must not create task state, stat err=%v", err)
		}
		return code
	})
	if code != 1 {
		t.Fatalf("expected untrusted go to exit 1, got %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "not trusted") {
		t.Fatalf("expected trust error, got %q", errOut.String())
	}
}

func TestGoRequiresInstruction(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run([]string{"go"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 2 {
		t.Fatalf("expected usage error exit 2, got %d", code)
	}
	if !strings.Contains(errOut.String(), "instruction text is required") {
		t.Fatalf("expected instruction error, got %q", errOut.String())
	}
}

func TestGoOpensTaskScopeAndHandoffWithoutConfiguredRoles(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		code := Run([]string{"go", "History: support /history in the terminal"}, bytes.NewBuffer(nil), &out, &errOut)
		active := readActiveTask(cwd)
		if active.Title != "History" {
			t.Fatalf("expected title from colon split, got %+v", active)
		}
		scope, err := os.ReadFile(filepath.Join(cwd, ".agents", active.Path, "scope.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(scope), "support /history in the terminal") {
			t.Fatalf("scope missing instruction:\n%s", scope)
		}
		handoffDir := filepath.Join(cwd, ".agents", active.Path, "handoffs")
		entries, err := os.ReadDir(handoffDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected one handoff, got %d", len(entries))
		}
		data, err := os.ReadFile(filepath.Join(handoffDir, entries[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"To: implementer", "support /history in the terminal"} {
			if !strings.Contains(string(data), want) {
				t.Fatalf("handoff missing %q, got:\n%s", want, data)
			}
		}
		return code
	})
	// No roles configured: the loop stops at "unconfigured" with exit 0.
	if code != 0 {
		t.Fatalf("expected go to stop cleanly at unconfigured, code=%d stderr=%s", code, errOut.String())
	}
	for _, want := range []string{"task: History", "agent: not configured for implementer"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("go output missing %q, got:\n%s", want, out.String())
		}
	}
}

func TestGoReportsLoopErrorsAsGo(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		implementer := filepath.Join(cwd, "implementer.sh")
		if err := os.WriteFile(implementer, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := "roles:\n  implementer: " + implementer + " {prompt_file}\n  reviewer: " + implementer + " {prompt_file}\n  coordinator: codex\n"
		if err := os.MkdirAll(filepath.Join(cwd, ".agents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"go", "Doomed task"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 1 {
		t.Fatalf("expected failing loop to exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "agents: go: implementer command failed") {
		t.Fatalf("expected error attributed to go, got %q", errOut.String())
	}
	if strings.Contains(errOut.String(), "agents: run:") {
		t.Fatalf("go must not report errors as agents: run:, got %q", errOut.String())
	}
}

func TestGoRunsLoopToHumanBoundary(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		// go opens the task itself, so the stubs locate it by glob. Stub
		// records use far-future timestamps to sort after go's own handoff.
		implementer := filepath.Join(cwd, "implementer.sh")
		implementerBody := strings.Join([]string{
			"#!/bin/sh",
			"set -eu",
			"trap 'exit 0' INT TERM",
			"task_dir=$(echo .agents/tasks/*)",
			"mkdir -p \"$task_dir/handoffs\"",
			"cat > \"$task_dir/handoffs/2099-01-01T000100Z-handoff-to-reviewer.md\" <<'EOF'",
			"# Handoff: reviewer",
			"",
			"To: reviewer",
			"From: implementer",
			"",
			"Ready for review.",
			"EOF",
			"",
		}, "\n")
		if err := os.WriteFile(implementer, []byte(implementerBody), 0o755); err != nil {
			t.Fatal(err)
		}
		reviewer := filepath.Join(cwd, "reviewer.sh")
		reviewerBody := strings.Join([]string{
			"#!/bin/sh",
			"set -eu",
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
		}, "\n")
		if err := os.WriteFile(reviewer, []byte(reviewerBody), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := "roles:\n  implementer: " + implementer + " {prompt_file}\n  reviewer: " + reviewer + " {prompt_file}\n  coordinator: codex\n"
		if err := os.MkdirAll(filepath.Join(cwd, ".agents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"go", "Add /history command"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("expected go to reach human boundary, code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	got := out.String()
	for _, want := range []string{
		"task: Add /history command",
		"running implementer...",
		"running reviewer...",
		"human approval required",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("go output missing %q:\n%s", want, got)
		}
	}
}

func TestInteractiveRejectsUntrustedNonTTY(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run(nil, bytes.NewBufferString(""), &out, &errOut)
	})
	if code == 0 {
		t.Fatalf("expected non-tty untrusted run to fail")
	}
	if !strings.Contains(errOut.String(), "not trusted") {
		t.Fatalf("expected trust error, got %q", errOut.String())
	}
	if strings.Contains(out.String(), "AGENTS") || strings.Contains(out.String(), "o---o") {
		t.Fatalf("non-tty untrusted run should not print banner, got:\n%s", out.String())
	}
}

func TestInteractiveTrustThenRunsLoop(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		writeDemoActiveTask(t, cwd)
		e := env{
			cwd:       cwd,
			home:      home,
			isTTY:     true,
			trustPath: filepath.Join(home, ".agents", "trust.json"),
		}
		return runInteractive(bytes.NewBufferString("y\n/status\n/quit\n"), &out, &errOut, e)
	})
	if code != 0 {
		t.Fatalf("runInteractive code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		"AGENTS",
		"Trusted this folder.",
		"active task: Demo Task",
		"Type plain text to chat with the coordinator.",
		"Press ? for shortcuts",
		"Tab completes slash commands",
		"› ",
		"Goodbye.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("interactive output missing %q, got:\n%s", want, got)
		}
	}
	if strings.Count(got, "active task: Demo Task") < 2 {
		t.Fatalf("status command should print the active task again, got:\n%s", got)
	}
}

func TestPrintBannerUsesFullAgentsWordmark(t *testing.T) {
	var out bytes.Buffer
	printBanner(&out)
	got := out.String()
	for _, want := range []string{
		"AGENTS",
		"   / \\   __ _  ___ _ __ | |_ ___ ",
		" / ___ \\ (_| |  __/ | | | |_\\__ \\",
		"        |___/                     ",
		"coding agents, coordinated",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("banner missing %q, got:\n%s", want, got)
		}
	}
	if lines := strings.Count(strings.TrimRight(got, "\n"), "\n") + 1; lines != 8 {
		t.Fatalf("banner should be 8 lines, got %d:\n%s", lines, got)
	}
}

func TestInteractiveCanDisableBanner(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		writeDemoActiveTask(t, cwd)
		e := env{
			cwd:           cwd,
			home:          home,
			isTTY:         true,
			disableBanner: true,
			trustPath:     filepath.Join(home, ".agents", "trust.json"),
		}
		return runInteractive(bytes.NewBufferString("y\n/status\n/quit\n"), &out, &errOut, e)
	})
	if code != 0 {
		t.Fatalf("runInteractive code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	if strings.Contains(got, "coding agents, coordinated") || strings.Contains(got, "/ ___ \\") {
		t.Fatalf("disabled banner should not print wordmark, got:\n%s", got)
	}
	for _, want := range []string{
		"Trusted this folder.",
		"active task: Demo Task",
		"Goodbye.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("interactive output missing %q, got:\n%s", want, got)
		}
	}
}

func TestBannerDisabledFromEnv(t *testing.T) {
	for _, value := range []string{"1", "true", "TRUE", "yes", "on"} {
		if !bannerDisabledFromEnv(value) {
			t.Fatalf("expected %q to disable banner", value)
		}
	}
	for _, value := range []string{"", "0", "false", "no", "off", "sometimes"} {
		if bannerDisabledFromEnv(value) {
			t.Fatalf("expected %q not to disable banner", value)
		}
	}
}

func TestInteractiveHelpAndExit(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{
			cwd:       cwd,
			home:      home,
			isTTY:     false,
			trustPath: filepath.Join(home, ".agents", "trust.json"),
		}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		return runInteractive(bytes.NewBufferString("?\n/exit\n"), &out, &errOut, e)
	})
	if code != 0 {
		t.Fatalf("runInteractive code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		"Shortcuts:",
		"Plain text  Chat with the coordinator",
		"Tab         Complete a unique slash command",
		"Slash commands:",
		`/status`,
		"Show workspace, active task, and record counts",
		`/task open|use|scope|status|next ...`,
		`/decision add "text"`,
		"/run",
		"/quit",
		"Goodbye.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("interactive help missing %q, got:\n%s", want, got)
		}
	}
}

func TestInteractiveSlashPrefixShowsMatchingCommands(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{
			cwd:       cwd,
			home:      home,
			isTTY:     false,
			trustPath: filepath.Join(home, ".agents", "trust.json"),
		}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		return runInteractive(bytes.NewBufferString("/st\n/\n/quit\n"), &out, &errOut, e)
	})
	if code != 0 {
		t.Fatalf("runInteractive code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	if !strings.Contains(got, "/status") || !strings.Contains(got, "Show workspace, active task, and record counts") {
		t.Fatalf("prefix should suggest /status, got:\n%s", got)
	}
	if !strings.Contains(got, "/start") || !strings.Contains(got, "Fast path") {
		t.Fatalf("bare slash should show full command menu, got:\n%s", got)
	}
	if strings.Contains(got, "unknown command: /st") {
		t.Fatalf("prefix should not run as unknown command, got:\n%s", got)
	}
}

func TestInteractiveUniqueSlashPrefixExecutes(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{
			cwd:       cwd,
			home:      home,
			isTTY:     false,
			trustPath: filepath.Join(home, ".agents", "trust.json"),
		}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		return runInteractive(bytes.NewBufferString("/q\n"), &out, &errOut, e)
	})
	if code != 0 {
		t.Fatalf("runInteractive code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Goodbye.") {
		t.Fatalf("/q should resolve to /quit, got:\n%s", out.String())
	}
}

func TestInteractiveExactQuitExits(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{
			cwd:       cwd,
			home:      home,
			isTTY:     false,
			trustPath: filepath.Join(home, ".agents", "trust.json"),
		}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		return runInteractive(bytes.NewBufferString("/quit\n"), &out, &errOut, e)
	})
	if code != 0 {
		t.Fatalf("runInteractive code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Goodbye.") {
		t.Fatalf("/quit should exit, got:\n%s", out.String())
	}
	if strings.Contains(errOut.String(), "unknown command") {
		t.Fatalf("/quit should not produce unknown command, got stderr:\n%s", errOut.String())
	}
}

func TestInteractiveUniqueSlashPrefixPreservesArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{
			cwd:       cwd,
			home:      home,
			isTTY:     false,
			trustPath: filepath.Join(home, ".agents", "trust.json"),
		}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		input := strings.Join([]string{
			`/task open "Prefix Task"`,
			`/dec add "Prefixes resolve"`,
			`/sta`,
			`/qu`,
			"",
		}, "\n")
		return runInteractive(bytes.NewBufferString(input), &out, &errOut, e)
	})
	if code != 0 {
		t.Fatalf("runInteractive code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		"decision: Prefixes resolve",
		"Slash commands:",
		"/start",
		"/status",
		"Goodbye.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("interactive output missing %q, got:\n%s", want, got)
		}
	}
}

func TestCompleteSlashInputCompletesUniquePrefix(t *testing.T) {
	got, ok := completeSlashInput("/qu")
	if !ok || got != "/quit " {
		t.Fatalf("completeSlashInput(/qu) got %q ok=%v", got, ok)
	}
}

func TestCompleteSlashInputLeavesAmbiguousPrefix(t *testing.T) {
	got, ok := completeSlashInput("/st")
	if ok || got != "/st" {
		t.Fatalf("completeSlashInput(/st) got %q ok=%v", got, ok)
	}
}

func TestComposerSuggestionsHideExactCommand(t *testing.T) {
	if got := composerSuggestionLines("/quit", false); len(got) != 0 {
		t.Fatalf("exact slash command should not keep suggestions open, got %#v", got)
	}
	if got := composerSuggestionLines("/qu", false); len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), "/quit") {
		t.Fatalf("partial slash command should suggest /quit, got %#v", got)
	}
}

func TestRenderComposerLineColorsExactSlashCommand(t *testing.T) {
	got := renderComposerLine("/implement all", true)
	if !strings.Contains(got, "\x1b[36m/implement\x1b[0m all") {
		t.Fatalf("slash command should be colored, got %q", got)
	}
	if plain := renderComposerLine("/implement all", false); plain != "/implement all" {
		t.Fatalf("plain composer line got %q", plain)
	}
}

func TestComposerSuggestionsAreCompact(t *testing.T) {
	got := composerSuggestionLines("/", false)
	if len(got) > liveSuggestionLimit+1 {
		t.Fatalf("live suggestions should stay compact, got %d lines: %#v", len(got), got)
	}
	text := strings.Join(got, "\n")
	if !strings.Contains(text, "more") || !strings.Contains(text, "? for all") {
		t.Fatalf("compact suggestions should point to full help, got:\n%s", text)
	}
}

func TestReadComposerLineIgnoresArrowKeys(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	go func() {
		_, _ = write.Write([]byte("\x1b[A\x1b[A/review\n"))
	}()
	var out bytes.Buffer
	got, err := readComposerLine(read, &out, env{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/review" {
		t.Fatalf("composer should ignore arrow keys, got %q; rendered:\n%s", got, out.String())
	}
}

func TestReadComposerLineNavigatesHistory(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	history := []string{"/review --max-iterations 6", "/implement all"}
	go func() {
		_, _ = write.Write([]byte("\x1b[A\n"))
	}()
	var out bytes.Buffer
	got, err := readComposerLine(read, &out, env{}, &history)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/implement all" {
		t.Fatalf("composer history got %q", got)
	}
}

func TestAppendCommandHistoryTrimsDedupesAndCaps(t *testing.T) {
	var history []string
	history = appendCommandHistory(history, "  /run  ")
	history = appendCommandHistory(history, "/run")
	history = appendCommandHistory(history, "")
	if len(history) != 1 || history[0] != "/run" {
		t.Fatalf("history should trim, skip blank, and dedupe adjacent entries, got %#v", history)
	}
	for i := 0; i < 120; i++ {
		history = appendCommandHistory(history, fmt.Sprintf("/cmd-%03d", i))
	}
	if len(history) != 100 {
		t.Fatalf("history should be capped at 100 entries, got %d", len(history))
	}
	if history[0] != "/cmd-020" || history[len(history)-1] != "/cmd-119" {
		t.Fatalf("history cap kept wrong entries: first=%q last=%q", history[0], history[len(history)-1])
	}
}

func TestReadEscapeInputHandlesApplicationCursorKeys(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	go func() {
		_, _ = write.Write([]byte("OA"))
	}()
	escape, pasted, ok, err := readEscapeInput(read)
	if err != nil {
		t.Fatal(err)
	}
	if escape != "up" || pasted != "" || ok {
		t.Fatalf("application cursor key got escape=%q pasted=%q ok=%v", escape, pasted, ok)
	}
}

func TestReadBracketedPasteBodyReturnsPastedText(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	go func() {
		_, _ = write.Write([]byte("hello\nworld\x1b[201~"))
	}()
	got, ok, err := readBracketedPasteBody(read)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != "hello\nworld" {
		t.Fatalf("bracketed paste got %q ok=%v", got, ok)
	}
}

func TestParseInteractiveRoleCommandArgsAcceptsText(t *testing.T) {
	maxRuns, note, err := parseInteractiveRoleCommandArgs([]string{"all", "--max-iterations", "2"})
	if err != nil {
		t.Fatal(err)
	}
	if maxRuns != 2 || note != "all" {
		t.Fatalf("parse args got maxRuns=%d note=%q", maxRuns, note)
	}
}

func TestNormalizePastedTextJoinsLines(t *testing.T) {
	got := normalizePastedText("I want to build a tiny dependency-free project from scratch.\nMake it runnable.\n\nAdd tests.")
	want := "I want to build a tiny dependency-free project from scratch. Make it runnable. Add tests."
	if got != want {
		t.Fatalf("normalizePastedText got %q want %q", got, want)
	}
}

func TestCleanCodexExecOutputReturnsFinalAnswer(t *testing.T) {
	raw := strings.Join([]string{
		"Reading additional input from stdin...",
		"OpenAI Codex v0.137.0",
		"user",
		"prompt text",
		"codex",
		"I will inspect files.",
		"exec",
		"rg --files",
		"hook: Stop",
		"tok" + "ens used",
		"21,091",
		"Here is the clear coordinator reply.",
		"",
		"What should it do?",
	}, "\n")
	got := cleanInteractiveAgentOutput("codex", raw)
	want := "Here is the clear coordinator reply.\n\nWhat should it do?"
	if got != want {
		t.Fatalf("cleanInteractiveAgentOutput got:\n%q\nwant:\n%q", got, want)
	}
	reply := cleanInteractiveAgentReply("codex", raw)
	if reply.Usage != "21,091 tokens" {
		t.Fatalf("usage got %q", reply.Usage)
	}
}

func TestCleanInteractiveAgentOutputHidesAgentsRecordOutput(t *testing.T) {
	raw := strings.Join([]string{
		"Reading additional input from stdin...",
		"What should this project do?",
		"Aborting operation...",
		`2026-06-11T12:03:39.367911Z  WARN codex_core_plugins::manifest: ignoring interface.defaultPrompt[0]: prompt must be at most 128 characters path=/home/user/.codex/plugin.json`,
		"handoff to: implementer",
		"path: tasks/example/handoffs/file.md",
	}, "\n")
	got := cleanInteractiveAgentOutput("custom", raw)
	want := "What should this project do?"
	if got != want {
		t.Fatalf("cleanInteractiveAgentOutput got %q want %q", got, want)
	}
}

func TestCleanInteractiveAgentReplyExtractsTokenUsage(t *testing.T) {
	raw := strings.Join([]string{
		"Here is the answer.",
		"",
		"Token usage: total=10,626 input=10,609 output=17",
	}, "\n")
	reply := cleanInteractiveAgentReply("claude", raw)
	if reply.Text != "Here is the answer." {
		t.Fatalf("reply text got %q", reply.Text)
	}
	if reply.Usage != "total=10,626 input=10,609 output=17" {
		t.Fatalf("usage got %q", reply.Usage)
	}
}

func TestPrintInteractiveRoleRunResultFallsBackToTaskProgress(t *testing.T) {
	var out bytes.Buffer
	result := roleRunResult{
		Role:        "implementer",
		Agent:       "agent",
		AgentOutput: "fixed finding: tasks/example/findings/cache.md\nhandoff to: reviewer\npath: tasks/example/handoffs/reviewer.md\nToken usage: input=7 output=3 cache_read=11",
		Before: loopState{NextRole: "implementer", Stats: taskStats{
			Handoffs:     3,
			FindingsOpen: 4,
		}},
		After: loopState{NextRole: "reviewer", Stats: taskStats{
			Handoffs:        4,
			LatestHandoffTo: "reviewer",
			FindingsOpen:    0,
			FindingsFixed:   4,
		}},
	}
	printInteractiveRoleRunResult(&out, result)
	got := ansiEscapes.ReplaceAllString(out.String(), "")
	for _, want := range []string{
		"Handoff recorded for reviewer.",
		"Finding fixed (4 fixed).",
		"Usage: input=7 output=3 cache_read=11",
		"Next: Reviewer",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("interactive result missing %q, got:\n%s", want, got)
		}
	}
}

func TestPrintInteractiveRoleHeaderUsesStyledTitle(t *testing.T) {
	var out bytes.Buffer
	printInteractiveRoleHeader(&out, "reviewer")
	got := ansiEscapes.ReplaceAllString(out.String(), "")
	if !strings.Contains(got, "Reviewer") {
		t.Fatalf("role header missing title:\n%s", got)
	}
	for _, unwanted := range []string{"##", "--------"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("role header should not render %q:\n%s", unwanted, got)
		}
	}
}

func TestPrintLoopStateOmitsStaleTaskNextAtHumanBoundary(t *testing.T) {
	var out bytes.Buffer
	printLoopState(&out, loopState{
		Task: activeTask{
			Title: "Tiny habits CLI",
			Path:  "tasks/2026-06-12-001-tiny-habits-cli",
		},
		Stats: taskStats{
			Next:             "Reviewer should verify the cache artifacts are gone.",
			FindingsVerified: 4,
			Handoffs:         19,
			Verification:     2,
		},
		NextRole:   "human",
		NextAction: "approve archive, commit, or next task",
		Boundary:   "human approval required",
	})
	got := out.String()
	for _, want := range []string{
		"next role: human",
		"next action: approve archive, commit, or next task",
		"boundary: human approval required",
		"findings: open=0 fixed=0 verified=4",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("loop state missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Reviewer should verify") {
		t.Fatalf("human boundary should not show stale task next:\n%s", got)
	}
}

func TestRenderTerminalMarkdownFormatsMarkdown(t *testing.T) {
	rendered, err := renderTerminalMarkdown("## What it does\n\n- **Task** routing\n\n| Concept | What it is |\n| --- | --- |\n| Task | Scoped work |\n", 80)
	if err != nil {
		t.Fatal(err)
	}
	plain := ansiEscapes.ReplaceAllString(rendered, "")
	for _, want := range []string{"What it does", "Task", "Concept", "Scoped work"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("rendered markdown missing %q:\n%s", want, plain)
		}
	}
}

func TestInteractiveRunHeaderWriterOnlyShowsRunPointers(t *testing.T) {
	var out bytes.Buffer
	writer := interactiveRunHeaderWriter{out: &out}
	raw := strings.Join([]string{
		"run id: 2026-06-11T120337Z-reviewer",
		"run log: .agents/runs/logs/2026-06-11T120337Z-reviewer.log",
		"Reading additional input from stdin...",
		"OpenAI Codex v0.137.0",
		`2026-06-11T12:03:39.367911Z  WARN codex_core_plugins::manifest: ignoring interface.defaultPrompt[0]: prompt must be at most 128 characters`,
		"```agents-progress",
		`{"status":"running checks","detail":"verifying tests and task records"}`,
		"```",
		"reviewer answer",
		"",
	}, "\n")
	if _, err := writer.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"run: 2026-06-11T120337Z-reviewer",
		"log: .agents/runs/logs/2026-06-11T120337Z-reviewer.log",
		"- Running checks: Verifying tests and task records",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("header writer missing %q, got:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"OpenAI Codex", "128 characters", "reviewer answer"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("header writer should hide %q, got:\n%s", unwanted, got)
		}
	}
}

func TestCleanInteractiveAgentOutputRemovesProgressBlocks(t *testing.T) {
	raw := strings.Join([]string{
		"```agents-progress",
		`{"status":"Editing files","detail":"Updating parser"}`,
		"```",
		"Implemented todo CLI.",
	}, "\n")
	got := cleanInteractiveAgentOutput("agent", raw)
	if got != "Implemented todo CLI." {
		t.Fatalf("progress block should be removed from final output, got %q", got)
	}
}

func TestProgressWriterSuppressesProviderOutput(t *testing.T) {
	var out bytes.Buffer
	writer := newProgressWriter(&out, "coordinator is thinking")
	if n, err := writer.Write([]byte("raw provider transcript")); err != nil || n != len("raw provider transcript") {
		t.Fatalf("progress writer Write n=%d err=%v", n, err)
	}
	if strings.Contains(out.String(), "raw provider transcript") {
		t.Fatalf("progress writer should not echo provider output, got %q", out.String())
	}
}

func TestProgressWriterDedupesSuccessiveProgress(t *testing.T) {
	var out bytes.Buffer
	writer := newProgressWriter(&out, "coordinator is thinking")
	block := strings.Join([]string{
		"```agents-progress",
		`{"status":"Routing to implementer","detail":"Main CLI work is complete; implementer should fix open cache-file findings before final review"}`,
		"```",
		"```agents-progress",
		`{"status":"Routing to implementer","detail":"Main CLI work is complete; implementer should fix open cache-file findings before final review"}`,
		"```",
		"```agents-progress",
		`{"status":"Checking context","detail":"Reading the latest task state"}`,
		"```",
		"```agents-progress",
		`{"status":"Routing to implementer","detail":"Main CLI work is complete; implementer should fix open cache-file findings before final review"}`,
		"```",
		"",
	}, "\n")
	if _, err := writer.Write([]byte(block)); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if count := strings.Count(got, "Routing to implementer"); count != 2 {
		t.Fatalf("expected only successive duplicate progress to be suppressed, count=%d output=%q", count, got)
	}
}

func TestInteractiveRunHeaderWriterDedupesSuccessiveProgress(t *testing.T) {
	var out bytes.Buffer
	writer := interactiveRunHeaderWriter{out: &out}
	block := strings.Join([]string{
		"```agents-progress",
		`{"status":"Running checks","detail":"Executing focused tests"}`,
		"```",
		"```agents-progress",
		`{"status":"Running checks","detail":"Executing focused tests"}`,
		"```",
		"```agents-progress",
		`{"status":"Reviewing files","detail":"Reading implementation"}`,
		"```",
		"```agents-progress",
		`{"status":"Running checks","detail":"Executing focused tests"}`,
		"```",
		"",
	}, "\n")
	if _, err := writer.Write([]byte(block)); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if count := strings.Count(got, "Running checks"); count != 2 {
		t.Fatalf("expected only successive duplicate progress to be suppressed, count=%d output=%q", count, got)
	}
}

func TestCoordinatorDirectiveParsesAndRemovesHandoffBlock(t *testing.T) {
	raw := "Ready to build.\n\n```agents-handoff\nrole: implementer\nready: true\ntext: Build the todo CLI.\nInclude README.\n```\n"
	directive, ok := parseCoordinatorDirective(raw)
	if !ok {
		t.Fatal("expected directive to parse")
	}
	if directive.Role != "implementer" {
		t.Fatalf("role got %q", directive.Role)
	}
	if !directive.Ready {
		t.Fatal("expected ready directive")
	}
	if directive.Text != "Build the todo CLI.\nInclude README." {
		t.Fatalf("text got %q", directive.Text)
	}
	if got := removeCoordinatorDirective(raw); got != "Ready to build." {
		t.Fatalf("removeCoordinatorDirective got %q", got)
	}
}

func TestCoordinatorDirectiveParsesJSONHandoffBlock(t *testing.T) {
	raw := "Ready.\n\n```agents-handoff\n{\"role\":\"implementer\",\"ready\":true,\"text\":\"Build the count command.\"}\n```\n"
	directive, ok := parseCoordinatorDirective(raw)
	if !ok {
		t.Fatal("expected directive to parse")
	}
	if directive.Role != "implementer" || !directive.Ready || directive.Text != "Build the count command." {
		t.Fatalf("unexpected directive: %+v", directive)
	}
	if cleaned := removeCoordinatorDirective(raw); cleaned != "Ready." {
		t.Fatalf("cleaned got %q", cleaned)
	}
}

func TestVerificationDirectiveParsesAndRecords(t *testing.T) {
	raw := "Looks clean.\n\n```agents-verify\n{\"text\":\"go test ./... passed\",\"cmd\":\"\"}\n```\n"
	directive, ok := parseVerificationDirective(raw)
	if !ok {
		t.Fatal("expected verification directive to parse")
	}
	if directive.Text != "go test ./... passed" || directive.Cmd != "" {
		t.Fatalf("unexpected directive: %+v", directive)
	}
	if cleaned := removeVerificationDirective(raw); cleaned != "Looks clean." {
		t.Fatalf("cleaned got %q", cleaned)
	}

	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Review"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if err := applyVerificationDirective(e, "reviewer", directive); err != nil {
			t.Fatal(err)
		}
		stats, err := readTaskStats(cwd, readActiveTask(cwd).Path)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Verification != 1 {
			t.Fatalf("verification count got %d", stats.Verification)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestCoordinatorCannotRecordVerificationDirective(t *testing.T) {
	raw := "Looks clean.\n\n```agents-verify\n{\"text\":\"tests passed\",\"cmd\":\"go test ./...\"}\n```\n"
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Review"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		_, err := applyRoleOutputDirectives(e, "coordinator", raw)
		if err == nil || !strings.Contains(err.Error(), "coordinator cannot record verification") {
			t.Fatalf("expected coordinator verification rejection, got %v", err)
		}
		stats, err := readTaskStats(cwd, readActiveTask(cwd).Path)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Verification != 0 {
			t.Fatalf("coordinator must not record verification, got %d", stats.Verification)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestCoordinatorPromptAvoidsCannedProgressAndAllowsReadOnlyRepoInspection(t *testing.T) {
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Planning"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		prompt, err := buildRolePrompt(e, loopState{
			Task:       task,
			NextRole:   "coordinator",
			NextAction: "answer latest coordinator chat",
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, unwanted := range []string{
			"Shaping scope",
			"Checking existing project context and missing constraints",
		} {
			if strings.Contains(prompt, unwanted) {
				t.Fatalf("coordinator prompt should not contain canned progress %q:\n%s", unwanted, prompt)
			}
		}
		for _, want := range []string{
			"Task record path: .agents/tasks/",
			"For greetings, thanks, or other small talk, answer briefly",
			"do not inspect the repository, summarize files, recommend code work, or emit progress",
			"Inspect the repository only when the user explicitly asks",
			"use read-only commands such as listing files, reading docs, and searching source",
			"repo-relative paths such as `README.md` or `.agents/tasks/...`",
			"do not print absolute machine-local paths",
			"Task record paths shown as `.agents/tasks/...` are inside the Agents state directory",
			"do not use canned or example status text",
		} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("coordinator prompt missing %q:\n%s", want, prompt)
			}
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestApplyCoordinatorDirectiveWritesHandoffFromParent(t *testing.T) {
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Brokered Handoff"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		directive := coordinatorDirective{Role: "implementer", Text: "Build the MVP.", Ready: true}
		if err := applyCoordinatorDirective(e, "coordinator", directive); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(filepath.Join(cwd, ".agents", task.Path, "handoffs"))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected one handoff, got %d", len(entries))
		}
		data, err := os.ReadFile(filepath.Join(cwd, ".agents", task.Path, "handoffs", entries[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		if got := string(data); !strings.Contains(got, "To: implementer") || !strings.Contains(got, "Build the MVP.") {
			t.Fatalf("unexpected handoff:\n%s", got)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestCoordinatorImplementerHandoffRequiresReady(t *testing.T) {
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Planning Only"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		directive := coordinatorDirective{Role: "implementer", Text: "Maybe implement this later."}
		if err := applyCoordinatorDirective(e, "coordinator", directive); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(filepath.Join(cwd, ".agents", task.Path, "handoffs"))
		if errors.Is(err, os.ErrNotExist) {
			return 0
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("planning-only coordinator handoff should not route to implementer, got %d entries", len(entries))
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestCoordinatorReviewerHandoffRequiresReady(t *testing.T) {
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Review Route"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		directive := coordinatorDirective{Role: "reviewer", Text: "Review the latest changes."}
		if err := applyCoordinatorDirective(e, "coordinator", directive); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(filepath.Join(cwd, ".agents", task.Path, "handoffs"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("planning-only coordinator handoff should not route to reviewer, got %d entries", len(entries))
		}

		directive.Ready = true
		if err := applyCoordinatorDirective(e, "coordinator", directive); err != nil {
			t.Fatal(err)
		}
		entries, err = os.ReadDir(filepath.Join(cwd, ".agents", task.Path, "handoffs"))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("ready coordinator handoff should route to reviewer, got %d entries", len(entries))
		}
		data, err := os.ReadFile(filepath.Join(cwd, ".agents", task.Path, "handoffs", entries[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		if got := string(data); !strings.Contains(got, "To: reviewer") || !strings.Contains(got, "Review the latest changes.") {
			t.Fatalf("unexpected handoff:\n%s", got)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestCoordinatorImplementerHandoffRejectsPlaceholderText(t *testing.T) {
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Planning Only"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		directive := coordinatorDirective{Role: "implementer", Text: "clear implementation request with context and verification", Ready: true}
		if err := applyCoordinatorDirective(e, "coordinator", directive); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(filepath.Join(cwd, ".agents", task.Path, "handoffs"))
		if errors.Is(err, os.ErrNotExist) {
			return 0
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("placeholder coordinator handoff should not route to implementer, got %d entries", len(entries))
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestApplyRoleOutputDirectivesBrokersReviewerHandoff(t *testing.T) {
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Reviewer Broker"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		raw := "No findings.\n\n```agents-handoff\nrole: coordinator\ntext: Review approved. node --check passed.\n```\n"
		cleaned, err := applyRoleOutputDirectives(e, "reviewer", raw)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(cleaned, "agents-handoff") {
			t.Fatalf("expected directive removed, got:\n%s", cleaned)
		}
		entries, err := os.ReadDir(filepath.Join(cwd, ".agents", task.Path, "handoffs"))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected one handoff, got %d", len(entries))
		}
		data, err := os.ReadFile(filepath.Join(cwd, ".agents", task.Path, "handoffs", entries[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		if got := string(data); !strings.Contains(got, "To: coordinator") || !strings.Contains(got, "Review approved") {
			t.Fatalf("unexpected handoff:\n%s", got)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestApplyRoleOutputDirectivesParsesJSONReviewerHandoff(t *testing.T) {
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Reviewer Handoff"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		raw := "Implemented.\n\n```agents-handoff\n{\"role\":\"reviewer\",\"text\":\"Added count command and go test ./... passed.\"}\n```\n"
		cleaned, err := applyRoleOutputDirectives(e, "implementer", raw)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(cleaned, "agents-handoff") {
			t.Fatalf("expected directive removed, got:\n%s", cleaned)
		}
		entries, err := os.ReadDir(filepath.Join(cwd, ".agents", task.Path, "handoffs"))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected one handoff, got %d", len(entries))
		}
		data, err := os.ReadFile(filepath.Join(cwd, ".agents", task.Path, "handoffs", entries[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		if got := string(data); !strings.Contains(got, "To: reviewer") || !strings.Contains(got, "Added count command") {
			t.Fatalf("unexpected handoff:\n%s", got)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestApplyRoleOutputDirectivesSkipsPlaceholderAndDuplicateVerification(t *testing.T) {
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Review"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		raw := strings.Join([]string{
			"Prompt echo:",
			"```agents-verify",
			`{"text":"specific checks performed and review result","cmd":"go test ./..."}`,
			"```",
			"",
			"Provider answer:",
			"```agents-verify",
			`{"text":"python tests passed","cmd":""}`,
			"```",
			"",
			"Provider answer repeated:",
			"```agents-verify",
			`{"text":"python tests passed","cmd":""}`,
			"```",
		}, "\n")
		cleaned, err := applyRoleOutputDirectives(e, "reviewer", raw)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(cleaned, "agents-verify") {
			t.Fatalf("expected directives removed, got:\n%s", cleaned)
		}
		stats, err := readTaskStats(cwd, task.Path)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Verification != 1 || stats.VerificationFail != 0 {
			t.Fatalf("verification stats got pass=%d fail=%d", stats.Verification, stats.VerificationFail)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestApplyRoleOutputDirectivesSkipsPlaceholderHandoff(t *testing.T) {
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Implement"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		raw := "Echo.\n\n```agents-handoff\n{\"role\":\"reviewer\",\"text\":\"specific changes made and checks performed\"}\n```\n"
		cleaned, err := applyRoleOutputDirectives(e, "implementer", raw)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(cleaned, "agents-handoff") {
			t.Fatalf("expected directive removed, got:\n%s", cleaned)
		}
		stats, err := readTaskStats(cwd, task.Path)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Handoffs != 0 {
			t.Fatalf("handoff count got %d", stats.Handoffs)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestTaskProgressLinesSummarizeStateChanges(t *testing.T) {
	before := taskStats{
		Next:         "decide next step",
		Handoffs:     1,
		FindingsOpen: 1,
	}
	after := taskStats{
		Next:             "review implementation",
		Handoffs:         2,
		LatestHandoffTo:  "reviewer",
		Verification:     1,
		VerificationFail: 1,
		FindingsOpen:     2,
		FindingsFixed:    1,
		FindingsVerified: 1,
	}
	got := strings.Join(taskProgressLines(before, after), "\n")
	for _, want := range []string{
		"Handoff recorded for Checking.",
		"Verification recorded.",
		"Verification failed.",
		"Finding added (2 open).",
		"Finding fixed (1 fixed).",
		"Finding verified (1 verified).",
		"Next: review implementation",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("progress lines missing %q:\n%s", want, got)
		}
	}
}

func TestInteractiveRequiresSlashForCommandsAndChatsWithCoordinator(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{
			cwd:       cwd,
			home:      home,
			isTTY:     false,
			trustPath: filepath.Join(home, ".agents", "trust.json"),
		}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		input := strings.Join([]string{
			`status`,
			`I want to add todo labels, but I need help shaping the plan.`,
			`/status`,
			`/quit`,
			"",
		}, "\n")
		return runInteractive(bytes.NewBufferString(input), &out, &errOut, e)
	})
	if code != 0 {
		t.Fatalf("runInteractive code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		"Started coordinator intake: status",
		"Sent to coordinator.",
		"active task: status",
		"records: decisions=0 handoffs=2 verification=0",
		"Goodbye.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("interactive output missing %q, got:\n%s", want, got)
		}
	}
}

func TestInteractiveTTYChatRunsCoordinatorResponse(t *testing.T) {
	var out bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{
			cwd:       cwd,
			home:      home,
			isTTY:     true,
			trustPath: filepath.Join(home, ".agents", "trust.json"),
		}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		if _, err := initWorkspace(e, "share"); err != nil {
			t.Fatal(err)
		}
		script := filepath.Join(cwd, "coordinator.sh")
		body := "#!/bin/sh\nset -eu\nprintf 'Hi. What constraints or reference should I use?\\n'\n"
		if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := "roles:\n  implementer: agent\n  reviewer: codex\n  coordinator: " + script + " {prompt_file}\n"
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := recordCoordinatorChat(e, "hi", &out); err != nil {
			t.Fatal(err)
		}
		if err := runCoordinatorChatResponse(e, &out); err != nil {
			t.Fatal(err)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
	got := out.String()
	for _, want := range []string{
		"run id:",
		"run log: .agents/runs/logs/",
		"Coordinator is thinking",
		"Hi. What constraints or reference should I use?",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("interactive coordinator response missing %q, got:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{
		"started intake:",
		"sent to coordinator",
		"handoff to:",
		"path:",
		"ran role:",
		"prompt:",
	} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("interactive coordinator response should hide %q, got:\n%s", unwanted, got)
		}
	}
}

func TestInteractiveTTYChatRunsCoordinatorWithOpenFindings(t *testing.T) {
	var out bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{
			cwd:       cwd,
			home:      home,
			isTTY:     true,
			trustPath: filepath.Join(home, ".agents", "trust.json"),
		}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		if _, err := initWorkspace(e, "share"); err != nil {
			t.Fatal(err)
		}
		if _, err := openTask(e, []string{"Tiny Habits CLI"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := addFinding(e, []string{"cache files remain", "--by", "reviewer"}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		script := filepath.Join(cwd, "coordinator.sh")
		body := "#!/bin/sh\nset -eu\nprintf 'You are in the active Tiny Habits task; next implementation should fix open findings.\\n'\n"
		if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := "roles:\n  implementer: agent\n  reviewer: codex\n  coordinator: " + script + " {prompt_file}\n"
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := recordCoordinatorChat(e, "where am i", &out); err != nil {
			t.Fatal(err)
		}
		if err := runCoordinatorChatResponse(e, &out); err != nil {
			t.Fatal(err)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
	got := out.String()
	if !strings.Contains(got, "You are in the active Tiny Habits task") {
		t.Fatalf("coordinator should answer even with open findings, got:\n%s", got)
	}
	if strings.Contains(got, "next role: implementer") {
		t.Fatalf("chat should not be replaced by loop state, got:\n%s", got)
	}
	if strings.Contains(got, "Ready for Implementer") {
		t.Fatalf("chat answer should not print a ready cue without an actionable handoff, got:\n%s", got)
	}
}

func TestInteractiveTTYChatRoutesButWaitsForRun(t *testing.T) {
	var out bytes.Buffer
	var runCwd string
	code := withTempProcessEnv(t, func(home, cwd string) int {
		runCwd = cwd
		e := env{
			cwd:       cwd,
			home:      home,
			isTTY:     true,
			trustPath: filepath.Join(home, ".agents", "trust.json"),
		}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		if _, err := initWorkspace(e, "share"); err != nil {
			t.Fatal(err)
		}
		coordinator := filepath.Join(cwd, "coordinator.sh")
		coordinatorBody := strings.Join([]string{
			"#!/bin/sh",
			"set -eu",
			"cat <<'EOF'",
			"Ready to build.",
			"",
			"```agents-handoff",
			"role: implementer",
			"ready: true",
			"text: Build the todo CLI.",
			"```",
			"EOF",
			"",
		}, "\n")
		if err := os.WriteFile(coordinator, []byte(coordinatorBody), 0o755); err != nil {
			t.Fatal(err)
		}
		implementer := filepath.Join(cwd, "implementer.sh")
		implementerBody := "#!/bin/sh\nset -eu\ntouch implemented-marker\nprintf 'implemented todo CLI\\n'\n"
		if err := os.WriteFile(implementer, []byte(implementerBody), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := "roles:\n  implementer: " + implementer + " {prompt_file}\n  reviewer: codex\n  coordinator: " + coordinator + " {prompt_file}\n"
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := recordCoordinatorChat(e, "go on", &out); err != nil {
			t.Fatal(err)
		}
		if err := runCoordinatorChatResponse(e, &out); err != nil {
			t.Fatal(err)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
	got := out.String()
	for _, want := range []string{
		"Ready to build.",
		"Ready for Implementer. Type /run to start.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("interactive coordinator output missing %q, got:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"Implementer\n-----------", "implemented todo CLI", "running implementer...", "ran role: implementer", "agent output:", "prompt:"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("interactive coordinator output should wait for /run and hide %q, got:\n%s", unwanted, got)
		}
	}
	if _, err := os.Stat(filepath.Join(runCwd, "implemented-marker")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("implementer should not have run during coordinator chat, stat err=%v", err)
	}
}

func TestInteractiveReviewCommandRunsReviewerDirectly(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{
			cwd:       cwd,
			home:      home,
			isTTY:     true,
			trustPath: filepath.Join(home, ".agents", "trust.json"),
		}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		if _, err := initWorkspace(e, "share"); err != nil {
			t.Fatal(err)
		}
		task, err := openTask(e, []string{"Review Command"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := addHandoff(e, []string{"--to", "implementer", "Build it."}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		reviewer := filepath.Join(cwd, "reviewer.sh")
		verificationPath := filepath.Join(".agents", task.Path, "verification", "2099-01-01T000000Z-review.md")
		reviewerBody := strings.Join([]string{
			"#!/bin/sh",
			"set -eu",
			"mkdir -p " + shellQuote(filepath.Dir(verificationPath)),
			"cat > " + shellQuote(verificationPath) + " <<'EOF'",
			"# Verification: review",
			"",
			"Result: pass",
			"By: reviewer",
			"",
			"Review passed.",
			"EOF",
			"printf 'reviewer ran\\n'",
			"",
		}, "\n")
		if err := os.WriteFile(reviewer, []byte(reviewerBody), 0o755); err != nil {
			t.Fatal(err)
		}
		coordinator := filepath.Join(cwd, "coordinator.sh")
		if err := os.WriteFile(coordinator, []byte("#!/bin/sh\nset -eu\ntouch coordinator-ran\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := "roles:\n  implementer: agent\n  reviewer: " + reviewer + " {prompt_file}\n  coordinator: " + coordinator + " {prompt_file}\n"
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		commandCode, done := handleInteractiveCommand("/review --max-iterations 1", &out, &errOut, e)
		if done {
			t.Fatal("/review should not exit interactive terminal")
		}
		if _, err := os.Stat(filepath.Join(cwd, "coordinator-ran")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("/review should not run coordinator, stat err=%v", err)
		}
		return commandCode
	})
	if code != 0 {
		t.Fatalf("/review code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	got := ansiEscapes.ReplaceAllString(out.String(), "")
	if !strings.Contains(got, "Reviewer") || !strings.Contains(got, "reviewer ran") {
		t.Fatalf("/review should run reviewer, got:\n%s", got)
	}
	if strings.Contains(got, "Coordinator is thinking") {
		t.Fatalf("/review should bypass coordinator chat, got:\n%s", got)
	}
}

func TestPrintInterruptHintIncludesActiveTask(t *testing.T) {
	var out bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Interruptible Session"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		printInterruptHint(&out, e)
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
	got := out.String()
	for _, want := range []string{
		"Active task: Interruptible Session",
		"To continue this session, run agents",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("interrupt hint missing %q, got:\n%s", want, got)
		}
	}
}

func TestInteractiveRunsSlashStartCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{
			cwd:       cwd,
			home:      home,
			isTTY:     false,
			trustPath: filepath.Join(home, ".agents", "trust.json"),
		}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		input := strings.Join([]string{
			`/start "Fix todo search" --ref BUG-44 "Fix uppercase todo matching and add tests"`,
			`/status`,
			`/quit`,
			"",
		}, "\n")
		return runInteractive(bytes.NewBufferString(input), &out, &errOut, e)
	})
	if code != 0 {
		t.Fatalf("runInteractive code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		"started task: Fix todo search",
		"handoff to: implementer",
		"active task: Fix todo search",
		"records: decisions=0 handoffs=1 verification=0",
		"Goodbye.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("interactive output missing %q, got:\n%s", want, got)
		}
	}
}

func TestParseCommandLine(t *testing.T) {
	got, err := parseCommandLine(`task open "Provider Driver Config" --ref PROVIDERS-1`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"task", "open", "Provider Driver Config", "--ref", "PROVIDERS-1"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("parseCommandLine got %#v want %#v", got, want)
	}
}

func TestParseCommandLineRejectsUnterminatedQuote(t *testing.T) {
	if _, err := parseCommandLine(`decision add "missing end`); err == nil {
		t.Fatalf("expected unterminated quote error")
	}
}

func TestTaskOpenCreatesChunksAndSetsActive(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		code := Run([]string{"task", "open", "Provider Driver Config", "--ref", "PROVIDERS-1"}, bytes.NewBuffer(nil), &out, &errOut)
		if code != 0 {
			return code
		}
		got := out.String()
		taskPath := "tasks/" + time.Now().UTC().Format("2006-01-02") + "-001-provider-driver-config--providers-1"
		for _, want := range []string{
			"opened task: Provider Driver Config",
			"task path: " + taskPath,
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("task open output missing %q, got:\n%s", want, got)
			}
		}
		taskMarkdown, err := os.ReadFile(filepath.Join(cwd, ".agents", taskPath, "task.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(taskMarkdown), "ref: PROVIDERS-1") {
			t.Fatalf("task.md missing ref, got:\n%s", taskMarkdown)
		}
		if strings.HasSuffix(string(taskMarkdown), "\n\n") {
			t.Fatalf("task.md should not end with a blank line, got:\n%s", taskMarkdown)
		}
		if active := readActiveTask(cwd); active.Title != "Provider Driver Config" {
			t.Fatalf("unexpected active task %+v", active)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("Run task open code=%d stderr=%s", code, errOut.String())
	}
}

func TestOpenTaskIncrementsSequenceForDate(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	e := env{cwd: repo, home: dir, trustPath: filepath.Join(dir, ".agents", "trust.json")}
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	first, err := openTask(e, []string{"First Task"}, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := openTask(e, []string{"Second Task"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Path != "tasks/2026-06-05-001-first-task" {
		t.Fatalf("unexpected first path %q", first.Path)
	}
	if second.Path != "tasks/2026-06-05-002-second-task" {
		t.Fatalf("unexpected second path %q", second.Path)
	}
}

func TestTaskUseSwitchesActiveTask(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		firstDir := filepath.Join(cwd, ".agents", "tasks", "2026-06-05-001-first")
		secondDir := filepath.Join(cwd, ".agents", "tasks", "2026-06-05-002-second")
		if err := os.MkdirAll(firstDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(secondDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(firstDir, "task.md"), []byte("# First\n\nstatus: active\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(secondDir, "task.md"), []byte("# Second\n\nstatus: active\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"task", "use", "tasks/2026-06-05-002-second"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run task use code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		"active task: Second",
		"task path: tasks/2026-06-05-002-second",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("task use output missing %q, got:\n%s", want, got)
		}
	}
}

func TestTaskUseRejectsEscapingPath(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run([]string{"task", "use", "../outside"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatalf("expected escaping task use to fail")
	}
	if !strings.Contains(errOut.String(), "stay inside .agents") {
		t.Fatalf("expected escaping path error, got %q", errOut.String())
	}
}

func TestDecisionAddWritesChunkUnderActiveTask(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		task, err := openTask(env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}, []string{"Task With Decision"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		code := Run([]string{"decision", "add", "Reviewers do not edit code"}, bytes.NewBuffer(nil), &out, &errOut)
		if code != 0 {
			return code
		}
		decisionDir := filepath.Join(cwd, ".agents", task.Path, "decisions")
		entries, err := os.ReadDir(decisionDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected one decision, got %d", len(entries))
		}
		data, err := os.ReadFile(filepath.Join(decisionDir, entries[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		got := string(data)
		for _, want := range []string{
			"# Decision: Reviewers do not edit code",
			"Status: proposed",
			"Reviewers do not edit code",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("decision missing %q, got:\n%s", want, got)
			}
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("Run decision add code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "decision: Reviewers do not edit code") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestDecisionAddRequiresActiveTask(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run([]string{"decision", "add", "A decision"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatalf("expected decision add without active task to fail")
	}
	if !strings.Contains(errOut.String(), "no active task") {
		t.Fatalf("expected active task error, got %q", errOut.String())
	}
}

func TestHandoffWritesChunkUnderActiveTask(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		task, err := openTask(env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}, []string{"Task With Handoff"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		code := Run([]string{"handoff", "--to", "reviewer", "Please review the task lifecycle commands."}, bytes.NewBuffer(nil), &out, &errOut)
		if code != 0 {
			return code
		}
		handoffDir := filepath.Join(cwd, ".agents", task.Path, "handoffs")
		entries, err := os.ReadDir(handoffDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected one handoff, got %d", len(entries))
		}
		data, err := os.ReadFile(filepath.Join(handoffDir, entries[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		got := string(data)
		for _, want := range []string{
			"# Handoff: reviewer",
			"To: reviewer",
			"Please review the task lifecycle commands.",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("handoff missing %q, got:\n%s", want, got)
			}
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("Run handoff code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "handoff to: reviewer") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestHandoffRequiresTo(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run([]string{"handoff", "Please review this."}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatalf("expected handoff without --to to fail")
	}
	if !strings.Contains(errOut.String(), "--to is required") {
		t.Fatalf("expected --to error, got %q", errOut.String())
	}
}

func TestFindingAddWritesOpenFinding(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		task, err := openTask(env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}, []string{"Task With Finding"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		code := Run([]string{"finding", "add", "fetch failure is ignored", "--severity", "high"}, bytes.NewBuffer(nil), &out, &errOut)
		if code != 0 {
			return code
		}
		findingDir := filepath.Join(cwd, ".agents", task.Path, "findings")
		entries, err := os.ReadDir(findingDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected one finding, got %d", len(entries))
		}
		data, err := os.ReadFile(filepath.Join(findingDir, entries[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		got := string(data)
		for _, want := range []string{
			"# Finding: fetch failure is ignored",
			"Status: open",
			"Severity: high",
			"fetch failure is ignored",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("finding missing %q, got:\n%s", want, got)
			}
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("Run finding add code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "finding: fetch failure is ignored") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestFindingFixAndVerifyUpdateStatus(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		task, err := openTask(env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}, []string{"Task With Finding"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		record, err := addFinding(env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}, []string{"fetch failure is ignored"}, time.Date(2026, 6, 5, 11, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		base := filepath.Base(record.Path)
		if code := Run([]string{"finding", "fix", base, "--by", "implementer"}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
			return code
		}
		if code := Run([]string{"finding", "verify", record.Path, "--by", "reviewer", "--evidence", "reran focused tests"}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
			return code
		}
		data, err := os.ReadFile(filepath.Join(cwd, ".agents", record.Path))
		if err != nil {
			t.Fatal(err)
		}
		got := string(data)
		for _, want := range []string{
			"Status: verified",
			"## Fixed",
			"By: implementer",
			"## Verification",
			"By: reviewer",
			"Evidence: reran focused tests",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("updated finding missing %q, got:\n%s", want, got)
			}
		}
		if task.Path == "" {
			t.Fatal("task should exist")
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("Run finding fix/verify code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "fixed finding:") || !strings.Contains(out.String(), "verified finding:") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestFindingAddRejectsInvalidSeverity(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		if _, err := openTask(env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}, []string{"Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"finding", "add", "bad thing", "--severity", "urgent"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatalf("expected invalid severity to fail")
	}
	if !strings.Contains(errOut.String(), "severity must be") {
		t.Fatalf("expected severity error, got %q", errOut.String())
	}
}

func TestFindingRequiresActiveTask(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run([]string{"finding", "add", "A finding"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatalf("expected finding add without active task to fail")
	}
	if !strings.Contains(errOut.String(), "no active task") {
		t.Fatalf("expected active task error, got %q", errOut.String())
	}
}

func TestFindingUpdateRejectsEscapingPath(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		if _, err := openTask(env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}, []string{"Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"finding", "fix", "../outside"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatalf("expected escaping finding path to fail")
	}
	if !strings.Contains(errOut.String(), "stay inside active task findings") {
		t.Fatalf("expected escaping path error, got %q", errOut.String())
	}
}

func TestArchiveSummarizesAndPrunesActiveTask(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Archive Me"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := addDecision(e, []string{"Keep durable decisions"}, time.Date(2026, 6, 5, 10, 5, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := addHandoff(e, []string{"--to", "reviewer", "Review before archive"}, time.Date(2026, 6, 5, 10, 6, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cwd, ".agents", task.Path, "scope.md"), []byte("# Scope\n\n- keep durable outcome\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cwd, ".agents", task.Path, "status.md"), []byte("# Status\n\nImplemented:\n\n- archive keeps useful summaries\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		openFinding, err := addFinding(e, []string{"open issue", "--severity", "high"}, time.Date(2026, 6, 5, 10, 7, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		verifiedFinding, err := addFinding(e, []string{"verified issue"}, time.Date(2026, 6, 5, 10, 8, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := updateFindingStatus(e, []string{verifiedFinding.Path, "--by", "implementer"}, "fixed", "Fixed", time.Date(2026, 6, 5, 10, 8, 30, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := updateFindingStatus(e, []string{verifiedFinding.Path, "--by", "reviewer"}, "verified", "Verification", time.Date(2026, 6, 5, 10, 9, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}

		code := Run([]string{"archive"}, bytes.NewBuffer(nil), &out, &errOut)
		if code != 0 {
			return code
		}
		archiveDir := filepath.Join(cwd, ".agents", "archive", filepath.Base(task.Path))
		summary, err := os.ReadFile(filepath.Join(archiveDir, "summary.md"))
		if err != nil {
			t.Fatal(err)
		}
		got := string(summary)
		for _, want := range []string{
			"# Archive Me",
			"status: archived",
			"- decisions: 1",
			"- handoffs: 1",
			"- findings open: 1",
			"- findings verified: 1",
			"## Scope",
			"- keep durable outcome",
			"## Final Status",
			"- archive keeps useful summaries",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("archive summary missing %q, got:\n%s", want, got)
			}
		}
		if _, err := os.Stat(filepath.Join(cwd, ".agents", task.Path)); !os.IsNotExist(err) {
			t.Fatalf("expected live task to be pruned, stat err=%v", err)
		}
		if active := readActiveTask(cwd); active.Path != "" {
			t.Fatalf("expected active task to be cleared, got %+v", active)
		}
		if _, err := os.Stat(filepath.Join(archiveDir, "decisions", "2026-06-05T100500Z-keep-durable-decisions.md")); err != nil {
			t.Fatalf("expected decision to be copied, err=%v", err)
		}
		if _, err := os.Stat(filepath.Join(archiveDir, "findings", filepath.Base(openFinding.Path))); err != nil {
			t.Fatalf("expected open finding to be preserved, err=%v", err)
		}
		if _, err := os.Stat(filepath.Join(archiveDir, "findings", filepath.Base(verifiedFinding.Path))); !os.IsNotExist(err) {
			t.Fatalf("expected verified finding not to be copied, err=%v", err)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("Run archive code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "archived task: Archive Me") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestArchiveRequiresActiveTask(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run([]string{"archive"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatalf("expected archive without active task to fail")
	}
	if !strings.Contains(errOut.String(), "no active task") {
		t.Fatalf("expected active task error, got %q", errOut.String())
	}
}

func TestRunRoutesOpenFindingsToImplementer(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		if _, err := openTask(e, []string{"Loop Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := addFinding(e, []string{"open issue"}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"run"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run run code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		"next role: implementer",
		"next action: fix open findings",
		"findings: open=1 fixed=0 verified=0",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("run output missing %q, got:\n%s", want, got)
		}
	}
}

func TestReviewerFindingRoutesNextRoleToImplementer(t *testing.T) {
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Review Finds Issue"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := addHandoff(e, []string{"--to", "reviewer", "Please review the implementation", "--by", "implementer"}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := addFinding(e, []string{"smoke command shadows a read-only shell variable", "--severity", "medium", "--by", "reviewer"}, time.Date(2026, 6, 5, 10, 2, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		state, err := analyzeLoopState(e)
		if err != nil {
			t.Fatal(err)
		}
		if state.NextRole != "implementer" || state.NextAction != "fix open findings" {
			t.Fatalf("reviewer finding should route back to implementer, got %+v", state)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestRunJSONRoutesOpenFindingsToImplementer(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Loop JSON Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := addFinding(e, []string{"open issue"}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"run", "--json"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run run --json code=%d stderr=%s", code, errOut.String())
	}
	var got loopJSON
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("run JSON should decode: %v\n%s", err, out.String())
	}
	if got.Task == nil || got.NextRole != "implementer" || got.NextAction != "fix open findings" || got.Task.Findings.Open != 1 {
		t.Fatalf("unexpected run JSON: %+v", got)
	}
}

func TestRunJSONRejectsUnknownFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run([]string{"run", "--later"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatal("expected run with unknown flag to fail")
	}
	if !strings.Contains(errOut.String(), "unknown flag --later") {
		t.Fatalf("expected unknown flag error, got %q", errOut.String())
	}
}

func TestRunRoutesFixedFindingsToReviewer(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		if _, err := openTask(e, []string{"Loop Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		finding, err := addFinding(e, []string{"fixed issue"}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := updateFindingStatus(e, []string{finding.Path}, "fixed", "Fixed", time.Date(2026, 6, 5, 10, 2, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"run"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run run code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "next role: reviewer") || !strings.Contains(out.String(), "verify fixed findings independently") {
		t.Fatalf("unexpected run output:\n%s", out.String())
	}
}

func TestRunRoutesHandoffToImplementerToImplementer(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		if _, err := openTask(e, []string{"Loop Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := addHandoff(e, []string{"--to", "implementer", "Please implement this task"}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"run"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run run code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		"next role: implementer",
		"next action: act on latest handoff",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("run output missing %q, got:\n%s", want, got)
		}
	}
}

func TestRunRoutesHandoffToCoordinatorToCoordinator(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		if _, err := openTask(e, []string{"Planning Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := addHandoff(e, []string{"--to", "coordinator", "Help me turn this into a clear plan"}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"run"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run run code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		"next role: coordinator",
		"next action: continue intake and planning",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("run output missing %q, got:\n%s", want, got)
		}
	}
}

func TestRunRoutesHandoffToReviewerToReviewer(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		if _, err := openTask(e, []string{"Loop Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := addHandoff(e, []string{"--to", "reviewer", "Please review this task"}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"run"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run run code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "next role: reviewer") || !strings.Contains(out.String(), "review latest handoff") {
		t.Fatalf("unexpected run output:\n%s", out.String())
	}
}

func TestGoContinueUsesActiveTaskAndCurrentNextRole(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		task, err := openTask(e, []string{"Tiny Habits CLI"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := addHandoff(e, []string{"--to", "reviewer", "Ready for review."}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		verificationPath := filepath.Join(".agents", task.Path, "verification", "2099-01-01T000000Z-review.md")
		reviewer := filepath.Join(cwd, "reviewer.sh")
		reviewerBody := strings.Join([]string{
			"#!/bin/sh",
			"set -eu",
			"mkdir -p " + shellQuote(filepath.Dir(verificationPath)),
			"cat > " + shellQuote(verificationPath) + " <<'EOF'",
			"# Verification: review",
			"",
			"Result: pass",
			"By: reviewer",
			"",
			"Review passed.",
			"EOF",
			"",
		}, "\n")
		if err := os.WriteFile(reviewer, []byte(reviewerBody), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := "roles:\n  implementer: agent\n  reviewer: " + reviewer + " {prompt_file}\n  coordinator: codex\n"
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		code := Run([]string{"go", "--continue", "--plain", "--max-iterations", "2", "Review the implemented habit tracker."}, bytes.NewBuffer(nil), &out, &errOut)
		entries, err := os.ReadDir(filepath.Join(cwd, ".agents", "tasks"))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("go --continue should not open a new task, got %d tasks", len(entries))
		}
		return code
	})
	if code != 0 {
		t.Fatalf("Run go --continue code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	got := out.String()
	for _, want := range []string{
		"task: Tiny Habits CLI",
		"running reviewer...",
		"human approval required",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("go --continue output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "task: Review the implemented habit tracker.") {
		t.Fatalf("go --continue opened a task from continuation text:\n%s", got)
	}
}

func TestGoReviewerFlagUsesActiveTaskAndRoutesToReviewer(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		task, err := openTask(e, []string{"Tiny Habits CLI"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := addHandoff(e, []string{"--to", "implementer", "Build the tiny habits CLI."}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		verificationPath := filepath.Join(".agents", task.Path, "verification", "2099-01-01T000000Z-review.md")
		reviewer := filepath.Join(cwd, "reviewer.sh")
		reviewerBody := strings.Join([]string{
			"#!/bin/sh",
			"set -eu",
			"mkdir -p " + shellQuote(filepath.Dir(verificationPath)),
			"cat > " + shellQuote(verificationPath) + " <<'EOF'",
			"# Verification: review",
			"",
			"Result: pass",
			"By: reviewer",
			"",
			"Review passed.",
			"EOF",
			"",
		}, "\n")
		if err := os.WriteFile(reviewer, []byte(reviewerBody), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := "roles:\n  implementer: agent\n  reviewer: " + reviewer + " {prompt_file}\n  coordinator: codex\n"
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		code := Run([]string{"go", "--reviewer", "--plain", "--max-iterations", "2", "Review the latest Tiny Habits changes."}, bytes.NewBuffer(nil), &out, &errOut)
		entries, err := os.ReadDir(filepath.Join(cwd, ".agents", "tasks"))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("go --reviewer should not open a new task, got %d tasks", len(entries))
		}
		return code
	})
	if code != 0 {
		t.Fatalf("Run go --reviewer code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	got := out.String()
	for _, want := range []string{
		"task: Tiny Habits CLI",
		"running reviewer...",
		"human approval required",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("go --reviewer output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "running implementer...") {
		t.Fatalf("go --reviewer should route directly to reviewer:\n%s", got)
	}
}

func TestGoReviewerFlagRunsReviewerEvenWithOpenFindings(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		task, err := openTask(e, []string{"Tiny Habits CLI"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := addFinding(e, []string{"existing review issue", "--severity", "medium", "--by", "reviewer"}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		reviewer := filepath.Join(cwd, "reviewer.sh")
		reviewerBody := strings.Join([]string{
			"#!/bin/sh",
			"set -eu",
			"task_dir=" + shellQuote(filepath.Join(".agents", task.Path)),
			"mkdir -p \"$task_dir/handoffs\"",
			"cat > \"$task_dir/handoffs/2099-01-01T000000Z-handoff-to-implementer.md\" <<'EOF'",
			"# Handoff: implementer",
			"",
			"To: implementer",
			"From: reviewer",
			"",
			"Open finding still needs implementation.",
			"EOF",
			"",
		}, "\n")
		if err := os.WriteFile(reviewer, []byte(reviewerBody), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := "roles:\n  implementer: agent\n  reviewer: " + reviewer + " {prompt_file}\n  coordinator: codex\n"
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"go", "--reviewer", "--plain", "--max-iterations", "1", "Review the latest Tiny Habits changes."}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 1 {
		t.Fatalf("expected max-iterations exit after forced reviewer, code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	got := out.String()
	if !strings.Contains(got, "running reviewer...") {
		t.Fatalf("go --reviewer should force reviewer before open-finding routing:\n%s", got)
	}
	if strings.Contains(got, "running implementer...") {
		t.Fatalf("go --reviewer --max-iterations 1 should not start implementer:\n%s", got)
	}
	if !strings.Contains(got, "next role: implementer") {
		t.Fatalf("after reviewer, open finding should route to implementer:\n%s", got)
	}
}

func TestRunStopsAtHumanBoundaryAfterVerification(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		task, err := openTask(e, []string{"Loop Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		verificationDir := filepath.Join(cwd, ".agents", task.Path, "verification")
		if err := os.MkdirAll(verificationDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(verificationDir, "2026-06-05T100100Z-go-test.md"), []byte("# Verification\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"run"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run run code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		"next role: human",
		"next action: approve archive, commit, or next task",
		"boundary: human approval required",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("run output missing %q, got:\n%s", want, got)
		}
	}
}

func TestRunExecutesConfiguredImplementer(t *testing.T) {
	var out, errOut bytes.Buffer
	var promptCopy string
	var runCwd string
	code := withTempProcessEnv(t, func(home, cwd string) int {
		runCwd = cwd
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		if _, err := openTask(e, []string{"Executable Loop"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		script := filepath.Join(cwd, "implementer.sh")
		body := "#!/bin/sh\nset -eu\ncp \"$1\" prompt-copy.txt\nprintf 'implemented from %s\\n' \"$1\"\nprintf 'checked stderr too\\n' >&2\n"
		if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := "roles:\n  implementer: " + script + " {prompt_file}\n  reviewer: codex\n  coordinator: codex\n"
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		code := Run([]string{"run", "--once"}, bytes.NewBuffer(nil), &out, &errOut)
		if data, err := os.ReadFile(filepath.Join(cwd, "prompt-copy.txt")); err == nil {
			promptCopy = string(data)
		}
		return code
	})
	if code != 0 {
		t.Fatalf("Run run code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		"run id: ",
		"run log: .agents/runs/logs/",
		"implemented from",
		"checked stderr too",
		"ran role: implementer",
		"prompt: .agents/runs/prompts/",
		"log: .agents/runs/logs/",
		"agent output:",
		"after:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("run output missing %q, got:\n%s", want, got)
		}
	}
	if !strings.Contains(promptCopy, "You are the implementer") {
		t.Fatalf("prompt copy should contain implementer prompt")
	}
	logs, err := filepath.Glob(filepath.Join(runCwd, ".agents", "runs", "logs", "*-implementer.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected one implementer log, got %d: %v", len(logs), logs)
	}
	logData, err := os.ReadFile(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logData)
	for _, want := range []string{
		"# Agents run ",
		"role: implementer",
		"agent: ",
		"model: provider default",
		"reasoning: provider default",
		"--- provider output ---",
		"implemented from",
		"checked stderr too",
		"[agents] completed at ",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("run log missing %q:\n%s", want, logText)
		}
	}
	if !strings.Contains(logText, "implemented from") || !strings.Contains(logText, "checked stderr too") {
		t.Fatalf("run log missing streamed output:\n%s", logText)
	}
}

func TestRunStopsImplementerAfterReviewerHandoff(t *testing.T) {
	var out, errOut bytes.Buffer
	var runCwd string
	code := withTempProcessEnv(t, func(home, cwd string) int {
		runCwd = cwd
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		task, err := openTask(e, []string{"Completion Handoff"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		script := filepath.Join(cwd, "implementer.sh")
		body := strings.Join([]string{
			"#!/bin/sh",
			"set -eu",
			"trap 'exit 0' INT TERM",
			"mkdir -p " + shellQuote(filepath.Join(".agents", task.Path, "handoffs")),
			"cat > " + shellQuote(filepath.Join(".agents", task.Path, "handoffs", "2026-06-05T100100Z-handoff-to-reviewer.md")) + " <<'EOF'",
			"# Handoff: reviewer",
			"",
			"To: reviewer",
			"",
			"Ready for review.",
			"EOF",
			"while :; do sleep 1; done",
			"",
		}, "\n")
		if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := "roles:\n  implementer: " + script + " {prompt_file}\n  reviewer: codex\n  coordinator: codex\n"
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"run", "--once"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run run code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	if !strings.Contains(out.String(), "ran role: implementer") {
		t.Fatalf("expected implementer run to finish, got:\n%s", out.String())
	}
	logs, err := filepath.Glob(filepath.Join(runCwd, ".agents", "runs", "logs", "*-implementer.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected one implementer log, got %d: %v", len(logs), logs)
	}
	data, err := os.ReadFile(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[agents] detected completion handoff to reviewer") {
		t.Fatalf("expected completion handoff marker in log:\n%s", string(data))
	}
}

func TestReadConfigParsesModelSettings(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgText := strings.Join([]string{
		"settings:",
		"  codex_model: gpt-5.5",
		"  codex_reasoning: low",
		"  claude_model: opus",
		"  claude_effort: high",
		"  cursor_model: claude-4.6-sonnet-medium-thinking",
		"  local_ai_enabled: true",
		"  local_ai_review_first_pass: true",
		"  local_ai_provider: lmstudio",
		"  local_ai_base_url: http://127.0.0.1:1234/v1",
		"  local_ai_model: google/gemma-3-4b",
		"  local_ai_timeout_ms: 700",
		"roles:",
		"  implementer: agent",
		"  reviewer: codex",
		"  coordinator: codex",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, ".agents", "config.yml"), []byte(cfgText), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := readConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Settings.CodexModel != "gpt-5.5" || cfg.Settings.CodexReasoning != "low" {
		t.Fatalf("unexpected codex settings: %+v", cfg.Settings)
	}
	if cfg.Settings.ClaudeModel != "opus" || cfg.Settings.ClaudeEffort != "high" || cfg.Settings.CursorModel != "claude-4.6-sonnet-medium-thinking" {
		t.Fatalf("unexpected settings: %+v", cfg.Settings)
	}
	if !cfg.Settings.LocalAIEnabled || !cfg.Settings.LocalAIReviewFirstPass || cfg.Settings.LocalAIProvider != "lmstudio" ||
		cfg.Settings.LocalAIBaseURL != "http://127.0.0.1:1234/v1" ||
		cfg.Settings.LocalAIModel != "google/gemma-3-4b" ||
		cfg.Settings.LocalAITimeout != 700 {
		t.Fatalf("unexpected local AI settings: %+v", cfg.Settings)
	}
	if cfg.Roles["coordinator"].Provider != "codex" {
		t.Fatalf("expected coordinator role to parse, got %+v", cfg.Roles)
	}
}

func TestLocalAISettingsDoNotReplaceCoordinatorRole(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgText := strings.Join([]string{
		"settings:",
		"  local_ai_enabled: true",
		"  local_ai_provider: lmstudio",
		"  local_ai_base_url: http://127.0.0.1:1234/v1",
		"  local_ai_model: google/gemma-3-4b",
		"roles:",
		"  coordinator: codex",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, ".agents", "config.yml"), []byte(cfgText), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := readConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	command, agent, _, err := resolveRoleCommand(cfg.Roles["coordinator"], cfg.Settings)
	if err != nil {
		t.Fatal(err)
	}
	if agent != "codex" || !strings.Contains(command, "codex exec") {
		t.Fatalf("local AI settings must not replace coordinator role, agent=%q command=%q", agent, command)
	}
}

func TestReadConfigParsesProviderMapAndReviewFirstPass(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgText := strings.Join([]string{
		"providers:",
		"  lmstudio:",
		"    type: openai_compatible",
		"    base_url: http://127.0.0.1:1234/v1",
		"    model: google/gemma-3-4b",
		"    timeout_ms: 900",
		"  reviewer-codex:",
		"    provider: codex",
		"    model: gpt-5.5",
		"    reasoning: high",
		"roles:",
		"  coordinator: lmstudio",
		"  reviewer: reviewer-codex",
		"review:",
		"  first_pass: lmstudio",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, ".agents", "config.yml"), []byte(cfgText), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := readConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Review.FirstPass != "lmstudio" {
		t.Fatalf("unexpected review config: %+v", cfg.Review)
	}
	if got := cfg.Providers["lmstudio"]; got.Type != "openai_compatible" || got.BaseURL != "http://127.0.0.1:1234/v1" || got.Model != "google/gemma-3-4b" || got.Timeout != 900 {
		t.Fatalf("unexpected lmstudio provider: %+v", got)
	}
	reviewer, settings := resolvedRole(cfg, cfg.Roles["reviewer"])
	if reviewer.Provider != "codex" {
		t.Fatalf("reviewer provider did not resolve through provider map: %+v", reviewer)
	}
	if settings.CodexModel != "gpt-5.5" || settings.CodexReasoning != "high" {
		t.Fatalf("unexpected resolved reviewer settings: %+v", settings)
	}
	firstPass, ok := firstPassReviewSettings(cfg)
	if !ok || firstPass.LocalAIProvider != "lmstudio" || firstPass.LocalAIActor != "lmstudio" ||
		firstPass.LocalAIBaseURL != "http://127.0.0.1:1234/v1" || firstPass.LocalAIModel != "google/gemma-3-4b" {
		t.Fatalf("unexpected first-pass settings ok=%v settings=%+v", ok, firstPass)
	}
}

func TestReviewerPromptDoesNotIncludeLocalAIReviewNotes(t *testing.T) {
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Review Notes"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := addHandoff(e, []string{"--to", "reviewer", "Review the changed CLI behavior."}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		cfgText := strings.Join([]string{
			"settings:",
			"  local_ai_enabled: true",
			"  local_ai_base_url: http://127.0.0.1:1234/v1",
			"  local_ai_model: google/gemma-3-4b",
			"roles:",
			"  reviewer: codex",
			"",
		}, "\n")
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfgText), 0o644); err != nil {
			t.Fatal(err)
		}
		prompt, err := buildRolePrompt(e, loopState{
			Task:       task,
			NextRole:   "reviewer",
			NextAction: "review latest handoff",
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Review the changed CLI behavior.", "Working agreement:"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("reviewer prompt missing %q:\n%s", want, prompt)
			}
		}
		for _, forbidden := range []string{
			"## Local AI pre-review notes",
			"advisory, verify independently",
		} {
			if strings.Contains(prompt, forbidden) {
				t.Fatalf("reviewer prompt should not include local AI notes %q:\n%s", forbidden, prompt)
			}
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestLocalAIFirstPassFindingsSkipConfiguredReviewer(t *testing.T) {
	old := runFirstPassReviewWithLocalAI
	defer func() { runFirstPassReviewWithLocalAI = old }()
	runFirstPassReviewWithLocalAI = func(settings agentSettings, input string) (localAIReviewResult, error) {
		if !strings.Contains(input, "Ready for review") {
			t.Fatalf("review input missing handoff:\n%s", input)
		}
		return localAIReviewResult{
			Result:  "fail",
			Summary: "local reviewer found a concrete issue",
			Findings: []localAIReviewFinding{{
				Severity: "high",
				Text:     "reviewer marker should not be written before fixing the bug",
				Evidence: "latest handoff says ready but diff is incomplete",
			}},
		}, nil
	}
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Local Review"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := addHandoff(e, []string{"--to", "reviewer", "Ready for review."}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(cwd, "remote-reviewer-ran")
		reviewer := filepath.Join(cwd, "reviewer.sh")
		if err := os.WriteFile(reviewer, []byte("#!/bin/sh\ntouch "+shellQuote(marker)+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := strings.Join([]string{
			"providers:",
			"  lmstudio:",
			"    base_url: http://127.0.0.1:1234/v1",
			"    model: google/gemma-3-4b",
			"    timeout_ms: 700",
			"review:",
			"  first_pass: lmstudio",
			"settings:",
			"  local_ai_enabled: true",
			"  local_ai_review_first_pass: true",
			"  local_ai_provider: legacy",
			"  local_ai_base_url: http://127.0.0.1:9999/v1",
			"  local_ai_model: legacy-model",
			"roles:",
			"  reviewer: " + reviewer + " {prompt_file}",
			"",
		}, "\n")
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		state, err := analyzeLoopState(e)
		if err != nil {
			t.Fatal(err)
		}
		var live bytes.Buffer
		result, err := runConfiguredRole(e, state, &live)
		if err != nil {
			t.Fatal(err)
		}
		if !result.AgentSkipped || result.Agent != "lmstudio" || result.After.NextRole != "implementer" || result.After.Stats.FindingsOpen != 1 {
			t.Fatalf("unexpected local first-pass result: %+v", result)
		}
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("configured reviewer should not run, stat err=%v", err)
		}
		if !strings.Contains(live.String(), "Local AI first-pass found 1 finding") {
			t.Fatalf("missing local first-pass output:\n%s", live.String())
		}
		findings, err := filepath.Glob(filepath.Join(cwd, ".agents", "tasks", "*", "findings", "*.md"))
		if err != nil || len(findings) != 1 {
			t.Fatalf("expected one finding, got %v err=%v", findings, err)
		}
		data, err := os.ReadFile(findings[0])
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Created by: lmstudio", "Agent: lmstudio (model google/gemma-3-4b)", "Severity: high", "Evidence:"} {
			if !strings.Contains(string(data), want) {
				t.Fatalf("finding missing %q:\n%s", want, string(data))
			}
		}
		if strings.Contains(string(data), "legacy-model") {
			t.Fatalf("new review.first_pass provider should override legacy settings:\n%s", string(data))
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestLegacyLocalAIFirstPassFindingsSkipConfiguredReviewer(t *testing.T) {
	old := runFirstPassReviewWithLocalAI
	defer func() { runFirstPassReviewWithLocalAI = old }()
	runFirstPassReviewWithLocalAI = func(settings agentSettings, input string) (localAIReviewResult, error) {
		return localAIReviewResult{
			Result: "fail",
			Findings: []localAIReviewFinding{{
				Text: "legacy local first-pass issue",
			}},
		}, nil
	}
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Legacy Local Review"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := addHandoff(e, []string{"--to", "reviewer", "Ready for review."}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(cwd, "remote-reviewer-ran")
		reviewer := filepath.Join(cwd, "reviewer.sh")
		if err := os.WriteFile(reviewer, []byte("#!/bin/sh\ntouch "+shellQuote(marker)+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := strings.Join([]string{
			"settings:",
			"  local_ai_enabled: true",
			"  local_ai_review_first_pass: true",
			"  local_ai_provider: lmstudio",
			"  local_ai_base_url: http://127.0.0.1:1234/v1",
			"  local_ai_model: google/gemma-3-4b",
			"roles:",
			"  reviewer: " + reviewer + " {prompt_file}",
			"",
		}, "\n")
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		state, err := analyzeLoopState(e)
		if err != nil {
			t.Fatal(err)
		}
		var live bytes.Buffer
		result, err := runConfiguredRole(e, state, &live)
		if err != nil {
			t.Fatal(err)
		}
		if !result.AgentSkipped || result.Agent != "local-ai" || result.After.NextRole != "implementer" || result.After.Stats.FindingsOpen != 1 {
			t.Fatalf("unexpected local first-pass result: %+v", result)
		}
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("configured reviewer should not run, stat err=%v", err)
		}
		if !strings.Contains(live.String(), "Local AI first-pass found 1 finding") {
			t.Fatalf("missing local first-pass output:\n%s", live.String())
		}
		findings, err := filepath.Glob(filepath.Join(cwd, ".agents", "tasks", "*", "findings", "*.md"))
		if err != nil || len(findings) != 1 {
			t.Fatalf("expected one finding, got %v err=%v", findings, err)
		}
		data, err := os.ReadFile(findings[0])
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Created by: local-ai", "Agent: lmstudio (model google/gemma-3-4b)", "Severity: medium"} {
			if !strings.Contains(string(data), want) {
				t.Fatalf("finding missing %q:\n%s", want, string(data))
			}
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestLocalAIFirstPassPassContinuesToConfiguredReviewer(t *testing.T) {
	old := runFirstPassReviewWithLocalAI
	defer func() { runFirstPassReviewWithLocalAI = old }()
	runFirstPassReviewWithLocalAI = func(settings agentSettings, input string) (localAIReviewResult, error) {
		return localAIReviewResult{Result: "pass", Summary: "clean"}, nil
	}
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Local Review"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := addHandoff(e, []string{"--to", "reviewer", "Ready for review."}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(cwd, "remote-reviewer-ran")
		reviewer := filepath.Join(cwd, "reviewer.sh")
		if err := os.WriteFile(reviewer, []byte("#!/bin/sh\ntouch "+shellQuote(marker)+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := strings.Join([]string{
			"settings:",
			"  local_ai_enabled: true",
			"  local_ai_review_first_pass: true",
			"  local_ai_base_url: http://127.0.0.1:1234/v1",
			"  local_ai_model: google/gemma-3-4b",
			"roles:",
			"  reviewer: " + reviewer + " {prompt_file}",
			"",
		}, "\n")
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		state, err := analyzeLoopState(e)
		if err != nil {
			t.Fatal(err)
		}
		var live bytes.Buffer
		result, err := runConfiguredRole(e, state, &live)
		if err != nil {
			t.Fatal(err)
		}
		if result.Agent == "local-ai" || result.AgentSkipped {
			t.Fatalf("configured reviewer should run after clean local pass: %+v", result)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("configured reviewer did not run: %v", err)
		}
		if !strings.Contains(live.String(), "continuing to configured reviewer") {
			t.Fatalf("missing local pass output:\n%s", live.String())
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestLocalAIFirstPassMalformedResultContinuesToConfiguredReviewer(t *testing.T) {
	old := runFirstPassReviewWithLocalAI
	defer func() { runFirstPassReviewWithLocalAI = old }()
	runFirstPassReviewWithLocalAI = func(settings agentSettings, input string) (localAIReviewResult, error) {
		return localAIReviewResult{Result: "maybe"}, nil
	}
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Local Review"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := addHandoff(e, []string{"--to", "reviewer", "Ready for review."}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(cwd, "remote-reviewer-ran")
		reviewer := filepath.Join(cwd, "reviewer.sh")
		if err := os.WriteFile(reviewer, []byte("#!/bin/sh\ntouch "+shellQuote(marker)+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := "settings:\n  local_ai_enabled: true\n  local_ai_review_first_pass: true\n  local_ai_base_url: http://127.0.0.1:1234/v1\n  local_ai_model: google/gemma-3-4b\nroles:\n  reviewer: " + reviewer + " {prompt_file}\n"
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		state, err := analyzeLoopState(e)
		if err != nil {
			t.Fatal(err)
		}
		var live bytes.Buffer
		if _, err := runConfiguredRole(e, state, &live); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("configured reviewer did not run after malformed local result: %v", err)
		}
		if !strings.Contains(live.String(), "Local AI first-pass unavailable") {
			t.Fatalf("missing local unavailable output:\n%s", live.String())
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestResolveCodexCommandUsesModelSettings(t *testing.T) {
	command, err := resolveAgentCommand("codex", agentSettings{CodexModel: "gpt-5.5", CodexReasoning: "low"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"codex exec",
		"--sandbox workspace-write",
		"--skip-git-repo-check",
		`-c 'sandbox_workspace_write.writable_roots=[".agents"]'`,
		"-m 'gpt-5.5'",
		"-c 'model_reasoning_effort=\"low\"'",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("codex command missing %q:\n%s", want, command)
		}
	}
}

func TestResolveAgentCommandUsesProviderModelSettings(t *testing.T) {
	claudeCommand, err := resolveAgentCommand("claude", agentSettings{ClaudeModel: "opus", ClaudeEffort: "xhigh"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"claude -p", "--permission-mode acceptEdits", "--model 'opus'", "--effort 'xhigh'"} {
		if !strings.Contains(claudeCommand, want) {
			t.Fatalf("claude command missing %q:\n%s", want, claudeCommand)
		}
	}

	cursorCommand, err := resolveAgentCommand("agent", agentSettings{CursorModel: "claude-4.6-sonnet-medium-thinking"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"agent -p", "--output-format stream-json", "--stream-partial-output", "--model 'claude-4.6-sonnet-medium-thinking'", "--force"} {
		if !strings.Contains(cursorCommand, want) {
			t.Fatalf("cursor command missing %q:\n%s", want, cursorCommand)
		}
	}
}

func TestCursorStreamWriterDecodesDeltasResultAndUsage(t *testing.T) {
	var out, log, live strings.Builder
	writer := &cursorStreamWriter{output: &out, log: &log, live: &live}
	chunks := []string{
		`{"type":"system","subtype":"init"}` + "\n",
		`{"type":"thinking","subtype":"delta","text":"hidden thought"}` + "\n",
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Hel"}]},"timestamp_ms":1}` + "\n",
		`{"type":"assistant","message":{"content":[{"type":"text","text":"lo"}]},"timestamp_ms":2}` + "\n",
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello"}]}}` + "\n",
		`{"type":"result","subtype":"success","result":"Hello","usage":{"inputTokens":12,"outputTokens":3,"cacheReadTokens":4}}` + "\n",
	}
	for _, chunk := range chunks {
		if _, err := writer.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if got := writer.FinalOutput(); got != "Hello\nToken usage: input=12 output=3 cache_read=4" {
		t.Fatalf("FinalOutput got %q", got)
	}
	if got := live.String(); got != "Hello\n" {
		t.Fatalf("live stream got %q", got)
	}
	if !strings.Contains(log.String(), `"type":"system"`) || !strings.Contains(log.String(), `"type":"result"`) {
		t.Fatalf("raw JSON should be preserved in log, got:\n%s", log.String())
	}
	if strings.Contains(live.String(), "hidden thought") || strings.Contains(live.String(), "Cursor:") {
		t.Fatalf("live stream should hide thinking and generic event labels, got:\n%s", live.String())
	}
}

func TestCursorStreamWriterRendersProgressAndToolSummaries(t *testing.T) {
	var out, log, live strings.Builder
	writer := &cursorStreamWriter{output: &out, log: &log, live: &live}
	chunks := []string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"` + "```agents-progress\\n{\\\"status\\\":\\\"editing files\\\",\\\"detail\\\":\\\"adding tests\\\"}\\n```" + `"}]},"timestamp_ms":1}` + "\n",
		`{"type":"tool_call","subtype":"started","tool_call":{"shellToolCall":{"args":{"description":"Run tests","command":"python3 -m unittest"}}}}` + "\n",
		`{"type":"tool_call","subtype":"completed","tool_call":{"editToolCall":{"args":{"path":"/tmp/tiny_habits.py"}}}}` + "\n",
	}
	for _, chunk := range chunks {
		if _, err := writer.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	got := live.String()
	for _, want := range []string{
		"- Editing files: Adding tests",
		"Running: Run tests",
		"Edited: tiny_habits.py",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("live stream missing %q:\n%s", want, got)
		}
	}
}

func TestCursorStreamWriterFiltersSplitProgressBlocks(t *testing.T) {
	var out, log, live strings.Builder
	writer := &cursorStreamWriter{output: &out, log: &log, live: &live}
	chunks := []string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"` + "```agents-pro" + `"}]},"timestamp_ms":1}` + "\n",
		`{"type":"assistant","message":{"content":[{"type":"text","text":"` + "gress\\n{\\\"status\\\":\\\"gathering context\\\",\\\"detail\\\":\\\"reading task\\\"}\\n```\\n" + `"}]},"timestamp_ms":2}` + "\n",
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Visible update"}]},"timestamp_ms":3}` + "\n",
	}
	for _, chunk := range chunks {
		if _, err := writer.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	got := live.String()
	for _, want := range []string{
		"- Gathering context: Reading task",
		"Visible update",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("live stream missing %q:\n%s", want, got)
		}
	}
	for _, hidden := range []string{"```agents-progress", `"status":"gathering context"`} {
		if strings.Contains(got, hidden) {
			t.Fatalf("live stream leaked %q:\n%s", hidden, got)
		}
	}
}

func TestCursorStreamWriterFiltersInlineProgressBlocks(t *testing.T) {
	var out, log, live strings.Builder
	writer := &cursorStreamWriter{output: &out, log: &log, live: &live}
	text := "Plan. ```agents-progress\n{\"status\":\"updating docs\",\"detail\":\"documenting commands\"}\n``` Done."
	chunk := fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"text","text":%q}]},"timestamp_ms":1}`+"\n", text)
	if _, err := writer.Write([]byte(chunk)); err != nil {
		t.Fatal(err)
	}
	final := writer.FinalOutput()
	if strings.Contains(final, "agents-progress") || strings.Contains(final, `"status"`) {
		t.Fatalf("final output leaked progress directive:\n%s", final)
	}
	got := live.String()
	for _, want := range []string{"Plan. ", "- Updating docs: Documenting commands", " Done."} {
		if !strings.Contains(got, want) {
			t.Fatalf("live stream missing %q:\n%s", want, got)
		}
	}
}

func TestCleanInteractiveAgentOutputStripsProgressDirectives(t *testing.T) {
	raw := strings.Join([]string{
		"Implemented changes.",
		"```agents-progress",
		`{"status":"running checks","detail":"executing tests"}`,
		"```",
		"Checks passed.",
	}, "\n")
	got := cleanInteractiveAgentOutput("agent", raw)
	if strings.Contains(got, "agents-progress") || strings.Contains(got, `"status"`) {
		t.Fatalf("cleaned output leaked progress directive:\n%s", got)
	}
	for _, want := range []string{"Implemented changes.", "Checks passed."} {
		if !strings.Contains(got, want) {
			t.Fatalf("cleaned output missing %q:\n%s", want, got)
		}
	}
}

func TestParseProgressDirectiveFromTextFindsAndTrimsDirective(t *testing.T) {
	progress, ok := parseProgressDirectiveFromText("before\n```agents-progress\n {\"status\":\" checking \",\"detail\":\" tests \"}\n```\nafter")
	if !ok {
		t.Fatal("expected progress directive")
	}
	if progress.Status != "checking" || progress.Detail != "tests" {
		t.Fatalf("unexpected progress directive: %+v", progress)
	}
	if _, ok := parseProgressDirectiveFromText("no directive"); ok {
		t.Fatal("plain text should not parse as progress")
	}
}

func TestCursorStreamWriterFinalOutputStripsProgressFromResult(t *testing.T) {
	var out, log, live strings.Builder
	writer := &cursorStreamWriter{output: &out, log: &log, live: &live}
	result := strings.Join([]string{
		"Done.",
		"```agents-progress",
		`{"status":"summarizing","detail":"should not render"}`,
		"```",
		"Final words.",
	}, "\n")
	chunk := fmt.Sprintf(`{"type":"result","subtype":"success","result":%q}`+"\n", result)
	if _, err := writer.Write([]byte(chunk)); err != nil {
		t.Fatal(err)
	}
	got := writer.FinalOutput()
	if strings.Contains(got, "agents-progress") || strings.Contains(got, `"status"`) {
		t.Fatalf("final result leaked progress directive:\n%s", got)
	}
	if !strings.Contains(got, "Done.") || !strings.Contains(got, "Final words.") {
		t.Fatalf("final result missing user text:\n%s", got)
	}
}

func TestCursorStreamWriterFlushesIncompleteAssistantText(t *testing.T) {
	var out, live strings.Builder
	writer := &cursorStreamWriter{output: &out, live: &live}
	if _, err := writer.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"partial answer"}]},"timestamp_ms":1}` + "\n")); err != nil {
		t.Fatal(err)
	}
	if got := writer.FinalOutput(); got != "partial answer" {
		t.Fatalf("FinalOutput should flush pending assistant text, got %q", got)
	}
}

func TestCursorStreamEventToolSummaryVariants(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "started shell command fallback",
			raw:  `{"subtype":"started","tool_call":{"shellToolCall":{"args":{"command":"go test ./...\necho done"}}}}`,
			want: "Running: go test ./...",
		},
		{
			name: "started read",
			raw:  `{"subtype":"started","tool_call":{"readToolCall":{"args":{"path":"/tmp/app.go"}}}}`,
			want: "Reading: app.go",
		},
		{
			name: "started edit",
			raw:  `{"subtype":"started","tool_call":{"editToolCall":{"args":{"path":"/tmp/app.go"}}}}`,
			want: "Editing: app.go",
		},
		{
			name: "completed shell command",
			raw:  `{"subtype":"completed","tool_call":{"shellToolCall":{"args":{"description":"Run checks"}}}}`,
			want: "Completed: Run checks",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ev cursorStreamEvent
			if err := json.Unmarshal([]byte(tc.raw), &ev); err != nil {
				t.Fatal(err)
			}
			if got := ev.toolSummary(); got != tc.want {
				t.Fatalf("toolSummary got %q want %q", got, tc.want)
			}
		})
	}
}

func TestFirstShellLineAndShortDuration(t *testing.T) {
	if got := firstShellLine("  first\nsecond  "); got != "first" {
		t.Fatalf("firstShellLine got %q", got)
	}
	long := firstShellLine(strings.Repeat("x", 90))
	if len([]rune(long)) != 80 || !strings.HasSuffix(long, "…") {
		t.Fatalf("long shell line should truncate to 80 runes with ellipsis, got %q", long)
	}
	for input, want := range map[time.Duration]string{
		-1 * time.Second:                     "0s",
		3*time.Second + 400*time.Millisecond: "3s",
		65 * time.Second:                     "1m05s",
	} {
		if got := shortDuration(input); got != want {
			t.Fatalf("shortDuration(%s) got %q want %q", input, got, want)
		}
	}
}

func TestEnsureShellCommandAvailableValidatesLocalExecutables(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.sh")
	if err := ensureShellCommandAvailable(missing+" {prompt_file}", "reviewer"); err == nil || !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("missing local command error got %v", err)
	}
	nonExec := filepath.Join(dir, "non-executable.sh")
	if err := os.WriteFile(nonExec, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureShellCommandAvailable(nonExec+" {prompt_file}", "reviewer"); err == nil || !strings.Contains(err.Error(), "is not executable") {
		t.Fatalf("non-executable command error got %v", err)
	}
	execFile := filepath.Join(dir, "ok.sh")
	if err := os.WriteFile(execFile, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureShellCommandAvailable("AGENTS_TEST=1 "+execFile+" {prompt_file}", "reviewer"); err != nil {
		t.Fatalf("executable command should pass: %v", err)
	}
	if got, err := shellCommandExecutable("AGENTS_TEST=1 " + execFile + " {prompt_file}"); err != nil || got != execFile {
		t.Fatalf("shellCommandExecutable got %q err=%v", got, err)
	}
}

func TestResolveCodexCommandKeepsLegacyModelSettings(t *testing.T) {
	command, err := resolveAgentCommand("codex", agentSettings{Model: "gpt-5.4", Reasoning: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-m 'gpt-5.4'", "-c 'model_reasoning_effort=\"medium\"'"} {
		if !strings.Contains(command, want) {
			t.Fatalf("legacy codex command missing %q:\n%s", want, command)
		}
	}
}

func TestRunReportsMissingConfiguredAgent(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		if _, err := openTask(e, []string{"Missing Agent"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		cfg := "roles:\n  implementer: definitely-missing-agents-provider {prompt_file}\n  reviewer: codex\n  coordinator: codex\n"
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"run"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatalf("expected missing provider to fail")
	}
	if strings.Contains(out.String(), "run log:") {
		t.Fatalf("missing provider should fail before printing run log, got:\n%s", out.String())
	}
	got := errOut.String()
	for _, want := range []string{
		`implementer agent command "definitely-missing-agents-provider" was not found on PATH`,
		"edit .agents/config.yml or install it",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing provider error should contain %q, got:\n%s", want, got)
		}
	}
}

func TestRunRequiresActiveTask(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run([]string{"run"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatalf("expected run without active task to fail")
	}
	if !strings.Contains(errOut.String(), "no active task") {
		t.Fatalf("expected active task error, got %q", errOut.String())
	}
}

func TestDispatchReviewerWaitsForHandoff(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Dispatch Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"dispatch", "reviewer"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run dispatch code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		"role: reviewer",
		"action: wait for an implementer handoff before reviewing",
		"boundary: handoff required",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("dispatch output missing %q, got:\n%s", want, got)
		}
	}
}

func TestDispatchShowsConfiguredAgent(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Dispatch Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte("roles:\n  implementer: claude\n  reviewer: codex\n  coordinator: codex\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"dispatch", "implementer"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run dispatch code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "agent: claude") {
		t.Fatalf("expected configured agent, got:\n%s", out.String())
	}
}

func TestDispatchJSONShowsConfiguredAgent(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Dispatch JSON Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte("roles:\n  implementer: claude\n  reviewer: codex\n  coordinator: codex\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"dispatch", "implementer", "--json"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run dispatch --json code=%d stderr=%s", code, errOut.String())
	}
	var got dispatchJSON
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("dispatch JSON should decode: %v\n%s", err, out.String())
	}
	if got.Role != "implementer" || got.Agent != "claude (model provider default)" || !got.Configured || got.Task == nil {
		t.Fatalf("unexpected dispatch JSON: %+v", got)
	}
}

func TestDispatchJSONRejectsUnknownFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Dispatch Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"dispatch", "implementer", "--later"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatal("expected dispatch with unknown flag to fail")
	}
	if !strings.Contains(errOut.String(), "unknown flag --later") {
		t.Fatalf("expected unknown flag error, got %q", errOut.String())
	}
}

func TestDispatchShowsUnconfiguredAgent(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Dispatch Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"dispatch", "implementer"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("Run dispatch code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "agent: not configured") {
		t.Fatalf("expected unconfigured agent, got:\n%s", out.String())
	}
}

func TestDispatchRejectsUnknownRole(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Dispatch Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"dispatch", "pilot"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatalf("expected unknown role to fail")
	}
	if !strings.Contains(errOut.String(), "unknown role") {
		t.Fatalf("expected unknown role error, got %q", errOut.String())
	}
}

func TestFindingVerifyRejectsSameActorAsFixer(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Independent Verification"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		record, err := addFinding(e, []string{"unchecked error"}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("AGENTS_ACTOR", "implementer")
		if code := Run([]string{"finding", "fix", record.Path}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
			t.Fatalf("finding fix code=%d stderr=%s", code, errOut.String())
		}
		// Same actor (via env) cannot verify its own fix.
		if code := Run([]string{"finding", "verify", record.Path}, bytes.NewBuffer(nil), &out, &errOut); code == 0 {
			t.Fatalf("expected same-actor verification to fail")
		}
		if !strings.Contains(errOut.String(), "requires a different actor") {
			t.Fatalf("expected independence error, got %q", errOut.String())
		}
		errOut.Reset()
		// A different actor can.
		if code := Run([]string{"finding", "verify", record.Path, "--by", "reviewer"}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
			t.Fatalf("reviewer verification code=%d stderr=%s", code, errOut.String())
		}
		data, err := os.ReadFile(filepath.Join(cwd, ".agents", record.Path))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "Status: verified") {
			t.Fatalf("expected verified status:\n%s", string(data))
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestFindingVerifyRequiresFixedStatus(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Lifecycle"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		record, err := addFinding(e, []string{"open issue"}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		return Run([]string{"finding", "verify", record.Path, "--by", "reviewer"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatalf("expected verify of open finding to fail")
	}
	if !strings.Contains(errOut.String(), "must be marked fixed") {
		t.Fatalf("expected lifecycle error, got %q", errOut.String())
	}
}

func TestChunksRecordActorIdentity(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Attribution"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("AGENTS_ACTOR", "codex")
		if code := Run([]string{"decision", "add", "use actor identity"}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
			t.Fatalf("decision add code=%d stderr=%s", code, errOut.String())
		}
		if code := Run([]string{"handoff", "--to", "reviewer", "ready"}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
			t.Fatalf("handoff code=%d stderr=%s", code, errOut.String())
		}
		if code := Run([]string{"finding", "add", "an issue", "--by", "reviewer"}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
			t.Fatalf("finding add code=%d stderr=%s", code, errOut.String())
		}
		checks := []struct {
			dir, want string
		}{
			{"decisions", "By: codex"},
			{"handoffs", "From: codex"},
			{"findings", "Created by: reviewer"},
		}
		for _, check := range checks {
			dir := filepath.Join(cwd, ".agents", task.Path, check.dir)
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) == 0 {
				t.Fatalf("expected %s chunk, err=%v", check.dir, err)
			}
			data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), check.want) {
				t.Fatalf("%s chunk missing %q:\n%s", check.dir, check.want, string(data))
			}
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestVerifyCommandRecordsEvidence(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Evidence"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if code := Run([]string{"verify", "smoke checks pass", "--cmd", "echo all-good", "--by", "reviewer"}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
			t.Fatalf("verify code=%d stderr=%s", code, errOut.String())
		}
		dir := filepath.Join(cwd, ".agents", task.Path, "verification")
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("expected one verification chunk, err=%v", err)
		}
		data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		got := string(data)
		for _, want := range []string{
			"# Verification: smoke checks pass",
			"Result: pass",
			"By: reviewer",
			"$ echo all-good",
			"exit: 0",
			"all-good",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("verification chunk missing %q:\n%s", want, got)
			}
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
	if !strings.Contains(out.String(), "result: pass") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestVerifyCommandRecordsFailureAndExitsNonZero(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Failed Evidence"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		code := Run([]string{"verify", "tests fail", "--cmd", "false", "--by", "reviewer"}, bytes.NewBuffer(nil), &out, &errOut)
		if code != 1 {
			t.Fatalf("expected exit 1 for failing evidence, got %d stderr=%s", code, errOut.String())
		}
		stats, err := readTaskStats(cwd, task.Path)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Verification != 0 || stats.VerificationFail != 1 {
			t.Fatalf("expected failed evidence to be excluded from passes, got %+v", stats)
		}
		// Failed evidence must not satisfy the human approval boundary.
		state, err := analyzeLoopState(e)
		if err != nil {
			t.Fatal(err)
		}
		if state.NextRole == "human" {
			t.Fatalf("failed verification must not reach human boundary, got %+v", state)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestImplementerVerificationDoesNotReachHumanBoundary(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Self Verification"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if code := Run([]string{"verify", "I checked my own work", "--by", "implementer"}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
			t.Fatalf("verify code=%d stderr=%s", code, errOut.String())
		}
		state, err := analyzeLoopState(e)
		if err != nil {
			t.Fatal(err)
		}
		if state.NextRole == "human" {
			t.Fatalf("implementer-recorded evidence must not reach human boundary, got %+v", state)
		}
		// Independent evidence does.
		if code := Run([]string{"verify", "independent check", "--by", "reviewer"}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
			t.Fatalf("verify code=%d stderr=%s", code, errOut.String())
		}
		state, err = analyzeLoopState(e)
		if err != nil {
			t.Fatal(err)
		}
		if state.NextRole != "human" {
			t.Fatalf("expected human boundary after independent verification, got %+v", state)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestStaleVerificationDoesNotReachHumanBoundary(t *testing.T) {
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Stale Evidence"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := addVerification(e, []string{"earlier evidence", "--by", "reviewer"}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		// New work hands off after the evidence was recorded.
		if _, err := addHandoff(e, []string{"--to", "reviewer", "new changes to review", "--by", "implementer"}, time.Date(2026, 6, 5, 10, 2, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		state, err := analyzeLoopState(e)
		if err != nil {
			t.Fatal(err)
		}
		if state.NextRole != "reviewer" {
			t.Fatalf("stale evidence must route to reviewer, got %+v", state)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestVerifyCmdRequiresActiveTaskBeforeRunningCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		marker := filepath.Join(cwd, "evidence-ran")
		code := Run([]string{"verify", "no task yet", "--cmd", "touch " + marker, "--by", "reviewer"}, bytes.NewBuffer(nil), &out, &errOut)
		if code == 0 {
			t.Fatalf("expected failure without an active task, got success")
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("evidence command must not run without an active task (marker exists, err=%v)", err)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestVerificationCommandTimesOut(t *testing.T) {
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Timeout Evidence"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		prev := verificationCommandTimeout
		verificationCommandTimeout = 100 * time.Millisecond
		defer func() { verificationCommandTimeout = prev }()
		record, pass, err := addVerification(e, []string{"slow evidence", "--cmd", "sleep 5", "--by", "reviewer"}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if pass {
			t.Fatalf("timed-out evidence must be recorded as a failure")
		}
		data, err := os.ReadFile(filepath.Join(cwd, ".agents", record.Path))
		if err != nil {
			t.Fatal(err)
		}
		body := string(data)
		if !strings.Contains(body, "exit: 124") || !strings.Contains(body, "timed out") {
			t.Fatalf("expected timeout evidence in chunk, got:\n%s", body)
		}
		stats, err := readTaskStats(cwd, task.Path)
		if err != nil {
			t.Fatal(err)
		}
		if stats.VerificationFail != 1 {
			t.Fatalf("expected timeout to count as failed verification, got %+v", stats)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestVerificationCurrentComparesTimestampsOnly(t *testing.T) {
	// Same-second tie: slug text must not decide ordering. The verification
	// slug sorts before the handoff slug, yet the evidence is current.
	tie := taskStats{
		LatestVerificationName: "2026-06-05T100200Z-aaa-evidence",
		LatestHandoffName:      "2026-06-05T100200Z-zzz-handoff",
	}
	if !verificationCurrent(tie) {
		t.Fatalf("same-second verification must be treated as current regardless of slug order")
	}
	stale := taskStats{
		LatestVerificationName: "2026-06-05T100100Z-zzz-evidence",
		LatestHandoffName:      "2026-06-05T100200Z-aaa-handoff",
	}
	if verificationCurrent(stale) {
		t.Fatalf("verification recorded before the latest handoff must be stale")
	}
}

func TestStatusJSONIncludesFailedVerification(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"JSON Failures"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if code := Run([]string{"verify", "tests fail", "--cmd", "false", "--by", "reviewer"}, bytes.NewBuffer(nil), &out, &errOut); code != 1 {
			t.Fatalf("expected exit 1 for failing evidence, got %d stderr=%s", code, errOut.String())
		}
		out.Reset()
		if code := Run([]string{"status", "--json"}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
			t.Fatalf("status --json code=%d stderr=%s", code, errOut.String())
		}
		if !strings.Contains(out.String(), `"verification_failed": 1`) {
			t.Fatalf("expected verification_failed in JSON, got:\n%s", out.String())
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestRunRefusesUntrustedWorkspace(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		// Init the workspace but never trust it; a config commits a command
		// template exactly like a malicious cloned repo would.
		if code := Run([]string{"init"}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
			t.Fatalf("init code=%d stderr=%s", code, errOut.String())
		}
		marker := filepath.Join(cwd, "pwned")
		cfg := "roles:\n  implementer:\n    cmd: \"touch " + marker + " #{prompt_file}\"\n"
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Untrusted Run"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		code := Run([]string{"run", "--once"}, bytes.NewBuffer(nil), &out, &errOut)
		if code == 0 {
			t.Fatalf("run must refuse an untrusted workspace")
		}
		if !strings.Contains(errOut.String(), "not trusted") {
			t.Fatalf("expected trust refusal, got stderr:\n%s", errOut.String())
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("config command must not run in an untrusted workspace")
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestSlugifyCapsLength(t *testing.T) {
	long := strings.Repeat("x", 300)
	slug := slugify(long)
	if len(slug) > maxSlugBytes {
		t.Fatalf("slug exceeds %d bytes: %d", maxSlugBytes, len(slug))
	}
	multibyte := strings.Repeat("ü", 300)
	slug = slugify(multibyte)
	if len(slug) > maxSlugBytes {
		t.Fatalf("multibyte slug exceeds %d bytes: %d", maxSlugBytes, len(slug))
	}
	if slugify("short title") != "short-title" {
		t.Fatalf("short slugs must be unchanged")
	}
}

func TestTaskOpenAcceptsLongTitle(t *testing.T) {
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{strings.Repeat("very long title ", 30)}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("long titles must not fail task open: %v", err)
		}
		if _, err := os.Stat(filepath.Join(cwd, ".agents", task.Path, "task.md")); err != nil {
			t.Fatal(err)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestCommandsResolveWorkspaceRootFromSubdir(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Root Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := initWorkspace(e, "share"); err != nil {
			t.Fatal(err)
		}
		sub := filepath.Join(cwd, "sub", "dir")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(sub); err != nil {
			t.Fatal(err)
		}
		defer os.Chdir(cwd)
		if code := Run([]string{"status"}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
			t.Fatalf("status code=%d stderr=%s", code, errOut.String())
		}
		if !strings.Contains(out.String(), "Root Task") {
			t.Fatalf("status from subdir must resolve the workspace root, got:\n%s", out.String())
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestStatusWarnsOnDanglingActivePointer(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := openTask(e, []string{"Ghost Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "active"), []byte("tasks/gone-task\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if code := Run([]string{"status"}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
			t.Fatalf("status code=%d stderr=%s", code, errOut.String())
		}
		if !strings.Contains(out.String(), "warning: active task directory is missing") {
			t.Fatalf("expected dangling pointer warning, got:\n%s", out.String())
		}
		out.Reset()
		if code := Run([]string{"status", "--json"}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
			t.Fatalf("status --json code=%d stderr=%s", code, errOut.String())
		}
		if !strings.Contains(out.String(), `"missing": true`) {
			t.Fatalf("expected missing flag in JSON, got:\n%s", out.String())
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestReadConfigParsesPerRoleOverrides(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgText := strings.Join([]string{
		"roles:",
		"  implementer: agent",
		"  reviewer:",
		"    provider: codex",
		"    model: gpt-5.5",
		"    reasoning: xhigh",
		"  coordinator:",
		"    cmd: my-agent {prompt_file}",
		"settings:",
		"  codex_model: gpt-5.4",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, ".agents", "config.yml"), []byte(cfgText), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := readConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Roles["implementer"].Provider != "agent" {
		t.Fatalf("scalar role should parse as provider, got %+v", cfg.Roles["implementer"])
	}
	reviewer := cfg.Roles["reviewer"]
	if reviewer.Provider != "codex" || reviewer.Model != "gpt-5.5" || reviewer.Reasoning != "xhigh" {
		t.Fatalf("unexpected reviewer config: %+v", reviewer)
	}
	if cfg.Roles["coordinator"].Cmd != "my-agent {prompt_file}" {
		t.Fatalf("unexpected coordinator config: %+v", cfg.Roles["coordinator"])
	}
	command, agent, _, err := resolveRoleCommand(reviewer, cfg.Settings)
	if err != nil {
		t.Fatal(err)
	}
	if agent != "codex" {
		t.Fatalf("unexpected agent label %q", agent)
	}
	for _, want := range []string{"-m 'gpt-5.5'", `model_reasoning_effort=\"xhigh\"`} {
		if !strings.Contains(command, strings.ReplaceAll(want, `\"`, `"`)) {
			t.Fatalf("reviewer command missing %q:\n%s", want, command)
		}
	}
}

func TestRunLoopStopsWhenRoleRecordsNoProgress(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		if _, err := openTask(e, []string{"Stalled Loop"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		script := filepath.Join(cwd, "implementer.sh")
		if err := os.WriteFile(script, []byte("#!/bin/sh\necho did nothing\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := "roles:\n  implementer: " + script + " {prompt_file}\n  reviewer: codex\n  coordinator: codex\n"
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"run"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 1 {
		t.Fatalf("expected stalled loop to exit 1, got %d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	if !strings.Contains(out.String(), "did not record new task progress") {
		t.Fatalf("expected no-progress stop message, got:\n%s", out.String())
	}
}

func TestRunLoopChainsRolesToHumanBoundary(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		task, err := openTask(e, []string{"Full Loop"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		handoffPath := filepath.Join(".agents", task.Path, "handoffs", "2026-06-05T100100Z-handoff-to-reviewer.md")
		implementer := filepath.Join(cwd, "implementer.sh")
		implementerBody := strings.Join([]string{
			"#!/bin/sh",
			"set -eu",
			"trap 'exit 0' INT TERM",
			"mkdir -p " + shellQuote(filepath.Dir(handoffPath)),
			"cat > " + shellQuote(handoffPath) + " <<'EOF'",
			"# Handoff: reviewer",
			"",
			"To: reviewer",
			"From: implementer",
			"",
			"Ready for review.",
			"EOF",
			"",
		}, "\n")
		if err := os.WriteFile(implementer, []byte(implementerBody), 0o755); err != nil {
			t.Fatal(err)
		}
		verificationPath := filepath.Join(".agents", task.Path, "verification", "2026-06-05T100200Z-checks.md")
		reviewer := filepath.Join(cwd, "reviewer.sh")
		reviewerBody := strings.Join([]string{
			"#!/bin/sh",
			"set -eu",
			"mkdir -p " + shellQuote(filepath.Dir(verificationPath)),
			"cat > " + shellQuote(verificationPath) + " <<'EOF'",
			"# Verification: checks",
			"",
			"Result: pass",
			"By: reviewer",
			"",
			"All checks pass.",
			"EOF",
			"",
		}, "\n")
		if err := os.WriteFile(reviewer, []byte(reviewerBody), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := "roles:\n  implementer: " + implementer + " {prompt_file}\n  reviewer: " + reviewer + " {prompt_file}\n  coordinator: codex\n"
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"run"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("expected loop to reach human boundary, code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	got := out.String()
	for _, want := range []string{
		"running implementer...",
		"running reviewer...",
		"human approval required",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("loop output missing %q:\n%s", want, got)
		}
	}
}

func TestInteractiveLoopSimulatesAllRolesAndFiltersProgress(t *testing.T) {
	var out bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		if _, err := initWorkspace(e, "share"); err != nil {
			t.Fatal(err)
		}
		task, err := openTask(e, []string{"All Roles"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := addHandoff(e, []string{"--to", "coordinator", "Route this through every role."}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}

		coordinator := filepath.Join(cwd, "coordinator.sh")
		coordinatorBody := strings.Join([]string{
			"#!/bin/sh",
			"set -eu",
			"printf '%s\\n' '```agents-progress'",
			"printf '%s\\n' '{\"status\":\"routing work\",\"detail\":\"sending a ready task to implementer\"}'",
			"printf '%s\\n' '```'",
			"cat <<'EOF'",
			"Coordinator routed the task.",
			"",
			"```agents-handoff",
			"role: implementer",
			"ready: true",
			"text: Build the requested behavior and hand it to review.",
			"```",
			"EOF",
		}, "\n")
		if err := os.WriteFile(coordinator, []byte(coordinatorBody), 0o755); err != nil {
			t.Fatal(err)
		}

		implementer := filepath.Join(cwd, "implementer.sh")
		implementerBody := strings.Join([]string{
			"#!/bin/sh",
			"set -eu",
			"printf '%s\\n' '```agents-progress'",
			"printf '%s\\n' '{\"status\":\"editing files\",\"detail\":\"making the requested change\"}'",
			"printf '%s\\n' '```'",
			"cat <<'EOF'",
			"Implementation complete.",
			"",
			"```agents-handoff",
			"role: reviewer",
			"text: Review the implemented behavior and checks.",
			"```",
			"EOF",
		}, "\n")
		if err := os.WriteFile(implementer, []byte(implementerBody), 0o755); err != nil {
			t.Fatal(err)
		}

		reviewer := filepath.Join(cwd, "reviewer.sh")
		reviewerBody := strings.Join([]string{
			"#!/bin/sh",
			"set -eu",
			"printf '%s\\n' '```agents-progress'",
			"printf '%s\\n' '{\"status\":\"running checks\",\"detail\":\"simulating reviewer verification\"}'",
			"printf '%s\\n' '```'",
			"cat <<'EOF'",
			"Review complete.",
			"",
			"```agents-verify",
			"{\"text\":\"simulated reviewer checks passed\",\"cmd\":\"printf ok\"}",
			"```",
			"EOF",
		}, "\n")
		if err := os.WriteFile(reviewer, []byte(reviewerBody), 0o755); err != nil {
			t.Fatal(err)
		}

		cfg := strings.Join([]string{
			"roles:",
			"  coordinator: " + coordinator + " {prompt_file}",
			"  implementer: " + implementer + " {prompt_file}",
			"  reviewer: " + reviewer + " {prompt_file}",
			"",
		}, "\n")
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}

		stop, err := runManagedLoop(e, &out, 3, false, nil, nil, loopDisplayInteractive, "")
		if err != nil {
			t.Fatal(err)
		}
		if stop.Reason != "human" || stop.Runs != 3 {
			t.Fatalf("expected all roles to reach human boundary, got %+v", stop)
		}
		stats, err := readTaskStats(cwd, task.Path)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Handoffs != 3 || stats.Verification != 1 {
			t.Fatalf("unexpected task stats after simulated roles: %+v", stats)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
	got := ansiEscapes.ReplaceAllString(out.String(), "")
	for _, want := range []string{
		"Coordinator",
		"- Routing work: Sending a ready task to implementer",
		"Implementer",
		"- Editing files: Making the requested change",
		"Reviewer",
		"- Running checks: Simulating reviewer verification",
		"Coordinator routed the task.",
		"Implementation complete.",
		"Review complete.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("interactive loop output missing %q:\n%s", want, got)
		}
	}
	for _, leaked := range []string{"```agents-progress", `"status":"routing work"`, `"status":"editing files"`, `"status":"running checks"`} {
		if strings.Contains(got, leaked) {
			t.Fatalf("interactive loop leaked progress directive %q:\n%s", leaked, got)
		}
	}
}

func TestRunLoopJSONEventsStreamsLifecycle(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		task, err := openTask(e, []string{"Event Loop"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		handoffPath := filepath.Join(".agents", task.Path, "handoffs", "2026-06-05T100100Z-handoff-to-reviewer.md")
		implementer := filepath.Join(cwd, "implementer.sh")
		implementerBody := strings.Join([]string{
			"#!/bin/sh",
			"set -eu",
			"trap 'exit 0' INT TERM",
			"echo building",
			"mkdir -p " + shellQuote(filepath.Dir(handoffPath)),
			"cat > " + shellQuote(handoffPath) + " <<'EOF'",
			"# Handoff: reviewer",
			"",
			"To: reviewer",
			"From: implementer",
			"",
			"Ready for review.",
			"EOF",
			"",
		}, "\n")
		if err := os.WriteFile(implementer, []byte(implementerBody), 0o755); err != nil {
			t.Fatal(err)
		}
		verificationPath := filepath.Join(".agents", task.Path, "verification", "2026-06-05T100200Z-checks.md")
		reviewer := filepath.Join(cwd, "reviewer.sh")
		reviewerBody := strings.Join([]string{
			"#!/bin/sh",
			"set -eu",
			"mkdir -p " + shellQuote(filepath.Dir(verificationPath)),
			"cat > " + shellQuote(verificationPath) + " <<'EOF'",
			"# Verification: checks",
			"",
			"Result: pass",
			"By: reviewer",
			"",
			"All checks pass.",
			"EOF",
			"",
		}, "\n")
		if err := os.WriteFile(reviewer, []byte(reviewerBody), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := "roles:\n  implementer: " + implementer + " {prompt_file}\n  reviewer: " + reviewer + " {prompt_file}\n  coordinator: codex\n"
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"run", "--json-events"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 0 {
		t.Fatalf("expected event loop to reach human boundary, code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	var events []loopEvent
	sawOutputChunk := false
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var ev loopEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("non-JSON line in --json-events output: %q (%v)", line, err)
		}
		if ev.Time == "" {
			t.Fatalf("event missing time: %q", line)
		}
		if ev.Event == "output" {
			if ev.RunID == "" || ev.Role == "" || ev.Chunk == "" {
				t.Fatalf("output event missing run attribution: %q", line)
			}
			sawOutputChunk = sawOutputChunk || strings.Contains(ev.Chunk, "building")
			continue
		}
		events = append(events, ev)
	}
	if !sawOutputChunk {
		t.Fatalf("expected an output event carrying implementer stdout, got:\n%s", out.String())
	}
	wantSequence := []string{
		"state_changed", // implementer next
		"run_started",   // implementer
		"run_finished",
		"state_changed", // reviewer next
		"run_started",   // reviewer
		"run_finished",
		"state_changed", // human boundary
		"stopped",
	}
	if len(events) != len(wantSequence) {
		t.Fatalf("expected %d lifecycle events, got %d:\n%s", len(wantSequence), len(events), out.String())
	}
	for i, want := range wantSequence {
		if events[i].Event != want {
			t.Fatalf("event %d = %q, want %q:\n%s", i, events[i].Event, want, out.String())
		}
	}
	if events[1].Role != "implementer" || events[4].Role != "reviewer" {
		t.Fatalf("unexpected run roles: %q then %q", events[1].Role, events[4].Role)
	}
	if events[1].RunID == "" || events[1].RunID != events[2].RunID {
		t.Fatalf("run_started/run_finished must share a run id: %q vs %q", events[1].RunID, events[2].RunID)
	}
	last := events[len(events)-1]
	if last.Reason != "human" || last.Runs == nil || *last.Runs != 2 {
		t.Fatalf("expected stopped event with reason human after 2 runs, got %+v", last)
	}
	if last.State == nil || last.State.Boundary == "" {
		t.Fatalf("expected stopped event to carry boundary state, got %+v", last.State)
	}
}

func TestRunLoopJSONEventsClosesFailedRun(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		if _, err := openTask(e, []string{"Failing Run"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		implementer := filepath.Join(cwd, "implementer.sh")
		implementerBody := strings.Join([]string{
			"#!/bin/sh",
			"echo about to fail",
			"exit 1",
			"",
		}, "\n")
		if err := os.WriteFile(implementer, []byte(implementerBody), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := "roles:\n  implementer: " + implementer + " {prompt_file}\n  reviewer: " + implementer + " {prompt_file}\n  coordinator: codex\n"
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"run", "--json-events"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatalf("expected failing role command to exit non-zero, stdout=%s", out.String())
	}
	var started, finished *loopEvent
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var ev loopEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("non-JSON line in --json-events output: %q (%v)", line, err)
		}
		switch ev.Event {
		case "run_started":
			started = &ev
		case "run_finished":
			finished = &ev
		}
	}
	if started == nil || finished == nil {
		t.Fatalf("expected run_started and run_finished events for a failed run, got:\n%s", out.String())
	}
	if finished.RunID == "" || finished.RunID != started.RunID {
		t.Fatalf("failed run_finished must share the run id: %q vs %q", started.RunID, finished.RunID)
	}
	if finished.Error == "" || !strings.Contains(finished.Error, "implementer command failed") {
		t.Fatalf("expected run_finished to carry the failure error, got %+v", finished)
	}
}

func TestRunRejectsJSONEventsCombinations(t *testing.T) {
	for _, args := range [][]string{
		{"run", "--json-events", "--json"},
		{"run", "--json-events", "--once"},
	} {
		var out, errOut bytes.Buffer
		code := withTempProcessEnv(t, func(home, cwd string) int {
			return Run(args, bytes.NewBuffer(nil), &out, &errOut)
		})
		if code != 2 {
			t.Fatalf("%v: expected usage error exit 2, got %d stderr=%s", args, code, errOut.String())
		}
		if !strings.Contains(errOut.String(), "--json-events cannot be combined") {
			t.Fatalf("%v: expected combination error, got: %s", args, errOut.String())
		}
	}
}

// handoffPingPongScript writes a handoff to the given target with a fresh
// timestamp on every run: alternating targets look like progress each round,
// so the loop only stops at its run budget.
func handoffPingPongScript(handoffDir, target, countFile, stampSuffix string) string {
	return strings.Join([]string{
		"#!/bin/sh",
		"set -eu",
		"trap 'exit 0' INT TERM",
		"i=$(cat " + shellQuote(countFile) + " 2>/dev/null || echo 0)",
		"i=$((i+1))",
		"echo $i > " + shellQuote(countFile),
		"mkdir -p " + shellQuote(handoffDir),
		"cat > " + shellQuote(handoffDir) + "/2026-06-05T1002${i}" + stampSuffix + "Z-loop.md <<'EOF'",
		"# Handoff: " + target,
		"",
		"To: " + target,
		"",
		"Keep going.",
		"EOF",
		"",
	}, "\n")
}

func TestRunLoopStopsAtMaxIterations(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		task, err := openTask(e, []string{"Churning Loop"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		handoffDir := filepath.Join(".agents", task.Path, "handoffs")
		// Implementer and reviewer keep handing off to each other without
		// recording findings or verification: never converging, but each
		// round changes the handoff target, so it is not same-target churn.
		implementer := filepath.Join(cwd, "implementer.sh")
		if err := os.WriteFile(implementer, []byte(handoffPingPongScript(handoffDir, "reviewer", "impl-count", "1")), 0o755); err != nil {
			t.Fatal(err)
		}
		reviewer := filepath.Join(cwd, "reviewer.sh")
		if err := os.WriteFile(reviewer, []byte(handoffPingPongScript(handoffDir, "implementer", "rev-count", "2")), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := "roles:\n  implementer: " + implementer + " {prompt_file}\n  reviewer: " + reviewer + " {prompt_file}\n  coordinator: codex\n"
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"run", "--max-iterations", "2"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 1 {
		t.Fatalf("expected max-iterations stop to exit 1, got %d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	if !strings.Contains(out.String(), "Loop paused after 2 role runs") {
		t.Fatalf("expected max-iterations stop message, got:\n%s", out.String())
	}
}

func TestRunLoopStopsOnRepeatedSameTargetHandoffs(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		if _, err := trustWorkspace(e); err != nil {
			t.Fatal(err)
		}
		task, err := openTask(e, []string{"Handoff Churn"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		handoffDir := filepath.Join(".agents", task.Path, "handoffs")
		// A do-nothing implementer that keeps writing handoffs to the same
		// target: new chunk names every round, but no findings or
		// verification movement. The loop must treat this as no progress.
		script := filepath.Join(cwd, "implementer.sh")
		if err := os.WriteFile(script, []byte(handoffPingPongScript(handoffDir, "implementer", "count", "0")), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := "roles:\n  implementer: " + script + " {prompt_file}\n  reviewer: codex\n  coordinator: codex\n"
		if err := os.WriteFile(filepath.Join(cwd, ".agents", "config.yml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		return Run([]string{"run"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code != 1 {
		t.Fatalf("expected handoff churn to exit 1, got %d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	if !strings.Contains(out.String(), "did not record new task progress") {
		t.Fatalf("expected no-progress stop message, got:\n%s", out.String())
	}
}

func TestAgentDescriptorQualifiesModel(t *testing.T) {
	cases := []struct {
		agent    string
		settings agentSettings
		want     string
	}{
		{"codex", agentSettings{CodexModel: "gpt-5.5", CodexReasoning: "high"}, "codex (model gpt-5.5, reasoning high)"},
		{"claude", agentSettings{ClaudeModel: "opus", ClaudeEffort: "xhigh"}, "claude (model opus, reasoning xhigh)"},
		{"agent", agentSettings{CursorModel: "gpt-5"}, "agent (model gpt-5)"},
		{"cursor", agentSettings{}, "cursor (model provider default)"},
		{"my-agent {prompt_file}", agentSettings{}, "my-agent {prompt_file} (custom command)"},
		{"", agentSettings{}, ""},
	}
	for _, tc := range cases {
		if got := agentDescriptor(tc.agent, tc.settings); got != tc.want {
			t.Fatalf("agentDescriptor(%q) = %q, want %q", tc.agent, got, tc.want)
		}
	}
}

func TestChunksRecordAgentDescriptor(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Model Attribution"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("AGENTS_ACTOR", "reviewer")
		t.Setenv("AGENTS_AGENT", "codex (model gpt-5.5, reasoning high)")
		if code := Run([]string{"finding", "add", "an issue"}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
			t.Fatalf("finding add code=%d stderr=%s", code, errOut.String())
		}
		if code := Run([]string{"verify", "checked it"}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
			t.Fatalf("verify code=%d stderr=%s", code, errOut.String())
		}
		for _, dir := range []string{"findings", "verification"} {
			entries, err := os.ReadDir(filepath.Join(cwd, ".agents", task.Path, dir))
			if err != nil || len(entries) == 0 {
				t.Fatalf("expected %s chunk, err=%v", dir, err)
			}
			data, err := os.ReadFile(filepath.Join(cwd, ".agents", task.Path, dir, entries[0].Name()))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "Agent: codex (model gpt-5.5, reasoning high)") {
				t.Fatalf("%s chunk missing qualified agent line:\n%s", dir, string(data))
			}
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestTaskChunkSettersUpdateTaskState(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Maintained Task"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		for _, set := range [][]string{
			{"task", "scope", "CLI surface and loop state only"},
			{"task", "status", "Setters implemented; tests pending"},
			{"task", "next", "Run the suite and hand off to reviewer"},
		} {
			if code := Run(set, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
				t.Fatalf("%v code=%d stderr=%s", set, code, errOut.String())
			}
		}
		checks := []struct {
			file, want string
		}{
			{"scope.md", "# Scope\n\nCLI surface and loop state only\n"},
			{"status.md", "# Status\n\nSetters implemented; tests pending\n"},
			{"next.md", "# Next\n\nRun the suite and hand off to reviewer\n"},
		}
		for _, check := range checks {
			data, err := os.ReadFile(filepath.Join(cwd, ".agents", task.Path, check.file))
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != check.want {
				t.Fatalf("%s = %q, want %q", check.file, string(data), check.want)
			}
		}
		// The next line should surface in status output.
		out.Reset()
		if code := Run([]string{"status"}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
			t.Fatalf("status code=%d stderr=%s", code, errOut.String())
		}
		if !strings.Contains(out.String(), "next: Run the suite and hand off to reviewer") {
			t.Fatalf("status missing updated next line:\n%s", out.String())
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestTaskChunkSetterRequiresActiveTask(t *testing.T) {
	var out, errOut bytes.Buffer
	code := withTempProcessEnv(t, func(home, cwd string) int {
		return Run([]string{"task", "status", "no task yet"}, bytes.NewBuffer(nil), &out, &errOut)
	})
	if code == 0 {
		t.Fatalf("expected task status without active task to fail")
	}
	if !strings.Contains(errOut.String(), "no active task") {
		t.Fatalf("expected active task error, got %q", errOut.String())
	}
}

func TestParseRunArgs(t *testing.T) {
	flags, err := parseRunArgs([]string{"--max-iterations", "4", "--once", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if !flags.JSON || !flags.Once || flags.MaxRuns != 4 {
		t.Fatalf("unexpected flags: %+v", flags)
	}
	if _, err := parseRunArgs([]string{"--max-iterations", "zero"}); err == nil {
		t.Fatal("expected invalid max-iterations to fail")
	}
	if _, err := parseRunArgs([]string{"--nope"}); err == nil {
		t.Fatal("expected unknown flag to fail")
	}
}

func withTempProcessEnv(t *testing.T, fn func(home, cwd string) int) int {
	t.Helper()
	origHome := os.Getenv("AGENTS_HOME")
	origNoBanner := os.Getenv("AGENTS_NO_BANNER")
	origPWD, _ := os.Getwd()
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origPWD)
		_ = os.Setenv("AGENTS_HOME", origHome)
		_ = os.Setenv("AGENTS_NO_BANNER", origNoBanner)
	})
	if err := os.Setenv("AGENTS_HOME", dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	return fn(dir, repo)
}

func writeDemoActiveTask(t *testing.T, cwd string) {
	t.Helper()
	taskDir := filepath.Join(cwd, ".agents", "tasks", "2026-06-04-001-demo--demo-1")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".agents", "active"), []byte("tasks/2026-06-04-001-demo--demo-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := "# Demo Task\n\nstatus: active\n\nA demo task.\n"
	if err := os.WriteFile(filepath.Join(taskDir, "task.md"), []byte(task), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeGitConfig(t *testing.T, repo, originURL string) {
	t.Helper()
	config := fmt.Sprintf("[remote \"origin\"]\n\turl = %s\n", originURL)
	if err := os.WriteFile(filepath.Join(repo, ".git", "config"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
}
