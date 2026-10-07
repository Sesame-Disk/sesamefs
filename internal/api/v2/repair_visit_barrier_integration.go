//go:build integration

package v2

import (
	"github.com/Sesame-Disk/sesamefs/internal/db"
	"sync"
)

var e113BeforeVisit sync.Map

// Observe only this sweep session/library after scheduling filters, before repair
// execution. Listing and classifier outcomes are never replaced.
func SetRepairBeforeVisitForIntegration(database *db.DB, repo string, fn func(string, string)) func() {
	key := e111VisitorKey{database, repo}
	if _, loaded := e113BeforeVisit.LoadOrStore(key, fn); loaded {
		panic("duplicate repair visit observer")
	}
	return func() { e113BeforeVisit.Delete(key) }
}

func repairBeforeVisitBarrier(database *db.DB, repo, commit, fs string) {
	if fn, ok := e113BeforeVisit.Load(e111VisitorKey{database, repo}); ok {
		fn.(func(string, string))(commit, fs)
	}
}
