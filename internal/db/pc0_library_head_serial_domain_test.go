package db

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// pc0HeadSerialDomainOp is one production MapScanCAS that must participate in
// the canonical libraries.head_commit_id Paxos domain (global SERIAL via
// LibraryHeadSerialConsistency). The list is derived from the PC-0 column
// writers (cas shape) plus the DELETE IF guards so a new inventoried CAS
// writer cannot omit the pin.
type pc0HeadSerialDomainOp struct {
	path string
	decl string
}

func pc0HeadSerialDomainOps() []pc0HeadSerialDomainOp {
	var ops []pc0HeadSerialDomainOp
	for _, writer := range pc0ExpectedHeadColumnWriters {
		if writer.shape != pc0HeadWriteCAS {
			continue
		}
		ops = append(ops, pc0HeadSerialDomainOp{path: writer.path, decl: writer.decl})
	}
	for _, guard := range pc0ExpectedHeadAuthorityGuards {
		ops = append(ops, pc0HeadSerialDomainOp{path: guard.path, decl: guard.decl})
	}
	return ops
}

var pc0QueryCASTerminals = map[string]bool{
	"ScanCAS":           true,
	"MapScanCAS":        true,
	"ScanCASContext":    true,
	"MapScanCASContext": true,
}

// TestPC0HeadAuthorityDeleteGuardsAreInventoried closes the DELETE-IF blind
// spot of TestPC0RawHeadColumnWritersAreInventoried: a competing DELETE of
// the libraries relation (whole-row LWT, cell-delete of head_commit_id, or
// any DELETE whose IF names head_commit_id) takes HEAD authority without
// necessarily writing the column, so it cannot hide as "not a writer".
// Discovery walks Query/Bind CQL entry points (inline
// literals, const/ident, and string concatenation), including package-level
// `var name = func(...)` seams. An unresolvable first argument fails closed
// unless it is in pc0AllowedUnresolvedHeadQueries.
func TestPC0HeadAuthorityDeleteGuardsAreInventoried(t *testing.T) {
	hits, unresolved := pc0HeadAuthorityDeleteFromQueries(t, "internal", "cmd")

	expected := map[string]pc0HeadColumnWriter{}
	for _, guard := range pc0ExpectedHeadAuthorityGuards {
		expected[pc0CallerKey(guard.path, guard.decl)] = guard
	}

	var unlisted []string
	for key := range hits {
		if _, listed := expected[key]; !listed {
			unlisted = append(unlisted, key)
		}
	}
	sort.Strings(unlisted)
	if len(unlisted) > 0 {
		t.Fatalf("PC0 HEAD SERIAL: unlisted DELETE FROM libraries IF head_commit_id guard %v; every competing HEAD-authority LWT must be inventoried in pc0ExpectedHeadAuthorityGuards", unlisted)
	}

	var missing []string
	for key := range expected {
		if _, found := hits[key]; !found {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("PC0 HEAD SERIAL: inventoried HEAD-authority DELETE guards no longer found: %v", missing)
	}

	pc0RequireUnresolvedHeadQueriesAllowed(t, unresolved)
}

// pc0AllowedUnresolvedHeadQueries are production Query/Bind call sites whose
// CQL is not a source-resolvable string. The HEAD inventory cannot prove they
// are not a libraries IF head_commit_id LWT, so each one must be named AND
// shape-pinned by TestPC0UnresolvedHeadQueriesStayOutOfHeadDomain. A count-only
// allowlist would stay green if UpdateLibrary kept one dynamic Query while
// changing it into a HEAD DELETE IF.
type pc0UnresolvedHeadQueryAllowance struct {
	count        int
	sprintfCount int
	reason       string
	shape        pc0UnresolvedHeadQueryShape
}

type pc0UnresolvedHeadQueryShape string

const (
	pc0UnresolvedShapeHardDeleteLock     pc0UnresolvedHeadQueryShape = "hard-delete-lock-table-param"
	pc0UnresolvedShapeUpdateLibrarySET   pc0UnresolvedHeadQueryShape = "update-library-set-prefix"
	pc0UnresolvedShapeOrgSettingSprintf  pc0UnresolvedHeadQueryShape = "org-setting-sprintf"
	pc0UnresolvedShapeOrgColumnSprintf   pc0UnresolvedHeadQueryShape = "org-column-sprintf"
	pc0UnresolvedShapeMigratorStatements pc0UnresolvedHeadQueryShape = "migrator-statements-range"
)

var pc0AllowedUnresolvedHeadQueries = map[string]pc0UnresolvedHeadQueryAllowance{
	"internal/gc/store_cassandra.go:acquireHardDeleteLock":          {count: 2, sprintfCount: 2, reason: "table name is a parameter; lock tables are gc_*_hard_delete_locks, not libraries", shape: pc0UnresolvedShapeHardDeleteLock},
	"internal/gc/store_cassandra.go:renewHardDeleteLock":            {count: 1, sprintfCount: 1, reason: "same helper family as acquireHardDeleteLock", shape: pc0UnresolvedShapeHardDeleteLock},
	"internal/gc/store_cassandra.go:releaseHardDeleteLock":          {count: 1, sprintfCount: 1, reason: "same helper family as acquireHardDeleteLock", shape: pc0UnresolvedShapeHardDeleteLock},
	"internal/api/v2/libraries.go:LibraryHandler.UpdateLibrary":     {count: 1, sprintfCount: 0, reason: "opens with literal UPDATE libraries SET and appends caller-built assignments; not a DELETE IF", shape: pc0UnresolvedShapeUpdateLibrarySET},
	"internal/api/v2/org_admin.go:OrgAdminHandler.updateOrgSetting": {count: 1, sprintfCount: 1, reason: "fmt.Sprintf into UPDATE organizations; relation fixed in the format string", shape: pc0UnresolvedShapeOrgSettingSprintf},
	"internal/api/v2/admin.go:AdminHandler.UpdateOrganization":      {count: 1, sprintfCount: 1, reason: "fmt.Sprintf into UPDATE organizations; relation fixed in the format string", shape: pc0UnresolvedShapeOrgColumnSprintf},
	"internal/db/migrator.go:Migrator.apply":                        {count: 1, sprintfCount: 0, reason: "applies checked-in DDL from migrations/*.cql; not conditional DML", shape: pc0UnresolvedShapeMigratorStatements},
}

func pc0RequireUnresolvedHeadQueriesAllowed(t *testing.T, unresolved map[string]int) {
	t.Helper()
	var extra []string
	for key, count := range unresolved {
		allowance, ok := pc0AllowedUnresolvedHeadQueries[key]
		if !ok {
			extra = append(extra, key)
			continue
		}
		if count != allowance.count {
			t.Errorf("PC0 HEAD SERIAL: unresolvable Query CQL at %s count=%d, allowlisted %d (%s)", key, count, allowance.count, allowance.reason)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		t.Fatalf("PC0 HEAD SERIAL: unresolvable Query/Bind CQL at %v; a constructed statement can hide a libraries IF head_commit_id LWT. Name it in pc0AllowedUnresolvedHeadQueries only after proving it is not a HEAD-authority DELETE", extra)
	}
	var stale []string
	for key := range pc0AllowedUnresolvedHeadQueries {
		if _, found := unresolved[key]; !found {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Fatalf("PC0 HEAD SERIAL: unused unresolved-Query allowlist entries %v", stale)
	}
}

func pc0HeadAuthorityDeleteFromQueries(t *testing.T, roots ...string) (map[string][]string, map[string]int) {
	t.Helper()
	repoRoot := r3RepositoryRoot(t)
	hits := map[string][]string{}
	unresolved := map[string]int{}
	for _, root := range roots {
		walkErr := filepath.WalkDir(filepath.Join(repoRoot, root), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			relPath, relErr := filepath.Rel(repoRoot, path)
			if relErr != nil {
				return relErr
			}
			relPath = filepath.ToSlash(relPath)
			file := r3ParseProductionFile(t, path)
			pkgBindings := pc0PackageConstStrings(file)
			for _, scope := range pc0FileQueryScopes(file) {
				key := pc0CallerKey(relPath, scope.name)
				bindings := pc0BlockStringBindings(scope.body, pkgBindings)
				pc0VisitCQLEntryPoints(scope.node, bindings, func(_ *ast.CallExpr, cql string, resolved bool) {
					if !resolved {
						unresolved[key]++
						return
					}
					if pc0CQLIsLibrariesHeadIFDelete(cql) {
						hits[key] = append(hits[key], cql)
					}
				})
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("PC0 HEAD SERIAL: walk %s: %v", root, walkErr)
		}
	}
	return hits, unresolved
}

func pc0FindQueryScope(t *testing.T, wantKey string) pc0QueryScope {
	t.Helper()
	repoRoot := r3RepositoryRoot(t)
	var found *pc0QueryScope
	walkErr := filepath.WalkDir(filepath.Join(repoRoot, "internal"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || found != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return relErr
		}
		relPath = filepath.ToSlash(relPath)
		file := r3ParseProductionFile(t, path)
		for _, scope := range pc0FileQueryScopes(file) {
			if pc0CallerKey(relPath, scope.name) == wantKey {
				copy := scope
				found = &copy
				return nil
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("PC0 HEAD SERIAL: walk for %s: %v", wantKey, walkErr)
	}
	if found == nil {
		t.Fatalf("PC0 HEAD SERIAL: allowlisted unresolved Query %s not found", wantKey)
	}
	return *found
}

func pc0VisitSprintfCalls(node ast.Node, bindings map[string]string, visit func(format string, resolved bool)) {
	if node == nil {
		return
	}
	ast.Inspect(node, func(current ast.Node) bool {
		call, ok := current.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Sprintf" {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name != "fmt" {
			return true
		}
		format, ok := pc0ResolveStringExpr(call.Args[0], bindings)
		visit(format, ok)
		return true
	})
}

func pc0RequireSprintfFormatsResolved(t *testing.T, key string, scope pc0QueryScope, wantCount int) []string {
	t.Helper()
	bindings := pc0BlockStringBindings(scope.body, nil)
	var formats []string
	pc0VisitSprintfCalls(scope.node, bindings, func(format string, resolved bool) {
		if !resolved {
			t.Errorf("PC0 HEAD SERIAL: allowlisted unresolved Query shape at %s: fmt.Sprintf format is not a source-resolvable string", key)
			formats = append(formats, "<unresolved>")
			return
		}
		formats = append(formats, format)
		// Lock helpers interpolate the table name. Expanding DELETE/INSERT
		// IF lease_token onto libraries is a whole-row LWT, but it is not a
		// HEAD competitor unless the CQL names head_commit_id. Using
		// pc0CQLCompetesForLibraryHead here would false-RED acquire/release.
		if pc0HeadCommitIDColumnPattern.MatchString(pc0PreparedCQL(pc0FormatAsLibrariesTable(format))) {
			t.Errorf("PC0 HEAD SERIAL: allowlisted unresolved Query shape at %s names head_commit_id when interpolated as libraries: %q", key, format)
		}
	})
	if len(formats) != wantCount {
		t.Errorf("PC0 HEAD SERIAL: allowlisted unresolved Query shape at %s fmt.Sprintf count=%d, want %d", key, len(formats), wantCount)
	}
	return formats
}

func pc0FormatAsLibrariesTable(format string) string {
	expanded := strings.ReplaceAll(format, "%s", "libraries")
	return strings.ReplaceAll(expanded, "%d", "0")
}

func pc0UnquoteBasicLit(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

var pc0AllowedHardDeleteLockTables = map[string]bool{
	"gc_library_hard_delete_locks": true,
	"gc_user_hard_delete_locks":    true,
	"gc_org_hard_delete_locks":     true,
}

var pc0HardDeleteLockHelpers = map[string]int{
	"acquireHardDeleteLock": 1,
	"renewHardDeleteLock":   1,
	"releaseHardDeleteLock": 1,
}

var pc0UpdateLibraryAllowedSETFragments = map[string]bool{
	"name = ?":             true,
	"description = ?":      true,
	"version_ttl_days = ?": true,
	"updated_at = ?":       true,
}

var pc0UpdateOrganizationAllowedColumns = map[string]bool{
	"name":                      true,
	"storage_quota":             true,
	"traffic_quota":             true,
	"traffic_upload_quota":      true,
	"traffic_download_quota":    true,
	"max_users":                 true,
	"plan":                      true,
	"quota_policy":              true,
	"storage_config":            true,
	"billing_cycle":             true,
	"current_period_started_at": true,
	"current_period_ends_at":    true,
}

// TestPC0UnresolvedHeadQueriesStayOutOfHeadDomain pins the real shape of every
// fail-closed allowlist exception. Count-only would accept UpdateLibrary still
// having one dynamic Query after that Query became DELETE/UPDATE IF head_commit_id.
func TestPC0UnresolvedHeadQueriesStayOutOfHeadDomain(t *testing.T) {
	lockCallSites := 0
	repoRoot := r3RepositoryRoot(t)
	walkErr := filepath.WalkDir(filepath.Join(repoRoot, "internal"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		file := r3ParseProductionFile(t, path)
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			argIndex, tracked := pc0HardDeleteLockHelpers[ident.Name]
			if !tracked {
				return true
			}
			lockCallSites++
			if len(call.Args) <= argIndex {
				t.Errorf("PC0 HEAD SERIAL: %s at %s has no table argument", ident.Name, path)
				return true
			}
			table, ok := pc0UnquoteBasicLit(call.Args[argIndex])
			if !ok {
				t.Errorf("PC0 HEAD SERIAL: %s at %s passes a non-literal table name; the unresolved-Query allowlist cannot prove it stays off libraries.head_commit_id", ident.Name, path)
				return true
			}
			if !pc0AllowedHardDeleteLockTables[table] {
				t.Errorf("PC0 HEAD SERIAL: %s at %s targets %q, which is not an allowed hard-delete lock table", ident.Name, path, table)
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("PC0 HEAD SERIAL: walk lock call sites: %v", walkErr)
	}
	if lockCallSites == 0 {
		t.Fatal("PC0 HEAD SERIAL: found no hard-delete lock call sites; the allowlist justification would pass vacuously")
	}

	pc0RequireEmbeddedMigrationsStayOutOfHeadDomain(t)

	for key, allowance := range pc0AllowedUnresolvedHeadQueries {
		if allowance.shape == "" {
			t.Errorf("PC0 HEAD SERIAL: allowlisted unresolved Query %s has no shape pin", key)
			continue
		}
		scope := pc0FindQueryScope(t, key)
		switch allowance.shape {
		case pc0UnresolvedShapeHardDeleteLock:
			pc0RequireLockHelperFormatNotHeadLWT(t, key, scope, allowance.sprintfCount)
		case pc0UnresolvedShapeUpdateLibrarySET:
			pc0RequireSprintfFormatsResolved(t, key, scope, allowance.sprintfCount)
			pc0RequireUpdateLibraryUnresolvedShape(t, scope)
		case pc0UnresolvedShapeOrgSettingSprintf:
			pc0RequireExactSprintfFormats(t, key, scope, allowance.sprintfCount, "UPDATE organizations SET settings['%s'] = ? WHERE org_id = ?")
		case pc0UnresolvedShapeOrgColumnSprintf:
			pc0RequireUpdateOrganizationUnresolvedShape(t, scope, allowance.sprintfCount)
		case pc0UnresolvedShapeMigratorStatements:
			pc0RequireSprintfFormatsResolved(t, key, scope, allowance.sprintfCount)
			pc0RequireMigratorApplyUnresolvedShape(t, scope)
		default:
			t.Errorf("PC0 HEAD SERIAL: allowlisted unresolved Query %s has unknown shape %q", key, allowance.shape)
		}
	}
}

func pc0RequireLockHelperFormatNotHeadLWT(t *testing.T, key string, scope pc0QueryScope, wantSprintf int) {
	t.Helper()
	pc0RequireSprintfFormatsResolved(t, key, scope, wantSprintf)
}

func pc0RequireUpdateLibraryUnresolvedShape(t *testing.T, scope pc0QueryScope) {
	t.Helper()
	const (
		wantPrefix = "UPDATE libraries SET "
		wantSuffix = " WHERE org_id = ? AND library_id = ?"
	)
	bindings := pc0BlockStringBindings(scope.body, nil)
	openedPrefix := false
	closedSuffix := false
	ast.Inspect(scope.node, func(node ast.Node) bool {
		switch stmt := node.(type) {
		case *ast.AssignStmt:
			for i, lhs := range stmt.Lhs {
				ident, ok := lhs.(*ast.Ident)
				if !ok || ident.Name != "query" || i >= len(stmt.Rhs) {
					continue
				}
				switch stmt.Tok {
				case token.DEFINE, token.ASSIGN:
					value, ok := pc0ResolveStringExpr(stmt.Rhs[i], bindings)
					if !ok {
						t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: query prefix is not a source-resolvable string")
						continue
					}
					if value != wantPrefix {
						t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: query prefix %q, want %q", value, wantPrefix)
					}
					openedPrefix = true
				case token.ADD_ASSIGN:
					if ident, ok := stmt.Rhs[i].(*ast.Ident); ok && ident.Name == "update" {
						continue
					}
					value, ok := pc0ResolveStringExpr(stmt.Rhs[i], bindings)
					if !ok {
						t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: query += non-literal fragment")
						continue
					}
					switch value {
					case ", ":
					case wantSuffix:
						closedSuffix = true
					default:
						t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: query suffix/fragment %q is not a non-HEAD SET fragment", value)
					}
				}
			}
		case *ast.CallExpr:
			ident, ok := stmt.Fun.(*ast.Ident)
			if !ok || ident.Name != "append" || len(stmt.Args) < 2 {
				return true
			}
			base, ok := stmt.Args[0].(*ast.Ident)
			if !ok || base.Name != "updates" {
				return true
			}
			for _, arg := range stmt.Args[1:] {
				value, ok := pc0ResolveStringExpr(arg, bindings)
				if !ok {
					t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: SET fragment is not a source-resolvable string")
					continue
				}
				if !pc0UpdateLibraryAllowedSETFragments[value] {
					t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: SET fragment %q is not in the non-HEAD column list", value)
				}
			}
		}
		return true
	})
	if !openedPrefix {
		t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: missing query := %q", wantPrefix)
	}
	if !closedSuffix {
		t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: missing query += %q without IF", wantSuffix)
	}
	var unresolvedQueryArgs int
	pc0VisitCQLEntryPoints(scope.node, bindings, func(call *ast.CallExpr, _ string, resolved bool) {
		if resolved {
			return
		}
		unresolvedQueryArgs++
		ident, ok := call.Args[0].(*ast.Ident)
		if !ok || ident.Name != "query" {
			t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: first argument is not ident query")
		}
	})
	if unresolvedQueryArgs != 1 {
		t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: unresolved Query count=%d, want 1", unresolvedQueryArgs)
	}
}

func pc0RequireExactSprintfFormats(t *testing.T, key string, scope pc0QueryScope, wantCount int, want string) {
	t.Helper()
	formats := pc0RequireSprintfFormatsResolved(t, key, scope, wantCount)
	if len(formats) != 1 || formats[0] != want {
		t.Errorf("PC0 HEAD SERIAL: allowlisted unresolved Query shape at %s fmt.Sprintf formats=%q, want [%q]", key, formats, want)
	}
}

func pc0RequireUpdateOrganizationUnresolvedShape(t *testing.T, scope pc0QueryScope, wantSprintf int) {
	t.Helper()
	const wantFormat = "UPDATE organizations SET %s = ? WHERE org_id = ?"
	pc0RequireExactSprintfFormats(t, "internal/api/v2/admin.go:AdminHandler.UpdateOrganization", scope, wantSprintf, wantFormat)
	ast.Inspect(scope.node, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		ident, ok := lit.Type.(*ast.Ident)
		if !ok || ident.Name != "colUpdate" || len(lit.Elts) == 0 {
			return true
		}
		column, ok := pc0UnquoteBasicLit(lit.Elts[0])
		if !ok {
			t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateOrganization unresolved Query shape: colUpdate column is not a string literal")
			return true
		}
		if !pc0UpdateOrganizationAllowedColumns[column] {
			t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateOrganization unresolved Query shape: column %q is not an organizations column", column)
		}
		return true
	})
}

func pc0RequireMigratorApplyUnresolvedShape(t *testing.T, scope pc0QueryScope) {
	t.Helper()
	rangedStatements := false
	ast.Inspect(scope.node, func(node ast.Node) bool {
		rng, ok := node.(*ast.RangeStmt)
		if !ok {
			return true
		}
		sel, ok := rng.X.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Statements" {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name != "mf" {
			return true
		}
		value, ok := rng.Value.(*ast.Ident)
		if !ok || value.Name != "stmt" {
			t.Errorf("PC0 HEAD SERIAL: allowlisted Migrator.apply unresolved Query shape: range value is not stmt")
			return true
		}
		rangedStatements = true
		return true
	})
	if !rangedStatements {
		t.Errorf("PC0 HEAD SERIAL: allowlisted Migrator.apply unresolved Query shape: missing for range mf.Statements")
	}
	var unresolvedQueryArgs int
	pc0VisitCQLEntryPoints(scope.node, pc0BlockStringBindings(scope.body, nil), func(call *ast.CallExpr, _ string, resolved bool) {
		if resolved {
			if cql, ok := pc0ResolveStringExpr(call.Args[0], nil); ok && pc0CQLCompetesForLibraryHead(cql) {
				t.Errorf("PC0 HEAD SERIAL: allowlisted Migrator.apply unresolved Query shape: inline CQL competes for libraries.head_commit_id")
			}
			return
		}
		unresolvedQueryArgs++
		ident, ok := call.Args[0].(*ast.Ident)
		if !ok || ident.Name != "stmt" {
			t.Errorf("PC0 HEAD SERIAL: allowlisted Migrator.apply unresolved Query shape: first argument is not ident stmt")
		}
	})
	if unresolvedQueryArgs != 1 {
		t.Errorf("PC0 HEAD SERIAL: allowlisted Migrator.apply unresolved Query shape: unresolved Query count=%d, want 1", unresolvedQueryArgs)
	}
}

func pc0RequireEmbeddedMigrationsStayOutOfHeadDomain(t *testing.T) {
	t.Helper()
	files, err := (&Migrator{}).loadFiles()
	if err != nil {
		t.Fatalf("PC0 HEAD SERIAL: load embedded migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("PC0 HEAD SERIAL: no embedded migrations; Migrator.apply allowlist would pass vacuously")
	}
	for _, mf := range files {
		for _, stmt := range mf.Statements {
			if pc0CQLCompetesForLibraryHead(stmt) {
				t.Errorf("PC0 HEAD SERIAL: embedded migration %s competes for libraries.head_commit_id: %s", mf.Filename, stmt)
			}
		}
	}
}

// TestPC0HeadSerialDomainPinsGlobalSerial is the productive pin for
// ISSUE-LIBRARY-HEAD-SERIAL-DOMAIN-01. It walks the MapScanCAS chain of every
// derived HEAD-authority LWT and requires SerialConsistency(LibraryHeadSerialConsistency)
// on that chain — not a substring of the function, which would go green on a
// confirm SELECT's Consistency(gocql.Serial) or a pin moved onto the wrong query.
func TestPC0HeadSerialDomainPinsGlobalSerial(t *testing.T) {
	ops := pc0HeadSerialDomainOps()
	if len(ops) != 4 {
		t.Fatalf("PC0 HEAD SERIAL: derived serial-domain ops = %d, want 4 (three cas writers + rollback DELETE)", len(ops))
	}

	seen := map[string]bool{}
	for _, op := range ops {
		key := pc0CallerKey(op.path, op.decl)
		if seen[key] {
			t.Fatalf("PC0 HEAD SERIAL: duplicate serial-domain op %s", key)
		}
		seen[key] = true
		pc0RequireHeadSerialPinOnCASChain(t, op)
	}
}

func pc0RequireHeadSerialPinOnCASChain(t *testing.T, op pc0HeadSerialDomainOp) {
	t.Helper()
	root := r3RepositoryRoot(t)
	full := filepath.Join(root, filepath.FromSlash(op.path))
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, full, nil, 0)
	if err != nil {
		t.Fatalf("PC0 HEAD SERIAL: parse %s: %v", op.path, err)
	}

	fn := pc0FindDeclByName(file, op.decl)
	if fn == nil {
		t.Fatalf("PC0 HEAD SERIAL: %s not found in %s", op.decl, op.path)
	}

	var terminals []*ast.CallExpr
	ast.Inspect(fn, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !pc0QueryCASTerminals[selector.Sel.Name] {
			return true
		}
		terminals = append(terminals, call)
		return true
	})
	if len(terminals) != 1 {
		t.Fatalf("PC0 HEAD SERIAL: %s in %s has %d Query CAS terminals, want exactly 1", op.decl, op.path, len(terminals))
	}

	chain := pc0QueryMethodChain(terminals[0])
	cql := pc0ChainQueryCQL(t, op, chain)
	if !pc0CQLCompetesForLibraryHead(cql) {
		t.Fatalf("PC0 HEAD SERIAL: %s in %s CAS CQL is not a libraries HEAD-authority LWT: %q", op.decl, op.path, cql)
	}

	args, ok := chain["SerialConsistency"]
	if !ok || len(args) != 1 {
		t.Fatalf("PC0 HEAD SERIAL: %s in %s must call SerialConsistency(LibraryHeadSerialConsistency) on the HEAD LWT chain (session default is not the canonical domain)", op.decl, op.path)
	}
	selector, ok := args[0].(*ast.SelectorExpr)
	if !ok {
		t.Fatalf("PC0 HEAD SERIAL: %s in %s SerialConsistency argument = %s, want LibraryHeadSerialConsistency", op.decl, op.path, pc0NodeText(t, args[0]))
	}
	if selector.Sel.Name == "LocalSerial" {
		t.Fatalf("PC0 HEAD SERIAL: %s in %s must not call SerialConsistency(gocql.LocalSerial)", op.decl, op.path)
	}
	if selector.Sel.Name != "LibraryHeadSerialConsistency" {
		t.Fatalf("PC0 HEAD SERIAL: %s in %s must call SerialConsistency(LibraryHeadSerialConsistency), got SerialConsistency(%s)", op.decl, op.path, pc0NodeText(t, args[0]))
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || (pkg.Name != "db" && pkg.Name != "dbpkg") {
		t.Fatalf("PC0 HEAD SERIAL: %s in %s must use db.LibraryHeadSerialConsistency (or dbpkg alias), got %s", op.decl, op.path, pc0NodeText(t, args[0]))
	}
}

func pc0FindDeclByName(file *ast.File, decl string) ast.Node {
	wantRecv, wantName, hasRecv := strings.Cut(decl, ".")
	if !hasRecv {
		wantName = decl
	}
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Name.Name != wantName {
			continue
		}
		if !hasRecv {
			if fn.Recv == nil {
				return fn
			}
			continue
		}
		if pc0ReceiverTypeName(fn) == wantRecv {
			return fn
		}
	}
	return nil
}

func pc0QueryMethodChain(terminal *ast.CallExpr) map[string][]ast.Expr {
	chain := map[string][]ast.Expr{}
	var expression ast.Expr = terminal
	for {
		call, ok := expression.(*ast.CallExpr)
		if !ok {
			break
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			break
		}
		chain[selector.Sel.Name] = call.Args
		expression = selector.X
	}
	return chain
}

func pc0ChainQueryCQL(t *testing.T, op pc0HeadSerialDomainOp, chain map[string][]ast.Expr) string {
	t.Helper()
	args, ok := chain["Query"]
	if !ok || len(args) == 0 {
		t.Fatalf("PC0 HEAD SERIAL: %s in %s CAS chain has no Query CQL", op.decl, op.path)
	}
	lit, ok := args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		t.Fatalf("PC0 HEAD SERIAL: %s in %s Query CQL must be an inline string literal, got %T", op.decl, op.path, args[0])
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		t.Fatalf("PC0 HEAD SERIAL: unquote %s Query CQL: %v", op.decl, err)
	}
	return value
}

func pc0NodeText(t *testing.T, node ast.Node) string {
	t.Helper()
	var output bytes.Buffer
	if err := format.Node(&output, token.NewFileSet(), node); err != nil {
		t.Fatalf("format AST node: %v", err)
	}
	return output.String()
}

func TestPC0HeadSerialDomainDoesNotIncludeInsertCreate(t *testing.T) {
	for _, writer := range pc0ExpectedHeadColumnWriters {
		if writer.shape != pc0HeadWriteInsertCreate {
			continue
		}
		for _, op := range pc0HeadSerialDomainOps() {
			if op.path == writer.path && op.decl == writer.decl {
				t.Fatalf("PC0 HEAD SERIAL: insert-create %s:%s must not be in the HEAD Paxos domain (it is not an LWT)", writer.path, writer.decl)
			}
		}
	}
}
