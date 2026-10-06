package v2

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Inspect the actual SELECT -> Scan chain, not an unrelated consistency call.
func historicalQuorumReadContract(source []byte, function, table string) error {
	file, err := parser.ParseFile(token.NewFileSet(), "resurrection_admission.go", source, 0)
	if err != nil {
		return err
	}
	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		if f, ok := decl.(*ast.FuncDecl); ok && f.Name.Name == function {
			fn = f
		}
	}
	if fn == nil {
		return fmt.Errorf("historical read function %s missing", function)
	}
	reads, pinned := 0, 0
	ast.Inspect(fn, func(n ast.Node) bool {
		scan, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := scan.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Scan" {
			return true
		}
		var expr ast.Expr = sel.X
		localQuorum, consistencySeen := false, false
		for {
			call, ok := expr.(*ast.CallExpr)
			if !ok {
				break
			}
			method, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				break
			}
			if method.Sel.Name == "Consistency" && !consistencySeen && len(call.Args) == 1 {
				consistencySeen = true
				arg, ok := call.Args[0].(*ast.SelectorExpr)
				if ok {
					pkg, ok := arg.X.(*ast.Ident)
					localQuorum = ok && pkg.Name == "gocql" && arg.Sel.Name == "LocalQuorum"
				}
			}
			if method.Sel.Name == "Query" && len(call.Args) > 0 {
				if lit, ok := call.Args[0].(*ast.BasicLit); ok {
					query, _ := strconv.Unquote(lit.Value)
					if strings.Contains(strings.Join(strings.Fields(strings.ToLower(query)), " "), "from "+table) {
						reads++
						if localQuorum {
							pinned++
						}
					}
				}
				break
			}
			expr = method.X
		}
		return true
	})
	if reads != 1 || pinned != reads {
		return fmt.Errorf("historical %s reads=%d pinned LOCAL_QUORUM=%d", table, reads, pinned)
	}
	return nil
}

// Check the caller as well as the shared reader: an unrelated pinned reader
// must not hide a new direct query or a bypass of the source-reader primitive.
func historicalSourceReaderContract(source []byte) error {
	file, err := parser.ParseFile(token.NewFileSet(), "resurrection_admission.go", source, 0)
	if err != nil {
		return err
	}
	readers, queries := 0, 0
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "captureRetainedHistoricalFile" {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name == "Query" {
				queries++
			}
			pkg, ok := sel.X.(*ast.Ident)
			if ok && pkg.Name == "db" && sel.Sel.Name == "ReadFSObjectIdentitySourceRow" {
				readers++
			}
			return true
		})
	}
	if readers != 1 || queries != 0 {
		return fmt.Errorf("historical source readers=%d direct queries=%d", readers, queries)
	}
	return nil
}

func TestHistoricalSourceAdmissionUsesQuorumReader(t *testing.T) {
	raw, err := os.ReadFile(r3SourcePath("internal", "api", "v2", "resurrection_admission.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := historicalSourceReaderContract(raw); err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(string(raw), "db.ReadFSObjectIdentitySourceRow(", "db.ReadUnpinnedHistoricalSourceRow(", 1)
	if mutated == string(raw) {
		t.Fatal("mutation did not bypass the shared source reader")
	}
	if err := historicalSourceReaderContract([]byte(mutated)); err == nil {
		t.Fatal("historical source admission accepted a reader bypass")
	}
}

func TestHistoricalReferenceAdmissionPinsLocalQuorum(t *testing.T) {
	for _, contract := range []struct {
		name, function, table string
		path                  []string
	}{
		{"source-layout", "ReadFSObjectIdentitySourceRow", "fs_objects", []string{"internal", "db", "identity_gateway.go"}},
		{"permanent-reference", "requireHistoricalFileReferences", "block_references", []string{"internal", "api", "v2", "resurrection_admission.go"}},
	} {
		t.Run(contract.name, func(t *testing.T) {
			raw, err := os.ReadFile(r3SourcePath(contract.path...))
			if err != nil {
				t.Fatal(err)
			}
			if err := historicalQuorumReadContract(raw, contract.function, contract.table); err != nil {
				t.Fatal(err)
			}
			// Mutate only the target function, independent of other pins' order.
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "source.go", raw, 0)
			if err != nil {
				t.Fatal(err)
			}
			var start, end int
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == contract.function {
					start, end = fset.Position(fn.Pos()).Offset, fset.Position(fn.End()).Offset
				}
			}
			body := string(raw[start:end])
			pin := regexp.MustCompile(`\.\s*Consistency\(gocql\.LocalQuorum\)`)
			for name, replacement := range map[string]string{"inherited-session": "", "weak-ONE": ".Consistency(gocql.One)", "overridden-ONE": ".Consistency(gocql.LocalQuorum).Consistency(gocql.One)"} {
				t.Run(name, func(t *testing.T) {
					changed := pin.ReplaceAllString(body, replacement)
					if changed == body {
						t.Fatal("mutation did not remove the production pin")
					}
					mutated := string(raw[:start]) + changed + string(raw[end:])
					if _, err := parser.ParseFile(token.NewFileSet(), "mutated.go", mutated, 0); err != nil {
						t.Fatalf("mutation must remain valid Go: %v", err)
					}
					if err := historicalQuorumReadContract([]byte(mutated), contract.function, contract.table); err == nil {
						t.Fatal("admission read accepted weak/inherited consistency")
					}
				})
			}
		})
	}
}
