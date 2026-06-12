package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"charm.land/glamour/v2"
	"golang.org/x/term"
)

const (
	productName         = "Agents"
	stateDir            = ".agents"
	liveSuggestionLimit = 4
)

var errComposerInterrupted = errors.New("composer interrupted")
var errRunInterrupted = errors.New("run interrupted")

var versionInfo = VersionInfo{
	Version: "dev",
	Commit:  "none",
	Date:    "unknown",
}

type VersionInfo struct {
	Version string
	Commit  string
	Date    string
}

func SetVersion(version, commit, date string) {
	versionInfo = VersionInfo{Version: version, Commit: commit, Date: date}
}

type slashCommandSpec struct {
	Name        string
	Usage       string
	Description string
}

var interactiveSlashCommands = []slashCommandSpec{
	{Name: "/start", Usage: `/start "title" [--ref REF] "what to do"`, Description: "Fast path: open a task and hand off to implementer"},
	{Name: "/status", Usage: "/status", Description: "Show workspace, active task, and record counts"},
	{Name: "/trust", Usage: "/trust", Description: "Trust this workspace"},
	{Name: "/init", Usage: "/init [--share|--local]", Description: "Initialize Agents task state"},
	{Name: "/task", Usage: `/task open|use|scope|status|next ...`, Description: "Open, switch, or update the active task"},
	{Name: "/decision", Usage: `/decision add "text"`, Description: "Record a planning decision"},
	{Name: "/handoff", Usage: `/handoff --to ROLE "text"`, Description: "Route context to another role"},
	{Name: "/finding", Usage: "/finding add|fix|verify ...", Description: "Track review findings"},
	{Name: "/verify", Usage: `/verify "text" [--cmd 'shell command']`, Description: "Record verification evidence"},
	{Name: "/dispatch", Usage: "/dispatch <role>", Description: "Print a role packet"},
	{Name: "/run", Usage: "/run", Description: "Run queued roles until a stop condition"},
	{Name: "/archive", Usage: "/archive", Description: "Archive the active task"},
	{Name: "/help", Usage: "/help", Description: "Show this command menu"},
	{Name: "/quit", Usage: "/quit", Description: "Exit the interactive terminal"},
	{Name: "/exit", Usage: "/exit", Description: "Exit the interactive terminal"},
}

type env struct {
	cwd           string
	home          string
	isTTY         bool
	disableBanner bool
	trustPath     string
	actor         string
	// agentDesc qualifies the actor with the concrete agent and model behind
	// it (from AGENTS_AGENT), e.g. "codex (model gpt-5.5, reasoning high)".
	// Provider names like cursor or codex are tools, not models; records
	// should say what actually ran.
	agentDesc string
}

type config struct {
	Roles    map[string]roleConfig
	Settings agentSettings
}

// roleConfig holds who plays a role. Provider is a built-in agent name
// (cursor, cursor-agent, agent, codex, claude); Cmd is an advanced escape
// hatch containing {prompt_file}. Model and Reasoning override the global
// provider settings for this role only.
type roleConfig struct {
	Provider  string
	Model     string
	Reasoning string
	Cmd       string
}

func (rc roleConfig) agentLabel() string {
	if strings.TrimSpace(rc.Cmd) != "" {
		return strings.TrimSpace(rc.Cmd)
	}
	return strings.TrimSpace(rc.Provider)
}

func (rc roleConfig) configured() bool {
	return rc.agentLabel() != ""
}

type agentSettings struct {
	Model          string
	Reasoning      string
	CodexModel     string
	CodexReasoning string
	ClaudeModel    string
	ClaudeEffort   string
	CursorModel    string
}

const promptFileMarker = "{prompt_file}"

// Run is the CLI entry point for both scriptable commands and the interactive
// terminal.
func Run(args []string, in io.Reader, out, errOut io.Writer) int {
	e, err := currentEnv(in)
	if err != nil {
		fmt.Fprintf(errOut, "agents: %v\n", err)
		return 1
	}

	// Resolve the workspace root by walking up to the nearest .agents
	// workspace so commands work from subdirectories. init and trust always
	// operate on the literal cwd (creating or trusting a new workspace).
	if len(args) == 0 || (args[0] != "init" && args[0] != "trust") {
		e.cwd = findWorkspaceRoot(e.cwd)
	}

	if len(args) > 0 {
		return runOneShot(args, in, out, errOut, e)
	}
	return runInteractive(in, out, errOut, e)
}

// findWorkspaceRoot walks up from cwd to the nearest directory containing an
// agents workspace. A bare .agents directory is not enough: the user-level
// trust store also lives in ~/.agents, so a workspace must carry config.yml,
// an active pointer, or a tasks directory. Returns cwd when nothing is found.
func findWorkspaceRoot(cwd string) string {
	dir := cwd
	for {
		if isWorkspaceDir(filepath.Join(dir, stateDir)) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd
		}
		dir = parent
	}
}

func isWorkspaceDir(statePath string) bool {
	info, err := os.Stat(statePath)
	if err != nil || !info.IsDir() {
		return false
	}
	for _, marker := range []string{"config.yml", "active", "tasks"} {
		if _, err := os.Stat(filepath.Join(statePath, marker)); err == nil {
			return true
		}
	}
	return false
}

func runOneShot(args []string, in io.Reader, out, errOut io.Writer, e env) int {
	switch args[0] {
	case "-h", "--help", "help":
		printHelp(out)
		return 0
	case "version":
		printVersion(out)
		return 0
	case "completion":
		return runCompletionCommand(args[1:], out, errOut, e)
	case "status":
		jsonOut, err := parseJSONFlag(args[1:])
		if err != nil {
			fmt.Fprintf(errOut, "agents: status: %v\n", err)
			return 2
		}
		if err := printStatus(out, e, jsonOut); err != nil {
			fmt.Fprintf(errOut, "agents: read status: %v\n", err)
			return 1
		}
		return 0
	case "trust":
		return runTrustCommand(args[1:], out, errOut, e)
	case "init":
		return runInitCommand(args[1:], out, errOut, e)
	case "start":
		return runStartCommand(args[1:], out, errOut, e)
	case "go":
		return runGoCommand(args[1:], in, out, errOut, e)
	case "task":
		return runTaskCommand(args[1:], out, errOut, e)
	case "decision":
		return runDecisionCommand(args[1:], out, errOut, e)
	case "handoff":
		return runHandoffCommand(args[1:], out, errOut, e)
	case "finding":
		return runFindingCommand(args[1:], out, errOut, e)
	case "verify":
		return runVerifyCommand(args[1:], out, errOut, e)
	case "archive":
		return runArchiveCommand(args[1:], out, errOut, e)
	case "dispatch":
		return runDispatchCommand(args[1:], out, errOut, e)
	case "run":
		return runLoopCommand(args[1:], out, errOut, e)
	default:
		fmt.Fprintf(errOut, "agents: unknown command: %s\n", strings.Join(args, " "))
		fmt.Fprintln(errOut, "Run `agents help` for usage.")
		return 2
	}
}

func runInteractive(in io.Reader, out, errOut io.Writer, e env) int {
	reader := bufio.NewReader(in)

	if e.isTTY && !e.disableBanner {
		printBanner(out)
		fmt.Fprintln(out)
	}

	trusted, err := isTrusted(e)
	if err != nil {
		fmt.Fprintf(errOut, "agents: read trust store: %v\n", err)
		return 1
	}
	if !trusted {
		if !e.isTTY {
			fmt.Fprintf(errOut, "agents: %s is not trusted; run `agents trust` or open `agents` in an interactive terminal to trust this folder\n", e.cwd)
			return 1
		}
		ok, err := promptTrust(reader, out, e.cwd)
		if err != nil {
			fmt.Fprintf(errOut, "agents: read trust prompt: %v\n", err)
			return 1
		}
		if !ok {
			fmt.Fprintln(out, "Not trusted. No changes made.")
			return 0
		}
		if _, err := trustWorkspace(e); err != nil {
			fmt.Fprintf(errOut, "agents: write trust store: %v\n", err)
			return 1
		}
		fmt.Fprintln(out, "Trusted this folder.")
		fmt.Fprintln(out)
	}

	return runTerminalLoop(in, reader, out, errOut, e)
}

func currentEnv(in io.Reader) (env, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return env{}, err
	}
	home := strings.TrimSpace(os.Getenv("AGENTS_HOME"))
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return env{}, err
		}
	}
	trustPath := filepath.Join(home, ".agents", "trust.json")
	return env{
		cwd:           cwd,
		home:          home,
		isTTY:         isTerminalReader(in),
		disableBanner: bannerDisabledFromEnv(os.Getenv("AGENTS_NO_BANNER")),
		trustPath:     trustPath,
		actor:         defaultActor(),
		agentDesc:     strings.TrimSpace(os.Getenv("AGENTS_AGENT")),
	}, nil
}

// agentDescriptor qualifies an agent name with the model and reasoning that
// will actually run. Built-in names (cursor, codex, claude) identify tools,
// not models, so durable records and run output should carry both.
func agentDescriptor(agent string, settings agentSettings) string {
	label := strings.TrimSpace(agent)
	if label == "" {
		return ""
	}
	if strings.Contains(label, promptFileMarker) {
		return label + " (custom command)"
	}
	desc := label + " (model " + settingOrDefault(effectiveModelSetting(label, settings))
	if reasoning := strings.TrimSpace(effectiveReasoningSetting(label, settings)); reasoning != "" {
		desc += ", reasoning " + reasoning
	}
	return desc + ")"
}

// defaultActor resolves the actor used for chunk attribution when --by is not
// given: AGENTS_ACTOR first, then the local OS user.
func defaultActor() string {
	if actor := strings.TrimSpace(os.Getenv("AGENTS_ACTOR")); actor != "" {
		return actor
	}
	if user := strings.TrimSpace(os.Getenv("USER")); user != "" {
		return user
	}
	return strings.TrimSpace(os.Getenv("LOGNAME"))
}

func resolveActor(e env, by string) (string, error) {
	if actor := strings.TrimSpace(by); actor != "" {
		return actor, nil
	}
	if e.actor != "" {
		return e.actor, nil
	}
	if actor := defaultActor(); actor != "" {
		return actor, nil
	}
	return "", fmt.Errorf("actor is required: set AGENTS_ACTOR or pass --by")
}

// agentLine renders the optional Agent metadata line that qualifies a record
// with the concrete agent and model behind the actor.
func agentLine(e env) string {
	if e.agentDesc == "" {
		return ""
	}
	return "Agent: " + e.agentDesc + "\n"
}

// extractByFlag removes a --by flag from args and returns its value with the
// remaining arguments.
func extractByFlag(args []string) (by string, rest []string, err error) {
	for i := 0; i < len(args); i++ {
		if strings.TrimSpace(args[i]) != "--by" {
			rest = append(rest, args[i])
			continue
		}
		if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
			return "", nil, fmt.Errorf("--by requires a value")
		}
		by = strings.TrimSpace(args[i+1])
		i++
	}
	return by, rest, nil
}

func bannerDisabledFromEnv(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func isTerminalReader(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func readConfig(cwd string) (config, error) {
	cfg := config{Roles: map[string]roleConfig{}}
	data, err := os.ReadFile(filepath.Join(cwd, stateDir, "config.yml"))
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	inRoles := false
	inSettings := false
	currentRole := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "roles:" {
			inRoles = true
			inSettings = false
			currentRole = ""
			continue
		}
		if trimmedLine == "settings:" {
			inRoles = false
			inSettings = true
			currentRole = ""
			continue
		}
		if inRoles {
			if !strings.HasPrefix(line, "  ") {
				inRoles = false
				currentRole = ""
				continue
			}
			trimmed := strings.TrimSpace(line)
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			value := strings.Trim(strings.TrimSpace(parts[1]), `"`)
			if strings.HasPrefix(line, "    ") {
				// Nested role field, e.g. "    model: gpt-5.5".
				if currentRole == "" {
					continue
				}
				rc := cfg.Roles[currentRole]
				switch key {
				case "provider":
					rc.Provider = value
				case "model":
					rc.Model = value
				case "reasoning":
					rc.Reasoning = value
				case "cmd":
					rc.Cmd = value
				}
				cfg.Roles[currentRole] = rc
				continue
			}
			if key != "implementer" && key != "reviewer" && key != "coordinator" {
				currentRole = ""
				continue
			}
			if value == "" {
				// Map form: fields follow on deeper-indented lines.
				currentRole = key
				if _, ok := cfg.Roles[key]; !ok {
					cfg.Roles[key] = roleConfig{}
				}
				continue
			}
			currentRole = ""
			if strings.Contains(value, promptFileMarker) {
				cfg.Roles[key] = roleConfig{Cmd: value}
			} else {
				cfg.Roles[key] = roleConfig{Provider: value}
			}
			continue
		}
		if inSettings {
			if !strings.HasPrefix(line, "  ") {
				inSettings = false
				continue
			}
			trimmed := strings.TrimSpace(line)
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			value := strings.Trim(strings.TrimSpace(parts[1]), `"`)
			switch key {
			case "model":
				cfg.Settings.Model = value
			case "reasoning":
				cfg.Settings.Reasoning = value
			case "codex_model":
				cfg.Settings.CodexModel = value
			case "codex_reasoning":
				cfg.Settings.CodexReasoning = value
			case "claude_model":
				cfg.Settings.ClaudeModel = value
			case "claude_effort":
				cfg.Settings.ClaudeEffort = value
			case "cursor_model":
				cfg.Settings.CursorModel = value
			}
		}
	}
	return cfg, nil
}

func configuredAgent(cwd, role string) (string, error) {
	cfg, err := readConfig(cwd)
	if err != nil {
		return "", err
	}
	return cfg.Roles[role].agentLabel(), nil
}

// roleSettings overlays a role's model/reasoning overrides onto the global
// provider settings so command construction and run markers see one view.
func roleSettings(rc roleConfig, settings agentSettings) agentSettings {
	effective := settings
	model := strings.TrimSpace(rc.Model)
	reasoning := strings.TrimSpace(rc.Reasoning)
	switch strings.ToLower(strings.TrimSpace(rc.Provider)) {
	case "codex":
		if model != "" {
			effective.CodexModel = model
		}
		if reasoning != "" {
			effective.CodexReasoning = reasoning
		}
	case "claude":
		if model != "" {
			effective.ClaudeModel = model
		}
		if reasoning != "" {
			effective.ClaudeEffort = reasoning
		}
	case "cursor", "cursor-agent", "agent":
		if model != "" {
			effective.CursorModel = model
		}
	default:
		if model != "" {
			effective.Model = model
		}
		if reasoning != "" {
			effective.Reasoning = reasoning
		}
	}
	return effective
}

// resolveRoleCommand resolves the shell command template for a configured
// role, applying per-role model/reasoning overrides.
func resolveRoleCommand(rc roleConfig, settings agentSettings) (command, agent string, effective agentSettings, err error) {
	effective = roleSettings(rc, settings)
	if cmd := strings.TrimSpace(rc.Cmd); cmd != "" {
		if !strings.Contains(cmd, promptFileMarker) {
			return "", "", effective, fmt.Errorf("role cmd must contain %s", promptFileMarker)
		}
		return cmd, cmd, effective, nil
	}
	command, err = resolveAgentCommand(rc.Provider, effective)
	if err != nil {
		return "", "", effective, err
	}
	return command, strings.TrimSpace(rc.Provider), effective, nil
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "Agents is a terminal for coordinating AI coding agents.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  agents          Open the interactive terminal")
	fmt.Fprintln(w, "  agents trust [--json]")
	fmt.Fprintln(w, "  agents init [--share|--local]")
	fmt.Fprintln(w, "  agents go \"what you want\" [--ref REF] [--max-iterations N] [--plain]")
	fmt.Fprintln(w, "  agents start \"title\" [--ref REF] \"what to do\"")
	fmt.Fprintln(w, "  agents status [--json]")
	fmt.Fprintln(w, "  agents task open \"title\" [--ref REF]")
	fmt.Fprintln(w, "  agents task use <task>")
	fmt.Fprintln(w, "  agents task scope|status|next \"text\"")
	fmt.Fprintln(w, "  agents decision add \"text\" [--by ACTOR]")
	fmt.Fprintln(w, "  agents handoff --to ROLE \"text\" [--by ACTOR]")
	fmt.Fprintln(w, "  agents finding add \"text\" [--severity low|medium|high] [--by ACTOR]")
	fmt.Fprintln(w, "  agents finding fix <finding> [--evidence \"text\"] [--by ACTOR]")
	fmt.Fprintln(w, "  agents finding verify <finding> [--evidence \"text\"] [--by ACTOR]")
	fmt.Fprintln(w, "  agents verify \"text\" [--cmd 'shell command'] [--by ACTOR]")
	fmt.Fprintln(w, "  agents archive")
	fmt.Fprintln(w, "  agents dispatch <implementer|reviewer|coordinator> [--json]")
	fmt.Fprintln(w, "  agents run [--max-iterations N] [--once] [--json] [--json-events]")
	fmt.Fprintln(w, "  agents completion zsh")
	fmt.Fprintln(w, "  agents completion install-zsh")
	fmt.Fprintln(w, "  agents version")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Actor identity:")
	fmt.Fprintln(w, "  Records are attributed to --by, then AGENTS_ACTOR, then the OS user.")
	fmt.Fprintln(w, "  A finding cannot be verified by the actor that fixed it.")
}

func printVersion(w io.Writer) {
	fmt.Fprintf(w, "agents %s (%s, %s)\n", versionInfo.Version, versionInfo.Commit, versionInfo.Date)
}

