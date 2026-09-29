//go:build integration

package v2

import (
	"sync"
	"time"
)

type fileFromBlocksPublicationBarriers struct {
	repoID           string
	afterVerified    func()
	afterBorrowedPin func()
	afterStaged      func()
	beforeHead       func() error
}

var (
	fileFromBlocksBarrierMu                    sync.Mutex
	fileFromBlocksPublicationBarriersInstalled *fileFromBlocksPublicationBarriers
)

// SetFileFromBlocksPublicationBarriersForTest installs process-local publication
// barriers for in-process CreateFileFromBlocks characterization. Hooks run only
// when the request repoID matches. The returned restore function must run from
// t.Cleanup. HTTP commits in other processes are unaffected.
func SetFileFromBlocksPublicationBarriersForTest(repoID string, afterVerified, afterBorrowedPin, afterStaged func(), beforeHead func() error) func() {
	fileFromBlocksBarrierMu.Lock()
	previous := fileFromBlocksPublicationBarriersInstalled
	fileFromBlocksPublicationBarriersInstalled = &fileFromBlocksPublicationBarriers{
		repoID:           repoID,
		afterVerified:    afterVerified,
		afterBorrowedPin: afterBorrowedPin,
		afterStaged:      afterStaged,
		beforeHead:       beforeHead,
	}
	fileFromBlocksBarrierMu.Unlock()
	return func() {
		fileFromBlocksBarrierMu.Lock()
		fileFromBlocksPublicationBarriersInstalled = previous
		fileFromBlocksBarrierMu.Unlock()
	}
}

func fileFromBlocksPublicationHooksForRepo(repoID string) *fileFromBlocksPublicationBarriers {
	fileFromBlocksBarrierMu.Lock()
	hooks := fileFromBlocksPublicationBarriersInstalled
	fileFromBlocksBarrierMu.Unlock()
	if hooks == nil || hooks.repoID == "" || hooks.repoID != repoID {
		return nil
	}
	return hooks
}

func fileFromBlocksAfterVerifiedBarrier(repoID string) {
	hooks := fileFromBlocksPublicationHooksForRepo(repoID)
	if hooks == nil || hooks.afterVerified == nil {
		return
	}
	hooks.afterVerified()
}

// SetFileFromBlocksOwnLivenessFailureForTest makes BorrowedFS own-liveness
// writes fail while the returned restore function is installed. It exists only
// to prove that publication cannot proceed when the safety pin is unavailable.
func SetFileFromBlocksOwnLivenessFailureForTest(err error) func() {
	previous := registerUploadedBlockAddProvisionalRefFn
	registerUploadedBlockAddProvisionalRefFn = func(*FSHelper, string, string, string, string, string, time.Time) error {
		return err
	}
	return func() { registerUploadedBlockAddProvisionalRefFn = previous }
}

func fileFromBlocksAfterBorrowedLivenessBarrier(repoID string) {
	hooks := fileFromBlocksPublicationHooksForRepo(repoID)
	if hooks == nil || hooks.afterBorrowedPin == nil {
		return
	}
	hooks.afterBorrowedPin()
}

func fileFromBlocksAfterStagedBarrier(repoID string) {
	hooks := fileFromBlocksPublicationHooksForRepo(repoID)
	if hooks == nil || hooks.afterStaged == nil {
		return
	}
	hooks.afterStaged()
}

// uploadFileAfterMaterializedHook is the UploadFile counterpart of the
// CreateFileFromBlocks barriers: it runs after the block is materialized
// (up: written, metadata installed) and before the shared finalizer stages
// pub:. Guarded by fileFromBlocksBarrierMu like the other hooks.
type uploadFileAfterMaterializedHook struct {
	repoID string
	fn     func()
}

var uploadFileAfterMaterializedInstalled *uploadFileAfterMaterializedHook

// SetUploadFileAfterMaterializedBarrierForTest installs a process-local hook
// for in-process UploadFile characterization. It runs only when the request
// repoID matches. The returned restore function must run from t.Cleanup.
func SetUploadFileAfterMaterializedBarrierForTest(repoID string, afterMaterialized func()) func() {
	fileFromBlocksBarrierMu.Lock()
	previous := uploadFileAfterMaterializedInstalled
	uploadFileAfterMaterializedInstalled = &uploadFileAfterMaterializedHook{repoID: repoID, fn: afterMaterialized}
	fileFromBlocksBarrierMu.Unlock()
	return func() {
		fileFromBlocksBarrierMu.Lock()
		uploadFileAfterMaterializedInstalled = previous
		fileFromBlocksBarrierMu.Unlock()
	}
}

func uploadFileAfterMaterializedBarrier(repoID string) {
	fileFromBlocksBarrierMu.Lock()
	hook := uploadFileAfterMaterializedInstalled
	fileFromBlocksBarrierMu.Unlock()
	if hook == nil || hook.repoID == "" || hook.repoID != repoID || hook.fn == nil {
		return
	}
	hook.fn()
}

func fileFromBlocksBeforeHeadBarrier(repoID string) error {
	hooks := fileFromBlocksPublicationHooksForRepo(repoID)
	if hooks == nil || hooks.beforeHead == nil {
		return nil
	}
	return hooks.beforeHead()
}
