//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

// PC-D1B.4 certification-window characterization.
//
// These tests freeze what main@62a2c0e0 does today when a lifecycle mutation
// lands inside the certifier's window (after its final revalidation, before or
// around witness settlement) or after a witness exists. They assert CURRENT
// behavior, including the unsafe outcomes, so they stay green on main. The
// runtime fence (PC-D1B.5) must invert every assertion marked UNSAFE; see
// docs/PC-D1B-CERTIFICATION-WINDOW-FENCE.md, "Race matrix".
//
// Mutations are injected through the integration-only certifier hooks and
// through production primitives: the identity gateway deletes and claims, and
// the GC store's soft/hard delete. Restore is the unexported
// restoreDeletedLibrary; its canonical-row statements are replayed verbatim
// and TestPCD1B4LifecycleStatementsAreInventoried pins them to the source.

type pcd1b4Fixture struct {
	database  *dbpkg.DB
	orgID     string
	libraryID string
	ownerID   string
	head      string
	rootFSID  string
	fileFSID  string
	entries   string
}

func newPCD1B4Fixture(t *testing.T, label string) pcd1b4Fixture {
	t.Helper()
	database := shareProjectionDBForTest(t)
	f := pcd1b4Fixture{
		database:  database,
		orgID:     uuid.NewString(),
		libraryID: uuid.NewString(),
		ownerID:   uuid.NewString(),
		head:      "pc-d1b4-" + label + "-" + uuid.NewString(),
	}
	f.rootFSID = baselineCertifierTestFSID("pc-d1b4-root-" + f.head)
	f.fileFSID = baselineCertifierTestFSID("pc-d1b4-file-" + f.head)
	entries, err := json.Marshal([]map[string]interface{}{{"id": f.fileFSID, "mode": 33188, "mtime": time.Now().Unix(), "name": "covered.txt"}})
	if err != nil {
		t.Fatalf("marshal PC-D1B.4 root: %v", err)
	}
	f.entries = string(entries)
	seedLibraryBaselineCertifierLibrary(t, database, f.orgID, f.libraryID, f.ownerID, f.head, 0)
	seedLibraryBaselineCertifierCommit(t, database, f.libraryID, f.head, f.rootFSID)
	seedBaselineCertifierEdgeFixture(t, database, f.orgID, f.libraryID, f.head, f.rootFSID, f.entries, f.fileFSID, true)
	t.Cleanup(func() { f.cleanup(t) })
	return f
}

