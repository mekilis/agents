package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
)

type reputationRound struct {
	Version    int                `json:"schema_version"`
	ID         string             `json:"round_id"`
	ProposalID string             `json:"proposal_id"`
	Completed  time.Time          `json:"completed_at"`
	Proposer   adviserIdentity    `json:"proposer"`
	Critics    []reputationCritic `json:"critics"`
	Outcome    string             `json:"outcome"`
}

type reputationCritic struct {
	adviserIdentity
	Verdict      string `json:"verdict"`
	FindingCount *int   `json:"finding_count"`
}

type adviserRating struct {
	adviserIdentity
	Clean  uint64  `json:"clean_rounds"`
	Total  uint64  `json:"total_rounds"`
	Rating float64 `json:"rating"`
	last   time.Time
}

type reputationHistory struct {
	total   uint64
	ratings map[string]adviserRating
}

var adviserRoundID = regexp.MustCompile(`^adv-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}$`)
var adviserEndpointID = regexp.MustCompile(`^ep:[0-9a-f]{16}$`)

func validRatedAdviser(a adviserIdentity) bool {
	if !validAdviserKey(a.ID) || a.ID != a.key() || a.Model == "" || a.Model != strings.TrimSpace(a.Model) ||
		a.Reasoning != strings.TrimSpace(a.Reasoning) {
		return false
	}
	if a.Provider == "openai-compat" {
		return a.Reasoning == "" && adviserEndpointID.MatchString(a.Endpoint)
	}
	return adviserDriver(a.Provider) == a.Provider && a.Provider != "" && a.Endpoint == "" &&
		(a.Reasoning == "" || a.Provider == "codex" || a.Provider == "claude")
}

func validAdviserKey(key string) bool {
	provider, model, ok := strings.Cut(key, "/")
	return ok && model != "" && (provider == "openai-compat" || (provider != "" && adviserDriver(provider) == provider))
}

func validateReputationRound(r reputationRound, filename string) error {
	_, offset := r.Completed.Zone()
	if r.Version != 1 || !adviserRoundID.MatchString(r.ID) || filename != r.ID+".json" ||
		r.ProposalID != "prop-"+r.ID || r.Completed.IsZero() || offset != 0 ||
		!validRatedAdviser(r.Proposer) || len(r.Critics) == 0 {
		return fmt.Errorf("invalid reputation round")
	}
	if _, err := time.Parse("20060102T150405Z", r.ID[len("adv-"):len(r.ID)-9]); err != nil {
		return fmt.Errorf("invalid reputation round timestamp")
	}
	seen := map[string]bool{r.Proposer.ID: true}
	outcome := "clean"
	for _, c := range r.Critics {
		if !validRatedAdviser(c.adviserIdentity) || seen[c.ID] || c.FindingCount == nil {
			return fmt.Errorf("invalid or duplicate reputation critic")
		}
		seen[c.ID] = true
		switch {
		case c.Verdict == "clean" && *c.FindingCount == 0:
		case c.Verdict == "findings" && *c.FindingCount > 0:
			outcome = "findings"
		default:
			return fmt.Errorf("invalid reputation critic verdict")
		}
	}
	if r.Outcome != outcome {
		return fmt.Errorf("reputation outcome does not match critiques")
	}
	return nil
}

func decodeAdviserJSON(data []byte, target any) error {
	// Reject duplicates before decoding into Go structs (which otherwise keep the last value).
	tokens := json.NewDecoder(bytes.NewReader(data))
	// Routing map keys are case-sensitive adviser identities. Struct fields
	// still reject case aliases because encoding/json matches them loosely.
	_, routing := target.(**adviserRouting)
	if err := checkAdviserJSONValue(tokens, routing, false); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON value")
	}
	return nil
}

func checkAdviserJSONValue(d *json.Decoder, routingRoot, identityKeys bool) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	switch token {
	case json.Delim('{'):
		keys := make(map[string]bool)
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name := key.(string)
			if !identityKeys {
				name = strings.ToLower(name)
			}
			if keys[name] {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			keys[name] = true
			isIdentityMap := routingRoot && (name == "proposer_failures" || name == "proposer_deferrals" || name == "critic_failures")
			if err := checkAdviserJSONValue(d, false, isIdentityMap); err != nil {
				return err
			}
		}
	case json.Delim('['):
		for d.More() {
			if err := checkAdviserJSONValue(d, false, false); err != nil {
				return err
			}
		}
	default:
		return nil
	}
	_, err = d.Token()
	return err
}

