//go:build integration

package integration

import (
	"context"
	"fmt"
	apipkg "github.com/Sesame-Disk/sesamefs/internal/api"
	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	"github.com/Sesame-Disk/sesamefs/internal/config"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Only discovery is narrowed: claim, zero-proof, handoff and finalization
// use the real store; physical existence is checked against MinIO. Recovery must never act on another fixture's D.
// Its global scan cursor is kept local to this test, not written to shared DB.
type w2OwnedRecoveryStore struct {
	gcpkg.GCStore
	org   uuid.UUID
	block string
}

func (s *w2OwnedRecoveryStore) LoadGCStats(string) (string, error) { return "", gocql.ErrNotFound }
func (s *w2OwnedRecoveryStore) SaveGCStats(string, string) error   { return nil }
func (s *w2OwnedRecoveryStore) ListS3OrphansByDay(day time.Time, bucket, limit int) ([]gcpkg.S3OrphanDiscoveryInfo, error) {
	// The productive LIMIT is global. Apply the fixture limit after filtering,
	// otherwise unrelated older rows can starve this test's exact projection.
	if limit <= 0 {
		limit = 100
	}
	for requested := limit; requested <= 1<<20; requested *= 2 {
		rows, err := s.GCStore.ListS3OrphansByDay(day, bucket, requested)
		if err != nil {
			return nil, err
		}
		var own []gcpkg.S3OrphanDiscoveryInfo
		for _, r := range rows {
			if r.OrgID == s.org && r.BlockID == s.block {
				own = append(own, r)
			}
		}
		if len(own) >= limit {
			return own[:limit], nil
		}
		if len(rows) < requested {
			return own, nil
		}
	}
	return nil, fmt.Errorf("fixture discovery partition exceeds isolation scan bound")
}

func (s *w2OwnedRecoveryStore) ListS3OrphanRecoveryRoots(bucket int, state []byte, limit int) (gcpkg.S3OrphanRecoveryRootPage, error) {
	page, err := s.GCStore.ListS3OrphanRecoveryRoots(bucket, state, limit)
	var own []gcpkg.S3OrphanRecoveryRootInfo
	for _, r := range page.Roots {
		if r.OrgID == s.org && r.BlockID == s.block {
			own = append(own, r)
		}
	}
	page.Roots = own
	return page, err
}
func w2Worker(t *testing.T, store gcpkg.GCStore, class string) *gcpkg.Worker {
	t.Helper()
	manager := storage.NewManager()
	manager.RegisterBackend(class, newVerificationS3Store(t), "")
	return gcpkg.NewWorker(store, gcpkg.NewStorageManagerAdapter(manager), gcpkg.NewQueue(store), 100, 0, false, &gcpkg.Stats{})
}
func w2Candidate(t *testing.T, store *gcpkg.CassandraStore, org uuid.UUID, block, class string) gcpkg.BlockGCCandidateInfo {
	t.Helper()
	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	c, err := store.EnsureBlockGCCandidateExact(org, block, class, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := enqueueExactBlockCandidateForTest(store, c, at); err != nil {
		t.Fatal(err)
	}
	// Do not let periodic server GC race the manually driven exclusive org.
	if err := store.RemoveOrgFromActiveSet(org, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	return c
}
func w2AssertCommittedContinuation(t *testing.T, store *gcpkg.CassandraStore, org uuid.UUID, block, class, key string, bs *storage.BlockStore) {
	t.Helper()
	if exists, err := store.BlockExists(org, block); err != nil || exists {
		t.Fatalf("W2 WORKER VIOLATION: committed D stalled by late repair: canonical exists=%v err=%v", exists, err)
	}
	// Discovery supplies identity only; confirm the exact canonical COMMITTED row.
	found := false
	var exactAuthority gcpkg.BlockDeleteAuthority
	for bucket := 0; bucket < dbpkg.GCDiscoveryBucketCount; bucket++ {
		var pageState []byte
		for {
			page, err := store.ListS3OrphanRecoveryRoots(bucket, pageState, 1000)
			if err != nil {
				t.Fatal(err)
			}
			for _, root := range page.Roots {
				if root.OrgID != org || root.BlockID != block {
					continue
				}
				orphan, exists, err := store.GetS3OrphanExact(org, block, root.Authority)
				if err != nil || !exists || orphan.RecoveryState != gcpkg.S3OrphanRecoveryStateCommitted || orphan.StorageClass != class || orphan.StorageKey != key {
					t.Fatalf("lost exact committed continuation: %+v exists=%v err=%v", orphan, exists, err)
				}
				exactAuthority = root.Authority
				found = true
			}
			pageState = page.PageState
			if len(pageState) == 0 {
				break
			}
		}
	}
	if !found {
		t.Fatal("committed physical continuation lost its recovery root")
	}

	scope := &w2OwnedRecoveryStore{GCStore: store, org: org, block: block}
	if n, err := w2Worker(t, scope, class).RecoverS3Orphans(t.Context(), 1000); err != nil || n != 1 {
		t.Fatalf("physical continuation recovered=%d err=%v", n, err)
	}
	if exists, err := bs.ObjectExists(t.Context(), key); err != nil || exists {
		t.Fatalf("committed physical continuation must delete exact K1: %v %v", exists, err)
	}
	if _, found, err := store.GetS3OrphanExact(org, block, exactAuthority); err != nil || found {
		t.Fatalf("completed D1 orphan survived: found=%v err=%v", found, err)
	}
	if _, found, err := store.GetS3OrphanRecoveryRootExact(org, block, exactAuthority); err != nil || found {
		t.Fatalf("completed D1 root survived: found=%v err=%v", found, err)
	}
	if n, err := w2Worker(t, scope, class).RecoverS3Orphans(t.Context(), 1000); err != nil || n != 0 {
		t.Fatalf("terminal restart recovered=%d err=%v", n, err)
	}
	var phase string
	if err := shareProjectionDBForTest(t).Session().Query(`SELECT phase FROM gc_block_delete_lifecycles WHERE org_id = ? AND block_id = ? AND claim_id = ?`, org.String(), block, exactAuthority.ClaimID).Scan(&phase); err != nil || phase != gcpkg.BlockDeleteLifecyclePhaseTerminal {
		t.Fatalf("expected terminal D phase=%s err=%v", phase, err)
	}
}

func TestW2WorkerRepairLifecycle(t *testing.T) {
	requireCassandra(t)
	database := shareProjectionDBForTest(t)
	store := gcpkg.NewCassandraStore(database)
	class := x1StorageClass(t)
	t.Run("repairBeforeCommitPreservesCandidate", func(t *testing.T) {
		w2ObservePublicationEvidence(t)
		org, block, bs := seedSyntheticBlock(t, class)
		x1Cleanup(t, database, org, block)
		t.Cleanup(func() { _ = bs.DeleteBlockByStorageKey(context.Background(), bs.StorageKeyForHash(block)) })
		repo, commit, fs := uuid.NewString(), uuid.NewString(), uuid.NewString()
		t.Cleanup(func() { _ = v2pkg.ClearPublishedFSObjectBlockReferenceRepair(database, org.String(), repo, commit, fs) })
		if err := v2pkg.QueuePublishedFSObjectBlockReferenceRepair(database, org.String(), repo, commit, fs, []string{block}); err != nil {
			t.Fatal(err)
		}
		c := w2Candidate(t, store, org, block, class)
		worker := w2Worker(t, store, class)
		if n, err := worker.ProcessOrgOnce(t.Context(), org); err != nil || n != 0 {
			t.Fatalf("W2 WORKER VIOLATION: repair-only consumed candidate/work item: processed=%d err=%v; want postpone", n, err)
		}
		if _, found, err := store.GetBlockGCCandidateExact(org, block, c.Identity()); err != nil || !found {
			t.Fatalf("W2 WORKER VIOLATION: repair-only consumed candidate: found=%v err=%v", found, err)
		}
		rows, err := store.ListBlockGCCandidatesByDay(c.CandidateAt, dbpkg.GCDiscoveryBucket(org.String(), block))
		if err != nil {
			t.Fatal(err)
		}
		projected := false
		for _, r := range rows {
			if r.OrgID == org && r.BlockID == block {
				projected = true
			}
		}
		if !projected {
			t.Fatal("candidate discovery projection lost while publication pending")
		}
		items, err := store.DequeueBatch(org, 100, time.Now().UTC().Add(time.Second))
		if err != nil || len(items) != 1 || items[0].RetryCount != 0 {
			t.Fatalf("postpone must retain one item without retry: %+v %v", items, err)
		}
		var state, claim *string
		var committed *bool
		if err := database.Session().Query(`SELECT gc_state,gc_claim_id,gc_orphan_handoff FROM blocks WHERE org_id = ? AND block_id = ?`, org.String(), block).Scan(&state, &claim, &committed); err != nil {
			t.Fatal(err)
		}
		if (state != nil && *state == "deleting") || (claim != nil && *claim != "") || (committed != nil && *committed) {
			t.Fatalf("guard-only left claim or D: state=%v claim=%v committed=%v", state, claim, committed)
		}
		if exists, err := bs.BlockExists(t.Context(), block); err != nil || !exists {
			t.Fatal("guard-only lost physical bytes")
		}
		if err := v2pkg.ClearPublishedFSObjectBlockReferenceRepair(database, org.String(), repo, commit, fs); err != nil {
			t.Fatal(err)
		}
		if n, err := worker.ProcessOrgOnce(t.Context(), org); err != nil || n != 1 {
			t.Fatalf("second pass processed=%d err=%v", n, err)
		}
		w2AssertCommittedContinuation(t, store, org, block, class, bs.StorageKeyForHash(block), bs)
	})
	t.Run("lateRepairDoesNotStallCommittedDelete", func(t *testing.T) {
		w2ObservePublicationEvidence(t)
		tenant := provisionIsolatedTenant(t, "w2-late-repair")
		repo := createTestLibrary(t, tenant.client, fmt.Sprintf("inttest-w2-worker-%d", time.Now().UnixNano()))
		org := mustParseUUID(t, tenant.orgID)
		initial := borrowedFSReadHead(t, database, tenant.orgID, repo)
		content := []byte("w2-worker-late-repair-" + uuid.NewString())
		tokens := dbpkg.NewTokenStore(database, time.Hour)
		token, err := tokens.CreateSyncToken(tenant.orgID, repo, tenant.userID)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := tokens.DeleteToken(token); err != nil {
				t.Error(err)
			}
		})
		syncClient := newTestClient(baseURL, token)
		fc := syncW2PutFileCommit(t, syncClient, repo, initial, "late.txt", content)
		fx := &w2CreateFileFixture{w2UploadFileFixture: &w2UploadFileFixture{borrowedFSHeadFixture: &borrowedFSHeadFixture{
			database: database, repoID: repo, orgID: tenant.orgID, orgUUID: org, userID: tenant.userID, content: content, blockID: fc.internalBlockID, sha1ID: fc.externalBlockID, headBefore: initial,
		}}}
		fx.target = fx.readTarget(t)
		x1Cleanup(t, database, org, fx.blockID)
		cleanupUploadedBlockArtifactsForTest(t, tenant.orgID, repo, fx.blockID, fx.sha1ID)
		bs := newVerificationBlockStore(t, tenant.orgID)
		t.Cleanup(func() { _ = bs.DeleteBlockByStorageKey(context.Background(), fx.target.StorageKey) })
		t.Cleanup(func() {
			for b := 0; b < 32; b++ {
				_ = database.Session().Query(`DELETE FROM published_block_reference_repairs WHERE bucket = ? AND org_id = ? AND repo_id = ?`, b, tenant.orgID, repo).Exec()
			}
		})
		manager := storage.NewManager()
		s3 := newVerificationS3Store(t)
		manager.SetDefaultClass(class)
		manager.RegisterBackend(class, s3, "")
		handler := apipkg.NewSyncHandler(database, s3, manager, &config.Config{Storage: config.StorageConfig{DefaultClass: class}}, nil)
		visited := false
		t.Cleanup(v2pkg.SetW2PublicationBeforeRepairForTest(repo, func() {
			visited = true
			w2ExpireTemporaryRefs(t, fx)
			w2Candidate(t, store, org, fx.blockID, class)
			a := x1Attempt(fx.target, "late-repair")
			x1ClaimAcquired(t, store, org, fx.blockID, a)
			live, err := store.BlockPublicationLivenessGlobal(org, fx.blockID)
			if err != nil || live != dbpkg.BlockPublicationZero {
				t.Fatalf("pre-D liveness=%v err=%v", live, err)
			}
			prepared := store.PrepareBlockDeleteOrphan(org, fx.blockID, a, fx.sha1ID, time.Now().UTC())
			if prepared.Cause != nil || prepared.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
				t.Fatalf("prepare=%v cause=%v", prepared.Outcome, prepared.Cause)
			}
			handoff, err := store.CommitBlockDeleteOrphanHandoff(org, fx.blockID, a)
			if err != nil || handoff.Outcome != gcpkg.BlockDeleteHandoffCommitted {
				t.Fatalf("handoff=%v err=%v", handoff, err)
			}
			fx.assertDUnrevoked(t, a)
		}))
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/seafhttp/repo/"+repo+"/update-branch?head="+fc.commitID, nil)
		c.Params = gin.Params{{Key: "repo_id", Value: repo}}
		c.Set("org_id", tenant.orgID)
		c.Set("user_id", tenant.userID)
		handler.UpdateBranch(c)
		if !visited || rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("late repair must reject HEAD: visited=%v status=%d %s", visited, rec.Code, rec.Body.String())
		}
		fx.assertHeadUnchanged(t)
		if len(w2Repairs(t, fx)) == 0 {
			t.Fatal("late shared repair must remain stranded for this regression")
		}
		if refs, err := database.ListBlockReferrers(tenant.orgID, fx.blockID); err != nil || len(refs) != 0 {
			t.Fatalf("need real zero refs: %v %v", refs, err)
		}
		if n, err := w2Worker(t, store, class).ProcessOrgOnce(t.Context(), org); err != nil || n != 1 {
			t.Fatalf("W2 WORKER VIOLATION: committed D stalled by late repair: processed=%d err=%v", n, err)
		}
		w2AssertCommittedContinuation(t, store, org, fx.blockID, class, fx.target.StorageKey, bs)
		fx.assertHeadUnchanged(t)
		if len(w2Repairs(t, fx)) == 0 {
			t.Fatal("GC must finish without inventing repair cleanup authority")
		}
	})
}
