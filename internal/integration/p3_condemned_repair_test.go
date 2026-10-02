//go:build integration

package integration

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/api/v2"
	"github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	"github.com/google/uuid"
)

const p3RequireEvidenceEnv = "SESAMEFS_REQUIRE_P3_EVIDENCE"

// TestP3CondemnedIncarnationCannotBeRepaired proves the writer-side P3 boundary
// against real Cassandra and MinIO. A writer captures reusable P1, current GC
// claims/commits D1, and the delayed writer can neither PUT P1 nor recreate its row.
func TestP3CondemnedIncarnationCannotBeRepaired(t *testing.T) {
	evidence := p3RequireCondemnedRepairEvidence(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	database := shareProjectionDBForTest(t)
	orgID := uuid.NewString()
	content := []byte(fmt.Sprintf("p3-condemned-repair-%d", time.Now().UnixNano()))
	digest := sha256.Sum256(content)
	blockID := hex.EncodeToString(digest[:])
	storageClass := discoverStorageClass(t)
	blockStore := newVerificationBlockStore(t, orgID)
	p1Key, err := blockStore.MintStorageKey(blockID)
	if err != nil {
		t.Fatalf("mint P1: %v", err)
	}
	p1 := db.BlockPhysicalLocation{StorageClass: storageClass, StorageKey: p1Key}
	if _, err := blockStore.PutObjectAutoDirect(ctx, p1Key, content); err != nil {
		t.Fatalf("PUT P1: %v", err)
	}
	installed := database.InstallBlockMetadata(ctx, orgID, db.PlainBlockRepresentationID, blockID, "", len(content), p1)
	if installed.Outcome != db.InstallBlockMetadataApplied {
		t.Fatalf("install P1: outcome=%v cause=%v", installed.Outcome, installed.Cause)
	}
	const referrer = "fs:p3:captured"
	if err := database.AddBlockReference(orgID, blockID, referrer, uuid.NewString(), 0); err != nil {
		t.Fatalf("add P1 reference: %v", err)
	}
	orphanClaimID := "p3-condemned-" + uuid.NewString()
	orphanClaimedAt := time.Now().UTC().Truncate(time.Millisecond)

	t.Cleanup(func() {
		_ = database.RemoveBlockReference(orgID, blockID, referrer)
		_ = database.Session().Query(`
			DELETE FROM gc_s3_orphans
			WHERE org_id = ? AND block_id = ? AND storage_class = ? AND storage_key = ?
			  AND gc_claim_id = ? AND gc_claimed_at = ?
		`, orgID, blockID, storageClass, p1Key, orphanClaimID, orphanClaimedAt).Exec()
		_ = database.Session().Query(`DELETE FROM blocks WHERE org_id = ? AND block_id = ?`, orgID, blockID).Exec()
		_ = blockStore.DeleteBlockByStorageKey(context.Background(), p1Key)
	})

	probe, err := database.ProbeBlockReuse(orgID, blockID)
	if err != nil || probe.Decision != db.BlockReuseReusable {
		t.Fatalf("capture reusable P1: probe=%+v err=%v", probe, err)
	}
	target := v2.BlockMaterializationTarget{Store: blockStore, StorageClass: probe.StorageClass, StorageKey: probe.StorageKey}

	if err := database.RemoveBlockReference(orgID, blockID, referrer); err != nil {
		t.Fatal(err)
	}
	store := gcpkg.NewCassandraStore(database)
	committed := p3CommitExactDeleteForTest(t, store, uuid.MustParse(orgID), blockID, p1, orphanClaimID, orphanClaimedAt)
	t.Cleanup(func() { cleanupGCBlockFixturesForTest(t, uuid.MustParse(orgID), blockID) })
	if err := blockStore.DeleteBlockByStorageKey(ctx, p1Key); err != nil {
		t.Fatalf("delete condemned P1 bytes: %v", err)
	}

	putCalls := 0
	_, err = v2.PutBlockMaterializationTarget(ctx, database, orgID, blockID, target, content, func(ctx context.Context, store *storage.BlockStore, key string, data []byte) (string, error) {
		putCalls++
		return store.PutObjectAutoDirect(ctx, key, data)
	}, nil)
	if !errors.Is(err, v2.ErrBlockDeleteInProgress) || putCalls != 0 {
		t.Fatalf("delayed P1 repair = err %v, PUTs %d; want fence and zero PUTs", err, putCalls)
	}
	p1Exists, err := blockStore.ObjectExists(ctx, p1Key)
	if err != nil || p1Exists {
		t.Fatalf("P1 existence after blocked repair = %v, %v; want false, nil", p1Exists, err)
	}

	if err := database.RemoveBlockReference(orgID, blockID, referrer); err != nil {
		t.Fatalf("remove P1 reference: %v", err)
	}
	if finalized, err := store.FinalizeBlockDelete(uuid.MustParse(orgID), blockID, committed); err != nil || finalized.Outcome != gcpkg.BlockDeleteFinalized {
		t.Fatalf("retire exact P1: %+v %v", finalized, err)
	}
	if err := database.RepairBlockMetadataIfCurrent(orgID, db.PlainBlockRepresentationID, blockID, "", len(content), p1); !errors.Is(err, db.ErrBlockRepairAuthorityChanged) {
		t.Fatalf("late P1 metadata repair = %v, want authority changed", err)
	}
	var rows int64
	if err := database.Session().Query(`SELECT COUNT(*) FROM blocks WHERE org_id = ? AND block_id = ?`, orgID, blockID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("canonical rows after stale repair = %d, %v; want 0, nil", rows, err)
	}

	p2Key, err := blockStore.MintStorageKey(blockID)
	if err != nil {
		t.Fatalf("mint P2: %v", err)
	}
	if p2Key == p1Key {
		t.Fatalf("P2 reused condemned P1 key %q", p1Key)
	}
	p2Target := v2.BlockMaterializationTarget{Store: blockStore, StorageClass: storageClass, StorageKey: p2Key, FreshInstall: true}
	if _, err := v2.PutBlockMaterializationTarget(ctx, database, orgID, blockID, p2Target, content, func(ctx context.Context, store *storage.BlockStore, key string, data []byte) (string, error) {
		return store.PutObjectAutoDirect(ctx, key, data)
	}, nil); err != nil {
		t.Fatalf("PUT fresh P2: %v", err)
	}
	t.Cleanup(func() { _ = blockStore.DeleteBlockByStorageKey(context.Background(), p2Key) })
	p2 := db.BlockPhysicalLocation{StorageClass: storageClass, StorageKey: p2Key}
	result := database.InstallBlockMetadata(ctx, orgID, db.PlainBlockRepresentationID, blockID, "", len(content), p2)
	if result.Outcome != db.InstallBlockMetadataApplied {
		t.Fatalf("install P2: outcome=%v cause=%v", result.Outcome, result.Cause)
	}

	t.Logf("P3_CONDEMNED_REPAIR_EVIDENCE p1=%q p2=%q stale_puts=%d stale_rows=%d", p1Key, p2Key, putCalls, rows)
	evidence.record(true)
}

// TestP3ResidualRaceDoesNotRecreateCanonicalRow covers the other side of R10:
// a writer that already passed authority may perform one last PUT, but a later
// repair must never recreate the condemned logical row after GC removes it.
func TestP3ResidualRaceDoesNotRecreateCanonicalRow(t *testing.T) {
	evidence := p3RequireCondemnedRepairEvidence(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	database := shareProjectionDBForTest(t)
	orgID := uuid.NewString()
	content := []byte(fmt.Sprintf("p3-residual-race-%d", time.Now().UnixNano()))
	digest := sha256.Sum256(content)
	blockID := hex.EncodeToString(digest[:])
	sha1Digest := sha1.Sum(content)
	sha1ID := hex.EncodeToString(sha1Digest[:])
	storageClass := discoverStorageClass(t)
	blockStore := newVerificationBlockStore(t, orgID)
	storageKey, err := blockStore.MintStorageKey(blockID)
	if err != nil {
		t.Fatalf("mint P1: %v", err)
	}
	p1 := db.BlockPhysicalLocation{StorageClass: storageClass, StorageKey: storageKey}
	if _, err := blockStore.PutObjectAutoDirect(ctx, storageKey, content); err != nil {
		t.Fatalf("PUT P1: %v", err)
	}
	installed := database.InstallBlockMetadata(ctx, orgID, db.PlainBlockRepresentationID, blockID, "", len(content), p1)
	if installed.Outcome != db.InstallBlockMetadataApplied {
		t.Fatalf("install P1: outcome=%v cause=%v", installed.Outcome, installed.Cause)
	}
	orphanClaimID := "p3-residual-" + uuid.NewString()
	orphanClaimedAt := time.Now().UTC().Truncate(time.Millisecond)
	t.Cleanup(func() {
		_ = database.Session().Query(`
			DELETE FROM gc_s3_orphans
			WHERE org_id = ? AND block_id = ? AND storage_class = ? AND storage_key = ?
			  AND gc_claim_id = ? AND gc_claimed_at = ?
		`, orgID, blockID, storageClass, storageKey, orphanClaimID, orphanClaimedAt).Exec()
		_ = database.Session().Query(`DELETE FROM blocks WHERE org_id = ? AND block_id = ?`, orgID, blockID).Exec()
		_ = blockStore.DeleteBlockByStorageKey(context.Background(), storageKey)
	})

	outcome, err := database.ValidateBlockRepairAuthority(orgID, blockID, p1)
	if err != nil || outcome != db.BlockRepairAuthorityAuthorized {
		t.Fatalf("authority before residual race = %v, %v; want authorized", outcome, err)
	}
	putCalls := 0
	if _, err := blockStore.PutObjectAutoDirect(ctx, storageKey, content); err != nil {
		t.Fatalf("last PUT after authority check: %v", err)
	} else {
		putCalls++
	}
	store := gcpkg.NewCassandraStore(database)
	committed := p3CommitExactDeleteForTest(t, store, uuid.MustParse(orgID), blockID, p1, orphanClaimID, orphanClaimedAt)
	t.Cleanup(func() { cleanupGCBlockFixturesForTest(t, uuid.MustParse(orgID), blockID) })
	if finalized, err := store.FinalizeBlockDelete(uuid.MustParse(orgID), blockID, committed); err != nil || finalized.Outcome != gcpkg.BlockDeleteFinalized {
		t.Fatalf("retire exact P1 in residual race: %+v %v", finalized, err)
	}

	repairErr := database.RepairBlockMetadataIfCurrent(orgID, db.PlainBlockRepresentationID, blockID, sha1ID, len(content), p1)
	if !errors.Is(repairErr, db.ErrBlockRepairBlocked) && !errors.Is(repairErr, db.ErrBlockRepairAuthorityChanged) {
		t.Fatalf("residual repair = %v; want blocked or changed", repairErr)
	}
	var rows int64
	if err := database.Session().Query(`SELECT COUNT(*) FROM blocks WHERE org_id = ? AND block_id = ?`, orgID, blockID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("rows after residual repair = %d, %v; want 0, nil", rows, err)
	}

	t.Logf("P3_RESIDUAL_RACE_EVIDENCE block=%q last_puts=%d stale_repair=%v rows=%d", blockID, putCalls, repairErr, rows)
	evidence.record(true)
}

type p3EvidenceGate struct{ observed bool }

func (gate *p3EvidenceGate) record(observed bool) { gate.observed = observed }

func p3RequireCondemnedRepairEvidence(t *testing.T) *p3EvidenceGate {
	t.Helper()
	gate := &p3EvidenceGate{}
	if os.Getenv(p3RequireEvidenceEnv) != "1" {
		return gate
	}
	t.Cleanup(func() {
		if t.Skipped() {
			t.Errorf("%s=1 requires real Cassandra+MinIO P3 evidence, but the test skipped", p3RequireEvidenceEnv)
		} else if !t.Failed() && !gate.observed {
			t.Errorf("%s=1 completed without condemned-repair evidence", p3RequireEvidenceEnv)
		}
	})
	return gate
}

// Fixtures use the same PREPARED -> COMMITTED handoff as the productive worker.
func p3CommitExactDeleteForTest(t *testing.T, store *gcpkg.CassandraStore, org uuid.UUID, block string, target db.BlockPhysicalLocation, claim string, at time.Time) gcpkg.CommittedBlockDeleteAuthority {
	t.Helper()
	authority := gcpkg.BlockDeleteAuthority{Target: gcpkg.BlockDeleteTarget{StorageClass: target.StorageClass, StorageKey: target.StorageKey}, ClaimID: claim, ClaimedAt: at}
	if claimed, err := store.ClaimBlockDelete(org, block, authority); err != nil || claimed.Outcome != gcpkg.BlockClaimAcquired {
		t.Fatalf("claim exact P1: %+v %v", claimed, err)
	}
	if live, err := store.BlockPublicationLivenessGlobal(org, block); err != nil || live != db.BlockPublicationZero {
		t.Fatalf("pre-D zero proof: %v %v", live, err)
	}
	if prepared := store.PrepareBlockDeleteOrphan(org, block, authority, "", at); prepared.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
		t.Fatalf("prepare exact D1: %+v", prepared)
	}
	handoff, err := store.CommitBlockDeleteOrphanHandoff(org, block, authority)
	if err != nil || handoff.Outcome != gcpkg.BlockDeleteHandoffCommitted {
		t.Fatalf("commit exact D1: %+v %v", handoff, err)
	}
	if promoted := store.PromoteBlockDeleteOrphan(org, block, handoff.Authority); promoted.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
		t.Fatalf("publish exact D1: %+v", promoted)
	}
	return handoff.Authority
}
