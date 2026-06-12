package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	planStateMissing  = "missing"
	planStateDraft    = "draft"
	planStateApproved = "approved"
	planStateStale    = "stale"
)

type taskPlan struct {
	State string
	Text  string
	Path  string
}

func runPlanCommand(args []string, out, errOut io.Writer, e env) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "agents: plan command requires a subcommand")
		return 2
	}
	switch args[0] {
	case "draft", "create":
		relPath, err := draftActiveTaskPlan(e, args[1:])
		if err != nil {
			fmt.Fprintf(errOut, "agents: plan %s: %v\n", args[0], err)
			return 1
		}
		fmt.Fprintf(out, "plan: %s\n", planStateDraft)
		fmt.Fprintf(out, "updated: %s\n", relPath)
		return 0
	case "show":
		if len(args) != 1 {
			fmt.Fprintln(errOut, "agents: plan show does not accept arguments")
			return 2
		}
		plan, err := readActiveTaskPlan(e)
		if err != nil {
			fmt.Fprintf(errOut, "agents: plan show: %v\n", err)
			return 1
		}
		if plan.State == planStateMissing {
			fmt.Fprintln(out, "plan: missing")
			return 0
		}
		fmt.Fprintf(out, "plan: %s\n", plan.State)
		fmt.Fprintf(out, "path: %s\n", plan.Path)
		fmt.Fprintln(out)
		fmt.Fprintln(out, strings.TrimRight(plan.Text, "\n"))
		return 0
	case "revise":
		relPath, err := writeActiveTaskPlan(e, args[1:], planStateDraft)
		if err != nil {
			fmt.Fprintf(errOut, "agents: plan revise: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "plan: %s\n", planStateDraft)
		fmt.Fprintf(out, "updated: %s\n", relPath)
		return 0
	case "approve":
		relPath, err := approveActiveTaskPlan(e)
		if err != nil {
			fmt.Fprintf(errOut, "agents: plan approve: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "plan: %s\n", planStateApproved)
		fmt.Fprintf(out, "updated: %s\n", relPath)
		return 0
	case "clear":
		relPath, err := clearActiveTaskPlan(e)
		if err != nil {
			fmt.Fprintf(errOut, "agents: plan clear: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "cleared: %s\n", relPath)
		return 0
	default:
		fmt.Fprintf(errOut, "agents: unknown plan command: %s\n", args[0])
		return 2
	}
}

func readActiveTaskPlan(e env) (taskPlan, error) {
	active := readActiveTask(e.cwd)
	if active.Path == "" {
		return taskPlan{}, fmt.Errorf("no active task")
	}
	return readTaskPlan(e.cwd, active.Path)
}

func readTaskPlan(cwd, activePath string) (taskPlan, error) {
	path := filepath.Join(cwd, stateDir, activePath, "plan.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return taskPlan{State: planStateMissing}, nil
		}
		return taskPlan{}, err
	}
	state := planStateDraft
	body := planBodyWithoutState(stripFirstMarkdownHeading(string(data)))
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "state:") {
			state = strings.ToLower(strings.TrimSpace(trimmed[len("state:"):]))
			break
		}
	}
	switch state {
	case planStateDraft, planStateApproved, planStateStale:
	default:
		state = planStateDraft
	}
	return taskPlan{
		State: state,
		Text:  strings.TrimSpace(body),
		Path:  filepath.Join(activePath, "plan.md"),
	}, nil
}

func writeActiveTaskPlan(e env, args []string, state string) (string, error) {
	text, err := parseSingleTextArg(args, "plan text")
	if err != nil {
		return "", err
	}
	return writeActiveTaskPlanText(e, text, state)
}

func draftActiveTaskPlan(e env, args []string) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("plan draft accepts at most one objective")
	}
	objective := "State the user-facing outcome in one or two sentences."
	if len(args) == 1 {
		if strings.TrimSpace(args[0]) == "" {
			return "", fmt.Errorf("objective must not be empty")
		}
		objective = strings.TrimSpace(args[0])
	}
	return writeActiveTaskPlanText(e, compactPlanTemplate(objective), planStateDraft)
}

func writeActiveTaskPlanText(e env, text string, state string) (string, error) {
	active := readActiveTask(e.cwd)
	if active.Path == "" {
		return "", fmt.Errorf("no active task")
	}
	taskDir := filepath.Join(e.cwd, stateDir, active.Path)
	if _, err := os.Stat(filepath.Join(taskDir, "task.md")); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("active task not found: %s", active.Path)
		}
		return "", err
	}
	body := fmt.Sprintf("# Plan\n\nState: %s\nUpdated: %s\n\n%s\n", state, time.Now().UTC().Format(time.RFC3339), strings.TrimRight(text, "\n"))
	relPath := filepath.Join(active.Path, "plan.md")
	if err := os.WriteFile(filepath.Join(taskDir, "plan.md"), []byte(body), 0o644); err != nil {
		return "", err
	}
	return relPath, nil
}

func compactPlanTemplate(objective string) string {
	return strings.TrimSpace(fmt.Sprintf(`%s

Write this as concise, editable Markdown rather than filling out a form. Make it specific enough that an implementer and reviewer can act on it without guessing.

Include the substance naturally:
- what outcome the user wants and why this plan is in scope
- repo-relative evidence from docs, code, task state, or prior handoffs
- assumptions, open questions, or decisions still needed
- the implementation shape: files, behavior, and action-sized slices
- validation: focused tests, checks, or acceptance criteria
- risks, trade-offs, and constraints such as permissions, boundaries, or actions Agents must avoid`, strings.TrimSpace(objective)))
}

func approveActiveTaskPlan(e env) (string, error) {
	plan, err := readActiveTaskPlan(e)
	if err != nil {
		return "", err
	}
	if plan.State == planStateMissing {
		return "", fmt.Errorf("no plan to approve")
	}
	return writeActiveTaskPlan(e, []string{plan.Text}, planStateApproved)
}

func clearActiveTaskPlan(e env) (string, error) {
	active := readActiveTask(e.cwd)
	if active.Path == "" {
		return "", fmt.Errorf("no active task")
	}
	relPath := filepath.Join(active.Path, "plan.md")
	err := os.Remove(filepath.Join(e.cwd, stateDir, relPath))
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return relPath, nil
}

func planBodyWithoutState(text string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "state:") || strings.HasPrefix(lower, "updated:") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func planStateForStats(cwd, activePath, latestHandoffName string) (string, error) {
	plan, err := readTaskPlan(cwd, activePath)
	if err != nil {
		return "", err
	}
	if plan.State == planStateApproved && planStale(cwd, activePath, latestHandoffName) {
		return planStateStale, nil
	}
	return plan.State, nil
}

func planStale(cwd, activePath, latestHandoffName string) bool {
	if latestHandoffName == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(cwd, stateDir, activePath, "plan.md"))
	if err != nil {
		return false
	}
	return info.ModTime().UTC().Format("2006-01-02T150405Z") < chunkStamp(latestHandoffName)
}