func runCompletionCommand(args []string, out, errOut io.Writer, e env) int {
	if len(args) != 1 {
		fmt.Fprintln(errOut, "agents: completion requires zsh or install-zsh")
		return 2
	}
	switch args[0] {
	case "zsh":
		fmt.Fprint(out, zshCompletionScript())
		return 0
	case "install-zsh":
		path, err := installZshCompletion(e.home)
		if err != nil {
			fmt.Fprintf(errOut, "agents: completion install-zsh: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "installed: %s\n", path)
		fmt.Fprintln(out, "restart zsh or run: exec zsh")
		return 0
	default:
		fmt.Fprintf(errOut, "agents: unknown completion shell: %s\n", args[0])
		return 2
	}
}

func installZshCompletion(home string) (string, error) {
	dir := filepath.Join(home, ".zsh", "completions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "_agents")
	if err := os.WriteFile(path, []byte(zshCompletionScript()), 0o644); err != nil {
		return "", err
	}
	zshrc := filepath.Join(home, ".zshrc")
	block := "\n# Agents shell completion\nfpath=(~/.zsh/completions $fpath)\nautoload -Uz compinit\ncompinit\n"
	data, err := os.ReadFile(zshrc)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if !strings.Contains(string(data), ".zsh/completions") {
		file, err := os.OpenFile(zshrc, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return "", err
		}
		defer file.Close()
		if _, err := file.WriteString(block); err != nil {
			return "", err
		}
	}
	return path, nil
}

func zshCompletionScript() string {
	return `#compdef agents

_agents() {
  local -a commands
  commands=(
    'help:show help'
    'trust:trust the current workspace'
    'init:initialize Agents state'
    'go:open a task and run the loop to a stop condition'
    'start:open a task and hand off to implementer'
    'status:show workspace and task status'
    'task:open or switch tasks'
    'decision:record a decision'
    'handoff:record a role handoff'
    'finding:track review findings'
    'verify:record verification evidence'
    'archive:archive the active task'
    'dispatch:print a role packet'
    'run:run queued roles until a stop condition'
    'completion:generate shell completion'
  )

  local -a roles severities completion_cmds init_flags json_flags task_cmds finding_cmds
  roles=('implementer:implementation role' 'reviewer:review role' 'coordinator:planning role')
  severities=('low:low severity' 'medium:medium severity' 'high:high severity')
  completion_cmds=('zsh:print zsh completion' 'install-zsh:install zsh completion locally')
  init_flags=('--share:commit task state' '--local:ignore .agents in this repo')
  json_flags=('--json:print JSON')
  task_cmds=('open:open a new task' 'use:switch active task' 'scope:set task scope' 'status:set task status' 'next:set next step')
  finding_cmds=('add:add a finding' 'fix:mark finding fixed' 'verify:mark finding verified')

  _arguments -C \
    '1:command:->cmds' \
    '*::arg:->args'

  case $state in
    cmds)
      _describe 'agents command' commands
      ;;
    args)
      case $words[2] in
        completion)
          _describe 'completion command' completion_cmds
          ;;
        init)
          _describe 'init option' init_flags
          ;;
        status|trust)
          _describe 'option' json_flags
          ;;
        run)
          _arguments '--json[print JSON]' '--once[run a single role]' '--max-iterations[role run budget]:count:'
          ;;
        go)
          _arguments '--ref[task reference]:ref:' '--max-iterations[role run budget]:count:' '--plain[plain output instead of the split view]'
          ;;
        verify)
          _arguments '--cmd[evidence command]:command:' '--by[actor]:actor:'
          ;;
        dispatch)
          if (( CURRENT == 3 )); then
            _describe 'role' roles
          else
            _describe 'option' json_flags
          fi
          ;;
        task)
          if (( CURRENT == 3 )); then
            _describe 'task command' task_cmds
          elif [[ $words[3] == open ]]; then
            _arguments '--ref[task reference]:ref:'
          elif [[ $words[3] == use ]]; then
            _files
          fi
          ;;
        finding)
          if (( CURRENT == 3 )); then
            _describe 'finding command' finding_cmds
          elif [[ $words[3] == add ]]; then
            _arguments '--severity[finding severity]:severity:->severity'
            if [[ $state == severity ]]; then
              _describe 'severity' severities
            fi
          else
            _files
          fi
          ;;
        handoff)
          _arguments '--to[target role]:role:->role'
          if [[ $state == role ]]; then
            _describe 'role' roles
          fi
          ;;
        start)
          _arguments '--ref[task reference]:ref:'
          ;;
      esac
      ;;
  esac
}

_agents "$@"
`
}

func printInteractiveHelp(w io.Writer) {
	fmt.Fprintln(w, "Shortcuts:")
	fmt.Fprintln(w, "  Plain text  Chat with the coordinator")
	fmt.Fprintln(w, "  /           Show slash commands")
	fmt.Fprintln(w, "  ?           Show this help")
	fmt.Fprintln(w, "  Tab         Complete a unique slash command")
	fmt.Fprintln(w)
	printSlashCommandSuggestions(w, "", false)
}

func printInteractiveFooter(w io.Writer) {
	fmt.Fprintln(w, `Type plain text to chat with the coordinator.`)
	fmt.Fprintln(w, `Press ? for shortcuts · Type / for commands · Tab completes slash commands`)
}

func printSlashCommandSuggestions(w io.Writer, prefix string, color bool) {
	prefix = strings.TrimSpace(prefix)
	matches := matchingSlashCommands(prefix)
	if len(matches) == 0 {
		fmt.Fprintf(w, "No slash commands match %q.\n", prefix)
		fmt.Fprintln(w, `Type "/" to see commands, or type plain text to chat with the coordinator.`)
		return
	}
	fmt.Fprintln(w, "Slash commands:")
	for _, command := range matches {
		usage := command.Usage
		description := command.Description
		if color {
			usage = "\x1b[36m" + usage + "\x1b[0m"
			description = "\x1b[2m" + description + "\x1b[0m"
		}
		fmt.Fprintf(w, "  %-42s %s\n", usage, description)
	}
}

func matchingSlashCommands(prefix string) []slashCommandSpec {
	if prefix == "" || prefix == "/" {
		return interactiveSlashCommands
	}
	var matches []slashCommandSpec
	for _, command := range interactiveSlashCommands {
		if strings.HasPrefix(command.Name, prefix) {
			matches = append(matches, command)
		}
	}
	return matches
}

func resolveInteractiveSlashCommand(input string, out io.Writer, color bool) (string, bool) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return "", false
	}
	slashHead := fields[0]
	matches := matchingSlashCommands(slashHead)
	switch len(matches) {
	case 0:
		fmt.Fprintf(out, "No slash commands match %q.\n", slashHead)
		fmt.Fprintln(out, `Type "/" to see commands, or type plain text to chat with the coordinator.`)
		return "", false
	case 1:
		resolved := strings.TrimPrefix(matches[0].Name, "/")
		if len(fields) > 1 {
			return resolved + strings.TrimPrefix(input, slashHead), true
		}
		return resolved, true
	default:
		printSlashCommandSuggestions(out, slashHead, color)
		return "", false
	}
}

func printBanner(w io.Writer) {
	fmt.Fprintln(w, `AGENTS`)
	fmt.Fprintln(w, `    _                    _       `)
	fmt.Fprintln(w, `   / \   __ _  ___ _ __ | |_ ___ `)
	fmt.Fprintln(w, `  / _ \ / _' |/ _ \ '_ \| __/ __|`)
	fmt.Fprintln(w, ` / ___ \ (_| |  __/ | | | |_\__ \`)
	fmt.Fprintln(w, `/_/   \_\__, |\___|_| |_|\__|___/`)
	fmt.Fprintln(w, `        |___/                     `)
	fmt.Fprintln(w, `coding agents, coordinated`)
}

func promptTrust(in *bufio.Reader, out io.Writer, cwd string) (bool, error) {
	fmt.Fprintln(out, "This folder is not trusted yet.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, cwd)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Agents may read files, write .agents/ state, run configured agent commands,")
	fmt.Fprintln(out, "and ask before irreversible actions.")
	fmt.Fprintln(out)
	fmt.Fprint(out, "Trust this folder? [y/N] ")

	line, err := in.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	answer := strings.TrimSpace(strings.ToLower(line))
	return answer == "y" || answer == "yes", nil
}

func runTerminalLoop(rawIn io.Reader, in *bufio.Reader, out, errOut io.Writer, e env) int {
	if err := printStatus(out, e, false); err != nil {
		fmt.Fprintf(errOut, "agents: read status: %v\n", err)
		return 1
	}
	fmt.Fprintln(out)
	printInteractiveFooter(out)
	if e.isTTY {
		if inFile, ok := rawIn.(*os.File); ok {
			return runTerminalLoopTTY(inFile, out, errOut, e)
		}
	}

	for {
		fmt.Fprint(out, "› ")
		line, err := in.ReadString('\n')
		if err != nil && err != io.EOF {
			fmt.Fprintf(errOut, "agents: read command: %v\n", err)
			return 1
		}

		rawCommand := strings.TrimSpace(line)
		if rawCommand == "" {
			if err == io.EOF {
				fmt.Fprintln(out)
				return 0
			}
		} else {
			code, done := handleInteractiveCommand(rawCommand, out, errOut, e)
			if done {
				return code
			}
		}

		if err == io.EOF {
			fmt.Fprintln(out)
			return 0
		}
	}
}

func handleInteractiveCommand(rawCommand string, out, errOut io.Writer, e env) (int, bool) {
	switch {
	case rawCommand == "/":
		printSlashCommandSuggestions(out, rawCommand, e.isTTY)
	case rawCommand == "?":
		printInteractiveHelp(out)
	case strings.HasPrefix(rawCommand, "/"):
		normalizedCommand, ok := resolveInteractiveSlashCommand(rawCommand, out, e.isTTY)
		if !ok {
			return 0, false
		}
		switch normalizedCommand {
		case "status":
			if statusErr := printStatus(out, e, false); statusErr != nil {
				fmt.Fprintf(errOut, "agents: read status: %v\n", statusErr)
				return 1, true
			}
		case "help":
			printInteractiveHelp(out)
		case "quit", "exit":
			fmt.Fprintln(out, "Goodbye.")
			return 0, true
		default:
			args, parseErr := parseCommandLine(normalizedCommand)
			if parseErr != nil {
				fmt.Fprintf(errOut, "agents: parse command: %v\n", parseErr)
			} else if len(args) > 0 && args[0] == "run" && e.isTTY {
				flags, flagErr := parseRunArgs(args[1:])
				if flagErr != nil {
					fmt.Fprintf(errOut, "agents: run: %v\n", flagErr)
				} else if flags.Once || flags.JSON || flags.JSONEvents {
					code := runOneShot(args, strings.NewReader(""), out, errOut, e)
					if code != 0 {
						fmt.Fprintf(out, "command exited: %d\n", code)
					}
				} else if runErr := runInteractiveQueuedRoles(e, out, flags.MaxRuns); runErr != nil {
					if errors.Is(runErr, errRunInterrupted) {
						printInterruptHint(out, e)
						return 130, true
					}
					fmt.Fprintf(errOut, "agents: loop: %v\n", runErr)
				}
			} else if len(args) > 0 {
				// The composer owns stdin here; a non-file reader makes
				// TTY-gated commands (go's view) fall back to plain output.
				code := runOneShot(args, strings.NewReader(""), out, errOut, e)
				if code != 0 {
					fmt.Fprintf(out, "command exited: %d\n", code)
				}
			}
		}
	default:
		if chatErr := recordCoordinatorChat(e, rawCommand, out); chatErr != nil {
			fmt.Fprintf(errOut, "agents: coordinator chat: %v\n", chatErr)
			return 1, true
		}
		if e.isTTY {
			if runErr := runCoordinatorChatResponse(e, out); runErr != nil {
				if errors.Is(runErr, errRunInterrupted) {
					printInterruptHint(out, e)
					return 130, true
				}
				fmt.Fprintf(errOut, "agents: coordinator response: %v\n", runErr)
			}
		}
	}
	return 0, false
}

func runTerminalLoopTTY(in *os.File, out, errOut io.Writer, e env) int {
	fd := int(in.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Fprintf(errOut, "agents: enable raw terminal: %v\n", err)
		return 1
	}
	defer term.Restore(fd, oldState)
	fmt.Fprint(out, "\x1b[?2004h")
	defer fmt.Fprint(out, "\x1b[?2004l")

	for {
		rawCommand, err := readComposerLine(in, out, e)
		if err != nil {
			if err == io.EOF {
				fmt.Fprintln(out)
				return 0
			}
			if errors.Is(err, errComposerInterrupted) {
				printInterruptHint(out, e)
				return 130
			}
			fmt.Fprintf(errOut, "agents: read command: %v\n", err)
			return 1
		}
		if rawCommand == "" {
			continue
		}
		term.Restore(fd, oldState)
		code, done := handleInteractiveCommand(rawCommand, out, errOut, e)
		if done {
			return code
		}
		oldState, err = term.MakeRaw(fd)
		if err != nil {
			fmt.Fprintf(errOut, "agents: enable raw terminal: %v\n", err)
			return 1
		}
	}
}

func printInterruptHint(out io.Writer, e env) {
	active := readActiveTask(e.cwd)
	if active.Title != "" {
		fmt.Fprintf(out, "Active task: %s\n", active.Title)
	}
	fmt.Fprintln(out, "To continue this session, run agents")
}

func readComposerLine(in *os.File, out io.Writer, e env) (string, error) {
	var line []rune
	renderedLines := 0
	renderedLines = renderComposer(out, string(line), e.isTTY, renderedLines)
	buf := make([]byte, 1)
	for {
		n, err := in.Read(buf)
		if err != nil {
			return "", err
		}
		if n == 0 {
			continue
		}
		switch b := buf[0]; b {
		case 3:
			fmt.Fprint(out, "\r\n")
			return "", errComposerInterrupted
		case '\r', '\n':
			fmt.Fprint(out, "\r\n")
			return strings.TrimSpace(string(line)), nil
		case '\t':
			if completed, ok := completeSlashInput(string(line)); ok {
				line = []rune(completed)
			}
		case 27:
			pasted, ok, err := readBracketedPaste(in)
			if err != nil {
				return "", err
			}
			if ok {
				line = append(line, []rune(normalizePastedText(pasted))...)
			}
		case 127, 8:
			if len(line) > 0 {
				line = line[:len(line)-1]
			}
		default:
			if b >= 32 {
				line = append(line, rune(b))
			}
		}
		renderedLines = renderComposer(out, string(line), e.isTTY, renderedLines)
	}
}

func renderComposer(out io.Writer, line string, color bool, previousExtraLines int) int {
	fmt.Fprint(out, "\r\033[J")
	fmt.Fprintf(out, "› %s", line)
	suggestions := composerSuggestionLines(line, color)
	for _, suggestion := range suggestions {
		fmt.Fprintf(out, "\r\n%s", suggestion)
	}
	if len(suggestions) > 0 {
		fmt.Fprintf(out, "\033[%dA", len(suggestions))
	}
	fmt.Fprint(out, "\r")
	if width := 2 + len(line); width > 0 {
		fmt.Fprintf(out, "\033[%dC", width)
	}
	return len(suggestions)
}

func composerSuggestionLines(line string, color bool) []string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || !strings.HasPrefix(trimmed, "/") || strings.Contains(trimmed, " ") {
		return nil
	}
	matches := matchingSlashCommands(trimmed)
	if len(matches) == 0 {
		return []string{fmt.Sprintf("No slash commands match %q.", trimmed)}
	}
	if len(matches) == 1 && matches[0].Name == trimmed {
		return nil
	}
	var lines []string
	visible := matches
	if len(visible) > liveSuggestionLimit {
		visible = visible[:liveSuggestionLimit]
	}
	for _, command := range visible {
		usage := command.Usage
		description := command.Description
		if color {
			usage = "\x1b[36m" + usage + "\x1b[0m"
			description = "\x1b[2m" + description + "\x1b[0m"
		}
		lines = append(lines, fmt.Sprintf("  %-42s %s", usage, description))
	}
	if hidden := len(matches) - len(visible); hidden > 0 {
		lines = append(lines, fmt.Sprintf("  ... %d more; type more, press Tab for unique match, or ? for all", hidden))
	}
	return lines
}

func readBracketedPaste(in *os.File) (string, bool, error) {
	seq := make([]byte, 5)
	if _, err := io.ReadFull(in, seq); err != nil {
		return "", false, err
	}
	if string(seq) != "[200~" {
		return "", false, nil
	}
	var b strings.Builder
	window := make([]byte, 0, 6)
	buf := make([]byte, 1)
	for {
		if _, err := in.Read(buf); err != nil {
			return "", false, err
		}
		window = append(window, buf[0])
		if len(window) > 6 {
			b.WriteByte(window[0])
			window = window[1:]
		}
		if string(window) == "\x1b[201~" {
			return b.String(), true, nil
		}
	}
}

func normalizePastedText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func completeSlashInput(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || !strings.HasPrefix(trimmed, "/") || strings.Contains(trimmed, " ") {
		return line, false
	}
	matches := matchingSlashCommands(trimmed)
	if len(matches) != 1 {
		return line, false
	}
	return matches[0].Name + " ", true
}

func parseCommandLine(line string) ([]string, error) {
	var args []string
	var b strings.Builder
	inQuote := false
	quote := rune(0)
	escaped := false
	for _, r := range line {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if inQuote {
			if r == quote {
				inQuote = false
			} else {
				b.WriteRune(r)
			}
			continue
		}
		if r == '"' || r == '\'' {
			inQuote = true
			quote = r
			continue
		}
		if unicode.IsSpace(r) {
			if b.Len() > 0 {
				args = append(args, b.String())
				b.Reset()
			}
			continue
		}
		b.WriteRune(r)
	}
	if escaped {
		b.WriteRune('\\')
	}
	if inQuote {
		return nil, fmt.Errorf("unterminated quote")
	}
	if b.Len() > 0 {
		args = append(args, b.String())
	}
	return args, nil
}

func recordCoordinatorChat(e env, text string, out io.Writer) error {
	task := readActiveTask(e.cwd)
	if task.Path == "" {
		opened, err := openTask(e, []string{coordinatorChatTitle(text)}, time.Now().UTC())
		if err != nil {
			return err
		}
		task = opened
		if !e.isTTY {
			fmt.Fprintf(out, "Started coordinator intake: %s\n", task.Title)
			fmt.Fprintf(out, "Task path: %s\n", task.Path)
		}
	}
	handoff, err := addHandoff(e, []string{"--to", "coordinator", text}, time.Now().UTC())
	if err != nil {
		return err
	}
	if e.isTTY {
		// Chat mode should feel conversational; the handoff is durable state,
		// but not user-facing ceremony.
	} else {
		fmt.Fprintln(out, "Sent to coordinator.")
		fmt.Fprintf(out, "Handoff path: %s\n", handoff.Path)
		fmt.Fprintln(out)
		fmt.Fprintf(out, "Next: /run to ask the coordinator, or keep typing to add context\n")
	}
	return nil
}

func runCoordinatorChatResponse(e env, out io.Writer) error {
	state, err := analyzeLoopState(e)
	if err != nil {
		return err
	}
	if state.NextRole != "coordinator" {
		printLoopState(out, state)
		return nil
	}
	agent, err := configuredAgent(e.cwd, state.NextRole)
	if err != nil {
		return err
	}
	if strings.TrimSpace(agent) == "" {
		fmt.Fprintln(out, "Coordinator is not configured.")
		fmt.Fprintln(out, "Next: configure roles.coordinator in .agents/config.yml and try again")
		return nil
	}
	progress := newProgressWriter(out, "Coordinator is thinking")
	progress.Start()
	result, err := runConfiguredRoleWithOptions(e, state, roleRunOptions{ShowRunHeader: true, LiveOutput: progress})
	progress.Stop()
	if err != nil {
		return err
	}
	reply := cleanInteractiveAgentReply(result.Agent, result.AgentOutput)
	directive, directiveOK := parseCoordinatorDirective(reply.Text)
	if directiveOK {
		allowDirective := coordinatorDirectiveActionable("coordinator", directive)
		if allowDirective {
			if err := applyCoordinatorDirective(e, "coordinator", directive); err != nil {
				return err
			}
		}
		reply.Text = removeCoordinatorDirective(reply.Text)
		if allowDirective {
			after, err := analyzeLoopState(e)
			if err != nil {
				return err
			}
			result.After = after
		}
	}
	if strings.TrimSpace(reply.Text) != "" {
		fmt.Fprintln(out)
		printInteractiveMarkdown(out, reply.Text)
	}
	if reply.Usage != "" {
		fmt.Fprintf(out, "Usage: %s\n", reply.Usage)
	}
	printInteractiveReadyCue(out, result)
	return nil
}

type coordinatorDirective struct {
	Role  string
	Text  string
	Ready bool
}

type verificationDirective struct {
	Text string
	Cmd  string
}

func parseCoordinatorDirective(text string) (coordinatorDirective, bool) {
	start := strings.LastIndex(text, "```agents-handoff")
	if start < 0 {
		return coordinatorDirective{}, false
	}
	blockStart := strings.Index(text[start:], "\n")
	if blockStart < 0 {
		return coordinatorDirective{}, false
	}
	contentStart := start + blockStart + 1
	endRel := strings.Index(text[contentStart:], "```")
	if endRel < 0 {
		return coordinatorDirective{}, false
	}
	content := text[contentStart : contentStart+endRel]
	if directive, ok := parseCoordinatorDirectiveJSON(content); ok {
		return directive, true
	}
	var directive coordinatorDirective
	var textLines []string
	inText := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "role:"):
			directive.Role = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "role:")))
			inText = false
		case strings.HasPrefix(trimmed, "ready:"):
			value := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "ready:")))
			directive.Ready = value == "true" || value == "yes"
			inText = false
		case strings.HasPrefix(trimmed, "text:"):
			textLines = append(textLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "text:")))
			inText = true
		case inText:
			textLines = append(textLines, strings.TrimRight(line, " \t"))
		}
	}
	directive.Text = strings.TrimSpace(strings.Join(textLines, "\n"))
	if directive.Role == "" || directive.Text == "" {
		return coordinatorDirective{}, false
	}
	return directive, true
}

