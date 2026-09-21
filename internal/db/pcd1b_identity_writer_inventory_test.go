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
// This is the foundation of the fence, not the fence itself. No call site is
// wired to ClaimIdentityAuthority in this PR; the primitive lands authority-only
// the way PC-D1A landed its witness CAS. Wiring is the follow-up, and this
// inventory is what keeps the follow-up honest about how many sites it has to
// cover.
//
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
	// --- Sync -----------------------------------------------------------------
	{path: "internal/api/sync.go", decl: "SyncHandler.createInitialCommit", shape: identityWriteInsert},
	{path: "internal/api/sync.go", decl: "SyncHandler.PutCommit", shape: identityWriteInsertLWT,
		note: "PR #208 first-writer-wins; the client-supplied id is checked against the request path, not recomputed"},
	{path: "internal/api/sync.go", decl: "SyncHandler.createSyncAutoMergeCommit", shape: identityWriteInsert},
	{path: "internal/api/sync.go", decl: "SyncHandler.createSyncDirectoryFSObject", shape: identityWriteInsert},
	{path: "internal/api/sync.go", decl: "SyncHandler.storeSyncFSObject", shape: identityWriteInsert,
		note: "RecvFS: LOCAL_QUORUM read plus an ordinary write, no per-object Paxos; writes the wire SHA-1 list into block_ids and leaves seafile_block_ids_sha1 unset, which is the SHA-1-only shape"},
	{path: "internal/api/sync.go", decl: "SyncHandler.storeSyncFSObject", shape: identityWriteUpdate,
		note: "completes a metadata-only placeholder with identity fields"},
	{path: "internal/api/sync.go", decl: "SyncHandler.updateFullPaths", shape: identityWriteDisplayOnly},

	// --- SeafHTTP ------------------------------------------------------------
	{path: "internal/api/seafhttp.go", decl: "SeafHTTPHandler.commitUploadedFileOnce", shape: identityWriteInsert},
	{path: "internal/api/seafhttp.go", decl: "SeafHTTPHandler.commitUploadedFileMultiBlockOnce", shape: identityWriteInsert},
	{path: "internal/api/seafhttp.go", decl: "SeafHTTPHandler.createDirectoryFSObject", shape: identityWriteInsert},
	{path: "internal/api/seafhttp.go", decl: "SeafHTTPHandler.createPendingSeafHTTPFileFSObject", shape: identityWriteInsert},

	// --- v2 ------------------------------------------------------------------
	{path: "internal/api/v2/fs_helpers.go", decl: "FSHelper.insertCommit", shape: identityWriteInsert},
	{path: "internal/api/v2/fs_helpers.go", decl: "FSHelper.InitializeLibraryFS", shape: identityWriteInsert},
	{path: "internal/api/v2/fs_helpers.go", decl: "FSHelper.CreateDirectoryFSObject", shape: identityWriteInsert},
	{path: "internal/api/v2/fs_helpers.go", decl: "FSHelper.createFileFSObjectRow", shape: identityWriteInsert},
	{path: "internal/api/v2/libraries.go", decl: "LibraryHandler.CreateLibrary", shape: identityWriteInsert},
	{path: "internal/api/v2/admin_libraries.go", decl: "AdminHandler.AdminCreateLibrary", shape: identityWriteInsert},

	// --- CLI -----------------------------------------------------------------
	{path: "cmd/sesamefs/main.go", decl: "runBackfillSearchIndex", shape: identityWriteDisplayOnly,
		note: "search-index backfill: obj_name/full_path only"},

	// --- deleters --------------------------------------------------------------
	// A delete does not remove the claim: the claim outlives its row so a
	// re-created key cannot mint fresh provenance. These are inventoried so the
	// wiring PR cannot forget that removal is part of the protocol.
	{path: "internal/api/v2/library_rollback.go", decl: "cleanupRolledBackLibraryDerivedState", shape: identityWriteDelete,
		note: "whole-partition teardown of a library that never published a HEAD"},
	{path: "internal/api/v2/publish_repair.go", decl: "cleanupFailedPublishDeleteCommitFn", shape: identityWriteDelete,
		note: "failed publish attempt's commit"},
	{path: "internal/api/v2/publish_repair.go", decl: "cleanupFailedPublishDeleteFSObjectFn", shape: identityWriteDelete,
		note: "declared but has NO production caller: CleanupFailedPublishArtifacts receives fsIDs and never deletes them"},
	{path: "internal/api/v2/fs_helpers.go", decl: "FSHelper.InitializeLibraryHeadIfUnset", shape: identityWriteDelete,
		note: "discards an attempt-unique commit after a definitively non-applied CAS"},
	{path: "internal/api/v2/fs_helpers.go", decl: "DiscardLosingInitialCommit", shape: identityWriteDelete,
		note: "refuses outright when the id equals the winning HEAD"},
	{path: "internal/gc/store_cassandra.go", decl: "CassandraStore.DeleteCommit", shape: identityWriteDelete,
		note: "GC expired-version cascade; the only path that can reach a HEAD-reachable identity (ISSUE-GC-PHASE5-CASCADE-SHARED-FSOBJECTS-01), dormant under GC_ENABLED=false"},
	{path: "internal/gc/store_cassandra.go", decl: "CassandraStore.DeleteFSObject", shape: identityWriteDelete,
		note: "same cascade"},
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
			continue // reported by TestIdentityWritersAreInventoried
		}
		for _, literal := range literals {
			if shape := identityShapeOf(literal); !shapes[shape] {
				mismatched = append(mismatched, key+" now has a "+string(shape)+" statement")
			}
		}
	}
	sort.Strings(mismatched)
	if len(mismatched) > 0 {
		t.Fatalf("PCD1B IDENTITY: statement shape changed for %v; update identityExpectedWriters and re-check what the identity-authority protocol must fence", mismatched)
	}
}

// TestIdentityAuthorityHasNoProductionConsumerYet freezes the scope of this PR.
// The primitive lands authority-only: if a production call site starts claiming
// or verifying identities, the wiring PR has arrived and this guard must be
// replaced by the real fence rather than silently left passing.
func TestIdentityAuthorityHasNoProductionConsumerYet(t *testing.T) {
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
			if relPath == "internal/db/identity_authority.go" {
				return nil
			}
			file := r3ParseProductionFile(t, path)
			ast.Inspect(file, func(node ast.Node) bool {
				ident, ok := node.(*ast.Ident)
				if !ok {
					return true
				}
				switch ident.Name {
				case "ClaimIdentityAuthority", "VerifyIdentityAuthority", "ReadIdentityAuthority":
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
		t.Fatalf("PCD1B IDENTITY: the authority primitive now has production consumers %v; this PR lands it authority-only, so the wiring PR must replace this guard with the real no-bypass fence instead of leaving it passing", consumers)
	}
}
