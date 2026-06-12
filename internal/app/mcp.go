package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/textproto"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const mcpProtocolVersion = "2025-06-18"

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *mcpError `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type mcpToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type mcpToolResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func runMCPCommand(args []string, in io.Reader, out, errOut io.Writer, e env) int {
	if len(args) != 0 {
		fmt.Fprintln(errOut, "agents: mcp does not accept arguments yet")
		return 2
	}
	if err := serveMCP(in, out, e); err != nil {
		fmt.Fprintf(errOut, "agents: mcp: %v\n", err)
		return 1
	}
	return 0
}

func serveMCP(in io.Reader, out io.Writer, e env) error {
	reader := bufio.NewReader(in)
	framing := "line"
	for {
		message, messageFraming, err := readMCPMessage(reader)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		framing = messageFraming
		var req mcpRequest
		if err := json.Unmarshal(message, &req); err != nil {
			if err := writeMCPMessage(out, framing, mcpResponse{JSONRPC: "2.0", Error: &mcpError{Code: -32700, Message: "parse error"}}); err != nil {
				return err
			}
			continue
		}
		resp, ok := handleMCPRequest(req, e)
		if !ok {
			continue
		}
		if err := writeMCPMessage(out, framing, resp); err != nil {
			return err
		}
	}
}

func readMCPMessage(reader *bufio.Reader) ([]byte, string, error) {
	peek, err := reader.Peek(1)
	if err != nil {
		return nil, "", err
	}
	if peek[0] != 'C' && peek[0] != 'c' {
		line, err := reader.ReadBytes('\n')
		if err != nil && !(err == io.EOF && len(line) > 0) {
			return nil, "", err
		}
		return bytes.TrimSpace(line), "line", nil
	}
	header, err := textproto.NewReader(reader).ReadMIMEHeader()
	if err != nil {
		if err != io.EOF {
			return nil, "", fmt.Errorf("missing Content-Length header: %w", err)
		}
		return nil, "", err
	}
	lengthText := strings.TrimSpace(header.Get("Content-Length"))
	if lengthText == "" {
		return nil, "", fmt.Errorf("missing Content-Length header")
	}
	length, err := strconv.Atoi(lengthText)
	if err != nil || length < 0 {
		return nil, "", fmt.Errorf("invalid Content-Length header")
	}
	message := make([]byte, length)
	if _, err := io.ReadFull(reader, message); err != nil {
		return nil, "", err
	}
	return message, "content-length", nil
}

func writeMCPMessage(out io.Writer, framing string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if framing == "line" {
		if _, err := out.Write(data); err != nil {
			return err
		}
		_, err = fmt.Fprintln(out)
		return err
	}
	if _, err := fmt.Fprintf(out, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return err
	}
	_, err = out.Write(data)
	return err
}

func handleMCPRequest(req mcpRequest, e env) (mcpResponse, bool) {
	if req.ID == nil {
		// MCP notifications, such as initialized, do not receive responses.
		return mcpResponse{}, false
	}
	resp := mcpResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "agents",
				"version": versionInfo.Version,
			},
		}
	case "tools/list":
		resp.Result = map[string]any{"tools": mcpTools()}
	case "tools/call":
		var params mcpToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &mcpError{Code: -32602, Message: "invalid tools/call params"}
			return resp, true
		}
		result, err := callMCPTool(e, params.Name, params.Arguments)
		if err != nil {
			resp.Result = mcpToolTextResult(map[string]any{"error": err.Error()}, true)
		} else {
			resp.Result = mcpToolTextResult(result, false)
		}
	default:
		resp.Error = &mcpError{Code: -32601, Message: "method not found"}
	}
	return resp, true
}

func mcpTools() []mcpTool {
	return []mcpTool{
		{Name: "status", Description: "Return scriptable Agents workspace and active task status.", InputSchema: objectSchema(nil, nil)},
		{Name: "run_once", Description: "Run one queued Agents role using existing local configuration.", InputSchema: objectSchema(nil, nil)},
		{Name: "recent_runs", Description: "Return persisted cleaned run activity for the active task.", InputSchema: objectSchema(nil, nil)},
		{Name: "watch_activity", Description: "Return the current persisted cleaned run activity snapshot.", InputSchema: objectSchema(nil, nil)},
		{Name: "add_handoff", Description: "Record a handoff to a role on the active task.", InputSchema: objectSchema(map[string]any{"to": stringSchema("Target role."), "text": stringSchema("Handoff text."), "by": stringSchema("Optional actor override.")}, []string{"to", "text"})},
		{Name: "add_finding", Description: "Record an open finding on the active task.", InputSchema: objectSchema(map[string]any{"text": stringSchema("Finding text."), "severity": enumSchema("Finding severity.", []string{"low", "medium", "high"}), "by": stringSchema("Optional actor override.")}, []string{"text"})},
		{Name: "fix_finding", Description: "Mark a finding fixed with optional evidence.", InputSchema: findingUpdateSchema()},
		{Name: "verify_finding", Description: "Mark a fixed finding independently verified with optional evidence.", InputSchema: findingUpdateSchema()},
		{Name: "add_decision", Description: "Record a decision on the active task.", InputSchema: objectSchema(map[string]any{"text": stringSchema("Decision text."), "by": stringSchema("Optional actor override.")}, []string{"text"})},
		{Name: "archive", Description: "Archive the active task using the existing local archive boundary.", InputSchema: objectSchema(nil, nil)},
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func enumSchema(description string, values []string) map[string]any {
	return map[string]any{"type": "string", "description": description, "enum": values}
}

func findingUpdateSchema() map[string]any {
	return objectSchema(map[string]any{
		"finding":  stringSchema("Finding path or filename."),
		"evidence": stringSchema("Optional evidence."),
		"by":       stringSchema("Optional actor override."),
	}, []string{"finding"})
}

func callMCPTool(e env, name string, rawArgs json.RawMessage) (any, error) {
	switch name {
	case "status":
		return statusPayload(e)
	case "run_once":
		var out, errOut bytes.Buffer
		code := runLoopCommand([]string{"--once"}, &out, &errOut, e)
		return commandResult(code, out.String(), errOut.String()), nil
	case "recent_runs", "watch_activity":
		return mcpRunActivity(e)
	case "add_handoff":
		var args struct {
			To   string `json:"to"`
			Text string `json:"text"`
			By   string `json:"by"`
		}
		if err := decodeMCPArgs(rawArgs, &args); err != nil {
			return nil, err
		}
		record, err := addHandoff(e, withBy(args.By, "--to", args.To, args.Text), time.Now().UTC())
		if err != nil {
			return nil, err
		}
		return map[string]any{"to": record.To, "path": record.Path}, nil
	case "add_finding":
		var args struct {
			Text     string `json:"text"`
			Severity string `json:"severity"`
			By       string `json:"by"`
		}
		if err := decodeMCPArgs(rawArgs, &args); err != nil {
			return nil, err
		}
		cliArgs := []string{args.Text}
		if strings.TrimSpace(args.Severity) != "" {
			cliArgs = append(cliArgs, "--severity", args.Severity)
		}
		record, err := addFinding(e, withBy(args.By, cliArgs...), time.Now().UTC())
		if err != nil {
			return nil, err
		}
		return map[string]any{"title": record.Title, "path": record.Path}, nil
	case "fix_finding":
		return mcpUpdateFinding(e, rawArgs, "fixed", "Fixed")
	case "verify_finding":
		return mcpUpdateFinding(e, rawArgs, "verified", "Verification")
	case "add_decision":
		var args struct {
			Text string `json:"text"`
			By   string `json:"by"`
		}
		if err := decodeMCPArgs(rawArgs, &args); err != nil {
			return nil, err
		}
		record, err := addDecision(e, withBy(args.By, args.Text), time.Now().UTC())
		if err != nil {
			return nil, err
		}
		return map[string]any{"title": record.Title, "path": record.Path}, nil
	case "archive":
		record, err := archiveActiveTask(e, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		return map[string]any{"title": record.Title, "path": record.Path}, nil
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func decodeMCPArgs(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	return json.Unmarshal(raw, v)
}

func withBy(by string, args ...string) []string {
	if strings.TrimSpace(by) == "" {
		return args
	}
	return append([]string{"--by", strings.TrimSpace(by)}, args...)
}

func mcpUpdateFinding(e env, rawArgs json.RawMessage, status, section string) (any, error) {
	var args struct {
		Finding  string `json:"finding"`
		Evidence string `json:"evidence"`
		By       string `json:"by"`
	}
	if err := decodeMCPArgs(rawArgs, &args); err != nil {
		return nil, err
	}
	cliArgs := []string{args.Finding}
	if strings.TrimSpace(args.Evidence) != "" {
		cliArgs = append(cliArgs, "--evidence", args.Evidence)
	}
	record, err := updateFindingStatus(e, withBy(args.By, cliArgs...), status, section, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": record.Path, "by": record.By}, nil
}

func mcpRunActivity(e env) (any, error) {
	active := readActiveTask(e.cwd)
	if active.Path == "" {
		return nil, fmt.Errorf("no active task")
	}
	dir := filepath.Join(e.cwd, stateDir, active.Path, "runs")
	records, err := readRunActivityRecords(dir)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"task":    taskActivityPayload(active),
		"records": records,
	}, nil
}

func taskActivityPayload(active activeTask) map[string]any {
	return map[string]any{
		"title": active.Title,
		"path":  active.Path,
	}
}

func commandResult(code int, stdout, stderr string) map[string]any {
	return map[string]any{
		"exit_code": code,
		"stdout":    strings.TrimRight(stdout, "\n"),
		"stderr":    strings.TrimRight(stderr, "\n"),
	}
}

func mcpToolTextResult(payload any, isError bool) mcpToolResult {
	data, err := json.Marshal(payload)
	if err != nil {
		data = []byte(fmt.Sprintf(`{"error":%q}`, err.Error()))
		isError = true
	}
	return mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: string(data)}},
		IsError: isError,
	}
}
