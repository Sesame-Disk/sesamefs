package db

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestW2PublicationGuardHandoffAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name             string
		refs             []bool
		pending          bool
		refErr, guardErr error
		want             bool
	}{
		{name: "live", refs: []bool{true}, want: true},
		{name: "repair", refs: []bool{false}, pending: true, want: true},
		{name: "settledToFS", refs: []bool{false, true}, want: true},
		{name: "actualZero", refs: []bool{false, false}},
		{name: "refUnavailable", refErr: errors.New("unavailable")},
		{name: "repairUnavailable", refs: []bool{false}, guardErr: errors.New("unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			got, err := publicationLivenessBeforeDestruction(func() (bool, error) {
				if tc.refErr != nil {
					return false, tc.refErr
				}
				if calls >= len(tc.refs) {
					t.Fatal("unexpected extra ref read")
				}
				v := tc.refs[calls]
				calls++
				return v, nil
			}, func() (bool, error) { return tc.pending, tc.guardErr })
			wantErr := tc.refErr != nil || tc.guardErr != nil
			if got != tc.want || (err != nil) != wantErr {
				t.Fatalf("live=%v err=%v", got, err)
			}
		})
	}
}

func TestW2PublicationGuardReadWriteDomain(t *testing.T) {
	read, err := os.ReadFile("publication_liveness.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(read)
	for _, required := range []string{"WHERE bucket = ? AND org_id = ?", "Consistency(gocql.EachQuorum)", "iter.Close()", "PageSize(256)"} {
		if !strings.Contains(s, required) {
			t.Fatalf("missing destructive-read contract %s", required)
		}
	}
	if strings.Contains(s, "LIMIT ") || strings.Contains(s, "ALLOW FILTERING") {
		t.Fatal("must consume all matching organization repair pages")
	}
	writer, err := os.ReadFile("../api/v2/publish_repair.go")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(writer), "var insertPublishedBlockReferenceRepairFn")
	end := strings.Index(string(writer)[start:], "var deletePublishedBlockReferenceRepairFn")
	if !strings.Contains(string(writer)[start:start+end], "Consistency(db.BlockReferenceWriteConsistency)") {
		t.Fatal("repair acquisition must pin LQ independently of session")
	}
}
