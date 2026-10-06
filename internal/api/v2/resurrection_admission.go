package v2

import (
	"context"
	"fmt"

	"github.com/Sesame-Disk/sesamefs/internal/db"
)

// captureRetainedHistoricalFile admits only a file whose own permanent fs:
// references have already settled. Metadata existence and somebody else's up:
// are insufficient. This does not certify commit ancestry or protect against
// Phase5/6 metadata destruction; those remain separate open prerequisites.
func (h *FSHelper) captureRetainedHistoricalFile(orgID, repoID string, entry FSEntry) ([]commitBlockPlacement, error) {
	var kind string
	var ids []string
	var size int64
	if err := h.db.Session().Query(`SELECT obj_type, block_ids, size_bytes FROM fs_objects WHERE library_id = ? AND fs_id = ?`, repoID, entry.ID).Scan(&kind, &ids, &size); err != nil {
		return nil, fmt.Errorf("read historical file: %w", err)
	}
	if kind != "file" || size < 0 || size != entry.Size || (size > 0 && len(ids) == 0) {
		return nil, fmt.Errorf("historical file layout is incomplete")
	}
	resolved, err := h.resolveStoredBlockIDs(orgID, repoID, ids)
	if err != nil {
		return nil, fmt.Errorf("resolve historical blocks: %w", err)
	}
	placements := make([]commitBlockPlacement, 0, len(resolved))
	for _, id := range db.NormalizeBlockIDs(resolved) {
		if !db.IsSHA256BlockID(id) {
			return nil, fmt.Errorf("historical block mapping is incomplete")
		}
		placements = append(placements, commitBlockPlacement{blockID: id})
	}
	if err := h.requireHistoricalFileReferences(orgID, repoID, entry.ID, placements); err != nil {
		return nil, err
	}
	return h.captureCopiedBlockPlacements(orgID, repoID, []*pendingPublishedFile{{fsID: entry.ID, externalBlockIDs: ids}})
}

func (h *FSHelper) requireHistoricalFileReferences(orgID, repoID, fsID string, placements []commitBlockPlacement) error {
	ref := db.BlockReferrerForFSObject(repoID, fsID)
	return runBoundedPublicationChecks(len(placements), func(ctx context.Context, i int) error {
		var observed string
		if err := h.db.Session().Query(`SELECT referrer FROM block_references WHERE org_id = ? AND block_id = ? AND referrer = ?`, orgID, placements[i].blockID, ref).WithContext(ctx).Scan(&observed); err != nil {
			return fmt.Errorf("historical file lacks settled permanent liveness: %w", err)
		}
		if observed != ref {
			return fmt.Errorf("historical reference observation is contradictory")
		}
		return nil
	})
}

// Recheck the retained references and original exact P after the per-item retry
// work and immediately before HEAD. Unknown, changed and claimed lives fail
// closed through the existing authority primitive. No new reference is acquired.
func (h *FSHelper) validateRetainedHistoricalFile(orgID, repoID, fsID string, placements []commitBlockPlacement) error {
	if err := h.requireHistoricalFileReferences(orgID, repoID, fsID, placements); err != nil {
		return err
	}
	return (&FileHandler{db: h.db}).validateCommitBlockPublicationFences(orgID, placements)
}
