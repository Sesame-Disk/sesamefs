package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	v2 "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	"github.com/Sesame-Disk/sesamefs/internal/config"
	"github.com/Sesame-Disk/sesamefs/internal/db"
	"golang.org/x/sync/errgroup"
)

func validateSeafHTTPPlacementShape(p seafHTTPBlockPlacement) error {
	if !db.IsSHA256BlockID(p.blockID) || db.NormalizeBlockID(p.blockID) != p.blockID || !config.IsCanonicalStorageClassName(p.storageClass) || p.storageKey == "" || strings.TrimSpace(p.storageKey) != p.storageKey {
		return fmt.Errorf("invalid streaming publication placement for block %s", p.blockID)
	}
	return nil
}

type seafHTTPPublicationPlacementError struct {
	rejectedBlockIDs []string
	cause            error
}

func (e *seafHTTPPublicationPlacementError) Error() string { return e.cause.Error() }
func (e *seafHTTPPublicationPlacementError) Unwrap() error { return e.cause }

var validateSeafHTTPStreamingAuthorityFn = func(database *db.DB, orgID string, p seafHTTPBlockPlacement) (db.BlockRepairAuthorityOutcome, error) {
	return database.ValidateBorrowedFSPublicationAuthority(orgID, p.blockID, db.BlockPhysicalLocation{StorageClass: p.storageClass, StorageKey: p.storageKey})
}

// Durable repair protects the handoff from these advisory authority reads to
// HEAD. Validate the original confirmed tuples, not recaptured canonical lives.
// Validate all distinct blocks with bounded fan-out and retain rejected IDs for
// selective same-tracker recovery. Unknown/Permanent never authorize reminting.
func validateSeafHTTPStreamingPublicationPlacements(ctx context.Context, database *db.DB, orgID string, placements []seafHTTPBlockPlacement) error {
	if len(placements) == 0 {
		return fmt.Errorf("missing streaming publication placements")
	}
	unique := make([]seafHTTPBlockPlacement, 0, len(placements))
	seen := make(map[string]seafHTTPBlockPlacement, len(placements))
	for _, p := range placements {
		if err := validateSeafHTTPPlacementShape(p); err != nil {
			return err
		}
		if previous, ok := seen[p.blockID]; ok {
			if previous != p {
				return fmt.Errorf("contradictory streaming placements for block %s", p.blockID)
			}
			continue
		}
		seen[p.blockID] = p
		unique = append(unique, p)
	}
	var mu sync.Mutex
	failures := make([]error, len(unique))
	rejected := make([]bool, len(unique))
	var group errgroup.Group
	group.SetLimit(finalizeUploadConcurrency)
	for i, p := range unique {
		group.Go(func() error {
			var failure error
			blocked := false
			if err := ctx.Err(); err != nil {
				failure = err
			} else {
				outcome, err := validateSeafHTTPStreamingAuthorityFn(database, orgID, p)
				if outcome != db.BlockRepairAuthorityAuthorized || err != nil {
					failure = fmt.Errorf("streaming block %s exact-P rejected (outcome=%v)", p.blockID, outcome)
					if err != nil {
						failure = errors.Join(failure, err)
					}
					if outcome == db.BlockRepairAuthorityBlocked || outcome == db.BlockRepairAuthorityChanged {
						blocked = true
						failure = errors.Join(v2.ErrBlockDeleteInProgress, failure)
					}
				}
			}
			mu.Lock()
			failures[i], rejected[i] = failure, blocked
			mu.Unlock()
			return nil
		})
	}
	_ = group.Wait()
	failure := errors.Join(ctx.Err(), errors.Join(failures...))
	if failure == nil {
		return nil
	}
	var ids []string
	for i, blocked := range rejected {
		if blocked {
			ids = append(ids, unique[i].blockID)
		}
	}
	return &seafHTTPPublicationPlacementError{rejectedBlockIDs: ids, cause: failure}
}
