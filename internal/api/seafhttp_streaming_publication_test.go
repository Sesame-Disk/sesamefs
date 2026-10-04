package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	v2 "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	"github.com/Sesame-Disk/sesamefs/internal/db"
)

func streamingTestPlacement(digit string) seafHTTPBlockPlacement {
	return seafHTTPBlockPlacement{blockID: strings.Repeat(digit, 64), storageClass: "hot", storageKey: "original-" + digit}
}

func TestStreamingPlacementAccountingRetainsOriginalAndSelectivelyRematerializes(t *testing.T) {
	var upload ChunkUpload
	a, b := streamingTestPlacement("a"), streamingTestPlacement("b")
	calls := 0
	account := func(index int, p seafHTTPBlockPlacement) {
		t.Helper()
		if err := upload.AccountBlockOnce(index, p.blockID, func() (seafHTTPBlockPlacement, error) { calls++; return p, nil }); err != nil {
			t.Fatal(err)
		}
	}
	account(0, a)
	account(1, b)
	account(2, a)
	replacement := a
	replacement.storageKey = "new-canonical-key"
	account(0, replacement) // Already accounted: do not recapture canonical.
	snapshot, err := upload.AccountedBlockPlacements(3)
	if err != nil || snapshot[0] != a || snapshot[1] != b || snapshot[2] != a || calls != 3 {
		t.Fatalf("original snapshot=%v calls=%d err=%v", snapshot, calls, err)
	}
	upload.invalidateBlockPlacements([]string{a.blockID})
	for _, i := range []int{0, 2} {
		if ok, err := upload.BlockAlreadyAccounted(i, a.blockID); err != nil || ok {
			t.Fatalf("victim position %d survived: %v %v", i, ok, err)
		}
	}
	if ok, err := upload.BlockAlreadyAccounted(1, b.blockID); err != nil || !ok {
		t.Fatalf("healthy position lost: %v %v", ok, err)
	}
	account(0, replacement)
	account(2, replacement)
	next, err := upload.AccountedBlockPlacements(3)
	if err != nil || next[0] != replacement || next[1] != b || next[2] != replacement || calls != 5 {
		t.Fatalf("new snapshot=%v calls=%d err=%v", next, calls, err)
	}
	if snapshot[0] != a {
		t.Fatal("snapshot was mutated by retry")
	}
}
func TestStreamingPlacementIncompleteStateFailsClosed(t *testing.T) {
	a := streamingTestPlacement("a")
	upload := ChunkUpload{accountedBlockPosition: map[int]string{0: a.blockID}}
	if _, err := upload.BlockAlreadyAccounted(0, a.blockID); err == nil {
		t.Fatal("missing placement reused")
	}
	if _, err := upload.AccountedBlockPlacements(1); err == nil {
		t.Fatal("missing snapshot accepted")
	}
	if err := upload.AccountBlockOnce(0, a.blockID, func() (seafHTTPBlockPlacement, error) { t.Fatal("must not recapture missing P"); return a, nil }); err == nil {
		t.Fatal("missing accounted P accepted")
	}
	var fresh ChunkUpload
	if err := fresh.AccountBlockOnce(0, a.blockID, func() (seafHTTPBlockPlacement, error) { return a, errors.New("registration failed") }); err == nil {
		t.Fatal("failed registration accepted")
	}
	if len(fresh.accountedBlockPosition) != 0 || len(fresh.accountedBlockPlacement) != 0 {
		t.Fatal("failed registration retained state")
	}
	malformed := a
	malformed.storageKey = ""
	if err := fresh.AccountBlockOnce(0, a.blockID, func() (seafHTTPBlockPlacement, error) { return malformed, nil }); err == nil {
		t.Fatal("malformed P accepted")
	}
}
func TestStreamingPublicationDuplicatePlacements(t *testing.T) {
	original := validateSeafHTTPStreamingAuthorityFn
	t.Cleanup(func() { validateSeafHTTPStreamingAuthorityFn = original })
	var reads atomic.Int32
	validateSeafHTTPStreamingAuthorityFn = func(_ *db.DB, _ string, p seafHTTPBlockPlacement) (db.BlockRepairAuthorityOutcome, error) {
		reads.Add(1)
		return db.BlockRepairAuthorityAuthorized, nil
	}
	a := streamingTestPlacement("a")
	if err := validateSeafHTTPStreamingPublicationPlacements(context.Background(), nil, "org", []seafHTTPBlockPlacement{a, a, a}); err != nil || reads.Load() != 1 {
		t.Fatalf("duplicate reads=%d err=%v", reads.Load(), err)
	}
	changed := a
	changed.storageKey = "different-P"
	if err := validateSeafHTTPStreamingPublicationPlacements(context.Background(), nil, "org", []seafHTTPBlockPlacement{a, changed}); err == nil || reads.Load() != 1 {
		t.Fatalf("contradictory duplicates reached authority: %v", err)
	}
	if err := validateSeafHTTPStreamingPublicationPlacements(context.Background(), nil, "org", nil); err == nil {
		t.Fatal("empty placements accepted")
	}
}
func TestStreamingPublicationAuthorityFailsClosedAndIdentifiesOnlyRejectedLives(t *testing.T) {
	original := validateSeafHTTPStreamingAuthorityFn
	t.Cleanup(func() { validateSeafHTTPStreamingAuthorityFn = original })
	a, b := streamingTestPlacement("a"), streamingTestPlacement("b")
	for _, tc := range []struct {
		name    string
		outcome db.BlockRepairAuthorityOutcome
		err     error
		retry   bool
	}{
		{"authorized", db.BlockRepairAuthorityAuthorized, nil, false},
		{"authorized-error", db.BlockRepairAuthorityAuthorized, errors.New("read error"), false},
		{"unknown", db.BlockRepairAuthorityUnknown, nil, false},
		{"unknown-error", db.BlockRepairAuthorityUnknown, errors.New("timeout"), false},
		{"permanent", db.BlockRepairAuthorityPermanent, errors.New("malformed row"), false},
		{"blocked", db.BlockRepairAuthorityBlocked, nil, true},
		{"changed", db.BlockRepairAuthorityChanged, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reads atomic.Int32
			validateSeafHTTPStreamingAuthorityFn = func(_ *db.DB, _ string, p seafHTTPBlockPlacement) (db.BlockRepairAuthorityOutcome, error) {
				reads.Add(1)
				if p.blockID == a.blockID {
					return tc.outcome, tc.err
				}
				return db.BlockRepairAuthorityAuthorized, nil
			}
			err := validateSeafHTTPStreamingPublicationPlacements(context.Background(), nil, "org", []seafHTTPBlockPlacement{a, b, a})
			if reads.Load() != 2 {
				t.Fatalf("not all distinct placements checked: %d", reads.Load())
			}
			if tc.name == "authorized" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || errors.Is(err, v2.ErrBlockDeleteInProgress) != tc.retry {
				t.Fatalf("authority result=%v", err)
			}
			var failure *seafHTTPPublicationPlacementError
			if !errors.As(err, &failure) {
				t.Fatalf("missing typed rejection: %v", err)
			}
			if tc.retry {
				if len(failure.rejectedBlockIDs) != 1 || failure.rejectedBlockIDs[0] != a.blockID {
					t.Fatalf("rejected IDs=%v", failure.rejectedBlockIDs)
				}
			} else if len(failure.rejectedBlockIDs) != 0 {
				t.Fatal("unknown/permanent authorized rematerialization")
			}
		})
	}
}
func TestStreamingPublicationAuthorityConcurrencyIsBounded(t *testing.T) {
	original := validateSeafHTTPStreamingAuthorityFn
	t.Cleanup(func() { validateSeafHTTPStreamingAuthorityFn = original })
	entered := make(chan struct{}, 32)
	release := make(chan struct{})
	var active, maximum atomic.Int32
	validateSeafHTTPStreamingAuthorityFn = func(_ *db.DB, _ string, _ seafHTTPBlockPlacement) (db.BlockRepairAuthorityOutcome, error) {
		n := active.Add(1)
		for {
			old := maximum.Load()
			if n <= old || maximum.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
		return db.BlockRepairAuthorityAuthorized, nil
	}
	var placements []seafHTTPBlockPlacement
	for i := 0; i < 19; i++ {
		placements = append(placements, seafHTTPBlockPlacement{blockID: fmt.Sprintf("%064x", i), storageClass: "hot", storageKey: fmt.Sprint(i)})
	}
	done := make(chan error, 1)
	go func() {
		done <- validateSeafHTTPStreamingPublicationPlacements(context.Background(), nil, "org", placements)
	}()
	for i := 0; i < finalizeUploadConcurrency; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("bounded workers did not start")
		}
	}
	select {
	case <-entered:
		close(release)
		t.Fatal("authority fan-out exceeded bound")
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("validator did not drain")
	}
	if maximum.Load() > finalizeUploadConcurrency {
		t.Fatalf("max concurrency=%d", maximum.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := validateSeafHTTPStreamingPublicationPlacements(ctx, nil, "org", placements); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context accepted: %v", err)
	}
}
