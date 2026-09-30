package db

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

const libraryLifecyclePinReason = "library lifecycle fence no longer pins global SERIAL"

// Every lifecycle LWT and settlement read must run in the global SERIAL Paxos
// domain, whatever the session default (database.serial_consistency).
func TestLibraryLifecycleFencePinsGlobalSerial(t *testing.T) {
	if DeletedLibraryMarkerSerialConsistency != gocql.Serial || LibraryHeadSerialConsistency != gocql.Serial {
		t.Fatalf("%s: DeletedLibraryMarkerSerialConsistency = %v, LibraryHeadSerialConsistency = %v", libraryLifecyclePinReason, DeletedLibraryMarkerSerialConsistency, LibraryHeadSerialConsistency)
	}

	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal(libraryLifecyclePinReason + ": runtime.Caller failed")
	}
	sourcePath := filepath.Join(filepath.Dir(testFile), "library_lifecycle.go")
	file, err := parser.ParseFile(token.NewFileSet(), sourcePath, nil, 0)
	if err != nil {
		t.Fatalf("%s: parse %s: %v", libraryLifecyclePinReason, sourcePath, err)
	}
	// Each helper pins its LWT (SerialConsistency) and its settlement read
	// (Consistency) to the named global-SERIAL domain of the table it touches.
	for _, helper := range []struct {
		name, domain string
		lwts, serial int
	}{
		{name: "DeleteTrashedLibraryGeneration", domain: "LibraryHeadSerialConsistency", lwts: 1},
		{name: "RestoreTrashedLibraryGeneration", domain: "LibraryHeadSerialConsistency", lwts: 1},
		{name: "readLibraryDeletedAtSerial", domain: "LibraryHeadSerialConsistency", serial: 1},
		{name: "ClearSoftDeleteMarkerGeneration", domain: "DeletedLibraryMarkerSerialConsistency", lwts: 1, serial: 1},
	} {
		var decl *ast.FuncDecl
		for _, d := range file.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == helper.name {
				decl = fn
			}
		}
		if decl == nil {
			t.Fatalf("%s: %s not found", libraryLifecyclePinReason, helper.name)
		}
		queries, lwts, serial := 0, 0, 0
		ast.Inspect(decl.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			method, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pinned := len(call.Args) == 1 && isIdent(call.Args[0], helper.domain)
			switch method.Sel.Name {
			case "Query":
				queries++
			case "SerialConsistency":
				if pinned {
					lwts++
				}
			case "Consistency":
				if pinned {
					serial++
				}
			}
			return true
		})
		if queries != helper.lwts+helper.serial || lwts != helper.lwts || serial != helper.serial {
			t.Errorf("%s: %s issues %d queries, %d LWTs pinned and %d SERIAL reads, want %d LWTs and %d SERIAL reads",
				libraryLifecyclePinReason, helper.name, queries, lwts, serial, helper.lwts, helper.serial)
		}
	}
}

