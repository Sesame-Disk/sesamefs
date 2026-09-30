//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
)

// Delete only this isolated writer's TTL-bound refs, then use the production
// Cassandra GC claim / EACH_QUORUM zero-proof / irreversible handoff protocol.
// This models TTL expiry without shortening production TTLs or sleeping.
func TestW2PublicationLivenessThroughHEAD(t *testing.T) {
	requireCassandra(t)
	database := shareProjectionDBForTest(t)
	store := gcpkg.NewCassandraStore(database)
	handler := newBorrowedFSHeadHandler(t, database, x1StorageClass(t))
	for _, leg := range []string{"expiryBeforeAuthority", "expiryAfterAuthority"} {
		t.Run(leg, func(t *testing.T) {
			w2ObservePublicationEvidence(t)
			fx := newW2CreateFileFixture(t, database, handler, ".docx")
			var attempt gcpkg.BlockDeleteAuthority
			expired, protected := false, false
			t.Cleanup(v2pkg.SetCreateFileAfterMaterializedBarrierForTest(fx.repoID, func() {
				fx.target = fx.readTarget(t)
				refs, err := database.ListBlockReferrers(fx.orgID, fx.blockID)
				if err != nil {
					t.Fatal(err)
				}
				for _, ref := range refs {
					if strings.HasPrefix(ref, "up:") {
						fx.uploadRefs = append(fx.uploadRefs, ref)
					}
				}
			}))
			expireAndGC := func() {
				expired = true
				fx.assertPubCount(t, 1, "pub must exist at the selected expiry boundary")
				refs, err := database.ListBlockReferrers(fx.orgID, fx.blockID)
				if err != nil {
					t.Fatal(err)
				}
				for _, ref := range refs {
					if !strings.HasPrefix(ref, "up:") && !strings.HasPrefix(ref, "pub:") {
						t.Fatalf("unexpected saving reference %s", ref)
					}
					if err := database.RemoveBlockReference(fx.orgID, fx.blockID, ref); err != nil {
						t.Fatal(err)
					}
				}
				if leg == "expiryAfterAuthority" {
					candidate := x1Attempt(fx.target, "w2-0-"+leg)
					x1ClaimAcquired(t, store, fx.orgUUID, fx.blockID, candidate)
					live, err := store.BlockHasReferencesGlobal(fx.orgUUID, fx.blockID)
					if err != nil {
						t.Fatal(err)
					}
					if live {
						protected = true
						if _, err := store.ReleaseBlockClaim(fx.orgUUID, fx.blockID, candidate); err != nil {
							t.Fatal(err)
						}
						t.Log("EVIDENCE: zero TTL refs, durable repair gate blocks D at EACH_QUORUM")
						return
					}
					if _, err := store.ReleaseBlockClaim(fx.orgUUID, fx.blockID, candidate); err != nil {
						t.Fatal(err)
					}
				}
				attempt = x1CommitHandoffAfterZeroRefs(t, store, fx.orgUUID, fx.blockID, x1Attempt(fx.target, "w2-0-"+leg))
				fx.assertDUnrevoked(t, attempt)
				t.Log("EVIDENCE: own TTL refs expired; EACH_QUORUM zero-proof; D(P) committed")
			}
			if leg == "expiryBeforeAuthority" {
				t.Cleanup(v2pkg.SetFileFromBlocksPublicationBarriersForTest(fx.repoID, nil, nil, expireAndGC, nil))
			} else {
				t.Cleanup(v2pkg.SetW2PublicationAfterAuthorityForTest(fx.repoID, expireAndGC))
			}
			rec := fx.create(t)
			if !expired {
				t.Fatal("expiry barrier did not run")
			}
			head := borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
			if !protected && head != fx.headBefore {
				t.Errorf("W2-0 VIOLATION: D(P) committed AND HEAD advanced depending on P; status=%d head=%s previous=%s", rec.Code, head, fx.headBefore)
			}
			want := http.StatusConflict
			if protected {
				want = http.StatusCreated
			}
			if rec.Code != want {
				t.Errorf("status=%d body=%s; want retryable 409", rec.Code, rec.Body.String())
			}
			if !protected {
				fx.assertDUnrevoked(t, attempt)
			}
		})
	}
}
