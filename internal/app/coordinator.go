package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func buildCoordinatorPrompt(e env, state loopState) (string, error) {
	latestText, _ := latestCoordinatorHandoffText(e.cwd, state.Task.Path)
	config, err := readConfig(e.cwd)
	if err != nil {
		return "", err
	}
	compact := coordinatorCompactPromptEligible(config.Settings, latestText)

	var b strings.Builder
	fmt.Fprintf(&b, "You are the coordinator for this Agents task.\n\n")
	fmt.Fprintf(&b, "Task: %s\nTask record path: .agents/%s\nNext action: %s\n\n", state.Task.Title, state.Task.Path, state.NextAction)
	appendPromptFile(&b, e.cwd, state.Task.Path, "scope.md", "Scope")
	appendPromptFile(&b, e.cwd, state.Task.Path, "status.md", "Status")
	appendPromptFile(&b, e.cwd, state.Task.Path, "next.md", "Next")
	appendPromptFile(&b, e.cwd, state.Task.Path, "plan.md", "Plan")
	if compact {
		if strings.TrimSpace(latestText) != "" {
			fmt.Fprintf(&b, "## Latest user message\n\n%s\n\n", strings.TrimSpace(latestText))
		}
	} else {
		appendPromptDir(&b, e.cwd, state.Task.Path, "handoffs", "Handoffs")
		appendPromptDir(&b, e.cwd, state.Task.Path, "decisions", "Decisions")
	}
	if advisory, ok := buildLocalAICoordinatorAdvisory(config.Settings, localAICoordinatorInput(e, state, latestText)); ok {
		appendLocalAICoordinatorAdvisory(&b, advisory)
	}
	appendCoordinatorWorkingAgreement(&b)
	if compact {
		fmt.Fprintln(&b, "- This is a small-talk turn. Reply in one short sentence or question, and do not inspect files or summarize the project.")
	}
	return b.String(), nil
}

func appendCoordinatorWorkingAgreement(b *strings.Builder) {
	fmt.Fprintln(b, "Working agreement:")
	fmt.Fprintln(b, "- Treat user handoffs to coordinator as conversational planning context.")
	fmt.Fprintln(b, "- Emit brief JSON progress updates for the interactive UI only when they describe actual work you are doing right now; do not use canned or example status text.")
	fmt.Fprintln(b, "- For greetings, thanks, or other small talk, answer briefly and ask what the user wants to do; do not inspect the repository, summarize files, recommend code work, or emit progress.")
	fmt.Fprintln(b, "- If the user is asking a question, comparing options, or says they are not ready to change code, answer and stop without an `agents-handoff` block.")
	fmt.Fprintln(b, "- Inspect the repository only when the user explicitly asks for project understanding, planning around a concrete goal, review routing, implementation scoping, or code investigation.")
	fmt.Fprintln(b, "- When inspection is warranted, use read-only commands such as listing files, reading docs, and searching source; do not edit files, run tests, perform verification, or do full reviewer work.")
	fmt.Fprintln(b, "- In user-facing replies, refer to project files with repo-relative paths such as `README.md` or `.agents/tasks/...`; do not print absolute machine-local paths.")
	fmt.Fprintln(b, "- Task record paths shown as `.agents/tasks/...` are inside the Agents state directory, not repository source paths.")
	fmt.Fprintln(b, "- If the user wants implementation, emit one `agents-handoff` block to `implementer` with `ready: true` and a specific request.")
	fmt.Fprintln(b, "- If the user wants review, emit one `agents-handoff` block to `reviewer` with `ready: true` and a specific review request.")
	fmt.Fprintln(b, "- Handoffs to implementer or reviewer must include `ready: true`; without it, Agents treats the response as planning-only.")
	fmt.Fprintln(b, "- Prefer telling the user to use `/run`, `/review`, or `/implement` for explicit action instead of inferring from casual chat.")
	fmt.Fprintln(b, "- If evidence is limited, answer with what you can infer and name the missing context briefly.")
	fmt.Fprintln(b, "- Ask concise follow-up questions when the task, desired behavior, ref, constraints, or verification are unclear.")
	fmt.Fprintln(b, "- Record decisions when the plan becomes concrete.")
	fmt.Fprintln(b, "- Do not run `agents` commands yourself; the parent Agents process records coordination state.")
	fmt.Fprintln(b, "- When work is explicitly ready for another role, end your response with one `agents-handoff` fenced block containing `role`, `ready: true`, and a specific `text` request.")
	fmt.Fprintln(b, "- Never copy placeholder handoff text; if you are still discussing, do not emit a handoff block.")
	fmt.Fprintln(b, "- Do not edit implementation code or coordination files directly.")
}

