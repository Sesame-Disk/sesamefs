package v2

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
)

// maxManifestBlocks is the supported manifest ceiling recorded in
// ISSUE-FILEFROMBLOCKS-WORKER-FANOUT-01.
const maxManifestBlocks = 131072

// waitForWorkerWave blocks until the admission-bounded first wave is active.
func waitForWorkerWave(t *testing.T, started <-chan struct{}) {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for range blockVerifyConcurrency {
		select {
		case <-started:
		case <-timeout.C:
			t.Fatal("worker wave did not start")
		}
	}
}

// A semaphore acquired inside already-created goroutines bounds active work
// but leaves roughly 131k blocked goroutines for a maximum-size manifest.
func TestVerifyManifestBlocksBoundsGoroutineFanout(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, blockVerifyConcurrency)
	var once sync.Once
	var calls atomic.Int32

	orig := classifyBlockForCommitFn
	t.Cleanup(func() { classifyBlockForCommitFn = orig })
	classifyBlockForCommitFn = func(*FileHandler, string, string, string, int64, bool) (int, string, blockCommitLivenessProvenance, db.BlockPhysicalLocation, error) {
		if calls.Add(1) <= blockVerifyConcurrency {
			started <- struct{}{}
		}
		<-release
		return blockStatusNeedsUpload, "", blockCommitLivenessNone, db.BlockPhysicalLocation{}, nil
	}

	hashes := make([]string, maxManifestBlocks)
	sizeByHash := make(map[string]int64, maxManifestBlocks)
	existsMap := make(map[string]bool, maxManifestBlocks)
	for i := range hashes {
		hashes[i] = hex64(i)
		existsMap[hashes[i]] = true
	}

	baseline := runtime.NumGoroutine()
	done := make(chan error, 1)
	completed := make(chan struct{})
	go func() {
		defer close(completed)
		_, _, _, err := (&FileHandler{}).verifyManifestBlocks(context.Background(), "org", "up:session", hashes, sizeByHash, existsMap)
		done <- err
	}()
	defer func() {
		once.Do(func() { close(release) })
		<-completed
	}()

	waitForWorkerWave(t, started)
	// Give a broken fan-out loop time to create its queued workers.
	time.Sleep(50 * time.Millisecond)
	if delta := runtime.NumGoroutine() - baseline; delta > blockVerifyConcurrency+32 {
		t.Errorf("goroutine fan-out = %d; expected a bounded worker wave", delta)
	}
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatalf("verifyManifestBlocks: %v", err)
	}
	if calls.Load() != maxManifestBlocks {
		t.Fatalf("classified %d blocks, want %d", calls.Load(), maxManifestBlocks)
	}
}

func TestEnsureCommitBlockOwnLivenessBoundsGoroutineFanout(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, blockVerifyConcurrency)
	var once sync.Once
	var calls atomic.Int32

	orig := registerUploadedBlockAddProvisionalRefFn
	t.Cleanup(func() { registerUploadedBlockAddProvisionalRefFn = orig })
	registerUploadedBlockAddProvisionalRefFn = func(*FSHelper, string, string, string, string, string, time.Time) error {
		if calls.Add(1) <= blockVerifyConcurrency {
			started <- struct{}{}
		}
		<-release
		return nil
	}

	blocks := make([]commitBlockPlacement, maxManifestBlocks)
	for i := range blocks {
		blocks[i] = commitBlockPlacement{blockID: hex64(i), storageClass: "hot", storageKey: "key"}
	}

	baseline := runtime.NumGoroutine()
	done := make(chan error, 1)
	completed := make(chan struct{})
	go func() {
		defer close(completed)
		done <- (&FileHandler{}).ensureCommitBlockOwnLiveness(context.Background(), "org", "repo", "session", blocks)
	}()
	defer func() {
		once.Do(func() { close(release) })
		<-completed
	}()

	waitForWorkerWave(t, started)
	time.Sleep(50 * time.Millisecond)
	if delta := runtime.NumGoroutine() - baseline; delta > blockVerifyConcurrency+32 {
		t.Errorf("goroutine fan-out = %d; expected a bounded worker wave", delta)
	}
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatalf("ensureCommitBlockOwnLiveness: %v", err)
	}
	if calls.Load() != maxManifestBlocks {
		t.Fatalf("wrote own liveness for %d blocks, want %d", calls.Load(), maxManifestBlocks)
	}
}

func TestEnsureCommitBlockOwnLivenessCanceledContextSpawnsNoWorkers(t *testing.T) {
	var calls atomic.Int32
	orig := registerUploadedBlockAddProvisionalRefFn
	t.Cleanup(func() { registerUploadedBlockAddProvisionalRefFn = orig })
	registerUploadedBlockAddProvisionalRefFn = func(*FSHelper, string, string, string, string, string, time.Time) error {
		calls.Add(1)
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	blocks := []commitBlockPlacement{{blockID: "b1", storageClass: "hot", storageKey: "k1"}}
	err := (&FileHandler{}).ensureCommitBlockOwnLiveness(ctx, "org", "repo", "session", blocks)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("canceled request performed %d own-liveness writes", calls.Load())
	}
}

