package app

import (
	"crypto/sha256"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type adviserConfig struct {
	Roles                 []roleConfig
	Critics, ExploreEvery int
}

func parseAdviserConfig(data string, cfg *adviserConfig) error {
	cfg.Critics, cfg.ExploreEvery = 2, 4
	var section string
	roleIndent := -1
	for _, line := range strings.Split(data, "\n") {
		content := strings.TrimSpace(line)
		if content == "" || strings.HasPrefix(content, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		key, value, field := strings.Cut(content, ":")
		key = strings.TrimSpace(key)
		if indent == 0 {
			if key == "adviser" && strings.TrimSpace(value) != "" {
				return fmt.Errorf("adviser settings must use a block mapping")
			}
			section, roleIndent = key, -1
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "\"")
		if section == "roles" && indent == 2 && content != "-" && !strings.HasPrefix(content, "- ") {
			roleIndent = -1
			if key != "adviser" || !field {
				continue
			}
			roleIndent = indent
			if value == "" {
				continue
			}
			roleIndent = -1
			items := []string{value}
			if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
				items = strings.Split(value[1:len(value)-1], ",")
			}
			for _, item := range items {
				if item = strings.Trim(strings.TrimSpace(item), "\""); item != "" {
					cfg.Roles = append(cfg.Roles, roleConfigFromValue(item))
				}
			}
			continue
		}
		if section == "roles" && roleIndent >= 0 && indent >= roleIndent {
			if content == "-" || strings.HasPrefix(content, "- ") {
				content = strings.TrimSpace(strings.TrimPrefix(content, "-"))
				key, value, field = strings.Cut(content, ":")
				key = strings.TrimSpace(key)
				value = strings.Trim(strings.TrimSpace(value), "\"")
				if !field {
					cfg.Roles = append(cfg.Roles, roleConfigFromValue(content))
					continue
				}
				cfg.Roles = append(cfg.Roles, roleConfig{})
			}
			if !field {
				return fmt.Errorf("invalid roles.adviser field %q", content)
			}
			if len(cfg.Roles) == 0 {
				cfg.Roles = append(cfg.Roles, roleConfig{})
			}
			role := &cfg.Roles[len(cfg.Roles)-1]
			switch key {
			case "provider":
				role.Provider = value
			case "model":
				role.Model = value
			case "reasoning", "effort":
				role.Reasoning = value
			case "cmd":
				role.Cmd = value
			default:
				return fmt.Errorf("unsupported roles.adviser field %q", key)
			}
			continue
		}
		if section == "providers" && indent >= 4 {
			switch strings.ToLower(strings.Trim(key, "\"'")) {
			case "score", "rating", "weight":
				return fmt.Errorf("providers cannot configure adviser %s", key)
			}
		}
		if section == "adviser" {
			n, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(value, " #", 2)[0]))
			switch key {
			case "critics", "explore_every":
				if err != nil || n < 1 {
					return fmt.Errorf("adviser.%s must be an integer >= 1", key)
				}
				if key == "critics" {
					cfg.Critics = n
				} else {
					cfg.ExploreEvery = n
				}
			default:
				return fmt.Errorf("unsupported adviser setting %q", key)
			}
		}
	}
	return nil
}