func parseCoordinatorDirectiveJSON(content string) (coordinatorDirective, bool) {
	var raw struct {
		Role  string `json:"role"`
		Text  string `json:"text"`
		Ready bool   `json:"ready"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &raw); err != nil {
		return coordinatorDirective{}, false
	}
	directive := coordinatorDirective{
		Role:  strings.ToLower(strings.TrimSpace(raw.Role)),
		Text:  strings.TrimSpace(raw.Text),
		Ready: raw.Ready,
	}
	if directive.Role == "" || directive.Text == "" {
		return coordinatorDirective{}, false
	}
	return directive, true
}

func removeCoordinatorDirective(text string) string {
	return removeFencedDirective(text, "agents-handoff")
}

func parseVerificationDirective(text string) (verificationDirective, bool) {
	content, ok := fencedDirectiveContent(text, "agents-verify")
	if !ok {
		return verificationDirective{}, false
	}
	var raw struct {
		Text string `json:"text"`
		Cmd  string `json:"cmd"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &raw); err != nil {
		return verificationDirective{}, false
	}
	directive := verificationDirective{
		Text: strings.TrimSpace(raw.Text),
		Cmd:  strings.TrimSpace(raw.Cmd),
	}
	if directive.Text == "" {
		return verificationDirective{}, false
	}
	return directive, true
}

func removeVerificationDirective(text string) string {
	return removeFencedDirective(text, "agents-verify")
}

func fencedDirectiveContent(text, name string) (string, bool) {
	start := strings.LastIndex(text, "```"+name)
	if start < 0 {
		return "", false
	}
	blockStart := strings.Index(text[start:], "\n")
	if blockStart < 0 {
		return "", false
	}
	contentStart := start + blockStart + 1
	endRel := strings.Index(text[contentStart:], "```")
	if endRel < 0 {
		return "", false
	}
	return text[contentStart : contentStart+endRel], true
}

func removeFencedDirective(text, name string) string {
	start := strings.LastIndex(text, "```"+name)
	if start < 0 {
		return text
	}
	blockStart := strings.Index(text[start:], "\n")
	if blockStart < 0 {
		return text
	}
	contentStart := start + blockStart + 1
	endRel := strings.Index(text[contentStart:], "```")
	if endRel < 0 {
		return text
	}
	end := contentStart + endRel + len("```")
	return strings.TrimSpace(text[:start] + text[end:])
}

func applyCoordinatorDirective(e env, actor string, directive coordinatorDirective) error {
	switch directive.Role {
	case "implementer", "reviewer", "coordinator":
	default:
		return fmt.Errorf("coordinator requested unknown handoff role %q", directive.Role)
	}
	if !coordinatorDirectiveActionable(actor, directive) {
		return nil
	}
	args := []string{"--to", directive.Role, directive.Text}
	if strings.TrimSpace(actor) != "" {
		args = append(args, "--by", actor)
	}
	_, err := addHandoff(e, args, time.Now().UTC())
	return err
}

func coordinatorDirectiveActionable(actor string, directive coordinatorDirective) bool {
	if coordinatorDirectivePlaceholder(directive) {
		return false
	}
	if actor != "coordinator" || directive.Role != "implementer" {
		return true
	}
	if !directive.Ready {
		return false
	}
	text := strings.TrimSpace(strings.ToLower(directive.Text))
	return text != "" && text != "clear implementation request with context and verification"
}

func coordinatorDirectivePlaceholder(directive coordinatorDirective) bool {
	text := strings.TrimSpace(strings.ToLower(directive.Text))
	switch text {
	case "specific changes made and checks performed",
		"clear implementation request with context and verification":
		return true
	default:
		return false
	}
}

type progressWriter struct {
	out       io.Writer
	label     string
	stop      chan struct{}
	done      chan struct{}
	once      sync.Once
	mu        sync.Mutex
	started   bool
	pending   string
	inBlock   bool
	blockLine []string
}

func newProgressWriter(out io.Writer, label string) *progressWriter {
	return &progressWriter{
		out:   out,
		label: label,
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
}

func (w *progressWriter) Start() {
	w.mu.Lock()
	w.started = true
	fmt.Fprintf(w.out, "\r%s |", w.label)
	w.mu.Unlock()
	go func() {
		frames := []rune{'|', '/', '-', '\\'}
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()
		defer close(w.done)
		i := 0
		for {
			select {
			case <-w.stop:
				w.mu.Lock()
				fmt.Fprintf(w.out, "\r\033[2K")
				w.mu.Unlock()
				return
			case <-ticker.C:
				w.mu.Lock()
				fmt.Fprintf(w.out, "\r%s %c", w.label, frames[i%len(frames)])
				w.mu.Unlock()
				i++
			}
		}
	}()
}

func (w *progressWriter) Stop() {
	w.once.Do(func() {
		close(w.stop)
		<-w.done
	})
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending += string(p)
	for {
		line, rest, ok := strings.Cut(w.pending, "\n")
		if !ok {
			break
		}
		w.pending = rest
		w.handleLine(line)
	}
	return len(p), nil
}

func (w *progressWriter) handleLine(line string) {
	trimmed := strings.TrimSpace(line)
	if w.inBlock {
		if trimmed == "```" {
			if progress, ok := parseProgressDirectiveLines(w.blockLine); ok {
				fmt.Fprint(w.out, "\r\033[2K")
				printInteractiveProgress(w.out, progress)
			}
			w.inBlock = false
			w.blockLine = nil
			return
		}
		w.blockLine = append(w.blockLine, line)
		return
	}
	switch {
	case strings.HasPrefix(trimmed, "run id:") || strings.HasPrefix(trimmed, "run log:"):
		_, _ = fmt.Fprintln(w.out, trimmed)
	case trimmed == "```agents-progress":
		w.inBlock = true
		w.blockLine = nil
	}
}

func printInteractiveReadyCue(out io.Writer, result roleRunResult) {
	if result.After.NextRole == "" || result.After.NextRole == result.Before.NextRole {
		return
	}
	fmt.Fprintf(out, "\nReady for %s. Type /run to start.\n", titleRole(result.After.NextRole))
}

type interactiveRunHeaderWriter struct {
	out       io.Writer
	pending   string
	inBlock   bool
	blockLine []string
}

func (w *interactiveRunHeaderWriter) Write(p []byte) (int, error) {
	w.pending += string(p)
	for {
		line, rest, ok := strings.Cut(w.pending, "\n")
		if !ok {
			break
		}
		w.pending = rest
		w.handleLine(line)
	}
	return len(p), nil
}

func (w *interactiveRunHeaderWriter) handleLine(line string) {
	trimmed := strings.TrimSpace(line)
	if w.inBlock {
		if trimmed == "```" {
			if progress, ok := parseProgressDirectiveLines(w.blockLine); ok {
				printInteractiveProgress(w.out, progress)
			}
			w.inBlock = false
			w.blockLine = nil
			return
		}
		w.blockLine = append(w.blockLine, line)
		return
	}
	switch {
	case strings.HasPrefix(trimmed, "run id:"):
		fmt.Fprintf(w.out, "run: %s\n", strings.TrimSpace(strings.TrimPrefix(trimmed, "run id:")))
	case strings.HasPrefix(trimmed, "run log:"):
		fmt.Fprintf(w.out, "log: %s\n", strings.TrimSpace(strings.TrimPrefix(trimmed, "run log:")))
	case trimmed == "```agents-progress":
		w.inBlock = true
		w.blockLine = nil
	}
}

type progressDirective struct {
	Status string
	Detail string
}

func parseProgressDirectiveLines(lines []string) (progressDirective, bool) {
	var p progressDirective
	data := strings.TrimSpace(strings.Join(lines, "\n"))
	if data == "" {
		return progressDirective{}, false
	}
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return progressDirective{}, false
	}
	p.Status = strings.TrimSpace(p.Status)
	p.Detail = strings.TrimSpace(p.Detail)
	return p, p.Status != ""
}

func printInteractiveProgress(w io.Writer, progress progressDirective) {
	status := strings.TrimSpace(progress.Status)
	if status == "" {
		return
	}
	detail := strings.TrimSpace(progress.Detail)
	if detail == "" {
		fmt.Fprintf(w, "- %s\n", sentenceCase(status))
		return
	}
	fmt.Fprintf(w, "- %s: %s\n", sentenceCase(status), sentenceCase(singleLine(detail)))
}

func printInteractiveMarkdown(w io.Writer, markdown string) {
	text := strings.TrimRight(markdown, "\n")
	if text == "" {
		return
	}
	if !isTerminalWriter(w) {
		fmt.Fprintln(w, text)
		return
	}
	width := terminalWriterWidth(w)
	rendered, err := renderTerminalMarkdown(text, width)
	if err != nil {
		fmt.Fprintln(w, text)
		return
	}
	fmt.Fprint(w, strings.TrimRight(rendered, "\n"))
	fmt.Fprintln(w)
}

