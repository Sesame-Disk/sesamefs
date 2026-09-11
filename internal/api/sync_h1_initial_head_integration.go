//go:build integration

package api

import "github.com/Sesame-Disk/sesamefs/internal/db"

// CreateInitialCommitForIntegration runs the real production Sync initial-HEAD
// path (the one GET /seafhttp/repo/:id/commit/HEAD takes when it reads an
// empty head) through a *db.DB connected to a specific datacenter, so the
// 3-DC evidence leg for ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01
// (internal/integration/h1_initial_head_multidc_test.go) can drive it from a
// datacenter whose replica has not seen a HEAD published elsewhere. Same
// pattern as sync_w2_putblock_xdc_integration.go: production function, not a
// reimplementation. Returns the canonical HEAD createInitialCommit settled on.
func CreateInitialCommitForIntegration(database *db.DB, repoID, orgID, userID string) (string, error) {
	h := &SyncHandler{db: database}
	return h.createInitialCommit(repoID, orgID, userID)
}
