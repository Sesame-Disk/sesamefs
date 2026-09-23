//go:build integration

package db

import (
	"context"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// BlockMappingEvidenceIntegrationInjected marks claims written by the
// integration-only race hook below. Production never writes it.
const BlockMappingEvidenceIntegrationInjected = "integration_injected"

// ClaimBlockMappingAuthorityForIntegration races the production write-once
// claim with a caller-chosen canonical value. Two different values can only
// both be provable under a real SHA-1 collision, which a test cannot produce,
// so 3-DC split-authority evidence injects them here. It exists only in
// integration-tagged test binaries and bypasses provenance on purpose.
func ClaimBlockMappingAuthorityForIntegration(ctx context.Context, session *gocql.Session, orgID, representationID, externalID, internalID string) (IdentityClaimOutcome, *BlockMappingAuthorityClaim, error) {
	identity, err := canonicalBlockMappingIdentity(orgID, representationID, externalID)
	if err != nil {
		return IdentityClaimUnknown, nil, err
	}
	return claimBlockMappingAuthority(ctx, session, blockMappingProvenance{
		identity:   identity,
		internalID: internalID,
		evidence:   BlockMappingEvidenceIntegrationInjected,
	})
}
