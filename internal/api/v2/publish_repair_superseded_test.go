package v2

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Sesame-Disk/sesamefs/internal/db"
)

// E1-15E superseded witness and settlement.

func supersededParents(extra map[string]string) map[string]string {
	// HEAD h2 -> h1 -> p -> root; the abandoned target c1 has parent p.
	parents := map[string]string{"h2": "h1", "h1": "p", "p": "root", "root": "", "c1": "p"}
	for k, v := range extra {
		parents[k] = v
	}
	return parents
}

func supersededLookup(parents map[string]string) func(context.Context, string) (string, error) {
	return func(_ context.Context, commit string) (string, error) {
		parent, ok := parents[commit]
		if !ok {
			return "", fmt.Errorf("missing commit %s", commit)
		}
		return parent, nil
	}
}

func TestSupersededWitnessWalk(t *testing.T) {
	parents := supersededParents(map[string]string{"published": "h2", "self": "self"})
	for _, tt := range []struct {
		name, target, parent, anchor, start string
		want                                publishedBlockReferenceRepairCommitOutcome
		wantErr                             bool
	}{
		{name: "parent before target", target: "c1", parent: "p", anchor: "h2", start: "h2", want: publishedBlockReferenceRepairCommitSuperseded},
		{name: "resumed cursor at parent", target: "c1", parent: "p", anchor: "h2", start: "p", want: publishedBlockReferenceRepairCommitSuperseded},
		{name: "anchor is the parent", target: "c1", parent: "p", anchor: "p", start: "p", want: publishedBlockReferenceRepairCommitUnknown},
		{name: "target on chain", target: "h1", parent: "p", anchor: "h2", start: "h2", want: publishedBlockReferenceRepairCommitReachable},
		{name: "unknown parent", target: "c1", parent: "", anchor: "h2", start: "h2", want: publishedBlockReferenceRepairCommitUnknown},
		{name: "unknown anchor", target: "c1", parent: "p", anchor: "", start: "h2", want: publishedBlockReferenceRepairCommitUnknown},
		{name: "self parent", target: "self", parent: "self", anchor: "h2", start: "h2", want: publishedBlockReferenceRepairCommitUnknown},
		{name: "parent off chain", target: "c1", parent: "elsewhere", anchor: "h2", start: "h2", want: publishedBlockReferenceRepairCommitUnknown},
		{name: "read error before parent", target: "c1", parent: "p", anchor: "x", start: "x", want: publishedBlockReferenceRepairCommitUnknown, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			walk, err := walkPublishedCommitReachabilityWitnessed(context.Background(), tt.target, tt.start, nil, publishedCommitSupersededWitness{TargetParentCommitID: tt.parent, AnchorHeadCommitID: tt.anchor}, 16, supersededLookup(parents))
			if walk.Outcome != tt.want || (err != nil) != tt.wantErr {
				t.Fatalf("walk = (%v, %v), want %v err=%t", walk.Outcome, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestSupersededWitnessNeverFromUnwitnessedWalks(t *testing.T) {
	parents := supersededParents(nil)
	if walk, _ := walkPublishedCommitReachabilitySeeded(context.Background(), "c1", "h2", nil, 16, supersededLookup(parents)); walk.Outcome != publishedBlockReferenceRepairCommitUnknown {
		t.Fatalf("seeded walk = %v, want UNKNOWN", walk.Outcome)
	}
	if outcome, _ := classifyPublishedCommitReachability(context.Background(), "c1", "h2", 16, supersededLookup(parents)); outcome != publishedBlockReferenceRepairCommitUnknown {
		t.Fatalf("non-resumable classifier = %v, want UNKNOWN", outcome)
	}
}

type supersededSettleCalls struct {
	removes, deletes, renews, promotes int
	removeErr, deleteErr               error
}

func installSupersededSettleHooks(t *testing.T, calls *supersededSettleCalls) {
	t.Helper()
	oldRemove := removePublishedBlockReferenceRepairOwnedLivenessFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	oldPromote := publishedBlockReferenceRepairPromoteFn
	oldRenew := renewPublishedBlockReferenceRepairLivenessFn
	t.Cleanup(func() {
		removePublishedBlockReferenceRepairOwnedLivenessFn = oldRemove
		deletePublishedBlockReferenceRepairFn = oldDelete
		publishedBlockReferenceRepairPromoteFn = oldPromote
		renewPublishedBlockReferenceRepairLivenessFn = oldRenew
	})
	removePublishedBlockReferenceRepairOwnedLivenessFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		calls.removes++
		return calls.removeErr
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		if calls.removes == 0 {
			t.Fatal("row deleted before the repair-owned pub: was removed")
		}
		calls.deletes++
		return calls.deleteErr
	}
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		calls.promotes++
		return nil
	}
	renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		calls.renews++
		return nil
	}
}

func TestSupersededRepairSettlesOwnedPubThenRow(t *testing.T) {
	memory := &publishedRepairProgressMemory{}
	installPublishedRepairResumableHooks(t, memory, "h2", supersededParents(nil))
	calls := &supersededSettleCalls{}
	installSupersededSettleHooks(t, calls)

	if err := repairPublishedBlockReferenceRepair(nil, newTestPublishedBlockReferenceRepair("c1")); err != nil {
		t.Fatalf("superseded visit = %v, want settled", err)
	}
	if calls.removes != 1 || calls.deletes != 1 || calls.renews != 0 || calls.promotes != 0 {
		t.Fatalf("settlement calls = %+v, want remove+delete only", calls)
	}
}

func TestSupersededRepairRetainedWhileHeadIsParent(t *testing.T) {
	memory := &publishedRepairProgressMemory{}
	installPublishedRepairResumableHooks(t, memory, "p", supersededParents(nil))
	calls := &supersededSettleCalls{}
	installSupersededSettleHooks(t, calls)

	err := repairPublishedBlockReferenceRepair(nil, newTestPublishedBlockReferenceRepair("c1"))
	if !errors.Is(err, errPublishedBlockReferenceRepairRetained) {
		t.Fatalf("HEAD = parent visit = %v, want retained UNKNOWN", err)
	}
	if calls.removes != 0 || calls.deletes != 0 || calls.renews != 1 {
		t.Fatalf("HEAD = parent calls = %+v, want one renewal and no cleanup", calls)
	}
}

func TestSupersededRepairRetainedWithoutTargetCommitRow(t *testing.T) {
	parents := supersededParents(nil)
	delete(parents, "c1") // crash between repair queueing and insertCommit
	memory := &publishedRepairProgressMemory{}
	installPublishedRepairResumableHooks(t, memory, "h2", parents)
	calls := &supersededSettleCalls{}
	installSupersededSettleHooks(t, calls)

	err := repairPublishedBlockReferenceRepair(nil, newTestPublishedBlockReferenceRepair("c1"))
	if !errors.Is(err, errPublishedBlockReferenceRepairRetained) || calls.removes != 0 || calls.deletes != 0 {
		t.Fatalf("missing target row = %v calls=%+v, want retained UNKNOWN", err, calls)
	}
}

func TestSupersededRepairAfterCleanGenesisReanchor(t *testing.T) {
	// A prior visit anchored at HEAD = p and walked to genesis; HEAD moved on.
	memory := &publishedRepairProgressMemory{anchor: "p", cursor: "p", exhausted: true}
	installPublishedRepairResumableHooks(t, memory, "h2", supersededParents(nil))
	calls := &supersededSettleCalls{}
	installSupersededSettleHooks(t, calls)

	repair := newTestPublishedBlockReferenceRepair("c1")
	repair.ReachabilityAnchorHeadCommitID, repair.ReachabilityCursorCommitID, repair.ReachabilityAnchorExhausted = "p", "p", true
	if err := repairPublishedBlockReferenceRepair(nil, repair); err != nil {
		t.Fatalf("re-anchored visit = %v, want settled", err)
	}
	if anchor, _ := memory.snapshot(); anchor != "h2" || calls.removes != 1 || calls.deletes != 1 {
		t.Fatalf("re-anchor anchor=%s calls=%+v", anchor, calls)
	}
}

func TestSupersededRepairAcrossChunks(t *testing.T) {
	parents := linearPublishedCommitParents(publishedCommitReachabilityMaxNodes + 4)
	p := fmt.Sprintf("c-%d", publishedCommitReachabilityMaxNodes+2)
	parents["abandoned"] = p
	memory := &publishedRepairProgressMemory{}
	installPublishedRepairResumableHooks(t, memory, "c-0", parents)
	calls := &supersededSettleCalls{}
	installSupersededSettleHooks(t, calls)

	repair := newTestPublishedBlockReferenceRepair("abandoned")
	if err := repairPublishedBlockReferenceRepair(nil, repair); err == nil || !strings.Contains(err.Error(), "limit") || calls.deletes != 0 {
		t.Fatalf("first chunk = %v calls=%+v, want UNKNOWN limit", err, calls)
	}
	if err := repairPublishedBlockReferenceRepair(nil, repair); err != nil || calls.removes != 1 || calls.deletes != 1 {
		t.Fatalf("resumed chunk = %v calls=%+v, want settled", err, calls)
	}
}

func TestSupersededSettlementFailureRetainsAndRetrySettles(t *testing.T) {
	for _, step := range []string{"remove", "delete"} {
		t.Run(step, func(t *testing.T) {
			memory := &publishedRepairProgressMemory{}
			installPublishedRepairResumableHooks(t, memory, "h2", supersededParents(nil))
			calls := &supersededSettleCalls{}
			installSupersededSettleHooks(t, calls)
			injected := errors.New("injected " + step + " failure")
			if step == "remove" {
				calls.removeErr = injected
			} else {
				calls.deleteErr = injected
			}
			repair := newTestPublishedBlockReferenceRepair("c1")
			err := repairPublishedBlockReferenceRepair(nil, repair)
			if !errors.Is(err, injected) || calls.renews != 0 {
				t.Fatalf("failed %s = %v calls=%+v, want error and no renewal", step, err, calls)
			}
			if step == "remove" && calls.deletes != 0 {
				t.Fatal("row deleted although pub: removal failed")
			}
			calls.removeErr, calls.deleteErr = nil, nil
			if err := repairPublishedBlockReferenceRepair(nil, repair); err != nil {
				t.Fatalf("retry after failed %s = %v, want settled", step, err)
			}
		})
	}
}

// The witness depends on every production HEAD writer installing a child of
// the current HEAD under a SERIAL CAS. A new writer must re-audit E1-15E.
func TestSupersededWitnessHeadWriterSetIsPinned(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	// UPDATEs move HEAD; INSERTs (upserts) set it, and must only ever create a
	// library under a freshly generated library_id.
	write := regexp.MustCompile(`(?i)UPDATE\s+libraries\s+SET\s+head_commit_id\s*=|INSERT\s+INTO\s+libraries\s*\([^)]*head_commit_id`)
	comment := regexp.MustCompile(`(?m)^\s*//.*$`)
	var found []string
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		body = comment.ReplaceAll(body, nil)
		for range write.FindAllIndex(body, -1) {
			found = append(found, filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator))))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(found)
	want := []string{
		"internal/api/sync.go",               // updateLibraryHeadWithStats: IF head = current
		"internal/api/v2/admin_libraries.go", // INSERT: new library, fresh newLibID
		"internal/api/v2/fs_helpers.go",      // UpdateLibraryHeadFromSnapshot: IF head = snapshot
		"internal/api/v2/fs_helpers.go",      // InitializeLibraryHeadIfUnset: IF head = null
		"internal/api/v2/libraries.go",       // INSERT: new (encrypted) library, fresh newLibID
		"internal/api/v2/libraries.go",       // INSERT: new library, fresh newLibID
		"internal/db/library_continuity.go",  // AdvanceLibraryCertifiedFrontier: no production caller
	}
	if strings.Join(found, ",") != strings.Join(want, ",") {
		t.Fatalf("production HEAD writers = %v, want %v; re-audit the E1-15E superseded witness", found, want)
	}
}