func renderTerminalMarkdown(markdown string, width int) (string, error) {
	if width <= 0 {
		width = 100
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithEnvironmentConfig(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return "", err
	}
	return renderer.Render(markdown)
}

func isTerminalWriter(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func terminalWriterWidth(w io.Writer) int {
	file, ok := w.(*os.File)
	if !ok {
		return 100
	}
	width, _, err := term.GetSize(int(file.Fd()))
	if err != nil || width <= 0 {
		return 100
	}
	if width > 120 {
		return 120
	}
	return width
}

func singleLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func sentenceCase(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	runes := []rune(text)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func printInteractiveRoleHeader(w io.Writer, role string) {
	title := titleRole(role)
	fmt.Fprintf(w, "\n%s\n%s\n", title, strings.Repeat("-", len(title)))
}

func titleRole(role string) string {
	if role == "" {
		return "Role"
	}
	return strings.ToUpper(role[:1]) + role[1:]
}

func printInteractiveRoleRunResult(w io.Writer, result roleRunResult) {
	reply := cleanInteractiveAgentReply(result.Agent, result.AgentOutput)
	if strings.TrimSpace(reply.Text) != "" {
		fmt.Fprintln(w)
		printInteractiveMarkdown(w, reply.Text)
	}
	if reply.Usage != "" {
		fmt.Fprintf(w, "Usage: %s\n", reply.Usage)
	}
	if result.After.NextRole != "" && result.After.NextRole != result.Before.NextRole {
		if result.After.NextRole == "human" {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Ready for you.")
			return
		}
		fmt.Fprintf(w, "\nNext: %s\n", titleRole(result.After.NextRole))
	}
}

func runInteractiveQueuedRoles(e env, out io.Writer, maxRuns int) error {
	stop, err := runManagedLoop(e, out, maxRuns, true, nil, nil, loopDisplayInteractive)
	if err != nil {
		return err
	}
	switch stop.Reason {
	case "human":
		fmt.Fprintln(out)
		printLoopState(out, stop.State)
	case "unconfigured":
		fmt.Fprintf(out, "\nNext: %s\n", titleRole(stop.State.NextRole))
		fmt.Fprintf(out, "Agent is not configured for %s.\n", stop.State.NextRole)
	case "no-progress":
		fmt.Fprintf(out, "\nPaused: %s did not record new task progress.\n", titleRole(stop.State.NextRole))
		fmt.Fprintln(out, "Review the run log, keep planning, or use /run again when ready.")
	case "max-runs":
		fmt.Fprintf(out, "\nLoop paused after %d role runs; use /run to continue.\n", stop.Runs)
	}
	return nil
}

func coordinatorChatTitle(text string) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return "Coordinator intake"
	}
	title := strings.Join(words, " ")
	if len(title) > 60 {
		title = strings.TrimSpace(title[:60])
	}
	if title == "" {
		return "Coordinator intake"
	}
	return title
}

func printStatus(w io.Writer, e env, jsonOut bool) error {
	trusted, err := isTrusted(e)
	if err != nil {
		return err
	}
	active := readActiveTask(e.cwd)
	if jsonOut {
		payload := statusJSON{
			Workspace: e.cwd,
			Trust:     trustStatus(trusted),
		}
		if active.Path != "" {
			stats, err := readTaskStats(e.cwd, active.Path)
			if err != nil {
				return err
			}
			payload.ActiveTask = taskPayload(active, stats)
		}
		return writeJSON(w, payload)
	}

	fmt.Fprintf(w, "workspace: %s\n", e.cwd)
	fmt.Fprintf(w, "trust: %s\n", trustStatus(trusted))
	if active.Path == "" {
		fmt.Fprintln(w, "active task: none")
	} else {
		fmt.Fprintf(w, "active task: %s\n", active.Title)
		if active.Missing {
			fmt.Fprintln(w, "warning: active task directory is missing; run `agents task use <task>` or `agents task open`")
		}
		if active.Status != "" {
			fmt.Fprintf(w, "task status: %s\n", active.Status)
		}
		fmt.Fprintf(w, "task path: %s\n", active.Path)
		stats, err := readTaskStats(e.cwd, active.Path)
		if err != nil {
			return err
		}
		printTaskStats(w, stats)
	}
	return nil
}

type statusJSON struct {
	Workspace  string    `json:"workspace"`
	Trust      string    `json:"trust"`
	ActiveTask *taskJSON `json:"active_task"`
}

type taskJSON struct {
	Title    string       `json:"title"`
	Status   string       `json:"status,omitempty"`
	Path     string       `json:"path"`
	Missing  bool         `json:"missing,omitempty"`
	Next     string       `json:"next,omitempty"`
	Findings findingsJSON `json:"findings"`
	Records  recordsJSON  `json:"records"`
}

type findingsJSON struct {
	Open     int `json:"open"`
	Fixed    int `json:"fixed"`
	Verified int `json:"verified"`
}

type recordsJSON struct {
	Decisions          int `json:"decisions"`
	Handoffs           int `json:"handoffs"`
	Verification       int `json:"verification"`
	VerificationFailed int `json:"verification_failed,omitempty"`
}

type dispatchJSON struct {
	Role       string    `json:"role"`
	Agent      string    `json:"agent,omitempty"`
	Configured bool      `json:"configured"`
	Task       *taskJSON `json:"task"`
	Action     string    `json:"action"`
	Boundary   string    `json:"boundary,omitempty"`
}

type loopJSON struct {
	Task       *taskJSON `json:"task"`
	NextRole   string    `json:"next_role"`
	NextAction string    `json:"next_action"`
	Boundary   string    `json:"boundary,omitempty"`
}

type trustJSON struct {
	Workspace string `json:"workspace"`
	Trust     string `json:"trust"`
	User      string `json:"user,omitempty"`
	Host      string `json:"host,omitempty"`
	GitRemote string `json:"git_remote,omitempty"`
}

func trustStatus(trusted bool) string {
	if trusted {
		return "trusted"
	}
	return "not trusted"
}

func taskPayload(task activeTask, stats taskStats) *taskJSON {
	return &taskJSON{
		Title:   task.Title,
		Status:  task.Status,
		Path:    task.Path,
		Missing: task.Missing,
		Next:    stats.Next,
		Findings: findingsJSON{
			Open:     stats.FindingsOpen,
			Fixed:    stats.FindingsFixed,
			Verified: stats.FindingsVerified,
		},
		Records: recordsJSON{
			Decisions:          stats.Decisions,
			Handoffs:           stats.Handoffs,
			Verification:       stats.Verification,
			VerificationFailed: stats.VerificationFail,
		},
	}
}

func writeJSON(w io.Writer, v any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}

func parseJSONFlag(args []string) (bool, error) {
	jsonOut := false
	for _, arg := range args {
		switch strings.TrimSpace(arg) {
		case "":
			continue
		case "--json":
			jsonOut = true
		default:
			return false, fmt.Errorf("unknown flag %s", arg)
		}
	}
	return jsonOut, nil
}

func runTrustCommand(args []string, out, errOut io.Writer, e env) int {
	jsonOut, err := parseJSONFlag(args)
	if err != nil {
		fmt.Fprintf(errOut, "agents: trust: %v\n", err)
		return 2
	}
	record, err := trustWorkspace(e)
	if err != nil {
		fmt.Fprintf(errOut, "agents: trust: %v\n", err)
		return 1
	}
	if jsonOut {
		if err := writeJSON(out, trustPayload(e.cwd, record)); err != nil {
			fmt.Fprintf(errOut, "agents: trust: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(out, "trusted: %s\n", e.cwd)
	if record.GitRemote != "" {
		fmt.Fprintf(out, "git remote: %s\n", record.GitRemote)
	}
	return 0
}

func trustPayload(cwd string, record trustedWorkspace) trustJSON {
	return trustJSON{
		Workspace: cwd,
		Trust:     "trusted",
		User:      record.User,
		Host:      record.Host,
		GitRemote: record.GitRemote,
	}
}

type activeTask struct {
	Path   string
	Title  string
	Status string
	// Missing reports a dangling active pointer: the pointer resolves but the
	// task directory no longer exists.
	Missing bool
}

type taskStats struct {
	Next             string
	Decisions        int
	Handoffs         int
	LatestHandoffTo  string
	Verification     int
	VerificationFail int
	FindingsOpen     int
	FindingsFixed    int
	FindingsVerified int
	// LatestHandoffName and LatestVerificationName are chunk filenames; their
	// timestamp prefixes order handoffs against verification evidence.
	LatestHandoffName      string
	LatestVerificationName string
}

func readActiveTask(cwd string) activeTask {
	data, err := os.ReadFile(filepath.Join(cwd, stateDir, "active"))
	if err != nil {
		return activeTask{}
	}
	activePath := strings.TrimSpace(string(data))
	if activePath == "" {
		return activeTask{}
	}
	cleanPath, ok := cleanActivePath(activePath)
	if !ok {
		return activeTask{}
	}

	task := activeTask{
		Path:  cleanPath,
		Title: cleanPath,
	}
	taskData, err := os.ReadFile(filepath.Join(cwd, stateDir, cleanPath, "task.md"))
	if err != nil {
		task.Missing = os.IsNotExist(err)
		return task
	}
	title, status := parseTaskSummary(string(taskData))
	if title != "" {
		task.Title = title
	}
	task.Status = status
	return task
}

func cleanActivePath(activePath string) (string, bool) {
	cleaned := filepath.Clean(activePath)
	if cleaned == "." || filepath.IsAbs(cleaned) {
		return "", false
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", false
	}
	return cleaned, true
}

func parseTaskSummary(markdown string) (title, status string) {
	for _, line := range strings.Split(markdown, "\n") {
		t := strings.TrimSpace(line)
		if title == "" && strings.HasPrefix(t, "# ") {
			title = strings.TrimSpace(strings.TrimPrefix(t, "# "))
			continue
		}
		if status == "" && strings.HasPrefix(strings.ToLower(t), "status:") {
			status = strings.TrimSpace(t[len("status:"):])
		}
	}
	return title, status
}

func runInitCommand(args []string, out, errOut io.Writer, e env) int {
	mode, err := parseInitArgs(args)
	if err != nil {
		fmt.Fprintf(errOut, "agents: init: %v\n", err)
		return 2
	}
	created, err := initWorkspace(e, mode)
	if err != nil {
		fmt.Fprintf(errOut, "agents: init: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "initialized: %s\n", filepath.Join(e.cwd, stateDir))
	fmt.Fprintf(out, "task state: %s\n", mode)
	if created.Config {
		fmt.Fprintln(out, "created: .agents/config.yml")
	} else {
		fmt.Fprintln(out, "kept: .agents/config.yml")
	}
	if created.Gitignore {
		fmt.Fprintln(out, "updated: .gitignore")
	}
	return 0
}

type initCreated struct {
	Config    bool
	Gitignore bool
}

func parseInitArgs(args []string) (string, error) {
	mode := "share"
	for _, arg := range args {
		switch strings.TrimSpace(arg) {
		case "":
			continue
		case "--share":
			mode = "share"
		case "--local":
			mode = "local"
		default:
			return "", fmt.Errorf("unknown flag %s", arg)
		}
	}
	return mode, nil
}

func initWorkspace(e env, mode string) (initCreated, error) {
	var created initCreated
	statePath := filepath.Join(e.cwd, stateDir)
	for _, dir := range []string{
		statePath,
		filepath.Join(statePath, "tasks"),
		filepath.Join(statePath, "archive"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return created, err
		}
	}
	if err := ensureAgentsGitignore(statePath); err != nil {
		return created, err
	}
	activePath := filepath.Join(statePath, "active")
	if _, err := os.Stat(activePath); err != nil {
		if os.IsNotExist(err) {
			if err := os.WriteFile(activePath, []byte(""), 0o644); err != nil {
				return created, err
			}
		} else {
			return created, err
		}
	}
	configPath := filepath.Join(statePath, "config.yml")
	if _, err := os.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			if err := os.WriteFile(configPath, []byte(defaultConfig(mode)), 0o644); err != nil {
				return created, err
			}
			created.Config = true
		} else {
			return created, err
		}
	}
	if mode == "local" {
		updated, err := ensureGitignoreLine(e.cwd, ".agents/")
		if err != nil {
			return created, err
		}
		created.Gitignore = updated
	}
	return created, nil
}

func ensureAgentsGitignore(statePath string) error {
	path := filepath.Join(statePath, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	needed := []string{"runs/", "tmp/", "active"}
	existing := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		existing[strings.TrimSpace(line)] = true
	}
	var b strings.Builder
	b.Write(data)
	for _, line := range needed {
		if existing[line] {
			continue
		}
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func defaultConfig(mode string) string {
	commitChunks := "true"
	if mode == "local" {
		commitChunks = "false"
	}
	return fmt.Sprintf(`version: 1
storage:
  commit_chunks: %s
  cleanup_on_archive: true
  cleanup_mode: summarize_then_prune
  keep:
    active_tasks: true
    archived_summaries: true
    decisions: promoted_only
    findings: unresolved_only
settings:
  # Codex CLI: model examples: gpt-5.5, gpt-5.4, gpt-5-codex, codex-mini-latest
  # Codex reasoning examples: minimal, low, medium, high, xhigh
  codex_model: ""
  codex_reasoning: ""
  # Claude Code: model names and aliases vary by account and CLI version.
  # Run 'claude --help' or use Claude's in-app model controls for current choices.
  # Claude effort: low, medium, high, xhigh, max; supported levels depend on the selected model.
  claude_model: ""
  claude_effort: ""
  # Cursor Agent: run 'agent models' or 'cursor-agent --list-models' for account-specific choices
  # Common examples from CLI help: gpt-5, claude-4.6-sonnet-medium, claude-4.6-sonnet-medium-thinking
  cursor_model: ""
# Each role is a provider name (cursor, cursor-agent, agent, codex, claude)
# or a map with provider plus optional model/reasoning overrides:
#   reviewer:
#     provider: codex
#     model: gpt-5.5
#     reasoning: high
# Advanced escape hatch: a cmd template containing {prompt_file}.
roles:
  implementer: agent
  reviewer: codex
  coordinator: codex
`, commitChunks)
}

func ensureGitignoreLine(cwd, line string) (bool, error) {
	path := filepath.Join(cwd, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	for _, existing := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(existing) == line {
			return false, nil
		}
	}
	var b strings.Builder
	b.Write(data)
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		b.WriteByte('\n')
	}
	b.WriteString(line)
	b.WriteByte('\n')
	return true, os.WriteFile(path, []byte(b.String()), 0o644)
}

func readTaskStats(cwd, activePath string) (taskStats, error) {
	taskDir := filepath.Join(cwd, stateDir, activePath)
	decisions, err := countMarkdownFiles(filepath.Join(taskDir, "decisions"))
	if err != nil {
		return taskStats{}, err
	}
	handoffs, err := countMarkdownFiles(filepath.Join(taskDir, "handoffs"))
	if err != nil {
		return taskStats{}, err
	}
	latestHandoffTo, latestHandoffName, err := readLatestHandoffTo(filepath.Join(taskDir, "handoffs"))
	if err != nil {
		return taskStats{}, err
	}
	verificationPass, verificationFail, latestVerificationName, err := readVerificationStats(filepath.Join(taskDir, "verification"))
	if err != nil {
		return taskStats{}, err
	}
	openFindings, fixedFindings, verifiedFindings, err := countFindingStatuses(filepath.Join(taskDir, "findings"))
	if err != nil {
		return taskStats{}, err
	}
	next, err := readNextLine(filepath.Join(taskDir, "next.md"))
	if err != nil {
		return taskStats{}, err
	}
	return taskStats{
		Next:                   next,
		Decisions:              decisions,
		Handoffs:               handoffs,
		LatestHandoffTo:        latestHandoffTo,
		LatestHandoffName:      latestHandoffName,
		Verification:           verificationPass,
		VerificationFail:       verificationFail,
		LatestVerificationName: latestVerificationName,
		FindingsOpen:           openFindings,
		FindingsFixed:          fixedFindings,
		FindingsVerified:       verifiedFindings,
	}, nil
}

// readVerificationStats counts passing and failing verification chunks and
// returns the newest *independent* passing chunk filename. Chunks without a
// Result line are manually recorded evidence and count as passing. Evidence
// recorded by the implementer never satisfies the human approval boundary:
// implementers do not verify their own work.
func readVerificationStats(dir string) (pass, fail int, latestPass string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, "", nil
		}
		return 0, 0, "", err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return 0, 0, "", err
		}
		failed := false
		resultSeen := false
		by := ""
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.ToLower(strings.TrimSpace(line))
			if !resultSeen && strings.HasPrefix(trimmed, "result:") {
				failed = strings.TrimSpace(strings.TrimPrefix(trimmed, "result:")) == "fail"
				resultSeen = true
			}
			if by == "" && strings.HasPrefix(trimmed, "by:") {
				by = strings.TrimSpace(strings.TrimPrefix(trimmed, "by:"))
			}
			if resultSeen && by != "" {
				break
			}
		}
		if failed {
			fail++
			continue
		}
		pass++
		if by != "implementer" && entry.Name() > latestPass {
			latestPass = entry.Name()
		}
	}
	return pass, fail, latestPass, nil
}

func printTaskStats(w io.Writer, stats taskStats) {
	if stats.Next != "" {
		fmt.Fprintf(w, "next: %s\n", stats.Next)
	}
	fmt.Fprintf(w, "findings: open=%d fixed=%d verified=%d\n", stats.FindingsOpen, stats.FindingsFixed, stats.FindingsVerified)
	if stats.VerificationFail > 0 {
		fmt.Fprintf(w, "records: decisions=%d handoffs=%d verification=%d (failed=%d)\n", stats.Decisions, stats.Handoffs, stats.Verification, stats.VerificationFail)
	} else {
		fmt.Fprintf(w, "records: decisions=%d handoffs=%d verification=%d\n", stats.Decisions, stats.Handoffs, stats.Verification)
	}
}

func readNextLine(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(t, "-")), nil
	}
	return "", nil
}

func readLatestHandoffTo(dir string) (to, name string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", nil
		}
		return "", "", err
	}
	latest := ""
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if latest == "" || entry.Name() > latest {
			latest = entry.Name()
		}
	}
	if latest == "" {
		return "", "", nil
	}
	data, err := os.ReadFile(filepath.Join(dir, latest))
	if err != nil {
		return "", "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "to:") {
			return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "To:"))), latest, nil
		}
	}
	return "", latest, nil
}

func runStartCommand(args []string, out, errOut io.Writer, e env) int {
	title, ref, text, err := parseStartArgs(args)
	if err != nil {
		fmt.Fprintf(errOut, "agents: start: %v\n", err)
		return 2
	}
	taskArgs := []string{title}
	if ref != "" {
		taskArgs = append(taskArgs, "--ref", ref)
	}
	task, err := openTask(e, taskArgs, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(errOut, "agents: start: %v\n", err)
		return 1
	}
	handoff, err := addHandoff(e, []string{"--to", "implementer", text}, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(errOut, "agents: start: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "started task: %s\n", task.Title)
	fmt.Fprintf(out, "task path: %s\n", task.Path)
	fmt.Fprintf(out, "handoff to: %s\n", handoff.To)
	fmt.Fprintf(out, "handoff path: %s\n", handoff.Path)
	return 0
}

func parseStartArgs(args []string) (title, ref, text string, err error) {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		switch arg {
		case "":
			continue
		case "--ref":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return "", "", "", fmt.Errorf("--ref requires a value")
			}
			ref = strings.TrimSpace(args[i+1])
			i++
		default:
			if strings.HasPrefix(arg, "--") {
				return "", "", "", fmt.Errorf("unknown flag %s", arg)
			}
			if title == "" {
				title = arg
				continue
			}
			if text == "" {
				text = arg
				continue
			}
			return "", "", "", fmt.Errorf("start accepts exactly one title and one instruction")
		}
	}
	if title == "" {
		return "", "", "", fmt.Errorf("task title is required")
	}
	if text == "" {
		return "", "", "", fmt.Errorf("instruction text is required")
	}
	return title, ref, text, nil
}

// runGoCommand is the one-command entry point: open a task, record scope,
// hand off to the implementer, and drive the managed loop to a stop
// condition. `agents go "what you want"` — no role homework. On a TTY the
// loop renders the unified split view; non-TTY (and --plain) falls back to
// the plain loop output automatically.
func runGoCommand(args []string, in io.Reader, out, errOut io.Writer, e env) int {
	text, ref, maxRuns, plain, err := parseGoArgs(args)
	if err != nil {
		fmt.Fprintf(errOut, "agents: go: %v\n", err)
		return 2
	}
	// go executes shell command templates from .agents/config.yml, so refuse
	// untrusted workspaces before any task state is created.
	trusted, err := isTrusted(e)
	if err != nil {
		fmt.Fprintf(errOut, "agents: go: %v\n", err)
		return 1
	}
	if !trusted {
		fmt.Fprintf(errOut, "agents: go: %s is not trusted; `agents go` executes commands from .agents/config.yml — review it, then run `agents trust`\n", e.cwd)
		return 1
	}
	title, instruction := splitGoText(text)
	taskArgs := []string{title}
	if ref != "" {
		taskArgs = append(taskArgs, "--ref", ref)
	}
	task, err := openTask(e, taskArgs, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(errOut, "agents: go: %v\n", err)
		return 1
	}
	if _, err := setTaskChunk(e, "scope", []string{instruction}); err != nil {
		fmt.Fprintf(errOut, "agents: go: %v\n", err)
		return 1
	}
	if _, err := addHandoff(e, []string{"--to", "implementer", instruction}, time.Now().UTC()); err != nil {
		fmt.Fprintf(errOut, "agents: go: %v\n", err)
		return 1
	}
	if !plain && e.isTTY {
		if inFile, outFile, ok := loopViewTerminals(in, out); ok {
			return runLoopWithView(inFile, outFile, errOut, e, task.Title, maxRuns)
		}
	}
	fmt.Fprintf(out, "task: %s\n", task.Title)
	fmt.Fprintf(out, "task path: %s\n", task.Path)
	fmt.Fprintln(out)
	return runLoopToStop("go", out, errOut, e, maxRuns, nil)
}

func parseGoArgs(args []string) (text, ref string, maxRuns int, plain bool, err error) {
	maxRuns = defaultMaxLoopRuns
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		switch arg {
		case "":
			continue
		case "--plain":
			plain = true
		case "--ref":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return "", "", 0, false, fmt.Errorf("--ref requires a value")
			}
			ref = strings.TrimSpace(args[i+1])
			i++
		case "--max-iterations":
			if i+1 >= len(args) {
				return "", "", 0, false, fmt.Errorf("--max-iterations requires a number")
			}
			n, convErr := strconv.Atoi(strings.TrimSpace(args[i+1]))
			if convErr != nil || n < 1 {
				return "", "", 0, false, fmt.Errorf("--max-iterations requires a positive number")
			}
			maxRuns = n
			i++
		default:
			if strings.HasPrefix(arg, "--") {
				return "", "", 0, false, fmt.Errorf("unknown flag %s", arg)
			}
			if text != "" {
				return "", "", 0, false, fmt.Errorf("go accepts exactly one instruction")
			}
			text = arg
		}
	}
	if text == "" {
		return "", "", 0, false, fmt.Errorf("instruction text is required, e.g. agents go \"Add /history command\"")
	}
	return text, ref, maxRuns, plain, nil
}

// splitGoText derives a task title from the instruction. "title: what to do"
// splits on the first ": " (a bare colon, as in URLs, does not split); plain
// text is both title and instruction.
func splitGoText(text string) (title, instruction string) {
	if idx := strings.Index(text, ": "); idx > 0 {
		title = strings.TrimSpace(text[:idx])
		instruction = strings.TrimSpace(text[idx+2:])
		if title != "" && instruction != "" {
			return title, instruction
		}
	}
	return text, text
}

func runTaskCommand(args []string, out, errOut io.Writer, e env) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "agents: task command requires a subcommand")
		fmt.Fprintln(errOut, "Run `agents help` for usage.")
		return 2
	}

	switch args[0] {
	case "open":
		task, err := openTask(e, args[1:], time.Now().UTC())
		if err != nil {
			fmt.Fprintf(errOut, "agents: task open: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "opened task: %s\n", task.Title)
		fmt.Fprintf(out, "task path: %s\n", task.Path)
		return 0
	case "use":
		if len(args) != 2 {
			fmt.Fprintln(errOut, "agents: task use requires exactly one task path")
			return 2
		}
		task, err := useTask(e, args[1])
		if err != nil {
			fmt.Fprintf(errOut, "agents: task use: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "active task: %s\n", task.Title)
		fmt.Fprintf(out, "task path: %s\n", task.Path)
		return 0
	case "scope", "status", "next":
		relPath, err := setTaskChunk(e, args[0], args[1:])
		if err != nil {
			fmt.Fprintf(errOut, "agents: task %s: %v\n", args[0], err)
			return 1
		}
		fmt.Fprintf(out, "updated: %s\n", relPath)
		return 0
	default:
		fmt.Fprintf(errOut, "agents: unknown task command: %s\n", args[0])
		return 2
	}
}

// setTaskChunk replaces the scope.md, status.md, or next.md chunk of the
// active task so task state is maintained through the CLI instead of
// hand-editing.
func setTaskChunk(e env, kind string, args []string) (string, error) {
	headings := map[string]string{
		"scope":  "Scope",
		"status": "Status",
		"next":   "Next",
	}
	heading, ok := headings[kind]
	if !ok {
		return "", fmt.Errorf("unknown task chunk %q", kind)
	}
	text, err := parseSingleTextArg(args, kind+" text")
	if err != nil {
		return "", err
	}
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
	body := fmt.Sprintf("# %s\n\n%s\n", heading, strings.TrimRight(text, "\n"))
	relPath := filepath.Join(active.Path, kind+".md")
	if err := os.WriteFile(filepath.Join(taskDir, kind+".md"), []byte(body), 0o644); err != nil {
		return "", err
	}
	return relPath, nil
}

func openTask(e env, args []string, now time.Time) (activeTask, error) {
	title, ref, err := parseTaskOpenArgs(args)
	if err != nil {
		return activeTask{}, err
	}
	date := now.Format("2006-01-02")
	seq, err := nextTaskSequence(filepath.Join(e.cwd, stateDir, "tasks"), date)
	if err != nil {
		return activeTask{}, err
	}

	slug := slugify(title)
	if slug == "" {
		slug = "task"
	}
	dirName := fmt.Sprintf("%s-%03d-%s", date, seq, slug)
	if ref != "" {
		dirName += "--" + slugify(ref)
	}
	cleanPath := filepath.Join("tasks", dirName)
	taskDir := filepath.Join(e.cwd, stateDir, cleanPath)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return activeTask{}, err
	}
	if err := writeTaskFiles(taskDir, title, ref, date); err != nil {
		return activeTask{}, err
	}
	if err := writeActiveTask(e.cwd, cleanPath); err != nil {
		return activeTask{}, err
	}

	return activeTask{Path: cleanPath, Title: title, Status: "active"}, nil
}

func parseTaskOpenArgs(args []string) (title, ref string, err error) {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		switch arg {
		case "":
			continue
		case "--ref":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return "", "", fmt.Errorf("--ref requires a value")
			}
			ref = strings.TrimSpace(args[i+1])
			i++
		default:
			if strings.HasPrefix(arg, "--") {
				return "", "", fmt.Errorf("unknown flag %s", arg)
			}
			if title != "" {
				return "", "", fmt.Errorf("task title must be one argument")
			}
			title = arg
		}
	}
	if title == "" {
		return "", "", fmt.Errorf("task title is required")
	}
	return title, ref, nil
}

