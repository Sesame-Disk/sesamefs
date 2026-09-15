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

// TestPC0HeadAuthorityDeleteGuardsAreInventoried closes the Query/Bind blind
// spot of TestPC0RawHeadColumnWritersAreInventoried: any resolved CQL that
// pc0CQLCompetesForLibraryHead accepts (UPDATE that writes or IFs on HEAD,
// whole-row DELETE LWT, cell-delete of HEAD, INSERT IF NOT EXISTS that
// writes HEAD) is a HEAD-authority mutation, including forms split across
// concat/const so that no single BasicLit contains `UPDATE libraries` and
// `head_commit_id`. Discovery walks Query/Bind entry points, including
// package-level `var name = func(...)` seams. Presence of the function key is
// not enough: each SERIAL-domain op must have exactly one competing Query/Bind,
// so a second DELETE/UPDATE IF executed via Query.Exec() in the same function
// cannot hide behind the inventoried CAS. An unresolvable first
// argument fails closed unless it is in pc0AllowedUnresolvedHeadQueries.
// Expected hits are exactly the SERIAL-domain ops (cas writers + guards).
func TestPC0HeadAuthorityDeleteGuardsAreInventoried(t *testing.T) {
	hits, unresolved := pc0HeadAuthorityFromQueries(t, "internal", "cmd")

	expected := map[string]struct{}{}
	for _, op := range pc0HeadSerialDomainOps() {
		expected[pc0CallerKey(op.path, op.decl)] = struct{}{}
	}

	var unlisted []string
	for key := range hits {
		if _, listed := expected[key]; !listed {
			unlisted = append(unlisted, key)
		}
	}
	sort.Strings(unlisted)
	if len(unlisted) > 0 {
		t.Fatalf("PC0 HEAD SERIAL: unlisted competing HEAD mutation %v; every Query/Bind that competes for libraries.head_commit_id must be a cas-shaped pc0ExpectedHeadColumnWriters entry or pc0ExpectedHeadAuthorityGuards", unlisted)
	}

	var missing []string
	for key := range expected {
		if _, found := hits[key]; !found {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("PC0 HEAD SERIAL: inventoried HEAD-authority Query/Bind mutations no longer found: %v", missing)
	}

	for _, op := range pc0HeadSerialDomainOps() {
		key := pc0CallerKey(op.path, op.decl)
		if n := len(hits[key]); n != 1 {
			t.Errorf("PC0 HEAD SERIAL: inventoried %s competing HEAD mutations count=%d, want 1; a second Query/Exec HEAD LWT in the same function would otherwise collapse into the existing allowlisted key", key, n)
		}
	}

	pc0RequireUnresolvedHeadQueriesAllowed(t, unresolved)
}

// pc0AllowedUnresolvedHeadQueries are production Query/Bind call sites whose
// CQL is not a source-resolvable string. The HEAD inventory cannot prove they
// are not a competing libraries.head_commit_id mutation, so each one must be
// named AND shape-pinned by TestPC0UnresolvedHeadQueriesStayOutOfHeadDomain.
// A count-only allowlist would stay green if UpdateLibrary kept one dynamic
// Query while changing it into a HEAD LWT.
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
	"internal/api/v2/libraries.go:LibraryHandler.UpdateLibrary":     {count: 1, sprintfCount: 0, reason: "opens with literal UPDATE libraries SET and appends caller-built assignments; not a HEAD LWT", shape: pc0UnresolvedShapeUpdateLibrarySET},
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
		t.Fatalf("PC0 HEAD SERIAL: unresolvable Query/Bind CQL at %v; a constructed statement can hide a competing HEAD mutation. Name it in pc0AllowedUnresolvedHeadQueries only after proving it is not a libraries.head_commit_id competitor", extra)
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

func pc0HeadAuthorityFromQueries(t *testing.T, roots ...string) (map[string][]string, map[string]int) {
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
					if pc0CQLCompetesForLibraryHead(cql) {
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
		// Substituting libraries into a lock DELETE/INSERT IF lease_token
		// would make a whole-row LWT a HEAD competitor. Call sites are
		// pinned separately to gc_*_hard_delete_locks; here we only reject
		// formats that explicitly acquire HEAD semantics.
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

// Exact fmt.Sprintf shapes for the allowlisted lock helpers. Call-site table
// literals do not prove the helper still interpolates tableName as the
// relation: a format can hard-code DELETE FROM libraries while keeping %s
// only to consume Sprintf arguments, which is a whole-row HEAD competitor
// without naming head_commit_id.
type pc0LockHelperSprintfPin struct {
	format string
	args   []string
}

var pc0HardDeleteLockSprintfPins = map[string][]pc0LockHelperSprintfPin{
	"internal/gc/store_cassandra.go:acquireHardDeleteLock": {
		{
			format: "\n\t\tINSERT INTO %s (%s, started_at, heartbeat, lease_token)\n\t\tVALUES (?, ?, ?, ?) IF NOT EXISTS USING TTL %d\n\t",
			args:   []string{"tableName", "keyColumn", "hardDeleteLockTTLSeconds"},
		},
		{
			format: "\n\t\tUPDATE %s USING TTL %d\n\t\tSET started_at = ?, heartbeat = ?, lease_token = ?\n\t\tWHERE %s = ? IF lease_token = ?\n\t",
			args:   []string{"tableName", "hardDeleteLockTTLSeconds", "keyColumn"},
		},
	},
	"internal/gc/store_cassandra.go:renewHardDeleteLock": {
		{
			format: "\n\t\tUPDATE %s USING TTL %d\n\t\tSET heartbeat = ?, lease_token = ?\n\t\tWHERE %s = ? IF lease_token = ?\n\t",
			args:   []string{"tableName", "hardDeleteLockTTLSeconds", "keyColumn"},
		},
	},
	"internal/gc/store_cassandra.go:releaseHardDeleteLock": {
		{
			format: "\n\t\tDELETE FROM %s WHERE %s = ? IF lease_token = ?\n\t",
			args:   []string{"tableName", "keyColumn"},
		},
	},
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

func pc0IsFmtSprintf(call *ast.CallExpr) bool {
	if call == nil || len(call.Args) == 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Sprintf" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == "fmt"
}

func pc0RequireLockHelperFormatNotHeadLWT(t *testing.T, key string, scope pc0QueryScope, wantSprintf int) {
	t.Helper()
	pins, ok := pc0HardDeleteLockSprintfPins[key]
	if !ok {
		t.Errorf("PC0 HEAD SERIAL: allowlisted unresolved Query shape at %s has no pinned lock CQL formats", key)
		return
	}
	if len(pins) != wantSprintf {
		t.Errorf("PC0 HEAD SERIAL: allowlisted unresolved Query shape at %s pinned lock CQL format count=%d, want %d", key, len(pins), wantSprintf)
	}
	bindings := pc0BlockStringBindings(scope.body, nil)
	n := 0
	ast.Inspect(scope.node, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !pc0IsFmtSprintf(call) {
			return true
		}
		if n >= len(pins) {
			t.Errorf("PC0 HEAD SERIAL: allowlisted unresolved Query shape at %s fmt.Sprintf count greater than %d", key, len(pins))
			n++
			return true
		}
		pin := pins[n]
		n++
		format, resolved := pc0ResolveStringExpr(call.Args[0], bindings)
		if !resolved {
			t.Errorf("PC0 HEAD SERIAL: allowlisted unresolved Query shape at %s: fmt.Sprintf format is not a source-resolvable string", key)
			return true
		}
		if pc0HeadCommitIDColumnPattern.MatchString(pc0PreparedCQL(pc0FormatAsLibrariesTable(format))) {
			t.Errorf("PC0 HEAD SERIAL: allowlisted unresolved Query shape at %s names head_commit_id when interpolated as libraries: %q", key, format)
		}
		if format != pin.format {
			t.Errorf("PC0 HEAD SERIAL: allowlisted unresolved Query shape at %s: fmt.Sprintf format is not the pinned lock CQL shape", key)
		}
		wantArgs := 1 + len(pin.args)
		if len(call.Args) != wantArgs {
			t.Errorf("PC0 HEAD SERIAL: allowlisted unresolved Query shape at %s fmt.Sprintf arg count=%d, want %d", key, len(call.Args), wantArgs)
			return true
		}
		for i, name := range pin.args {
			if !pc0IdentNamed(call.Args[i+1], name) {
				t.Errorf("PC0 HEAD SERIAL: allowlisted unresolved Query shape at %s fmt.Sprintf arg %d is not ident %s", key, i+1, name)
			}
		}
		return true
	})
	if n != len(pins) {
		t.Errorf("PC0 HEAD SERIAL: allowlisted unresolved Query shape at %s fmt.Sprintf count=%d, want %d", key, n, len(pins))
	}
}

func pc0RequireUpdateLibraryUnresolvedShape(t *testing.T, scope pc0QueryScope) {
	t.Helper()
	const (
		wantPrefix = "UPDATE libraries SET "
		wantSuffix = " WHERE org_id = ? AND library_id = ?"
	)
	bindings := pc0BlockStringBindings(scope.body, nil)
	pc0RequireUpdateLibraryUpdatesSlice(t, scope, bindings)
	var updatesRange *ast.RangeStmt
	updatesRanges := 0
	ast.Inspect(scope.node, func(node ast.Node) bool {
		rng, ok := node.(*ast.RangeStmt)
		if !ok || !pc0IsUpdateLibraryUpdatesRange(rng) {
			return true
		}
		updatesRanges++
		updatesRange = rng
		return true
	})
	if updatesRanges != 1 {
		t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: for i, update := range updates count=%d, want 1", updatesRanges)
	}
	openedPrefix := false
	closedSuffix := false
	ast.Inspect(scope.node, func(node ast.Node) bool {
		stmt, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
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
				if pc0IdentNamed(stmt.Rhs[i], "update") {
					if updatesRange == nil || !pc0StmtInBlock(updatesRange.Body, stmt) {
						t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: query += update is not inside for i, update := range updates")
					}
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
	pc0RequireUpdateLibraryIdentWhitelist(t, scope, bindings, updatesRange)
}

func pc0IdentNamed(expr ast.Expr, name string) bool {
	ident, ok := pc0UnwrapParen(expr).(*ast.Ident)
	return ok && ident.Name == name
}

func pc0ExprReferencesUpdates(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok || ident.Name != "updates" {
			return true
		}
		found = true
		return false
	})
	return found
}

func pc0StmtInBlock(block *ast.BlockStmt, stmt ast.Stmt) bool {
	if block == nil || stmt == nil {
		return false
	}
	for _, item := range block.List {
		if item == stmt {
			return true
		}
	}
	return false
}

func pc0CallInsideRangeNotClosure(rng *ast.RangeStmt, call *ast.CallExpr) bool {
	if rng == nil || rng.Body == nil || call == nil {
		return false
	}
	if call.Pos() < rng.Body.Pos() || call.End() > rng.Body.End() {
		return false
	}
	enclosed := false
	ast.Inspect(rng.Body, func(node ast.Node) bool {
		if enclosed || node == nil {
			return false
		}
		lit, ok := node.(*ast.FuncLit)
		if !ok {
			return true
		}
		if call.Pos() >= lit.Pos() && call.End() <= lit.End() {
			enclosed = true
			return false
		}
		return true
	})
	return !enclosed
}

func pc0IdentIsSelectorSel(stack []ast.Node, ident *ast.Ident) bool {
	if len(stack) == 0 {
		return false
	}
	sel, ok := stack[len(stack)-1].(*ast.SelectorExpr)
	return ok && sel.Sel == ident
}

func pc0MarkUpdateLibraryIdent(allowed map[*ast.Ident]struct{}, expr ast.Expr) {
	ident, ok := pc0UnwrapParen(expr).(*ast.Ident)
	if !ok {
		return
	}
	allowed[ident] = struct{}{}
}

func pc0RequireUpdateLibraryIdentWhitelist(t *testing.T, scope pc0QueryScope, bindings map[string]string, updatesRange *ast.RangeStmt) {
	t.Helper()
	const (
		wantPrefix = "UPDATE libraries SET "
		wantSuffix = " WHERE org_id = ? AND library_id = ?"
	)
	allowed := map[*ast.Ident]struct{}{}
	ast.Inspect(scope.node, func(node ast.Node) bool {
		switch stmt := node.(type) {
		case *ast.AssignStmt:
			if len(stmt.Lhs) != 1 || len(stmt.Rhs) != 1 {
				return true
			}
			lhs, rhs := stmt.Lhs[0], stmt.Rhs[0]
			switch stmt.Tok {
			case token.DEFINE:
				if pc0IdentNamed(lhs, "updates") && pc0IsEmptyStringSliceLit(rhs) {
					pc0MarkUpdateLibraryIdent(allowed, lhs)
				}
				if pc0IdentNamed(lhs, "query") {
					if value, ok := pc0ResolveStringExpr(rhs, bindings); ok && value == wantPrefix {
						pc0MarkUpdateLibraryIdent(allowed, lhs)
					}
				}
			case token.ASSIGN:
				if pc0IdentNamed(lhs, "updates") {
					if call, ok := pc0UpdatesAppendCall(rhs); ok {
						pc0MarkUpdateLibraryIdent(allowed, lhs)
						pc0MarkUpdateLibraryIdent(allowed, call.Args[0])
					}
				}
			case token.ADD_ASSIGN:
				if !pc0IdentNamed(lhs, "query") {
					return true
				}
				if pc0IdentNamed(rhs, "update") {
					if updatesRange != nil && pc0StmtInBlock(updatesRange.Body, stmt) {
						pc0MarkUpdateLibraryIdent(allowed, lhs)
						pc0MarkUpdateLibraryIdent(allowed, rhs)
					}
					return true
				}
				value, ok := pc0ResolveStringExpr(rhs, bindings)
				if ok && (value == ", " || value == wantSuffix) {
					pc0MarkUpdateLibraryIdent(allowed, lhs)
				}
			}
		case *ast.RangeStmt:
			if pc0IsUpdateLibraryUpdatesRange(stmt) {
				pc0MarkUpdateLibraryIdent(allowed, stmt.X)
				pc0MarkUpdateLibraryIdent(allowed, stmt.Value)
			}
		case *ast.CallExpr:
			name := pc0CallFunName(stmt)
			if name == "len" && len(stmt.Args) == 1 && pc0IdentNamed(stmt.Args[0], "updates") {
				pc0MarkUpdateLibraryIdent(allowed, stmt.Args[0])
			}
			if pc0IsCQLEntryPoint(name, len(stmt.Args)) && pc0IdentNamed(stmt.Args[0], "query") {
				pc0MarkUpdateLibraryIdent(allowed, stmt.Args[0])
			}
		}
		return true
	})

	var stack []ast.Node
	ast.Inspect(scope.node, func(node ast.Node) bool {
		if node == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return true
		}
		if ident, ok := node.(*ast.Ident); ok {
			switch ident.Name {
			case "query", "updates", "update":
				if !pc0IdentIsSelectorSel(stack, ident) {
					if _, ok := allowed[ident]; !ok {
						t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: ident %s is used outside the pinned shape", ident.Name)
					}
				}
			}
		}
		stack = append(stack, node)
		return true
	})
}

func pc0IsUpdateLibraryUpdatesRange(rng *ast.RangeStmt) bool {
	return rng != nil && rng.Tok == token.DEFINE && pc0IdentNamed(rng.X, "updates") && pc0IdentNamed(rng.Key, "i") && pc0IdentNamed(rng.Value, "update")
}

func pc0UnwrapParen(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

func pc0IsEmptyStringSliceLit(expr ast.Expr) bool {
	lit, ok := pc0UnwrapParen(expr).(*ast.CompositeLit)
	if !ok || len(lit.Elts) != 0 {
		return false
	}
	arr, ok := lit.Type.(*ast.ArrayType)
	if !ok || arr.Len != nil {
		return false
	}
	ident, ok := arr.Elt.(*ast.Ident)
	return ok && ident.Name == "string"
}

func pc0UpdatesAppendCall(expr ast.Expr) (*ast.CallExpr, bool) {
	call, ok := pc0UnwrapParen(expr).(*ast.CallExpr)
	if !ok || call.Ellipsis != token.NoPos {
		return nil, false
	}
	ident, ok := call.Fun.(*ast.Ident)
	if !ok || ident.Name != "append" || len(call.Args) < 2 {
		return nil, false
	}
	if !pc0IdentNamed(call.Args[0], "updates") {
		return nil, false
	}
	return call, true
}

func pc0CallFunName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	default:
		return ""
	}
}

func pc0RequireUpdateLibraryAppendFragments(t *testing.T, call *ast.CallExpr, bindings map[string]string) {
	t.Helper()
	if call.Ellipsis != token.NoPos {
		t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: append(updates, x...) is not a proven SET fragment")
		return
	}
	if len(call.Args) < 2 {
		t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: append(updates) has no SET fragment")
		return
	}
	for _, arg := range call.Args[1:] {
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

func pc0RequireUpdateLibraryUpdatesSlice(t *testing.T, scope pc0QueryScope, bindings map[string]string) {
	t.Helper()
	emptyInits := 0
	ast.Inspect(scope.node, func(node ast.Node) bool {
		switch stmt := node.(type) {
		case *ast.DeclStmt:
			gen, ok := stmt.Decl.(*ast.GenDecl)
			if !ok {
				return true
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range value.Names {
					if name.Name == "updates" {
						t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: updates declared with var; want updates := []string{}")
					}
				}
			}
		case *ast.RangeStmt:
			if pc0IdentNamed(stmt.Key, "updates") || pc0IdentNamed(stmt.Value, "updates") {
				t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: range rebinds updates")
			}
			if pc0IdentNamed(stmt.Value, "update") && !pc0IdentNamed(stmt.X, "updates") {
				t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: range value update must iterate updates")
			}
		case *ast.AssignStmt:
			if stmt.Tok == token.DEFINE {
				for _, lhs := range stmt.Lhs {
					if ident, ok := lhs.(*ast.Ident); ok && ident.Name == "update" {
						t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: update rebound")
					}
				}
			}
			for _, lhs := range stmt.Lhs {
				switch typed := lhs.(type) {
				case *ast.IndexExpr:
					if pc0IdentNamed(typed.X, "updates") {
						t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: updates element assignment")
					}
				case *ast.SliceExpr:
					if pc0IdentNamed(typed.X, "updates") {
						t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: updates slice assignment")
					}
				}
			}
			if len(stmt.Rhs) == 1 {
				rhs := pc0UnwrapParen(stmt.Rhs[0])
				aliased := false
				switch src := rhs.(type) {
				case *ast.Ident:
					aliased = src.Name == "updates"
				case *ast.SliceExpr:
					aliased = pc0IdentNamed(src.X, "updates")
				case *ast.UnaryExpr:
					aliased = src.Op == token.AND && pc0IdentNamed(src.X, "updates")
				}
				if aliased {
					for _, lhs := range stmt.Lhs {
						ident, ok := lhs.(*ast.Ident)
						if !ok || ident.Name == "updates" || ident.Name == "_" {
							continue
						}
						t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: updates aliased as %s", ident.Name)
					}
				}
			}
			for _, lhs := range stmt.Lhs {
				ident, ok := lhs.(*ast.Ident)
				if !ok || ident.Name != "updates" {
					continue
				}
				if len(stmt.Lhs) != 1 || len(stmt.Rhs) != 1 {
					t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: updates assigned in a multi-value form")
					continue
				}
				rhs := stmt.Rhs[0]
				switch stmt.Tok {
				case token.DEFINE:
					if !pc0IsEmptyStringSliceLit(rhs) {
						t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: updates initializer is not empty []string{}")
						continue
					}
					emptyInits++
				case token.ASSIGN:
					if _, ok := pc0UpdatesAppendCall(rhs); !ok {
						t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: updates = ... must be append(updates, <SET fragments>)")
					}
				default:
					t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: updates mutated with %s", stmt.Tok)
				}
			}
		case *ast.CallExpr:
			name := pc0CallFunName(stmt)
			for i, arg := range stmt.Args {
				if !pc0ExprReferencesUpdates(arg) {
					continue
				}
				exact := pc0IdentNamed(arg, "updates")
				switch {
				case name == "append" && i == 0 && exact:
					pc0RequireUpdateLibraryAppendFragments(t, stmt, bindings)
				case name == "len" && i == 0 && exact:
				default:
					t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: updates passed to %s", name)
				}
			}
		}
		return true
	})
	if emptyInits != 1 {
		t.Errorf("PC0 HEAD SERIAL: allowlisted UpdateLibrary unresolved Query shape: empty []string{} initializer count=%d, want 1", emptyInits)
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

func pc0IsFmtErrorf(call *ast.CallExpr) bool {
	if call == nil {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Errorf" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == "fmt"
}

func pc0MigratorMFStatementsIdent(expr ast.Expr) *ast.Ident {
	sel, ok := pc0UnwrapParen(expr).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Statements" {
		return nil
	}
	ident, ok := pc0UnwrapParen(sel.X).(*ast.Ident)
	if !ok || ident.Name != "mf" {
		return nil
	}
	return ident
}

func pc0IsMigratorApplyStatementsRange(rng *ast.RangeStmt) bool {
	return rng != nil && rng.Tok == token.DEFINE && pc0IdentNamed(rng.Key, "i") && pc0IdentNamed(rng.Value, "stmt") && pc0MigratorMFStatementsIdent(rng.X) != nil
}

func pc0RequireMigratorApplyIdentWhitelist(t *testing.T, scope pc0QueryScope, statementsRange *ast.RangeStmt) {
	t.Helper()
	const wantErrorf = "statement %d/%d failed: %w\nCQL: %.300s"
	allowed := map[*ast.Ident]struct{}{}
	fn, ok := scope.node.(*ast.FuncDecl)
	if ok && fn.Type != nil && fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			for _, name := range field.Names {
				if name.Name == "mf" {
					allowed[name] = struct{}{}
				}
			}
		}
	}
	if statementsRange != nil {
		if ident := pc0MigratorMFStatementsIdent(statementsRange.X); ident != nil {
			allowed[ident] = struct{}{}
		}
		pc0MarkUpdateLibraryIdent(allowed, statementsRange.Value)
	}
	ast.Inspect(scope.node, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := pc0CallFunName(call)
		if name == "len" && len(call.Args) == 1 {
			if ident := pc0MigratorMFStatementsIdent(call.Args[0]); ident != nil {
				allowed[ident] = struct{}{}
			}
		}
		if name == "stamp" && len(call.Args) == 1 && pc0IdentNamed(call.Args[0], "mf") {
			pc0MarkUpdateLibraryIdent(allowed, call.Args[0])
		}
		if pc0IsCQLEntryPoint(name, len(call.Args)) && len(call.Args) > 0 && pc0IdentNamed(call.Args[0], "stmt") && pc0CallInsideRangeNotClosure(statementsRange, call) {
			pc0MarkUpdateLibraryIdent(allowed, call.Args[0])
		}
		if pc0IsFmtErrorf(call) && len(call.Args) == 5 && pc0IdentNamed(call.Args[4], "stmt") && pc0CallInsideRangeNotClosure(statementsRange, call) {
			if format, ok := pc0ResolveStringExpr(call.Args[0], nil); ok && format == wantErrorf {
				pc0MarkUpdateLibraryIdent(allowed, call.Args[4])
			}
		}
		return true
	})
	var stack []ast.Node
	ast.Inspect(scope.node, func(node ast.Node) bool {
		if node == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return true
		}
		if ident, ok := node.(*ast.Ident); ok {
			switch ident.Name {
			case "mf", "stmt":
				if !pc0IdentIsSelectorSel(stack, ident) {
					if _, ok := allowed[ident]; !ok {
						t.Errorf("PC0 HEAD SERIAL: allowlisted Migrator.apply unresolved Query shape: ident %s is used outside the pinned shape", ident.Name)
					}
				}
			}
		}
		stack = append(stack, node)
		return true
	})
}

func pc0RequireMigratorApplyUnresolvedShape(t *testing.T, scope pc0QueryScope) {
	t.Helper()
	var statementsRange *ast.RangeStmt
	ranges := 0
	ast.Inspect(scope.node, func(node ast.Node) bool {
		rng, ok := node.(*ast.RangeStmt)
		if !ok || !pc0IsMigratorApplyStatementsRange(rng) {
			return true
		}
		ranges++
		statementsRange = rng
		return true
	})
	if ranges != 1 {
		t.Errorf("PC0 HEAD SERIAL: allowlisted Migrator.apply unresolved Query shape: for i, stmt := range mf.Statements count=%d, want 1", ranges)
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
			return
		}
		if !pc0CallInsideRangeNotClosure(statementsRange, call) {
			t.Errorf("PC0 HEAD SERIAL: allowlisted Migrator.apply unresolved Query shape: Query(stmt) is not inside for i, stmt := range mf.Statements")
		}
	})
	if unresolvedQueryArgs != 1 {
		t.Errorf("PC0 HEAD SERIAL: allowlisted Migrator.apply unresolved Query shape: unresolved Query count=%d, want 1", unresolvedQueryArgs)
	}
	pc0RequireMigratorApplyIdentWhitelist(t, scope, statementsRange)
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

	serialCount := chain.counts["SerialConsistency"]
	args := chain.args["SerialConsistency"]
	if serialCount == 0 || len(args) != 1 {
		t.Fatalf("PC0 HEAD SERIAL: %s in %s must call SerialConsistency(LibraryHeadSerialConsistency) on the HEAD LWT chain (session default is not the canonical domain)", op.decl, op.path)
	}
	if serialCount != 1 {
		t.Fatalf("PC0 HEAD SERIAL: %s in %s SerialConsistency count=%d, want exactly 1; later SerialConsistency(localSerial) would win at runtime while the inner pin stayed visible to an overwriting scanner", op.decl, op.path, serialCount)
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

func pc0QueryMethodChain(terminal *ast.CallExpr) pc0QueryChain {
	chain := pc0QueryChain{
		args:   map[string][]ast.Expr{},
		counts: map[string]int{},
	}
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
		name := selector.Sel.Name
		chain.counts[name]++
		// Walk from the CAS terminal inward. The first occurrence of a method
		// is the outermost call, which is the last one the driver applies.
		// Still count every call: a second SerialConsistency must not vanish
		// just because an inner LibraryHeadSerialConsistency pin remains.
		if _, seen := chain.args[name]; !seen {
			chain.args[name] = call.Args
		}
		expression = selector.X
	}
	return chain
}

func pc0ChainQueryCQL(t *testing.T, op pc0HeadSerialDomainOp, chain pc0QueryChain) string {
	t.Helper()
	args, ok := chain.args["Query"]
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

type pc0QueryChain struct {
	args   map[string][]ast.Expr
	counts map[string]int
}

func pc0NodeText(t *testing.T, node ast.Node) string {
	t.Helper()
	var output bytes.Buffer
	if err := format.Node(&output, token.NewFileSet(), node); err != nil {
		t.Fatalf("format AST node: %v", err)
	}
	return output.String()
}

func TestPC0QueryMethodChainCountsRepeatedSerialConsistency(t *testing.T) {
	src := `package example
func f() {
	session.Query("UPDATE libraries SET head_commit_id = ? IF EXISTS").
		SerialConsistency(db.LibraryHeadSerialConsistency).
		SerialConsistency(localSerial).
		MapScanCAS(cas)
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "example.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var terminal *ast.CallExpr
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "MapScanCAS" {
			return true
		}
		terminal = call
		return false
	})
	if terminal == nil {
		t.Fatal("MapScanCAS terminal not found")
	}
	chain := pc0QueryMethodChain(terminal)
	if chain.counts["SerialConsistency"] != 2 {
		t.Fatalf("SerialConsistency count=%d, want 2; an overwriting map would hide the outer localSerial pin", chain.counts["SerialConsistency"])
	}
	args := chain.args["SerialConsistency"]
	if len(args) != 1 {
		t.Fatalf("outer SerialConsistency args=%d, want 1", len(args))
	}
	ident, ok := args[0].(*ast.Ident)
	if !ok || ident.Name != "localSerial" {
		t.Fatalf("outer SerialConsistency argument = %s, want ident localSerial (runtime last-write)", pc0NodeText(t, args[0]))
	}
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
