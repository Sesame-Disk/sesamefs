package db

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

const libraryLifecyclePinReason = "library lifecycle fence no longer pins global SERIAL"

// Every lifecycle LWT and settlement read must run in the global SERIAL Paxos
// domain, whatever the session default (database.serial_consistency).
func TestLibraryLifecycleFencePinsGlobalSerial(t *testing.T) {
	if LibraryHeadSerialConsistency != gocql.Serial {
		t.Fatalf("%s: LibraryHeadSerialConsistency = %v", libraryLifecyclePinReason, LibraryHeadSerialConsistency)
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
		{name: "DeleteTrashedLibraryGenerationWithIntent", domain: "LibraryHeadSerialConsistency", lwts: 1},
		{name: "RestoreTrashedLibraryGenerationWithIntent", domain: "LibraryHeadSerialConsistency", lwts: 1},
		{name: "FenceLibraryLifecycleAttempt", domain: "LibraryHeadSerialConsistency", lwts: 2},
		{name: "ReadLibraryLifecycleSerial", domain: "LibraryHeadSerialConsistency", serial: 1},
		{name: "readCanonicalLibraryRowSerial", domain: "LibraryHeadSerialConsistency", serial: 1},
		{name: "SoftDeleteLibraryGenerationWithIntent", domain: "LibraryHeadSerialConsistency", lwts: 1},
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

	trashed := LibraryLifecycleState{Present: true, DeletedAt: gen, LifecycleAt: gen}
	fenced := LibraryLifecycleState{Present: true, DeletedAt: gen, LifecycleAt: gen.Add(time.Millisecond)}

	t.Run("delete", func(t *testing.T) {
		got, err := settleTrashedLibraryDelete("l", gen, trashed, casErr, LibraryLifecycleState{}, nil)
		check(t, "row gone", got, err, LibraryLifecycleApplied, false)
		got, err = settleTrashedLibraryDelete("l", gen, trashed, casErr, trashed, nil)
		check(t, "row still at precondition", got, err, LibraryLifecycleGenerationChanged, true)
		got, err = settleTrashedLibraryDelete("l", gen, trashed, casErr, fenced, nil)
		check(t, "same generation, clock moved (fenced)", got, err, LibraryLifecycleGenerationChanged, false)
		got, err = settleTrashedLibraryDelete("l", gen, trashed, casErr, LibraryLifecycleState{Present: true, LifecycleAt: newer}, nil)
		check(t, "row restored", got, err, LibraryLifecycleGenerationChanged, false)
		got, err = settleTrashedLibraryDelete("l", gen, trashed, casErr, LibraryLifecycleState{Present: true, DeletedAt: newer, LifecycleAt: newer}, nil)
		check(t, "row trashed again", got, err, LibraryLifecycleGenerationChanged, false)
		got, err = settleTrashedLibraryDelete("l", gen, trashed, casErr, LibraryLifecycleState{}, readErr)
		check(t, "read failed", got, err, LibraryLifecycleGenerationChanged, true)
	})

	t.Run("restore", func(t *testing.T) {
		restored := newer
		active := func(at time.Time) LibraryLifecycleState {
			return LibraryLifecycleState{Present: true, LifecycleAt: at}
		}
		got, err := settleTrashedLibraryRestore("l", gen, restored, trashed, casErr, active(restored), nil)
		check(t, "row active at the restore's lifecycle value", got, err, LibraryLifecycleApplied, false)
		got, err = settleTrashedLibraryRestore("l", gen, restored, trashed, casErr, trashed, nil)
		check(t, "row still at precondition", got, err, LibraryLifecycleGenerationChanged, true)
		got, err = settleTrashedLibraryRestore("l", gen, restored, trashed, casErr, fenced, nil)
		check(t, "same generation, clock moved (fenced)", got, err, LibraryLifecycleGenerationChanged, false)
		got, err = settleTrashedLibraryRestore("l", gen, restored, trashed, casErr, LibraryLifecycleState{}, nil)
		check(t, "row gone", got, err, LibraryLifecycleTargetAbsent, false)
		got, err = settleTrashedLibraryRestore("l", gen, restored, trashed, casErr, active(restored.Add(time.Millisecond)), nil)
		check(t, "restored by another owner", got, err, LibraryLifecycleGenerationChanged, false)
		got, err = settleTrashedLibraryRestore("l", gen, restored, trashed, casErr, LibraryLifecycleState{}, readErr)
		check(t, "read failed", got, err, LibraryLifecycleGenerationChanged, true)
	})

	t.Run("soft delete", func(t *testing.T) {
		before := LibraryLifecycleState{Present: true, LifecycleAt: gen}
		got, err := settleLibrarySoftDelete("l", newer, before, casErr, LibraryLifecycleState{Present: true, DeletedAt: newer, LifecycleAt: newer}, nil)
		check(t, "row at the new generation", got, err, LibraryLifecycleApplied, false)
		got, err = settleLibrarySoftDelete("l", newer, before, casErr, before, nil)
		check(t, "row unchanged", got, err, LibraryLifecycleGenerationChanged, true)
		got, err = settleLibrarySoftDelete("l", newer, before, casErr, LibraryLifecycleState{}, nil)
		check(t, "row gone", got, err, LibraryLifecycleTargetAbsent, false)
		other := newer.Add(time.Millisecond)
		got, err = settleLibrarySoftDelete("l", newer, before, casErr, LibraryLifecycleState{Present: true, DeletedAt: other, LifecycleAt: other}, nil)
		check(t, "row trashed by another request", got, err, LibraryLifecycleGenerationChanged, false)
		got, err = settleLibrarySoftDelete("l", newer, before, casErr, LibraryLifecycleState{}, readErr)
		check(t, "read failed", got, err, LibraryLifecycleGenerationChanged, true)
	})
}

// The lifecycle clock is strictly increasing even when the node's clock is
// behind or has not moved, so a trash generation is unique per library.
func TestNextLibraryLifecycleAt(t *testing.T) {
	base := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	if got := NextLibraryLifecycleAt(time.Time{}, base.Add(123*time.Microsecond)); !got.Equal(base) {
		t.Fatalf("first value = %s, want %s (millisecond precision)", got, base)
	}
	if got := NextLibraryLifecycleAt(base, base); !got.Equal(base.Add(time.Millisecond)) {
		t.Fatalf("same millisecond = %s, want previous + 1ms", got)
	}
	if got := NextLibraryLifecycleAt(base, base.Add(-time.Hour)); !got.Equal(base.Add(time.Millisecond)) {
		t.Fatalf("clock behind = %s, want previous + 1ms", got)
	}
	if got := NextLibraryLifecycleAt(base, base.Add(time.Second)); !got.Equal(base.Add(time.Second)) {
		t.Fatalf("clock ahead = %s, want now", got)
	}
}

// Recovery takes the canonical row at SERIAL and the trash listing at
// EACH_QUORUM, so a state another datacenter acknowledged is never read as
// "nothing to repair".
func TestLibraryLifecycleRepairReadsAreStrong(t *testing.T) {
	const reason = "lifecycle recovery no longer reads at a strength that sees other datacenters"
	raw, err := os.ReadFile("library_lifecycle.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	i := strings.Index(src, "func RepairLibraryLifecycleDerivedState(")
	j := strings.Index(src[i+1:], "\nfunc ")
	repair := src[i : i+1+j]
	if !strings.Contains(repair, "readCanonicalLibraryRowSerial(") || !strings.Contains(repair, "ListDeletedAdminLibraryRowsByOrgEachQuorum(") || strings.Contains(repair, "ListDeletedAdminLibraryRowsByOrg(") {
		t.Errorf("%s: RepairLibraryLifecycleDerivedState must read the canonical row at SERIAL and the trash listing at EACH_QUORUM", reason)
	}
	read, err := os.ReadFile("admin_library_read_models.go")
	if err != nil {
		t.Fatal(err)
	}
	k := strings.Index(string(read), "func ListDeletedAdminLibraryRowsByOrgEachQuorum(")
	if k < 0 || !strings.Contains(string(read)[k:k+600], ".Consistency(gocql.EachQuorum)") {
		t.Errorf("%s: ListDeletedAdminLibraryRowsByOrgEachQuorum is not EACH_QUORUM", reason)
	}
}

// An attempt can apply only while the canonical row still matches its LWT
// condition, lifecycle clock included: a fence (or any later transition) that
// moves the clock retires it.
func TestLibraryLifecyclePendingCanStillApply(t *testing.T) {
	l0 := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	d := l0.Add(time.Second)
	active := LibraryLifecycleState{Present: true, LifecycleAt: l0}
	trashed := LibraryLifecycleState{Present: true, DeletedAt: d, LifecycleAt: d}
	soft := LibraryLifecyclePending{Operation: LibraryLifecycleOpSoftDelete, PrevLifecycleAt: l0}
	restore := LibraryLifecyclePending{Operation: LibraryLifecycleOpRestore, PrevDeletedAt: d, PrevLifecycleAt: d}
	purge := LibraryLifecyclePending{Operation: LibraryLifecycleOpPermanentDelete, PrevDeletedAt: d, PrevLifecycleAt: d}
	moved := func(s LibraryLifecycleState) LibraryLifecycleState {
		s.LifecycleAt = s.LifecycleAt.Add(time.Millisecond)
		return s
	}
	for _, tc := range []struct {
		name    string
		pending LibraryLifecyclePending
		state   LibraryLifecycleState
		want    bool
	}{
		{"soft delete at precondition", soft, active, true},
		{"soft delete after fence", soft, moved(active), false},
		{"soft delete after it applied", soft, trashed, false},
		{"soft delete of a missing row", soft, LibraryLifecycleState{}, false},
		{"restore at precondition", restore, trashed, true},
		{"restore after fence", restore, moved(trashed), false},
		{"restore after it applied", restore, LibraryLifecycleState{Present: true, LifecycleAt: d.Add(time.Second)}, false},
		{"permanent delete at precondition", purge, trashed, true},
		{"permanent delete after fence", purge, moved(trashed), false},
		{"permanent delete after it applied", purge, LibraryLifecycleState{}, false},
		{"legacy trashed row without clock", LibraryLifecyclePending{Operation: LibraryLifecycleOpRestore, PrevDeletedAt: d}, LibraryLifecycleState{Present: true, DeletedAt: d}, true},
	} {
		if got := tc.pending.CanStillApply(tc.state); got != tc.want {
			t.Errorf("%s: CanStillApply = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestLibraryLifecyclePendingAbandoned(t *testing.T) {
	recorded := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	p := LibraryLifecyclePending{RecordedAt: recorded}
	if p.Abandoned(recorded.Add(LibraryLifecycleAttemptAbandonAfter - time.Second)) {
		t.Error("a fresh attempt is reported abandoned")
	}
	if !p.Abandoned(recorded.Add(LibraryLifecycleAttemptAbandonAfter)) {
		t.Error("an attempt past the threshold is not reported abandoned")
	}
	if (LibraryLifecyclePending{}).Abandoned(recorded) {
		t.Error("an attempt without recorded_at is reported abandoned")
	}
}

// The ordinary read model is confirmed against every projected canonical
// column, not only the lifecycle clock: an owner transfer, a rename or a size
// change moves no lifecycle value.
func TestLibraryProjectionCurrent(t *testing.T) {
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	row := AdminLibraryProjectionRow{OwnerID: "a", Name: "n", StorageClass: "hot", SizeBytes: 1, FileCount: 1, CreatedAt: at, UpdatedAt: at}
	state := LibraryLifecycleState{Present: true, LifecycleAt: at}
	if !libraryProjectionCurrent(row, state, row, state) {
		t.Fatal("identical snapshots are not current")
	}
	if !libraryProjectionCurrent(row, state, row, LibraryLifecycleState{Present: true, LifecycleAt: at.Add(time.Millisecond)}) {
		t.Error("a lifecycle clock move alone (a fence) changes no projected column")
	}
	for name, mutate := range map[string]func(*AdminLibraryProjectionRow, *LibraryLifecycleState){
		"owner":     func(r *AdminLibraryProjectionRow, _ *LibraryLifecycleState) { r.OwnerID = "b" },
		"name":      func(r *AdminLibraryProjectionRow, _ *LibraryLifecycleState) { r.Name = "m" },
		"size":      func(r *AdminLibraryProjectionRow, _ *LibraryLifecycleState) { r.SizeBytes = 2 },
		"files":     func(r *AdminLibraryProjectionRow, _ *LibraryLifecycleState) { r.FileCount = 2 },
		"updated":   func(r *AdminLibraryProjectionRow, _ *LibraryLifecycleState) { r.UpdatedAt = at.Add(time.Second) },
		"class":     func(r *AdminLibraryProjectionRow, _ *LibraryLifecycleState) { r.StorageClass = "cold" },
		"encrypted": func(r *AdminLibraryProjectionRow, _ *LibraryLifecycleState) { r.Encrypted = true },
		"trashed":   func(_ *AdminLibraryProjectionRow, s *LibraryLifecycleState) { s.DeletedAt = at },
		"gone":      func(_ *AdminLibraryProjectionRow, s *LibraryLifecycleState) { *s = LibraryLifecycleState{} },
	} {
		next, nextState := row, state
		mutate(&next, &nextState)
		if libraryProjectionCurrent(row, state, next, nextState) {
			t.Errorf("a %s change is reported current", name)
		}
	}
}

// Continuations are written, deleted and discovered at global QUORUM, so a
// row acknowledged in one datacenter is seen from every other one and an
// unreachable quorum fails discovery instead of reading as empty.
func TestLibraryLifecyclePendingIsGlobalQuorum(t *testing.T) {
	const reason = "library lifecycle continuation no longer read/written at global QUORUM"
	if LibraryLifecyclePendingConsistency != gocql.Quorum {
		t.Fatalf("%s: LibraryLifecyclePendingConsistency = %v", reason, LibraryLifecyclePendingConsistency)
	}
	raw, err := os.ReadFile("library_lifecycle_pending.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	for _, name := range []string{"InsertLibraryLifecyclePending", "DeleteLibraryLifecyclePending", "ListLibraryLifecyclePending"} {
		i := strings.Index(src, "func "+name+"(")
		if i < 0 {
			t.Fatalf("%s: %s not found", reason, name)
		}
		body := src[i:]
		if j := strings.Index(body[1:], "\nfunc "); j >= 0 {
			body = body[:j+1]
		}
		if strings.Count(body, ".Query(") != 1 || !strings.Contains(body, "Consistency(LibraryLifecyclePendingConsistency)") {
			t.Errorf("%s: %s does not pin LibraryLifecyclePendingConsistency", reason, name)
		}
	}
}

func libraryLifecycleFuncBody(t *testing.T, file, name string) string {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	i := strings.Index(src, "func "+name+"(")
	if i < 0 {
		t.Fatalf("%s not found in %s", name, file)
	}
	body := src[i:]
	if j := strings.Index(body, "\n}\n"); j >= 0 {
		body = body[:j+3]
	}
	return body
}

// Round 6: the read-model publication confirms the ordinary columns at
// EACH_QUORUM (an owner transfer acknowledged at LOCAL_QUORUM in another
// datacenter is seen); the trash reconciliation deletes on a generation
// mismatch only after the lifecycle authority confirms it; continuation rows
// are written and retired with explicit timestamps; the ordinary read-model
// columns never carry deleted_at, which is stamped with lifecycle values.
func TestLibraryLifecycleRound6Pins(t *testing.T) {
	const reason = "library lifecycle round-6 property no longer pinned"
	overlay := libraryLifecycleFuncBody(t, "library_lifecycle.go", "overlayOrdinaryColumnsEachQuorum")
	if !strings.Contains(overlay, ".Consistency(gocql.EachQuorum)") {
		t.Errorf("%s: overlayOrdinaryColumnsEachQuorum does not read at EACH_QUORUM", reason)
	}
	if !strings.Contains(libraryLifecycleFuncBody(t, "library_lifecycle.go", "publishLibraryReadModel"), "overlayOrdinaryColumnsEachQuorum(") ||
		!strings.Contains(libraryLifecycleFuncBody(t, "library_lifecycle.go", "publishLibraryReadModel"), "readLibraryProjectionSnapshot(") ||
		!strings.Contains(libraryLifecycleFuncBody(t, "library_lifecycle.go", "readLibraryProjectionSnapshot"), "overlayOrdinaryColumnsEachQuorum(") {
		t.Errorf("%s: publishLibraryReadModel does not snapshot and confirm the ordinary columns at EACH_QUORUM", reason)
	}
	if !strings.Contains(libraryLifecycleFuncBody(t, "admin_library_read_models.go", "ReconcileDeletedAdminLibraryRowsByOrg"), "ReadLibraryLifecycleSerial(") {
		t.Errorf("%s: ReconcileDeletedAdminLibraryRowsByOrg deletes without the lifecycle authority", reason)
	}
	for _, name := range []string{"InsertLibraryLifecyclePending", "DeleteLibraryLifecyclePending"} {
		if !strings.Contains(libraryLifecycleFuncBody(t, "library_lifecycle_pending.go", name), "USING TIMESTAMP ?") {
			t.Errorf("%s: %s relies on an implicit client timestamp", reason, name)
		}
	}
	if strings.Contains(libraryLifecycleFuncBody(t, "admin_library_read_models.go", "AddUpsertAdminLibraryOrdinaryRowsQuery"), "deleted_at") {
		t.Errorf("%s: the ordinary read-model upsert writes deleted_at with a client timestamp", reason)
	}
}

func TestLibraryLifecyclePendingRetireTimestamp(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if got := libraryLifecyclePendingRetireTimestamp(LibraryLifecyclePending{WriteTimestamp: now.Add(time.Hour).UnixMicro()}, now); got != now.Add(time.Hour).UnixMicro()+1 {
		t.Errorf("retirement after a fast insert = %d, want insert + 1", got)
	}
	if got := libraryLifecyclePendingRetireTimestamp(LibraryLifecyclePending{WriteTimestamp: now.Add(-time.Hour).UnixMicro()}, now); got != now.UnixMicro() {
		t.Errorf("retirement after an old insert = %d, want now", got)
	}
}
