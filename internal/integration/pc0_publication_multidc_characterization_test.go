//go:build integration

package integration

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

const pc0PublicationCharacterizationEnv = "SESAMEFS_REQUIRE_PC0_PUBLICATION_CHARACTERIZATION"

var pc0PublicationMatrix pc0PublicationMatrixLog

type pc0PublicationMatrixLog struct {
	rows map[string]string
}

func (e *pc0PublicationMatrixLog) record(t *testing.T, name, result string) {
	t.Helper()
	if e.rows == nil {
		e.rows = map[string]string{}
	}
	if strings.TrimSpace(name) == "" || strings.TrimSpace(result) == "" {
		t.Fatalf("PC0 3-DC: empty matrix row name or result")
	}
	if previous, ok := e.rows[name]; ok && previous != result {
		t.Fatalf("PC0 3-DC: matrix row %s recorded twice (%s vs %s)", name, previous, result)
	}
	e.rows[name] = result
	t.Logf("PC0 3-DC matrix %s = %s", name, result)
}

func (e *pc0PublicationMatrixLog) matrixRecorded() bool {
	return len(e.missing()) == 0
}

func (e *pc0PublicationMatrixLog) missing() []string {
	required := pc0RequiredMultiDCMatrixRows()
	var missing []string
	for _, name := range required {
		if strings.TrimSpace(e.rows[name]) == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

func pc0RequiredMultiDCMatrixRows() []string {
	return []string{"M1", "M2", "M3", "M4", "M5", "M6", "M7", "M8"}
}

func pc0CharacterizationGateArmed() bool {
	return os.Getenv(pc0PublicationCharacterizationEnv) == "1"
}

func TestPC0PublicationMultiDCCharacterization(t *testing.T) {
	if !pc0CharacterizationGateArmed() {
		t.Skipf("%s is not set", pc0PublicationCharacterizationEnv)
	}

	// Missing fixture cannot skip-green. This gate proves 3-DC topology
	// connectivity and records the characterization matrix. It does not
	// execute publication races M1–M8; UNKNOWN / PRIOR-EVIDENCE-NOT-RERUN
	// / GAP rows are characterization status, not executed evidence.
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

	pc0PublicationMatrix.record(t, "M1", "OBSERVED-SOURCE: LQ presence/stage/exact-P reads are local; HEAD LWT is the global exception")
	pc0PublicationMatrix.record(t, "M2", "UNKNOWN: #210 not in this baseline; LQ miss currently skips Sync readiness")
	pc0PublicationMatrix.record(t, "M3", "UNKNOWN: publication-complete one-DC-down not re-run here; repair EQ/X2 remain separate evidence")
	pc0PublicationMatrix.record(t, "M4", "PRIOR-EVIDENCE-NOT-RERUN: W2 post-HEAD 3-DC settlement/retain; this harness only proved 3-DC connectivity")
	pc0PublicationMatrix.record(t, "M5", "UNKNOWN: live two-DC concurrent publishers not executed; CAS winner is Paxos-level only")
	pc0PublicationMatrix.record(t, "M6", "PRIOR-EVIDENCE-NOT-RERUN: W2 post-HEAD 3-DC local-miss is not cleanup; this harness only proved 3-DC connectivity")
	pc0PublicationMatrix.record(t, "M7", "OBSERVED-SOURCE: CreateFileFromBlocks exact-P; other funnels have no fence")
	pc0PublicationMatrix.record(t, "M8", "MIXED: CFFB/shared OBSERVED; Sync xDC GAP; OnlyOffice/SeafHTTP/cross-repo 3-DC EVIDENCE GAP")

	names := append([]string{}, pc0RequiredMultiDCMatrixRows()...)
	sort.Strings(names)
	fmt.Printf("PC0 3-DC topology+matrix rows:")
	for _, name := range names {
		fmt.Printf(" %s=%s;", name, pc0PublicationMatrix.rows[name])
	}
	fmt.Printf("\n")
	if !pc0PublicationMatrix.matrixRecorded() {
		t.Fatalf("PC0 3-DC: missing matrix rows %s", strings.Join(pc0PublicationMatrix.missing(), ","))
	}
}
