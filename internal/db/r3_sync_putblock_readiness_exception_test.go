package db

import (
	"go/ast"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestR3SyncPutBlockReadinessDeclaredExceptionIsFrozen freezes the one
// explicit, intentional exception #206 carves out of the R3 hot-path
// "canonical/orphan authority reads added = 0" baseline documented in
// docs/R3-LIVENESS-CONTINUITY.md's "Hot-path performance contract" section.
//
// ensureSyncCommitBlockPublicationReadiness (internal/api/sync.go, called
// from both handleSyncHeadPromotion and tryAutoMergeSyncHeadPromotion before
// the HEAD CAS) is deliberately outside TestR3PublicationHotPathIsFailClosed's
// roots -- which only start from stageSyncCommitBlockDelta and
// finalizeSyncCommitBlockDelta, neither of which calls it -- and is only
// scanned flatly, not walked into, by
// TestR3PublicationStageToHeadHasNoUnlistedDirectDBCalls's stage->HEAD span
// check (that check flags a direct h.db.Method call or a new CQL entry point
// appearing literally between the stage and HEAD calls, but a call to this
// function's own name is neither). Neither existing guard therefore ever
// walks into the O(N)-per-provenanced-block cost this function adds through
// its own db-package calls, which is exactly how that cost went undeclared
// in the original PR.
//
// This test closes that specific gap for WHICH internal/db primitives are
// reachable: it walks the same type-aware interprocedural graph
// TestR3PublicationHotPathTypedReceiversAndCQLBudget already builds for the
// other guarded roots (same r3BuildTypedProgram, r3TypedCallTargets,
// r3TypedExprType machinery), so a call reached only through a package-level
// function-variable indirection or a struct-field method value cannot hide
// from it either. Unlike that test, it does not fail merely because an
// authority-shaped read is reachable -- accepting that is this root's whole
// declared purpose. Instead it freezes the EXACT reachable db-package
// surface as an allow-list and fails closed on anything else: an unlisted
// db call, or an unresolved method on a tracked receiver type. A future
// change that adds another db call or swaps one out must update the
// allow-list below and docs/R3-LIVENESS-CONTINUITY.md in the same change --
// it cannot silently pass by adding one more layer of helper indirection.
// (ISSUE-SYNC-PUTBLOCK-CROSS-DC-PROVENANCE-VISIBILITY-01's fix is exactly
// such an update: it added BlockReferenceExistsEachQuorum to the allow-list
// below in the same change that introduced the call.)
//
// WHAT THIS DOES NOT COVER. It stops at the internal/db method boundary by
// design (see the "continue" below) and does not descend into
// BlockReferenceExistsLocalQuorum, BlockReferenceExistsEachQuorum,
// ProbeBlockReuse, AddProvisionalBlockReferenceWithExpiry, or
// ValidateBorrowedFSPublicationAuthority themselves. The SERIAL/EACH_QUORUM
// identifier check below only inspects the walked internal/api-side code
// between this root and that boundary -- it would catch a new raw
// gocql.EachQuorum/gocql.Serial reference added directly to that wrapper
// code, but it cannot see a consistency level or an added internal call a
// future change makes inside one of those five functions' own bodies. That
// is the job of each primitive's own existing, narrower tests --
// TestValidateBorrowedFSPublicationAuthorityUsesAdvisoryReads and
// TestP3FenceReadConsistencyIsLocalQuorum pin advisory/fence read
// consistency, TestBlockReferenceProducersPinWriteConsistency pins every
// block_references writer including the one inside
// AddProvisionalBlockReferenceWithExpiry, and
// TestSyncBlockReferenceCrossDCFallbackConsistencyIsEachQuorum pins
// BlockReferenceExistsEachQuorum's named SyncBlockReferenceCrossDCFallbackConsistency
// constant to gocql.EachQuorum -- not of this one.
func TestR3SyncPutBlockReadinessDeclaredExceptionIsFrozen(t *testing.T) {
	root := r3RepositoryRoot(t)
	const module = "github.com/Sesame-Disk/sesamefs"
	packages := []r3ProgramPackage{
		{importPath: module + "/internal/db", directory: filepath.Join(root, "internal", "db")},
		{importPath: module + "/internal/api/v2", directory: filepath.Join(root, "internal", "api", "v2")},
		{importPath: module + "/internal/api", directory: filepath.Join(root, "internal", "api")},
	}
	program := r3BuildTypedProgram(t, packages)
	rootSymbol := r3ProgramSymbol{pkg: module + "/internal/api", name: "ensureSyncCommitBlockPublicationReadiness"}

	// The declared, reviewed allow-list. Every entry is documented in
	// docs/R3-LIVENESS-CONTINUITY.md's "Hot-path performance contract" as
	// part of the Sync PutBlock-provenanced readiness exception. Four are
	// LOCAL_QUORUM or session-inherited (production: LOCAL_QUORUM). The fifth,
	// BlockReferenceExistsEachQuorum, is a DELIBERATE, SCOPED EACH_QUORUM
	// exception (ISSUE-SYNC-PUTBLOCK-CROSS-DC-PROVENANCE-VISIBILITY-01): it
	// never runs on the fast path (a local hit or a local error both settle
	// the answer first) and is reached only when a local read cleanly
	// reports absent, so it is bounded by the local-miss rate, not by every
	// provenanced block. This is the one intentional exception to the "no
	// new per-block EACH_QUORUM operation" line elsewhere in this doc.
	allowedDBCalls := map[string]string{
		"BlockReferenceExistsLocalQuorum":        "scope gate fast path: does this block already have live up:sync:<repo>:<block> provenance, same-DC",
		"BlockReferenceExistsEachQuorum":          "scope gate cross-DC fallback, reached only on a clean local miss -- the one deliberate EACH_QUORUM exception here",
		"ProbeBlockReuse":                        "resolve current physical placement for a provenanced block",
		"AddProvisionalBlockReferenceWithExpiry": "renew (idempotent upsert, no CAS) own liveness for that placement",
		"ValidateBorrowedFSPublicationAuthority": "advisory LOCAL_QUORUM final exact-placement fence, before repair-row queue and HEAD",
	}

	forbiddenConsistency := regexp.MustCompile(`(?i)\bEachQuorum\b|\bSerial\b`)
	seen := make(map[string]bool, len(allowedDBCalls))
	visited := make(map[*r3TypedCallable]bool)

	var walk func(*r3TypedCallable, []string)
	walk = func(callable *r3TypedCallable, path []string) {
		if visited[callable] {
			return
		}
		visited[callable] = true
		path = append(path, filepath.Base(callable.symbol.pkg)+"."+callable.symbol.name)
		locals, localAliases, localUnknown := r3TypedLocalBindings(callable, program)

		ast.Inspect(callable.body, func(node ast.Node) bool {
			if ident, ok := node.(*ast.Ident); ok && forbiddenConsistency.MatchString(ident.Name) {
				t.Fatalf("R3 SYNC READINESS EXCEPTION: %s reaches disallowed consistency identifier %q; the declared exception is LOCAL_QUORUM/session-inherited only, never SERIAL/EACH_QUORUM", strings.Join(path, " -> "), ident.Name)
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			called := r3TypedCallLabel(call)
			targets := r3TypedCallTargets(call, callable, program, locals, localAliases)
			if len(targets) == 0 {
				if localUnknown[called] {
					t.Fatalf("R3 SYNC READINESS EXCEPTION: unresolved local function seam %s is reachable through %s", called, strings.Join(path, " -> "))
				}
				if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
					if receiver, typed := r3TypedExprType(selector.X, callable, program, locals, localAliases); typed && program.packages[receiver.pkg] {
						if receiver.pkg == module+"/internal/db" {
							if _, ok := allowedDBCalls[called]; !ok {
								t.Fatalf("R3 SYNC READINESS EXCEPTION: unlisted db call %s.%s reachable through %s; update the allow-list and docs/R3-LIVENESS-CONTINUITY.md if this is an intentional new cost", filepath.Base(receiver.pkg), called, strings.Join(path, " -> "))
							}
							seen[called] = true
							return true
						}
						t.Fatalf("R3 SYNC READINESS EXCEPTION: unresolved method %s on indexed receiver %s.%s through %s", selector.Sel.Name, filepath.Base(receiver.pkg), receiver.name, strings.Join(path, " -> "))
					}
				}
				return true
			}
			for _, target := range targets {
				// A method on *db.DB (or any other internal/db receiver type) is
				// the actual I/O boundary and must be on the allow-list. A plain
				// package-level internal/db function (e.g. a pure string helper
				// like BlockReferrerForUpload) is not itself I/O -- keep walking
				// into it in case it reaches something that is.
				if target.receiver != nil && target.receiver.pkg == module+"/internal/db" {
					if _, ok := allowedDBCalls[called]; !ok {
						t.Fatalf("R3 SYNC READINESS EXCEPTION: unlisted db call %s reachable through %s", called, strings.Join(path, " -> "))
					}
					seen[called] = true
					continue
				}
				walk(target, path)
			}
			return true
		})
	}

	rootCallables := append([]*r3TypedCallable(nil), program.functions[rootSymbol]...)
	for receiver, methods := range program.methods {
		if receiver.pkg == rootSymbol.pkg {
			rootCallables = append(rootCallables, methods[rootSymbol.name]...)
		}
	}
	if len(rootCallables) == 0 {
		t.Fatalf("R3 SYNC READINESS EXCEPTION: root %s.%s not found", filepath.Base(rootSymbol.pkg), rootSymbol.name)
	}
	for _, callable := range rootCallables {
		walk(callable, nil)
	}

	var missing []string
	for name := range allowedDBCalls {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("R3 SYNC READINESS EXCEPTION: expected reachable db calls not observed: %s; update the allow-list if this is intentional", strings.Join(missing, ", "))
	}
}
