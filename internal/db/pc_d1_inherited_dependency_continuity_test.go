package db

import (
	"reflect"
	"testing"
)

type pcd1ContinuityStatus string

const (
	pcd1ContinuityUnknown     pcd1ContinuityStatus = "UNKNOWN"
	pcd1ContinuityConditional pcd1ContinuityStatus = "CONDITIONAL"
)

// TestPCD1LogicalPositiveDeltaOmitsUncertifiedInheritedDependencies is the
// minimum executable counterexample for ISSUE-PC0-INHERITED-DEPENDENCY-
// CONTINUITY-01. It calls the same test-only logical operation already used by
// R3 vectors and adds the missing continuity state explicitly.
func TestPCD1LogicalPositiveDeltaOmitsUncertifiedInheritedDependencies(t *testing.T) {
	oldHead := []string{"A", "B", "C"}
	newHead := []string{"A", "B", "C", "D"}
	continuity := map[string]pcd1ContinuityStatus{
		"A": pcd1ContinuityUnknown,
		"B": pcd1ContinuityUnknown,
		"C": pcd1ContinuityConditional,
	}

	got := r3LogicalPositiveDelta(oldHead, newHead, nil)
	want := []string{"D"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LogicalPositiveBlockDelta(H1,H2) = %v, want %v", got, want)
	}
	for inherited, status := range continuity {
		if status != pcd1ContinuityUnknown && status != pcd1ContinuityConditional {
			t.Fatalf("unexpected seed status for %s: %q", inherited, status)
		}
		for _, id := range got {
			if id == inherited {
				t.Fatalf("inherited %s with continuity %s unexpectedly entered positive delta", inherited, status)
			}
		}
	}
	if len(got) != 1 || got[0] != "D" {
		t.Fatalf("newly-live dependency D was not retained in delta: %v", got)
	}
}
