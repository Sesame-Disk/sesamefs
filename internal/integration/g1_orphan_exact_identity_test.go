//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/google/uuid"
)

const g1OrphanRequireEvidenceEnv = "SESAMEFS_REQUIRE_G1_ORPHAN_EVIDENCE"

type g1OrphanEvidenceGate struct{ observed bool }

func g1RequireOrphanEvidence(t *testing.T) *g1OrphanEvidenceGate {
	t.Helper()
	gate := &g1OrphanEvidenceGate{}
	if os.Getenv(g1OrphanRequireEvidenceEnv) != "1" {
		return gate
	}
	t.Cleanup(func() {
		if t.Skipped() {
			t.Errorf("%s=1 requires real Cassandra G1 orphan evidence, but the test skipped", g1OrphanRequireEvidenceEnv)
		} else if !t.Failed() && !gate.observed {
			t.Errorf("%s=1 completed without G1 orphan evidence", g1OrphanRequireEvidenceEnv)
		}
	})
	return gate
}

func TestG1OrphanExactIdentityAndDurableRecoveryAtRealCassandra(t *testing.T) {
	gate := g1RequireOrphanEvidence(t)
	requireCassandra(t)
	database := shareProjectionDBForTest(t)
	store := gcpkg.NewCassandraStore(database)

	t.Run("exact P-D mutations do not touch a sibling identity", func(t *testing.T) {
		orgID := uuid.New()
		blockID := g1IntegrationBlockID("exact")
		p1 := testCommittedOrphanAuthorityWithClaimID(blockID, "hot", syntheticCanonicalStorageKeyForTest(orgID.String(), blockID), "g1-p1-"+uuid.NewString())
		p2 := testCommittedOrphanAuthorityWithClaimID(blockID, "cold", "cold/"+blockID, "g1-p2-"+uuid.NewString())
		firstSeenAt := time.Now().UTC().Truncate(time.Millisecond).Add(123 * time.Microsecond)

		createdP1 := store.StartBlockDeleteOrphan(orgID, blockID, p1, "sha1-p1", firstSeenAt)
		if createdP1.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
			t.Fatalf("publish P1 = %s: %v", createdP1.Outcome, createdP1.Cause)
		}
		t.Cleanup(func() {
			_ = store.DeleteS3Orphan(orgID, blockID, p1.Authority(), createdP1.FirstSeenAt)
		})
		createdP2 := store.StartBlockDeleteOrphan(orgID, blockID, p2, "sha1-p2", firstSeenAt.Add(time.Second))
		if createdP2.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
			t.Fatalf("publish P2 = %s: %v", createdP2.Outcome, createdP2.Cause)
		}
		t.Cleanup(func() {
			_ = store.DeleteS3Orphan(orgID, blockID, p2.Authority(), createdP2.FirstSeenAt)
		})

		if err := store.UpdateS3OrphanAttempt(orgID, blockID, p1.Authority(), "P1 retry", time.Now().UTC()); err != nil {
			t.Fatalf("update P1: %v", err)
		}
		if err := store.DeleteS3Orphan(orgID, blockID, p1.Authority(), firstSeenAt); err != nil {
			t.Fatalf("delete P1: %v", err)
		}
		if _, found, err := store.GetS3OrphanExact(orgID, blockID, p1.Authority()); err != nil || found {
			t.Fatalf("exact P1 read after delete = found:%v err:%v, want absent", found, err)
		}
		remaining, found, err := store.GetS3OrphanExact(orgID, blockID, p2.Authority())
		if err != nil || !found {
			t.Fatalf("exact P2 read after P1 delete = found:%v err:%v, want present", found, err)
		}
		if remaining.ExternalSHA1 != "sha1-p2" || remaining.Authority.ClaimID != p2.Authority().ClaimID {
			t.Fatalf("P2 changed after exact P1 mutations: %+v", remaining)
		}
		discovery, err := store.ListS3OrphansByDay(createdP2.FirstSeenAt, db.GCDiscoveryBucket(orgID.String(), blockID), 100)
		if err != nil {
			t.Fatalf("list P2 discovery after P1 delete: %v", err)
		}
		foundP1Discovery := false
		foundP2Discovery := false
		for _, row := range discovery {
			if row.OrgID != orgID || row.BlockID != blockID {
				continue
			}
			if g1SameAuthority(row.Authority, p1.Authority()) {
				foundP1Discovery = true
			}
			if g1SameAuthority(row.Authority, p2.Authority()) {
				foundP2Discovery = true
			}
		}
		if foundP1Discovery {
			t.Fatal("P1 discovery projection remained after exact delete")
		}
		if !foundP2Discovery {
			t.Fatal("P2 discovery projection disappeared after exact P1 delete")
		}

	})

	t.Run("same P with different D remains isolated and normalized", func(t *testing.T) {
		orgID := uuid.New()
		blockID := g1IntegrationBlockID("same-p-different-d")
		storageKey := syntheticCanonicalStorageKeyForTest(orgID.String(), blockID)
		claimedAt := time.Now().UTC().Add(-time.Hour).Add(123456 * time.Nanosecond)
		firstSeenAt := claimedAt.Add(time.Second)
		p1 := gcpkg.CommittedBlockDeleteAuthorityForTest(gcpkg.BlockDeleteAuthority{
			Target:    gcpkg.BlockDeleteTarget{StorageClass: "hot", StorageKey: storageKey},
			ClaimID:   "g1-same-p-d1-" + uuid.NewString(),
			ClaimedAt: claimedAt,
		})
		p2 := gcpkg.CommittedBlockDeleteAuthorityForTest(gcpkg.BlockDeleteAuthority{
			Target:    gcpkg.BlockDeleteTarget{StorageClass: "hot", StorageKey: storageKey},
			ClaimID:   "g1-same-p-d2-" + uuid.NewString(),
			ClaimedAt: claimedAt.Add(time.Second),
		})

		createdP1 := store.StartBlockDeleteOrphan(orgID, blockID, p1, "sha1-d1", firstSeenAt)
		if createdP1.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
			t.Fatalf("publish P1/D1 = %s: %v", createdP1.Outcome, createdP1.Cause)
		}
		createdP2 := store.StartBlockDeleteOrphan(orgID, blockID, p2, "sha1-d2", firstSeenAt.Add(time.Second))
		if createdP2.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
			t.Fatalf("publish P1/D2 = %s: %v", createdP2.Outcome, createdP2.Cause)
		}
		t.Cleanup(func() {
			_ = store.DeleteS3Orphan(orgID, blockID, p1.Authority(), createdP1.FirstSeenAt)
			_ = store.DeleteS3Orphan(orgID, blockID, p2.Authority(), createdP2.FirstSeenAt)
		})

		roundTripP1, found, err := store.GetS3OrphanExact(orgID, blockID, p1.Authority())
		if err != nil || !found {
			t.Fatalf("P1/D1 round trip = found:%v err:%v", found, err)
		}
		if !createdP1.FirstSeenAt.Equal(firstSeenAt.Truncate(time.Millisecond)) || !roundTripP1.Authority.ClaimedAt.Equal(claimedAt.Truncate(time.Millisecond)) {
			t.Fatalf("sub-millisecond normalization = first_seen:%v claimed_at:%v", createdP1.FirstSeenAt, roundTripP1.Authority.ClaimedAt)
		}
		replayed := store.StartBlockDeleteOrphan(orgID, blockID, p2, "sha1-replayed", firstSeenAt.Add(2*time.Second))
		if replayed.Outcome != gcpkg.StartBlockDeleteOrphanSameAuthority {
			t.Fatalf("replayed P1/D2 = %s, want same_authority", replayed.Outcome)
		}
		if root, found := g1RecoveryRootInfo(t, store, orgID, blockID, p2.Authority()); !found || !root.FirstSeenAt.Equal(createdP2.FirstSeenAt) {
			t.Fatalf("replayed root first_seen_at = (%v, found:%v), want stable %v", root.FirstSeenAt, found, createdP2.FirstSeenAt)
		}

		if err := store.DeleteS3Orphan(orgID, blockID, p1.Authority(), createdP1.FirstSeenAt); err != nil {
			t.Fatalf("delete P1/D1: %v", err)
		}
		remaining, found, err := store.GetS3OrphanExact(orgID, blockID, p2.Authority())
		if err != nil || !found {
			t.Fatalf("P1/D2 after P1/D1 delete = found:%v err:%v, want present", found, err)
		}
		if remaining.ExternalSHA1 != "sha1-d2" || remaining.Authority.Target != p2.Authority().Target ||
			remaining.Authority.ClaimID != p2.Authority().ClaimID ||
			!remaining.Authority.ClaimedAt.Equal(p2.Authority().ClaimedAt.UTC().Truncate(time.Millisecond)) {
			t.Fatalf("P1/D2 changed after sibling settlement: %+v", remaining)
		}
	})

	t.Run("old root is restart-findable without historical cursor rewind", func(t *testing.T) {
		orgID := uuid.New()
		blockID := g1IntegrationBlockID("old-root")
		authority := testCommittedOrphanAuthority(blockID, "hot", syntheticCanonicalStorageKeyForTest(orgID.String(), blockID))
		firstSeenAt := time.Now().UTC().AddDate(0, 0, -120).Truncate(time.Millisecond)
		created := store.StartBlockDeleteOrphan(orgID, blockID, authority, "", firstSeenAt)
		if created.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
			t.Fatalf("publish old root = %s: %v", created.Outcome, created.Cause)
		}
		t.Cleanup(func() {
			_ = store.DeleteS3Orphan(orgID, blockID, authority.Authority(), created.FirstSeenAt)
		})
		g1DeleteOrphanProjection(t, database, orgID, blockID, authority.Authority(), created.FirstSeenAt)

		worker := gcpkg.NewWorker(store, nil, gcpkg.NewQueue(store), 100, 0, false, &gcpkg.Stats{})
		if recovered, err := worker.RecoverS3Orphans(context.Background(), 100); err != nil || recovered != 0 {
			t.Fatalf("old-root reconciliation = (%d, %v), want root repair without physical execution", recovered, err)
		}
		discovery, err := store.ListS3OrphansByDay(created.FirstSeenAt, db.GCDiscoveryBucket(orgID.String(), blockID), 10)
		if err != nil {
			t.Fatalf("list repaired old-root projection: %v", err)
		}
		if len(discovery) != 1 || !g1SameAuthority(discovery[0].Authority, authority.Authority()) {
			t.Fatalf("old-root projection after restart = %+v, want exact repaired identity", discovery)
		}
	})

	t.Run("recovery root pagination finds every exact identity", func(t *testing.T) {
		type fixture struct {
			orgID     uuid.UUID
			blockID   string
			authority gcpkg.BlockDeleteAuthority
			firstSeen time.Time
		}
		byBucket := make(map[int][]fixture)
		var selectedBucket int
		var selected []fixture
		for i := 0; i < 128 && len(selected) < 3; i++ {
			orgID := uuid.New()
			blockID := g1IntegrationBlockID(fmt.Sprintf("root-page-%d", i))
			authority := testCommittedOrphanAuthorityWithClaimID(blockID, "hot", syntheticCanonicalStorageKeyForTest(orgID.String(), blockID), "g1-page-"+uuid.NewString()).Authority()
			firstSeen := time.Now().UTC().Add(-time.Hour).Add(time.Duration(i) * time.Millisecond)
			created := store.StartBlockDeleteOrphan(orgID, blockID, gcpkg.CommittedBlockDeleteAuthorityForTest(authority), "", firstSeen)
			if created.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
				t.Fatalf("seed root page %d = %s: %v", i, created.Outcome, created.Cause)
			}
			fixture := fixture{orgID: orgID, blockID: blockID, authority: authority, firstSeen: created.FirstSeenAt}
			byBucket[g1RecoveryRootBucket(authority)] = append(byBucket[g1RecoveryRootBucket(authority)], fixture)
			for bucket, fixtures := range byBucket {
				if len(fixtures) >= 3 {
					selectedBucket = bucket
					selected = fixtures
					break
				}
			}
		}
		if len(selected) < 3 {
			t.Fatalf("could not seed three roots in one bucket; buckets=%v", byBucket)
		}
		for _, item := range byBucket {
			for _, root := range item {
				t.Cleanup(func() {
					_ = store.DeleteS3Orphan(root.orgID, root.blockID, root.authority, root.firstSeen)
				})
			}
		}

		want := make(map[string]struct{}, len(selected))
		for _, item := range selected {
			want[item.blockID] = struct{}{}
		}
		seen := make(map[string]struct{}, len(want))
		var pageState []byte
		pages := 0
		for {
			page, err := store.ListS3OrphanRecoveryRoots(selectedBucket, pageState, 2)
			if err != nil {
				t.Fatalf("list root page %d: %v", pages, err)
			}
			pages++
			for _, root := range page.Roots {
				if _, ok := want[root.BlockID]; ok {
					seen[root.BlockID] = struct{}{}
				}
			}
			if len(page.PageState) == 0 {
				break
			}
			pageState = page.PageState
		}
		if pages < 2 || len(seen) != len(want) {
			t.Fatalf("root pagination pages=%d seen=%d want=%d", pages, len(seen), len(want))
		}
	})

	t.Run("root repairs projection and recovers exact canonical row", func(t *testing.T) {
		orgID := uuid.New()
		blockID := g1IntegrationBlockID("repair")
		authority := testCommittedOrphanAuthority(blockID, "hot", syntheticCanonicalStorageKeyForTest(orgID.String(), blockID))
		firstSeenAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
		created := store.StartBlockDeleteOrphan(orgID, blockID, authority, "", firstSeenAt)
		if created.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
			t.Fatalf("publish repair row = %s: %v", created.Outcome, created.Cause)
		}
		t.Cleanup(func() {
			_ = store.DeleteS3Orphan(orgID, blockID, authority.Authority(), created.FirstSeenAt)
		})
		g1DeleteOrphanProjection(t, database, orgID, blockID, authority.Authority(), created.FirstSeenAt)

		storage := &gcpkg.MockStorageProvider{}
		worker := gcpkg.NewWorker(store, storage, gcpkg.NewQueue(store), 100, 0, false, &gcpkg.Stats{})
		recovered, err := worker.RecoverS3Orphans(context.Background(), 100)
		if err != nil || recovered != 1 {
			t.Fatalf("RecoverS3Orphans = (%d, %v), want one exact root recovery", recovered, err)
		}
		if len(storage.DeletedBlocks()) != 1 || storage.DeletedBlocks()[0] != authority.Authority().Target.StorageKey {
			t.Fatalf("physical recovery = %v, want %q", storage.DeletedBlocks(), authority.Authority().Target.StorageKey)
		}
		if _, found, err := store.GetS3OrphanExact(orgID, blockID, authority.Authority()); err != nil || found {
			t.Fatalf("recovered canonical row = found:%v err:%v, want absent", found, err)
		}
		if g1RecoveryRootExists(t, store, orgID, blockID, authority.Authority()) {
			t.Fatal("recovery root remained after exact settlement")
		}
	})

	t.Run("root without canonical remains durable", func(t *testing.T) {
		orgID := uuid.New()
		blockID := g1IntegrationBlockID("missing-canonical")
		authority := testCommittedOrphanAuthority(blockID, "hot", syntheticCanonicalStorageKeyForTest(orgID.String(), blockID))
		created := store.StartBlockDeleteOrphan(orgID, blockID, authority, "", time.Now().UTC().Add(-time.Hour))
		if created.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
			t.Fatalf("publish root-only row = %s: %v", created.Outcome, created.Cause)
		}
		t.Cleanup(func() {
			_ = store.DeleteS3OrphanRecoveryRoot(orgID, blockID, authority.Authority())
		})
		g1DeleteOrphanCanonical(t, database, orgID, blockID, authority.Authority())
		g1DeleteOrphanProjection(t, database, orgID, blockID, authority.Authority(), created.FirstSeenAt)

		storage := &gcpkg.MockStorageProvider{}
		worker := gcpkg.NewWorker(store, storage, gcpkg.NewQueue(store), 100, 0, false, &gcpkg.Stats{})
		recovered, err := worker.RecoverS3Orphans(context.Background(), 100)
		if err != nil || recovered != 0 {
			t.Fatalf("root-only recovery = (%d, %v), want no physical recovery", recovered, err)
		}
		if len(storage.DeletedBlocks()) != 0 || !g1RecoveryRootExists(t, store, orgID, blockID, authority.Authority()) {
			t.Fatalf("root-only lifecycle was retired: deletes=%v root=%t", storage.DeletedBlocks(), g1RecoveryRootExists(t, store, orgID, blockID, authority.Authority()))
		}
	})

	t.Run("PREPARED canonical state remains retained", func(t *testing.T) {
		orgID := uuid.New()
		blockID := g1IntegrationBlockID("prepared")
		authority := testCommittedOrphanAuthority(blockID, "hot", syntheticCanonicalStorageKeyForTest(orgID.String(), blockID))
		created := store.StartBlockDeleteOrphan(orgID, blockID, authority, "", time.Now().UTC().Add(-time.Hour))
		if created.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
			t.Fatalf("publish prepared row = %s: %v", created.Outcome, created.Cause)
		}
		t.Cleanup(func() {
			_ = store.DeleteS3Orphan(orgID, blockID, authority.Authority(), created.FirstSeenAt)
		})
		g1DeleteOrphanProjection(t, database, orgID, blockID, authority.Authority(), created.FirstSeenAt)
		if err := database.Session().Query(`
			UPDATE gc_s3_orphans SET recovery_state = ?
			WHERE org_id = ? AND block_id = ? AND storage_class = ? AND storage_key = ?
			  AND gc_claim_id = ? AND gc_claimed_at = ?
		`, gcpkg.S3OrphanRecoveryStatePrepared, orgID.String(), blockID,
			authority.Authority().Target.StorageClass, authority.Authority().Target.StorageKey,
			authority.Authority().ClaimID, authority.Authority().ClaimedAt).Exec(); err != nil {
			t.Fatalf("mark PREPARED: %v", err)
		}

		storage := &gcpkg.MockStorageProvider{}
		worker := gcpkg.NewWorker(store, storage, gcpkg.NewQueue(store), 100, 0, false, &gcpkg.Stats{})
		recovered, err := worker.RecoverS3Orphans(context.Background(), 100)
		if err != nil || recovered != 0 {
			t.Fatalf("prepared recovery = (%d, %v), want retained state", recovered, err)
		}
		canonical, found, err := store.GetS3OrphanExact(orgID, blockID, authority.Authority())
		if err != nil || !found || canonical.RecoveryState != gcpkg.S3OrphanRecoveryStatePrepared {
			t.Fatalf("prepared canonical row = found:%v state:%q err:%v, want retained PREPARED", found, canonical.RecoveryState, err)
		}
		if len(storage.DeletedBlocks()) != 0 || !g1RecoveryRootExists(t, store, orgID, blockID, authority.Authority()) {
			t.Fatalf("prepared lifecycle changed: deletes=%v root=%t", storage.DeletedBlocks(), g1RecoveryRootExists(t, store, orgID, blockID, authority.Authority()))
		}
		replayed := store.StartBlockDeleteOrphan(orgID, blockID, authority, "", time.Now().UTC())
		if replayed.Outcome == gcpkg.StartBlockDeleteOrphanCreated || replayed.Outcome == gcpkg.StartBlockDeleteOrphanSameAuthority {
			t.Fatalf("PREPARED replay returned destructive publication outcome %s", replayed.Outcome)
		}
		if err := store.DeleteS3Orphan(orgID, blockID, authority.Authority(), created.FirstSeenAt); err != nil {
			t.Fatalf("cleanup prepared row: %v", err)
		}
	})

	t.Run("terminal settlement clears discovery before root", func(t *testing.T) {
		orgID := uuid.New()
		blockID := g1IntegrationBlockID("terminal-settlement")
		authority := testCommittedOrphanAuthority(blockID, "hot", syntheticCanonicalStorageKeyForTest(orgID.String(), blockID))
		created := store.StartBlockDeleteOrphan(orgID, blockID, authority, "", time.Now().UTC().Add(-time.Hour))
		if created.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
			t.Fatalf("publish terminal-settlement row = %s: %v", created.Outcome, created.Cause)
		}
		if terminated, err := store.TerminateBlockDeleteLifecycle(orgID, blockID, authority); err != nil ||
			(terminated.Outcome != gcpkg.BlockDeleteLifecycleTerminated && terminated.Outcome != gcpkg.BlockDeleteLifecycleAlreadyTerminal) {
			t.Fatalf("terminate terminal-settlement lifecycle = %+v: %v", terminated, err)
		}
		t.Cleanup(func() {
			_ = store.DeleteS3OrphanRecoveryRoot(orgID, blockID, authority.Authority())
		})
		g1DeleteOrphanCanonical(t, database, orgID, blockID, authority.Authority())

		worker := gcpkg.NewWorker(store, &gcpkg.MockStorageProvider{}, gcpkg.NewQueue(store), 100, 0, false, &gcpkg.Stats{})
		recovered, err := worker.RecoverS3Orphans(context.Background(), 100)
		if err != nil || recovered != 1 {
			t.Fatalf("terminal root settlement = (%d, %v), want one metadata settlement", recovered, err)
		}
		if g1RecoveryRootExists(t, store, orgID, blockID, authority.Authority()) {
			t.Fatal("terminal root remained after exact discovery settlement")
		}
		discovery, err := store.ListS3OrphansByDay(created.FirstSeenAt, db.GCDiscoveryBucket(orgID.String(), blockID), 10)
		if err != nil {
			t.Fatalf("list terminal discovery after settlement: %v", err)
		}
		for _, row := range discovery {
			if row.OrgID == orgID && row.BlockID == blockID && g1SameAuthority(row.Authority, authority.Authority()) {
				t.Fatal("terminal discovery projection remained after root settlement")
			}
		}
	})

	t.Run("multiple orphan lives keep the writer fence", func(t *testing.T) {
		orgID := uuid.New()
		blockID := g1IntegrationBlockID("writer-fence")
		p1 := testCommittedOrphanAuthorityWithClaimID(blockID, "hot", syntheticCanonicalStorageKeyForTest(orgID.String(), blockID), "g1-fence-p1-"+uuid.NewString())
		p2 := testCommittedOrphanAuthorityWithClaimID(blockID, "cold", "cold/"+blockID, "g1-fence-p2-"+uuid.NewString())
		createdP1 := store.StartBlockDeleteOrphan(orgID, blockID, p1, "", time.Now().UTC())
		createdP2 := store.StartBlockDeleteOrphan(orgID, blockID, p2, "", time.Now().UTC().Add(time.Second))
		if createdP1.Outcome != gcpkg.StartBlockDeleteOrphanCreated || createdP2.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
			t.Fatalf("publish fence lives = (%s, %s), want created/created", createdP1.Outcome, createdP2.Outcome)
		}
		t.Cleanup(func() {
			_ = store.DeleteS3Orphan(orgID, blockID, p1.Authority(), createdP1.FirstSeenAt)
			_ = store.DeleteS3Orphan(orgID, blockID, p2.Authority(), createdP2.FirstSeenAt)
		})
		if fenced, err := database.BlockDeleteFenceActive(orgID.String(), blockID); err != nil || !fenced {
			t.Fatalf("writer fence with two lives = (%v, %v), want true", fenced, err)
		}
		if err := store.DeleteS3Orphan(orgID, blockID, p1.Authority(), createdP1.FirstSeenAt); err != nil {
			t.Fatalf("settle first writer-fence life: %v", err)
		}
		if fenced, err := database.BlockDeleteFenceActive(orgID.String(), blockID); err != nil || !fenced {
			t.Fatalf("writer fence after first life settlement = (%v, %v), want true for P2", fenced, err)
		}
	})

	gate.observed = true
	t.Log("G1_ORPHAN_EXACT_IDENTITY_EVIDENCE exact_pd=1 root_repair=1 root_without_canonical_retained=1 prepared_retained=1 old_root=1 replay=1 terminal_settlement=1 writer_fence=1 no_ttl=1")
}