func TestVerifyManifestBlocksPropagatesClassificationError(t *testing.T) {
	sentinel := errors.New("classification unavailable")
	orig := classifyBlockForCommitFn
	t.Cleanup(func() { classifyBlockForCommitFn = orig })
	classifyBlockForCommitFn = func(*FileHandler, string, string, string, int64, bool) (int, string, blockCommitLivenessProvenance, db.BlockPhysicalLocation, error) {
		return 0, "", blockCommitLivenessNone, db.BlockPhysicalLocation{}, sentinel
	}

	_, _, _, err := (&FileHandler{}).verifyManifestBlocks(context.Background(), "org", "up:session", []string{hex64(1)}, nil, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the classification error propagated verbatim", err)
	}
}

func TestPublicationChecksCancellationWhileSaturated(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, blockVerifyConcurrency)
	release := make(chan struct{})
	completed := make(chan struct{})
	var once sync.Once
	var calls atomic.Int32
	baseline := runtime.NumGoroutine()
	done := make(chan error, 1)
	go func() {
		defer close(completed)
		done <- runBoundedPublicationChecksWithContext(ctx, maxManifestBlocks, func(context.Context, int) error {
			if calls.Add(1) <= blockVerifyConcurrency {
				started <- struct{}{}
			}
			<-release
			return nil
		})
	}()
	defer func() {
		cancel()
		once.Do(func() { close(release) })
		<-completed
	}()
	waitForWorkerWave(t, started)
	delta := runtime.NumGoroutine() - baseline
	if delta > blockVerifyConcurrency+32 {
		t.Errorf("goroutine fan-out = %d; expected a bounded worker wave", delta)
	}
	t.Logf("%d queued tasks: active callbacks=%d; extra goroutines=%d", maxManifestBlocks, calls.Load(), delta)
	cancel()
	once.Do(func() { close(release) })
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel error=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled runner did not return")
	}
	if calls.Load() != blockVerifyConcurrency {
		t.Fatalf("queued work ran after cancel: %d calls", calls.Load())
	}
}

func TestVerifyManifestBlocksCanceledContextSpawnsNoWorkers(t *testing.T) {
	old := classifyBlockForCommitFn
	defer func() { classifyBlockForCommitFn = old }()
	classifyBlockForCommitFn = func(*FileHandler, string, string, string, int64, bool) (int, string, blockCommitLivenessProvenance, db.BlockPhysicalLocation, error) {
		t.Error("classification after cancellation")
		return 0, "", blockCommitLivenessNone, db.BlockPhysicalLocation{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, err := (&FileHandler{}).verifyManifestBlocks(ctx, "org", "up:s", []string{hex64(1)}, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if err := runBoundedPublicationChecksWithContext(ctx, 0, func(context.Context, int) error { t.Fatal("empty set ran"); return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureCommitBlockOwnLivenessDedupValidationAndErrors(t *testing.T) {
	old := registerUploadedBlockAddProvisionalRefFn
	defer func() { registerUploadedBlockAddProvisionalRefFn = old }()
	var calls atomic.Int32
	registerUploadedBlockAddProvisionalRefFn = func(*FSHelper, string, string, string, string, string, time.Time) error { calls.Add(1); return nil }
	good := commitBlockPlacement{blockID: hex64(1), storageClass: "hot", storageKey: "key"}
	h := &FileHandler{}
	if err := h.ensureCommitBlockOwnLiveness(nil, "org", "repo", "session", []commitBlockPlacement{good, good}); err != nil || calls.Load() != 1 {
		t.Fatalf("dedup err=%v calls=%d", err, calls.Load())
	}
	calls.Store(0)
	bad := commitBlockPlacement{blockID: hex64(2), storageClass: "hot"}
	if err := h.ensureCommitBlockOwnLiveness(context.Background(), "org", "repo", "session", []commitBlockPlacement{good, bad}); !errors.Is(err, db.ErrBlockMetadataPermanent) || calls.Load() != 0 {
		t.Fatalf("invalid placement: err=%v calls=%d", err, calls.Load())
	}
	sentinel := errors.New("storage unavailable")
	registerUploadedBlockAddProvisionalRefFn = func(*FSHelper, string, string, string, string, string, time.Time) error { return sentinel }
	if err := h.ensureCommitBlockOwnLiveness(context.Background(), "org", "repo", "session", []commitBlockPlacement{good}); !errors.Is(err, sentinel) || !errors.Is(err, ErrBlockMaterializationTransient) {
		t.Fatalf("transient wrapping: %v", err)
	}
	registerUploadedBlockAddProvisionalRefFn = func(*FSHelper, string, string, string, string, string, time.Time) error {
		return db.ErrBlockMetadataPermanent
	}
	if err := h.ensureCommitBlockOwnLiveness(context.Background(), "org", "repo", "session", []commitBlockPlacement{good}); !errors.Is(err, db.ErrBlockMetadataPermanent) || errors.Is(err, ErrBlockMaterializationTransient) {
		t.Fatalf("permanent wrapping: %v", err)
	}
}
