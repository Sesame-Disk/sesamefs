//go:build integration

package integration

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

const libraryHardDeleteLeaseSerialDomainEvidenceEnv = "SESAMEFS_REQUIRE_LIBRARY_HARD_DELETE_LEASE_SERIAL_DOMAIN_EVIDENCE"

var libraryHardDeleteLeaseSerialDomainEvidence bool

type libraryHardDeleteLeaseContender struct {
	dc       string
	database interface{ Session() *gocql.Session }
	token    uuid.UUID
}

func libraryHardDeleteLeaseSerialDomain3DCReady(t *testing.T) map[string]string {
	t.Helper()
	require := os.Getenv(libraryHardDeleteLeaseSerialDomainEvidenceEnv) == "1"
	if strings.TrimSpace(os.Getenv(w2PostHeadMultidcEndpoints)) == "" {
		if require {
			t.Fatalf("%s=1 requires %s (sessions configured with LOCAL_SERIAL against the 3-DC fixture)", libraryHardDeleteLeaseSerialDomainEvidenceEnv, w2PostHeadMultidcEndpoints)
		}
		t.Skip(w2PostHeadMultidcEndpoints + " is not set")
	}
	return w2PostHead3DCEndpoints(t)
}

func TestLibraryHardDeleteLeaseSerialDomain3DC(t *testing.T) {
	endpoints := libraryHardDeleteLeaseSerialDomain3DCReady(t)
	dcNA := w2PostHead3DCConnectSerial(t, "dc-na", endpoints, "LOCAL_SERIAL")
	dcEU := w2PostHead3DCConnectSerial(t, "dc-eu", endpoints, "LOCAL_SERIAL")
	dcAsia := w2PostHead3DCConnectSerial(t, "dc-asia", endpoints, "LOCAL_SERIAL")

	var libraryID uuid.UUID
	var raced []libraryHardDeleteLeaseRound
	staleLibraryID := uuid.New()
	staleOwnerToken, takeoverToken := uuid.New(), uuid.New()
	nextOwnerToken := uuid.New()
	cleanupNeeded := true
	t.Cleanup(func() {
		if !cleanupNeeded {
			return
		}
		for _, round := range raced {
			for _, contender := range round.contenders {
				_ = gcpkg.ReleaseLibraryHardDeleteLockLease(contender.database.Session(), round.libraryID, contender.token)
			}
		}
		_ = gcpkg.ReleaseLibraryHardDeleteLockLease(dcNA.Session(), libraryID, nextOwnerToken)
		_ = gcpkg.ReleaseLibraryHardDeleteLockLease(dcNA.Session(), staleLibraryID, takeoverToken)
		_ = gcpkg.ReleaseLibraryHardDeleteLockLease(dcNA.Session(), staleLibraryID, staleOwnerToken)
	})

	// Global SERIAL contention between DCs can leave a proposal's outcome
	// unknown. Such an acquire must report an error and must not leave its token
	// owning the lease; a round in which every contender is ambiguous or loses
	// is re-raced on a fresh library. Two owners in any round is the failure.
	var results []libraryHardDeleteLeaseAcquireResult
	winnerIndex := -1
	for round := 1; winnerIndex < 0; round++ {
		if round > libraryHardDeleteLeaseRaceRounds {
			t.Fatalf("library hard-delete lease had no owner after %d concurrent rounds", libraryHardDeleteLeaseRaceRounds)
		}
		libraryID = uuid.New()
		contenders := []libraryHardDeleteLeaseContender{
			{dc: "dc-na", database: dcNA, token: uuid.New()},
			{dc: "dc-eu", database: dcEU, token: uuid.New()},
		}
		raced = append(raced, libraryHardDeleteLeaseRound{libraryID: libraryID, contenders: contenders})
		results = raceLibraryHardDeleteLease(contenders, libraryID)
		for i, result := range results {
			if result.err != nil {
				if !isAmbiguousLibraryHardDeleteLeaseError(result.err) {
					t.Fatalf("acquire from %s: %v", result.contender.dc, result.err)
				}
				t.Logf("round %d: acquire from %s had an unknown outcome: %v", round, result.contender.dc, result.err)
				continue
			}
			if result.acquired {
				if winnerIndex >= 0 {
					t.Fatalf("library hard-delete lease has multiple owners: %s and %s both acquired under LOCAL_SERIAL", results[winnerIndex].contender.dc, result.contender.dc)
				}
				winnerIndex = i
			}
		}
		owner, _, err := readLibraryHardDeleteLease3DC(t, dcAsia.Session(), libraryID)
		if err != nil && !errors.Is(err, gocql.ErrNotFound) {
			t.Fatalf("authoritative EACH_QUORUM read after round %d: %v", round, err)
		}
		for _, result := range results {
			if result.err != nil && owner == result.contender.token.String() {
				t.Fatalf("round %d: ambiguous acquire from %s left its token owning the lease", round, result.contender.dc)
			}
		}
		if winnerIndex < 0 && owner != "" {
			t.Fatalf("round %d: lease owned by %s although no contender acquired it", round, owner)
		}
	}
	winner, loser := results[winnerIndex].contender, results[1-winnerIndex].contender
	t.Logf("global SERIAL selected %s as the sole owner against contender in %s", winner.dc, loser.dc)

	owner, ttl, err := readLibraryHardDeleteLease3DC(t, winner.database.Session(), libraryID)
	if err != nil {
		t.Fatalf("authoritative EACH_QUORUM read after acquire: %v", err)
	}
	if owner != winner.token.String() {
		t.Fatalf("authoritative owner after acquire = %s, want %s from %s", owner, winner.token, winner.dc)
	}
	assertLibraryHardDeleteLeaseTTL(t, ttl, "acquire")

	// Operations under the owner's own token are idempotent, so an unknown CAS
	// outcome is retried. A non-owner operation cannot apply; an unknown outcome
	// there is accepted and the authoritative read below decides.
	var renewed bool
	err = retryAmbiguousLibraryHardDeleteLease(func() (err error) {
		renewed, err = gcpkg.RenewLibraryHardDeleteLockLease(winner.database.Session(), libraryID, winner.token)
		return err
	})
	if err != nil || !renewed {
		t.Fatalf("owner %s renew: applied=%v err=%v, want applied", winner.dc, renewed, err)
	}
	renewed, err = gcpkg.RenewLibraryHardDeleteLockLease(loser.database.Session(), libraryID, loser.token)
	if (err != nil && !isAmbiguousLibraryHardDeleteLeaseError(err)) || renewed {
		t.Fatalf("non-owner %s renew: applied=%v err=%v, want NOT_APPLIED", loser.dc, renewed, err)
	}
	owner, ttl, err = readLibraryHardDeleteLease3DC(t, dcAsia.Session(), libraryID)
	if err != nil {
		t.Fatalf("authoritative EACH_QUORUM read after renew: %v", err)
	}
	if owner != winner.token.String() {
		t.Fatalf("owner after cross-DC renew = %s, want %s", owner, winner.token)
	}
	assertLibraryHardDeleteLeaseTTL(t, ttl, "renew")

	if err := gcpkg.ReleaseLibraryHardDeleteLockLease(loser.database.Session(), libraryID, loser.token); err != nil && !isAmbiguousLibraryHardDeleteLeaseError(err) {
		t.Fatalf("wrong-token release from %s: %v", loser.dc, err)
	}
	owner, _, err = readLibraryHardDeleteLease3DC(t, dcAsia.Session(), libraryID)
	if err != nil {
		t.Fatalf("authoritative EACH_QUORUM read after wrong-token release: %v", err)
	}
	if owner != winner.token.String() {
		t.Fatalf("wrong-token release removed or changed owner: got %s, want %s", owner, winner.token)
	}

	if err := retryAmbiguousLibraryHardDeleteLease(func() error {
		return gcpkg.ReleaseLibraryHardDeleteLockLease(winner.database.Session(), libraryID, winner.token)
	}); err != nil {
		t.Fatalf("owner release from %s: %v", winner.dc, err)
	}
	if _, _, err := readLibraryHardDeleteLease3DC(t, dcAsia.Session(), libraryID); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("authoritative EACH_QUORUM read after owner release: err=%v, want no lease", err)
	}

	var acquired bool
	err = retryAmbiguousLibraryHardDeleteLease(func() (err error) {
		acquired, err = gcpkg.AcquireLibraryHardDeleteLockLease(dcEU.Session(), libraryID, nextOwnerToken)
		return err
	})
	if err != nil || !acquired {
		t.Fatalf("next owner acquire from dc-eu: applied=%v err=%v, want acquired", acquired, err)
	}
	owner, _, err = readLibraryHardDeleteLease3DC(t, dcAsia.Session(), libraryID)
	if err != nil {
		t.Fatalf("authoritative EACH_QUORUM read after next acquire: %v", err)
	}
	if owner != nextOwnerToken.String() {
		t.Fatalf("owner after release and next acquire = %s, want %s", owner, nextOwnerToken)
	}

	staleAt := time.Now().UTC().Add(-2 * time.Hour)
	w2PostHeadRetryEachQuorum(t, "seed stale lease row", func() error {
		return dcNA.Session().Query(`
			INSERT INTO gc_library_hard_delete_locks (library_id, started_at, heartbeat, lease_token)
			VALUES (?, ?, ?, ?)
		`, staleLibraryID.String(), staleAt, staleAt, staleOwnerToken.String()).Consistency(gocql.EachQuorum).Exec()
	})
	err = retryAmbiguousLibraryHardDeleteLease(func() (err error) {
		acquired, err = gcpkg.AcquireLibraryHardDeleteLockLease(dcEU.Session(), staleLibraryID, takeoverToken)
		return err
	})
	if err != nil || !acquired {
		t.Fatalf("stale cross-DC takeover from dc-eu: applied=%v err=%v, want acquired", acquired, err)
	}
	owner, ttl, err = readLibraryHardDeleteLease3DC(t, dcAsia.Session(), staleLibraryID)
	if err != nil {
		t.Fatalf("authoritative EACH_QUORUM read after stale takeover: %v", err)
	}
	if owner != takeoverToken.String() {
		t.Fatalf("stale takeover owner = %s, want %s", owner, takeoverToken)
	}
	assertLibraryHardDeleteLeaseTTL(t, ttl, "stale takeover")
	if err := retryAmbiguousLibraryHardDeleteLease(func() error {
		return gcpkg.ReleaseLibraryHardDeleteLockLease(dcEU.Session(), libraryID, nextOwnerToken)
	}); err != nil {
		t.Fatalf("next owner release from dc-eu: %v", err)
	}
	if err := retryAmbiguousLibraryHardDeleteLease(func() error {
		return gcpkg.ReleaseLibraryHardDeleteLockLease(dcEU.Session(), staleLibraryID, takeoverToken)
	}); err != nil {
		t.Fatalf("stale takeover owner release from dc-eu: %v", err)
	}

	cleanupNeeded = false
	libraryHardDeleteLeaseSerialDomainEvidence = true
}