func g1IntegrationBlockID(label string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("g1-%s-%s", label, uuid.NewString())))
	return hex.EncodeToString(sum[:])
}

func g1RecoveryRootBucket(authority gcpkg.BlockDeleteAuthority) int {
	authority.ClaimedAt = authority.ClaimedAt.UTC().Truncate(time.Millisecond)
	return db.GCDiscoveryBucket(
		"s3-orphan-recovery-root",
		authority.Target.StorageClass,
		authority.Target.StorageKey,
		authority.ClaimID,
		authority.ClaimedAt.Format(time.RFC3339Nano),
	)
}

func g1RecoveryRootExists(t *testing.T, store *gcpkg.CassandraStore, orgID uuid.UUID, blockID string, authority gcpkg.BlockDeleteAuthority) bool {
	t.Helper()
	_, found := g1RecoveryRootInfo(t, store, orgID, blockID, authority)
	return found
}

func g1RecoveryRootInfo(t *testing.T, store *gcpkg.CassandraStore, orgID uuid.UUID, blockID string, authority gcpkg.BlockDeleteAuthority) (gcpkg.S3OrphanRecoveryRootInfo, bool) {
	t.Helper()
	page, err := store.ListS3OrphanRecoveryRoots(g1RecoveryRootBucket(authority), nil, 100)
	if err != nil {
		t.Fatalf("list recovery root: %v", err)
	}
	for _, root := range page.Roots {
		if root.OrgID == orgID && root.BlockID == blockID &&
			root.Authority.Target == authority.Target &&
			root.Authority.ClaimID == authority.ClaimID &&
			root.Authority.ClaimedAt.Equal(authority.ClaimedAt) {
			return root, true
		}
	}
	return gcpkg.S3OrphanRecoveryRootInfo{}, false
}