func readReputationRound(path string) (reputationRound, error) {
	var r reputationRound
	data, err := os.ReadFile(path)
	if err == nil {
		err = decodeAdviserJSON(data, &r)
	}
	if err == nil {
		err = validateReputationRound(r, filepath.Base(path))
	}
	if err != nil {
		return r, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
}

func readReputation(dir string) (reputationHistory, error) {
	history := reputationHistory{ratings: make(map[string]adviserRating)}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return history, nil
	}
	if err != nil {
		return history, err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") && entry.Type().IsRegular() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".json") {
			return history, fmt.Errorf("%s: unexpected entry in reputation store", path)
		}
		r, err := readReputationRound(path)
		if err != nil {
			return history, err
		}
		rating := history.ratings[r.Proposer.ID]
		rating.adviserIdentity = r.Proposer
		rating.Total++
		if r.Outcome == "clean" {
			rating.Clean++
		}
		if r.Completed.After(rating.last) {
			rating.last = r.Completed
		}
		history.total++
		history.ratings[r.Proposer.ID] = rating
	}
	return history, nil
}

func compareAdviserRating(a, b adviserRating) int {
	leftHigh, leftLow := bits.Mul64(a.Clean+2, b.Total+4)
	rightHigh, rightLow := bits.Mul64(b.Clean+2, a.Total+4)
	switch {
	case leftHigh > rightHigh || leftHigh == rightHigh && leftLow > rightLow:
		return -1
	case leftHigh < rightHigh || leftLow < rightLow:
		return 1
	default:
		return 0
	}
}

type adviserBackoff struct {
	Count uint64 `json:"consecutive_failures"`
	Last  uint64 `json:"last_failed_seq"`
	Until uint64 `json:"skip_until_T"`
}

type adviserRouting struct {
	Seq       uint64                    `json:"attempt_seq"`
	Proposers map[string]adviserBackoff `json:"proposer_failures"`
	Deferrals map[string]adviserBackoff `json:"proposer_deferrals"`
	Critics   map[string]adviserBackoff `json:"critic_failures"`
}

