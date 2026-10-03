//go:build integration

package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

var g5CoexistenceObserved, g5PaginationObserved, g5GraceObserved bool

// Real Cassandra INSTALL, exact handoff and fresh-worker MinIO continuation.
// Both physical objects coexist until recovery, including P2's live reference.
func TestG5CassandraMinIOOldLifeWithoutDayScheduling(t *testing.T) {
	requireCassandra(t)
	ctx := context.Background()
	database := shareProjectionDBForTest(t)
	store := gcpkg.NewCassandraStore(database)
	class := discoverStorageClass(t)
	org := uuid.New()
	data := []byte(fmt.Sprintf("g5-content-%s", uuid.NewString()))
	block := sha256hex(data)
	bs := newVerificationBlockStore(t, org.String())
	p1key, err := bs.MintStorageKey(block)
	if err != nil {
		t.Fatal(err)
	}
	p2key, err := bs.MintStorageKey(block)
	if err != nil {
		t.Fatal(err)
	}
	p1 := db.BlockPhysicalLocation{StorageClass: class, StorageKey: p1key}
	p2 := db.BlockPhysicalLocation{StorageClass: class, StorageKey: p2key}
	if p1 == p2 {
		t.Fatal("minted lives must differ")
	}
	t.Cleanup(func() {
		cleanupGCBlockFixturesForTest(t, org, block)
		_ = database.Session().Query(`DELETE FROM blocks WHERE org_id = ? AND block_id = ?`, org.String(), block).Exec()
		_ = database.Session().Query(`DELETE FROM block_references WHERE org_id = ? AND block_id = ?`, org.String(), block).Exec()
		_ = bs.DeleteBlockByStorageKey(ctx, p1key)
		_ = bs.DeleteBlockByStorageKey(ctx, p2key)
	})
	if _, err := bs.PutObjectAutoDirect(ctx, p1key, data); err != nil {
		t.Fatal(err)
	}
	if result := database.InstallBlockMetadata(ctx, org.String(), db.PlainBlockRepresentationID, block, "", len(data), p1); result.Outcome != db.InstallBlockMetadataApplied {
		t.Fatalf("INSTALL P1: %+v", result)
	}
	old := time.Now().UTC().AddDate(0, 0, -120).Truncate(time.Millisecond)
	candidate, err := store.EnsureBlockGCCandidateExact(org, block, class, old)
	if err != nil {
		t.Fatal(err)
	}
	if err := enqueueExactBlockCandidateForTest(store, candidate, old); err != nil {
		t.Fatal(err)
	}
	if exists, err := store.PendingItemExists(org, uuid.Nil, gcpkg.ItemBlock, block, candidate.ItemIdentity()); err != nil || !exists {
		t.Fatalf("pending fixture: %v %v", exists, err)
	}
	authority := gcpkg.BlockDeleteAuthority{Target: gcpkg.BlockDeleteTarget{StorageClass: class, StorageKey: p1key}, ClaimID: uuid.NewString(), ClaimedAt: old}
	if claim, err := store.ClaimBlockDelete(org, block, authority); err != nil || claim.Outcome != gcpkg.BlockClaimAcquired {
		t.Fatalf("claim: %+v %v", claim, err)
	}
	if prepared := store.PrepareBlockDeleteOrphan(org, block, authority, "", old); prepared.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
		t.Fatalf("prepare: %+v", prepared)
	}
	handoff, err := store.CommitBlockDeleteOrphanHandoff(org, block, authority)
	if err != nil || handoff.Outcome != gcpkg.BlockDeleteHandoffCommitted {
		t.Fatalf("commit: %+v %v", handoff, err)
	}
	if promoted := store.PromoteBlockDeleteOrphan(org, block, handoff.Authority); promoted.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
		t.Fatalf("promote: %+v", promoted)
	}
	if retired, err := store.FinalizeBlockDelete(org, block, handoff.Authority); err != nil || retired.Outcome != gcpkg.BlockDeleteFinalized {
		t.Fatalf("retire: %+v %v", retired, err)
	}
	if probe, err := database.ProbeBlockReuse(org.String(), block); err != nil || probe.Decision != db.BlockReuseNeedsPut {
		t.Fatalf("rowless writer: %+v %v", probe, err)
	}
	if fenced, err := database.BlockDeleteFenceActive(org.String(), block); err != nil || fenced {
		t.Fatalf("retired fence: %v %v", fenced, err)
	}
	if _, err := bs.PutObjectAutoDirect(ctx, p2key, data); err != nil {
		t.Fatal(err)
	}
	if result := database.InstallBlockMetadata(ctx, org.String(), db.PlainBlockRepresentationID, block, "", len(data), p2); result.Outcome != db.InstallBlockMetadataApplied {
		t.Fatalf("INSTALL P2 with orphan P1: %+v", result)
	}
	if err := database.AddBlockReference(org.String(), block, "up:g4-p2", uuid.Nil.String(), 0); err != nil {
		t.Fatal(err)
	}
	if probe, err := database.ProbeBlockReuse(org.String(), block); err != nil || probe.Decision != db.BlockReuseReusable || probe.StorageKey != p2key {
		t.Fatalf("reuse P2: %+v %v", probe, err)
	}
	if outcome, err := database.ValidateBlockRepairAuthority(org.String(), block, p2); err != nil || outcome != db.BlockRepairAuthorityAuthorized {
		t.Fatalf("P2 authority: %v %v", outcome, err)
	}
	if outcome, _ := database.ValidateBlockRepairAuthority(org.String(), block, p1); outcome != db.BlockRepairAuthorityChanged {
		t.Fatalf("stale P1 authority=%v", outcome)
	}
	if orphan, found, err := store.GetS3OrphanExact(org, block, authority); err != nil || !found || orphan.RecoveryState != gcpkg.S3OrphanRecoveryStateCommitted {
		t.Fatalf("coexistence: %+v %v %v", orphan, found, err)
	}
	// Remove this exact queue/pending lifecycle after the productive handoff.
	if err := store.CompleteItem(org, old, gcpkg.ItemBlock, block, candidate.ItemIdentity()); err != nil {
		t.Fatal(err)
	}
	if exists, err := store.PendingItemExists(org, uuid.Nil, gcpkg.ItemBlock, block, candidate.ItemIdentity()); err != nil || exists {
		t.Fatalf("pending was not lost: %v %v", exists, err)
	}
	if queued, err := store.DequeueBatch(org, 100, time.Now()); err != nil || len(queued) != 0 {
		t.Fatalf("queue was not lost: %+v %v", queued, err)
	}
	// Lose only this fixture's exact secondary discovery identity before restart.
	if err := database.Session().Query(`DELETE FROM gc_s3_orphans_by_day WHERE first_seen_day = ? AND bucket = ? AND first_seen_at = ? AND org_id = ? AND block_id = ? AND storage_class = ? AND storage_key = ? AND gc_claim_id = ? AND gc_claimed_at = ?`, db.GCProjectionUTCDate(old), db.GCDiscoveryBucket(org.String(), block), old, org.String(), block, class, p1key, authority.ClaimID, authority.ClaimedAt).Exec(); err != nil {
		t.Fatal(err)
	}
	manager := storage.NewManager()
	manager.RegisterBackend(class, newVerificationS3Store(t), "")
	restarted := gcpkg.NewWorker(g4IsolatedRecoveryStore{GCStore: gcpkg.NewCassandraStore(database), org: org}, gcpkg.NewStorageManagerAdapter(manager), gcpkg.NewQueue(store), 100, 0, false, &gcpkg.Stats{})
	if n, err := restarted.RecoverS3Orphans(ctx, 100); err != nil || n < 1 {
		t.Fatalf("restart continuation: %d %v", n, err)
	}
	if exists, err := bs.ObjectExists(ctx, p1key); err != nil || exists {
		t.Fatalf("K1 retained: %v %v", exists, err)
	}
	if got, err := bs.GetBlockByStorageKey(ctx, p2key); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("K2 damaged: %v", err)
	}
	if info, err := store.GetBlockInfo(org, block); err != nil || info.StorageKey != p2key {
		t.Fatalf("P2 metadata changed: %+v %v", info, err)
	}
	if has, err := database.BlockHasReferencesGlobal(org.String(), block); err != nil || !has {
		t.Fatal("P2 refs lost")
	}
	if _, found, err := store.GetS3OrphanExact(org, block, authority); err != nil || found {
		t.Fatalf("orphan not settled: %v %v", found, err)
	}
	g5CoexistenceObserved = true
}

