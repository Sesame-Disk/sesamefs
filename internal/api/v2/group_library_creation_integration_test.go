//go:build integration

package v2

import (
	"errors"
	"sync"
	"testing"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

// Claim-primitive evidence against a real Cassandra (the LWT semantics are
// the point; a map-backed mock cannot stand in for Paxos). Run with:
//
//	go test -tags integration ./internal/api/v2/ -run GroupLibraryCreationClaim
//
// with CASSANDRA_HOSTS/USERNAME/PASSWORD pointing at the dev stack.

func claimKeyForTest() groupLibraryCreationClaim {
	return groupLibraryCreationClaim{
		OrgID:        uuid.NewString(),
		OwnerID:      uuid.NewString(),
		GroupID:      uuid.NewString(),
		Name:         "inttest-claim",
		StorageClass: "default",
		CreatedAt:    time.Now(),
	}
}

func deleteClaimUnconditionally(t *testing.T, session *gocql.Session, key groupLibraryCreationClaim) {
	t.Helper()
	if err := session.Query(`DELETE FROM group_library_creation_claims WHERE org_id = ? AND owner_id = ? AND group_id = ? AND name = ?`, key.OrgID, key.OwnerID, key.GroupID, key.Name).Exec(); err != nil {
		t.Errorf("cleanup claim: %v", err)
	}
}

// TestGroupLibraryCreationClaimSingleOwnerUnderConcurrency: N concurrent
// acquires of the same logical create (distinct library ids) — exactly one
// applies; every loser is handed the winner's claim, never its own and never
// nothing.
func TestGroupLibraryCreationClaimSingleOwnerUnderConcurrency(t *testing.T) {
	session := restoreGuardDBForTest(t).Session()
	key := claimKeyForTest()
	t.Cleanup(func() { deleteClaimUnconditionally(t, session, key) })

	const attempts = 12
	type result struct {
		mine    string
		current groupLibraryCreationClaim
		applied bool
		found   bool
		err     error
	}
	results := make([]result, attempts)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			claim := key
			claim.LibraryID = uuid.NewString()
			claim.ShareID = uuid.NewString()
			current, applied, found, err := acquireGroupLibraryCreationClaim(session, claim)
			results[i] = result{mine: claim.LibraryID, current: current, applied: applied, found: found, err: err}
		}(i)
	}
	close(start)
	wg.Wait()

	winners := 0
	winner := ""
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("attempt %d: %v", i, r.err)
		}
		if r.applied {
			winners++
			winner = r.mine
		}
	}
	if winners != 1 {
		t.Fatalf("applied claims = %d, want exactly 1 (Paxos single owner)", winners)
	}
	for i, r := range results {
		if r.applied {
			continue
		}
		if !r.found {
			t.Fatalf("attempt %d lost but was handed no owner; a loser must resume the winner", i)
		}
		if r.current.LibraryID != winner {
			t.Fatalf("attempt %d was handed owner %s, want the single winner %s", i, r.current.LibraryID, winner)
		}
	}
	stored, found, err := readGroupLibraryCreationClaim(session, key.OrgID, key.OwnerID, key.GroupID, key.Name, gocql.Serial)
	if err != nil || !found || stored.LibraryID != winner {
		t.Fatalf("stored claim = %+v found=%v err=%v, want winner %s", stored, found, err, winner)
	}
}

