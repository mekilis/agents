package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

type registeredAgent struct {
	Name    string `json:"name"`
	Role    string `json:"role"`
	Harness string `json:"harness,omitempty"`
}

type inboxItem struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	To       string `json:"to"`
	Agent    string `json:"agent,omitempty"`
	From     string `json:"from,omitempty"`
	Text     string `json:"text,omitempty"`
	Consumed bool   `json:"consumed"`
}

func runAgentCommand(args []string, out, errOut io.Writer, e env) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "agents: agent command requires a subcommand")
		return 2
	}
	var err error
	switch args[0] {
	case "add":
		err = addRegisteredAgent(e, args[1:])
	case "list":
		var agents []registeredAgent
		agents, err = readRegisteredAgents(e)
		if err == nil {
			for _, agent := range agents {
				fmt.Fprintf(out, "%s\t%s\t%s\n", agent.Name, agent.Role, agent.Harness)
			}
		}
	case "remove":
		err = removeRegisteredAgent(e, args[1:])
	default:
		fmt.Fprintf(errOut, "agents: unknown agent command: %s\n", args[0])
		return 2
	}
	if err != nil {
		fmt.Fprintf(errOut, "agents: agent %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func addRegisteredAgent(e env, args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf("agent name is required")
	}
	agent := registeredAgent{Name: strings.TrimSpace(args[0])}
	if err := validateAgentName(agent.Name); err != nil {
		return err
	}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--role":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return fmt.Errorf("--role requires a role")
			}
			agent.Role = strings.TrimSpace(args[i+1])
			i++
		case "--harness":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return fmt.Errorf("--harness requires a label")
			}
			agent.Harness = strings.TrimSpace(args[i+1])
			i++
		default:
			return fmt.Errorf("unknown flag %s", args[i])
		}
	}
	if agent.Role == "" {
		return fmt.Errorf("--role is required")
	}
	agents, err := readRegisteredAgents(e)
	if err != nil {
		return err
	}
	replaced := false
	for i := range agents {
		if agents[i].Name == agent.Name {
			agents[i] = agent
			replaced = true
			break
		}
	}
	if !replaced {
		agents = append(agents, agent)
	}
	return writeRegisteredAgents(e, agents)
}

func removeRegisteredAgent(e env, args []string) error {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf("agent name is required")
	}
	name := strings.TrimSpace(args[0])
	agents, err := readRegisteredAgents(e)
	if err != nil {
		return err
	}
	filtered := agents[:0]
	for _, agent := range agents {
		if agent.Name != name {
			filtered = append(filtered, agent)
		}
	}
	return writeRegisteredAgents(e, filtered)
}

