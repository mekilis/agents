package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func testReputationStore(t *testing.T) reputationStore {
	t.Helper()
	return reputationStore{t.TempDir(), syncReputationDir}
}

func testRatedAdviser(model string) adviserInvocation {
	a := adviserIdentity{Provider: "codex", Model: model}
	a.ID = a.key()
	return adviserInvocation{adviserIdentity: a}
}

func testReputationRound() reputationRound {
	return reputationRound{
		Version:    1,
		ID:         "adv-20261120T210000Z-00000001",
		ProposalID: "prop-adv-20261120T210000Z-00000001",
		Completed:  time.Now().UTC(),
		Proposer:   testRatedAdviser("A").adviserIdentity,
		Critics:    []reputationCritic{{adviserIdentity: testRatedAdviser("B").adviserIdentity, Verdict: "clean", FindingCount: new(int)}},
		Outcome:    "clean",
	}
}

func TestReputationComparisonAndRouting(t *testing.T) {
	for _, pair := range [][2]adviserRating{
		{{Clean: 9, Total: 10}, {Clean: 60, Total: 100}},
		{{Clean: 90, Total: 100}, {Clean: 1, Total: 1}},
		{{Clean: 1, Total: 1}, {Clean: 50, Total: 100}},
		{{Clean: 1, Total: 1}, {}},
		{{}, {Clean: 0, Total: 1}},
	} {
		if compareAdviserRating(pair[0], pair[1]) != -1 || compareAdviserRating(pair[1], pair[0]) != 1 {
			t.Fatalf("rating order = %+v", pair)
		}
	}
	if compareAdviserRating(adviserRating{}, adviserRating{Clean: 50, Total: 100}) != 0 {
		t.Fatal("expected exact tie")
	}
	a, b, c, d := testRatedAdviser("A"), testRatedAdviser("B"), testRatedAdviser("C"), testRatedAdviser("D")
	pool := []adviserInvocation{a, b, c, d}
	cfg := adviserConfig{ExploreEvery: 4, Critics: 2}
	history := reputationHistory{total: 8, ratings: map[string]adviserRating{
		a.ID: {Total: 4, Clean: 4},
		b.ID: {Total: 2, Clean: 1},
		c.ID: {Total: 1, Clean: 1},
		d.ID: {Total: 1, Clean: 0, last: time.Now()},
	}}
	routing := adviserRouting{Proposers: map[string]adviserBackoff{}, Critics: map[string]adviserBackoff{}}
	if got := adviserOrder(pool, history, cfg, routing, "")[0].ID; got != c.ID {
		t.Fatalf("exploration order picked %s", got)
	}
	routing.Proposers[c.ID] = adviserBackoff{Count: 1, Last: 1, Until: 9}
	if got := adviserOrder(pool, history, cfg, routing, "")[0].ID; got != d.ID {
		t.Fatalf("failed exploration did not advance: %s", got)
	}
	routing.Critics[c.ID] = adviserBackoff{Count: 1, Last: 1, Until: 9}
	critics := adviserOrder(pool, history, cfg, routing, a.ID)
	if critics[0].ID != b.ID || critics[1].ID != d.ID {
		t.Fatalf("critics did not bypass cooling peer: %+v", critics)
	}
	history.total++
	if got := adviserOrder(pool, history, cfg, routing, "")[0].ID; got != a.ID {
		t.Fatalf("exploitation order picked %s", got)
	}
	if got := adviserOrder(pool, history, cfg, routing, a.ID)[0].ID; got != c.ID {
		t.Fatalf("critic did not recover after cooldown: %s", got)
	}
}

func TestReputationPublicationSyncAndReplay(t *testing.T) {
	store := testReputationStore(t)
	round := testReputationRound()
	final := filepath.Join(store.rounds(), round.ID+".json")
	for _, failDir := range []string{store.state, store.rounds()} {
		var synced []string
		errSync := errors.New("injected sync failure")
		store.syncDir = func(dir string) error {
			synced = append(synced, dir)
			if dir == failDir {
				return errSync
			}
			return syncReputationDir(dir)
		}
		if err := store.publish(round); !errors.Is(err, errSync) {
			t.Fatalf("expected sync error at %s: %v", failDir, err)
		}
		want := []string{store.state}
		if failDir == store.rounds() {
			want = append(want, filepath.Dir(store.rounds()), store.rounds())
		}
		if !reflect.DeepEqual(synced, want) {
			t.Fatalf("sync paths %v, want %v", synced, want)
		}
		_, err := os.Stat(final)
		if failDir == store.state && !errors.Is(err, os.ErrNotExist) || failDir == store.rounds() && err != nil {
			t.Fatalf("unexpected publication before sync at %s: %v", failDir, err)
		}
	}
	store.syncDir = syncReputationDir
	if err := store.publish(round); err != nil {
		t.Fatal(err)
	}
	round.Completed = round.Completed.Add(time.Second)
	if err := store.publish(round); err != nil {
		t.Fatalf("matching replay failed: %v", err)
	}
	history, err := readReputation(store.rounds())
	if err != nil || history.total != 1 {
		t.Fatalf("expected one round, got %+v %v", history, err)
	}
	round.Proposer = testRatedAdviser("C").adviserIdentity
	if err := store.publish(round); err == nil {
		t.Fatal("conflicting replay accepted")
	}
}