// Fixture-scoped mutations use the real store; stats are persisted under this org.
// The global root page remains bounded even when it contains unrelated identities.
type g5IsolatedRecoveryStore struct {
	gcpkg.GCStore
	org    uuid.UUID
	poison map[string]bool
}

func (s g5IsolatedRecoveryStore) ListS3OrphanRecoveryRoots(bucket int, state []byte, limit int) (gcpkg.S3OrphanRecoveryRootPage, error) {
	page, err := s.GCStore.ListS3OrphanRecoveryRoots(bucket, state, limit)
	rows := page.Roots[:0]
	for _, root := range page.Roots {
		if root.OrgID == s.org {
			rows = append(rows, root)
		}
	}
	page.Roots = rows
	return page, err
}
func (s g5IsolatedRecoveryStore) LoadGCStats(key string) (string, error) {
	return s.GCStore.LoadGCStats("test.g5." + s.org.String() + "." + key)
}
func (s g5IsolatedRecoveryStore) SaveGCStats(key, value string) error {
	return s.GCStore.SaveGCStats("test.g5."+s.org.String()+"."+key, value)
}
func (s g5IsolatedRecoveryStore) GetS3OrphanExact(org uuid.UUID, block string, authority gcpkg.BlockDeleteAuthority) (gcpkg.S3OrphanInfo, bool, error) {
	if s.poison[block] {
		return gcpkg.S3OrphanInfo{}, false, errors.New("G5 isolated prefix is unavailable")
	}
	return s.GCStore.GetS3OrphanExact(org, block, authority)
}
func TestG5CassandraMinIOBoundedRecoveryAcrossRestart(t *testing.T) {
	requireCassandra(t)
	ctx := context.Background()
	database := shareProjectionDBForTest(t)
	store := gcpkg.NewCassandraStore(database)
	org := uuid.New()
	// The suite's live backend also runs GC. Give these roots a fixture-only
	// class, backed by real MinIO in this worker, so the background collector
	// cannot consume the retained prefix before the bounded restart assertions.
	class := "g5-pagination-" + org.String()
	bs := newVerificationBlockStore(t, org.String())
	manager := storage.NewManager()
	manager.RegisterBackend(class, newVerificationS3Store(t), "")
	isolated := g5IsolatedRecoveryStore{GCStore: store, org: org, poison: map[string]bool{}}
	now := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	var rows []gcpkg.S3OrphanInfo
	for i := 0; i < 5; i++ {
		data := []byte(fmt.Sprintf("g5-page-%s-%d", org, i))
		block := sha256hex(data)
		key, err := bs.MintStorageKey(block)
		if err != nil {
			t.Fatal(err)
		}
		authority := gcpkg.BlockDeleteAuthority{Target: gcpkg.BlockDeleteTarget{StorageClass: class, StorageKey: key}, ClaimedAt: now.Add(time.Duration(i) * time.Second)}
		for n := 0; n < 10000; n++ {
			authority.ClaimID = fmt.Sprintf("g5-page-%d-%d", i, n)
			if g1RecoveryRootBucket(authority) == 0 {
				break
			}
		}
		if g1RecoveryRootBucket(authority) != 0 {
			t.Fatal("fixture bucket")
		}
		if _, err := bs.PutObjectAutoDirect(ctx, key, data); err != nil {
			t.Fatal(err)
		}
		if result := database.InstallBlockMetadata(ctx, org.String(), db.PlainBlockRepresentationID, block, "", len(data), db.BlockPhysicalLocation{StorageClass: class, StorageKey: key}); result.Outcome != db.InstallBlockMetadataApplied {
			t.Fatal(result)
		}
		if result, err := store.ClaimBlockDelete(org, block, authority); err != nil || result.Outcome != gcpkg.BlockClaimAcquired {
			t.Fatalf("claim: %+v %v", result, err)
		}
		prepared := store.PrepareBlockDeleteOrphan(org, block, authority, "", authority.ClaimedAt)
		if prepared.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
			t.Fatal(prepared)
		}
		handoff, err := store.CommitBlockDeleteOrphanHandoff(org, block, authority)
		if err != nil || handoff.Outcome != gcpkg.BlockDeleteHandoffCommitted {
			t.Fatalf("commit: %+v %v", handoff, err)
		}
		if result := store.PromoteBlockDeleteOrphan(org, block, handoff.Authority); result.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
			t.Fatal(result)
		}
		if result, err := store.FinalizeBlockDelete(org, block, handoff.Authority); err != nil || result.Outcome != gcpkg.BlockDeleteFinalized {
			t.Fatalf("retire: %+v %v", result, err)
		}
		row, found, err := store.GetS3OrphanExact(org, block, authority)
		if err != nil || !found {
			t.Fatalf("canonical: %v %v", found, err)
		}
		rows = append(rows, row)
		g1DeleteOrphanProjection(t, database, org, block, authority, row.FirstSeenAt)
		t.Cleanup(func() { cleanupGCBlockFixturesForTest(t, org, block); _ = bs.DeleteBlockByStorageKey(ctx, key) })
		if i < 3 {
			isolated.poison[block] = true
		}
	}
	t.Cleanup(func() {
		for bucket := 0; bucket < db.GCDiscoveryBucketCount; bucket++ {
			key := fmt.Sprintf("test.g5.%s.gc.scan.s3_roots.bucket.%02d", org, bucket)
			_ = database.Session().Query(`DELETE FROM gc_stats WHERE stat_key = ?`, key).Exec()
		}
	})
	recovered := 0
	ticks := 0
	for ticks < 256 && recovered < 2 {
		// Each restart reloads the persisted exact seek checkpoint from Cassandra.
		worker := gcpkg.NewWorker(isolated, gcpkg.NewStorageManagerAdapter(manager), gcpkg.NewQueue(isolated), 100, 0, false, &gcpkg.Stats{})
		n, err := worker.RecoverS3Orphans(ctx, 2)
		if n > 2 {
			t.Fatalf("bucket bound exceeded: %d", n)
		}
		if err != nil && !strings.Contains(err.Error(), "G5 isolated prefix") {
			t.Fatal(err)
		}
		recovered += n
		ticks++
	}
	if recovered != 2 || ticks < 3 {
		t.Fatalf("prefix pressure/restart convergence: recovered=%d ticks=%d", recovered, ticks)
	}
	for i, row := range rows {
		exists, err := bs.ObjectExists(ctx, row.StorageKey)
		if err != nil || exists != (i < 3) {
			t.Fatalf("exact object %d: exists=%v err=%v", i, exists, err)
		}
	}
	// Retry retained failed roots automatically on the next finite cycle.
	isolated.poison = map[string]bool{}
	for ticks < 512 && recovered < 5 {
		worker := gcpkg.NewWorker(isolated, gcpkg.NewStorageManagerAdapter(manager), gcpkg.NewQueue(isolated), 100, 0, false, &gcpkg.Stats{})
		n, err := worker.RecoverS3Orphans(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		recovered += n
		ticks++
	}
	if recovered != 5 {
		t.Fatalf("failed prefix not revisited: %d", recovered)
	}
	for _, row := range rows {
		if _, found, err := store.GetS3OrphanRecoveryRootExact(org, row.BlockID, row.Authority); err != nil || found {
			t.Fatalf("root not settled: %v %v", found, err)
		}
	}
	g5PaginationObserved = true
}