func localAICoordinatorInput(e env, state loopState, latestText string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task: %s\n", state.Task.Title)
	fmt.Fprintf(&b, "Next action: %s\n", state.NextAction)
	if text := strings.TrimSpace(latestText); text != "" {
		fmt.Fprintf(&b, "\nLatest coordinator handoff:\n%s\n", text)
	}
	for _, file := range []struct {
		name  string
		title string
	}{
		{name: "scope.md", title: "Scope"},
		{name: "status.md", title: "Status"},
		{name: "next.md", title: "Next"},
		{name: "plan.md", title: "Plan"},
	} {
		data, err := os.ReadFile(filepath.Join(e.cwd, stateDir, state.Task.Path, file.name))
		if err != nil || strings.TrimSpace(string(data)) == "" {
			continue
		}
		fmt.Fprintf(&b, "\n%s:\n%s\n", file.title, strings.TrimSpace(string(data)))
	}
	return b.String()
}

func appendLocalAICoordinatorAdvisory(b *strings.Builder, advisory localAICoordinatorAdvisory) {
	fmt.Fprintln(b, "## Local AI coordinator advisory")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Advisory context only. Preserve the configured coordinator role and make all routing decisions yourself.")
	if advisory.Summary != "" {
		fmt.Fprintf(b, "Summary: %s\n", advisory.Summary)
	}
	if len(advisory.Notes) > 0 {
		fmt.Fprintln(b, "Notes:")
		for _, note := range advisory.Notes {
			fmt.Fprintf(b, "- %s\n", note)
		}
	}
	if len(advisory.Questions) > 0 {
		fmt.Fprintln(b, "Questions:")
		for _, question := range advisory.Questions {
			fmt.Fprintf(b, "- %s\n", question)
		}
	}
	fmt.Fprintln(b)
}

func latestCoordinatorHandoffText(cwd, taskPath string) (string, error) {
	return latestHandoffTextToRole(cwd, taskPath, "coordinator")
}

func latestHandoffTextToRole(cwd, taskPath, role string) (string, error) {
	dir := filepath.Join(cwd, stateDir, taskPath, "handoffs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return "", err
		}
		if !handoffToRole(string(data), role) {
			continue
		}
		return handoffBodyText(string(data)), nil
	}
	return "", nil
}

func handoffToRole(markdown, role string) bool {
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "to:") {
			return strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(trimmed, "To:")), role)
		}
	}
	return false
}

func handoffBodyText(markdown string) string {
	var lines []string
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		switch {
		case trimmed == "":
			continue
		case strings.HasPrefix(trimmed, "#"):
			continue
		case strings.HasPrefix(lower, "to:"):
			continue
		case strings.HasPrefix(lower, "from:"):
			continue
		case strings.HasPrefix(lower, "agent:"):
			continue
		default:
			lines = append(lines, trimmed)
		}
	}
	return strings.Join(lines, "\n")
}

func coordinatorCompactPromptEligible(settings agentSettings, text string) bool {
	if coordinatorSmallTalk(text) {
		return true
	}
	if coordinatorActionRequest(text) {
		return false
	}
	classification, ok := classifyCoordinatorIntent(settings, text)
	return ok && classification.Intent == localAIIntentSmallTalk && classification.Confidence >= 0.8
}

func coordinatorSmallTalk(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(strings.Trim(text, ".!? \t\r\n")))
	if normalized == "" {
		return false
	}
	smallTalk := map[string]bool{
		"hello":        true,
		"hey":          true,
		"hi":           true,
		"howdy":        true,
		"thanks":       true,
		"thank you":    true,
		"yo":           true,
		"good morning": true,
		"good evening": true,
		"good night":   true,
	}
	if !smallTalk[normalized] {
		return false
	}
	for _, keyword := range []string{"code", "repo", "inspect", "review", "implement", "plan", "fix", "change", "build", "test", "check"} {
		if strings.Contains(normalized, keyword) {
			return false
		}
	}
	return true
}

func coordinatorActionRequest(text string) bool {
	normalized := strings.ToLower(text)
	for _, keyword := range []string{"code", "repo", "inspect", "review", "implement", "plan", "fix", "change", "build", "test", "check"} {
		if strings.Contains(normalized, keyword) {
			return true
		}
	}
	return false
}
