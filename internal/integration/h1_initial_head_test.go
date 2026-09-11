//go:build integration

package integration

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

// Single-cluster regression for ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01: HEAD
// initialization goes through the conditional initializer, so concurrent
// initializers converge on exactly one HEAD and an initializer that arrives
// after a HEAD exists keeps it. The multi-DC reversion variant is covered by
// h1_initial_head_multidc_test.go on the real 3-DC fixture.

type h1HeadResponse struct {
	HeadCommitID string `json:"head_commit_id"`
	IsCorrupted  int    `json:"is_corrupted"`
}

// TestSyncGetHeadCommitConcurrentInitializersConvergeOnOneHead takes an
// API-created library, resets its head_commit_id to null (the state every
// creation path leaves before initialization), and fires concurrent
// GET /seafhttp/repo/:id/commit/HEAD requests, each of which enters
// createInitialCommit. Exactly one initial commit may win; every response
// must report that same head; losers must not leave dangling commit rows.
func TestSyncGetHeadCommitConcurrentInitializersConvergeOnOneHead(t *testing.T) {
	repoID := createTestLibraryWithCleanup(t, adminClient, adminClient, "inttest-h1-concurrent-init")

	database, err := openIntegrationProjectionDB()
	if err != nil {
		t.Fatalf("open Cassandra: %v", err)
	}
	defer database.Close()

	var orgID string
	if err := database.Session().Query(`SELECT org_id FROM libraries_by_id WHERE library_id = ?`, repoID).Scan(&orgID); err != nil {
		t.Fatalf("resolve org for %s: %v", repoID, err)
	}
	var originalHead string
	if err := database.Session().Query(`SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, repoID).Scan(&originalHead); err != nil {
		t.Fatalf("read original head: %v", err)
	}
	if originalHead == "" {
		t.Fatal("API-created library must have an initial head")
	}
	if err := database.Session().Query(`UPDATE libraries SET head_commit_id = null WHERE org_id = ? AND library_id = ?`, orgID, repoID).Exec(); err != nil {
		t.Fatalf("reset head to null: %v", err)
	}

	const attempts = 16
	heads := make([]string, attempts)
	statuses := make([]int, attempts)
	errs := make([]error, attempts)
	var wg sync.WaitGroup
	start := make(chan struct{})
	client := &http.Client{Timeout: 30 * time.Second}
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			req, err := http.NewRequest(http.MethodGet, adminClient.baseURL+fmt.Sprintf("/seafhttp/repo/%s/commit/HEAD", repoID), nil)
			if err != nil {
				errs[i] = err
				return
			}
			req.Header.Set("Authorization", "Token "+adminClient.token)
			resp, err := client.Do(req)
			if err != nil {
				errs[i] = err
				return
			}
			defer resp.Body.Close()
			statuses[i] = resp.StatusCode
			var body h1HeadResponse
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				errs[i] = err
				return
			}
			heads[i] = body.HeadCommitID
		}(i)
	}
	close(start)
	wg.Wait()

	winner := ""
	for i := 0; i < attempts; i++ {
		if errs[i] != nil {
			t.Fatalf("request %d: %v", i, errs[i])
		}
		if statuses[i] != http.StatusOK {
			t.Fatalf("request %d: status %d, want 200", i, statuses[i])
		}
		if heads[i] == "" {
			t.Fatalf("request %d: empty head_commit_id; initialization must never answer with an empty HEAD", i)
		}
		if winner == "" {
			winner = heads[i]
		} else if heads[i] != winner {
			t.Fatalf("request %d reported head %s, request 0 reported %s; concurrent initializers must converge on one HEAD", i, heads[i], winner)
		}
	}

	var canonical string
	if err := database.Session().Query(`SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, repoID).Scan(&canonical); err != nil {
		t.Fatalf("read canonical head: %v", err)
	}
	if canonical != winner {
		t.Fatalf("canonical head %s differs from the head every request reported (%s)", canonical, winner)
	}

	// Original commit (from library creation) + exactly one surviving initial
	// commit: every KNOWN_LOSER initializer discards its own attempt-unique
	// commit row (best effort; on a healthy cluster the DELETE succeeds).
	var commitCount int
	if err := database.Session().Query(`SELECT count(*) FROM commits WHERE library_id = ?`, repoID).Scan(&commitCount); err != nil {
		t.Fatalf("count commits: %v", err)
	}
	if commitCount != 2 {
		t.Fatalf("commits rows = %d, want 2 (original + one surviving initial commit); losing initializers must not leave dangling commits", commitCount)
	}
	t.Logf("GREEN: %d concurrent initializers converged on head %s with no dangling commits", attempts, winner)
}

