package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCoordinatorPromptCompactsSmallTalk(t *testing.T) {
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Chat"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := addHandoff(e, []string{"--to", "coordinator", "old implementation context that should not be replayed"}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := addHandoff(e, []string{"--to", "coordinator", "hi"}, time.Date(2026, 6, 5, 10, 2, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}

		prompt, err := buildCoordinatorPrompt(e, loopState{
			Task:       task,
			NextRole:   "coordinator",
			NextAction: "answer latest coordinator chat",
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"## Latest user message",
			"hi",
			"This is a small-talk turn",
			"do not inspect files or summarize the project",
		} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("compact coordinator prompt missing %q:\n%s", want, prompt)
			}
		}
		for _, unwanted := range []string{
			"## Handoffs",
			"old implementation context that should not be replayed",
		} {
			if strings.Contains(prompt, unwanted) {
				t.Fatalf("compact coordinator prompt should omit %q:\n%s", unwanted, prompt)
			}
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestCoordinatorPromptKeepsHistoryForConcreteRequests(t *testing.T) {
	code := withTempProcessEnv(t, func(home, cwd string) int {
		e := env{cwd: cwd, home: home, trustPath: filepath.Join(home, ".agents", "trust.json")}
		task, err := openTask(e, []string{"Planning"}, time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := addHandoff(e, []string{"--to", "coordinator", "please inspect the repo and plan the change"}, time.Date(2026, 6, 5, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		prompt, err := buildCoordinatorPrompt(e, loopState{
			Task:       task,
			NextRole:   "coordinator",
			NextAction: "answer latest coordinator chat",
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"## Handoffs", "please inspect the repo and plan the change"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("full coordinator prompt missing %q:\n%s", want, prompt)
			}
		}
		if strings.Contains(prompt, "This is a small-talk turn") {
			t.Fatalf("concrete request should not use small-talk prompt:\n%s", prompt)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("withTempProcessEnv code=%d", code)
	}
}

func TestCoordinatorSmallTalkHeuristic(t *testing.T) {
	for _, text := range []string{"hi", "hello!", "thanks", "good morning"} {
		if !coordinatorSmallTalk(text) {
			t.Fatalf("expected small talk for %q", text)
		}
	}
	for _, text := range []string{"", "hi inspect repo", "check the code", "plan a change"} {
		if coordinatorSmallTalk(text) {
			t.Fatalf("did not expect small talk for %q", text)
		}
	}
}