const libraryHardDeleteLeaseRaceRounds = 5

type libraryHardDeleteLeaseRound struct {
	libraryID  uuid.UUID
	contenders []libraryHardDeleteLeaseContender
}

type libraryHardDeleteLeaseAcquireResult struct {
	contender libraryHardDeleteLeaseContender
	acquired  bool
	err       error
}

func raceLibraryHardDeleteLease(contenders []libraryHardDeleteLeaseContender, libraryID uuid.UUID) []libraryHardDeleteLeaseAcquireResult {
	results := make([]libraryHardDeleteLeaseAcquireResult, len(contenders))
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(len(contenders))
	for i, contender := range contenders {
		go func(i int, contender libraryHardDeleteLeaseContender) {
			defer wg.Done()
			<-start
			results[i].contender = contender
			results[i].acquired, results[i].err = gcpkg.AcquireLibraryHardDeleteLockLease(contender.database.Session(), libraryID, contender.token)
		}(i, contender)
	}
	close(start)
	wg.Wait()
	return results
}

// retryAmbiguousLibraryHardDeleteLease retries an operation under the caller's
// own lease token while its CAS outcome is unknown.
func retryAmbiguousLibraryHardDeleteLease(op func() error) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		if err = op(); err == nil || !isAmbiguousLibraryHardDeleteLeaseError(err) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return err
}

