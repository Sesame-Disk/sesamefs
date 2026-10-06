//go:build integration

package v2

import "sync"

var revertDirectoryBarrierMu sync.Mutex
var revertDirectoryHooks struct {
	repoID     string
	historical func(string)
	head       func()
}

// SetRevertDirectoryPublicationBarriersForTest schedules the actual handler. It does
// not substitute metadata, block placement, liveness or HEAD query outcomes.
func SetRevertDirectoryPublicationBarriersForTest(repoID string, historical func(string), head func()) func() {
	revertDirectoryBarrierMu.Lock()
	previous := revertDirectoryHooks
	revertDirectoryHooks.repoID, revertDirectoryHooks.historical, revertDirectoryHooks.head = repoID, historical, head
	revertDirectoryBarrierMu.Unlock()
	return func() {
		revertDirectoryBarrierMu.Lock()
		revertDirectoryHooks = previous
		revertDirectoryBarrierMu.Unlock()
	}
}
func revertDirectoryAfterHistoricalEntryBarrier(repoID, fsID string) {
	revertDirectoryBarrierMu.Lock()
	hook := revertDirectoryHooks
	revertDirectoryBarrierMu.Unlock()
	if hook.repoID == repoID && hook.historical != nil {
		hook.historical(fsID)
	}
}
func revertDirectoryBeforeHeadBarrier(repoID string) {
	revertDirectoryBarrierMu.Lock()
	hook := revertDirectoryHooks
	revertDirectoryBarrierMu.Unlock()
	if hook.repoID == repoID && hook.head != nil {
		hook.head()
	}
}
