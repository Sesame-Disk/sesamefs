//go:build integration

package db

import (
	"context"

	"github.com/Sesame-Disk/sesamefs/internal/storage"
)

// LibraryBaselineCertifierIntegrationHooks exposes deterministic race and
// ambiguous-response injection only to integration-tagged test binaries.
type LibraryBaselineCertifierIntegrationHooks struct {
	AfterLiveness    func(context.Context, string, string, BlockPhysicalLocation)
	BeforeWitnessCAS func(context.Context, string, string, string)
	AfterWitnessCAS  func(LibraryContinuityCASResult, error) (LibraryContinuityCASResult, error)
}

// CertifyLibraryBaselineWithIntegrationHooks runs the production certifier
// with narrowly-scoped hooks for multi-DC race/fault evidence.
func (db *DB) CertifyLibraryBaselineWithIntegrationHooks(
	ctx context.Context,
	storageManager *storage.Manager,
	orgID, libraryID, observedHead string,
	hooks LibraryBaselineCertifierIntegrationHooks,
) LibraryBaselineCertificationResult {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, libraryBaselineCertifierTestHooksContextKey{}, libraryBaselineCertifierTestHooks{
		afterLiveness:    hooks.AfterLiveness,
		beforeWitnessCAS: hooks.BeforeWitnessCAS,
		afterWitnessCAS:  hooks.AfterWitnessCAS,
	})
	return db.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, observedHead)
}
