package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type localAIIntent string

const (
	localAIIntentSmallTalk        localAIIntent = "small_talk"
	localAIIntentPlanning         localAIIntent = "planning"
	localAIIntentRepoQuestion     localAIIntent = "repo_question"
	localAIIntentImplementRequest localAIIntent = "implement_request"
	localAIIntentReviewRequest    localAIIntent = "review_request"
	localAIIntentUnclear          localAIIntent = "unclear"
)

type localAIClassification struct {
	Intent     localAIIntent `json:"intent"`
	Confidence float64       `json:"confidence"`
}

type localAIReviewResult struct {
	Result   string                 `json:"result"`
	Summary  string                 `json:"summary"`
	Findings []localAIReviewFinding `json:"findings"`
}

type localAIReviewFinding struct {
	Severity string `json:"severity"`
	Text     string `json:"text"`
	Evidence string `json:"evidence"`
}

type localAICoordinatorAdvisory struct {
	Summary   string   `json:"summary"`
	Notes     []string `json:"notes"`
	Questions []string `json:"questions"`
}

var classifyCoordinatorIntentWithLocalAI = classifyCoordinatorIntentWithOpenAICompat
var adviseCoordinatorWithLocalAI = adviseCoordinatorWithOpenAICompat
var runFirstPassReviewWithLocalAI = runFirstPassReviewWithOpenAICompat

func localAIConfigured(settings agentSettings) bool {
	return settings.LocalAIEnabled &&
		strings.TrimSpace(settings.LocalAIBaseURL) != "" &&
		strings.TrimSpace(settings.LocalAIModel) != ""
}

func classifyCoordinatorIntent(settings agentSettings, text string) (localAIClassification, bool) {
	if !localAIConfigured(settings) {
		return localAIClassification{}, false
	}
	classification, err := classifyCoordinatorIntentWithLocalAI(settings, text)
	if err != nil || !classification.valid() {
		return localAIClassification{}, false
	}
	return classification, true
}

func (c localAIClassification) valid() bool {
	switch c.Intent {
	case localAIIntentSmallTalk, localAIIntentPlanning, localAIIntentRepoQuestion, localAIIntentImplementRequest, localAIIntentReviewRequest, localAIIntentUnclear:
	default:
		return false
	}
	return c.Confidence >= 0 && c.Confidence <= 1
}

func classifyCoordinatorIntentWithOpenAICompat(settings agentSettings, text string) (localAIClassification, error) {
	content, err := localAIChat(settings,
		"Classify the user's latest coordinator message. Return only JSON with intent and confidence. "+
			"Allowed intents: small_talk, planning, repo_question, implement_request, review_request, unclear.",
		strings.TrimSpace(text),
	)
	if err != nil {
		return localAIClassification{}, err
	}
	return parseLocalAIClassification(content)
}

func buildLocalAICoordinatorAdvisory(settings agentSettings, input string) (localAICoordinatorAdvisory, bool) {
	if !localAIConfigured(settings) {
		return localAICoordinatorAdvisory{}, false
	}
	advisory, err := adviseCoordinatorWithLocalAI(settings, input)
	if err != nil || !advisory.valid() {
		return localAICoordinatorAdvisory{}, false
	}
	return advisory.normalized(), true
}

func adviseCoordinatorWithOpenAICompat(settings agentSettings, input string) (localAICoordinatorAdvisory, error) {
	content, err := localAIChat(settings,
		"You are an advisory preflight for an Agents coordinator. You do not route work, select roles, create handoffs, or approve final decisions. "+
			"Return only JSON: {\"summary\":\"...\",\"notes\":[\"...\"],\"questions\":[\"...\"]}. "+
			"Keep the advice brief and grounded in the provided task context.",
		strings.TrimSpace(input),
	)
	if err != nil {
		return localAICoordinatorAdvisory{}, err
	}
	return parseLocalAICoordinatorAdvisory(content)
}

func localAIFirstPassReviewEnabled(settings agentSettings) bool {
	return localAIConfigured(settings) && settings.LocalAIReviewFirstPass
}

