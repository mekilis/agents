package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var errAdviserIsolationUnavailable = errors.New("isolation_unavailable: adviser requires a working OS filesystem sandbox")
var errAdviserIsolationViolation = errors.New("isolation_violation: workspace changed during adviser execution")

func resolveAdviserCommand(a adviserInvocation, prompt, promptPath string) []string {
	if a.command != "" {
		return []string{"/bin/sh", "-c", substitutePromptFile(a.command, promptPath)}
	}
	var argv []string
	modelFlag, promptFlag := "--model", "--"
	switch a.Provider {
	case "codex":
		argv = []string{"codex", "exec", "--skip-git-repo-check", "--sandbox", "read-only"}
		modelFlag = "-m"
	case "claude":
		argv = []string{"claude", "-p", "--permission-mode", "plan", "--tools", ""}
	case "cursor":
		argv = []string{"agent", "-p", "--output-format", "stream-json", "--stream-partial-output"}
	case "gemini":
		argv = []string{"gemini", "--skip-trust"}
		promptFlag = "--prompt"
	case "agy":
		argv = []string{"agy"}
		promptFlag = "--print"
	}
	if a.Model != "" {
		argv = append(argv, modelFlag, a.Model)
	}
	if a.Provider == "codex" && a.Reasoning != "" {
		argv = append(argv, "-c", "model_reasoning_effort="+quoteTOMLString(a.Reasoning))
	}
	if a.Provider == "claude" && a.Reasoning != "" {
		argv = append(argv, "--effort", a.Reasoning)
	}
	return append(argv, promptFlag, prompt)
}

func pathInside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func adviserScratch(roots []string) (string, error) {
	for _, parent := range []string{os.TempDir(), "/tmp"} {
		realParent, err := filepath.EvalSymlinks(parent)
		if err != nil {
			continue
		}
		protected := false
		for _, root := range roots {
			protected = protected || pathInside(root, realParent)
		}
		if protected {
			continue
		}
		scratch, err := os.MkdirTemp(realParent, "agents-adviser-*")
		if err == nil {
			return scratch, nil
		}
	}
	return "", errAdviserIsolationUnavailable
}

func adviserGit(ctx context.Context, cwd string, args ...string) ([]byte, error) {
	gitArgs := append([]string{"-C", cwd, "--no-optional-locks", "-c", "core.fsmonitor=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	return cmd.Output()
}

func adviserGitPaths(ctx context.Context, cwd string) ([]string, error) {
	data, err := adviserGit(ctx, cwd, "rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir")
	if err != nil {
		// Recognize standalone workspaces separately from a broken Git worktree.
		for dir := cwd; ; dir = filepath.Dir(dir) {
			if _, statErr := os.Lstat(filepath.Join(dir, ".git")); !errors.Is(statErr, os.ErrNotExist) {
				return nil, fmt.Errorf("cannot resolve adviser Git paths: %w", err)
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, nil
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 2 {
		return nil, fmt.Errorf("cannot resolve adviser Git paths")
	}
	for i, path := range lines {
		lines[i], err = filepath.EvalSymlinks(path)
		if err != nil {
			return nil, err
		}
	}
	return lines, nil
}

func adviserSandboxArgs(roots []string, scratch string) ([]string, error) {
	switch runtime.GOOS {
	case "darwin":
		var profile strings.Builder
		profile.WriteString("(version 1)\n(allow default)\n")
		for _, root := range roots {
			fmt.Fprintf(&profile, "(deny file-write* (subpath %s))\n", strconv.Quote(root))
		}
		return []string{"/usr/bin/sandbox-exec", "-p", profile.String()}, nil
	case "linux":
		bwrap, err := exec.LookPath("bwrap")
		if err != nil {
			return nil, errAdviserIsolationUnavailable
		}
		args := []string{bwrap, "--die-with-parent", "--unshare-pid", "--dev-bind", "/", "/", "--proc", "/proc"}
		for _, root := range roots {
			args = append(args, "--ro-bind", root, root)
		}
		return append(args, "--bind", scratch, scratch, "--chdir", scratch, "--"), nil
	default:
		return nil, errAdviserIsolationUnavailable
	}
}

func executeSubprocessAdviser(ctx context.Context, cwd string, a adviserInvocation, prompt string, sandboxArgs func([]string, string) ([]string, error)) (string, error) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return "", errAdviserIsolationUnavailable
	}
	workspace, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", err
	}
	state, err := filepath.EvalSymlinks(filepath.Join(workspace, stateDir))
	if err != nil {
		return "", err
	}
	gitPaths, err := adviserGitPaths(ctx, workspace)
	if err != nil {
		return "", err
	}
	roots := append([]string{workspace, state}, gitPaths...)
	scratch, err := adviserScratch(roots)
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)
	wrapper, err := sandboxArgs(roots, scratch)
	if err != nil {
		return "", err
	}
	prompt = strings.ReplaceAll(prompt, workspace, ".")
	promptPath := filepath.Join(scratch, "prompt.txt")
	if err := os.WriteFile(promptPath, []byte(prompt), 0400); err != nil {
		return "", err
	}
	build := func(ctx context.Context, argv []string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, wrapper[0], append(append([]string{}, wrapper[1:]...), argv...)...)
		cmd.Dir = scratch
		configureManagedProcess(cmd)
		cmd.Cancel = func() error { return killManagedProcess(cmd) }
		cmd.WaitDelay = 500 * time.Millisecond
		for _, entry := range cmd.Environ() {
			key, _, _ := strings.Cut(entry, "=")
			switch {
			case strings.HasPrefix(key, "AGENTS_"), strings.HasPrefix(key, "GIT_"),
				key == "OLDPWD", key == "TMPDIR", key == "TMP", key == "TEMP", key == "XDG_CACHE_HOME",
				strings.Contains(entry, workspace), strings.Contains(entry, cwd):
				continue
			}
			cmd.Env = append(cmd.Env, entry)
		}
		cmd.Env = append(cmd.Env, "TMPDIR="+scratch, "TMP="+scratch, "TEMP="+scratch, "XDG_CACHE_HOME="+scratch)
		return cmd
	}
	if err := build(ctx, []string{"/bin/sh", "-c", ":"}).Run(); err != nil {
		return "", errAdviserIsolationUnavailable
	}
	before, err := adviserManifest(ctx, workspace, state, gitPaths)
	if err != nil {
		return "", err
	}
	argv := resolveAdviserCommand(a, prompt, promptPath)
	cmd := build(ctx, argv)
	var output adviserOutput
	cmd.Stdout = &output
	runErr := cmd.Run()
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	after, err := adviserManifest(checkCtx, workspace, state, gitPaths)
	if err != nil || !reflect.DeepEqual(before, after) {
		return "", errAdviserIsolationViolation
	}
	if runErr != nil {
		return "", fmt.Errorf("adviser subprocess failed: %w", runErr)
	}
	if output.overflow {
		return "", fmt.Errorf("adviser subprocess output exceeds limit")
	}
	if a.Provider == "cursor" {
		return cursorAdviserText(output.String()), nil
	}
	return output.String(), nil
}