// TestInitializeLibraryFSKeepsExistingHead drives the v2 initializer against
// a library that already has a HEAD: it must succeed without changing HEAD
// and without leaving its own commit row behind.
func TestInitializeLibraryFSKeepsExistingHead(t *testing.T) {
	repoID := createTestLibraryWithCleanup(t, adminClient, adminClient, "inttest-h1-init-keeps-head")

	database, err := openIntegrationProjectionDB()
	if err != nil {
		t.Fatalf("open Cassandra: %v", err)
	}
	defer database.Close()

	var orgID, ownerID string
	if err := database.Session().Query(`SELECT org_id, owner_id FROM libraries_by_id WHERE library_id = ?`, repoID).Scan(&orgID, &ownerID); err != nil {
		t.Fatalf("resolve org/owner for %s: %v", repoID, err)
	}
	var before string
	if err := database.Session().Query(`SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, repoID).Scan(&before); err != nil {
		t.Fatalf("read head: %v", err)
	}
	var commitsBefore int
	if err := database.Session().Query(`SELECT count(*) FROM commits WHERE library_id = ?`, repoID).Scan(&commitsBefore); err != nil {
		t.Fatalf("count commits: %v", err)
	}

	if err := v2pkg.NewFSHelper(database).InitializeLibraryFS(orgID, repoID, ownerID, "inttest-h1-init-keeps-head"); err != nil {
		t.Fatalf("InitializeLibraryFS on an initialized library must be idempotent, got: %v", err)
	}

	var after string
	if err := database.Session().Query(`SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, repoID).Scan(&after); err != nil {
		t.Fatalf("read head: %v", err)
	}
	if after != before {
		t.Fatalf("InitializeLibraryFS overwrote an existing HEAD: %s -> %s", before, after)
	}
	var commitsAfter int
	if err := database.Session().Query(`SELECT count(*) FROM commits WHERE library_id = ?`, repoID).Scan(&commitsAfter); err != nil {
		t.Fatalf("count commits: %v", err)
	}
	if commitsAfter != commitsBefore {
		t.Fatalf("losing initializer left commit rows behind: %d -> %d", commitsBefore, commitsAfter)
	}
}