func runLocalAIFirstPassReview(e env, state loopState, settings agentSettings, options roleRunOptions) (bool, roleRunResult, error) {
	if state.NextRole != "reviewer" || !localAIFirstPassReviewEnabled(settings) {
		return false, roleRunResult{}, nil
	}
	live := options.LiveOutput
	if live == nil {
		live = io.Discard
	}
	agentDesc := localAIAgentDescriptor(settings)
	review, err := runFirstPassReviewWithLocalAI(settings, localAIReviewInput(e, state))
	if err != nil || !review.valid() {
		msg := "Local AI first-pass unavailable; continuing to configured reviewer.\n"
		if err != nil {
			msg = fmt.Sprintf("Local AI first-pass unavailable (%s); continuing to configured reviewer.\n", err)
		}
		fmt.Fprint(live, msg)
		return false, roleRunResult{}, nil
	}
	if review.blocksReviewer() {
		result := roleRunResult{
			Role:         state.NextRole,
			Agent:        "local-ai",
			AgentDesc:    agentDesc,
			Before:       state,
			AgentSkipped: true,
		}
		var output strings.Builder
		fmt.Fprintf(&output, "Local AI first-pass found %d finding(s); skipping configured reviewer until they are fixed.\n", len(review.Findings))
		if summary := strings.TrimSpace(review.Summary); summary != "" {
			fmt.Fprintf(&output, "%s\n", summary)
		}
		localEnv := e
		localEnv.actor = "local-ai"
		localEnv.agentDesc = agentDesc
		for _, finding := range review.normalizedFindings() {
			text := "Local AI first-pass: " + finding.Text
			if evidence := strings.TrimSpace(finding.Evidence); evidence != "" {
				text += "\nEvidence: " + evidence
			}
			if _, err := addFinding(localEnv, []string{text, "--severity", finding.Severity, "--by", "local-ai"}, time.Now().UTC()); err != nil {
				return true, result, err
			}
		}
		after, err := analyzeLoopState(e)
		if err != nil {
			return true, result, err
		}
		result.After = after
		result.AgentOutput = output.String()
		fmt.Fprint(live, result.AgentOutput)
		return true, result, nil
	}
	msg := "Local AI first-pass found no blocking findings; continuing to configured reviewer.\n"
	if strings.EqualFold(strings.TrimSpace(review.Result), "escalate") {
		msg = "Local AI first-pass requested escalation; continuing to configured reviewer.\n"
	}
	fmt.Fprint(live, msg)
	return false, roleRunResult{}, nil
}

func runFirstPassReviewWithOpenAICompat(settings agentSettings, input string) (localAIReviewResult, error) {
	content, err := localAIChat(settings,
		"You are the first-pass reviewer in an Agents workflow. Review only the provided task context. "+
			"Return only JSON: {\"result\":\"pass|fail|escalate\",\"summary\":\"...\",\"findings\":[{\"severity\":\"low|medium|high\",\"text\":\"...\",\"evidence\":\"...\"}]}. "+
			"Use fail only for concrete, grounded issues. Use pass when there are no blocking issues. Use escalate when the context is insufficient or uncertain.",
		strings.TrimSpace(input),
	)
	if err != nil {
		return localAIReviewResult{}, err
	}
	return parseLocalAIReviewResult(content)
}

func localAIReviewInput(e env, state loopState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task: %s\n", state.Task.Title)
	fmt.Fprintf(&b, "Next action: %s\n", state.NextAction)
	if handoff, err := latestHandoffTextToRole(e.cwd, state.Task.Path, "reviewer"); err == nil && strings.TrimSpace(handoff) != "" {
		fmt.Fprintf(&b, "\nLatest reviewer handoff:\n%s\n", strings.TrimSpace(handoff))
	}
	if stat := gitDiffStat(e.cwd); strings.TrimSpace(stat) != "" {
		fmt.Fprintf(&b, "\nGit diff stat:\n%s\n", stat)
	}
	return b.String()
}

func localAIChat(settings agentSettings, systemPrompt, userPrompt string) (string, error) {
	timeout := time.Duration(settings.LocalAITimeout) * time.Millisecond
	if timeout <= 0 {
		timeout = 800 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	baseURL := strings.TrimRight(strings.TrimSpace(settings.LocalAIBaseURL), "/")
	reqBody := map[string]any{
		"model":       strings.TrimSpace(settings.LocalAIModel),
		"temperature": 0,
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": systemPrompt,
			},
			{"role": "user", "content": userPrompt},
		},
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("local AI status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &completion); err != nil {
		return "", err
	}
	if len(completion.Choices) == 0 {
		return "", fmt.Errorf("local AI returned no choices")
	}
	return completion.Choices[0].Message.Content, nil
}

