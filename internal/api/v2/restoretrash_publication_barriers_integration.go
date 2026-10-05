//go:build integration

package v2

import "sync"

var restoreTrashBarrierMu sync.Mutex
var restoreTrashHooks struct {
	repoID     string
	historical func(string)
	head       func()
}

// SetRestoreTrashPublicationBarriersForTest schedules the actual handler. It does
// not substitute metadata, block placement, liveness or HEAD query outcomes.
func SetRestoreTrashPublicationBarriersForTest(repoID string, historical func(string), head func()) func() {
	restoreTrashBarrierMu.Lock()
	previous := restoreTrashHooks
	restoreTrashHooks.repoID, restoreTrashHooks.historical, restoreTrashHooks.head = repoID, historical, head
	restoreTrashBarrierMu.Unlock()
	return func() {
		restoreTrashBarrierMu.Lock()
		restoreTrashHooks = previous
		restoreTrashBarrierMu.Unlock()
	}
}
func restoreTrashAfterHistoricalEntryBarrier(repoID, fsID string) {
	restoreTrashBarrierMu.Lock()
	hook := restoreTrashHooks
	restoreTrashBarrierMu.Unlock()
	if hook.repoID == repoID && hook.historical != nil {
		hook.historical(fsID)
	}
}
func restoreTrashBeforeHeadBarrier(repoID string) {
	restoreTrashBarrierMu.Lock()
	hook := restoreTrashHooks
	restoreTrashBarrierMu.Unlock()
	if hook.repoID == repoID && hook.head != nil {
		hook.head()
	}
}