func nextTaskSequence(tasksDir, date string) (int, error) {
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 1, nil
		}
		return 0, err
	}
	maxSeq := 0
	prefix := date + "-"
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		rest := strings.TrimPrefix(entry.Name(), prefix)
		if len(rest) < 3 {
			continue
		}
		seq, err := strconv.Atoi(rest[:3])
		if err == nil && seq > maxSeq {
			maxSeq = seq
		}
	}
	return maxSeq + 1, nil
}

func writeTaskFiles(taskDir, title, ref, date string) error {
	var task strings.Builder
	fmt.Fprintf(&task, "# %s\n\n", title)
	if ref != "" {
		fmt.Fprintf(&task, "ref: %s\n", ref)
	}
	fmt.Fprintf(&task, "date: %s\n", date)
	fmt.Fprintln(&task, "status: active")

	if err := os.WriteFile(filepath.Join(taskDir, "task.md"), []byte(task.String()), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(taskDir, "scope.md"), []byte("# Scope\n\n- define scope\n"), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(taskDir, "status.md"), []byte("# Status\n\nStarting task.\n"), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(taskDir, "next.md"), []byte("# Next\n\n- decide next step\n"), 0o644)
}

func useTask(e env, activePath string) (activeTask, error) {
	cleanPath, ok := cleanActivePath(activePath)
	if !ok {
		return activeTask{}, fmt.Errorf("task path must stay inside .agents")
	}
	taskMarkdown := filepath.Join(e.cwd, stateDir, cleanPath, "task.md")
	if _, err := os.Stat(taskMarkdown); err != nil {
		if os.IsNotExist(err) {
			return activeTask{}, fmt.Errorf("task not found: %s", cleanPath)
		}
		return activeTask{}, err
	}
	if err := writeActiveTask(e.cwd, cleanPath); err != nil {
		return activeTask{}, err
	}
	task := readActiveTask(e.cwd)
	if task.Path == "" {
		return activeTask{}, fmt.Errorf("could not read active task after switch")
	}
	return task, nil
}

func writeActiveTask(cwd, cleanPath string) error {
	activeFile := filepath.Join(cwd, stateDir, "active")
	if err := os.MkdirAll(filepath.Dir(activeFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(activeFile, []byte(cleanPath+"\n"), 0o644)
}

// maxSlugBytes bounds slug length so task and chunk filenames stay well under
// filesystem name limits (255 bytes) even with date prefixes and refs.
const maxSlugBytes = 80

func slugify(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	for len(slug) > maxSlugBytes {
		runes := []rune(slug)
		slug = strings.TrimRight(string(runes[:len(runes)-1]), "-")
	}
	return slug
}

func runDecisionCommand(args []string, out, errOut io.Writer, e env) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "agents: decision command requires a subcommand")
		return 2
	}
	switch args[0] {
	case "add":
		record, err := addDecision(e, args[1:], time.Now().UTC())
		if err != nil {
			fmt.Fprintf(errOut, "agents: decision add: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "decision: %s\n", record.Title)
		fmt.Fprintf(out, "path: %s\n", record.Path)
		return 0
	default:
		fmt.Fprintf(errOut, "agents: unknown decision command: %s\n", args[0])
		return 2
	}
}

func runHandoffCommand(args []string, out, errOut io.Writer, e env) int {
	record, err := addHandoff(e, args, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(errOut, "agents: handoff: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "handoff to: %s\n", record.To)
	fmt.Fprintf(out, "path: %s\n", record.Path)
	return 0
}

type chunkRecord struct {
	Path  string
	Title string
	To    string
	By    string
}

func addDecision(e env, args []string, now time.Time) (chunkRecord, error) {
	by, rest, err := extractByFlag(args)
	if err != nil {
		return chunkRecord{}, err
	}
	actor, err := resolveActor(e, by)
	if err != nil {
		return chunkRecord{}, err
	}
	text, err := parseSingleTextArg(rest, "decision text")
	if err != nil {
		return chunkRecord{}, err
	}
	title := text
	if len(title) > 80 {
		title = strings.TrimSpace(title[:80])
	}
	body := fmt.Sprintf("# Decision: %s\n\nStatus: proposed\nBy: %s\n%s\n%s\n", title, actor, agentLine(e), text)
	record, err := writeActiveTaskChunk(e, "decisions", title, body, now)
	if err != nil {
		return chunkRecord{}, err
	}
	return record, nil
}

func addHandoff(e env, args []string, now time.Time) (chunkRecord, error) {
	by, rest, err := extractByFlag(args)
	if err != nil {
		return chunkRecord{}, err
	}
	actor, err := resolveActor(e, by)
	if err != nil {
		return chunkRecord{}, err
	}
	to, text, err := parseHandoffArgs(rest)
	if err != nil {
		return chunkRecord{}, err
	}
	title := "Handoff to " + to
	body := fmt.Sprintf("# Handoff: %s\n\nTo: %s\nFrom: %s\n%s\n%s\n", to, to, actor, agentLine(e), text)
	record, err := writeActiveTaskChunk(e, "handoffs", title, body, now)
	if err != nil {
		return chunkRecord{}, err
	}
	record.To = to
	return record, nil
}

func parseSingleTextArg(args []string, name string) (string, error) {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return "", fmt.Errorf("%s must be one argument", name)
	}
	return strings.TrimSpace(args[0]), nil
}

func parseHandoffArgs(args []string) (to, text string, err error) {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		switch arg {
		case "":
			continue
		case "--to":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return "", "", fmt.Errorf("--to requires a role")
			}
			to = strings.TrimSpace(args[i+1])
			i++
		default:
			if strings.HasPrefix(arg, "--") {
				return "", "", fmt.Errorf("unknown flag %s", arg)
			}
			if text != "" {
				return "", "", fmt.Errorf("handoff text must be one argument")
			}
			text = arg
		}
	}
	if to == "" {
		return "", "", fmt.Errorf("--to is required")
	}
	if text == "" {
		return "", "", fmt.Errorf("handoff text is required")
	}
	return to, text, nil
}

func writeActiveTaskChunk(e env, kind, title, body string, now time.Time) (chunkRecord, error) {
	active := readActiveTask(e.cwd)
	if active.Path == "" {
		return chunkRecord{}, fmt.Errorf("no active task")
	}
	taskDir := filepath.Join(e.cwd, stateDir, active.Path)
	if _, err := os.Stat(filepath.Join(taskDir, "task.md")); err != nil {
		if os.IsNotExist(err) {
			return chunkRecord{}, fmt.Errorf("active task not found: %s", active.Path)
		}
		return chunkRecord{}, err
	}

	dir := filepath.Join(taskDir, kind)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return chunkRecord{}, err
	}
	stamp := now.Format("2006-01-02T150405Z")
	name := stamp + "-" + slugify(title)
	if name == stamp+"-" {
		name += "note"
	}
	name, err := uniqueChunkName(dir, name)
	if err != nil {
		return chunkRecord{}, err
	}
	relPath := filepath.Join(active.Path, kind, name+".md")
	path := filepath.Join(e.cwd, stateDir, relPath)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return chunkRecord{}, err
	}
	return chunkRecord{Path: relPath, Title: title}, nil
}

func uniqueChunkName(dir, base string) (string, error) {
	name := base
	for i := 2; ; i++ {
		path := filepath.Join(dir, name+".md")
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return name, nil
			}
			return "", err
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
}

func runFindingCommand(args []string, out, errOut io.Writer, e env) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "agents: finding command requires a subcommand")
		return 2
	}
	switch args[0] {
	case "add":
		record, err := addFinding(e, args[1:], time.Now().UTC())
		if err != nil {
			fmt.Fprintf(errOut, "agents: finding add: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "finding: %s\n", record.Title)
		fmt.Fprintf(out, "path: %s\n", record.Path)
		return 0
	case "fix":
		record, err := updateFindingStatus(e, args[1:], "fixed", "Fixed", time.Now().UTC())
		if err != nil {
			fmt.Fprintf(errOut, "agents: finding fix: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "fixed finding: %s\n", record.Path)
		return 0
	case "verify":
		record, err := updateFindingStatus(e, args[1:], "verified", "Verification", time.Now().UTC())
		if err != nil {
			fmt.Fprintf(errOut, "agents: finding verify: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "verified finding: %s\n", record.Path)
		fmt.Fprintf(out, "verified by: %s\n", record.By)
		return 0
	default:
		fmt.Fprintf(errOut, "agents: unknown finding command: %s\n", args[0])
		return 2
	}
}

func addFinding(e env, args []string, now time.Time) (chunkRecord, error) {
	by, rest, err := extractByFlag(args)
	if err != nil {
		return chunkRecord{}, err
	}
	actor, err := resolveActor(e, by)
	if err != nil {
		return chunkRecord{}, err
	}
	text, severity, err := parseFindingAddArgs(rest)
	if err != nil {
		return chunkRecord{}, err
	}
	title := text
	if len(title) > 80 {
		title = strings.TrimSpace(title[:80])
	}
	body := fmt.Sprintf("# Finding: %s\n\nStatus: open\nSeverity: %s\nCreated by: %s\n%s\n%s\n", title, severity, actor, agentLine(e), text)
	return writeActiveTaskChunk(e, "findings", title, body, now)
}

func parseFindingAddArgs(args []string) (text, severity string, err error) {
	severity = "medium"
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		switch arg {
		case "":
			continue
		case "--severity":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return "", "", fmt.Errorf("--severity requires a value")
			}
			severity = strings.ToLower(strings.TrimSpace(args[i+1]))
			if severity != "low" && severity != "medium" && severity != "high" {
				return "", "", fmt.Errorf("severity must be low, medium, or high")
			}
			i++
		default:
			if strings.HasPrefix(arg, "--") {
				return "", "", fmt.Errorf("unknown flag %s", arg)
			}
			if text != "" {
				return "", "", fmt.Errorf("finding text must be one argument")
			}
			text = arg
		}
	}
	if text == "" {
		return "", "", fmt.Errorf("finding text is required")
	}
	return text, severity, nil
}

func updateFindingStatus(e env, args []string, status, section string, now time.Time) (chunkRecord, error) {
	by, rest, err := extractByFlag(args)
	if err != nil {
		return chunkRecord{}, err
	}
	evidence, rest, err := extractFlagValue(rest, "--evidence")
	if err != nil {
		return chunkRecord{}, err
	}
	actor, err := resolveActor(e, by)
	if err != nil {
		return chunkRecord{}, err
	}
	findingArg, err := parseSingleTextArg(rest, "finding path")
	if err != nil {
		return chunkRecord{}, err
	}
	active := readActiveTask(e.cwd)
	if active.Path == "" {
		return chunkRecord{}, fmt.Errorf("no active task")
	}
	relPath, path, err := resolveFindingPath(e, active.Path, findingArg)
	if err != nil {
		return chunkRecord{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return chunkRecord{}, err
	}
	markdown := string(data)
	current := findingStatus(markdown)
	switch status {
	case "fixed":
		if current == "verified" {
			return chunkRecord{}, fmt.Errorf("finding is already verified")
		}
	case "verified":
		if current != "fixed" {
			return chunkRecord{}, fmt.Errorf("finding must be marked fixed before it can be verified (current status: %s)", current)
		}
		fixedBy := findingSectionActor(markdown, "Fixed")
		if fixedBy != "" && fixedBy == actor {
			return chunkRecord{}, fmt.Errorf("finding was fixed by %q; verification requires a different actor (set AGENTS_ACTOR or pass --by)", fixedBy)
		}
	}
	updated := replaceStatusLine(markdown, status)
	var b strings.Builder
	b.WriteString(strings.TrimRight(updated, "\n"))
	fmt.Fprintf(&b, "\n\n## %s\n\nBy: %s\n%sRecorded at: %s\n", section, actor, agentLine(e), now.Format(time.RFC3339))
	if strings.TrimSpace(evidence) != "" {
		fmt.Fprintf(&b, "Evidence: %s\n", strings.TrimSpace(evidence))
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return chunkRecord{}, err
	}
	return chunkRecord{Path: relPath, By: actor}, nil
}

// extractFlagValue removes a `flag value` pair from args.
func extractFlagValue(args []string, flag string) (value string, rest []string, err error) {
	for i := 0; i < len(args); i++ {
		if strings.TrimSpace(args[i]) != flag {
			rest = append(rest, args[i])
			continue
		}
		if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
			return "", nil, fmt.Errorf("%s requires a value", flag)
		}
		value = strings.TrimSpace(args[i+1])
		i++
	}
	return value, rest, nil
}

// findingSectionActor returns the actor recorded by the most recent
// "## <section>" block in a finding chunk.
func findingSectionActor(markdown, section string) string {
	lines := strings.Split(markdown, "\n")
	heading := "## " + strings.ToLower(section)
	actor := ""
	inSection := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "## ") {
			inSection = lower == heading
			continue
		}
		if inSection && strings.HasPrefix(lower, "by:") {
			actor = strings.TrimSpace(trimmed[len("by:"):])
		}
	}
	return actor
}

func resolveFindingPath(e env, activePath, findingArg string) (relPath, absPath string, err error) {
	cleaned := filepath.Clean(strings.TrimSpace(findingArg))
	if cleaned == "." || filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("finding path must stay inside active task findings")
	}
	if !strings.HasSuffix(cleaned, ".md") {
		cleaned += ".md"
	}
	if strings.Contains(cleaned, string(filepath.Separator)) {
		cleaned, ok := cleanActivePath(cleaned)
		if !ok {
			return "", "", fmt.Errorf("finding path must stay inside active task findings")
		}
		prefix := filepath.Join(activePath, "findings") + string(filepath.Separator)
		if !strings.HasPrefix(cleaned, prefix) {
			return "", "", fmt.Errorf("finding path must stay inside active task findings")
		}
		relPath = cleaned
	} else {
		relPath = filepath.Join(activePath, "findings", cleaned)
	}
	absPath = filepath.Join(e.cwd, stateDir, relPath)
	if _, err := os.Stat(absPath); err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("finding not found: %s", findingArg)
		}
		return "", "", err
	}
	return relPath, absPath, nil
}

func replaceStatusLine(markdown, status string) string {
	lines := strings.Split(markdown, "\n")
	replaced := false
	for i, line := range lines {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "status:") {
			lines[i] = "Status: " + status
			replaced = true
			break
		}
	}
	if !replaced {
		return "Status: " + status + "\n" + markdown
	}
	return strings.Join(lines, "\n")
}

// runVerifyCommand records verification evidence as a chunk under the active
// task. With --cmd it executes the command and records the exit code and
// output, so the evidence is reproducible instead of claimed.
func runVerifyCommand(args []string, out, errOut io.Writer, e env) int {
	record, passed, err := addVerification(e, args, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(errOut, "agents: verify: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "verification: %s\n", record.Title)
	fmt.Fprintf(out, "path: %s\n", record.Path)
	fmt.Fprintf(out, "by: %s\n", record.By)
	if !passed {
		fmt.Fprintln(out, "result: fail")
		return 1
	}
	fmt.Fprintln(out, "result: pass")
	return 0
}

const verificationOutputLimit = 4000

func addVerification(e env, args []string, now time.Time) (chunkRecord, bool, error) {
	by, rest, err := extractByFlag(args)
	if err != nil {
		return chunkRecord{}, false, err
	}
	command, rest, err := extractFlagValue(rest, "--cmd")
	if err != nil {
		return chunkRecord{}, false, err
	}
	actor, err := resolveActor(e, by)
	if err != nil {
		return chunkRecord{}, false, err
	}
	text, err := parseSingleTextArg(rest, "verification text")
	if err != nil {
		return chunkRecord{}, false, err
	}
	title := text
	if len(title) > 80 {
		title = strings.TrimSpace(title[:80])
	}
	result := "pass"
	var commandSection string
	if strings.TrimSpace(command) != "" {
		// Refuse to execute the evidence command when there is no active task
		// to record the result into.
		if active := readActiveTask(e.cwd); active.Path == "" {
			return chunkRecord{}, false, fmt.Errorf("no active task")
		}
		output, exitCode, runErr := runVerificationCommand(e.cwd, command)
		if runErr != nil {
			return chunkRecord{}, false, runErr
		}
		if exitCode != 0 {
			result = "fail"
		}
		var section strings.Builder
		fmt.Fprintf(&section, "\n## Command\n\n$ %s\nexit: %d\n", command, exitCode)
		if trimmed := strings.TrimSpace(output); trimmed != "" {
			if len(trimmed) > verificationOutputLimit {
				trimmed = "...\n" + trimmed[len(trimmed)-verificationOutputLimit:]
			}
			fmt.Fprintf(&section, "\n```\n%s\n```\n", trimmed)
		}
		commandSection = section.String()
	}
	body := fmt.Sprintf("# Verification: %s\n\nResult: %s\nBy: %s\n%sRecorded at: %s\n\n%s\n%s",
		title, result, actor, agentLine(e), now.Format(time.RFC3339), text, commandSection)
	record, err := writeActiveTaskChunk(e, "verification", title, body, now)
	if err != nil {
		return chunkRecord{}, false, err
	}
	record.By = actor
	return record, result == "pass", nil
}

// verificationCommandTimeout caps evidence command runtime so a hung command
// cannot stall the CLI or the managed run loop. Var so tests can shrink it.
var verificationCommandTimeout = 10 * time.Minute

// runVerificationCommand executes an evidence command and returns its
// combined output and exit code. A non-zero exit is evidence, not an error.
// Commands that exceed verificationCommandTimeout are killed and reported as
// exit 124 with a timeout note appended to the output.
func runVerificationCommand(cwd, command string) (string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), verificationCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = cwd
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		note := fmt.Sprintf("%s\n[agents] command timed out after %s", strings.TrimRight(string(output), "\n"), verificationCommandTimeout)
		return note, 124, nil
	}
	if err == nil {
		return string(output), 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(output), exitErr.ExitCode(), nil
	}
	return string(output), -1, err
}