func readAdviserRouting(path string) (adviserRouting, error) {
	r := adviserRouting{
		Proposers: make(map[string]adviserBackoff),
		Deferrals: make(map[string]adviserBackoff),
		Critics:   make(map[string]adviserBackoff),
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	var stored *adviserRouting
	err = decodeAdviserJSON(data, &stored)
	if err == nil && (stored == nil || stored.Seq == 0 || stored.Proposers == nil || stored.Deferrals == nil || stored.Critics == nil) {
		err = fmt.Errorf("invalid adviser routing state")
	}
	if err == nil {
		for _, entries := range []map[string]adviserBackoff{stored.Proposers, stored.Deferrals, stored.Critics} {
			for id, b := range entries {
				if !validAdviserKey(id) || b.Count == 0 || b.Count > b.Last || b.Last > stored.Seq || b.Until == 0 {
					err = fmt.Errorf("invalid adviser cooldown")
				}
			}
		}
	}
	if err != nil {
		return r, fmt.Errorf("%s: %w", path, err)
	}
	return *stored, nil
}

func adviserOrder(pool []adviserInvocation, h reputationHistory, config adviserConfig, routing adviserRouting, proposer string) []adviserInvocation {
	order := make([]adviserInvocation, 0, len(pool))
	calibrate := false
	for _, adviser := range pool {
		if adviser.ID != proposer {
			order = append(order, adviser)
			calibrate = calibrate || h.ratings[adviser.ID].Total == 0
		}
	}
	slices.SortStableFunc(order, func(a, b adviserInvocation) int {
		return compareAdviserRating(h.ratings[a.ID], h.ratings[b.ID])
	})
	backoff := func(id string) adviserBackoff { return routing.Critics[id] }
	if proposer == "" {
		backoff = func(id string) adviserBackoff {
			failure, deferral := routing.Proposers[id], routing.Deferrals[id]
			return adviserBackoff{failure.Count + deferral.Count, max(failure.Last, deferral.Last), max(failure.Until, deferral.Until)}
		}
		if calibrate {
			base := slices.Clone(order)
			order = order[:0]
			for _, a := range pool {
				if h.ratings[a.ID].Total == 0 {
					order = append(order, a)
				}
			}
			slices.SortStableFunc(order, func(a, b adviserInvocation) int {
				return compareCount(backoff(a.ID).Count, backoff(b.ID).Count)
			})
			for _, a := range base {
				if h.ratings[a.ID].Total != 0 {
					order = append(order, a)
				}
			}
		} else if len(order) > 1 && h.total > 0 && h.total%uint64(config.ExploreEvery) == 0 {
			leader := order[0]
			order = order[:0]
			for _, a := range pool {
				if a.ID != leader.ID {
					order = append(order, a)
				}
			}
			slices.SortStableFunc(order, func(a, b adviserInvocation) int {
				x, y := h.ratings[a.ID], h.ratings[b.ID]
				if c := compareCount(x.Total, y.Total); c != 0 {
					return c
				}
				return x.last.Compare(y.last)
			})
			order = append(order, leader)
		}
	}
	slices.SortStableFunc(order, func(a, b adviserInvocation) int {
		x, y := backoff(a.ID), backoff(b.ID)
		switch {
		case x.Until <= h.total && y.Until <= h.total:
			return 0
		case x.Until <= h.total:
			return -1
		case y.Until <= h.total:
			return 1
		}
		if c := compareCount(x.Count, y.Count); c != 0 {
			return c
		}
		return compareCount(x.Last, y.Last)
	})
	return order
}

func compareCount(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

type reputationStore struct {
	state   string
	syncDir func(string) error
}

func (s reputationStore) rounds() string { return filepath.Join(s.state, "reputation", "rounds") }
func (s reputationStore) routing() string {
	return filepath.Join(s.state, "tmp", "adviser-routing.json")
}

func (s reputationStore) mkdir(path string, mode os.FileMode) error {
	if err := os.Mkdir(path, mode); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	// Also sync existing directories' parents to recover a previously failed sync.
	return s.syncDir(filepath.Dir(path))
}

func (s reputationStore) write(path string, value any, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = f.Chmod(mode)
	if err == nil {
		err = json.NewEncoder(f).Encode(value)
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	if err := s.syncDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("publication durability for %s: %w", path, err)
	}
	return nil
}

func (s reputationStore) publish(r reputationRound) error {
	filename := r.ID + ".json"
	if err := validateReputationRound(r, filename); err != nil {
		return err
	}
	for _, dir := range []string{filepath.Dir(s.rounds()), s.rounds()} {
		if err := s.mkdir(dir, 0755); err != nil {
			return err
		}
	}
	path := filepath.Join(s.rounds(), filename)
	existing, err := readReputationRound(path)
	if errors.Is(err, os.ErrNotExist) {
		return s.write(path, r, 0644)
	}
	if err != nil {
		return err
	}
	existing.Completed = r.Completed
	if !reflect.DeepEqual(existing, r) {
		return fmt.Errorf("%s: conflicting reputation round", path)
	}
	if err := s.syncDir(s.rounds()); err != nil {
		return fmt.Errorf("publication durability for %s: %w", path, err)
	}
	return nil
}

type adviserAttempt struct {
	proposer string
	proposed bool
	critics  map[string]bool
	round    *reputationRound
}

func (s reputationStore) commit(ctx context.Context, a adviserAttempt, explore int) (reputationHistory, adviserRouting, error) {
	var history reputationHistory
	var routing adviserRouting
	err := s.withLock(ctx, func() error {
		if a.round != nil {
			// Fail closed if history was damaged since selection.
			if _, err := readReputation(s.rounds()); err != nil {
				return err
			}
			if err := s.publish(*a.round); err != nil {
				return err
			}
		}
		var err error
		history, err = readReputation(s.rounds())
		if err != nil {
			return err
		}
		routing, err = readAdviserRouting(s.routing())
		if err != nil {
			return err
		}
		if routing.Seq == ^uint64(0) {
			return fmt.Errorf("%s: attempt sequence exhausted", s.routing())
		}
		routing.Seq++
		update := func(entries map[string]adviserBackoff, id string, success bool) {
			if success {
				delete(entries, id)
				return
			}
			count := entries[id].Count + 1
			entries[id] = adviserBackoff{count, routing.Seq, history.total + min(count, uint64(explore))}
		}
		update(routing.Proposers, a.proposer, a.proposed)
		if a.proposed {
			update(routing.Deferrals, a.proposer, a.round != nil)
		}
		for id, success := range a.critics {
			update(routing.Critics, id, success)
		}
		return s.write(s.routing(), routing, 0600)
	})
	return history, routing, err
}

func (s reputationStore) withLock(ctx context.Context, fn func() error) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	dir := filepath.Dir(s.routing())
	if err := s.mkdir(dir, 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "reputation.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	ticker := time.NewTicker(15 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%s: acquire reputation lock: %w", f.Name(), err)
		}
		locked, err := tryReputationLock(f)
		if err != nil {
			return fmt.Errorf("%s: acquire reputation lock: %w", f.Name(), err)
		}
		if locked {
			return fn()
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}
