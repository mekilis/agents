package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type trustStore struct {
	Workspaces []trustedWorkspace `json:"workspaces"`
}

type trustedWorkspace struct {
	Path      string `json:"path"`
	TrustedAt string `json:"trusted_at"`
	User      string `json:"user,omitempty"`
	Host      string `json:"host,omitempty"`
	GitRemote string `json:"git_remote,omitempty"`
}

func isTrusted(e env) (bool, error) {
	store, err := readTrustStore(e.trustPath)
	if err != nil {
		return false, err
	}
	currentRemote := readGitRemote(e.cwd)
	cwd := canonicalPath(e.cwd)
	for _, ws := range store.Workspaces {
		if canonicalPath(ws.Path) == cwd && trustRemoteMatches(ws.GitRemote, currentRemote) {
			return true, nil
		}
	}
	return false, nil
}

// canonicalPath resolves symlinks so trust survives path aliases (on macOS
// /tmp and /var are symlinks into /private). Unresolvable paths compare as-is.
func canonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

func trustWorkspace(e env) (trustedWorkspace, error) {
	store, err := readTrustStore(e.trustPath)
	if err != nil {
		return trustedWorkspace{}, err
	}
	user, host := localTrustIdentity()
	record := trustedWorkspace{
		Path:      e.cwd,
		TrustedAt: time.Now().UTC().Format(time.RFC3339),
		User:      user,
		Host:      host,
		GitRemote: readGitRemote(e.cwd),
	}
	replaced := false
	for i, ws := range store.Workspaces {
		if ws.Path == e.cwd {
			store.Workspaces[i] = record
			replaced = true
			break
		}
	}
	if !replaced {
		store.Workspaces = append(store.Workspaces, record)
	}
	sort.Slice(store.Workspaces, func(i, j int) bool {
		return store.Workspaces[i].Path < store.Workspaces[j].Path
	})
	return record, writeTrustStore(e.trustPath, store)
}

func localTrustIdentity() (user, host string) {
	user = strings.TrimSpace(os.Getenv("USER"))
	if user == "" {
		user = strings.TrimSpace(os.Getenv("LOGNAME"))
	}
	if h, err := os.Hostname(); err == nil {
		host = strings.TrimSpace(h)
	}
	return user, host
}

func trustRemoteMatches(trustedRemote, currentRemote string) bool {
	return trustedRemote == "" || trustedRemote == currentRemote
}

func readGitRemote(cwd string) string {
	data, err := os.ReadFile(filepath.Join(cwd, ".git", "config"))
	if err != nil {
		return ""
	}
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.TrimSpace(strings.Trim(trimmed, "[]"))
			continue
		}
		if section != `remote "origin"` {
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if ok && strings.TrimSpace(key) == "url" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func readTrustStore(path string) (trustStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return trustStore{}, nil
		}
		return trustStore{}, err
	}
	var store trustStore
	if err := json.Unmarshal(data, &store); err != nil {
		return trustStore{}, err
	}
	return store, nil
}

func writeTrustStore(path string, store trustStore) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}
