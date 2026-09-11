package gc

import (
	"context"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/google/uuid"
)

// TestPC0Characterization_Phase5CascadeRemovesFSObjectsSharedWithHEAD is the
// executable counterexample recorded by PC-0 for
// ISSUE-GC-PHASE5-CASCADE-SHARED-FSOBJECTS-01 and for option 1 of
// ISSUE-PC0-INHERITED-DEPENDENCY-CONTINUITY-01 ("ordinary GC reachability
// already protects inherited dependencies"): it does not.
//
// scanExpiredVersions (Phase 5) enqueues any commit outside the HEAD parent
// chain that is older than version_ttl_days as ItemCommit. processCommit then
// enqueues the commit's root fs_object and processFSObject cascades through
// the whole tree, removing block references and deleting fs_object rows
// without asking whether the same content-addressed fs_id is still reachable
// from the live HEAD. Dangling commits that share almost their entire tree
// with HEAD exist in normal operation (v2 CAS losers keep their commits row,
// Sync PutCommit rows whose client never promoted them, Sync auto-merge
// targets).
//
// This test FREEZES THE OBSERVED, UNSAFE behavior so the counterexample is
// reproducible and cannot drift silently. It is not an endorsement. When
// Phase 5 becomes sharing-aware (PRE-GC follow-up), invert the final
// assertion into a regression test and close the issue. GC_ENABLED=false in
// production keeps this dormant today.
func TestPC0Characterization_Phase5CascadeRemovesFSObjectsSharedWithHEAD(t *testing.T) {
	store := NewMockStore()
	stats := &Stats{}
	q := NewQueue(store)
	w := NewWorker(store, nil, q, 100, 0, false, stats)

	orgID := uuid.New()
	libID := uuid.New()

	// Live HEAD commit H:      root-H -> dir-D -> file-F (blockB)
	// Dangling commit X (old): root-X -> dir-D (same content-addressed fs_id) -> file-F
	store.AddLibraryWithTTL(orgID, libID, "standard", "commit-H", 30)
	store.AddCommitWithDetails(libID, "commit-H", "root-H", "", time.Now())
	store.AddCommitWithDetails(libID, "commit-X", "root-X", "", time.Now().Add(-90*24*time.Hour))
	store.AddFSObjectWithEntries(libID, "root-H", "dir", nil, []string{"dir-D"})
	store.AddFSObjectWithEntries(libID, "root-X", "dir", nil, []string{"dir-D"})
	store.AddFSObjectWithEntries(libID, "dir-D", "dir", nil, []string{"file-F"})
	store.AddFSObject(libID, "file-F", "file", []string{"blockB"})
	store.AddBlock(orgID, "blockB", "standard", 1)
	store.AddBlockReferenceForTest(orgID, "blockB", db.BlockReferrerForFSObject(libID.String(), "file-F"))

	// Exactly the queue item scanExpiredVersions produces for commit-X.
	if err := store.EnqueueBatch([]QueueItem{{
		OrgID:                 orgID,
		QueuedAt:              time.Now().Add(-2 * time.Hour),
		IdentityAt:            time.Now().Add(-2 * time.Hour),
		ItemType:              ItemCommit,
		ItemID:                "commit-X",
		LibraryID:             libID,
		BlockRepresentationID: db.PlainBlockRepresentationID,
	}}); err != nil {
		t.Fatalf("seed expired-version commit: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 20; i++ {
		n, err := w.ProcessOnce(ctx)
		if err != nil {
			t.Fatalf("ProcessOnce: %v", err)
		}
		if n == 0 {
			break
		}
	}

	if store.GetCommitRecord(libID, "commit-H") == nil {
		t.Fatal("HEAD commit row itself must survive; Phase 5 only targets non-HEAD-chain commits")
	}
	_, errD := store.GetFSObject(libID, "dir-D")
	_, errF := store.GetFSObject(libID, "file-F")
	t.Logf("OBSERVED after Phase-5 cascade of dangling commit-X: dir-D err=%v, file-F err=%v, blockB refs=%d (HEAD=commit-H still references dir-D/file-F)",
		errD, errF, store.BlockReferenceCount(orgID, "blockB"))

	// OBSERVED (the bug): both shared nodes are gone and HEAD's tree is broken.
	if errD == nil || errF == nil {
		t.Fatalf("PC0 CHARACTERIZATION CHANGED: Phase-5 cascade no longer removes fs_objects shared with HEAD (dir-D err=%v, file-F err=%v). If Phase 5 is now sharing-aware, invert this assertion into a regression test and close ISSUE-GC-PHASE5-CASCADE-SHARED-FSOBJECTS-01", errD, errF)
	}
}
