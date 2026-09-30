//go:build integration

package integration

import (
	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"net/http"
	"testing"
)

// Use the same productive irreversible claim/zero-proof/handoff as T2. A missing
// repair gate recreates D+HEAD, not merely a unit-contract failure.
func w2TryDeleteAfterExpiry(t *testing.T, fx *w2CreateFileFixture) bool {
	t.Helper()
	w2ExpireTemporaryRefs(t, fx)
	// Foreign fs: was removed by the BorrowedFS caller; no ref may save this leg.
	refs, err := fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
	if err != nil || len(refs) != 0 {
		t.Fatalf("not actual zero: %v %v", refs, err)
	}
	store := gcpkg.NewCassandraStore(fx.database)
	a := x1Attempt(fx.target, "w2-covered-writer")
	x1ClaimAcquired(t, store, fx.orgUUID, fx.blockID, a)
	live, err := store.BlockPublicationLivenessGlobal(fx.orgUUID, fx.blockID)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := store.ReleaseBlockClaim(fx.orgUUID, fx.blockID, a)
	if err != nil || outcome != gcpkg.BlockReleaseReleased {
		t.Fatalf("release=%v %v", outcome, err)
	}
	if live != dbpkg.BlockPublicationZero {
		return true
	}
	committed := x1CommitHandoffAfterZeroRefs(t, store, fx.orgUUID, fx.blockID, a)
	fx.assertDUnrevoked(t, committed)
	return false
}
func TestW2CoveredStoredAndSessionWriters(t *testing.T) {
	requireCassandra(t)
	t.Run("UploadFile", func(t *testing.T) {
		w2ObservePublicationEvidence(t)
		fx := w2RepairFixture(t)
		protected, visited := false, false
		t.Cleanup(v2pkg.SetW2PublicationAfterAuthorityForTest(fx.repoID, func() {
			visited = true
			fx.target = fx.readTarget(t)
			refs, err := fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range refs {
				if len(r) > 3 && r[:3] == "up:" {
					fx.uploadRefs = append(fx.uploadRefs, r)
				}
			}
			protected = w2TryDeleteAfterExpiry(t, fx)
		}))
		rec := fx.upload(t)
		if !protected && borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID) != fx.headBefore {
			t.Fatal("W2-0 VIOLATION: D(P) committed AND HEAD advanced depending on P")
		}
		if !visited || !protected || rec.Code != http.StatusOK || !fx.hasOwnFSReferrer(t) || len(w2Repairs(t, fx)) != 0 {
			t.Fatalf("UploadFile continuity/settlement: visited=%v protected=%v status=%d", visited, protected, rec.Code)
		}
	})
	for _, session := range []bool{false, true} {
		name := "BorrowedFS"
		if session {
			name = "SessionUpload"
		}
		t.Run(name, func(t *testing.T) {
			w2ObservePublicationEvidence(t)
			database := shareProjectionDBForTest(t)
			base := newBorrowedFSHeadFixture(t, database, newBorrowedFSHeadHandler(t, database, x1StorageClass(t)), x1StorageClass(t))
			fx := &w2CreateFileFixture{w2UploadFileFixture: &w2UploadFileFixture{borrowedFSHeadFixture: base}}
			if session {
				base.pinSessionUpload(t)
			}
			protected, visited := false, false
			t.Cleanup(v2pkg.SetW2PublicationAfterAuthorityForTest(fx.repoID, func() {
				visited = true
				base.dropForeignFS(t)
				protected = w2TryDeleteAfterExpiry(t, fx)
			}))
			rec := base.commit(t)
			if !protected && borrowedFSReadHead(t, database, fx.orgID, fx.repoID) != fx.headBefore {
				t.Fatal("W2-0 VIOLATION: D(P) committed AND HEAD advanced depending on P")
			}
			if !visited || !protected || rec.Code != http.StatusOK || !fx.hasOwnFSReferrer(t) || len(w2Repairs(t, fx)) != 0 {
				t.Fatalf("CFFB continuity/settlement: visited=%v protected=%v status=%d %s", visited, protected, rec.Code, rec.Body.String())
			}
		})
	}
}