func TestReputationCorruptionFailsClosed(t *testing.T) {
	base := adviserTestServer(t, func(_, _, _, _ string) (int, string) {
		t.Error("corrupt state called provider")
		return 500, ""
	})
	e := adviserTestEnv(t, fmt.Sprintf("providers:\n  local:\n    base_url: %s\n    model: Test\nroles:\n  adviser: local\n", base))
	store := reputationStore{filepath.Join(e.cwd, stateDir), syncReputationDir}
	adviserCLI(t, e, 0, "ratings", "--json")
	round := testReputationRound()
	data, err := json.Marshal(round)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.rounds(), 0755); err != nil {
		t.Fatal(err)
	}
	temporary := filepath.Join(store.rounds(), "orphan.tmp")
	if err := os.WriteFile(temporary, []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	adviserCLI(t, e, 0, "ratings")
	for _, defect := range []struct{ filename, contents string }{
		{round.ID + ".json", "{"},
		{round.ID + ".json", strings.Replace(string(data), `"schema_version":1`, `"schema_version":2`, 1)},
		{round.ID + ".json", strings.Replace(string(data), `"outcome":"clean"`, `"ignored":"clean"`, 1)},
		{round.ID + ".json", strings.ReplaceAll(strings.ReplaceAll(string(data), "codex/B", "codex/A"), `"model":"B"`, `"model":"A"`)},
		{"anything.json", string(data)},
	} {
		path := filepath.Join(store.rounds(), defect.filename)
		if err := os.WriteFile(path, []byte(defect.contents), 0644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"ask", "bad store"}, {"ratings", "--json"}} {
			if errMsg := adviserCLI(t, e, 1, args...); !strings.Contains(errMsg, path) {
				t.Fatalf("diagnostic omitted bad record: %s", errMsg)
			}
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(store.routing()), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.routing(), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if errMsg := adviserCLI(t, e, 1, "ratings"); !strings.Contains(errMsg, store.routing()) {
		t.Fatal(errMsg)
	}
}

func TestReputationConcurrentCommitMergesDeltas(t *testing.T) {
	store := testReputationStore(t)
	round := testReputationRound()
	proposer, critic := round.Proposer.ID, round.Critics[0].ID
	a := adviserAttempt{proposer: proposer, proposed: true, critics: map[string]bool{critic: true}, round: &round}
	failedProposer := testRatedAdviser("C").ID
	b := adviserAttempt{proposer: failedProposer, critics: map[string]bool{proposer: false}}
	const failures = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, attempt := range append([]adviserAttempt{a, a}, repeatAdviserAttempt(b, failures)...) {
		wg.Go(func() {
			<-start
			if _, _, err := store.commit(context.Background(), attempt, 4); err != nil {
				t.Errorf("commit: %v", err)
			}
		})
	}
	close(start)
	wg.Wait()
	h, err := readReputation(store.rounds())
	if err != nil || h.total != 1 {
		t.Fatalf("round replay changed count: %+v %v", h, err)
	}
	routing, err := readAdviserRouting(store.routing())
	if err != nil || routing.Seq != failures+2 || routing.Proposers[failedProposer].Count != failures ||
		routing.Critics[proposer].Count != failures || routing.Proposers[failedProposer].Until < 4 ||
		routing.Proposers[failedProposer].Last != routing.Critics[proposer].Last {
		t.Fatalf("routing lost deltas: %+v %v", routing, err)
	}
}

func repeatAdviserAttempt(attempt adviserAttempt, count int) []adviserAttempt {
	attempts := make([]adviserAttempt, count)
	for i := range attempts {
		attempts[i] = attempt
	}
	return attempts
}

func TestReputationLockHelper(t *testing.T) {
	state := os.Getenv("AGENTS_TEST_REPUTATION_LOCK")
	if state == "" {
		return
	}
	store := reputationStore{state, syncReputationDir}
	err := store.withLock(context.Background(), func() error {
		if err := os.WriteFile(filepath.Join(state, "locked"), nil, 0600); err != nil {
			return err
		}
		time.Sleep(30 * time.Second)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReputationLockRecoversAfterProcessExit(t *testing.T) {
	store := testReputationStore(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestReputationLockHelper$")
	cmd.Env = append(os.Environ(), "AGENTS_TEST_REPUTATION_LOCK="+store.state)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := os.Stat(filepath.Join(store.state, "locked"))
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) || time.Now().After(deadline) {
			t.Fatalf("helper did not acquire lock: %v", err)
		}
		time.Sleep(15 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Millisecond)
	defer cancel()
	if err := store.withLock(ctx, func() error { return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("contending lock: %v", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("expected killed process")
	}
	round := testReputationRound()
	h, _, err := store.commit(context.Background(), adviserAttempt{proposer: round.Proposer.ID, proposed: true, round: &round}, 4)
	if err != nil || h.total != 1 {
		t.Fatalf("lock did not recover: %+v %v", h, err)
	}
}

func TestReputationRoutingPreservesCaseDistinctKeys(t *testing.T) {
	store := testReputationStore(t)
	a, b := testRatedAdviser("ModelA").ID, testRatedAdviser("modela").ID
	for _, id := range []string{a, b} {
		attempt := adviserAttempt{proposer: id, critics: map[string]bool{a: false, b: false}}
		if _, _, err := store.commit(context.Background(), attempt, 4); err != nil {
			t.Fatal(err)
		}
	}
	routing, err := readAdviserRouting(store.routing())
	if err != nil || routing.Proposers[a].Count != 1 || routing.Proposers[b].Count != 1 ||
		routing.Critics[a].Count != 2 || routing.Critics[b].Count != 2 {
		t.Fatalf("routing: %+v (%v)", routing, err)
	}
}
