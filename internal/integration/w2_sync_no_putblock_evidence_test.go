//go:build integration

package integration

import "testing"

const w24EvidenceEnv = "SESAMEFS_REQUIRE_W24_CHARACTERIZATION"

var w24Observed = map[string]bool{}
var w24Required = []string{
	"TestW2SyncNoPutBlock/direct/writerFirst",
	"TestW2SyncNoPutBlock/direct/gcBeforeStage",
	"TestW2SyncNoPutBlock/direct/gcBeforeRepair",
	"TestW2SyncNoPutBlock/direct/repairFirst",
	"TestW2SyncNoPutBlock/direct/fullyRetired",
	"TestW2SyncNoPutBlock/autoMerge/writerFirst",
	"TestW2SyncNoPutBlock/autoMerge/gcBeforeStage",
	"TestW2SyncNoPutBlock/autoMerge/gcBeforeRepair",
	"TestW2SyncNoPutBlock/autoMerge/repairFirst",
	"TestW2SyncNoPutBlock/autoMerge/fullyRetired",
}

func w24Observe(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		if !t.Failed() && !t.Skipped() {
			w24Observed[t.Name()] = true
		}
	})
}

func w24Missing(observed map[string]bool) []string {
	var missing []string
	for _, name := range w24Required {
		if !observed[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

func TestW24EvidenceCompleteness(t *testing.T) {
	observed := map[string]bool{}
	if len(w24Missing(observed)) != 10 {
		t.Fatal("all ten named legs required")
	}
	for _, name := range w24Required {
		observed[name] = true
	}
	if len(w24Missing(observed)) != 0 {
		t.Fatal("complete evidence rejected")
	}
	delete(observed, "TestW2SyncNoPutBlock/autoMerge/gcBeforeRepair")
	if len(w24Missing(observed)) != 1 {
		t.Fatal("missing auto-merge leg hidden")
	}
}