// TestInitializeLibraryHeadIfUnsetMissingRowIsNotFound pins, against a real
// Cassandra, the shape MapScanCAS returns for a non-existent partition: the
// conditional initializer must report ErrLibraryHeadNotFound and must NOT
// upsert a phantom libraries row (IF head_commit_id = null alone would).
func TestInitializeLibraryHeadIfUnsetMissingRowIsNotFound(t *testing.T) {
	database, err := openIntegrationProjectionDB()
	if err != nil {
		t.Fatalf("open Cassandra: %v", err)
	}
	defer database.Close()

	orgID, repoID := uuid.NewString(), uuid.NewString()
	commitID := "c-missing"
	if err := database.Session().Query(`INSERT INTO commits (library_id, commit_id, root_fs_id, creator_id, description, created_at) VALUES (?, ?, ?, ?, ?, ?)`, repoID, commitID, "root", uuid.NewString(), "Initial commit", time.Now()).Exec(); err != nil {
		t.Fatalf("seed attempt commit row: %v", err)
	}
	head, outcome, err := v2pkg.NewFSHelper(database).InitializeLibraryHeadIfUnset(orgID, repoID, commitID, time.Now())
	if !errors.Is(err, v2pkg.ErrLibraryHeadNotFound) {
		t.Fatalf("missing row: err=%v head=%q outcome=%q, want ErrLibraryHeadNotFound", err, head, outcome)
	}
	var phantom string
	scanErr := database.Session().Query(`SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, repoID).Scan(&phantom)
	if !errors.Is(scanErr, gocql.ErrNotFound) {
		t.Fatalf("a phantom libraries row was created (scan err=%v head=%q); the created_at != null guard is not holding", scanErr, phantom)
	}
	var danglingCommit string
	commitScanErr := database.Session().Query(`SELECT commit_id FROM commits WHERE library_id = ? AND commit_id = ?`, repoID, commitID).Scan(&danglingCommit)
	if !errors.Is(commitScanErr, gocql.ErrNotFound) {
		t.Fatalf("a demonstrably-not-applied attempt (missing library row) must discard its own unattributed commit row, got scan err=%v commit=%q", commitScanErr, danglingCommit)
	}
}

// TestInitializeLibraryHeadIfUnsetRefusesInvalidRows pins, against a real
// Cassandra, the two invalid row states the initializer refuses instead of
// "repairing": an empty-string head, and a null head on a row with a null
// created_at.
func TestInitializeLibraryHeadIfUnsetRefusesInvalidRows(t *testing.T) {
	database, err := openIntegrationProjectionDB()
	if err != nil {
		t.Fatalf("open Cassandra: %v", err)
	}
	defer database.Close()
	helper := v2pkg.NewFSHelper(database)
	orgID := uuid.NewString()

	emptyHeadRepo := uuid.NewString()
	if err := database.Session().Query(`INSERT INTO libraries (org_id, library_id, name, head_commit_id, created_at, updated_at) VALUES (?, ?, ?, '', ?, ?)`, orgID, emptyHeadRepo, "inttest-h1-empty-head", time.Now(), time.Now()).Exec(); err != nil {
		t.Fatalf("seed empty-head row: %v", err)
	}
	defer database.Session().Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, emptyHeadRepo).Exec()
	if err := database.Session().Query(`INSERT INTO commits (library_id, commit_id, root_fs_id, creator_id, description, created_at) VALUES (?, ?, ?, ?, ?, ?)`, emptyHeadRepo, "c-x", "root", uuid.NewString(), "Initial commit", time.Now()).Exec(); err != nil {
		t.Fatalf("seed attempt commit row: %v", err)
	}
	if _, _, err := helper.InitializeLibraryHeadIfUnset(orgID, emptyHeadRepo, "c-x", time.Now()); !errors.Is(err, v2pkg.ErrLibraryHeadUninitializable) || !strings.Contains(err.Error(), "empty string") {
		t.Fatalf("empty-string head: err=%v, want ErrLibraryHeadUninitializable (empty string)", err)
	}
	var danglingCX string
	if scanErr := database.Session().Query(`SELECT commit_id FROM commits WHERE library_id = ? AND commit_id = ?`, emptyHeadRepo, "c-x").Scan(&danglingCX); !errors.Is(scanErr, gocql.ErrNotFound) {
		t.Fatalf("a demonstrably-not-applied attempt (invalid empty-string head row) must discard its own unattributed commit row, got scan err=%v commit=%q", scanErr, danglingCX)
	}

	noCreatedAtRepo := uuid.NewString()
	if err := database.Session().Query(`INSERT INTO libraries (org_id, library_id, name) VALUES (?, ?, ?)`, orgID, noCreatedAtRepo, "inttest-h1-no-created-at").Exec(); err != nil {
		t.Fatalf("seed no-created_at row: %v", err)
	}
	defer database.Session().Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, noCreatedAtRepo).Exec()
	if err := database.Session().Query(`INSERT INTO commits (library_id, commit_id, root_fs_id, creator_id, description, created_at) VALUES (?, ?, ?, ?, ?, ?)`, noCreatedAtRepo, "c-y", "root", uuid.NewString(), "Initial commit", time.Now()).Exec(); err != nil {
		t.Fatalf("seed attempt commit row: %v", err)
	}
	if _, _, err := helper.InitializeLibraryHeadIfUnset(orgID, noCreatedAtRepo, "c-y", time.Now()); !errors.Is(err, v2pkg.ErrLibraryHeadUninitializable) || !strings.Contains(err.Error(), "created_at is null") {
		t.Fatalf("null created_at: err=%v, want ErrLibraryHeadUninitializable (created_at is null)", err)
	}
	var stillNull string
	if err := database.Session().Query(`SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, noCreatedAtRepo).Scan(&stillNull); err != nil || stillNull != "" {
		t.Fatalf("refused row must stay untouched: err=%v head=%q", err, stillNull)
	}
	var danglingCY string
	if scanErr := database.Session().Query(`SELECT commit_id FROM commits WHERE library_id = ? AND commit_id = ?`, noCreatedAtRepo, "c-y").Scan(&danglingCY); !errors.Is(scanErr, gocql.ErrNotFound) {
		t.Fatalf("a demonstrably-not-applied attempt (null created_at row) must discard its own unattributed commit row, got scan err=%v commit=%q", scanErr, danglingCY)
	}
}

