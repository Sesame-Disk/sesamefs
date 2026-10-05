package v2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCopiedPublicationRejectsChangedStagedBlockIdentity(t *testing.T) {
	first, other := strings.Repeat("a", 64), strings.Repeat("b", 64)
	helper := &FSHelper{}
	captured := []commitBlockPlacement{{blockID: first, storageClass: "hot-minio-local", storageKey: "original-P"}}
	for _, ids := range [][]string{nil, {other}, {first, other}} {
		if err := helper.validateCopiedBlockPublication("org", []*pendingPublishedFile{{internalBlockIDs: ids}}, captured); !errors.Is(err, ErrBlockDeleteInProgress) {
			t.Fatalf("staged identity drift must fail before authority lookup: %v", err)
		}
	}
}
func TestCopiedPublicationUsesCapturedPhysicalAuthorityAndFailsClosed(t *testing.T) {
	old := validateBorrowedFSPublicationAuthorityFn
	t.Cleanup(func() { validateBorrowedFSPublicationAuthorityFn = old })
	id := strings.Repeat("c", 64)
	captured := []commitBlockPlacement{{blockID: id, storageClass: "hot-minio-local", storageKey: "original-P"}}
	for _, outcome := range []db.BlockRepairAuthorityOutcome{db.BlockRepairAuthorityAuthorized, db.BlockRepairAuthorityBlocked, db.BlockRepairAuthorityChanged, db.BlockRepairAuthorityUnknown, db.BlockRepairAuthorityPermanent} {
		t.Run(fmt.Sprint(outcome), func(t *testing.T) {
			var calls atomic.Int32
			validateBorrowedFSPublicationAuthorityFn = func(_ *db.DB, org, block string, expected db.BlockPhysicalLocation) (db.BlockRepairAuthorityOutcome, error) {
				calls.Add(1)
				if org != "org" || block != id || expected.StorageClass != "hot-minio-local" || expected.StorageKey != "original-P" {
					t.Errorf("lost original exact P: %s %s %+v", org, block, expected)
				}
				return outcome, nil
			}
			err := (&FSHelper{}).validateCopiedBlockPublication("org", []*pendingPublishedFile{{internalBlockIDs: []string{id, id}}}, captured)
			if calls.Load() != 1 || ((outcome == db.BlockRepairAuthorityAuthorized) != (err == nil)) {
				t.Fatalf("outcome=%v calls=%d err=%v", outcome, calls.Load(), err)
			}
		})
	}
}
func TestBatchProgressSnapshotsConcurrentTaskUpdates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	task := &AsyncTask{ID: "task", Status: "processing", Total: 1}
	h := &BatchOperationHandler{tasks: &TaskStore{tasks: map[string]*AsyncTask{"task": task}}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			h.tasks.mu.Lock()
			task.Done = i % 2
			task.Status = "processing"
			if task.Done == 1 {
				task.Status = "done"
			}
			h.tasks.mu.Unlock()
		}
	}()
	for i := 0; i < 1000; i++ {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("GET", "/progress?task_id=task", nil)
		h.GetTaskProgress(c)
		var p map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		if (p["done"] == true) != (p["successful"] == float64(1)) {
			t.Fatalf("inconsistent task snapshot: %v", p)
		}
	}
	<-done
}

// Hold the first worker wave of a maximum-size manifest. A semaphore inside
// already-created goroutines would leave roughly 131k blocked goroutines here.
func TestPublicationChecksBoundMaximumManifestGoroutines(t *testing.T) {
	const count = 131072
	for _, finalGate := range []bool{false, true} {
		t.Run(fmt.Sprintf("final-gate=%v", finalGate), func(t *testing.T) {
			release := make(chan struct{})
			started := make(chan struct{}, blockVerifyConcurrency)
			var once sync.Once
			var calls atomic.Int32
			check := func() {
				if calls.Add(1) <= blockVerifyConcurrency {
					started <- struct{}{}
				}
				<-release
			}
			old := validateBorrowedFSPublicationAuthorityFn
			defer func() { validateBorrowedFSPublicationAuthorityFn = old }()
			validateBorrowedFSPublicationAuthorityFn = func(_ *db.DB, _, _ string, _ db.BlockPhysicalLocation) (db.BlockRepairAuthorityOutcome, error) {
				check()
				return db.BlockRepairAuthorityAuthorized, nil
			}
			baseline := runtime.NumGoroutine()
			done := make(chan error, 1)
			completed := make(chan struct{})
			go func() {
				defer close(completed)
				if finalGate {
					done <- (&FileHandler{}).validateCommitBlockPublicationFences("org", make([]commitBlockPlacement, count))
				} else {
					done <- runBoundedPublicationChecks(count, func(_ context.Context, _ int) error { check(); return nil })
				}
			}()
			defer func() { once.Do(func() { close(release) }); <-completed }()
			timeout := time.NewTimer(5 * time.Second)
			defer timeout.Stop()
			for i := 0; i < blockVerifyConcurrency; i++ {
				select {
				case <-started:
				case <-timeout.C:
					t.Fatal("worker wave did not start")
				}
			}
			// Allow a broken fan-out loop to continue creating its queued workers.
			time.Sleep(50 * time.Millisecond)
			if delta := runtime.NumGoroutine() - baseline; delta > blockVerifyConcurrency+32 {
				t.Errorf("goroutine fan-out = %d; expected bounded worker wave", delta)
			}
			if calls.Load() != blockVerifyConcurrency {
				t.Errorf("active reads = %d", calls.Load())
			}
			once.Do(func() { close(release) })
			err := <-done
			if err != nil || calls.Load() != count {
				t.Fatalf("err=%v checked=%d want=%d", err, calls.Load(), count)
			}
		})
	}
}

func TestPublicationChecksStopAfterAuthorityFailure(t *testing.T) {
	sentinel := errors.New("authority unavailable")
	var calls atomic.Int32
	err := runBoundedPublicationChecks(131072, func(_ context.Context, _ int) error {
		calls.Add(1)
		return sentinel
	})
	if !errors.Is(err, sentinel) || calls.Load() > blockVerifyConcurrency {
		t.Fatalf("fail-closed bounded scheduling: calls=%d err=%v", calls.Load(), err)
	}
}