// cleanup leaves the fixture as the other certifier tests do: a live canonical
// row, or none. It never leaves a trashed library for the dev stack's GC.
func (f pcd1b4Fixture) cleanup(t *testing.T) {
	t.Helper()
	session := f.database.Session()
	_ = session.Query(`DELETE FROM deleted_libraries WHERE library_id = ?`, f.libraryID).Exec()
	var head *string
	err := session.Query(`SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?`, f.orgID, f.libraryID).Consistency(gocql.Serial).Scan(&head)
	if err == nil && (head == nil || *head == "") {
		// A witness-only ghost row (R9) has no HEAD; remove it entirely.
		_ = session.Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ?`, f.orgID, f.libraryID).Exec()
		return
	}
	_ = session.Query(`DELETE deleted_at, deleted_by FROM libraries WHERE org_id = ? AND library_id = ?`, f.orgID, f.libraryID).Exec()
}

func (f pcd1b4Fixture) certify(t *testing.T, hooks dbpkg.LibraryBaselineCertifierIntegrationHooks) dbpkg.LibraryBaselineCertificationResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return f.database.CertifyLibraryBaselineWithIntegrationHooks(ctx, storage.NewManager(), f.orgID, f.libraryID, f.head, hooks)
}

func (f pcd1b4Fixture) state(t *testing.T) dbpkg.LibraryState {
	t.Helper()
	var state dbpkg.LibraryState
	var certified, contract *string
	var head *string
	var deletedAt time.Time
	err := f.database.Session().Query(`
		SELECT head_commit_id, continuity_certified_head_commit_id, continuity_contract_version, deleted_at
		FROM libraries WHERE org_id = ? AND library_id = ?
	`, f.orgID, f.libraryID).Consistency(gocql.Serial).Scan(&head, &certified, &contract, &deletedAt)
	if err != nil {
		t.Fatalf("SERIAL read of PC-D1B.4 library state: %v", err)
	}
	state.OrgID, state.LibraryID = f.orgID, f.libraryID
	if head != nil {
		state.HeadCommitID = *head
	}
	state.ContinuityCertifiedHeadCommitID = certified
	state.ContinuityContractVersion = contract
	if !deletedAt.IsZero() {
		state.DeletedAt = &deletedAt
	}
	return state
}

func (f pcd1b4Fixture) witnessValid(t *testing.T) bool {
	t.Helper()
	return f.state(t).ContinuityWitnessValidFor(dbpkg.SupportedContinuityContractVersion)
}

func (f pcd1b4Fixture) fsObjectPresent(t *testing.T, fsID string) bool {
	t.Helper()
	_, err := dbpkg.ReadFSObjectIdentitySourceRow(context.Background(), f.database.Session(), f.libraryID, fsID)
	if errors.Is(err, gocql.ErrNotFound) {
		return false
	}
	if err != nil {
		t.Fatalf("read fs_object %s: %v", fsID, err)
	}
	return true
}

func (f pcd1b4Fixture) commitPresent(t *testing.T) bool {
	t.Helper()
	_, err := dbpkg.ReadCommitIdentitySourceRow(context.Background(), f.database.Session(), f.libraryID, f.head)
	if errors.Is(err, gocql.ErrNotFound) {
		return false
	}
	if err != nil {
		t.Fatalf("read commit %s: %v", f.head, err)
	}
	return true
}

func (f pcd1b4Fixture) softDelete(t *testing.T) {
	t.Helper()
	store := gcpkg.NewCassandraStore(f.database)
	if err := store.SoftDeleteLibrary(uuid.MustParse(f.orgID), uuid.MustParse(f.libraryID), uuid.MustParse(f.ownerID)); err != nil {
		t.Fatalf("production soft-delete: %v", err)
	}
}

// restore replays restoreDeletedLibrary's canonical-row statements.
func (f pcd1b4Fixture) restore(t *testing.T) {
	t.Helper()
	batch := f.database.Session().Batch(gocql.LoggedBatch)
	batch.Query(`UPDATE libraries SET updated_at = ? WHERE org_id = ? AND library_id = ?`, time.Now().UTC(), f.orgID, f.libraryID)
	batch.Query(`DELETE deleted_at, deleted_by FROM libraries WHERE org_id = ? AND library_id = ?`, f.orgID, f.libraryID)
	batch.Query(`DELETE FROM deleted_libraries WHERE library_id = ?`, f.libraryID)
	if err := batch.Exec(); err != nil {
		t.Fatalf("replay restore canonical statements: %v", err)
	}
}

func (f pcd1b4Fixture) deleteFile(t *testing.T) {
	t.Helper()
	if err := dbpkg.DeleteFSObjectIdentity(f.database.Session(), f.libraryID, f.fileFSID); err != nil {
		t.Fatalf("gateway fs_object delete: %v", err)
	}
}

func (f pcd1b4Fixture) zeroFileProjection() dbpkg.FSObjectProjection {
	return dbpkg.FSObjectProjection{
		LibraryID: f.libraryID, FSID: f.fileFSID, ObjectType: "file", SizeBytes: 0,
		FileLayout: dbpkg.FileStorageSHA1Only, LogicalSHA1IDs: []string{},
	}
}

func requirePCD1B4Outcome(t *testing.T, label string, got dbpkg.LibraryBaselineCertificationResult, outcome dbpkg.LibraryBaselineCertificationOutcome, reason dbpkg.LibraryBaselineCertificationReason) {
	t.Helper()
	if got.Outcome != outcome || got.Reason != reason {
		t.Fatalf("%s: certification=%s/%s (%v); CURRENT behavior is %s/%s", label, got.Outcome, got.Reason, got.Diagnostic, outcome, reason)
	}
}

// R4/R5 — a covered identity deleted after the final revalidation. UNSAFE:
// the witness settles and stays valid while the certified tree is gone.
func TestPCD1B4Characterization_InWindowIdentityDeleteSettlesWitness(t *testing.T) {
	t.Run("R5 reachable fs_object", func(t *testing.T) {
		f := newPCD1B4Fixture(t, "r5")
		result := f.certify(t, dbpkg.LibraryBaselineCertifierIntegrationHooks{
			BeforeWitnessCAS: func(context.Context, string, string, string) { f.deleteFile(t) },
		})
		requirePCD1B4Outcome(t, "R5", result, dbpkg.LibraryBaselineCertificationCertified, dbpkg.LibraryBaselineReasonApplied)
		if f.fsObjectPresent(t, f.fileFSID) || !f.witnessValid(t) {
			t.Fatalf("R5 CURRENT: want a valid witness over a deleted fs_object (file present=%v valid=%v)", f.fsObjectPresent(t, f.fileFSID), f.witnessValid(t))
		}
		recheck := f.certify(t, dbpkg.LibraryBaselineCertifierIntegrationHooks{})
		requirePCD1B4Outcome(t, "R5 fresh certification", recheck, dbpkg.LibraryBaselineCertificationNotCertified, dbpkg.LibraryBaselineReasonMissingFSObject)
		if !f.witnessValid(t) {
			t.Fatal("R5 CURRENT: a failed recertification does not clear the stale witness")
		}
	})
	t.Run("R4 HEAD commit", func(t *testing.T) {
		f := newPCD1B4Fixture(t, "r4")
		result := f.certify(t, dbpkg.LibraryBaselineCertifierIntegrationHooks{
			BeforeWitnessCAS: func(context.Context, string, string, string) {
				if err := dbpkg.DeleteCommitIdentity(f.database.Session(), f.libraryID, f.head); err != nil {
					t.Fatalf("gateway commit delete: %v", err)
				}
			},
		})
		requirePCD1B4Outcome(t, "R4", result, dbpkg.LibraryBaselineCertificationCertified, dbpkg.LibraryBaselineReasonApplied)
		if f.commitPresent(t) || !f.witnessValid(t) {
			t.Fatalf("R4 CURRENT: want a valid witness over a deleted HEAD commit (commit present=%v valid=%v)", f.commitPresent(t), f.witnessValid(t))
		}
	})
}

// R1/R3 — library lifecycle inside the window, single DC.
func TestPCD1B4Characterization_InWindowLibraryLifecycle(t *testing.T) {
	t.Run("R1 soft-delete is fenced by deleted_at in one DC", func(t *testing.T) {
		f := newPCD1B4Fixture(t, "r1")
		result := f.certify(t, dbpkg.LibraryBaselineCertifierIntegrationHooks{
			BeforeWitnessCAS: func(context.Context, string, string, string) { f.softDelete(t) },
		})
		requirePCD1B4Outcome(t, "R1", result, dbpkg.LibraryBaselineCertificationNotCertified, dbpkg.LibraryBaselineReasonWitnessNotApplied)
		if state := f.state(t); state.ContinuityCertifiedHeadCommitID != nil {
			t.Fatalf("R1 CURRENT: an acknowledged soft-delete must not receive a witness, got %v", *state.ContinuityCertifiedHeadCommitID)
		}
	})
	t.Run("R3 soft-delete then restore is an ABA on deleted_at", func(t *testing.T) {
		f := newPCD1B4Fixture(t, "r3")
		result := f.certify(t, dbpkg.LibraryBaselineCertifierIntegrationHooks{
			BeforeWitnessCAS: func(context.Context, string, string, string) {
				f.softDelete(t)
				f.restore(t)
			},
		})
		requirePCD1B4Outcome(t, "R3", result, dbpkg.LibraryBaselineCertificationCertified, dbpkg.LibraryBaselineReasonApplied)
		if !f.witnessValid(t) || !f.fsObjectPresent(t, f.fileFSID) {
			t.Fatal("R3 CURRENT: the witness applies across a trash round trip and the tree is still intact")
		}
	})
	t.Run("R3b destroy during the trash round trip is UNSAFE", func(t *testing.T) {
		f := newPCD1B4Fixture(t, "r3b")
		result := f.certify(t, dbpkg.LibraryBaselineCertifierIntegrationHooks{
			BeforeWitnessCAS: func(context.Context, string, string, string) {
				f.softDelete(t)
				f.deleteFile(t)
				f.restore(t)
			},
		})
		requirePCD1B4Outcome(t, "R3b", result, dbpkg.LibraryBaselineCertificationCertified, dbpkg.LibraryBaselineReasonApplied)
		if f.fsObjectPresent(t, f.fileFSID) || !f.witnessValid(t) {
			t.Fatal("R3b CURRENT: want a valid witness over a tree destroyed while the library was in trash")
		}
	})
}

// R6/R7 — delete/recreate at the same key, and an authority conflict attempt,
// inside the window. SAFE today because identity claims are immutable and
// survive source deletion (#230/#231): only the claimed digest can come back.
func TestPCD1B4Characterization_InWindowRecreateIsBoundByAuthority(t *testing.T) {
	divergent := func(f pcd1b4Fixture) dbpkg.FSObjectProjection {
		return dbpkg.FSObjectProjection{
			LibraryID: f.libraryID, FSID: f.fileFSID, ObjectType: "file", SizeBytes: 7,
			FileLayout: dbpkg.FileStorageSHA1Only, LogicalSHA1IDs: []string{baselineCertifierTestFSID("pc-d1b4-divergent-block-" + f.head)},
		}
	}
	t.Run("R6 delete and recreate same digest", func(t *testing.T) {
		f := newPCD1B4Fixture(t, "r6")
		var recreateErr, divergentErr error
		result := f.certify(t, dbpkg.LibraryBaselineCertifierIntegrationHooks{
			BeforeWitnessCAS: func(context.Context, string, string, string) {
				f.deleteFile(t)
				_, divergentErr = dbpkg.AuthorizeFSObjectProjection(context.Background(), f.database.Session(), divergent(f))
				seedBaselineCertifierEdgeFixture(t, f.database, f.orgID, f.libraryID, f.head, f.rootFSID, f.entries, f.fileFSID, false)
				_, recreateErr = dbpkg.AuthorizeFSObjectProjection(context.Background(), f.database.Session(), f.zeroFileProjection())
			},
		})
		if !errors.Is(divergentErr, dbpkg.IdentityAuthorityConflict) {
			t.Fatalf("R6: re-creating a deleted key with a different digest = %v, want IdentityAuthorityConflict", divergentErr)
		}
		if recreateErr != nil {
			t.Fatalf("R6: re-creating the claimed digest = %v, want an idempotent authorization", recreateErr)
		}
		requirePCD1B4Outcome(t, "R6", result, dbpkg.LibraryBaselineCertificationCertified, dbpkg.LibraryBaselineReasonApplied)
		if !f.fsObjectPresent(t, f.fileFSID) {
			t.Fatal("R6: the claimed identity was re-materialized and must be present")
		}
		verified, err := dbpkg.VerifyFSObjectProjection(context.Background(), f.database.Session(), f.zeroFileProjection())
		if err != nil || verified != dbpkg.IdentityVerificationVerified {
			t.Fatalf("R6: the surviving claim must still verify the original digest: %s %v", verified, err)
		}
	})
	t.Run("R7 authority conflict attempt on a live key", func(t *testing.T) {
		f := newPCD1B4Fixture(t, "r7")
		var conflictErr error
		result := f.certify(t, dbpkg.LibraryBaselineCertifierIntegrationHooks{
			BeforeWitnessCAS: func(context.Context, string, string, string) {
				_, conflictErr = dbpkg.AuthorizeFSObjectProjection(context.Background(), f.database.Session(), divergent(f))
			},
		})
		if !errors.Is(conflictErr, dbpkg.IdentityAuthorityConflict) {
			t.Fatalf("R7: divergent authorization of a live key = %v, want IdentityAuthorityConflict", conflictErr)
		}
		requirePCD1B4Outcome(t, "R7", result, dbpkg.LibraryBaselineCertificationCertified, dbpkg.LibraryBaselineReasonApplied)
	})
}

// R8/R9/R10 — lifecycle after the witness exists.
func TestPCD1B4Characterization_PostWitnessLifecycle(t *testing.T) {
	t.Run("R8 soft-delete invalidates while deleted", func(t *testing.T) {
		f := newPCD1B4Fixture(t, "r8")
		requirePCD1B4Outcome(t, "R8 baseline", f.certify(t, dbpkg.LibraryBaselineCertifierIntegrationHooks{}), dbpkg.LibraryBaselineCertificationCertified, dbpkg.LibraryBaselineReasonApplied)
		f.softDelete(t)
		state := f.state(t)
		if state.ContinuityWitnessValidFor(dbpkg.SupportedContinuityContractVersion) || state.ContinuityCertifiedHeadCommitID == nil {
			t.Fatalf("R8 CURRENT: the witness columns survive soft-delete but validity is false; got %+v", state)
		}
	})
	t.Run("R10 restore revives a witness over a tree destroyed in trash", func(t *testing.T) {
		f := newPCD1B4Fixture(t, "r10")
		requirePCD1B4Outcome(t, "R10 baseline", f.certify(t, dbpkg.LibraryBaselineCertifierIntegrationHooks{}), dbpkg.LibraryBaselineCertificationCertified, dbpkg.LibraryBaselineReasonApplied)
		f.softDelete(t)
		f.deleteFile(t)
		f.restore(t)
		if !f.witnessValid(t) || f.fsObjectPresent(t, f.fileFSID) {
			t.Fatal("R10 CURRENT (UNSAFE): restore revives the witness although a covered fs_object was destroyed during trash")
		}
	})
	t.Run("R10b witness survives a live-library destroy", func(t *testing.T) {
		f := newPCD1B4Fixture(t, "r10b")
		requirePCD1B4Outcome(t, "R10b baseline", f.certify(t, dbpkg.LibraryBaselineCertifierIntegrationHooks{}), dbpkg.LibraryBaselineCertificationCertified, dbpkg.LibraryBaselineReasonApplied)
		f.deleteFile(t)
		if !f.witnessValid(t) {
			t.Fatal("R10b CURRENT (UNSAFE): a destroy after settlement leaves the witness valid")
		}
	})
	t.Run("R9 hard delete removes the witness with the row", func(t *testing.T) {
		f := newPCD1B4Fixture(t, "r9")
		requirePCD1B4Outcome(t, "R9 baseline", f.certify(t, dbpkg.LibraryBaselineCertifierIntegrationHooks{}), dbpkg.LibraryBaselineCertificationCertified, dbpkg.LibraryBaselineReasonApplied)
		f.softDelete(t)
		if err := gcpkg.NewCassandraStore(f.database).HardDeleteLibrary(uuid.MustParse(f.orgID), uuid.MustParse(f.libraryID)); err != nil {
			t.Fatalf("production hard delete: %v", err)
		}
		var remaining string
		err := f.database.Session().Query(`SELECT library_id FROM libraries WHERE org_id = ? AND library_id = ?`, f.orgID, f.libraryID).Consistency(gocql.Serial).Scan(&remaining)
		if !errors.Is(err, gocql.ErrNotFound) {
			t.Fatalf("R9 CURRENT: the canonical row and its witness are gone after hard delete; read err=%v", err)
		}
	})
	t.Run("R9g witness commit ordered after a hard delete leaves a ghost row", func(t *testing.T) {
		f := newPCD1B4Fixture(t, "r9g")
		requirePCD1B4Outcome(t, "R9g baseline", f.certify(t, dbpkg.LibraryBaselineCertifierIntegrationHooks{}), dbpkg.LibraryBaselineCertificationCertified, dbpkg.LibraryBaselineReasonApplied)
		var witnessWriteTime int64
		if err := f.database.Session().Query(`SELECT WRITETIME(continuity_certified_head_commit_id) FROM libraries WHERE org_id = ? AND library_id = ?`, f.orgID, f.libraryID).Consistency(gocql.Serial).Scan(&witnessWriteTime); err != nil {
			t.Fatalf("read witness write time: %v", err)
		}
		// Timestamp-order model of the interleaving: the witness CAS read the
		// row before a concurrent plain hard delete reached the replicas, and
		// its Paxos ballot timestamp is later than the delete's client
		// timestamp. Replaying the production row delete with the earlier
		// timestamp yields exactly the merged state that interleaving leaves.
		if err := f.database.Session().Query(`DELETE FROM libraries USING TIMESTAMP ? WHERE org_id = ? AND library_id = ?`, witnessWriteTime-1, f.orgID, f.libraryID).Exec(); err != nil {
			t.Fatalf("timestamp-ordered hard delete: %v", err)
		}
		state := f.state(t)
		if state.HeadCommitID != "" || state.ContinuityCertifiedHeadCommitID == nil || *state.ContinuityCertifiedHeadCommitID != f.head {
			t.Fatalf("R9g CURRENT: want a ghost row with witness cells and no HEAD, got %+v", state)
		}
		if state.ContinuityWitnessValidFor(dbpkg.SupportedContinuityContractVersion) {
			t.Fatal("R9g: a ghost row must never be a valid witness (HEAD is null)")
		}
		exists, err := gcpkg.NewCassandraStore(f.database).CanonicalLibraryExists(uuid.MustParse(f.orgID), uuid.MustParse(f.libraryID))
		if err != nil || !exists {
			t.Fatalf("R9g CURRENT: GC's canonical-existence check sees the ghost as a present library (exists=%v err=%v)", exists, err)
		}
	})
}

// R11 — ambiguous witness CAS with a concurrent lifecycle mutation.
func TestPCD1B4Characterization_AmbiguousSettlementWithConcurrentLifecycle(t *testing.T) {
	unknown := errors.New("pc-d1b4 injected timeout after the witness CAS applied")
	t.Run("R11a applied then soft-deleted settles NOT_CERTIFIED but the witness persists", func(t *testing.T) {
		f := newPCD1B4Fixture(t, "r11a")
		result := f.certify(t, dbpkg.LibraryBaselineCertifierIntegrationHooks{
			AfterWitnessCAS: func(cas dbpkg.LibraryContinuityCASResult, err error) (dbpkg.LibraryContinuityCASResult, error) {
				if err != nil || cas.Outcome != dbpkg.LibraryContinuityCASApplied {
					t.Fatalf("R11a: the real witness CAS must apply before the injected ambiguity, got %+v %v", cas, err)
				}
				f.softDelete(t)
				return dbpkg.LibraryContinuityCASResult{Outcome: dbpkg.LibraryContinuityCASUnknown}, unknown
			},
		})
		requirePCD1B4Outcome(t, "R11a", result, dbpkg.LibraryBaselineCertificationNotCertified, dbpkg.LibraryBaselineReasonLibraryDeleted)
		f.restore(t)
		if !f.witnessValid(t) {
			t.Fatal("R11a CURRENT: the applied-but-reported-deleted witness revives on restore")
		}
	})
	t.Run("R11b applied then destroyed settles CERTIFIED", func(t *testing.T) {
		f := newPCD1B4Fixture(t, "r11b")
		result := f.certify(t, dbpkg.LibraryBaselineCertifierIntegrationHooks{
			AfterWitnessCAS: func(cas dbpkg.LibraryContinuityCASResult, err error) (dbpkg.LibraryContinuityCASResult, error) {
				if err != nil || cas.Outcome != dbpkg.LibraryContinuityCASApplied {
					t.Fatalf("R11b: the real witness CAS must apply before the injected ambiguity, got %+v %v", cas, err)
				}
				f.deleteFile(t)
				return dbpkg.LibraryContinuityCASResult{Outcome: dbpkg.LibraryContinuityCASUnknown}, unknown
			},
		})
		requirePCD1B4Outcome(t, "R11b", result, dbpkg.LibraryBaselineCertificationCertified, dbpkg.LibraryBaselineReasonWitnessSettled)
		if f.fsObjectPresent(t, f.fileFSID) {
			t.Fatal("R11b CURRENT (UNSAFE): settlement reports CERTIFIED over a destroyed covered fs_object")
		}
	})
}
