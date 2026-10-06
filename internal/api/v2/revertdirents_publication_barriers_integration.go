//go:build integration

package v2

import "sync"

var revertDirentsBarrierMu sync.Mutex
var revertDirentsHooks struct {
	repoID     string
	historical func(string, string)
	head       func(string)
}

// SetRevertDirentsPublicationBarriersForTest schedules the actual handler. It does
// not substitute metadata, block placement, liveness or HEAD query outcomes.
func SetRevertDirentsPublicationBarriersForTest(repoID string, historical func(string, string), head func(string)) func() {
	revertDirentsBarrierMu.Lock()
	previous := revertDirentsHooks
	revertDirentsHooks.repoID, revertDirentsHooks.historical, revertDirentsHooks.head = repoID, historical, head
	revertDirentsBarrierMu.Unlock()
	return func() {
		revertDirentsBarrierMu.Lock()
		revertDirentsHooks = previous
		revertDirentsBarrierMu.Unlock()
	}
}
func revertDirentsAfterHistoricalEntryBarrier(repoID, itemPath, fsID string) {
	revertDirentsBarrierMu.Lock()
	hook := revertDirentsHooks
	revertDirentsBarrierMu.Unlock()
	if hook.repoID == repoID && hook.historical != nil {
		hook.historical(itemPath, fsID)
	}
}
func revertDirentsBeforeHeadBarrier(repoID, itemPath string) {
	revertDirentsBarrierMu.Lock()
	hook := revertDirentsHooks
	revertDirentsBarrierMu.Unlock()
	if hook.repoID == repoID && hook.head != nil {
		hook.head(itemPath)
	}
}
