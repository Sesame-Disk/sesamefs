//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

func TestPCD1B5DestructionFenceIntentAndTakeoverOnCassandra(t *testing.T) {
	f := newPCD1B4Fixture(t, "pc-d1b5-intent")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	session := f.database.Session()

	initial := f.certify(t, dbpkg.LibraryBaselineCertifierIntegrationHooks{})
	if initial.Outcome != dbpkg.LibraryBaselineCertificationCertified {
		t.Fatalf("baseline certification = %s/%s (%v)", initial.Outcome, initial.Reason, initial.Diagnostic)
	}

	captured, err := dbpkg.CaptureDestructionFence(ctx, session, f.orgID, f.libraryID)
	if err != nil || captured.Status != dbpkg.DestructionFenceIdle || captured.Epoch != nil || len(captured.Pending) != 0 {
		t.Fatalf("initial SERIAL fence capture = %+v, %v", captured, err)
	}
	token := uuid.New()
	g1 := gocql.UUIDFromTime(time.Now().UTC().Add(-time.Second))
	first, err := dbpkg.BeginDestructionIntent(ctx, session, f.orgID, f.libraryID, token, g1, captured)
	if err != nil || first.Outcome != dbpkg.DestructionIntentApplied || first.Capability == nil {
		t.Fatalf("first intent = %+v, %v", first, err)
	}
	blocked := f.certify(t, dbpkg.LibraryBaselineCertifierIntegrationHooks{})
	if blocked.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || blocked.Reason != dbpkg.LibraryBaselineReasonIdentityDestructionPending {
		t.Fatalf("pending intent certification = %s/%s (%v), want NOT_CERTIFIED/identity_destruction_pending", blocked.Outcome, blocked.Reason, blocked.Diagnostic)
	}

	observed, err := dbpkg.CaptureDestructionFence(ctx, session, f.orgID, f.libraryID)
	if err != nil || observed.Status != dbpkg.DestructionFencePending || observed.Pending[token] != g1 {
		t.Fatalf("pending SERIAL capture = %+v, %v", observed, err)
	}
	g2 := gocql.UUIDFromTime(time.Now().UTC())
	second, err := dbpkg.BeginDestructionIntent(ctx, session, f.orgID, f.libraryID, token, g2, observed)
	if err != nil || second.Outcome != dbpkg.DestructionIntentApplied || second.Capability == nil {
		t.Fatalf("takeover intent = %+v, %v", second, err)
	}
	owned, err := dbpkg.CaptureDestructionFence(ctx, session, f.orgID, f.libraryID)
	if err != nil || owned.Status != dbpkg.DestructionFencePending || owned.Pending[token] != g2 || owned.Superseded == nil || *owned.Superseded != g1 {
		t.Fatalf("takeover fence = %+v, %v; want P[t]=G2 and S=G1", owned, err)
	}
	if completed, completeErr := dbpkg.CompleteDestructionIntent(ctx, session, *first.Capability); completeErr != nil || completed {
		t.Fatalf("stale completion = (%v, %v), want conditional miss", completed, completeErr)
	}
	stillOwned, err := dbpkg.CaptureDestructionFence(ctx, session, f.orgID, f.libraryID)
	if err != nil || stillOwned.Pending[token] != g2 {
		t.Fatalf("stale completion changed the new owner's pending token: %+v, %v", stillOwned, err)
	}
	if completed, completeErr := dbpkg.CompleteDestructionIntent(ctx, session, *second.Capability); completeErr != nil || !completed {
		t.Fatalf("current-owner completion = (%v, %v), want applied", completed, completeErr)
	}
	idle, err := dbpkg.CaptureDestructionFence(ctx, session, f.orgID, f.libraryID)
	if err != nil || idle.Status != dbpkg.DestructionFenceIdle || len(idle.Pending) != 0 || idle.Superseded == nil || *idle.Superseded != g1 {
		t.Fatalf("completed fence = %+v, %v", idle, err)
	}
}

func TestPCD1B5CanonicalAbsenceProofAndNoGhostIntentOnCassandra(t *testing.T) {
	database := shareProjectionDBForTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	orgID, libraryID := uuid.NewString(), uuid.NewString()
	createdAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)

	if _, err := dbpkg.CaptureDestructionFence(ctx, database.Session(), orgID, libraryID); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("capture absent canonical row = %v, want ErrNotFound", err)
	}
	proof, err := dbpkg.ProveGlobalCanonicalAbsence(ctx, database.Session(), orgID, libraryID)
	if err != nil || !proof.Matches(orgID, libraryID) {
		t.Fatalf("stable global canonical absence proof = %+v, %v", proof, err)
	}

	// Deliberately supply a stale/nonexistent observation. The exact created_at
	// predicate must make Cassandra return NOT_APPLIED without creating a row.
	stale := dbpkg.DestructionFenceSnapshot{Status: dbpkg.DestructionFenceIdle, CreatedAt: createdAt, Pending: map[uuid.UUID]gocql.UUID{}}
	generation := gocql.UUIDFromTime(time.Now().UTC().Add(-time.Second))
	result, err := dbpkg.BeginDestructionIntent(ctx, database.Session(), orgID, libraryID, uuid.New(), generation, stale)
	if err != nil || result.Outcome != dbpkg.DestructionIntentNotApplied || result.Capability != nil {
		t.Fatalf("absent-row intent = %+v, %v; want NOT_APPLIED", result, err)
	}
	var head *string
	if err := database.Session().Query(`SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, libraryID).Consistency(gocql.EachQuorum).Scan(&head); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("conditional intent created a ghost canonical row: head=%v err=%v", head, err)
	}

	if err := database.Session().Query(`
		INSERT INTO libraries (org_id, library_id, name, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, orgID, libraryID, "pc-d1b5-present-proof-veto", createdAt, createdAt).Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("seed canonical row for present-proof veto: %v", err)
	}
	presentProof, presentErr := dbpkg.ProveGlobalCanonicalAbsence(ctx, database.Session(), orgID, libraryID)
	if presentErr == nil || presentProof.Matches(orgID, libraryID) {
		t.Fatalf("present canonical row minted absence proof: proof=%+v err=%v", presentProof, presentErr)
	}
}
