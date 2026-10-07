//go:build integration

package v2

import (
	"github.com/Sesame-Disk/sesamefs/internal/db"
	"sync"
)

var e115BeforeRenew sync.Map

// Observe only this sweep session/library after the last durable pending
// re-check observed the row, before the renewal write. Nothing is replaced.
func SetRepairBeforeRenewForIntegration(database *db.DB, repo string, fn func(string, string)) func() {
	key := e111VisitorKey{database, repo}
	if _, loaded := e115BeforeRenew.LoadOrStore(key, fn); loaded {
		panic("duplicate repair renewal observer")
	}
	return func() { e115BeforeRenew.Delete(key) }
}

func repairBeforeRenewBarrier(database *db.DB, repo, commit, fs string) {
	if fn, ok := e115BeforeRenew.Load(e111VisitorKey{database, repo}); ok {
		fn.(func(string, string))(commit, fs)
	}
}
