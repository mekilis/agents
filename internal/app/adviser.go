package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type adviserFinding struct {
	Severity string `json:"severity"`
	Text     string `json:"text"`
	Evidence string `json:"evidence"`
}

type adviserCritique struct {
	RoundID    string           `json:"round_id"`
	ProposalID string           `json:"proposal_id"`
	CriticID   string           `json:"critic_id"`
	Verdict    string           `json:"verdict"`
	Summary    *string          `json:"summary"`
	Findings   []adviserFinding `json:"findings"`
}

type adviserCritiqueResult struct {
	Critic   adviserIdentity  `json:"critic"`
	Critique *adviserCritique `json:"critique,omitempty"`
	Error    string           `json:"error,omitempty"`
}

type adviserResponse struct {
	RoundID    string                  `json:"round_id"`
	ProposalID string                  `json:"proposal_id"`
	Proposer   adviserIdentity         `json:"proposer"`
	Proposal   string                  `json:"proposal"`
	Critiques  []adviserCritiqueResult `json:"critiques"`
	Outcome    string                  `json:"outcome"`
	Scored     bool                    `json:"scored"`
	Reason     string                  `json:"unrated_reason"`
	Ratings    []adviserRating         `json:"ratings"`
	Excluded   []adviserIdentity       `json:"excluded_unverifiable,omitempty"`
}

func parseAdviserArgs(args []string) (subcommand, prompt string, jsonOut bool, err error) {
	if len(args) == 0 || args[0] != "ask" && args[0] != "ratings" {
		return "", "", false, fmt.Errorf("expected ask or ratings")
	}
	subcommand = args[0]
	literal, gotPrompt := false, false
	for _, arg := range args[1:] {
		switch {
		case !literal && arg == "--json" && !jsonOut:
			jsonOut = true
		case !literal && arg == "--" && subcommand == "ask":
			literal = true
		case subcommand != "ask" || gotPrompt || !literal && strings.HasPrefix(arg, "-"):
			return "", "", false, fmt.Errorf("unexpected argument %q", arg)
		default:
			prompt, gotPrompt = arg, true
		}
	}
	if subcommand == "ask" && strings.TrimSpace(prompt) == "" {
		err = fmt.Errorf("expected a non-empty advice prompt")
	}
	return
}

func runAdviserCommand(args []string, out, errOut io.Writer, e env) int {
	subcommand, prompt, jsonOut, err := parseAdviserArgs(args)
	if err != nil {
		fmt.Fprintf(errOut, "agents: advise: %v\nUsage: agents advise ask [--json] [--] \"<prompt>\"\n       agents advise ratings [--json]\n", err)
		return 2
	}
	if err := runAdviser(context.Background(), e, subcommand, prompt, jsonOut, out, executeAdviser); err != nil {
		fmt.Fprintf(errOut, "agents: advise: %v\n", err)
		return 1
	}
	return 0
}

func adviserRatings(order []adviserInvocation, history reputationHistory) []adviserRating {
	ratings := make([]adviserRating, 0, len(order))
	for _, a := range order {
		rating := history.ratings[a.ID]
		rating.adviserIdentity = a.adviserIdentity
		rating.Rating = float64(rating.Clean+2) / float64(rating.Total+4)
		ratings = append(ratings, rating)
	}
	return ratings
}

func printAdviserRatings(out io.Writer, ratings []adviserRating) {
	for i, r := range ratings {
		fmt.Fprintf(out, "%d. %s  %.3f  (%d/%d clean rounds)\n", i+1, r.ID, r.Rating, r.Clean, r.Total)
	}
}

type adviserExecutor func(context.Context, string, adviserInvocation, string) (string, error)

