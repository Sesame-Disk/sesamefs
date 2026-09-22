package db

import (
	"go/ast"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// PC-D1B no-bypass inventory. The identity-authority claim is only worth
// anything if every production route that creates, changes or removes a
// `commits` / `fs_objects` identity goes through it. A claim table a writer can
// sidestep proves nothing, so this guard freezes the inventory: a new writer or
// deleter that is not listed here turns red, and whoever adds it has to decide
// consciously whether it participates in the protocol.
//
// This inventory is paired with the gateway caller and literal-CQL guards below.
// Every semantic source statement is confined to the audited gateway; the
// repo-wide scan fails when a new writer, deleter, or extra statement appears.

// See docs/PC-D1B-METADATA-IDENTITY-AUTHORITY.md and
// ISSUE-PCD1B-METADATA-IDENTITY-AUTHORITY-01.

type identityWriteShape string

const (
	// identityWriteInsert is a plain INSERT. Cassandra treats it as an upsert,
	// which is exactly why a claim is needed.
	identityWriteInsert identityWriteShape = "insert"
	// identityWriteInsertLWT is INSERT ... IF NOT EXISTS: first-writer-wins for
	// that one endpoint, but not a protocol shared with every writer.
	identityWriteInsertLWT identityWriteShape = "insert-lwt"
	// identityWriteUpdate changes at least one semantic identity field in the
	// V1 projection (fs-object fields plus commit root/parent/creator/description/time).
	identityWriteUpdate identityWriteShape = "update"
	// identityWriteDisplayOnly touches only obj_name / full_path / mtime, which
	// the decision excludes from the identity projection. Inventoried so the
	// exclusion is a recorded decision, not an oversight.
	identityWriteDisplayOnly identityWriteShape = "display-only"
	identityWriteDelete      identityWriteShape = "delete"
)

type identityWriter struct {
	path  string
	decl  string
	shape identityWriteShape
	note  string
}

// Every production statement that writes or removes a `commits` / `fs_objects`
// row, keyed by the declaration that contains the literal. Shapes are recorded
// as they are today, before any of them is migrated onto the claim: the point of
// the inventory is that the migration has to visit all of them.
//
// The list was derived from the source, not written from memory; the inventory
// test fails on any drift in either direction.
var identityExpectedWriters = []identityWriter{
	{path: "internal/db/identity_gateway.go", decl: "AddAuthorizedCommitToBatch", shape: identityWriteInsert},
	{path: "internal/db/identity_gateway.go", decl: "AddAuthorizedFSObjectToBatch", shape: identityWriteInsert},
	{path: "internal/db/identity_gateway.go", decl: "DeleteCommitIdentity", shape: identityWriteDelete},
	{path: "internal/db/identity_gateway.go", decl: "DeleteFSObjectIdentity", shape: identityWriteDelete},
	{path: "internal/db/identity_gateway.go", decl: "AddUnpublishedLibraryIdentityPartitionDeletesToBatch", shape: identityWriteDelete},
	{path: "internal/api/sync.go", decl: "SyncHandler.updateFullPaths", shape: identityWriteDisplayOnly},
	{path: "cmd/sesamefs/main.go", decl: "runBackfillSearchIndex", shape: identityWriteDisplayOnly},
}

var identityExpectedStatementCounts = map[string]int{
	"internal/db/identity_gateway.go:AddAuthorizedCommitToBatch":                           1,
	"internal/db/identity_gateway.go:AddAuthorizedFSObjectToBatch":                         6,
	"internal/db/identity_gateway.go:DeleteCommitIdentity":                                 1,
	"internal/db/identity_gateway.go:DeleteFSObjectIdentity":                               1,
	"internal/db/identity_gateway.go:AddUnpublishedLibraryIdentityPartitionDeletesToBatch": 2,
	"internal/api/sync.go:SyncHandler.updateFullPaths":                                     1,
	"cmd/sesamefs/main.go:runBackfillSearchIndex":                                          1,
}

var identityExpectedGatewayCallers = []struct {
	path, decl string
	calls      []string
}{
	{"internal/api/sync.go", "SyncHandler.createInitialCommit", []string{"AuthorizeFSObjectProjection", "MaterializeAuthorizedFSObject", "AuthorizeCommitProjection", "MaterializeAuthorizedCommit"}},
	{"internal/api/sync.go", "SyncHandler.PutCommit", []string{"AuthorizeCommitProjection", "MaterializeAuthorizedCommit"}},
	{"internal/api/sync.go", "SyncHandler.createSyncAutoMergeCommit", []string{"AuthorizeCommitProjection", "MaterializeAuthorizedCommit"}},
	{"internal/api/sync.go", "SyncHandler.createSyncDirectoryFSObject", []string{"AuthorizeFSObjectProjection", "MaterializeAuthorizedFSObject"}},
	{"internal/api/sync.go", "SyncHandler.storeSyncFSObject", []string{"VerifyFSObjectProjection", "AuthorizeFSObjectProjection", "MaterializeAuthorizedFSObject"}},
	{"internal/api/seafhttp.go", "SeafHTTPHandler.createPendingSeafHTTPFileFSObject", []string{"AuthorizeFSObjectProjection", "MaterializeAuthorizedFSObject"}},
	{"internal/api/seafhttp.go", "SeafHTTPHandler.commitUploadedFileMultiBlockOnce", []string{"AuthorizeCommitProjection", "MaterializeAuthorizedCommit"}},
	{"internal/api/seafhttp.go", "SeafHTTPHandler.commitUploadedFileOnce", []string{"AuthorizeCommitProjection", "MaterializeAuthorizedCommit"}},
	{"internal/api/seafhttp.go", "SeafHTTPHandler.createDirectoryFSObject", []string{"AuthorizeFSObjectProjection", "MaterializeAuthorizedFSObject"}},
	{"internal/api/v2/fs_helpers.go", "FSHelper.CreateDirectoryFSObject", []string{"AuthorizeFSObjectProjection", "MaterializeAuthorizedFSObject"}},
	{"internal/api/v2/fs_helpers.go", "FSHelper.insertCommit", []string{"AuthorizeCommitProjection", "MaterializeAuthorizedCommit"}},
	{"internal/api/v2/fs_helpers.go", "FSHelper.InitializeLibraryFS", []string{"AuthorizeFSObjectProjection", "AuthorizeCommitProjection", "AddAuthorizedFSObjectToBatch", "AddAuthorizedCommitToBatch"}},
	{"internal/api/v2/fs_helpers.go", "FSHelper.createFileFSObjectRow", []string{"AuthorizeFSObjectProjection", "MaterializeAuthorizedFSObject"}},
	{"internal/api/v2/libraries.go", "LibraryHandler.CreateLibrary", []string{"AuthorizeFSObjectProjection", "AuthorizeCommitProjection", "AddAuthorizedFSObjectToBatch", "AddAuthorizedCommitToBatch"}},
	{"internal/api/v2/admin_libraries.go", "AdminHandler.AdminCreateLibrary", []string{"AuthorizeFSObjectProjection", "AuthorizeCommitProjection", "AddAuthorizedFSObjectToBatch", "AddAuthorizedCommitToBatch"}},
	{"internal/api/v2/library_rollback.go", "cleanupRolledBackLibraryDerivedState", []string{"AddUnpublishedLibraryIdentityPartitionDeletesToBatch"}},
	{"internal/api/v2/fs_helpers.go", "FSHelper.InitializeLibraryHeadIfUnset", []string{"DeleteCommitIdentity"}},
	{"internal/api/v2/fs_helpers.go", "DiscardLosingInitialCommit", []string{"DeleteCommitIdentity"}},
	{"internal/api/v2/publish_repair.go", "cleanupFailedPublishDeleteCommitFn", []string{"DeleteCommitIdentity"}},
	{"internal/api/v2/publish_repair.go", "cleanupFailedPublishDeleteFSObjectFn", []string{"DeleteFSObjectIdentity"}},
	{"internal/gc/store_cassandra.go", "CassandraStore.DeleteCommit", []string{"DeleteCommitIdentity"}},
	{"internal/gc/store_cassandra.go", "CassandraStore.DeleteFSObject", []string{"DeleteFSObjectIdentity"}},
}
var identityStatementPattern = regexp.MustCompile(`(?is)(INSERT\s+INTO|UPDATE|DELETE\s+FROM)\s+(commits|fs_objects)\b`)

func identityShapeOf(literal string) identityWriteShape {
	upper := strings.ToUpper(literal)
	switch {
	case strings.Contains(upper, "DELETE FROM"):
		return identityWriteDelete
	case strings.Contains(upper, "INSERT INTO"):
		if strings.Contains(upper, "IF NOT EXISTS") {
			return identityWriteInsertLWT
		}
		return identityWriteInsert
	default:
		if identitySemanticFieldPattern.MatchString(literal) {
			return identityWriteUpdate
		}
		return identityWriteDisplayOnly
	}
}

// identitySemanticFieldPattern matches an UPDATE that touches a field inside the
// frozen identity projection. An UPDATE that sets none of these is display-only.
var identitySemanticFieldPattern = regexp.MustCompile(`(?is)\bSET\b[^;]*\b(obj_type|size_bytes|dir_entries|block_ids|seafile_block_ids_sha1|root_fs_id|parent_id|creator_id|description|created_at)\s*=`)

// identityStatementLiterals returns every production string literal that writes
// or deletes a commits/fs_objects row, keyed by the declaration containing it.
func identityStatementLiterals(t *testing.T, roots ...string) map[string][]string {
	t.Helper()
	repoRoot := r3RepositoryRoot(t)
	hits := map[string][]string{}
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
			for _, decl := range file.Decls {
				name := pc0HeadColumnDeclName(decl)
				ast.Inspect(decl, func(node ast.Node) bool {
					lit, ok := node.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						return true
					}
					if identityStatementPattern.MatchString(lit.Value) {
						hits[pc0CallerKey(relPath, name)] = append(hits[pc0CallerKey(relPath, name)], lit.Value)
					}
					return true
				})
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("PCD1B IDENTITY: walk %s: %v", root, walkErr)
		}
	}
	return hits
}