func readRegisteredAgents(e env) ([]registeredAgent, error) {
	path, err := activeTaskFile(e, "agents.json")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var agents []registeredAgent
	if err := json.Unmarshal(data, &agents); err != nil {
		return nil, err
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	return agents, nil
}

func writeRegisteredAgents(e env, agents []registeredAgent) error {
	path, err := activeTaskFile(e, "agents.json")
	if err != nil {
		return err
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	data, err := json.MarshalIndent(agents, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func runInboxCommand(args []string, out, errOut io.Writer, e env) int {
	opts, err := parseInboxArgs(args)
	if err != nil {
		fmt.Fprintf(errOut, "agents: inbox: %v\n", err)
		return 2
	}
	if opts.As == "" {
		fmt.Fprintln(errOut, "agents: inbox: --as is required")
		return 2
	}
	if opts.Consume {
		items, err := consumeInboxItems(e, opts.As, opts.ConsumeID)
		if err != nil {
			fmt.Fprintf(errOut, "agents: inbox: %v\n", err)
			return 1
		}
		if opts.JSON {
			_ = writeJSON(out, map[string]any{"agent": opts.As, "items": items, "consumed": len(items)})
		} else {
			for _, item := range items {
				fmt.Fprintf(out, "consumed: %s\n", item.ID)
			}
		}
		return 0
	}
	if opts.Watch {
		return runInboxWatch(out, errOut, e, opts)
	}
	items, err := listInboxItems(e, opts.As)
	if err != nil {
		fmt.Fprintf(errOut, "agents: inbox: %v\n", err)
		return 1
	}
	if opts.JSON {
		_ = writeJSON(out, map[string]any{"agent": opts.As, "items": items})
		return 0
	}
	for _, item := range items {
		if !item.Consumed {
			fmt.Fprintf(out, "%s\tto=%s\tagent=%s\t%s\n", item.ID, item.To, item.Agent, item.Text)
		}
	}
	return 0
}

type inboxOptions struct {
	As        string
	Consume   bool
	ConsumeID string
	Watch     bool
	JSON      bool
}

var inboxWatchSleep = func() {
	time.Sleep(200 * time.Millisecond)
}
var inboxWatchSleepMu sync.Mutex

func sleepInboxWatch() {
	inboxWatchSleepMu.Lock()
	sleep := inboxWatchSleep
	inboxWatchSleepMu.Unlock()
	sleep()
}

func setInboxWatchSleep(sleep func()) func() {
	inboxWatchSleepMu.Lock()
	oldSleep := inboxWatchSleep
	inboxWatchSleep = sleep
	inboxWatchSleepMu.Unlock()
	return func() {
		inboxWatchSleepMu.Lock()
		inboxWatchSleep = oldSleep
		inboxWatchSleepMu.Unlock()
	}
}

func parseInboxArgs(args []string) (inboxOptions, error) {
	var opts inboxOptions
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--as":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return opts, fmt.Errorf("--as requires a name")
			}
			opts.As = strings.TrimSpace(args[i+1])
			i++
		case "--consume":
			opts.Consume = true
			if i+1 < len(args) && strings.TrimSpace(args[i+1]) != "" && !strings.HasPrefix(strings.TrimSpace(args[i+1]), "--") {
				opts.ConsumeID = strings.TrimSpace(args[i+1])
				i++
			}
		case "--watch":
			opts.Watch = true
		case "--json":
			opts.JSON = true
		default:
			return opts, fmt.Errorf("unknown flag %s", args[i])
		}
	}
	return opts, nil
}

func consumeInboxItems(e env, name, id string) ([]inboxItem, error) {
	if strings.TrimSpace(id) != "" {
		item, err := consumeInboxItem(e, name, id)
		if err != nil {
			return nil, err
		}
		return []inboxItem{item}, nil
	}
	items, err := listInboxItems(e, name)
	if err != nil {
		return nil, err
	}
	var consumed []inboxItem
	for _, item := range items {
		if item.Consumed {
			continue
		}
		if err := markInboxConsumed(e, name, item.ID); err != nil {
			return nil, err
		}
		item.Consumed = true
		consumed = append(consumed, item)
	}
	return consumed, nil
}

func runInboxWatch(out, errOut io.Writer, e env, opts inboxOptions) int {
	seen := map[string]bool{}
	items, err := listInboxItems(e, opts.As)
	if err != nil {
		fmt.Fprintf(errOut, "agents: inbox: %v\n", err)
		return 1
	}
	for _, item := range items {
		if item.Consumed {
			continue
		}
		seen[item.ID] = true
		data, _ := json.Marshal(map[string]any{"event": "inbox_item", "agent": opts.As, "item": item})
		fmt.Fprintln(out, string(data))
	}
	for {
		items, err = listInboxItems(e, opts.As)
		if err != nil {
			fmt.Fprintf(errOut, "agents: inbox: %v\n", err)
			return 1
		}
		for _, item := range items {
			if item.Consumed || seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			data, _ := json.Marshal(map[string]any{"event": "inbox_item", "agent": opts.As, "item": item})
			fmt.Fprintln(out, string(data))
		}
		sleepInboxWatch()
	}
}

func validateAgentName(name string) error {
	if name == "" {
		return fmt.Errorf("agent name is required")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("agent name must contain only letters, digits, dot, underscore, or hyphen")
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' {
			continue
		}
		return fmt.Errorf("agent name must contain only letters, digits, dot, underscore, or hyphen")
	}
	return nil
}