func runArchiveCommand(args []string, out, errOut io.Writer, e env) int {
	if len(args) != 0 {
		fmt.Fprintln(errOut, "agents: archive does not accept arguments yet")
		return 2
	}
	record, err := archiveActiveTask(e, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(errOut, "agents: archive: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "archived task: %s\n", record.Title)
	fmt.Fprintf(out, "archive path: %s\n", record.Path)
	return 0
}

func archiveActiveTask(e env, now time.Time) (chunkRecord, error) {
	active := readActiveTask(e.cwd)
	if active.Path == "" {
		return chunkRecord{}, fmt.Errorf("no active task")
	}
	taskDir := filepath.Join(e.cwd, stateDir, active.Path)
	if _, err := os.Stat(filepath.Join(taskDir, "task.md")); err != nil {
		if os.IsNotExist(err) {
			return chunkRecord{}, fmt.Errorf("active task not found: %s", active.Path)
		}
		return chunkRecord{}, err
	}

	archiveRel := filepath.Join("archive", filepath.Base(active.Path))
	archiveDir := filepath.Join(e.cwd, stateDir, archiveRel)
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return chunkRecord{}, err
	}
	if err := writeArchiveSummary(taskDir, archiveDir, active, now); err != nil {
		return chunkRecord{}, err
	}
	if err := copyMarkdownDir(filepath.Join(taskDir, "decisions"), filepath.Join(archiveDir, "decisions"), nil); err != nil {
		return chunkRecord{}, err
	}
	if err := copyMarkdownDir(filepath.Join(taskDir, "findings"), filepath.Join(archiveDir, "findings"), shouldKeepArchivedFinding); err != nil {
		return chunkRecord{}, err
	}
	if err := os.RemoveAll(taskDir); err != nil {
		return chunkRecord{}, err
	}
	if err := clearActiveTask(e.cwd); err != nil {
		return chunkRecord{}, err
	}
	return chunkRecord{Path: archiveRel, Title: active.Title}, nil
}

func writeArchiveSummary(taskDir, archiveDir string, active activeTask, now time.Time) error {
	decisionCount, err := countMarkdownFiles(filepath.Join(taskDir, "decisions"))
	if err != nil {
		return err
	}
	handoffCount, err := countMarkdownFiles(filepath.Join(taskDir, "handoffs"))
	if err != nil {
		return err
	}
	verificationCount, err := countMarkdownFiles(filepath.Join(taskDir, "verification"))
	if err != nil {
		return err
	}
	openFindings, fixedFindings, verifiedFindings, err := countFindingStatuses(filepath.Join(taskDir, "findings"))
	if err != nil {
		return err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", active.Title)
	fmt.Fprintln(&b, "status: archived")
	if active.Status != "" {
		fmt.Fprintf(&b, "previous_status: %s\n", active.Status)
	}
	fmt.Fprintf(&b, "archived_at: %s\n", now.Format(time.RFC3339))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Summary:")
	fmt.Fprintf(&b, "- decisions: %d\n", decisionCount)
	fmt.Fprintf(&b, "- handoffs: %d\n", handoffCount)
	fmt.Fprintf(&b, "- verification records: %d\n", verificationCount)
	fmt.Fprintf(&b, "- findings open: %d\n", openFindings)
	fmt.Fprintf(&b, "- findings fixed: %d\n", fixedFindings)
	fmt.Fprintf(&b, "- findings verified: %d\n", verifiedFindings)
	fmt.Fprintln(&b)
	appendArchiveSection(&b, "Scope", filepath.Join(taskDir, "scope.md"))
	appendArchiveSection(&b, "Final Status", filepath.Join(taskDir, "status.md"))
	fmt.Fprintln(&b, "Detailed transient chunks remain available in git history.")
	return os.WriteFile(filepath.Join(archiveDir, "summary.md"), []byte(b.String()), 0o644)
}

func appendArchiveSection(b *strings.Builder, title, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	body := strings.TrimSpace(stripFirstMarkdownHeading(string(data)))
	if body == "" {
		return
	}
	fmt.Fprintf(b, "## %s\n\n", title)
	fmt.Fprintln(b, body)
	fmt.Fprintln(b)
}

func stripFirstMarkdownHeading(markdown string) string {
	lines := strings.Split(markdown, "\n")
	if len(lines) == 0 {
		return markdown
	}
	if strings.HasPrefix(strings.TrimSpace(lines[0]), "# ") {
		return strings.Join(lines[1:], "\n")
	}
	return markdown
}

func countMarkdownFiles(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			count++
		}
	}
	return count, nil
}

func countFindingStatuses(dir string) (openCount, fixedCount, verifiedCount int, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, 0, nil
		}
		return 0, 0, 0, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return 0, 0, 0, err
		}
		switch findingStatus(string(data)) {
		case "verified":
			verifiedCount++
		case "fixed":
			fixedCount++
		default:
			openCount++
		}
	}
	return openCount, fixedCount, verifiedCount, nil
}

func findingStatus(markdown string) string {
	for _, line := range strings.Split(markdown, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(t), "status:") {
			return strings.ToLower(strings.TrimSpace(t[len("status:"):]))
		}
	}
	return "open"
}

func copyMarkdownDir(src, dst string, keep func(string) bool) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	wrote := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, entry.Name()))
		if err != nil {
			return err
		}
		if keep != nil && !keep(string(data)) {
			continue
		}
		if !wrote {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
			wrote = true
		}
		if err := os.WriteFile(filepath.Join(dst, entry.Name()), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func shouldKeepArchivedFinding(markdown string) bool {
	return findingStatus(markdown) != "verified"
}

func clearActiveTask(cwd string) error {
	activeFile := filepath.Join(cwd, stateDir, "active")
	if err := os.MkdirAll(filepath.Dir(activeFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(activeFile, []byte(""), 0o644)
}

func runDispatchCommand(args []string, out, errOut io.Writer, e env) int {
	role, jsonOut, err := parseDispatchArgs(args)
	if err != nil {
		fmt.Fprintf(errOut, "agents: dispatch: %v\n", err)
		return 2
	}
	if role == "" {
		fmt.Fprintln(errOut, "agents: dispatch requires exactly one role")
		return 2
	}
	packet, err := dispatchPacket(e, role)
	if err != nil {
		fmt.Fprintf(errOut, "agents: dispatch: %v\n", err)
		return 1
	}
	if jsonOut {
		if err := writeJSON(out, dispatchPayload(packet)); err != nil {
			fmt.Fprintf(errOut, "agents: dispatch: %v\n", err)
			return 1
		}
		return 0
	}
	printDispatchPacket(out, packet)
	return 0
}

const defaultMaxLoopRuns = 6

type runFlags struct {
	JSON       bool
	JSONEvents bool
	Once       bool
	MaxRuns    int
}

func parseRunArgs(args []string) (runFlags, error) {
	flags := runFlags{MaxRuns: defaultMaxLoopRuns}
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		switch arg {
		case "":
			continue
		case "--json":
			flags.JSON = true
		case "--json-events":
			flags.JSONEvents = true
		case "--once":
			flags.Once = true
		case "--max-iterations":
			if i+1 >= len(args) {
				return flags, fmt.Errorf("--max-iterations requires a number")
			}
			n, err := strconv.Atoi(strings.TrimSpace(args[i+1]))
			if err != nil || n < 1 {
				return flags, fmt.Errorf("--max-iterations requires a positive number")
			}
			flags.MaxRuns = n
			i++
		default:
			return flags, fmt.Errorf("unknown flag %s", arg)
		}
	}
	if flags.JSONEvents && (flags.JSON || flags.Once) {
		return flags, fmt.Errorf("--json-events cannot be combined with --json or --once")
	}
	return flags, nil
}

// loopStop describes why a managed loop stopped.
type loopStop struct {
	Reason string // human | coordinator | unconfigured | no-progress | max-runs
	State  loopState
	Runs   int
}

// loopSignature fingerprints coordination state so the loop can detect a role
// run that recorded no progress and stop instead of churning.
func loopSignature(state loopState) string {
	s := state.Stats
	return fmt.Sprintf("%s|%d|%d|%d|%d|%s|%s",
		state.NextRole, s.FindingsOpen, s.FindingsFixed, s.FindingsVerified,
		s.Verification, s.LatestHandoffName, s.LatestVerificationName)
}

// handoffChurn reports a run whose only recorded "progress" is another
// handoff to the same target as before: findings and verification unchanged,
// latest handoff target repeated. Without this, a do-nothing role that keeps
// writing handoffs counts as progress every round until the run budget.
func handoffChurn(before, after loopState) bool {
	b, a := before.Stats, after.Stats
	if b.FindingsOpen != a.FindingsOpen || b.FindingsFixed != a.FindingsFixed ||
		b.FindingsVerified != a.FindingsVerified || b.Verification != a.Verification {
		return false
	}
	if b.LatestVerificationName != a.LatestVerificationName {
		return false
	}
	return a.LatestHandoffTo != "" && a.LatestHandoffTo == b.LatestHandoffTo
}

// loopEvent is one lifecycle event from a managed loop, consumable by
// frontends (JSONL via `agents run --json-events`, future renderers).
type loopEvent struct {
	Event  string    `json:"event"` // state_changed | run_started | output | run_finished | stopped
	Time   string    `json:"time"`
	RunID  string    `json:"run_id,omitempty"`
	Role   string    `json:"role,omitempty"`
	Agent  string    `json:"agent,omitempty"`
	Chunk  string    `json:"chunk,omitempty"`
	Reason string    `json:"reason,omitempty"`
	Runs   *int      `json:"runs,omitempty"`
	Error  string    `json:"error,omitempty"`
	State  *loopJSON `json:"state,omitempty"`
}

// loopEventSink receives loop events as they happen. A nil sink disables
// event emission.
type loopEventSink func(loopEvent)

// jsonlEventSink encodes each event as one JSON line. Safe for concurrent
// emitters (run output streams while the loop owns state events).
func jsonlEventSink(w io.Writer) loopEventSink {
	var mu sync.Mutex
	encoder := json.NewEncoder(w)
	return func(ev loopEvent) {
		mu.Lock()
		defer mu.Unlock()
		ev.Time = time.Now().UTC().Format(time.RFC3339)
		_ = encoder.Encode(ev)
	}
}

// eventChunkWriter forwards run output to the event sink as output chunks.
type eventChunkWriter struct {
	events loopEventSink
	runID  string
	role   string
}

func (w *eventChunkWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.events(loopEvent{Event: "output", RunID: w.runID, Role: w.role, Chunk: string(p)})
	}
	return len(p), nil
}

func statePayload(state loopState) *loopJSON {
	payload := loopPayload(state)
	return &payload
}

// runManagedLoop drives configured roles until a stop condition: the human
// approval boundary, an unconfigured role, no recorded progress after a run,
// or the run budget. With stopAtCoordinator the loop yields to the caller
// instead of running the coordinator (used by the interactive terminal where
// coordinator turns belong to chat). A non-nil control lets the caller stop
// the loop between role runs (quit/pause from the unified view); the current
// run always completes so records stay consistent.
type loopDisplayMode int

const (
	loopDisplayPlain loopDisplayMode = iota
	loopDisplayInteractive
)

func runManagedLoop(e env, out io.Writer, maxRuns int, stopAtCoordinator bool, events loopEventSink, control *loopControl, display loopDisplayMode) (loopStop, error) {
	emit := func(ev loopEvent) {
		if events != nil {
			events(ev)
		}
	}
	stopped := func(stop loopStop) loopStop {
		runs := stop.Runs
		emit(loopEvent{Event: "stopped", Reason: stop.Reason, Runs: &runs, State: statePayload(stop.State)})
		return stop
	}
	runs := 0
	for {
		state, err := analyzeLoopState(e)
		if err != nil {
			return loopStop{}, err
		}
		emit(loopEvent{Event: "state_changed", State: statePayload(state)})
		if control != nil {
			if reason := control.requested(); reason != "" {
				return stopped(loopStop{Reason: reason, State: state, Runs: runs}), nil
			}
		}
		switch state.NextRole {
		case "human":
			return stopped(loopStop{Reason: "human", State: state, Runs: runs}), nil
		case "coordinator":
			if stopAtCoordinator {
				return stopped(loopStop{Reason: "coordinator", State: state, Runs: runs}), nil
			}
		case "implementer", "reviewer":
		default:
			return loopStop{}, fmt.Errorf("cannot run role %q", state.NextRole)
		}
		if runs >= maxRuns {
			return stopped(loopStop{Reason: "max-runs", State: state, Runs: runs}), nil
		}
		agent, err := configuredAgent(e.cwd, state.NextRole)
		if err != nil {
			return loopStop{}, err
		}
		if strings.TrimSpace(agent) == "" {
			return stopped(loopStop{Reason: "unconfigured", State: state, Runs: runs}), nil
		}
		if display == loopDisplayInteractive {
			printInteractiveRoleHeader(out, state.NextRole)
		} else {
			fmt.Fprintf(out, "\nrunning %s...\n", state.NextRole)
		}
		liveOut := out
		if display == loopDisplayInteractive {
			liveOut = &interactiveRunHeaderWriter{out: out}
		}
		result, err := runConfiguredRoleWithOptions(e, state, roleRunOptions{
			ShowRunHeader: events == nil,
			LiveOutput:    liveOut,
			Events:        events,
		})
		if err != nil {
			return loopStop{}, err
		}
		runs++
		if display == loopDisplayInteractive {
			printInteractiveRoleRunResult(out, result)
		} else {
			printRoleRunResult(out, result)
		}
		if loopSignature(result.After) == loopSignature(state) || handoffChurn(state, result.After) {
			return stopped(loopStop{Reason: "no-progress", State: result.After, Runs: runs}), nil
		}
	}
}

func runLoopCommand(args []string, out, errOut io.Writer, e env) int {
	flags, err := parseRunArgs(args)
	if err != nil {
		fmt.Fprintf(errOut, "agents: run: %v\n", err)
		return 2
	}
	state, err := analyzeLoopState(e)
	if err != nil {
		fmt.Fprintf(errOut, "agents: run: %v\n", err)
		return 1
	}
	if flags.JSON {
		if err := writeJSON(out, loopPayload(state)); err != nil {
			fmt.Fprintf(errOut, "agents: run: %v\n", err)
			return 1
		}
		return 0
	}
	// run executes shell command templates from .agents/config.yml, so an
	// untrusted workspace (e.g. a freshly cloned repo) must refuse before
	// anything is dispatched.
	trusted, err := isTrusted(e)
	if err != nil {
		fmt.Fprintf(errOut, "agents: run: %v\n", err)
		return 1
	}
	if !trusted {
		fmt.Fprintf(errOut, "agents: run: %s is not trusted; `agents run` executes commands from .agents/config.yml — review it, then run `agents trust`\n", e.cwd)
		return 1
	}
	if flags.Once {
		return runLoopOnce(out, errOut, e, state)
	}
	// With --json-events the JSONL stream owns stdout: plain loop printing is
	// discarded and every lifecycle change is emitted as an event instead.
	var events loopEventSink
	loopOut := out
	if flags.JSONEvents {
		events = jsonlEventSink(out)
		loopOut = io.Discard
	}
	return runLoopToStop("run", loopOut, errOut, e, flags.MaxRuns, events)
}

// runLoopToStop drives the managed loop until a stop condition and translates
// the stop into plain output and an exit code. Shared by `agents run` and
// `agents go`; cmd names the invoking command in error output.
func runLoopToStop(cmd string, loopOut, errOut io.Writer, e env, maxRuns int, events loopEventSink) int {
	stop, err := runManagedLoop(e, loopOut, maxRuns, false, events, nil, loopDisplayPlain)
	if err != nil {
		if errors.Is(err, errRunInterrupted) {
			printInterruptHint(loopOut, e)
			return 130
		}
		fmt.Fprintf(errOut, "agents: %s: %v\n", cmd, err)
		return 1
	}
	switch stop.Reason {
	case "human":
		fmt.Fprintln(loopOut)
		printLoopState(loopOut, stop.State)
		return 0
	case "unconfigured":
		printLoopState(loopOut, stop.State)
		fmt.Fprintf(loopOut, "agent: not configured for %s\n", stop.State.NextRole)
		return 0
	case "no-progress":
		fmt.Fprintf(loopOut, "\nPaused: %s did not record new task progress.\n", titleRole(stop.State.NextRole))
		fmt.Fprintln(loopOut, "Inspect the run log, keep planning, or run again when ready.")
		printLoopState(loopOut, stop.State)
		return 1
	case "max-runs":
		fmt.Fprintf(loopOut, "\nLoop paused after %d role runs with work remaining; rerun `agents run` to continue.\n", stop.Runs)
		printLoopState(loopOut, stop.State)
		return 1
	default:
		return 0
	}
}

// runLoopOnce preserves single-step dispatch for scripts that drive the loop
// themselves.
func runLoopOnce(out, errOut io.Writer, e env, state loopState) int {
	if state.NextRole == "human" {
		printLoopState(out, state)
		return 0
	}
	agent, err := configuredAgent(e.cwd, state.NextRole)
	if err != nil {
		fmt.Fprintf(errOut, "agents: run: %v\n", err)
		return 1
	}
	if strings.TrimSpace(agent) == "" {
		printLoopState(out, state)
		fmt.Fprintf(out, "agent: not configured for %s\n", state.NextRole)
		return 0
	}
	result, err := runConfiguredRole(e, state, out)
	if err != nil {
		if errors.Is(err, errRunInterrupted) {
			printInterruptHint(out, e)
			return 130
		}
		fmt.Fprintf(errOut, "agents: run: %v\n", err)
		return 1
	}
	printRoleRunResult(out, result)
	return 0
}

func parseDispatchArgs(args []string) (role string, jsonOut bool, err error) {
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		switch trimmed {
		case "":
			continue
		case "--json":
			jsonOut = true
		default:
			if strings.HasPrefix(trimmed, "--") {
				return "", false, fmt.Errorf("unknown flag %s", trimmed)
			}
			if role != "" {
				return "", false, fmt.Errorf("dispatch requires exactly one role")
			}
			role = strings.ToLower(trimmed)
		}
	}
	return role, jsonOut, nil
}

type dispatchInfo struct {
	Role     string
	Agent    string
	Task     activeTask
	Stats    taskStats
	Action   string
	Boundary string
}

type loopState struct {
	Task       activeTask
	Stats      taskStats
	NextRole   string
	NextAction string
	Boundary   string
}

type roleRunResult struct {
	RunID        string
	Role         string
	Agent        string
	AgentDesc    string
	Command      string
	PromptPath   string
	LogPath      string
	Before       loopState
	After        loopState
	AgentOutput  string
	AgentSkipped bool
}

type roleRunOptions struct {
	ShowRunHeader bool
	LiveOutput    io.Writer
	Events        loopEventSink
}

type interactiveAgentReply struct {
	Text  string
	Usage string
}

type runArtifactPaths struct {
	ID     string
	Prompt string
	Log    string
}

func dispatchPacket(e env, role string) (dispatchInfo, error) {
	if role != "implementer" && role != "reviewer" && role != "coordinator" {
		return dispatchInfo{}, fmt.Errorf("unknown role: %s", role)
	}
	state, err := analyzeLoopState(e)
	if err != nil {
		return dispatchInfo{}, err
	}
	config, err := readConfig(e.cwd)
	if err != nil {
		return dispatchInfo{}, err
	}
	rc := config.Roles[role]
	info := dispatchInfo{
		Role:  role,
		Agent: agentDescriptor(rc.agentLabel(), roleSettings(rc, config.Settings)),
		Task:  state.Task,
		Stats: state.Stats,
	}
	switch role {
	case "implementer":
		if state.Stats.FindingsOpen > 0 {
			info.Action = "fix open findings, run relevant checks, then write a handoff"
		} else if state.Stats.LatestHandoffTo == "implementer" {
			info.Action = "act on latest handoff, run relevant checks, then write a handoff to reviewer"
		} else {
			info.Action = "implement the next task step, run relevant checks, then write a handoff"
		}
	case "reviewer":
		if state.Stats.LatestHandoffTo != "reviewer" {
			info.Action = "wait for an implementer handoff before reviewing"
			info.Boundary = "handoff required"
		} else {
			info.Action = "review code, tests, and task chunks; add findings or verify fixes"
		}
	case "coordinator":
		info.Action = "summarize task state, route the next role, and protect human approval boundaries"
	}
	return info, nil
}

