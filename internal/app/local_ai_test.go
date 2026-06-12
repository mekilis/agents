package app

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseLocalAIClassificationExtractsJSON(t *testing.T) {
	got, err := parseLocalAIClassification("```json\n{\"intent\":\"small_talk\",\"confidence\":0.91}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if got.Intent != localAIIntentSmallTalk || got.Confidence != 0.91 {
		t.Fatalf("unexpected classification: %+v", got)
	}
	if _, err := parseLocalAIClassification("not json"); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func TestParseLocalAIReviewResultExtractsJSON(t *testing.T) {
	got, err := parseLocalAIReviewResult("```json\n{\"result\":\"fail\",\"summary\":\"needs work\",\"findings\":[{\"severity\":\"high\",\"text\":\"unchecked error\",\"evidence\":\"main.go:10\"}]}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if !got.valid() || !got.blocksReviewer() {
		t.Fatalf("expected blocking valid result: %+v", got)
	}
	findings := got.normalizedFindings()
	if len(findings) != 1 || findings[0].Severity != "high" || findings[0].Text != "unchecked error" || findings[0].Evidence != "main.go:10" {
		t.Fatalf("unexpected findings: %+v", findings)
	}
	if _, err := parseLocalAIReviewResult("nope"); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func TestLocalAIReviewResultIgnoresNoOpFindings(t *testing.T) {
	result := localAIReviewResult{
		Result: "fail",
		Findings: []localAIReviewFinding{
			{Text: ""},
			{Text: "None"},
			{Text: "No concrete issues remain."},
			{Text: "No issues remained after the latest changes.", Evidence: "Tests passed."},
		},
	}
	if !result.valid() {
		t.Fatalf("no-op findings should still be a valid local review result: %+v", result)
	}
	if result.blocksReviewer() {
		t.Fatalf("no-op findings should not block reviewer: %+v", result.normalizedFindings())
	}
	if findings := result.normalizedFindings(); len(findings) != 0 {
		t.Fatalf("expected no normalized findings, got %+v", findings)
	}
}

func TestLocalAIReviewResultPreservesRealFindings(t *testing.T) {
	result := localAIReviewResult{
		Result: "fail",
		Findings: []localAIReviewFinding{
			{Text: "None"},
			{Text: "unchecked error still returns nil", Evidence: "internal/app/app.go:42"},
		},
	}
	if !result.valid() || !result.blocksReviewer() {
		t.Fatalf("real finding should remain blocking: %+v", result)
	}
	findings := result.normalizedFindings()
	if len(findings) != 1 || findings[0].Severity != "medium" || findings[0].Text != "unchecked error still returns nil" {
		t.Fatalf("unexpected normalized findings: %+v", findings)
	}
}

func TestParseLocalAICoordinatorAdvisoryExtractsJSON(t *testing.T) {
	got, err := parseLocalAICoordinatorAdvisory("```json\n{\"summary\":\"inspect scope\",\"notes\":[\"keep roles intact\"],\"questions\":[\"Need more context?\"]}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if !got.valid() {
		t.Fatalf("expected valid advisory: %+v", got)
	}
	normalized := got.normalized()
	if normalized.Summary != "inspect scope" || len(normalized.Notes) != 1 || normalized.Notes[0] != "keep roles intact" || len(normalized.Questions) != 1 {
		t.Fatalf("unexpected advisory: %+v", normalized)
	}
	if _, err := parseLocalAICoordinatorAdvisory("not json"); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func TestLocalAICoordinatorAdvisoryRejectsRoutingText(t *testing.T) {
	for _, text := range []string{
		"```agents-handoff\n{\"role\":\"implementer\"}\n```",
		`{"role": "implementer"}`,
		`{"role": "reviewer"}`,
	} {
		advisory := localAICoordinatorAdvisory{
			Summary: "Use this",
			Notes:   []string{text},
		}
		if advisory.valid() {
			t.Fatalf("routing text must not be valid advisory: %+v", advisory)
		}
	}
}

func TestClassifyCoordinatorIntentWithOpenAICompat(t *testing.T) {
	server := newLocalAITestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Model != "google/gemma-3-4b" {
			t.Fatalf("unexpected model %q", req.Model)
		}
		if len(req.Messages) != 2 || req.Messages[1].Content != "appreciate it" {
			t.Fatalf("unexpected messages: %+v", req.Messages)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"intent\":\"small_talk\",\"confidence\":0.95}"}}]}`))
	}))
	defer server.Close()

	got, err := classifyCoordinatorIntentWithOpenAICompat(agentSettings{
		LocalAIBaseURL: server.URL + "/v1",
		LocalAIModel:   "google/gemma-3-4b",
	}, "appreciate it")
	if err != nil {
		t.Fatal(err)
	}
	if got.Intent != localAIIntentSmallTalk || got.Confidence != 0.95 {
		t.Fatalf("unexpected classification: %+v", got)
	}
}

func TestRunFirstPassReviewWithOpenAICompat(t *testing.T) {
	server := newLocalAITestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Model != "google/gemma-3-4b" || len(req.Messages) != 2 || req.Messages[1].Content != "review context" {
			t.Fatalf("unexpected request: %+v", req)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"result\":\"pass\",\"summary\":\"clean\",\"findings\":[]}"}}]}`))
	}))
	defer server.Close()

	got, err := runFirstPassReviewWithOpenAICompat(agentSettings{
		LocalAIBaseURL: server.URL + "/v1",
		LocalAIModel:   "google/gemma-3-4b",
	}, "review context")
	if err != nil {
		t.Fatal(err)
	}
	if got.Result != "pass" || got.Summary != "clean" || len(got.Findings) != 0 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func newLocalAITestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot bind local HTTP test server on loopback: %v", err)
	}

	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	return server
}

func TestClassifyCoordinatorIntentRequiresConfigAndValidResult(t *testing.T) {
	if _, ok := classifyCoordinatorIntent(agentSettings{}, "hi"); ok {
		t.Fatal("disabled local AI should not classify")
	}
	old := classifyCoordinatorIntentWithLocalAI
	defer func() { classifyCoordinatorIntentWithLocalAI = old }()
	classifyCoordinatorIntentWithLocalAI = func(settings agentSettings, text string) (localAIClassification, error) {
		return localAIClassification{Intent: "surprise", Confidence: 1}, nil
	}
	if _, ok := classifyCoordinatorIntent(agentSettings{
		LocalAIEnabled: true,
		LocalAIBaseURL: "http://127.0.0.1:1234/v1",
		LocalAIModel:   "google/gemma-3-4b",
	}, "hi"); ok {
		t.Fatal("invalid local AI intent should be ignored")
	}
}
