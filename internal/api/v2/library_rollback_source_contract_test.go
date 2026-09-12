package v2

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLibraryRollbackMarkerIsPersistedBeforeAuthority(t *testing.T) {
	src := v2FunctionSource(t, "library_rollback.go", "rollbackNewLibrary")
	persistAt := strings.Index(src, "persistLibraryRollbackPendingFn")
	authorityAt := strings.Index(src, "applyAuthorizedLibraryRollbackCleanup")
	if persistAt < 0 || authorityAt < 0 {
		t.Fatalf("rollbackNewLibrary must persist the marker then apply the HEAD LWT; source=%s", src)
	}
	if persistAt > authorityAt {
		t.Fatal("REGRESSION: rollbackNewLibrary must persist library_rollback_pending before the authority LWT")
	}
	if strings.Contains(src, "deleteUnpublishedLibraryRow") {
		t.Fatal("rollbackNewLibrary must not call deleteUnpublishedLibraryRow before the marker is durable; authority stays inside applyAuthorizedLibraryRollbackCleanup")
	}
}

func TestLibraryRollbackCleanupRequiresHeadLWT(t *testing.T) {
	src := v2FunctionSource(t, "library_rollback.go", "runAuthorizedLibraryRollbackCleanup")
	lwtAt := strings.Index(src, "deleteUnpublishedLibraryRow")
	cleanupAt := strings.Index(src, "cleanupRolledBackLibraryDerivedStateFn")
	if lwtAt < 0 || cleanupAt < 0 {
		t.Fatalf("runAuthorizedLibraryRollbackCleanup must keep the HEAD LWT before derived cleanup; source=%s", src)
	}
	if lwtAt > cleanupAt {
		t.Fatal("REGRESSION: derived cleanup must not run before deleteUnpublishedLibraryRow")
	}
	if !strings.Contains(src, "IF head_commit_id = null") && !strings.Contains(src, "deleteUnpublishedLibraryRow") {
		t.Fatal("cleanup authority must remain the HEAD LWT")
	}
}

func TestLibraryRollbackMarkerDeletedOnlyAfterCleanup(t *testing.T) {
	src := v2FunctionSource(t, "library_rollback.go", "applyAuthorizedLibraryRollbackCleanup")
	cleanupAt := strings.Index(src, "runAuthorizedLibraryRollbackCleanup")
	// The success-path marker delete is the last deleteLibraryRollbackPendingFn
	// in the function; the earlier one settles a refused-HEAD marker without cleanup.
	firstDelete := strings.Index(src, "deleteLibraryRollbackPendingFn")
	lastDelete := strings.LastIndex(src, "deleteLibraryRollbackPendingFn")
	if cleanupAt < 0 || firstDelete < 0 || lastDelete < 0 {
		t.Fatalf("applyAuthorizedLibraryRollbackCleanup must settle the marker; source=%s", src)
	}
	if lastDelete < cleanupAt {
		t.Fatal("REGRESSION: marker must not be deleted before authorized cleanup has been attempted")
	}
}

func TestLibraryRollbackRecoveryReusesAuthorityGate(t *testing.T) {
	src := v2FunctionSource(t, "library_rollback_reaper.go", "recoverPendingLibraryRollback")
	if !strings.Contains(src, "applyAuthorizedLibraryRollbackCleanup") {
		t.Fatal("recovery must reuse applyAuthorizedLibraryRollbackCleanup, not call derived cleanup blindly")
	}
	if strings.Contains(src, "cleanupRolledBackLibraryDerivedState(") || strings.Contains(src, "cleanupRolledBackLibraryDerivedStateFn") {
		t.Fatal("recovery must not invoke derived cleanup except through the authority helper")
	}
}

func TestLibraryRollbackRecoveryIsBoundedAndEnumerable(t *testing.T) {
	src := v2FunctionSource(t, "library_rollback_reaper.go", "recoverPendingLibraryRollbacksFrom")
	for _, needle := range []string{
		"db.GCDiscoveryBucketCount",
		"libraryRollbackRecoveryPageSize",
		"libraryRollbackRecoveryMaxPerSweep",
		"listLibraryRollbackPendingAfterFn",
		"startBucket",
	} {
		if !strings.Contains(src, needle) {
			t.Fatalf("recoverPendingLibraryRollbacksFrom must keep bounded fair pagination (%s missing)", needle)
		}
	}
}

func TestLibraryRollbackRecoveryAdvancesCursorPastFailures(t *testing.T) {
	src := v2FunctionSource(t, "library_rollback_reaper.go", "recoverPendingLibraryRollbacksFrom")
	processedAt := strings.Index(src, "processed++")
	recoverAt := strings.Index(src, "recoverPendingLibraryRollbackFn")
	cursorAt := strings.Index(src, "state.after[bucket] = after")
	if processedAt < 0 || recoverAt < 0 || cursorAt < 0 {
		t.Fatalf("sweep must count work, recover, and advance the clustering cursor; source=%s", src)
	}
	if cursorAt < recoverAt {
		t.Fatal("REGRESSION: clustering cursor must advance after recoverPendingLibraryRollbackFn so a persistent failure cannot pin the next sweep to the same prefix")
	}
}

func TestLibraryRollbackReaperNilDatabaseIsNoOp(t *testing.T) {
	if got := StartLibraryRollbackReaper(nil); got != nil {
		t.Fatal("StartLibraryRollbackReaper(nil) must be a no-op")
	}
	var r *LibraryRollbackReaper
	r.Stop()
}

func v2FunctionSource(t *testing.T, file, function string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	full := filepath.Join(filepath.Dir(thisFile), file)
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, full, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	src, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	var target ast.Node
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != function {
			continue
		}
		target = fn
		break
	}
	if target == nil {
		t.Fatalf("function %s not found in %s", function, file)
	}
	start := int(target.Pos()) - int(parsed.Pos())
	end := int(target.End()) - int(parsed.Pos())
	if start < 0 || end > len(src) || start >= end {
		t.Fatalf("%s source range out of bounds in %s", function, file)
	}
	return string(src[start:end])
}