func g1SameAuthority(left, right gcpkg.BlockDeleteAuthority) bool {
	return left.Target == right.Target &&
		left.ClaimID == right.ClaimID &&
		left.ClaimedAt.UTC().Truncate(time.Millisecond).Equal(right.ClaimedAt.UTC().Truncate(time.Millisecond))
}

func g1DeleteOrphanCanonical(t *testing.T, database *db.DB, orgID uuid.UUID, blockID string, authority gcpkg.BlockDeleteAuthority) {
	t.Helper()
	if err := database.Session().Query(`
		DELETE FROM gc_s3_orphans
		WHERE org_id = ? AND block_id = ? AND storage_class = ? AND storage_key = ?
		  AND gc_claim_id = ? AND gc_claimed_at = ?
	`, orgID.String(), blockID, authority.Target.StorageClass, authority.Target.StorageKey,
		authority.ClaimID, authority.ClaimedAt.UTC().Truncate(time.Millisecond)).Exec(); err != nil {
		t.Fatalf("delete exact canonical orphan: %v", err)
	}
}

func g1DeleteOrphanProjection(t *testing.T, database *db.DB, orgID uuid.UUID, blockID string, authority gcpkg.BlockDeleteAuthority, firstSeenAt time.Time) {
	t.Helper()
	firstSeenAt = firstSeenAt.UTC().Truncate(time.Millisecond)
	if err := database.Session().Query(`
		DELETE FROM gc_s3_orphans_by_day
		WHERE first_seen_day = ? AND bucket = ? AND first_seen_at = ? AND org_id = ? AND block_id = ?
		  AND storage_class = ? AND storage_key = ? AND gc_claim_id = ? AND gc_claimed_at = ?
	`, db.GCProjectionUTCDate(firstSeenAt), db.GCDiscoveryBucket(orgID.String(), blockID), firstSeenAt,
		orgID.String(), blockID, authority.Target.StorageClass, authority.Target.StorageKey,
		authority.ClaimID, authority.ClaimedAt.UTC().Truncate(time.Millisecond)).Exec(); err != nil {
		t.Fatalf("delete exact orphan projection: %v", err)
	}
}
