//go:build integration

package v2

import (
	"github.com/Sesame-Disk/sesamefs/internal/db"
	"sync"
)

// Only the exact dedicated visitor session/library can stop here. No classifier
// result is replaced and no process-wide function variable is overridden.
var e111AfterClassify sync.Map

type e111VisitorKey struct {
	database *db.DB
	repo     string
}

func SetRepairAfterClassifyForIntegration(database *db.DB, repo string, fn func(string, error)) func() {
	key := e111VisitorKey{database, repo}
	if _, loaded := e111AfterClassify.LoadOrStore(key, fn); loaded {
		panic("duplicate repair classifier barrier")
	}
	return func() { e111AfterClassify.Delete(key) }
}

func repairAfterClassifyBarrier(database *db.DB, repo string, outcome publishedBlockReferenceRepairCommitOutcome, err error) {
	if value, ok := e111AfterClassify.Load(e111VisitorKey{database, repo}); ok {
		name := "unknown"
		if outcome == publishedBlockReferenceRepairCommitReachable {
			name = "reachable"
		}
		if outcome == publishedBlockReferenceRepairCommitNoLongerPending {
			name = "no_longer_pending"
		}
		if outcome == publishedBlockReferenceRepairCommitSuperseded {
			name = "superseded"
		}
		if outcome == publishedBlockReferenceRepairCommitDefinitelyNotReachable {
			name = "definitely_not_reachable"
		}
		value.(func(string, error))(name, err)
	}
}