type adviserIdentity struct {
	ID        string `json:"adviser_id"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Reasoning string `json:"reasoning,omitempty"`
	Endpoint  string `json:"endpoint_id,omitempty"`
}

func (a adviserIdentity) key() string {
	query := url.Values{}
	if a.Reasoning != "" {
		query.Set("reasoning", a.Reasoning)
	}
	if a.Endpoint != "" {
		query.Set("endpoint", a.Endpoint)
	}
	id := a.Provider + "/" + url.PathEscape(a.Model)
	if len(query) != 0 {
		id += "?" + query.Encode()
	}
	return id
}

type adviserInvocation struct {
	adviserIdentity
	baseURL, command string
	timeout          time.Duration
}

func adviserDriver(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "cursor", "cursor-agent", "agent":
		return "cursor"
	case "agy", "antigravity":
		return "agy"
	case "codex", "claude", "gemini":
		return strings.ToLower(strings.TrimSpace(name))
	default:
		return ""
	}
}

func resolveAdviserInvocation(cfg config, rc roleConfig) (adviserInvocation, error) {
	pc, _ := providerForRole(cfg, rc)
	a := adviserInvocation{command: firstNonEmpty(rc.Cmd, pc.Cmd), timeout: 2 * time.Minute}
	if pc.Timeout > 0 && int64(pc.Timeout) <= int64((1<<63-1)/time.Millisecond) {
		a.timeout = time.Duration(pc.Timeout) * time.Millisecond
	}
	isHTTP := pc.BaseURL != "" || pc.Type == "openai_compatible"
	switch {
	case a.command != "":
		if isHTTP {
			return a, fmt.Errorf("adviser cmd cannot be combined with HTTP settings")
		}
		a.Provider = "cmd"
	case isHTTP:
		if adviserDriver(pc.Provider) != "" {
			return a, fmt.Errorf("HTTP adviser cannot specify a CLI driver")
		}
		base, err := canonicalizeAdviserBaseURL(pc.BaseURL)
		if err != nil {
			return a, err
		}
		a.baseURL = base
		a.Provider, a.Model = "openai-compat", firstNonEmpty(rc.Model, pc.Model)
		sum := sha256.Sum256([]byte("agents-adviser-endpoint-v1\x00" + base))
		a.Endpoint = fmt.Sprintf("ep:%x", sum[:8])
	default:
		a.Provider = adviserDriver(providerDriver(pc))
		s := cfg.Settings
		model := firstNonEmpty(rc.Model, pc.Model)
		reasoning := firstNonEmpty(rc.Reasoning, pc.Reasoning, pc.Effort)
		switch a.Provider {
		case "codex":
			a.Model = firstNonEmpty(model, s.CodexModel, s.Model)
			a.Reasoning = firstNonEmpty(reasoning, s.CodexReasoning, s.Reasoning)
		case "claude":
			a.Model = firstNonEmpty(model, s.ClaudeModel)
			a.Reasoning = firstNonEmpty(reasoning, s.ClaudeEffort)
		case "cursor":
			a.Model = firstNonEmpty(model, s.CursorModel)
		case "gemini":
			a.Model = firstNonEmpty(model, s.GeminiModel, s.Model)
		case "agy":
			a.Model = firstNonEmpty(model, s.AntigravityModel, s.Model)
		default:
			return a, fmt.Errorf("unknown adviser provider %q", rc.Provider)
		}
	}
	a.Model, a.Reasoning = strings.TrimSpace(a.Model), strings.TrimSpace(a.Reasoning)
	a.ID = a.key()
	return a, nil
}

func canonicalizeAdviserBaseURL(raw string) (string, error) {
	invalid := fmt.Errorf("invalid adviser base_url")
	raw = strings.TrimSpace(raw)
	if strings.ContainsAny(raw, "?#\\") || strings.ContainsFunc(raw, func(r rune) bool { return r < 32 || r == 127 }) {
		return "", invalid
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Opaque != "" || u.Hostname() == "" ||
		(u.Scheme != "http" && u.Scheme != "https") || strings.HasSuffix(u.Host, ":") {
		return "", invalid
	}
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if strings.Contains(host, ":") && !strings.HasPrefix(u.Host, "[") {
		return "", invalid
	}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", invalid
		}
		port = strconv.Itoa(n)
		if u.Scheme == "http" && n == 80 || u.Scheme == "https" && n == 443 {
			port = ""
		}
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.HasPrefix(u.Host, "[") {
		host = "[" + strings.ReplaceAll(host, "%", "%25") + "]"
	}
	if port != "" && strings.Contains(host, "%") {
		host = strings.ReplaceAll(host, "%", "%25")
	}
	path := strings.TrimSuffix(u.EscapedPath(), "/")
	if path == "" {
		return u.Scheme + "://" + host, nil
	}
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, segment := range segments {
		var normalized strings.Builder
		for j := 0; j < len(segment); j++ {
			if segment[j] != '%' {
				normalized.WriteByte(segment[j])
				continue
			}
			if j+2 >= len(segment) {
				return "", invalid
			}
			n, err := strconv.ParseUint(segment[j+1:j+3], 16, 8)
			if err != nil || n < 32 || n == 127 {
				return "", invalid
			}
			if strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~", rune(n)) {
				normalized.WriteByte(byte(n))
			} else {
				fmt.Fprintf(&normalized, "%%%02X", n)
			}
			j += 2
		}
		segments[i] = normalized.String()
		if segments[i] == "" || segments[i] == "." || segments[i] == ".." {
			return "", invalid
		}
	}
	return u.Scheme + "://" + host + "/" + strings.Join(segments, "/"), nil
}
