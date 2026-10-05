package v2

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Sesame-Disk/sesamefs/internal/config"
	"github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"golang.org/x/sync/errgroup"
)

// captureCopiedBlockPlacements retains the source attempt's canonical physical
// lives. It does not rematerialize content or treat a later mapping as authority.
func (h *FSHelper) captureCopiedBlockPlacements(orgID, srcRepoID string, files []*pendingPublishedFile) ([]commitBlockPlacement, error) {
	var placements []commitBlockPlacement
	seen := make(map[string]bool)
	for _, file := range files {
		if file == nil {
			continue
		}
		ids, err := h.resolveStoredBlockIDs(orgID, srcRepoID, file.externalBlockIDs)
		if err != nil {
			return nil, fmt.Errorf("resolve copied source blocks: %w", err)
		}
		for _, id := range db.NormalizeBlockIDs(ids) {
			if seen[id] {
				continue
			}

			seen[id] = true
			placements = append(placements, commitBlockPlacement{blockID: id})
		}
	}
	g, gctx := errgroup.WithContext(context.Background())
	sem := make(chan struct{}, blockVerifyConcurrency)
	for i := range placements {
		i := i
		g.Go(func() error {
			select {
			case sem <- struct{}{}:
			case <-gctx.Done():
				return gctx.Err()
			}
			defer func() { <-sem }()
			block := &placements[i]
			id := block.blockID
			err := h.db.Session().Query(`SELECT storage_class, storage_key FROM blocks WHERE org_id = ? AND block_id = ?`, orgID, id).WithContext(gctx).Consistency(gocql.LocalQuorum).Scan(&block.storageClass, &block.storageKey)
			if errors.Is(err, gocql.ErrNotFound) {
				return fmt.Errorf("%w: copied block %s has no canonical physical life", ErrBlockDeleteInProgress, id)
			}
			if err != nil {
				return fmt.Errorf("read copied block %s physical life: %w", id, err)
			}
			if !db.IsSHA256BlockID(id) || !config.IsCanonicalStorageClassName(block.storageClass) || block.storageKey == "" || strings.TrimSpace(block.storageKey) != block.storageKey {
				return fmt.Errorf("%w: incomplete copied physical life for %s", db.ErrBlockMetadataPermanent, id)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return placements, nil
}

// Staging re-resolves the destination's IDs. Require that it still names exactly
// the captured set, then reuse the final exact-P primitive after durable repair.
func (h *FSHelper) validateCopiedBlockPublication(orgID string, files []*pendingPublishedFile, placements []commitBlockPlacement) error {
	expected := make(map[string]bool, len(placements))
	for _, block := range placements {
		expected[block.blockID] = true
	}
	staged := db.NormalizeBlockIDs(pendingPublishedFileInternalBlockIDs(files))
	if len(staged) != len(expected) {
		return fmt.Errorf("%w: copied block set changed during staging", ErrBlockDeleteInProgress)
	}
	for _, id := range staged {
		if !expected[id] {
			return fmt.Errorf("%w: copied block identity changed during staging", ErrBlockDeleteInProgress)
		}
	}
	return (&FileHandler{db: h.db}).validateCommitBlockPublicationFences(orgID, placements)
}