type adviserOutput struct {
	bytes.Buffer
	overflow bool
}

func (o *adviserOutput) Write(p []byte) (int, error) {
	if len(p) > maxAdviserOutput-o.Len() {
		o.overflow = true
		return len(p), nil
	}
	return o.Buffer.Write(p)
}

func cursorAdviserText(output string) string {
	var assistant strings.Builder
	for _, line := range strings.Split(output, "\n") {
		var event struct {
			Type    string
			Result  string
			Message struct {
				Content []struct{ Text string }
			}
		}
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		if event.Type == "result" && event.Result != "" {
			return event.Result
		}
		if event.Type == "assistant" {
			for _, content := range event.Message.Content {
				assistant.WriteString(content.Text)
			}
		}
	}
	if assistant.Len() != 0 {
		return assistant.String()
	}
	return output
}

type adviserFileDigest struct {
	mode fs.FileMode
	sum  [32]byte
}

func adviserManifest(ctx context.Context, workspace, state string, gitPaths []string) (map[string]adviserFileDigest, error) {
	files := make(map[string]adviserFileDigest)
	record := func(path string, info fs.FileInfo) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var digest adviserFileDigest
		digest.mode = info.Mode()
		switch {
		case info.Mode().IsRegular():
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			hash := sha256.New()
			_, err = io.Copy(hash, f)
			if err := errors.Join(err, f.Close()); err != nil {
				return err
			}
			copy(digest.sum[:], hash.Sum(nil))
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			digest.sum = sha256.Sum256([]byte(target))
		}
		files[path] = digest
		return nil
	}
	// Round publication by another runner can occur during execution. The OS
	// protects these paths too, while the manifest ignores the concurrent stores.
	operational := func(path string) bool {
		for _, dir := range []string{state, filepath.Join(workspace, stateDir)} {
			if pathInside(filepath.Join(dir, "reputation"), path) {
				return true
			}
			if filepath.Dir(path) == filepath.Join(dir, "tmp") {
				name := filepath.Base(path)
				if name == "adviser-routing.json" || name == "reputation.lock" ||
					strings.HasPrefix(name, "adviser-routing.json.") && strings.HasSuffix(name, ".tmp") {
					return true
				}
			}
		}
		return false
	}
	walk := func(root string) error {
		return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if operational(path) {
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if err != nil {
				return err
			}
			if d.IsDir() && path == filepath.Join(state, "tmp") {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			return record(path, info)
		})
	}
	if err := walk(state); err != nil {
		return nil, err
	}
	if len(gitPaths) == 0 {
		return files, nil
	}
	status, err := adviserGit(ctx, workspace, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return nil, err
	}
	// Git porcelain paths are relative to the repository top-level, even
	// when the Agents workspace lives in a subdirectory.
	top, err := adviserGit(ctx, workspace, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	topDir := strings.TrimSuffix(string(top), "\n")
	var stableStatus bytes.Buffer
	for _, entry := range bytes.Split(status, []byte{0}) {
		if len(entry) < 4 {
			continue
		}
		path := filepath.Join(topDir, string(entry[3:]))
		if operational(path) {
			continue
		}
		stableStatus.Write(entry)
		stableStatus.WriteByte(0)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := record(path, info); err != nil {
			return nil, err
		}
	}
	files["git status"] = adviserFileDigest{sum: sha256.Sum256(stableStatus.Bytes())}
	for _, dir := range gitPaths {
		for _, name := range []string{"HEAD", "index", "refs", "packed-refs"} {
			err := walk(filepath.Join(dir, name))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
	}
	return files, nil
}