func analyzeLoopState(e env) (loopState, error) {
	active := readActiveTask(e.cwd)
	if active.Path == "" {
		return loopState{}, fmt.Errorf("no active task")
	}
	stats, err := readTaskStats(e.cwd, active.Path)
	if err != nil {
		return loopState{}, err
	}
	state := loopState{Task: active, Stats: stats}
	switch {
	case stats.FindingsOpen > 0:
		state.NextRole = "implementer"
		state.NextAction = "fix open findings"
	case stats.FindingsFixed > 0:
		state.NextRole = "reviewer"
		state.NextAction = "verify fixed findings independently"
	case verificationCurrent(stats):
		state.NextRole = "human"
		state.NextAction = "approve archive, commit, or next task"
		state.Boundary = "human approval required"
	case stats.LatestHandoffTo == "reviewer":
		state.NextRole = "reviewer"
		state.NextAction = "review latest handoff"
	case stats.LatestHandoffTo == "implementer":
		state.NextRole = "implementer"
		state.NextAction = "act on latest handoff"
	case stats.LatestHandoffTo == "coordinator":
		state.NextRole = "coordinator"
		state.NextAction = "continue intake and planning"
	default:
		state.NextRole = "implementer"
		state.NextAction = "start or continue implementation"
	}
	return state, nil
}

// verificationCurrent reports whether passing verification evidence exists
// that is at least as new as the latest handoff. Only the UTC timestamp
// prefix of each chunk filename is compared — never the slug text — so
// same-second ties resolve in favor of the verification (the common flow is
// handoff first, evidence after). Stale evidence recorded before the latest
// handoff does not satisfy the human boundary.
func verificationCurrent(stats taskStats) bool {
	if stats.FindingsOpen > 0 || stats.FindingsFixed > 0 {
		return false
	}
	if stats.LatestVerificationName == "" {
		return false
	}
	return chunkStamp(stats.LatestVerificationName) >= chunkStamp(stats.LatestHandoffName)
}

// chunkStamp returns the leading UTC timestamp of a chunk filename
// (e.g. "2026-06-10T112546Z"), or the whole name if it is shorter.
func chunkStamp(name string) string {
	const stampLen = len("2006-01-02T150405Z")
	if len(name) >= stampLen {
		return name[:stampLen]
	}
	return name
}

func runConfiguredRole(e env, state loopState, out io.Writer) (roleRunResult, error) {
	return runConfiguredRoleWithOptions(e, state, roleRunOptions{ShowRunHeader: true, LiveOutput: out})
}

func runConfiguredRoleWithOptions(e env, state loopState, options roleRunOptions) (roleRunResult, error) {
	if state.NextRole != "implementer" && state.NextRole != "reviewer" && state.NextRole != "coordinator" {
		return roleRunResult{}, fmt.Errorf("cannot run role %q", state.NextRole)
	}
	config, err := readConfig(e.cwd)
	if err != nil {
		return roleRunResult{}, err
	}
	rc := config.Roles[state.NextRole]
	if !rc.configured() {
		return roleRunResult{}, fmt.Errorf("no agent configured for %s", state.NextRole)
	}
	commandTemplate, agent, effectiveSettings, err := resolveRoleCommand(rc, config.Settings)
	if err != nil {
		return roleRunResult{}, err
	}
	prompt, err := buildRolePrompt(e, state)
	if err != nil {
		return roleRunResult{}, err
	}
	artifacts, err := writePromptFile(e.cwd, state.NextRole, prompt)
	if err != nil {
		return roleRunResult{}, err
	}
	command := substitutePromptFile(commandTemplate, artifacts.Prompt)
	if err := ensureShellCommandAvailable(command, state.NextRole); err != nil {
		return roleRunResult{}, err
	}
	live := options.LiveOutput
	if live == nil {
		live = io.Discard
	}
	agentDesc := agentDescriptor(agent, effectiveSettings)
	if options.Events != nil {
		live = io.MultiWriter(live, &eventChunkWriter{events: options.Events, runID: artifacts.ID, role: state.NextRole})
		options.Events(loopEvent{Event: "run_started", RunID: artifacts.ID, Role: state.NextRole, Agent: agentDesc})
	}
	// After run_started, every error return must close the run with a
	// run_finished event so JSONL consumers can pair starts and finishes.
	failRun := func(err error) (roleRunResult, error) {
		if options.Events != nil {
			options.Events(loopEvent{Event: "run_finished", RunID: artifacts.ID, Role: state.NextRole, Agent: agentDesc, Error: err.Error()})
		}
		return roleRunResult{}, err
	}
	if options.ShowRunHeader {
		fmt.Fprintf(live, "run id: %s\n", artifacts.ID)
		fmt.Fprintf(live, "run log: %s\n\n", relPathFrom(e.cwd, artifacts.Log))
	}
	output, err := runStreamingShellCommand(e.cwd, command, artifacts, state, agent, effectiveSettings, live)
	if err != nil {
		return failRun(fmt.Errorf("%s command failed: %w\n%s", state.NextRole, err, strings.TrimSpace(output)))
	}
	output, err = applyRoleOutputDirectives(e, state.NextRole, output)
	if err != nil {
		return failRun(err)
	}
	after, err := analyzeLoopState(e)
	if err != nil {
		return failRun(err)
	}
	if options.Events != nil {
		options.Events(loopEvent{Event: "run_finished", RunID: artifacts.ID, Role: state.NextRole, Agent: agentDesc, State: statePayload(after)})
	}
	return roleRunResult{
		RunID:       artifacts.ID,
		Role:        state.NextRole,
		Agent:       agent,
		AgentDesc:   agentDesc,
		Command:     command,
		PromptPath:  artifacts.Prompt,
		LogPath:     artifacts.Log,
		Before:      state,
		After:       after,
		AgentOutput: output,
	}, nil
}

func resolveAgentCommand(agent string, settings agentSettings) (string, error) {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return "", fmt.Errorf("agent is empty")
	}
	if strings.Contains(agent, promptFileMarker) {
		return agent, nil
	}
	switch strings.ToLower(agent) {
	case "cursor", "cursor-agent", "agent":
		args := []string{"agent -p --output-format stream-json --stream-partial-output"}
		if strings.TrimSpace(settings.CursorModel) != "" {
			args = append(args, "--model "+shellQuote(strings.TrimSpace(settings.CursorModel)))
		}
		args = append(args, `"$(cat `+promptFileMarker+`)" --force`)
		return strings.Join(args, " "), nil
	case "codex":
		// Codex's workspace-write sandbox protects .agents/ like .git/, which
		// blocks reviewers from recording findings. Re-add it explicitly.
		args := []string{`codex exec --sandbox workspace-write -c 'sandbox_workspace_write.writable_roots=[".agents"]'`}
		codexModel := firstNonEmpty(settings.CodexModel, settings.Model)
		codexReasoning := firstNonEmpty(settings.CodexReasoning, settings.Reasoning)
		if strings.TrimSpace(codexModel) != "" {
			args = append(args, "-m "+shellQuote(strings.TrimSpace(codexModel)))
		}
		if strings.TrimSpace(codexReasoning) != "" {
			args = append(args, "-c "+shellQuote("model_reasoning_effort="+quoteTOMLString(strings.TrimSpace(codexReasoning))))
		}
		args = append(args, `"$(cat `+promptFileMarker+`)" < /dev/null`)
		return strings.Join(args, " "), nil
	case "claude":
		args := []string{"claude -p --permission-mode acceptEdits"}
		if strings.TrimSpace(settings.ClaudeModel) != "" {
			args = append(args, "--model "+shellQuote(strings.TrimSpace(settings.ClaudeModel)))
		}
		if strings.TrimSpace(settings.ClaudeEffort) != "" {
			args = append(args, "--effort "+shellQuote(strings.TrimSpace(settings.ClaudeEffort)))
		}
		args = append(args, `"$(cat `+promptFileMarker+`)"`)
		return strings.Join(args, " "), nil
	default:
		return "", fmt.Errorf("unknown agent %q: use cursor, cursor-agent, agent, codex, claude, or a command containing %s", agent, promptFileMarker)
	}
}

func substitutePromptFile(command, promptPath string) string {
	return strings.ReplaceAll(command, promptFileMarker, shellQuote(promptPath))
}

func ensureShellCommandAvailable(command, role string) error {
	executable, err := shellCommandExecutable(command)
	if err != nil {
		return err
	}
	if executable == "" {
		return nil
	}
	if strings.ContainsRune(executable, filepath.Separator) {
		info, err := os.Stat(executable)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("%s agent command %q was not found; edit .agents/config.yml or install it", role, executable)
			}
			return err
		}
		if info.IsDir() || info.Mode()&0o111 == 0 {
			return fmt.Errorf("%s agent command %q is not executable; edit .agents/config.yml or fix permissions", role, executable)
		}
		return nil
	}
	if _, err := exec.LookPath(executable); err != nil {
		return fmt.Errorf("%s agent command %q was not found on PATH; edit .agents/config.yml or install it", role, executable)
	}
	return nil
}

func shellCommandExecutable(command string) (string, error) {
	args, err := parseCommandLine(command)
	if err != nil {
		return "", fmt.Errorf("parse agent command: %w", err)
	}
	for _, arg := range args {
		if strings.Contains(arg, "=") && !strings.HasPrefix(arg, "-") {
			parts := strings.SplitN(arg, "=", 2)
			if parts[0] != "" && parts[1] != "" && !strings.ContainsAny(parts[0], "/.") {
				continue
			}
		}
		return arg, nil
	}
	return "", nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func quoteTOMLString(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func runStreamingShellCommand(cwd, command string, artifacts runArtifactPaths, state loopState, agent string, settings agentSettings, live io.Writer) (string, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = cwd
	// Attribute any `agents` CLI calls the role makes to the role itself, so
	// findings, fixes, and verifications carry enforceable actor identity.
	// AGENTS_AGENT qualifies records with the concrete provider and model.
	cmd.Env = append(os.Environ(),
		"AGENTS_ACTOR="+state.NextRole,
		"AGENTS_AGENT="+agentDescriptor(agent, settings),
	)
	configureManagedProcess(cmd)
	if err := os.MkdirAll(filepath.Dir(artifacts.Log), 0o755); err != nil {
		return "", err
	}
	logFile, err := os.Create(artifacts.Log)
	if err != nil {
		return "", err
	}
	defer logFile.Close()
	var output strings.Builder
	marker := runLaunchMarker(artifacts, state, agent, settings)
	if _, err := logFile.WriteString(marker); err != nil {
		return "", err
	}
	var cursorWriter *cursorStreamWriter
	var writer io.Writer
	if isCursorAgent(agent) {
		cursorWriter = &cursorStreamWriter{output: &output, log: logFile, live: live}
		writer = cursorWriter
	} else {
		writer = io.MultiWriter(&output, logFile, live)
	}
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err := cmd.Start(); err != nil {
		return output.String(), err
	}
	done := make(chan struct{})
	startedAt := time.Now()
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		lastStats := state.Stats
		for {
			select {
			case <-done:
				return
			case now := <-ticker.C:
				fmt.Fprintf(logFile, "\n[agents] still running at %s\n", now.UTC().Format(time.RFC3339))
				fmt.Fprintf(live, "\rStill running... %s\r", shortDuration(now.Sub(startedAt)))
				stats, err := readTaskStats(cwd, state.Task.Path)
				if err != nil {
					continue
				}
				for _, line := range taskProgressLines(lastStats, stats) {
					fmt.Fprintln(live)
					fmt.Fprintln(live, line)
				}
				lastStats = stats
			}
		}
	}()
	completedByHandoff := make(chan string, 1)
	if target := completionHandoffTarget(state.NextRole); target != "" {
		go watchForCompletionHandoff(cwd, state.Task.Path, target, completedByHandoff, done)
	}
	waitErr := make(chan error, 1)
	go func() {
		waitErr <- cmd.Wait()
	}()
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	var completedBy string
	select {
	case err = <-waitErr:
	case <-interrupts:
		fmt.Fprintf(logFile, "\n[agents] interrupted at %s\n", time.Now().UTC().Format(time.RFC3339))
		_ = interruptManagedProcess(cmd)
		select {
		case <-waitErr:
		case <-time.After(5 * time.Second):
			fmt.Fprintf(logFile, "[agents] process did not exit after interrupt; killing at %s\n", time.Now().UTC().Format(time.RFC3339))
			_ = killManagedProcess(cmd)
			<-waitErr
		}
		err = errRunInterrupted
	case completedBy = <-completedByHandoff:
		fmt.Fprintf(logFile, "\n[agents] detected completion handoff to %s at %s\n", completedBy, time.Now().UTC().Format(time.RFC3339))
		fmt.Fprintf(live, "Handoff recorded. Moving to %s...\n", activityName(completedBy))
		_ = interruptManagedProcess(cmd)
		select {
		case err = <-waitErr:
		case <-time.After(5 * time.Second):
			fmt.Fprintf(logFile, "[agents] process did not exit after completion handoff; killing at %s\n", time.Now().UTC().Format(time.RFC3339))
			_ = killManagedProcess(cmd)
			err = <-waitErr
		}
		if completedBy != "" {
			err = nil
		}
	}
	close(done)
	if err != nil {
		fmt.Fprintf(logFile, "\n[agents] exited at %s with error: %v\n", time.Now().UTC().Format(time.RFC3339), err)
	} else {
		fmt.Fprintf(logFile, "\n[agents] completed at %s\n", time.Now().UTC().Format(time.RFC3339))
	}
	if cursorWriter != nil {
		return cursorWriter.FinalOutput(), err
	}
	return output.String(), err
}

func isCursorAgent(agent string) bool {
	switch strings.ToLower(strings.TrimSpace(agent)) {
	case "cursor", "cursor-agent", "agent":
		return true
	default:
		return false
	}
}

type cursorStreamWriter struct {
	mu             sync.Mutex
	output         *strings.Builder
	log            io.Writer
	live           io.Writer
	pending        string
	deltas         strings.Builder
	finalAssistant string
	result         string
	usage          string
}

func (w *cursorStreamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending += string(p)
	for {
		line, rest, ok := strings.Cut(w.pending, "\n")
		if !ok {
			break
		}
		w.pending = rest
		w.handleLine(line)
	}
	return len(p), nil
}

func (w *cursorStreamWriter) FinalOutput() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if strings.TrimSpace(w.pending) != "" {
		w.handleLine(w.pending)
		w.pending = ""
	}
	text := firstNonEmpty(w.result, w.finalAssistant, w.deltas.String(), w.output.String())
	if w.usage != "" {
		text = strings.TrimSpace(text) + "\nToken usage: " + w.usage
	}
	return strings.TrimSpace(text)
}

func (w *cursorStreamWriter) handleLine(line string) {
	if w.log != nil {
		fmt.Fprintln(w.log, line)
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return
	}
	var ev cursorStreamEvent
	if err := json.Unmarshal([]byte(trimmed), &ev); err != nil {
		fmt.Fprintln(w.output, line)
		if w.live != nil {
			fmt.Fprintln(w.live, line)
		}
		return
	}
	switch ev.Type {
	case "assistant":
		text := ev.messageText()
		if text == "" {
			return
		}
		if ev.TimestampMS != nil {
			w.deltas.WriteString(text)
			if w.live != nil {
				fmt.Fprint(w.live, text)
			}
			return
		}
		w.finalAssistant = text
	case "result":
		w.result = strings.TrimSpace(ev.Result)
		w.usage = ev.usageString()
	case "system", "user":
		return
	default:
		summary := ev.summary()
		if summary == "" {
			return
		}
		fmt.Fprintln(w.output, summary)
		if w.live != nil {
			fmt.Fprintln(w.live, summary)
		}
	}
}

type cursorStreamEvent struct {
	Type        string              `json:"type"`
	Subtype     string              `json:"subtype"`
	Message     cursorStreamMessage `json:"message"`
	Result      string              `json:"result"`
	TimestampMS *int64              `json:"timestamp_ms"`
	Usage       struct {
		InputTokens      int `json:"inputTokens"`
		OutputTokens     int `json:"outputTokens"`
		CacheReadTokens  int `json:"cacheReadTokens"`
		CacheWriteTokens int `json:"cacheWriteTokens"`
	} `json:"usage"`
}

type cursorStreamMessage struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
		Name string `json:"name"`
	} `json:"content"`
}

func (e cursorStreamEvent) messageText() string {
	var b strings.Builder
	for _, part := range e.Message.Content {
		if part.Type == "text" && part.Text != "" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

func (e cursorStreamEvent) usageString() string {
	if e.Usage.InputTokens == 0 && e.Usage.OutputTokens == 0 &&
		e.Usage.CacheReadTokens == 0 && e.Usage.CacheWriteTokens == 0 {
		return ""
	}
	parts := []string{
		fmt.Sprintf("input=%d", e.Usage.InputTokens),
		fmt.Sprintf("output=%d", e.Usage.OutputTokens),
	}
	if e.Usage.CacheReadTokens > 0 {
		parts = append(parts, fmt.Sprintf("cache_read=%d", e.Usage.CacheReadTokens))
	}
	if e.Usage.CacheWriteTokens > 0 {
		parts = append(parts, fmt.Sprintf("cache_write=%d", e.Usage.CacheWriteTokens))
	}
	return strings.Join(parts, " ")
}

func (e cursorStreamEvent) summary() string {
	name := strings.TrimSpace(e.Subtype)
	if name == "" {
		name = strings.TrimSpace(e.Type)
	}
	if name == "" {
		return ""
	}
	return "Cursor: " + name
}

func shortDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Round(time.Second).Seconds())
	if total < 60 {
		return fmt.Sprintf("%ds", total)
	}
	return fmt.Sprintf("%dm%02ds", total/60, total%60)
}

func taskProgressLines(before, after taskStats) []string {
	var lines []string
	if after.Handoffs > before.Handoffs {
		if after.LatestHandoffTo != "" {
			lines = append(lines, fmt.Sprintf("Handoff recorded for %s.", activityName(after.LatestHandoffTo)))
		} else {
			lines = append(lines, "Handoff recorded.")
		}
	}
	if after.Verification > before.Verification {
		lines = append(lines, "Verification recorded.")
	}
	if after.VerificationFail > before.VerificationFail {
		lines = append(lines, "Verification failed.")
	}
	if after.FindingsOpen > before.FindingsOpen {
		lines = append(lines, fmt.Sprintf("Finding added (%d open).", after.FindingsOpen))
	}
	if after.FindingsFixed > before.FindingsFixed {
		lines = append(lines, fmt.Sprintf("Finding fixed (%d fixed).", after.FindingsFixed))
	}
	if after.FindingsVerified > before.FindingsVerified {
		lines = append(lines, fmt.Sprintf("Finding verified (%d verified).", after.FindingsVerified))
	}
	if after.Next != "" && after.Next != before.Next {
		lines = append(lines, "Next: "+strings.TrimPrefix(after.Next, "- "))
	}
	return lines
}