func parseLocalAIClassification(text string) (localAIClassification, error) {
	text = strings.TrimSpace(text)
	if start := strings.IndexByte(text, '{'); start >= 0 {
		text = text[start:]
	}
	if end := strings.LastIndexByte(text, '}'); end >= 0 {
		text = text[:end+1]
	}
	var classification localAIClassification
	if err := json.Unmarshal([]byte(text), &classification); err != nil {
		return localAIClassification{}, err
	}
	return classification, nil
}

func parseLocalAICoordinatorAdvisory(text string) (localAICoordinatorAdvisory, error) {
	text = strings.TrimSpace(text)
	if start := strings.IndexByte(text, '{'); start >= 0 {
		text = text[start:]
	}
	if end := strings.LastIndexByte(text, '}'); end >= 0 {
		text = text[:end+1]
	}
	var advisory localAICoordinatorAdvisory
	if err := json.Unmarshal([]byte(text), &advisory); err != nil {
		return localAICoordinatorAdvisory{}, err
	}
	return advisory, nil
}

func parseLocalAIReviewResult(text string) (localAIReviewResult, error) {
	text = strings.TrimSpace(text)
	if start := strings.IndexByte(text, '{'); start >= 0 {
		text = text[start:]
	}
	if end := strings.LastIndexByte(text, '}'); end >= 0 {
		text = text[:end+1]
	}
	var result localAIReviewResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return localAIReviewResult{}, err
	}
	return result, nil
}

func (a localAICoordinatorAdvisory) valid() bool {
	normalized := a.normalized()
	if normalized.Summary == "" && len(normalized.Notes) == 0 && len(normalized.Questions) == 0 {
		return false
	}
	for _, text := range append(append([]string{normalized.Summary}, normalized.Notes...), normalized.Questions...) {
		if localAIAdvisoryContainsRouting(text) {
			return false
		}
	}
	return true
}

func (a localAICoordinatorAdvisory) normalized() localAICoordinatorAdvisory {
	return localAICoordinatorAdvisory{
		Summary:   strings.TrimSpace(a.Summary),
		Notes:     normalizedAdvisoryStrings(a.Notes),
		Questions: normalizedAdvisoryStrings(a.Questions),
	}
}

func normalizedAdvisoryStrings(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	return normalized
}

func localAIAdvisoryContainsRouting(text string) bool {
	normalized := strings.ToLower(text)
	for _, marker := range []string{"agents-handoff", "to: implementer", "to: reviewer", "\"role\":\"implementer\"", "\"role\":\"reviewer\""} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	compact := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, normalized)
	for _, marker := range []string{"\"role\":\"implementer\"", "\"role\":\"reviewer\""} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

func (r localAIReviewResult) valid() bool {
	switch strings.ToLower(strings.TrimSpace(r.Result)) {
	case "pass", "fail", "escalate":
	default:
		return false
	}
	for _, finding := range r.Findings {
		if strings.TrimSpace(finding.Text) == "" {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(finding.Severity)) {
		case "", "low", "medium", "high":
		default:
			return false
		}
	}
	return true
}

func (r localAIReviewResult) blocksReviewer() bool {
	return strings.EqualFold(strings.TrimSpace(r.Result), "fail") || len(r.Findings) > 0
}

func (r localAIReviewResult) normalizedFindings() []localAIReviewFinding {
	findings := make([]localAIReviewFinding, 0, len(r.Findings))
	for _, finding := range r.Findings {
		severity := strings.ToLower(strings.TrimSpace(finding.Severity))
		if severity == "" {
			severity = "medium"
		}
		findings = append(findings, localAIReviewFinding{
			Severity: severity,
			Text:     strings.TrimSpace(finding.Text),
			Evidence: strings.TrimSpace(finding.Evidence),
		})
	}
	return findings
}

func localAIAgentDescriptor(settings agentSettings) string {
	provider := strings.TrimSpace(settings.LocalAIProvider)
	if provider == "" {
		provider = "local-ai"
	}
	return fmt.Sprintf("%s (model %s)", provider, settingOrDefault(settings.LocalAIModel))
}