// TestSettleAdoptedInitialHeadDiscardsKnownLoserEvenWhenVisibilityCheckFails
// pins the fix for the ordering bug found reviewing H1: a demonstrated
// KNOWN_LOSER's own attempt-unique commit is fully attributable to it
// regardless of whether the winning HEAD it lost to happens to be visible
// here yet, so the best-effort discard must not be skipped just because
// EnsureAdoptedHeadCommitVisible fails first. The winning head is left with
// no commit row at all so the visibility check fails deterministically on a
// single cluster, without needing the real 3-DC fixture.
func TestSettleAdoptedInitialHeadDiscardsKnownLoserEvenWhenVisibilityCheckFails(t *testing.T) {
	database, err := openIntegrationProjectionDB()
	if err != nil {
		t.Fatalf("open Cassandra: %v", err)
	}
	defer database.Close()

	repoID := uuid.NewString()
	losingCommitID := "c-loser"
	winningHead := "c-winner-never-written"
	if err := database.Session().Query(`INSERT INTO commits (library_id, commit_id, root_fs_id, creator_id, description, created_at) VALUES (?, ?, ?, ?, ?, ?)`, repoID, losingCommitID, "root", uuid.NewString(), "Initial commit", time.Now()).Exec(); err != nil {
		t.Fatalf("seed losing commit row: %v", err)
	}

	settleErr := v2pkg.NewFSHelper(database).SettleAdoptedInitialHead(repoID, losingCommitID, winningHead, v2pkg.InitialHeadAlreadyInitialized)
	if !errors.Is(settleErr, v2pkg.ErrLibraryHeadCommitNotVisibleLocally) {
		t.Fatalf("settle err=%v, want ErrLibraryHeadCommitNotVisibleLocally (winning head commit row was never written)", settleErr)
	}

	var stillThere string
	scanErr := database.Session().Query(`SELECT commit_id FROM commits WHERE library_id = ? AND commit_id = ?`, repoID, losingCommitID).Scan(&stillThere)
	if !errors.Is(scanErr, gocql.ErrNotFound) {
		t.Fatalf("KNOWN_LOSER's own commit must be discarded (best effort) even though the visibility check failed; got scan err=%v commit=%q", scanErr, stillThere)
	}
}