// isAmbiguousLibraryHardDeleteLeaseError mirrors the gc package's classifier for
// a lease LWT whose outcome Cassandra could not report.
func isAmbiguousLibraryHardDeleteLeaseError(err error) bool {
	var casUnknown *gocql.RequestErrCASWriteUnknown
	if errors.As(err, &casUnknown) || errors.Is(err, gocql.ErrTimeoutNoResponse) || errors.Is(err, gocql.ErrConnectionClosed) {
		return true
	}
	var writeTimeout *gocql.RequestErrWriteTimeout
	if errors.As(err, &writeTimeout) && writeTimeout.WriteType == "CAS" {
		return true
	}
	var writeFailure *gocql.RequestErrWriteFailure
	return errors.As(err, &writeFailure) && writeFailure.WriteType == "CAS"
}

// readLibraryHardDeleteLease3DC is the authoritative EACH_QUORUM read. A freshly
// started fixture can time out a cross-DC read; like w2PostHeadRetryEachQuorum,
// timeouts and unavailability are retried, while ErrNotFound is an answer.
func readLibraryHardDeleteLease3DC(t *testing.T, session *gocql.Session, libraryID uuid.UUID) (string, int, error) {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for {
		var owner string
		var ttl int
		err := session.Query(`
			SELECT lease_token, TTL(heartbeat) FROM gc_library_hard_delete_locks WHERE library_id = ?
		`, libraryID.String()).Consistency(gocql.EachQuorum).Scan(&owner, &ttl)
		if err == nil || errors.Is(err, gocql.ErrNotFound) || !isTransientLibraryHardDeleteLeaseReadError(err) || time.Now().After(deadline) {
			return owner, ttl, err
		}
		time.Sleep(2 * time.Second)
	}
}

func isTransientLibraryHardDeleteLeaseReadError(err error) bool {
	var unavailable *gocql.RequestErrUnavailable
	var readTimeout *gocql.RequestErrReadTimeout
	msg := strings.ToLower(err.Error())
	return errors.As(err, &unavailable) || errors.As(err, &readTimeout) ||
		strings.Contains(msg, "received only") || strings.Contains(msg, "timed out")
}

func assertLibraryHardDeleteLeaseTTL(t *testing.T, ttl int, operation string) {
	t.Helper()
	if ttl <= 0 || ttl > libraryHardDeleteLeaseTTLSeconds {
		t.Fatalf("library hard-delete lease TTL after %s = %d, want 1..%d", operation, ttl, libraryHardDeleteLeaseTTLSeconds)
	}
}

const libraryHardDeleteLeaseTTLSeconds = 21600