// TestIdentityWritersAreInventoried is the no-bypass guard. A production route
// that writes or removes a commits/fs_objects identity and is not listed must
// turn red: the wiring PR has to cover every one of them, and a route added
// afterwards must not slip in outside the protocol.
func TestIdentityWritersAreInventoried(t *testing.T) {
	hits := identityStatementLiterals(t, "internal", "cmd")

	expected := map[string][]identityWriter{}
	for _, writer := range identityExpectedWriters {
		key := pc0CallerKey(writer.path, writer.decl)
		expected[key] = append(expected[key], writer)
	}

	var unlisted []string
	for key := range hits {
		if _, listed := expected[key]; !listed {
			unlisted = append(unlisted, key)
		}
	}
	sort.Strings(unlisted)
	if len(unlisted) > 0 {
		t.Fatalf("PCD1B IDENTITY: unlisted commits/fs_objects writer or deleter %v; every route that creates, changes or removes one of these identities must be inventoried in identityExpectedWriters and must decide whether it participates in the identity-authority protocol (ISSUE-PCD1B-METADATA-IDENTITY-AUTHORITY-01)", unlisted)
	}

	var missing []string
	for key := range expected {
		if _, found := hits[key]; !found {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("PCD1B IDENTITY: inventoried writers no longer found: %v; the inventory has drifted from the source", missing)
	}
}

// Every field in the frozen identity projection must classify a commits
// UPDATE as semantic. This specifically prevents commit V1 fields from being
// silently recorded as display-only by the source inventory.
func TestIdentitySemanticUpdatesAreClassified(t *testing.T) {
	for _, field := range []string{
		"root_fs_id", "parent_id", "creator_id", "description", "created_at",
		"obj_type", "size_bytes", "dir_entries", "block_ids", "seafile_block_ids_sha1",
	} {
		t.Run(field, func(t *testing.T) {
			statement := "UPDATE commits SET " + field + " = ? WHERE library_id = ? AND commit_id = ?"
			if got := identityShapeOf(statement); got != identityWriteUpdate {
				t.Fatalf("UPDATE of semantic field %s classified as %s, want %s", field, got, identityWriteUpdate)
			}
		})
	}
}

// TestIdentityWriterShapesAreFrozen records what each route does today. A shape
// change is not necessarily wrong, but it must be a decision: turning an
// ordinary INSERT into an LWT, or adding a delete where there was none, changes
// what the claim protocol has to fence.
func TestIdentityWriterShapesAreFrozen(t *testing.T) {
	hits := identityStatementLiterals(t, "internal", "cmd")
	expected := map[string]map[identityWriteShape]bool{}
	for _, writer := range identityExpectedWriters {
		key := pc0CallerKey(writer.path, writer.decl)
		if expected[key] == nil {
			expected[key] = map[identityWriteShape]bool{}
		}
		expected[key][writer.shape] = true
	}
	var mismatched []string
	for key, literals := range hits {
		shapes, listed := expected[key]
		if !listed {
			continue
		}
		if count, ok := identityExpectedStatementCounts[key]; !ok || len(literals) != count {
			mismatched = append(mismatched, key+" has an unexpected statement count")
		}
		for _, literal := range literals {
			if shape := identityShapeOf(literal); !shapes[shape] {
				mismatched = append(mismatched, key+" now has a "+string(shape)+" statement")
			}
			if identityShapeOf(literal) == identityWriteDisplayOnly && !identityDisplayOnlySetAllowed(literal) {
				mismatched = append(mismatched, key+" display-only SET fields escaped the exact allowlist")
			}
		}
	}
	for key := range identityExpectedStatementCounts {
		if _, ok := hits[key]; !ok {
			mismatched = append(mismatched, key+" is missing")
		}
	}
	sort.Strings(mismatched)
	if len(mismatched) > 0 {
		t.Fatalf("PCD1B IDENTITY: semantic CQL gateway inventory drift: %v; every statement must stay inside the reviewed gateway and every display-only SET must remain exact", mismatched)
	}
}

func identityDisplayOnlySetAllowed(statement string) bool {
	upper := strings.ToUpper(statement)
	setAt := strings.Index(upper, " SET ")
	if setAt < 0 {
		return false
	}
	whereOffset := strings.Index(upper[setAt+5:], " WHERE ")
	if whereOffset < 0 {
		return false
	}
	assignments := strings.Split(statement[setAt+5:setAt+5+whereOffset], ",")
	allowed := map[string]bool{"obj_name": true, "full_path": true, "mtime": true}
	for _, assignment := range assignments {
		parts := strings.Split(assignment, "=")
		if len(parts) != 2 || !allowed[strings.ToLower(strings.TrimSpace(parts[0]))] || strings.TrimSpace(parts[1]) != "?" {
			return false
		}
	}
	return len(assignments) > 0
}
func TestIdentityAuthorityPrimitiveHasNoRawProductionCallerOutsideGateway(t *testing.T) {
	repoRoot := r3RepositoryRoot(t)
	var consumers []string
	for _, root := range []string{"internal", "cmd"} {
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
			if relPath == "internal/db/identity_authority.go" || relPath == "internal/db/identity_gateway.go" {
				return nil
			}
			file := r3ParseProductionFile(t, path)
			ast.Inspect(file, func(node ast.Node) bool {
				ident, ok := node.(*ast.Ident)
				if !ok {
					return true
				}
				switch ident.Name {
				case "ClaimIdentityAuthority", "ClaimIdentityAuthorityAt", "VerifyIdentityAuthority", "ReadIdentityAuthority":
					consumers = append(consumers, relPath+":"+ident.Name)
				}
				return true
			})
			return nil
		})
		if walkErr != nil {
			t.Fatalf("PCD1B IDENTITY: walk %s: %v", root, walkErr)
		}
	}
	sort.Strings(consumers)
	if len(consumers) > 0 {
		t.Fatalf("PCD1B IDENTITY: raw authority primitive has production consumers outside the gateway: %v", consumers)
	}
}

