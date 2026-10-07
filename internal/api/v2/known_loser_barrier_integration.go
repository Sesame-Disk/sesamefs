//go:build integration

package v2

import (
	"github.com/Sesame-Disk/sesamefs/internal/db"
	"sync"
)

var e112LoserObservers sync.Map

// Observe only the exact Office writer session/library, after the real definitive
// HEAD conflict and before any request-local cleanup. No outcome is replaced.
func SetKnownLoserBeforeCleanupForIntegration(database *db.DB, repo string, fn func(string)) func() {
	key := e111VisitorKey{database, repo}
	if _, loaded := e112LoserObservers.LoadOrStore(key, fn); loaded {
		panic("duplicate known loser observer")
	}
	return func() { e112LoserObservers.Delete(key) }
}

func knownLoserBeforeCleanupBarrier(database *db.DB, repo, commit string) {
	if fn, ok := e112LoserObservers.Load(e111VisitorKey{database, repo}); ok {
		fn.(func(string))(commit)
	}
}
