package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

func continuityString(value string) *string {
	return &value
}

func continuityTime(value time.Time) *time.Time {
	return &value
}

func continuityFunctionSource(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "library_continuity.go"))
	if err != nil {
		t.Fatalf("read continuity implementation: %v", err)
	}
	source := string(raw)
	start := strings.Index(source, "func "+name+"(")
	if start < 0 {
		t.Fatalf("function %s not found", name)
	}
	rest := source[start:]
	if next := strings.Index(rest[len("func "+name+"("):], string([]byte{10})+"func "); next >= 0 {
		return rest[:len("func "+name+"(")+next]
	}
	return rest
}

func TestLibraryContinuityAuthorityCQLContracts(t *testing.T) {
	baseline := continuityFunctionSource(t, "CommitLibraryContinuityWitness")
	for _, needle := range []string{
		"SET continuity_certified_head_commit_id = ?, continuity_contract_version = ?",
		"IF head_commit_id = ?",
		"AND deleted_at = null",
		"SerialConsistency(LibraryHeadSerialConsistency)",
	} {
		if !strings.Contains(baseline, needle) {
			t.Errorf("baseline witness is missing %q", needle)
		}
	}
	if got := strings.Count(baseline, "MapScanCAS("); got != 1 {
		t.Errorf("baseline witness MapScanCAS count=%d, want 1", got)
	}

	advance := continuityFunctionSource(t, "AdvanceLibraryCertifiedFrontier")
	for _, needle := range []string{
		"SET head_commit_id = ?, continuity_certified_head_commit_id = ?, continuity_contract_version = ?",
		"IF head_commit_id = ?",
		"AND continuity_certified_head_commit_id = ?",
		"AND continuity_contract_version = ?",
		"AND deleted_at = null",
		"SerialConsistency(LibraryHeadSerialConsistency)",
	} {
		if !strings.Contains(advance, needle) {
			t.Errorf("atomic frontier advance is missing %q", needle)
		}
	}
	if got := strings.Count(advance, "MapScanCAS("); got != 1 {
		t.Errorf("atomic frontier advance MapScanCAS count=%d, want 1", got)
	}
}

func TestContinuityWitnessValidity(t *testing.T) {
	tests := []struct {
		name    string
		state   LibraryState
		version string
		want    bool
	}{
		{
			name:    "current head under V1",
			state:   LibraryState{HeadCommitID: "H", ContinuityCertifiedHeadCommitID: continuityString("H"), ContinuityContractVersion: continuityString(SupportedContinuityContractVersion)},
			version: SupportedContinuityContractVersion,
			want:    true,
		},
		{
			name:    "soft-deleted library",
			state:   LibraryState{HeadCommitID: "H", ContinuityCertifiedHeadCommitID: continuityString("H"), ContinuityContractVersion: continuityString(SupportedContinuityContractVersion), DeletedAt: continuityTime(time.Unix(1, 0))},
			version: SupportedContinuityContractVersion,
			want:    false,
		},
		{name: "missing witness", state: LibraryState{HeadCommitID: "H"}, version: SupportedContinuityContractVersion},
		{name: "stale witness", state: LibraryState{HeadCommitID: "H2", ContinuityCertifiedHeadCommitID: continuityString("H1"), ContinuityContractVersion: continuityString(SupportedContinuityContractVersion)}, version: SupportedContinuityContractVersion},
		{name: "wrong contract", state: LibraryState{HeadCommitID: "H", ContinuityCertifiedHeadCommitID: continuityString("H"), ContinuityContractVersion: continuityString("V0")}, version: SupportedContinuityContractVersion},
		{name: "unsupported requested contract", state: LibraryState{HeadCommitID: "H", ContinuityCertifiedHeadCommitID: continuityString("H"), ContinuityContractVersion: continuityString("V0")}, version: "V0"},
		{name: "empty head", state: LibraryState{ContinuityCertifiedHeadCommitID: continuityString(""), ContinuityContractVersion: continuityString(SupportedContinuityContractVersion)}, version: SupportedContinuityContractVersion},
		{name: "empty requested contract", state: LibraryState{HeadCommitID: "H", ContinuityCertifiedHeadCommitID: continuityString("H"), ContinuityContractVersion: continuityString(SupportedContinuityContractVersion)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.state.ContinuityWitnessValidFor(test.version); got != test.want {
				t.Fatalf("ContinuityWitnessValidFor() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestClassifyLibraryContinuityCASFailsClosedOnErrors(t *testing.T) {
	tests := []struct {
		name    string
		applied bool
		err     error
		want    LibraryContinuityCASOutcome
	}{
		{name: "applied", applied: true, want: LibraryContinuityCASApplied},
		{name: "predicate miss", want: LibraryContinuityCASNotApplied},
		{name: "timeout", err: context.DeadlineExceeded, want: LibraryContinuityCASUnknown},
		{name: "ambiguous CAS", err: &gocql.RequestErrCASWriteUnknown{}, want: LibraryContinuityCASUnknown},
		{name: "applied plus error remains unknown", applied: true, err: errors.New("transport failure"), want: LibraryContinuityCASUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyLibraryContinuityCAS(test.applied, test.err); got != test.want {
				t.Fatalf("classifyLibraryContinuityCAS() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestValidateLibraryContinuityInput(t *testing.T) {
	tests := []struct {
		name string
		org  string
		lib  string
		head string
		ver  string
		want error
	}{
		{name: "valid", org: "org", lib: "lib", head: "H", ver: SupportedContinuityContractVersion},
		{name: "missing org", lib: "lib", head: "H", ver: SupportedContinuityContractVersion, want: ErrInvalidLibraryContinuityInput},
		{name: "missing head", org: "org", lib: "lib", ver: SupportedContinuityContractVersion, want: ErrInvalidLibraryContinuityInput},
		{name: "missing version", org: "org", lib: "lib", head: "H", want: ErrInvalidLibraryContinuityInput},
		{name: "unsupported version", org: "org", lib: "lib", head: "H", ver: "V0", want: ErrUnsupportedContinuityContract},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateLibraryContinuityInput(test.org, test.lib, test.head, test.ver)
			if test.want == nil {
				if err != nil {
					t.Fatalf("validateLibraryContinuityInput() error = %v", err)
				}
				return
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("validateLibraryContinuityInput() error = %v, want %v", err, test.want)
			}
		})
	}
}
