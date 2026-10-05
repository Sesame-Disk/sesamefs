//go:build integration

package v2

import "sync"

var revertFileBarrierMu sync.Mutex
var revertFileHooks struct {
	repoID     string
	historical func(string)
	head       func()
}

// SetRevertFilePublicationBarriersForTest schedules the actual handler. It does
// not substitute metadata, block placement, liveness or HEAD query outcomes.
func SetRevertFilePublicationBarriersForTest(repoID string, historical func(string), head func()) func() {
	revertFileBarrierMu.Lock()
	previous := revertFileHooks
	revertFileHooks.repoID, revertFileHooks.historical, revertFileHooks.head = repoID, historical, head
	revertFileBarrierMu.Unlock()
	return func() {
		revertFileBarrierMu.Lock()
		revertFileHooks = previous
		revertFileBarrierMu.Unlock()
	}
}
func revertFileAfterHistoricalEntryBarrier(repoID, fsID string) {
	revertFileBarrierMu.Lock()
	hook := revertFileHooks
	revertFileBarrierMu.Unlock()
	if hook.repoID == repoID && hook.historical != nil {
		hook.historical(fsID)
	}
}
func revertFileBeforeHeadBarrier(repoID string) {
	revertFileBarrierMu.Lock()
	hook := revertFileHooks
	revertFileBarrierMu.Unlock()
	if hook.repoID == repoID && hook.head != nil {
		hook.head()
	}
}