func isIdent(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

func TestIsAmbiguousLibraryLifecycleCASError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"cas write unknown", &gocql.RequestErrCASWriteUnknown{}, true},
		{"no response", gocql.ErrTimeoutNoResponse, true},
		{"connection closed", gocql.ErrConnectionClosed, true},
		{"v4 CAS write timeout", &gocql.RequestErrWriteTimeout{WriteType: "CAS"}, true},
		{"v4 CAS write failure", &gocql.RequestErrWriteFailure{WriteType: "CAS"}, true},
		{"simple write timeout", &gocql.RequestErrWriteTimeout{WriteType: "SIMPLE"}, false},
		{"unavailable", &gocql.RequestErrUnavailable{}, false},
		{"other", errors.New("boom"), false},
	} {
		if got := isAmbiguousLibraryLifecycleCASError(tc.err); got != tc.want {
			t.Errorf("%s: ambiguous = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Settlement of an ambiguous outcome: a legitimate continuation (the generation
// is already gone for a delete, already active for a restore) proceeds; a row
// still at the precondition is unknown and fails closed; anything else is a
// changed generation.
func TestLibraryLifecycleSettlement(t *testing.T) {
	gen := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	newer := gen.Add(time.Minute)
	casErr := &gocql.RequestErrCASWriteUnknown{}
	readErr := errors.New("read failed")

	check := func(t *testing.T, name string, got LibraryLifecycleOutcome, err error, want LibraryLifecycleOutcome, wantUnknown bool) {
		t.Helper()
		if got != want {
			t.Errorf("%s: outcome = %v, want %v", name, got, want)
		}
		if errors.Is(err, ErrLibraryLifecycleOutcomeUnknown) != wantUnknown {
			t.Errorf("%s: err = %v, want unknown=%v", name, err, wantUnknown)
		}
		if !wantUnknown && err != nil {
			t.Errorf("%s: unexpected error %v", name, err)
		}
	}

	t.Run("delete", func(t *testing.T) {
		got, err := settleTrashedLibraryDelete("l", gen, casErr, false, time.Time{}, nil)
		check(t, "row gone", got, err, LibraryLifecycleApplied, false)
		got, err = settleTrashedLibraryDelete("l", gen, casErr, true, gen, nil)
		check(t, "row still at generation", got, err, LibraryLifecycleGenerationChanged, true)
		got, err = settleTrashedLibraryDelete("l", gen, casErr, true, time.Time{}, nil)
		check(t, "row restored", got, err, LibraryLifecycleGenerationChanged, false)
		got, err = settleTrashedLibraryDelete("l", gen, casErr, true, newer, nil)
		check(t, "row trashed again", got, err, LibraryLifecycleGenerationChanged, false)
		got, err = settleTrashedLibraryDelete("l", gen, casErr, false, time.Time{}, readErr)
		check(t, "read failed", got, err, LibraryLifecycleGenerationChanged, true)
	})

	t.Run("restore", func(t *testing.T) {
		got, err := settleTrashedLibraryRestore("l", gen, casErr, true, time.Time{}, nil)
		check(t, "row active", got, err, LibraryLifecycleApplied, false)
		got, err = settleTrashedLibraryRestore("l", gen, casErr, true, gen, nil)
		check(t, "row still at generation", got, err, LibraryLifecycleGenerationChanged, true)
		got, err = settleTrashedLibraryRestore("l", gen, casErr, false, time.Time{}, nil)
		check(t, "row gone", got, err, LibraryLifecycleTargetAbsent, false)
		got, err = settleTrashedLibraryRestore("l", gen, casErr, true, newer, nil)
		check(t, "row trashed again", got, err, LibraryLifecycleGenerationChanged, false)
		got, err = settleTrashedLibraryRestore("l", gen, casErr, true, time.Time{}, readErr)
		check(t, "read failed", got, err, LibraryLifecycleGenerationChanged, true)
	})

	t.Run("marker", func(t *testing.T) {
		got, err := settleSoftDeleteMarkerClear("l", gen, casErr, false, time.Time{}, time.Time{}, nil)
		check(t, "marker gone", got, err, LibraryLifecycleTargetAbsent, false)
		got, err = settleSoftDeleteMarkerClear("l", gen, casErr, true, gen, time.Time{}, nil)
		check(t, "soft-delete marker still there", got, err, LibraryLifecycleGenerationChanged, true)
		got, err = settleSoftDeleteMarkerClear("l", gen, casErr, true, gen, newer, nil)
		check(t, "permanent-delete marker", got, err, LibraryLifecycleGenerationChanged, false)
		got, err = settleSoftDeleteMarkerClear("l", gen, casErr, true, newer, time.Time{}, nil)
		check(t, "newer generation marker", got, err, LibraryLifecycleGenerationChanged, false)
		got, err = settleSoftDeleteMarkerClear("l", gen, casErr, false, time.Time{}, time.Time{}, readErr)
		check(t, "read failed", got, err, LibraryLifecycleGenerationChanged, true)
	})
}
