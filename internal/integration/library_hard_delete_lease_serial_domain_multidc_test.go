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

	libraryID := uuid.New()
	contenders := []libraryHardDeleteLeaseContender{
		{dc: "dc-na", database: dcNA, token: uuid.New()},
		{dc: "dc-eu", database: dcEU, token: uuid.New()},
	}
	staleLibraryID := uuid.New()
	staleOwnerToken, takeoverToken := uuid.New(), uuid.New()
	nextOwnerToken := uuid.New()
	cleanupNeeded := true
	t.Cleanup(func() {
		if !cleanupNeeded {
			return
		}
		for _, contender := range contenders {
			_ = gcpkg.ReleaseLibraryHardDeleteLockLease(contender.database.Session(), libraryID, contender.token)
		}
		_ = gcpkg.ReleaseLibraryHardDeleteLockLease(dcNA.Session(), libraryID, nextOwnerToken)
		_ = gcpkg.ReleaseLibraryHardDeleteLockLease(dcNA.Session(), staleLibraryID, takeoverToken)
		_ = gcpkg.ReleaseLibraryHardDeleteLockLease(dcNA.Session(), staleLibraryID, staleOwnerToken)
	})

	type acquireResult struct {
		contender libraryHardDeleteLeaseContender
		acquired  bool
		err       error
	}
	results := make([]acquireResult, len(contenders))
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

	winnerIndex := -1
	for i, result := range results {
		if result.err != nil {
			t.Fatalf("acquire from %s: %v", result.contender.dc, result.err)
		}
		if result.acquired {
			if winnerIndex >= 0 {
				t.Fatalf("library hard-delete lease has multiple owners: %s and %s both acquired under LOCAL_SERIAL", results[winnerIndex].contender.dc, result.contender.dc)
			}
			winnerIndex = i
		}
	}
	if winnerIndex < 0 {
		t.Fatal("library hard-delete lease has no owner after two DC contenders")
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

	renewed, err := gcpkg.RenewLibraryHardDeleteLockLease(winner.database.Session(), libraryID, winner.token)
	if err != nil || !renewed {
		t.Fatalf("owner %s renew: applied=%v err=%v, want applied", winner.dc, renewed, err)
	}
	renewed, err = gcpkg.RenewLibraryHardDeleteLockLease(loser.database.Session(), libraryID, loser.token)
	if err != nil || renewed {
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

	if err := gcpkg.ReleaseLibraryHardDeleteLockLease(loser.database.Session(), libraryID, loser.token); err != nil {
		t.Fatalf("wrong-token release from %s: %v", loser.dc, err)
	}
	owner, _, err = readLibraryHardDeleteLease3DC(t, dcAsia.Session(), libraryID)
	if err != nil {
		t.Fatalf("authoritative EACH_QUORUM read after wrong-token release: %v", err)
	}
	if owner != winner.token.String() {
		t.Fatalf("wrong-token release removed or changed owner: got %s, want %s", owner, winner.token)
	}

	if err := gcpkg.ReleaseLibraryHardDeleteLockLease(winner.database.Session(), libraryID, winner.token); err != nil {
		t.Fatalf("owner release from %s: %v", winner.dc, err)
	}
	if _, _, err := readLibraryHardDeleteLease3DC(t, dcAsia.Session(), libraryID); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("authoritative EACH_QUORUM read after owner release: err=%v, want no lease", err)
	}

	acquired, err := gcpkg.AcquireLibraryHardDeleteLockLease(dcEU.Session(), libraryID, nextOwnerToken)
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
	if err := dcNA.Session().Query(`
		INSERT INTO gc_library_hard_delete_locks (library_id, started_at, heartbeat, lease_token)
		VALUES (?, ?, ?, ?)
	`, staleLibraryID.String(), staleAt, staleAt, staleOwnerToken.String()).Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("seed stale lease row: %v", err)
	}
	acquired, err = gcpkg.AcquireLibraryHardDeleteLockLease(dcEU.Session(), staleLibraryID, takeoverToken)
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
	if err := gcpkg.ReleaseLibraryHardDeleteLockLease(dcEU.Session(), libraryID, nextOwnerToken); err != nil {
		t.Fatalf("next owner release from dc-eu: %v", err)
	}
	if err := gcpkg.ReleaseLibraryHardDeleteLockLease(dcEU.Session(), staleLibraryID, takeoverToken); err != nil {
		t.Fatalf("stale takeover owner release from dc-eu: %v", err)
	}

	cleanupNeeded = false
	libraryHardDeleteLeaseSerialDomainEvidence = true
}

func readLibraryHardDeleteLease3DC(t *testing.T, session *gocql.Session, libraryID uuid.UUID) (string, int, error) {
	t.Helper()
	var owner string
	var ttl int
	err := session.Query(`
		SELECT lease_token, TTL(heartbeat) FROM gc_library_hard_delete_locks WHERE library_id = ?
	`, libraryID.String()).Consistency(gocql.EachQuorum).Scan(&owner, &ttl)
	return owner, ttl, err
}

func assertLibraryHardDeleteLeaseTTL(t *testing.T, ttl int, operation string) {
	t.Helper()
	if ttl <= 0 || ttl > libraryHardDeleteLeaseTTLSeconds {
		t.Fatalf("library hard-delete lease TTL after %s = %d, want 1..%d", operation, ttl, libraryHardDeleteLeaseTTLSeconds)
	}
}

const libraryHardDeleteLeaseTTLSeconds = 21600
