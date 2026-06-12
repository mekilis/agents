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

var classifyCoordinatorIntentWithLocalAI = classifyCoordinatorIntentWithOpenAICompat

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
				"role": "system",
				"content": "Classify the user's latest coordinator message. Return only JSON with intent and confidence. " +
					"Allowed intents: small_talk, planning, repo_question, implement_request, review_request, unclear.",
			},
			{"role": "user", "content": strings.TrimSpace(text)},
		},
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return localAIClassification{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return localAIClassification{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return localAIClassification{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return localAIClassification{}, fmt.Errorf("local AI status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return localAIClassification{}, err
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &completion); err != nil {
		return localAIClassification{}, err
	}
	if len(completion.Choices) == 0 {
		return localAIClassification{}, fmt.Errorf("local AI returned no choices")
	}
	return parseLocalAIClassification(completion.Choices[0].Message.Content)
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