func completionHandoffTarget(role string) string {
	switch role {
	case "implementer":
		return "reviewer"
	default:
		return ""
	}
}

func watchForCompletionHandoff(cwd, taskPath, target string, completed chan<- string, done <-chan struct{}) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			stats, err := readTaskStats(cwd, taskPath)
			if err == nil && stats.LatestHandoffTo == target {
				select {
				case completed <- target:
				default:
				}
				return
			}
		}
	}
}

func runLaunchMarker(artifacts runArtifactPaths, state loopState, agent string, settings agentSettings) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Agents run %s\n", artifacts.ID)
	fmt.Fprintf(&b, "started_at: %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "role: %s\n", state.NextRole)
	fmt.Fprintf(&b, "agent: %s\n", agent)
	fmt.Fprintf(&b, "model: %s\n", settingOrDefault(effectiveModelSetting(agent, settings)))
	fmt.Fprintf(&b, "reasoning: %s\n", settingOrDefault(effectiveReasoningSetting(agent, settings)))
	fmt.Fprintf(&b, "task: %s\n", state.Task.Title)
	fmt.Fprintf(&b, "prompt: %s\n", relPathOrSelf(artifacts.Prompt, ""))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "--- provider output ---")
	return b.String()
}

func effectiveModelSetting(agent string, settings agentSettings) string {
	switch strings.ToLower(strings.TrimSpace(agent)) {
	case "codex":
		return firstNonEmpty(settings.CodexModel, settings.Model)
	case "claude":
		return settings.ClaudeModel
	case "cursor", "cursor-agent", "agent":
		return settings.CursorModel
	default:
		return settings.Model
	}
}

func effectiveReasoningSetting(agent string, settings agentSettings) string {
	switch strings.ToLower(strings.TrimSpace(agent)) {
	case "codex":
		return firstNonEmpty(settings.CodexReasoning, settings.Reasoning)
	case "claude":
		return settings.ClaudeEffort
	default:
		return settings.Reasoning
	}
}

func settingOrDefault(value string) string {
	if strings.TrimSpace(value) == "" {
		return "provider default"
	}
	return strings.TrimSpace(value)
}

func cleanInteractiveAgentOutput(agent, output string) string {
	return cleanInteractiveAgentReply(agent, output).Text
}

func cleanInteractiveAgentReply(agent, output string) interactiveAgentReply {
	text := strings.TrimSpace(output)
	if text == "" {
		return interactiveAgentReply{}
	}
	reply := interactiveAgentReply{Usage: extractProviderUsage(text)}
	if strings.EqualFold(strings.TrimSpace(agent), "codex") {
		if cleaned := cleanCodexExecOutput(text); cleaned != "" {
			reply.Text = cleanAgentsCLIRecordOutput(cleaned)
			return reply
		}
	}
	reply.Text = cleanAgentsCLIRecordOutput(text)
	return reply
}

func extractProviderUsage(output string) string {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, "token usage:"):
			return strings.TrimSpace(trimmed[len("Token usage:"):])
		case trimmed == "tok"+"ens used":
			if i+1 < len(lines) {
				return strings.TrimSpace(lines[i+1]) + " tokens"
			}
		}
	}
	return ""
}

func cleanCodexExecOutput(output string) string {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "tok"+"ens used" {
			continue
		}
		j := i + 1
		if j < len(lines) {
			j++
		}
		for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
			j++
		}
		reply := strings.TrimSpace(strings.Join(lines[j:], "\n"))
		if reply != "" {
			return reply
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "codex" {
			continue
		}
		reply := strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
		if reply != "" && !strings.HasPrefix(reply, "exec\n") {
			return reply
		}
	}
	return ""
}

func cleanAgentsCLIRecordOutput(output string) string {
	var kept []string
	inProgressBlock := false
	skipNextUsageLine := false
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if skipNextUsageLine {
			skipNextUsageLine = false
			continue
		}
		if inProgressBlock {
			if trimmed == "```" {
				inProgressBlock = false
			}
			continue
		}
		switch {
		case trimmed == "```agents-progress":
			inProgressBlock = true
			continue
		case trimmed == "Aborting operation...":
			continue
		case trimmed == "Reading additional input from stdin...":
			continue
		case strings.Contains(trimmed, " WARN codex_"):
			continue
		case strings.Contains(trimmed, "ignoring interface.defaultPrompt[0]"):
			continue
		case strings.Contains(trimmed, "ignoring interface.icon_"):
			continue
		case strings.HasPrefix(lower, "token usage:"):
			continue
		case trimmed == "tok"+"ens used":
			skipNextUsageLine = true
			continue
		case strings.Contains(trimmed, "failed to initialize MCP client during shutdown"):
			continue
		case strings.HasPrefix(trimmed, "handoff to:"):
			continue
		case strings.HasPrefix(trimmed, "path:"):
			continue
		case strings.HasPrefix(trimmed, "decision:"):
			continue
		case strings.HasPrefix(trimmed, "finding:"):
			continue
		case strings.HasPrefix(trimmed, "fixed finding:"):
			continue
		case strings.HasPrefix(trimmed, "verified finding:"):
			continue
		default:
			kept = append(kept, line)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func writePromptFile(cwd, role, prompt string) (runArtifactPaths, error) {
	id := time.Now().UTC().Format("2006-01-02T150405Z") + "-" + role
	promptDir := filepath.Join(cwd, stateDir, "runs", "prompts")
	logDir := filepath.Join(cwd, stateDir, "runs", "logs")
	for _, dir := range []string{promptDir, logDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return runArtifactPaths{}, err
		}
	}
	artifacts := runArtifactPaths{
		ID:     id,
		Prompt: filepath.Join(promptDir, id+".txt"),
		Log:    filepath.Join(logDir, id+".log"),
	}
	return artifacts, os.WriteFile(artifacts.Prompt, []byte(prompt), 0o600)
}

func buildRolePrompt(e env, state loopState) (string, error) {
	var b strings.Builder
	switch state.NextRole {
	case "implementer":
		fmt.Fprintf(&b, "You are the implementer for this Agents task.\n\n")
		fmt.Fprintf(&b, "Task: %s\nPath: %s\nNext action: %s\n\n", state.Task.Title, state.Task.Path, state.NextAction)
		appendPromptFile(&b, e.cwd, state.Task.Path, "scope.md", "Scope")
		appendPromptFile(&b, e.cwd, state.Task.Path, "status.md", "Status")
		appendPromptFile(&b, e.cwd, state.Task.Path, "next.md", "Next")
		appendPromptDir(&b, e.cwd, state.Task.Path, "handoffs", "Handoffs")
		appendPromptDir(&b, e.cwd, state.Task.Path, "decisions", "Decisions")
		appendPromptDir(&b, e.cwd, state.Task.Path, "findings", "Findings")
		fmt.Fprintln(&b, "Working agreement:")
		fmt.Fprintln(&b, "- Edit the working tree to implement the task or fix open findings.")
		fmt.Fprintln(&b, "- Emit brief JSON progress updates for the interactive UI when phases change:")
		fmt.Fprintln(&b, "  ```agents-progress")
		fmt.Fprintln(&b, `  {"status":"Editing files","detail":"Updating the CLI parser and focused tests"}`)
		fmt.Fprintln(&b, "  ```")
		fmt.Fprintln(&b, "- Run relevant checks.")
		fmt.Fprintln(&b, "- Mark findings you fixed with `agents finding fix <finding> --evidence \"what proves the fix\"`.")
		fmt.Fprintln(&b, "- Keep task state current: `agents task status \"where things stand\"` and `agents task next \"what should happen next\"`.")
		fmt.Fprintln(&b, "- When ready for review, end with one `agents-handoff` JSON fenced block so the parent process records the reviewer handoff:")
		fmt.Fprintln(&b, "```agents-handoff")
		fmt.Fprintln(&b, `{"role":"reviewer","text":"specific changes made and checks performed"}`)
		fmt.Fprintln(&b, "```")
		fmt.Fprintln(&b, "  Replace the text with the actual changes and checks; do not copy placeholder handoff text.")
		fmt.Fprintln(&b, "- You cannot verify your own work: do not run `agents verify` or `agents finding verify`; the reviewer records verification independently.")
		fmt.Fprintln(&b, "- Keep build caches, compiled binaries, and scratch files out of the repository (including .agents/); use the system temp directory instead.")
		fmt.Fprintln(&b, "- Do not commit; leave changes for review.")
	case "reviewer":
		fmt.Fprintf(&b, "You are the reviewer for this Agents task.\n\n")
		fmt.Fprintf(&b, "Task: %s\nPath: %s\nNext action: %s\n\n", state.Task.Title, state.Task.Path, state.NextAction)
		appendPromptFile(&b, e.cwd, state.Task.Path, "scope.md", "Scope")
		appendPromptFile(&b, e.cwd, state.Task.Path, "status.md", "Status")
		appendPromptDir(&b, e.cwd, state.Task.Path, "handoffs", "Handoffs")
		appendPromptDir(&b, e.cwd, state.Task.Path, "findings", "Findings")
		appendPromptDir(&b, e.cwd, state.Task.Path, "verification", "Verification")
		fmt.Fprintln(&b, "Working agreement:")
		fmt.Fprintln(&b, "- Review the working tree, tests, and task chunks; verify claims against the code, not the handoff text.")
		fmt.Fprintln(&b, "- Emit brief JSON progress updates for the interactive UI when phases change:")
		fmt.Fprintln(&b, "  ```agents-progress")
		fmt.Fprintln(&b, `  {"status":"Running checks","detail":"Verifying tests and reviewing the diff"}`)
		fmt.Fprintln(&b, "  ```")
		fmt.Fprintln(&b, "- Record each real issue as a finding:")
		fmt.Fprintln(&b, `  agents finding add "summary" --severity low|medium|high`)
		fmt.Fprintln(&b, "- For findings marked fixed, verify them independently with evidence:")
		fmt.Fprintln(&b, `  agents finding verify <finding> --evidence "what you checked"`)
		fmt.Fprintln(&b, "- When the work is clean, end with one `agents-verify` JSON fenced block so the parent process records reproducible verification evidence:")
		fmt.Fprintln(&b, "```agents-verify")
		fmt.Fprintln(&b, `{"text":"specific checks performed and review result","cmd":"go test ./..."}`)
		fmt.Fprintln(&b, "```")
		fmt.Fprintln(&b, "  Replace the text and command with the actual verification you performed; do not copy placeholder evidence.")
		fmt.Fprintln(&b, "  The parent Agents process records this verification; do not run `agents verify` yourself.")
		fmt.Fprintln(&b, "  Recording passing verification reaches the human approval boundary; nothing ships on your claim alone.")
		fmt.Fprintln(&b, "- Summarize the review in your response: issues first, then what was verified.")
		fmt.Fprintln(&b, "- Keep build caches, compiled binaries, and scratch files out of the repository (including .agents/); use the system temp directory instead.")
		fmt.Fprintln(&b, "- Do not edit implementation code, do not mark findings fixed, and do not commit.")
	case "coordinator":
		fmt.Fprintf(&b, "You are the coordinator for this Agents task.\n\n")
		fmt.Fprintf(&b, "Task: %s\nPath: %s\nNext action: %s\n\n", state.Task.Title, state.Task.Path, state.NextAction)
		appendPromptFile(&b, e.cwd, state.Task.Path, "scope.md", "Scope")
		appendPromptFile(&b, e.cwd, state.Task.Path, "status.md", "Status")
		appendPromptFile(&b, e.cwd, state.Task.Path, "next.md", "Next")
		appendPromptDir(&b, e.cwd, state.Task.Path, "handoffs", "Handoffs")
		appendPromptDir(&b, e.cwd, state.Task.Path, "decisions", "Decisions")
		fmt.Fprintln(&b, "Working agreement:")
		fmt.Fprintln(&b, "- Treat user handoffs to coordinator as conversational planning context.")
		fmt.Fprintln(&b, "- Emit brief JSON progress updates for the interactive UI when phases change:")
		fmt.Fprintln(&b, "  ```agents-progress")
		fmt.Fprintln(&b, `  {"status":"Shaping scope","detail":"Checking existing project context and missing constraints"}`)
		fmt.Fprintln(&b, "  ```")
		fmt.Fprintln(&b, "- If the user is asking a question, comparing options, or says they are not ready to change code, answer and stop without an `agents-handoff` block.")
		fmt.Fprintln(&b, "- Emit an `agents-handoff` to implementer only when the user clearly asks to implement, proceed, run, or hand work to an agent.")
		fmt.Fprintln(&b, "- Handoffs to implementer must include `ready: true`; without it, Agents treats the response as planning-only.")
		fmt.Fprintln(&b, "- You may inspect the repository in read-only mode to answer planning or project-understanding questions.")
		fmt.Fprintln(&b, "- Use whatever read-only tools are available; do not block only because a preferred search tool is unavailable.")
		fmt.Fprintln(&b, "- If evidence is limited, answer with what you can infer and name the missing context briefly.")
		fmt.Fprintln(&b, "- Ask concise follow-up questions when the task, desired behavior, ref, constraints, or verification are unclear.")
		fmt.Fprintln(&b, "- Record decisions when the plan becomes concrete.")
		fmt.Fprintln(&b, "- Do not run `agents` commands yourself; the parent Agents process records coordination state.")
		fmt.Fprintln(&b, "- When implementation is explicitly ready, end your response with one `agents-handoff` fenced block containing `role: implementer`, `ready: true`, and a specific `text:` request for the implementer.")
		fmt.Fprintln(&b, "- Never copy placeholder handoff text; if you are still discussing, do not emit a handoff block.")
		fmt.Fprintln(&b, "- Do not edit implementation code or coordination files directly.")
	}
	return b.String(), nil
}

func applyRoleOutputDirectives(e env, actor, output string) (string, error) {
	cleaned := output
	seenHandoffs := map[string]bool{}
	seenVerification := map[string]bool{}
	for {
		changed := false
		directive, ok := parseCoordinatorDirective(cleaned)
		if ok {
			key := directive.Role + "\x00" + directive.Text
			if !seenHandoffs[key] {
				seenHandoffs[key] = true
				if err := applyCoordinatorDirective(e, actor, directive); err != nil {
					return cleaned, err
				}
			}
			cleaned = removeCoordinatorDirective(cleaned)
			changed = true
		}
		verification, ok := parseVerificationDirective(cleaned)
		if ok {
			key := verification.Text + "\x00" + verification.Cmd
			if !seenVerification[key] {
				seenVerification[key] = true
				if err := applyVerificationDirective(e, actor, verification); err != nil {
					return cleaned, err
				}
			}
			cleaned = removeVerificationDirective(cleaned)
			changed = true
		}
		if !changed {
			break
		}
	}
	return cleaned, nil
}

func applyVerificationDirective(e env, actor string, directive verificationDirective) error {
	if verificationDirectivePlaceholder(directive) {
		return nil
	}
	args := []string{directive.Text, "--by", actor}
	if directive.Cmd != "" {
		args = append(args, "--cmd", directive.Cmd)
	}
	_, _, err := addVerification(e, args, time.Now().UTC())
	return err
}

func verificationDirectivePlaceholder(directive verificationDirective) bool {
	return strings.EqualFold(strings.TrimSpace(directive.Text), "specific checks performed and review result") &&
		strings.EqualFold(strings.TrimSpace(directive.Cmd), "go test ./...")
}

func appendPromptFile(b *strings.Builder, cwd, taskPath, name, heading string) {
	data, err := os.ReadFile(filepath.Join(cwd, stateDir, taskPath, name))
	if err != nil {
		return
	}
	fmt.Fprintf(b, "## %s\n\n%s\n\n", heading, strings.TrimSpace(string(data)))
}

func appendPromptDir(b *strings.Builder, cwd, taskPath, dir, heading string) {
	entries, err := os.ReadDir(filepath.Join(cwd, stateDir, taskPath, dir))
	if err != nil || len(entries) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n\n", heading)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cwd, stateDir, taskPath, dir, entry.Name()))
		if err != nil {
			continue
		}
		fmt.Fprintf(b, "### %s\n\n%s\n\n", entry.Name(), strings.TrimSpace(string(data)))
	}
}

func printDispatchPacket(w io.Writer, info dispatchInfo) {
	fmt.Fprintf(w, "role: %s\n", info.Role)
	if info.Agent != "" {
		fmt.Fprintf(w, "agent: %s\n", info.Agent)
	} else {
		fmt.Fprintln(w, "agent: not configured")
	}
	fmt.Fprintf(w, "active task: %s\n", info.Task.Title)
	fmt.Fprintf(w, "task path: %s\n", info.Task.Path)
	fmt.Fprintf(w, "action: %s\n", info.Action)
	if info.Boundary != "" {
		fmt.Fprintf(w, "boundary: %s\n", info.Boundary)
	}
	printTaskStats(w, info.Stats)
}

func printLoopState(w io.Writer, state loopState) {
	fmt.Fprintf(w, "active task: %s\n", state.Task.Title)
	fmt.Fprintf(w, "task path: %s\n", state.Task.Path)
	fmt.Fprintf(w, "next role: %s\n", state.NextRole)
	fmt.Fprintf(w, "next action: %s\n", state.NextAction)
	if state.Boundary != "" {
		fmt.Fprintf(w, "boundary: %s\n", state.Boundary)
	}
	printTaskStats(w, state.Stats)
}

func printRoleRunResult(w io.Writer, result roleRunResult) {
	printRoleRunSummary(w, result, true)
}

func printRoleRunSummary(w io.Writer, result roleRunResult, includeOutput bool) {
	fmt.Fprintf(w, "ran role: %s\n", result.Role)
	agent := result.AgentDesc
	if agent == "" {
		agent = result.Agent
	}
	fmt.Fprintf(w, "agent: %s\n", agent)
	fmt.Fprintf(w, "prompt: %s\n", relPathOrSelf(result.PromptPath, result.Before.Task.Path))
	fmt.Fprintf(w, "log: %s\n", relPathOrSelf(result.LogPath, result.Before.Task.Path))
	if includeOutput && strings.TrimSpace(result.AgentOutput) != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "agent output:")
		fmt.Fprintln(w, strings.TrimRight(result.AgentOutput, "\n"))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "after:")
	printLoopState(w, result.After)
}

func relPathOrSelf(path, _ string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return path
	}
	return relPathFrom(cwd, path)
}

func relPathFrom(cwd, path string) string {
	rel, err := filepath.Rel(cwd, path)
	if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return rel
}

func dispatchPayload(info dispatchInfo) dispatchJSON {
	return dispatchJSON{
		Role:       info.Role,
		Agent:      info.Agent,
		Configured: info.Agent != "",
		Task:       taskPayload(info.Task, info.Stats),
		Action:     info.Action,
		Boundary:   info.Boundary,
	}
}

func loopPayload(state loopState) loopJSON {
	return loopJSON{
		Task:       taskPayload(state.Task, state.Stats),
		NextRole:   state.NextRole,
		NextAction: state.NextAction,
		Boundary:   state.Boundary,
	}
}