func TestIdentityProductionWritersUseGateway(t *testing.T) {
	repoRoot := r3RepositoryRoot(t)
	parsed := map[string]*ast.File{}
	for _, expected := range identityExpectedGatewayCallers {
		file := parsed[expected.path]
		if file == nil {
			file = r3ParseProductionFile(t, filepath.Join(repoRoot, filepath.FromSlash(expected.path)))
			parsed[expected.path] = file
		}
		var target ast.Decl
		for _, decl := range file.Decls {
			if pc0HeadColumnDeclName(decl) == expected.decl {
				target = decl
				break
			}
		}
		if target == nil {
			t.Errorf("PCD1B IDENTITY: wired producer %s::%s disappeared", expected.path, expected.decl)
			continue
		}
		found := map[string]bool{}
		ast.Inspect(target, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
				found[selector.Sel.Name] = true
			}
			return true
		})
		for _, name := range expected.calls {
			if !found[name] {
				t.Errorf("PCD1B IDENTITY: %s::%s bypasses gateway call %s", expected.path, expected.decl, name)
			}
		}
	}
}

func TestIdentityDynamicCQLIsClosed(t *testing.T) {
	repoRoot := r3RepositoryRoot(t)
	for _, root := range []string{"internal", "cmd"} {
		walkErr := filepath.WalkDir(filepath.Join(repoRoot, root), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") || filepath.ToSlash(path) == filepath.ToSlash(filepath.Join(repoRoot, "internal", "db", "identity_gateway.go")) {
				return nil
			}
			file := r3ParseProductionFile(t, path)
			for _, decl := range file.Decls {
				declName := pc0HeadColumnDeclName(decl)
				hasIdentityLiteral := false
				ast.Inspect(decl, func(node ast.Node) bool {
					lit, ok := node.(*ast.BasicLit)
					if ok && lit.Kind == token.STRING && identityStatementPattern.MatchString(lit.Value) {
						hasIdentityLiteral = true
					}
					return true
				})
				if !hasIdentityLiteral {
					continue
				}
				ast.Inspect(decl, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if !ok {
						return true
					}
					selector, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || selector.Sel.Name != "Query" || len(call.Args) == 0 {
						return true
					}
					if _, literal := call.Args[0].(*ast.BasicLit); !literal {
						t.Errorf("PCD1B IDENTITY: dynamic Query in identity declaration %s", declName)
					}
					return true
				})
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("PCD1B IDENTITY: walk %s: %v", root, walkErr)
		}
	}
}

func TestIdentityGatewayCQLIsLiteralAndBounded(t *testing.T) {
	repoRoot := r3RepositoryRoot(t)
	file := r3ParseProductionFile(t, filepath.Join(repoRoot, "internal/db/identity_gateway.go"))
	var failures []string
	for _, decl := range file.Decls {
		name := pc0HeadColumnDeclName(decl)
		if name != "AddAuthorizedCommitToBatch" && name != "AddAuthorizedFSObjectToBatch" &&
			name != "DeleteCommitIdentity" && name != "DeleteFSObjectIdentity" &&
			name != "AddUnpublishedLibraryIdentityPartitionDeletesToBatch" {
			continue
		}
		ast.Inspect(decl, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Query" {
				return true
			}
			if len(call.Args) == 0 {
				failures = append(failures, name+" has an unresolved Query")
				return true
			}
			if _, literal := call.Args[0].(*ast.BasicLit); !literal {
				failures = append(failures, name+" has dynamic or unresolved CQL")
			}
			return true
		})
	}
	if len(failures) > 0 {
		t.Fatalf("PCD1B IDENTITY: %v", failures)
	}
}
