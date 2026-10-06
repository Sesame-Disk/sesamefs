package v2

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Inspect the actual SELECT -> Scan chain, not an unrelated consistency call.
func historicalReferenceReadContract(source []byte) error {
	file, err := parser.ParseFile(token.NewFileSet(), "resurrection_admission.go", source, 0)
	if err != nil {
		return err
	}
	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		if f, ok := decl.(*ast.FuncDecl); ok && f.Name.Name == "requireHistoricalFileReferences" {
			fn = f
		}
	}
	if fn == nil {
		return fmt.Errorf("historical reference admission function missing")
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
					if strings.Contains(strings.Join(strings.Fields(strings.ToLower(query)), " "), "from block_references") {
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
		return fmt.Errorf("historical permanent-reference reads=%d pinned LOCAL_QUORUM=%d", reads, pinned)
	}
	return nil
}

func TestHistoricalReferenceAdmissionPinsLocalQuorum(t *testing.T) {
	raw, err := os.ReadFile(r3SourcePath("internal", "api", "v2", "resurrection_admission.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := historicalReferenceReadContract(raw); err != nil {
		t.Fatal(err)
	}
	for name, replacement := range map[string]string{"inherited-session": "", "weak-ONE": ".Consistency(gocql.One)", "overridden-ONE": ".Consistency(gocql.LocalQuorum).Consistency(gocql.One)"} {
		t.Run(name, func(t *testing.T) {
			mutated := strings.Replace(string(raw), ".Consistency(gocql.LocalQuorum)", replacement, 1)
			if mutated == string(raw) {
				t.Fatal("mutation did not remove the production pin")
			}
			if err := historicalReferenceReadContract([]byte(mutated)); err == nil {
				t.Fatal("admission read accepted weak/inherited consistency")
			}
		})
	}
}
