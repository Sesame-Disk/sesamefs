//go:build integration

package v2

import "sync"

var e115bBeforeFinalFence sync.Map

// Observe only the matching CreateFile library after pub: staging, durable repair
// queueing and insertCommit, before the final exact-P fence. Nothing is replaced.
func SetCreateFileBeforeFinalFenceForTest(repoID string, fn func()) func() {
	if _, loaded := e115bBeforeFinalFence.LoadOrStore(repoID, fn); loaded {
		panic("duplicate CreateFile final-fence observer")
	}
	return func() { e115bBeforeFinalFence.Delete(repoID) }
}

func createFileBeforeFinalFenceBarrier(repoID string) {
	if fn, ok := e115bBeforeFinalFence.Load(repoID); ok {
		fn.(func())()
	}
}
