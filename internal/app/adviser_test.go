package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func adviserTestEnv(t *testing.T, config string) env {
	t.Helper()
	e := env{cwd: t.TempDir(), trustPath: filepath.Join(t.TempDir(), "trust.json"), actor: "test"}
	dir := filepath.Join(e.cwd, stateDir)
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := trustWorkspace(e); err != nil {
		t.Fatal(err)
	}
	return e
}

func adviserCLI(t *testing.T, e env, wantCode int, args ...string) string {
	t.Helper()
	var out, stderr bytes.Buffer
	code := runOneShot(append([]string{"advise"}, args...), strings.NewReader(""), &out, &stderr, e)
	if code != wantCode {
		t.Fatalf("advise %v exit %d want %d\nstdout: %s\nstderr: %s", args, code, wantCode, &out, &stderr)
	}
	if code != 0 {
		return stderr.String()
	}
	return out.String()
}

func adviserAsk(t *testing.T, e env, prompt string) adviserResponse {
	t.Helper()
	data := adviserCLI(t, e, 0, "ask", "--json", "--", prompt)
	var result adviserResponse
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		t.Fatal(err, data)
	}
	return result
}

var adviserTestBinding = regexp.MustCompile(`(?m)^\{"round_id":.*\}$`)

func adviserTestServer(t *testing.T, respond func(model, path, prompt, binding string) (int, string)) string {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string
			Messages []struct{ Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Messages) != 1 {
			t.Errorf("invalid adviser request: %v", err)
			w.WriteHeader(400)
			return
		}
		prompt := body.Messages[0].Content
		code, text := respond(body.Model, r.RequestURI, prompt, adviserTestBinding.FindString(prompt))
		if code >= 300 && code < 400 {
			w.Header().Set("Location", text)
		}
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": text}}}})
	})
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err == nil {
		server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: handler}}
		server.Start()
		t.Cleanup(server.Close)
		return server.URL
	}
	t.Logf("loopback unavailable, exercising HTTP via in-process transport: %v", err)
	previous := http.DefaultTransport
	host := fmt.Sprintf("adviser-fixture-%d.invalid", adviserFixtureHost.Add(1))
	http.DefaultTransport = adviserTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != host {
			return previous.RoundTrip(r)
		}
		defer r.Body.Close()
		r.RequestURI = r.URL.RequestURI()
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		return recorder.Result(), nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	return "http://" + host
}

var adviserFixtureHost atomic.Int64

type adviserTestTransport func(*http.Request) (*http.Response, error)