// Referenced settlement must not donate an expired age to a later zero epoch.
func TestG5CassandraNewZeroEpochUsesFreshGrace(t *testing.T) {
	requireCassandra(t)
	database := shareProjectionDBForTest(t)
	store := gcpkg.NewCassandraStore(database)
	org := uuid.New()
	block := sha256hex([]byte(uuid.NewString()))
	bs := newVerificationBlockStore(t, org.String())
	key, err := bs.MintStorageKey(block)
	if err != nil {
		t.Fatal(err)
	}
	class := discoverStorageClass(t)
	if result := database.InstallBlockMetadata(context.Background(), org.String(), db.PlainBlockRepresentationID, block, "", 1, db.BlockPhysicalLocation{StorageClass: class, StorageKey: key}); result.Outcome != db.InstallBlockMetadataApplied {
		t.Fatal(result)
	}
	t.Cleanup(func() {
		cleanupGCBlockFixturesForTest(t, org, block)
		_ = database.Session().Query(`DELETE FROM blocks WHERE org_id = ? AND block_id = ?`, org.String(), block).Exec()
		_ = database.Session().Query(`DELETE FROM block_references WHERE org_id = ? AND block_id = ?`, org.String(), block).Exec()
	})
	if err := database.AddBlockReference(org.String(), block, "up:g5-grace", uuid.Nil.String(), 0); err != nil {
		t.Fatal(err)
	}
	oldAt := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Millisecond)
	old, err := store.EnsureBlockGCCandidateExact(org, block, class, oldAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := enqueueExactBlockCandidateForTest(store, old, oldAt); err != nil {
		t.Fatal(err)
	}
	worker := gcpkg.NewWorker(store, nil, gcpkg.NewQueue(store), 100, 0, false, &gcpkg.Stats{})
	if n, err := worker.ProcessOrgOnce(context.Background(), org); err != nil || n != 1 {
		t.Fatalf("referenced settlement: %d %v", n, err)
	}
	if _, found, err := store.GetBlockGCCandidateExact(org, block, old.Identity()); err != nil || found {
		t.Fatalf("referenced age retained: %v %v", found, err)
	}
	if err := database.Session().Query(`DELETE FROM block_references WHERE org_id = ? AND block_id = ? AND referrer = ?`, org.String(), block, "up:g5-grace").Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
	zeroAt := time.Now().UTC().Truncate(time.Millisecond)
	fresh, err := store.EnsureBlockGCCandidateExact(org, block, class, zeroAt)
	if err != nil || fresh.Target != old.Target || !fresh.CandidateAt.Equal(zeroAt) {
		t.Fatalf("fresh same-P zero epoch: %+v %v", fresh, err)
	}
	// An old queue timestamp deliberately bypasses queue grace; candidate grace must veto.
	if err := enqueueExactBlockCandidateForTest(store, fresh, oldAt); err != nil {
		t.Fatal(err)
	}
	worker = gcpkg.NewWorker(store, nil, gcpkg.NewQueue(store), 100, time.Hour, false, &gcpkg.Stats{})
	if n, err := worker.ProcessOrgOnce(context.Background(), org); err != nil || n != 0 {
		t.Fatalf("fresh grace: %d %v", n, err)
	}
	var state, claim string
	if err := database.Session().Query(`SELECT gc_state, gc_claim_id FROM blocks WHERE org_id = ? AND block_id = ?`, org.String(), block).Consistency(gocql.EachQuorum).Scan(&state, &claim); err != nil || state != "" || claim != "" {
		t.Fatalf("claim before fresh grace: state=%q claim=%q err=%v", state, claim, err)
	}
	if _, found, err := store.GetBlockGCCandidateExact(org, block, fresh.Identity()); err != nil || !found {
		t.Fatalf("fresh candidate lost: %v %v", found, err)
	}
	g5GraceObserved = true
}
