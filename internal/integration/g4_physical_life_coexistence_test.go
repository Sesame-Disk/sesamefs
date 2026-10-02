//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

var g4CoexistenceObserved bool

// Real Cassandra INSTALL, exact handoff and fresh-worker MinIO continuation.
// Both physical objects coexist until recovery, including P2's live reference.
func TestG4CassandraMinIOPhysicalLifeCoexistence(t *testing.T) {
	requireCassandra(t)
	ctx := context.Background()
	database := shareProjectionDBForTest(t)
	store := gcpkg.NewCassandraStore(database)
	class := discoverStorageClass(t)
	org := uuid.New()
	data := []byte(fmt.Sprintf("g4-content-%s", uuid.NewString()))
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
	old := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
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
	g4CoexistenceObserved = true
}

// Restrict enumeration and scheduling statistics to this fixture. Every exact
// lifecycle mutation and storage operation still uses the real production store.
type g4IsolatedRecoveryStore struct {
	gcpkg.GCStore
	org uuid.UUID
}

func (s g4IsolatedRecoveryStore) ListS3OrphanRecoveryRoots(bucket int, state []byte, limit int) (gcpkg.S3OrphanRecoveryRootPage, error) {
	page, err := s.GCStore.ListS3OrphanRecoveryRoots(bucket, state, limit)
	filtered := page.Roots[:0]
	for _, root := range page.Roots {
		if root.OrgID == s.org {
			filtered = append(filtered, root)
		}
	}
	page.Roots = filtered
	return page, err
}
func (s g4IsolatedRecoveryStore) ListS3OrphansByDay(day time.Time, bucket, limit int) ([]gcpkg.S3OrphanDiscoveryInfo, error) {
	rows, err := s.GCStore.ListS3OrphansByDay(day, bucket, limit)
	filtered := rows[:0]
	for _, row := range rows {
		if row.OrgID == s.org {
			filtered = append(filtered, row)
		}
	}
	return filtered, err
}
func (s g4IsolatedRecoveryStore) LoadGCStats(string) (string, error) { return "", gocql.ErrNotFound }
func (s g4IsolatedRecoveryStore) SaveGCStats(string, string) error   { return nil }