func listInboxItems(e env, name string) ([]inboxItem, error) {
	agent, err := registeredAgentByName(e, name)
	if err != nil {
		return nil, err
	}
	active := readActiveTask(e.cwd)
	dir := filepath.Join(e.cwd, stateDir, active.Path, "handoffs")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var items []inboxItem
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		item, err := parseInboxHandoff(filepath.Join(dir, entry.Name()), active.Path, entry.Name())
		if err != nil {
			return nil, err
		}
		if item.Agent != "" {
			if item.Agent != agent.Name {
				continue
			}
		} else if item.To != agent.Role {
			continue
		}
		item.Consumed = inboxConsumed(e, agent.Name, item.ID)
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func parseInboxHandoff(path, activePath, name string) (inboxItem, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return inboxItem{}, err
	}
	item := inboxItem{ID: strings.TrimSuffix(name, ".md"), Path: filepath.Join(activePath, "handoffs", name)}
	lines := strings.Split(string(data), "\n")
	bodyAt := 0
	sawFrom := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if sawFrom && trimmed == "" {
			bodyAt = i + 1
			break
		}
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "to:") {
			item.To = strings.TrimSpace(trimmed[len("To:"):])
		} else if strings.HasPrefix(lower, "agent:") && item.From == "" {
			item.Agent = strings.TrimSpace(trimmed[len("Agent:"):])
		} else if strings.HasPrefix(lower, "from:") {
			item.From = strings.TrimSpace(trimmed[len("From:"):])
			sawFrom = true
		}
	}
	if bodyAt < len(lines) {
		item.Text = strings.TrimSpace(strings.Join(lines[bodyAt:], "\n"))
	}
	return item, nil
}

func consumeInboxItem(e env, name, id string) (inboxItem, error) {
	items, err := listInboxItems(e, name)
	if err != nil {
		return inboxItem{}, err
	}
	for _, item := range items {
		if item.ID == id || filepath.Base(item.Path) == id || item.Path == id {
			if err := markInboxConsumed(e, name, item.ID); err != nil {
				return inboxItem{}, err
			}
			item.Consumed = true
			return item, nil
		}
	}
	return inboxItem{}, fmt.Errorf("inbox item not found: %s", id)
}

func registeredAgentByName(e env, name string) (registeredAgent, error) {
	agents, err := readRegisteredAgents(e)
	if err != nil {
		return registeredAgent{}, err
	}
	for _, agent := range agents {
		if agent.Name == name {
			return agent, nil
		}
	}
	return registeredAgent{}, fmt.Errorf("agent is not registered: %s", name)
}

func inboxConsumed(e env, name, id string) bool {
	path, err := activeTaskFile(e, filepath.Join("inbox", name, id+".consumed"))
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func markInboxConsumed(e env, name, id string) error {
	path, err := activeTaskFile(e, filepath.Join("inbox", name, id+".consumed"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("consumed\n"), 0o644)
}

func activeTaskFile(e env, rel string) (string, error) {
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
	return filepath.Join(taskDir, rel), nil
}

func agentStatusPayload(e env, activePath string) ([]agentJSON, error) {
	agents, err := readRegisteredAgents(e)
	if err != nil {
		return nil, err
	}
	var payload []agentJSON
	for _, agent := range agents {
		items, err := listInboxItems(e, agent.Name)
		if err != nil {
			return nil, err
		}
		entry := agentJSON{Name: agent.Name, Role: agent.Role, Harness: agent.Harness}
		for _, item := range items {
			if !item.Consumed {
				entry.Unconsumed++
			}
		}
		payload = append(payload, entry)
	}
	_ = activePath
	return payload, nil
}