func runAdviser(ctx context.Context, e env, subcommand, prompt string, jsonOut bool, out io.Writer, execute adviserExecutor) error {
	cwd := e.cwd
	cfg, err := readConfig(cwd)
	if err != nil {
		return err
	}
	if len(cfg.Adviser.Roles) == 0 && subcommand == "ask" {
		return fmt.Errorf("configure at least one entry in roles.adviser in .agents/config.yml")
	}
	if subcommand == "ask" {
		trusted, err := isTrusted(e)
		if err != nil {
			return err
		}
		if !trusted {
			return fmt.Errorf("review .agents/config.yml, then run `agents trust` before asking advisers")
		}
	}
	var pool []adviserInvocation
	var first adviserInvocation
	response := adviserResponse{Critiques: []adviserCritiqueResult{}, Outcome: "unrated"}
	seen := make(map[string]bool)
	for i, role := range cfg.Adviser.Roles {
		a, err := resolveAdviserInvocation(cfg, role)
		if err != nil {
			return fmt.Errorf("roles.adviser entry %d: %w", i+1, err)
		}
		if i == 0 {
			first = a
		}
		if a.Model == "" || a.command != "" {
			response.Excluded = append(response.Excluded, a.adviserIdentity)
		} else if !seen[a.ID] {
			seen[a.ID] = true
			pool = append(pool, a)
		}
	}
	store := reputationStore{filepath.Join(cwd, stateDir), syncReputationDir}
	history, err := readReputation(store.rounds())
	if err != nil {
		return err
	}
	routing, err := readAdviserRouting(store.routing())
	if err != nil {
		return err
	}
	order := adviserOrder(pool, history, cfg.Adviser, routing, "")
	if subcommand == "ratings" {
		ratings := adviserRatings(order, history)
		if jsonOut {
			return json.NewEncoder(out).Encode(ratings)
		}
		printAdviserRatings(out, ratings)
		return nil
	}
	response.Ratings = adviserRatings(order, history)
	var random [4]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	response.RoundID = fmt.Sprintf("adv-%s-%x", time.Now().UTC().Format("20060102T150405Z"), random)
	response.ProposalID = "prop-" + response.RoundID
	contextPrompt := adviserContext(cwd, prompt)
	proposalPrompt := "Propose advice for the request below. Respond with the proposal only.\n\n" + contextPrompt
	if len(pool) < 2 {
		response.Proposer = first.adviserIdentity
		response.Reason = "insufficient_verifiable_peers"
		response.Proposal, err = execute(ctx, cwd, first, proposalPrompt)
		if err != nil && !errors.Is(err, errAdviserIsolationViolation) {
			return fmt.Errorf("unrated proposal from %s: %w", first.ID, err)
		}
		if err != nil {
			response.Reason = "isolation_violation"
		}
	} else {
		proposer := order[0]
		critics := adviserOrder(pool, history, cfg.Adviser, routing, proposer.ID)
		critics = critics[:min(cfg.Adviser.Critics, len(critics))]
		attempt, err := executeAdviserRound(ctx, cwd, proposer, critics, proposalPrompt, contextPrompt, &response, execute)
		history, routing, commitErr := store.commit(ctx, attempt, cfg.Adviser.ExploreEvery)
		if commitErr != nil {
			return errors.Join(err, commitErr)
		}
		response.Ratings = adviserRatings(adviserOrder(pool, history, cfg.Adviser, routing, ""), history)
		response.Scored = attempt.round != nil
		if err != nil {
			return err
		}
	}
	if jsonOut {
		return json.NewEncoder(out).Encode(response)
	}
	fmt.Fprintf(out, "%s (%s)\nProposer: %s\n%s\n", response.RoundID, response.ProposalID, response.Proposer.ID, response.Proposal)
	for _, c := range response.Critiques {
		if c.Critique == nil {
			fmt.Fprintf(out, "\nCritic %s: %s\n", c.Critic.ID, c.Error)
			continue
		}
		fmt.Fprintf(out, "\nCritic %s: %s\n%s\n", c.Critic.ID, c.Critique.Verdict, *c.Critique.Summary)
		for _, f := range c.Critique.Findings {
			fmt.Fprintf(out, "- [%s] %s\n  Evidence: %s\n", f.Severity, f.Text, f.Evidence)
		}
	}
	if response.Scored {
		fmt.Fprintf(out, "\nScored: %s\n", response.Outcome)
	} else {
		fmt.Fprintf(out, "\nUnrated: %s\n", response.Reason)
	}
	for _, a := range response.Excluded {
		fmt.Fprintf(out, "Unverifiable adviser: %s\n", a.ID)
	}
	printAdviserRatings(out, response.Ratings)
	return nil
}