// TestGroupLibraryCreationClaimReleaseIsOwnershipChecked: attempt A cannot
// clear attempt B's claim; only the holder can release, and releasing an
// absent claim is a no-op rather than an error.
func TestGroupLibraryCreationClaimReleaseIsOwnershipChecked(t *testing.T) {
	session := restoreGuardDBForTest(t).Session()
	key := claimKeyForTest()
	t.Cleanup(func() { deleteClaimUnconditionally(t, session, key) })

	claimB := key
	claimB.LibraryID, claimB.ShareID = uuid.NewString(), uuid.NewString()
	if _, applied, _, err := acquireGroupLibraryCreationClaim(session, claimB); err != nil || !applied {
		t.Fatalf("B acquire: applied=%v err=%v", applied, err)
	}

	claimA := key
	claimA.LibraryID, claimA.ShareID = uuid.NewString(), uuid.NewString()
	if err := releaseGroupLibraryCreationClaim(session, claimA); err != nil {
		t.Fatalf("A's release of a claim it does not hold must be a no-op, got %v", err)
	}
	stored, found, err := readGroupLibraryCreationClaim(session, key.OrgID, key.OwnerID, key.GroupID, key.Name, gocql.Serial)
	if err != nil || !found || stored.LibraryID != claimB.LibraryID {
		t.Fatalf("after A's release B's claim must be intact: %+v found=%v err=%v", stored, found, err)
	}

	if err := releaseGroupLibraryCreationClaim(session, claimB); err != nil {
		t.Fatalf("B release: %v", err)
	}
	if _, found, err := readGroupLibraryCreationClaim(session, key.OrgID, key.OwnerID, key.GroupID, key.Name, gocql.Serial); err != nil || found {
		t.Fatalf("B's own release must remove the claim: found=%v err=%v", found, err)
	}
	if err := releaseGroupLibraryCreationClaim(session, claimB); err != nil {
		t.Fatalf("releasing an already-released claim must be a no-op, got %v", err)
	}
}

// TestGroupLibraryCreationClaimStaleClaimIsReleasedAndReplaced: a claim whose
// library row no longer exists is stale; beginGroupLibraryCreation releases
// it (ownership-checked) and mints a fresh library under a new claim, and
// never resumes the dead library id.
func TestGroupLibraryCreationClaimStaleClaimIsReleasedAndReplaced(t *testing.T) {
	database := restoreGuardDBForTest(t)
	session := database.Session()
	key := claimKeyForTest()
	t.Cleanup(func() { deleteClaimUnconditionally(t, session, key) })

	stale := key
	stale.LibraryID, stale.ShareID = uuid.NewString(), uuid.NewString()
	if _, applied, _, err := acquireGroupLibraryCreationClaim(session, stale); err != nil || !applied {
		t.Fatalf("seed stale claim: applied=%v err=%v", applied, err)
	}

	req := groupLibraryCreationRequest{OrgID: key.OrgID, OwnerID: key.OwnerID, GroupID: key.GroupID, Name: key.Name, ResolvedStorageClass: "default", Now: time.Now()}
	creation, err := beginGroupLibraryCreation(database, req, nil)
	if err != nil {
		t.Fatalf("begin over a stale claim: %v", err)
	}
	t.Cleanup(func() {
		_ = rollbackNewLibrary(database, creation.ProjectionRow)
	})
	if creation.Resumed || creation.Claim.LibraryID == stale.LibraryID {
		t.Fatalf("begin resumed the dead library %s (resumed=%v); a stale claim must be released and replaced", creation.Claim.LibraryID, creation.Resumed)
	}
	stored, found, err := readGroupLibraryCreationClaim(session, key.OrgID, key.OwnerID, key.GroupID, key.Name, gocql.Serial)
	if err != nil || !found || stored.LibraryID != creation.Claim.LibraryID {
		t.Fatalf("claim after replacement = %+v found=%v err=%v, want the fresh library %s", stored, found, err, creation.Claim.LibraryID)
	}
	if _, err := dbpkgReadLibraryName(session, key.OrgID, creation.Claim.LibraryID); err != nil {
		t.Fatalf("fresh library rows must exist: %v", err)
	}
}

func dbpkgReadLibraryName(session *gocql.Session, orgID, libraryID string) (string, error) {
	var name string
	err := session.Query(`SELECT name FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, libraryID).Scan(&name)
	if errors.Is(err, gocql.ErrNotFound) {
		return "", err
	}
	return name, err
}
