//go:build integration

package db

import (
	"context"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// BlockMappingEvidenceIntegrationInjected marks claims written by the
// integration-only race hook below. Production never writes it and never
// accepts it as usable authority.
const BlockMappingEvidenceIntegrationInjected = "integration_injected"

// ClaimBlockMappingAuthorityForIntegration writes the production write-once
// claim with a caller-chosen canonical value and evidence, bypassing
// provenance and the projection freeze. Two different values can only both be
// provable under a real SHA-1 collision, which a test cannot produce, so 3-DC
// split-authority evidence injects them here; passing
// BlockMappingEvidencePhysicalBytesV1 reproduces a claim whose promotion
// stopped before freezing its projection. It exists only in integration-tagged
// test binaries.
func ClaimBlockMappingAuthorityForIntegration(ctx context.Context, session *gocql.Session, orgID, representationID, externalID, internalID, evidence string) (IdentityClaimOutcome, *BlockMappingAuthorityClaim, error) {
	identity, err := canonicalBlockMappingIdentity(orgID, representationID, externalID)
	if err != nil {
		return IdentityClaimUnknown, nil, err
	}
	return claimBlockMappingAuthority(ctx, session, blockMappingProvenance{
		identity:   identity,
		internalID: internalID,
		evidence:   evidence,
	})
}