func adviserContext(cwd, prompt string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Request:\n%s\n", prompt)
	task := readActiveTask(cwd)
	if task.Path == "" || task.Missing {
		b.WriteString("\nStandalone workspace request.\n")
	} else {
		fmt.Fprintf(&b, "\nActive task: %s\n", task.Title)
		for _, name := range []string{"scope.md", "status.md", "next.md", "plan.md"} {
			f, err := os.Open(filepath.Join(cwd, stateDir, task.Path, name))
			if err != nil {
				continue
			}
			data, err := io.ReadAll(io.LimitReader(f, 16<<10))
			f.Close()
			if err == nil {
				fmt.Fprintf(&b, "\n%s\n%s\n", name, data)
			}
		}
	}
	return strings.ReplaceAll(b.String(), cwd, ".")
}

func executeAdviserRound(ctx context.Context, cwd string, proposer adviserInvocation, critics []adviserInvocation, proposalPrompt, contextPrompt string, response *adviserResponse, execute adviserExecutor) (adviserAttempt, error) {
	attempt := adviserAttempt{proposer: proposer.ID, critics: make(map[string]bool)}
	response.Proposer = proposer.adviserIdentity
	proposal, err := execute(ctx, cwd, proposer, proposalPrompt)
	if errors.Is(err, errAdviserIsolationUnavailable) {
		return attempt, err
	}
	if err != nil {
		response.Reason = "proposer_failed"
		if errors.Is(err, errAdviserIsolationViolation) {
			response.Reason = "isolation_violation"
		}
		return attempt, nil
	}
	response.Proposal = proposal
	attempt.proposed = true
	round := reputationRound{Version: 1, ID: response.RoundID, ProposalID: response.ProposalID, Proposer: proposer.adviserIdentity, Outcome: "clean"}
	for _, c := range critics {
		result := adviserCritiqueResult{Critic: c.adviserIdentity}
		criticPrompt := fmt.Sprintf("Independently critique the proposal against the request. Return one agents-critique JSON fenced block. Use verdict clean with findings [] or verdict findings with concrete flaws. Each finding must have severity low, medium or high, text, and evidence. Include all these fields and copy the three binding IDs exactly.\n%s\n\n%s\n\nProposal:\n%s",
			critiqueTemplate(round, c.ID), contextPrompt, proposal)
		output, err := execute(ctx, cwd, c, criticPrompt)
		if errors.Is(err, errAdviserIsolationUnavailable) {
			attempt.critics[c.ID] = false
			return attempt, err
		}
		if errors.Is(err, errAdviserIsolationViolation) {
			response.Reason = "isolation_violation"
			attempt.critics[c.ID] = false
			result.Error = response.Reason
			response.Critiques = append(response.Critiques, result)
			return attempt, nil
		}
		if err != nil {
			result.Error = "incomplete_critique"
		} else if critique, err := parseAdviserCritique(output, round, c.ID, c.baseURL != ""); err != nil {
			result.Error = "invalid_critique"
		} else {
			result.Critique = &critique
			count := len(critique.Findings)
			round.Critics = append(round.Critics, reputationCritic{c.adviserIdentity, critique.Verdict, &count})
			if critique.Verdict == "findings" {
				round.Outcome = "findings"
			}
		}
		attempt.critics[c.ID] = result.Critique != nil
		response.Critiques = append(response.Critiques, result)
	}
	if len(round.Critics) != len(critics) {
		response.Reason = "incomplete_critique"
		if slices.ContainsFunc(response.Critiques, func(c adviserCritiqueResult) bool { return c.Error == "invalid_critique" }) {
			response.Reason = "invalid_critique"
		}
		return attempt, nil
	}
	round.Completed = time.Now().UTC()
	attempt.round = &round
	response.Outcome = round.Outcome
	return attempt, nil
}

