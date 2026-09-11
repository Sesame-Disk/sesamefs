//go:build integration

package v2

import (
	"time"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
)

// GroupLibraryCreationRequest is the production groupLibraryCreationRequest,
// exposed so the 3-DC evidence leg for the group-library creation claim
// (internal/integration/h1_initial_head_multidc_test.go) can drive the
// production protocol through a *db.DB connected to a specific datacenter.
// Same pattern as internal/api/sync_h1_initial_head_integration.go:
// production functions, not a reimplementation.
type GroupLibraryCreationRequest = groupLibraryCreationRequest

// GroupLibraryCreationForIntegration is what BeginGroupLibraryCreationForIntegration
// resolved: the claim's fixed parameters and whether it resumed an existing
// claim.
type GroupLibraryCreationForIntegration struct {
	LibraryID    string
	ShareID      string
	StorageClass string
	CreatedAt    time.Time
	Resumed      bool
}

func exportGroupLibraryCreation(creation groupLibraryCreation) GroupLibraryCreationForIntegration {
	return GroupLibraryCreationForIntegration{
		LibraryID:    creation.Claim.LibraryID,
		ShareID:      creation.Claim.ShareID,
		StorageClass: creation.ProjectionRow.StorageClass,
		CreatedAt:    creation.Claim.CreatedAt,
		Resumed:      creation.Resumed,
	}
}

// BeginGroupLibraryCreationForIntegration runs only the claim phase (mint
// rows + acquire, or resume), leaving the library unpublished and unshared
// with its claim held — the state a creation handler leaves behind when
// InitializeLibraryFS ends pending and it answers 503.
func BeginGroupLibraryCreationForIntegration(database *dbpkg.DB, req GroupLibraryCreationRequest) (GroupLibraryCreationForIntegration, error) {
	creation, err := beginGroupLibraryCreation(database, req, nil)
	if err != nil {
		return GroupLibraryCreationForIntegration{}, err
	}
	return exportGroupLibraryCreation(creation), nil
}

// RunGroupLibraryCreationForIntegration runs the full production sequence
// (claim → initialize → share → release) exactly as the handlers do. outcome
// is "completed", "pending" or "failed".
func RunGroupLibraryCreationForIntegration(database *dbpkg.DB, req GroupLibraryCreationRequest) (GroupLibraryCreationForIntegration, string, error) {
	creation, outcome, err := runGroupLibraryCreation(database, req, nil)
	names := map[groupLibraryCreationOutcome]string{
		groupLibraryCreationCompleted: "completed",
		groupLibraryCreationPending:   "pending",
		groupLibraryCreationFailed:    "failed",
	}
	return exportGroupLibraryCreation(creation), names[outcome], err
}
