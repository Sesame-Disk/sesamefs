//go:build integration

package integration

import "testing"

const w2PublicationContinuityEnv = "SESAMEFS_REQUIRE_W2_PUBLICATION_CONTINUITY_EVIDENCE"

var w2PublicationContinuityObserved = map[string]bool{}
var w2PublicationContinuityRequired = []string{
	"TestW2WorkerRepairLifecycle/repairBeforeCommitPreservesCandidate",
	"TestW2WorkerRepairLifecycle/lateRepairDoesNotStallCommittedDelete",
	"TestW2PublicationLivenessThroughHEAD/expiryBeforeAuthority",
	"TestW2PublicationLivenessThroughHEAD/expiryAfterAuthority",
	"TestW2PublicationSettlementAndRecovery/normalWriter",
	"TestW2PublicationSettlementAndRecovery/knownLoserRetry",
	"TestW2PublicationSettlementAndRecovery/ambiguousSettlementRetains",
	"TestW2PublicationSettlementAndRecovery/crashBeforeHEAD",
	"TestW2PublicationSettlementAndRecovery/crashAfterHEADRecovery",
	"TestW2PublicationGuardPagingAndOrganizationIsolation",
	"TestW2CoveredStoredAndSessionWriters/UploadFile",
	"TestW2CoveredStoredAndSessionWriters/BorrowedFS",
	"TestW2CoveredStoredAndSessionWriters/SessionUpload",
	"TestW2SyncPublicationContinuityAfterAuthority/direct",
	"TestW2SyncPublicationContinuityAfterAuthority/autoMerge",
	"TestW2SyncPublicationContinuityAfterAuthority/directBeforeRepair",
	"TestW2SyncPublicationContinuityAfterAuthority/autoMergeBeforeRepair",
}

func w2ObservePublicationEvidence(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		if !t.Skipped() && !t.Failed() {
			w2PublicationContinuityObserved[t.Name()] = true
		}
	})
}
func w2PublicationContinuityMissing(observed map[string]bool) []string {
	var missing []string
	for _, name := range w2PublicationContinuityRequired {
		if !observed[name] {
			missing = append(missing, name)
		}
	}
	return missing
}
func TestW2PublicationContinuityEvidenceCompleteness(t *testing.T) {
	observed := map[string]bool{}
	if len(w2PublicationContinuityMissing(observed)) != 17 {
		t.Fatal("must require all 17 named legs")
	}
	for _, name := range w2PublicationContinuityRequired {
		observed[name] = true
	}
	if len(w2PublicationContinuityMissing(observed)) != 0 {
		t.Fatal("complete evidence rejected")
	}
	delete(observed, "TestW2SyncPublicationContinuityAfterAuthority/autoMerge")
	if len(w2PublicationContinuityMissing(observed)) != 1 {
		t.Fatal("one observed leg cannot hide another missing leg")
	}
}
