//go:build integration

package api

import (
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
)

// This file exposes the minimum production Sync PutBlock provenance surface
// needed by the standalone real 3-DC evidence leg for
// ISSUE-SYNC-PUTBLOCK-CROSS-DC-PROVENANCE-VISIBILITY-01
// (internal/integration/sync_w2_putblock_xdc_provenance_multidc_test.go),
// following the same pattern as
// internal/api/v2/publish_repair_integration.go: a real production function
// called directly through a *db.DB connected to a specific datacenter,
// without standing up a full sesamefs instance per datacenter.

// SyncBlockUploadReferrerForIntegration exposes the exact
// up:sync:<repo>:<block> referrer format PutBlock establishes, so the 3-DC
// leg can drive db.DB.BlockReferenceExistsLocalQuorum/BlockReferenceExistsEachQuorum
// directly without duplicating the format string.
func SyncBlockUploadReferrerForIntegration(repoID, blockID string) string {
	return syncBlockUploadReferrer(repoID, blockID)
}

// SyncSimulatePutBlockProvenanceForIntegration writes the exact up:
// reference a real PutBlock call establishes for blockID, using the
// production AddProvisionalBlockReferenceWithExpiry primitive. Used to
// simulate "PutBlock happened in this datacenter" from a direct Cassandra
// connection.
func SyncSimulatePutBlockProvenanceForIntegration(database *db.DB, orgID, repoID, blockID, storageClass string, expiresAt time.Time) error {
	referrer := syncBlockUploadReferrer(repoID, blockID)
	return database.AddProvisionalBlockReferenceWithExpiry(orgID, blockID, referrer, repoID, storageClass, expiresAt)
}

// SyncBlockHasOwnLivenessProvenanceForIntegration runs the real production
// Sync PutBlock provenance scope-gate decision -- local LOCAL_QUORUM first,
// EACH_QUORUM cross-DC fallback only on a clean local miss -- without
// exposing SyncHandler's internals to integration packages. This calls
// syncBlockHasOwnLivenessProvenanceFn itself, not a reimplementation: it is
// RED (found=false) without the EACH_QUORUM fallback and GREEN with it.
func SyncBlockHasOwnLivenessProvenanceForIntegration(database *db.DB, orgID, repoID, blockID string) (bool, error) {
	h := &SyncHandler{db: database}
	return syncBlockHasOwnLivenessProvenanceFn(h, orgID, repoID, blockID)
}
