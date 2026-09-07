//go:build integration

package v2

import (
	"fmt"
	"strings"

	"github.com/Sesame-Disk/sesamefs/internal/db"
)

// SettlePublishedBlockReferenceRepairForIntegration invokes the production
// settlement path with an explicitly selected classifier outcome. It lets the
// evidence suite model an ambiguous CAS confirmation result without replacing
// a process-wide classifier that a live repair worker may call concurrently.
func SettlePublishedBlockReferenceRepairForIntegration(database *db.DB, orgID, repoID, commitID, fsID string, stagedBlockIDs []string, outcome string, injectedErr error) error {
	var outcomeValue publishedBlockReferenceRepairCommitOutcome
	switch strings.ToLower(strings.TrimSpace(outcome)) {
	case "reachable":
		outcomeValue = publishedBlockReferenceRepairCommitReachable
	case "unknown":
		outcomeValue = publishedBlockReferenceRepairCommitUnknown
	default:
		return fmt.Errorf("unknown integration repair outcome %q", outcome)
	}
	return settlePublishedBlockReferenceRepair(database, newPublishedBlockReferenceRepair(orgID, repoID, commitID, fsID, stagedBlockIDs), outcomeValue, injectedErr)
}

// PublishedBlockReferenceRepairCommitOutcomeForIntegration runs the production
// cold-path classifier without exposing its internal outcome type to integration
// packages. It is used by the standalone 3-DC evidence leg.
func PublishedBlockReferenceRepairCommitOutcomeForIntegration(database *db.DB, orgID, repoID, commitID string) (string, error) {
	outcome, err := publishedBlockReferenceRepairCommitReachableFn(database, orgID, repoID, commitID)
	switch outcome {
	case publishedBlockReferenceRepairCommitReachable:
		return "reachable", err
	default:
		return "unknown", err
	}
}