func critiqueTemplate(round reputationRound, critic string) string {
	summary := "brief review summary"
	data, _ := json.Marshal(adviserCritique{RoundID: round.ID, ProposalID: round.ProposalID, CriticID: critic, Verdict: "clean", Summary: &summary, Findings: []adviserFinding{}})
	return string(data)
}

func parseAdviserCritique(output string, round reputationRound, critic string, allowRaw bool) (adviserCritique, error) {
	var critique adviserCritique
	text := strings.TrimSpace(output)
	invalid := fmt.Errorf("invalid agents-critique")
	remaining := text
	var block strings.Builder
	found, inside := false, false
	for remaining != "" {
		var line string
		line, remaining, _ = strings.Cut(remaining, "\n")
		switch strings.TrimSpace(line) {
		case "```agents-critique":
			if found {
				return critique, invalid
			}
			found, inside = true, true
		case "```":
			inside = false
		default:
			if inside {
				block.WriteString(line)
				block.WriteByte('\n')
			}
		}
	}
	if inside {
		return critique, invalid
	}
	if found {
		text = block.String()
	} else if !allowRaw || !strings.HasPrefix(text, "{") {
		return critique, invalid
	}
	if err := decodeAdviserJSON([]byte(text), &critique); err != nil {
		return critique, invalid
	}
	if critique.RoundID != round.ID || critique.ProposalID != round.ProposalID || critique.CriticID == "" ||
		critique.CriticID != critic || critic == round.Proposer.ID || critique.Summary == nil || critique.Findings == nil {
		return critique, invalid
	}
	switch critique.Verdict {
	case "clean":
		if len(critique.Findings) != 0 || strings.TrimSpace(*critique.Summary) == "" {
			return critique, invalid
		}
	case "findings":
		if len(critique.Findings) == 0 {
			return critique, invalid
		}
		for _, f := range critique.Findings {
			if f.Severity != "low" && f.Severity != "medium" && f.Severity != "high" ||
				strings.TrimSpace(f.Text) == "" || strings.TrimSpace(f.Evidence) == "" {
				return critique, invalid
			}
		}
	default:
		return critique, invalid
	}
	return critique, nil
}

const maxAdviserOutput = 1 << 20

func executeAdviser(ctx context.Context, cwd string, a adviserInvocation, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	var output string
	var err error
	if a.baseURL != "" {
		output, err = executeHTTPAdviser(ctx, a, prompt)
	} else {
		output, err = executeSubprocessAdviser(ctx, cwd, a, prompt, adviserSandboxArgs)
	}
	output = strings.TrimSpace(output)
	if err == nil && output == "" {
		err = fmt.Errorf("empty adviser output")
	}
	return output, err
}

func executeHTTPAdviser(ctx context.Context, a adviserInvocation, prompt string) (string, error) {
	data, err := json.Marshal(map[string]any{
		"model":       a.Model,
		"temperature": 0,
		"messages":    []map[string]string{{"role": "user", "content": prompt}},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("invalid adviser request")
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("adviser HTTP request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("adviser HTTP status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAdviserOutput+1))
	if err != nil || len(body) > maxAdviserOutput {
		return "", fmt.Errorf("cannot read adviser HTTP response")
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &completion); err != nil || len(completion.Choices) == 0 {
		return "", fmt.Errorf("invalid adviser HTTP completion")
	}
	return completion.Choices[0].Message.Content, nil
}
