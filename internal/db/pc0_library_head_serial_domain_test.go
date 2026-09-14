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
// spot of TestPC0RawHeadColumnWritersAreInventoried: a conditional DELETE of
// the libraries relation whose IF clause names head_commit_id competes for
// canonical HEAD authority without writing the column, so it cannot hide as
// "not a writer". Discovery uses R12-style table/IF folding (qualified and
// quoted identifiers, head_commit_id in any IF predicate).
func TestPC0HeadAuthorityDeleteGuardsAreInventoried(t *testing.T) {
	hits := pc0HeadAuthorityDeleteLiterals(t, "internal", "cmd")

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
}

func pc0HeadAuthorityDeleteLiterals(t *testing.T, roots ...string) map[string][]string {
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
					value, err := strconv.Unquote(lit.Value)
					if err != nil {
						value = lit.Value
					}
					if pc0CQLIsLibrariesHeadIFDelete(value) {
						key := pc0CallerKey(relPath, name)
						hits[key] = append(hits[key], value)
					}
					return true
				})
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("PC0 HEAD SERIAL: walk %s: %v", root, walkErr)
		}
	}
	return hits
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
		t.Fatalf("PC0 HEAD SERIAL: %s in %s CAS CQL is not a libraries IF head_commit_id LWT: %q", op.decl, op.path, cql)
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