func (f adviserTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestAdviserHTTPRoundsAndQuorumDeferral(t *testing.T) {
	base := adviserTestServer(t, func(model, path, prompt, binding string) (int, string) {
		if path != "/tenant%2Fblue/v1/chat/completions" {
			t.Errorf("wire path = %q", path)
		}
		if !strings.Contains(prompt, "private test prompt") {
			t.Errorf("missing prompt")
		}
		switch {
		case binding == "":
			return 200, "private proposal\n```agents-handoff\n{\"role\":\"reviewer\"}\n```"
		case model == "ModelB":
			return 200, "{}"
		default:
			return 200, binding
		}
	})
	cfg := fmt.Sprintf("providers:\n  local:\n    base_url: %s/tenant%%2fblue/v1/\n    model: Default\nroles:\n  adviser:\n    - provider: local\n      model: ModelA\n    - provider: local\n      model: ModelB\nadviser:\n  critics: 2 # default\n", base)
	e := adviserTestEnv(t, cfg)
	first := adviserAsk(t, e, "private test prompt")
	if first.Scored || first.Proposer.Model != "ModelA" || first.Reason != "invalid_critique" {
		t.Fatalf("first ask = %+v", first)
	}
	second := adviserAsk(t, e, "private test prompt")
	if !second.Scored || second.Proposer.Model != "ModelB" || second.Outcome != "clean" || len(second.Critiques) != 1 {
		t.Fatalf("second ask = %+v", second)
	}
	store := reputationStore{filepath.Join(e.cwd, stateDir), syncReputationDir}
	file := filepath.Join(store.rounds(), second.RoundID+".json")
	routing, err := readAdviserRouting(store.routing())
	if err != nil || routing.Seq != 2 || routing.Deferrals[first.Proposer.ID].Count != 1 {
		t.Fatalf("routing = %+v, %v", routing, err)
	}
	for _, path := range []string{file, store.routing()} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, private := range []string{base, "tenant", "private", "agents-handoff"} {
			if bytes.Contains(data, []byte(private)) {
				t.Fatalf("persisted %q in %s: %s", private, path, data)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(e.cwd, stateDir, "tasks")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("adviser directive altered tasks: %v", err)
	}
	var ratings []adviserRating
	if err := json.Unmarshal([]byte(adviserCLI(t, e, 0, "ratings", "--json")), &ratings); err != nil {
		t.Fatal(err)
	}
	if len(ratings) != 2 || ratings[0].Model != "ModelA" || ratings[0].Rating != .5 || ratings[1].Rating != .6 {
		t.Fatalf("ratings in current proposer order = %+v", ratings)
	}
}

func TestAdviserFindingsPrecedenceAndArchive(t *testing.T) {
	base := adviserTestServer(t, func(model, _, _, binding string) (int, string) {
		if binding == "" {
			return 200, "proposal"
		}
		if model == "B" {
			binding = strings.Replace(binding, `"verdict":"clean"`, `"verdict":"findings"`, 1)
			binding = strings.Replace(binding, `"findings":[]`, `"findings":[{"severity":"high","text":"missing cost","evidence":"proposal gives no estimate"},{"severity":"low","text":"missing owner","evidence":"proposal assigns no owner"}]`, 1)
		}
		return 200, binding
	})
	e := adviserTestEnv(t, fmt.Sprintf("providers:\n  a:\n    base_url: %s\n    model: A\n  b:\n    base_url: %s\n    model: B\n  c:\n    base_url: %s\n    model: C\nroles:\n  adviser: [a, b, c]\n", base, base, base))
	task, err := openTask(e, []string{"Task to archive"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	result := adviserAsk(t, e, "advice")
	if !result.Scored || result.Outcome != "findings" || len(result.Critiques) != 2 {
		t.Fatalf("ask = %+v", result)
	}
	if _, err := archiveActiveTask(e, time.Now()); err != nil {
		t.Fatalf("archive %s: %v", task.Path, err)
	}
	store := reputationStore{filepath.Join(e.cwd, stateDir), syncReputationDir}
	h, err := readReputation(store.rounds())
	rating := h.ratings[result.Proposer.ID]
	if err != nil || h.total != 1 || rating.Total != 1 || rating.Clean != 0 {
		t.Fatalf("history = %+v, %v", h, err)
	}
	if _, err := os.Stat(store.routing()); err != nil {
		t.Fatal(err)
	}
}

func TestAdviserGrammarFallbackAndAliases(t *testing.T) {
	base := adviserTestServer(t, func(model, _, prompt, binding string) (int, string) {
		if model != "ModelA" || binding != "" {
			t.Errorf("unexpected model or critique: %q %q", model, binding)
		}
		return 200, prompt
	})
	e := adviserTestEnv(t, fmt.Sprintf("providers:\n  a:\n    base_url: %s/v1\n    model: ModelA\n  alias:\n    base_url: %s/v1/\n    model: ModelA\n    effort: high\nroles:\n  adviser: [a, alias]\n", base, base))
	adviserCLI(t, e, 2, "ratings", "ratings")
	adviserCLI(t, e, 2, "anything")
	for _, args := range [][]string{{"ask", "ratings", "--json"}, {"ask", "--json", "--", "--json"}} {
		out := adviserCLI(t, e, 0, args...)
		if !strings.Contains(out, "insufficient_verifiable_peers") || !strings.Contains(out, "Standalone workspace") {
			t.Fatalf("fallback = %s", out)
		}
	}
	if _, err := os.Stat(filepath.Join(e.cwd, stateDir, "reputation")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fallback wrote reputation: %v", err)
	}
	e = adviserTestEnv(t, "roles:\n  implementer: agent\n")
	if output := adviserCLI(t, e, 1, "ask", "hi"); !strings.Contains(output, "roles.adviser") {
		t.Fatal(output)
	}
}

func TestAdviserRedirectRefusal(t *testing.T) {
	var redirected atomic.Int64
	target := adviserTestServer(t, func(_, _, _, _ string) (int, string) {
		redirected.Add(1)
		return 200, "unexpected"
	})
	for _, code := range []int{301, 302, 303, 307, 308} {
		for _, stage := range []string{"fallback", "proposer", "critic"} {
			t.Run(fmt.Sprintf("%d/%s", code, stage), func(t *testing.T) {
				base := adviserTestServer(t, func(model, _, _, binding string) (int, string) {
					if stage != "critic" || model == "B" {
						return code, target
					}
					return 200, "proposal"
				})
				roles := "a"
				if stage != "fallback" {
					roles = "[a, b]"
				}
				e := adviserTestEnv(t, fmt.Sprintf("providers:\n  a:\n    base_url: %s\n    model: A\n  b:\n    base_url: %s\n    model: B\nroles:\n  adviser: %s\n", base, base, roles))
				if stage == "fallback" {
					adviserCLI(t, e, 1, "ask", "redirect")
				} else if result := adviserAsk(t, e, "redirect"); result.Scored {
					t.Fatalf("redirect was scored: %+v", result)
				}
				if redirected.Load() != 0 {
					t.Fatalf("redirect target was called")
				}
				history, err := readReputation(filepath.Join(e.cwd, stateDir, "reputation", "rounds"))
				if err != nil || history.total != 0 {
					t.Fatalf("redirect changed reputation: %+v, %v", history, err)
				}
			})
		}
	}
}

func TestAdviserConfigAndInvocationIdentity(t *testing.T) {
	for _, config := range []string{
		"adviser:\n  critics: 0\n", "adviser:\n  explore_every: x\n", "adviser:\n  rating: 5\n",
		"roles:\n  adviser:\n    - provider: codex\n      score: 1\n",
		"providers:\n  private:\n    weight: 2\n",
	} {
		if err := parseAdviserConfig(config, &adviserConfig{}); err == nil {
			t.Fatalf("accepted %q", config)
		}
	}
	cfg := config{
		Providers: map[string]providerConfig{
			"local":      {Name: "local", BaseURL: "http://host/v1", Model: "ModelA"},
			"opaque":     {Name: "opaque", Cmd: "cat {prompt_file}", Model: "ModelA"},
			"bad":        {Name: "bad", Cmd: "cat", BaseURL: "http://host"},
			"bad-driver": {Name: "bad-driver", Provider: "codex", BaseURL: "http://host"},
			"bad-url":    {Name: "bad-url", Type: "openai_compatible"},
		},
		Settings: agentSettings{Reasoning: "LOW"},
	}
	for _, test := range []struct {
		providers            []string
		canonical, reasoning string
	}{
		{[]string{"cursor", "cursor-agent", "agent"}, "cursor", ""},
		{[]string{"agy", "antigravity"}, "agy", ""},
		{[]string{"gemini"}, "gemini", ""},
		{[]string{"codex"}, "codex", "HIGH"},
		{[]string{"claude"}, "claude", "HIGH"},
		{[]string{"local"}, "openai-compat", ""},
	} {
		var id string
		for _, name := range test.providers {
			a, err := resolveAdviserInvocation(cfg, roleConfig{Provider: name, Model: " Model/B@X:# ", Reasoning: " HIGH "})
			if err != nil || a.Provider != test.canonical || a.Model != "Model/B@X:#" || a.Reasoning != test.reasoning || (id != "" && a.ID != id) {
				t.Fatalf("resolve %s = %+v, %v", name, a, err)
			}
			id = a.ID
			if a.baseURL == "" {
				command := strings.Join(resolveAdviserCommand(a, "prompt", "unused"), " ")
				if !strings.Contains(command, a.Model) || strings.Contains(command, "--force") ||
					strings.Contains(command, "workspace-write") || strings.Contains(command, "acceptEdits") ||
					strings.Contains(command, "auto_edit") || strings.Contains(command, "skip-permissions") {
					t.Fatal(command)
				}
			}
		}
	}
	a, err := resolveAdviserInvocation(cfg, roleConfig{Provider: "claude"})
	if err != nil || a.Reasoning != "" || a.Model != "" {
		t.Fatalf("implicit Claude settings incorrectly sent: %+v, %v", a, err)
	}
	a, err = resolveAdviserInvocation(cfg, roleConfig{Provider: "opaque"})
	if err != nil || a.Model != "" || a.command == "" {
		t.Fatalf("opaque command received a rated identity: %+v, %v", a, err)
	}
	for _, provider := range []string{"bad", "bad-driver", "bad-url", "unknown"} {
		if _, err := resolveAdviserInvocation(cfg, roleConfig{Provider: provider}); err == nil {
			t.Fatalf("accepted %s", provider)
		}
	}
}

func TestAdviserCanonicalEndpoint(t *testing.T) {
	for input, want := range map[string]string{
		"HTTP://Host:80/v1/":                 "http://host/v1",
		"https://[::1]:443/%61":              "https://[::1]/a",
		"http://Host:8000/tenant%2fblue/v1/": "http://host:8000/tenant%2Fblue/v1",
		"http://Host:8000/tenant/blue/v1/":   "http://host:8000/tenant/blue/v1",
	} {
		got, err := canonicalizeAdviserBaseURL(input)
		if err != nil || got != want {
			t.Errorf("%q: %q, %v want %q", input, got, err, want)
		}
	}
	for _, input := range []string{
		"git://host", "http://", "http://user@host", "http://host?", "http://host#", "http://host/path\\",
		"http://host/v1//", "http://host//a", "http://host/.", "http://host/%2e%2e", "http://host/%00",
		"http://host/%1a", "http://host/%7f", "http://host/%XZ", "http://host:0", "http://host:65536", "http://host:",
	} {
		if _, err := canonicalizeAdviserBaseURL(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestAdviserCritiqueRejectsInvalidContracts(t *testing.T) {
	round := reputationRound{ID: "round", ProposalID: "proposal", Proposer: adviserIdentity{ID: "proposer"}}
	valid := critiqueTemplate(round, "critic")
	fenced := "```agents-critique\n" + valid + "\n```"
	for _, output := range []string{valid, fenced, "Review follows.\r\n" + strings.ReplaceAll(fenced, "\n", "\r\n") + "\r\nEnd of review."} {
		if _, err := parseAdviserCritique(output, round, "critic", true); err != nil {
			t.Fatalf("reject valid critique: %v", err)
		}
	}
	for _, output := range []string{"looks good", "{", fenced + "\nFraming prose\n" + fenced, strings.TrimSuffix(fenced, "\n```"),
		strings.Replace(valid, `"critic_id":"critic"`, `"critic_id":"proposer"`, 1),
		strings.Replace(valid, `"round_id":"round"`, `"round_id":"other"`, 1),
		strings.Replace(valid, `"proposal_id":"proposal"`, `"proposal_id":"other"`, 1),
		strings.Replace(valid, `"verdict":"clean"`, `"verdict":"findings"`, 1),
		strings.Replace(valid, `"verdict":"clean"`, `"verdict":"approved"`, 1),
		strings.Replace(valid, `"verdict":"clean"`, `"verdict":"invalid","verdict":"clean"`, 1),
		strings.Replace(valid, `"findings":[]`, `"findings":[{"severity":"high","text":"bad","evidence":"bad"}]`, 1),
		strings.Replace(valid, `,"findings":[]`, "", 1),
		strings.Replace(valid, `"findings":[]`, `"findings":null`, 1),
	} {
		if _, err := parseAdviserCritique(output, round, "critic", true); err == nil {
			t.Fatalf("accepted %s", output)
		}
	}
	for _, fields := range []string{
		`"severity":"HIGH","text":"problem","evidence":"specific"`,
		`"severity":"low","text":"  ","evidence":"specific"`,
		`"severity":"low","text":"problem","evidence":" "`,
	} {
		output := strings.Replace(valid, `"verdict":"clean"`, `"verdict":"findings"`, 1)
		output = strings.Replace(output, `"findings":[]`, `"findings":[{`+fields+`}]`, 1)
		if _, err := parseAdviserCritique(output, round, "critic", true); err == nil {
			t.Fatalf("accepted %s", output)
		}
	}
	if _, err := parseAdviserCritique(valid, round, "critic", false); err == nil {
		t.Fatal("subprocess raw JSON accepted")
	}
}

func TestAdviserSubprocessFilesystemBoundary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no subprocess sandbox on Windows")
	}
	e := adviserTestEnv(t, "")
	ctx := context.Background()
	git := func(args ...string) {
		t.Helper()
		if output, err := adviserGit(ctx, e.cwd, args...); err != nil {
			t.Fatalf("git %v: %v %s", args, err, output)
		}
	}
	git("init")
	files := []string{
		filepath.Join(e.cwd, "clean"),
		filepath.Join(e.cwd, "dirty"),
		filepath.Join(e.cwd, stateDir, "task"),
	}
	for _, path := range files {
		if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	git("add", "clean", "dirty")
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "fixture")
	if err := os.WriteFile(files[1], []byte("already dirty"), 0644); err != nil {
		t.Fatal(err)
	}
	newFile := filepath.Join(e.cwd, "new")
	marker := filepath.Join(t.TempDir(), "cache")
	var script strings.Builder
	script.WriteString("#!/bin/sh\n")
	for _, path := range append(append([]string{}, files...), newFile) {
		fmt.Fprintf(&script, "(printf mutated > %s) 2>/dev/null && exit 71\n", shellQuote(path))
	}
	fmt.Fprintf(&script, "printf cache > %s || exit 72\n", shellQuote(marker))
	script.WriteString("printf scratch > relative.txt || exit 73\n")
	script.WriteString("printf 'proposal only'\n")
	fakeBin := t.TempDir()
	fakeCodex := filepath.Join(fakeBin, "codex")
	if err := os.WriteFile(fakeCodex, []byte(script.String()), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Check sandbox availability independently of the production preflight.
	darwinSandboxAvailable := false
	if runtime.GOOS == "darwin" {
		output, err := exec.Command("/usr/bin/sandbox-exec", "-p", "(version 1) (allow default)", "/usr/bin/true").CombinedOutput()
		darwinSandboxAvailable = err == nil
		if err != nil {
			t.Logf("macOS sandbox probe: %v: %s", err, output)
		}
	}
	for _, a := range []adviserInvocation{
		{adviserIdentity: adviserIdentity{Provider: "cmd"}, command: shellQuote(fakeCodex), timeout: 10 * time.Second},
		{adviserIdentity: adviserIdentity{Provider: "codex", Model: "Test"}, timeout: 10 * time.Second},
	} {
		output, err := executeAdviser(ctx, e.cwd, a, "test")
		if errors.Is(err, errAdviserIsolationUnavailable) && !darwinSandboxAvailable {
			for _, path := range []string{newFile, marker} {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("unavailable sandbox let subprocess write %s", path)
				}
			}
			t.Log("OS sandbox cannot initialize here; verified subprocess refusal")
		} else if err != nil || output != "proposal only" {
			t.Fatalf("isolated execution: %q %v", output, err)
		} else if data, err := os.ReadFile(marker); err != nil || string(data) != "cache" {
			t.Fatalf("outside cache write: %q %v", data, err)
		}
		for i, path := range files {
			data, err := os.ReadFile(path)
			if want := []string{"original", "already dirty", "original"}[i]; err != nil || string(data) != want {
				t.Fatalf("boundary modified %s: %q %v", path, data, err)
			}
		}
		if _, err := os.Stat(newFile); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("boundary let subprocess create file: %v", err)
		}
	}
}

func TestAdviserStateDoesNotChangeLegacyRoleConfig(t *testing.T) {
	for _, adviser := range []string{"", "  adviser:\n    - provider: codex\n      model: Test\n    - gemini\n"} {
		e := adviserTestEnv(t, "providers:\n  local:\n    type: openai_compatible\n    base_url: http://localhost/v1\n    model: Test\nroles:\n  implementer: agent\n  reviewer: [local, codex]\n"+adviser)
		cfg, err := readConfig(e.cwd)
		if err != nil {
			t.Fatal(err)
		}
		reviewers := cfg.reviewerRoles()
		if cfg.Roles["implementer"].Provider != "agent" || len(reviewers) != 2 || reviewers[0].Provider != "local" ||
			reviewers[1].Provider != "codex" || cfg.Providers["local"].BaseURL != "http://localhost/v1" {
			t.Fatalf("legacy routing changed: %+v", cfg)
		}
		if _, err := openTask(e, []string{"Legacy role fixture"}, time.Now()); err != nil {
			t.Fatal(err)
		}
		var out, diagnostic bytes.Buffer
		for _, args := range [][]string{{"status", "--json"}, {"run", "--json"}} {
			out.Reset()
			if code := runOneShot(args, strings.NewReader(""), &out, &diagnostic, e); code != 0 || !json.Valid(out.Bytes()) {
				t.Fatalf("legacy %v: %d %s %s", args, code, &out, &diagnostic)
			}
		}
		for _, args := range [][]string{{"plan", "draft", "Fixture objective"}, {"plan", "approve"}} {
			if code := runOneShot(args, strings.NewReader(""), &out, &diagnostic, e); code != 0 {
				t.Fatalf("legacy %v: %d %s", args, code, &diagnostic)
			}
		}
		plan, err := readActiveTaskPlan(e)
		if err != nil || plan.State != planStateApproved {
			t.Fatalf("plan approval changed: %+v %v", plan, err)
		}
	}
}

func TestAdviserUnavailableLinuxBackend(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux backend lookup")
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := exec.LookPath("bwrap"); err == nil {
		t.Fatal("unexpected bwrap on restricted PATH")
	}
	if _, err := adviserSandboxArgs(nil, "/tmp"); !errors.Is(err, errAdviserIsolationUnavailable) {
		t.Fatalf("missing backend: %v", err)
	}
}

func TestAdviserIsolationViolationUpdatesRoutingOnly(t *testing.T) {
	e := adviserTestEnv(t, "roles:\n  adviser:\n    - provider: codex\n      model: ModelA\n    - provider: codex\n      model: modela\n    - provider: codex\n      model: ModelB\n")
	store := reputationStore{filepath.Join(e.cwd, stateDir), syncReputationDir}
	for i := range 2 {
		execute := func(_ context.Context, _ string, a adviserInvocation, _ string) (string, error) {
			if a.Model == "ModelA" {
				return "", errAdviserIsolationViolation
			}
			return "proposal", nil
		}
		var out bytes.Buffer
		if err := runAdviser(context.Background(), e, "ask", "test", true, &out, execute); err != nil {
			t.Fatal(err)
		}
		var response adviserResponse
		if err := json.Unmarshal(out.Bytes(), &response); err != nil || response.Scored || response.Reason != "isolation_violation" {
			t.Fatalf("response: %s (%v)", out.String(), err)
		}
		if want := []string{"ModelA", "modela"}[i]; response.Proposer.Model != want {
			t.Fatalf("unexpected proposer: %s", response.Proposer.Model)
		}
	}
	routing, err := readAdviserRouting(store.routing())
	if err != nil || routing.Seq != 2 || routing.Proposers["codex/ModelA"].Count != 1 || routing.Deferrals["codex/modela"].Count != 1 || routing.Critics["codex/ModelA"].Count != 1 {
		t.Fatalf("routing: %+v (%v)", routing, err)
	}
	if _, err := os.Stat(store.rounds()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("isolation violation created reputation store: %v", err)
	}
}

func TestAdviserSandboxStartupFailureLeavesHTTPAvailable(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("no subprocess sandbox backend")
	}
	base := adviserTestServer(t, func(_, _, _, _ string) (int, string) { return 200, "HTTP proposal" })
	e := adviserTestEnv(t, fmt.Sprintf("providers:\n  local:\n    base_url: %s\n    model: Test\nroles:\n  adviser: [local]\n", base))
	marker := filepath.Join(t.TempDir(), "spawned")
	a := adviserInvocation{command: "echo ran > " + shellQuote(marker)}
	wrapper := func(_ []string, _ string) ([]string, error) {
		return []string{"/bin/sh", "-c", "exit 1"}, nil
	}
	if _, err := executeSubprocessAdviser(context.Background(), e.cwd, a, "test", wrapper); !errors.Is(err, errAdviserIsolationUnavailable) {
		t.Fatalf("sandbox startup failure: %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("adviser ran despite sandbox startup failure: %v", err)
	}
	if result := adviserAsk(t, e, "test"); result.Proposal != "HTTP proposal" || result.Scored {
		t.Fatalf("HTTP fallback: %+v", result)
	}
}
