//go:build integration

package integration

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

const pc0PublicationEvidenceEnv = "SESAMEFS_REQUIRE_PC0_PUBLICATION_CHARACTERIZATION"

var pc0PublicationEvidence pc0PublicationEvidenceLog

type pc0PublicationEvidenceLog struct {
	legs map[string]string
}

func (e *pc0PublicationEvidenceLog) record(t *testing.T, name, result string) {
	t.Helper()
	if e.legs == nil {
		e.legs = map[string]string{}
	}
	if strings.TrimSpace(name) == "" || strings.TrimSpace(result) == "" {
		t.Fatalf("PC0 3-DC: empty leg name or result")
	}
	if previous, ok := e.legs[name]; ok && previous != result {
		t.Fatalf("PC0 3-DC: leg %s recorded twice (%s vs %s)", name, previous, result)
	}
	e.legs[name] = result
	t.Logf("PC0 3-DC leg %s = %s", name, result)
}

func (e *pc0PublicationEvidenceLog) complete() bool {
	missing := e.missing()
	return len(missing) == 0
}

func (e *pc0PublicationEvidenceLog) missing() []string {
	required := pc0RequiredMultiDCLegs()
	var missing []string
	for _, name := range required {
		if strings.TrimSpace(e.legs[name]) == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

func pc0RequiredMultiDCLegs() []string {
	return []string{"M1", "M2", "M3", "M4", "M5", "M6", "M7", "M8"}
}

func pc0GateArmed() bool {
	return os.Getenv(pc0PublicationEvidenceEnv) == "1"
}

func TestPC0PublicationMultiDCCharacterization(t *testing.T) {
	if !pc0GateArmed() {
		t.Skipf("%s is not set", pc0PublicationEvidenceEnv)
	}

	// Missing fixture cannot skip-green. The W2 post-HEAD 3-DC hosts are the
	// reused NetworkTopologyStrategy stack (dc-na/dc-eu/dc-asia).
	endpoints := w2PostHead3DCEndpoints(t)
	for _, dc := range []string{"dc-na", "dc-eu", "dc-asia"} {
		database := w2PostHead3DCConnect(t, dc, endpoints)
		var localDC string
		if err := database.Session().Query(`SELECT data_center FROM system.local`).Scan(&localDC); err != nil {
			t.Fatalf("PC0 3-DC: %s system.local: %v", dc, err)
		}
		if localDC != dc {
			t.Fatalf("PC0 3-DC: connected through %s but system.local data_center=%q", dc, localDC)
		}
	}

	pc0PublicationEvidence.record(t, "M1", "OBSERVED-SOURCE: LQ presence/stage/exact-P reads are local; HEAD LWT is the global exception")
	pc0PublicationEvidence.record(t, "M2", "UNKNOWN: #210 not in this baseline; LQ miss currently skips Sync readiness")
	pc0PublicationEvidence.record(t, "M3", "UNKNOWN: publication-complete one-DC-down not re-run here; repair EQ/X2 remain separate evidence")
	pc0PublicationEvidence.record(t, "M4", "PRIOR-EVIDENCE-NOT-RERUN: W2 post-HEAD 3-DC settlement/retain; this harness only proved 3-DC connectivity")
	pc0PublicationEvidence.record(t, "M5", "UNKNOWN: live two-DC concurrent publishers not executed; CAS winner is Paxos-level only")
	pc0PublicationEvidence.record(t, "M6", "PRIOR-EVIDENCE-NOT-RERUN: W2 post-HEAD 3-DC local-miss is not cleanup; this harness only proved 3-DC connectivity")
	pc0PublicationEvidence.record(t, "M7", "OBSERVED-SOURCE: CreateFileFromBlocks exact-P; other funnels have no fence")
	pc0PublicationEvidence.record(t, "M8", "MIXED: CFFB/shared OBSERVED; Sync xDC GAP; OnlyOffice/SeafHTTP/cross-repo 3-DC EVIDENCE GAP")

	names := append([]string{}, pc0RequiredMultiDCLegs()...)
	sort.Strings(names)
	fmt.Printf("PC0 3-DC executed legs:")
	for _, name := range names {
		fmt.Printf(" %s=%s;", name, pc0PublicationEvidence.legs[name])
	}
	fmt.Printf("\n")
	if !pc0PublicationEvidence.complete() {
		t.Fatalf("PC0 3-DC: missing legs %s", strings.Join(pc0PublicationEvidence.missing(), ","))
	}
}
