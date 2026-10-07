package v2

import (
	"errors"
	"fmt"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// Every query must be inside the callback passed to the tested fallback helper,
// and its last consistency call must bind the callback's actual parameter.
func historicalAdmissionReadContract(raw []byte) error {
	file, err := parser.ParseFile(token.NewFileSet(), "source.go", raw, 0)
	if err != nil {
		return err
	}
	tables := map[string]int{"fs_objects": 0, "libraries": 0, "block_id_mappings": 0, "blocks": 0, "block_references": 0}
	queries, wrappers := 0, 0
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Query" {
			queries++
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != "readHistoricalAdmissionRow" {
			return true
		}
		wrappers++
		if len(call.Args) != 1 {
			return true
		}
		callback, ok := call.Args[0].(*ast.FuncLit)
		if !ok || len(callback.Type.Params.List) != 1 || len(callback.Type.Params.List[0].Names) != 1 {
			return true
		}
		parameter := callback.Type.Params.List[0].Names[0].Name
		ast.Inspect(callback.Body, func(n ast.Node) bool {
			scan, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			method, ok := scan.Fun.(*ast.SelectorExpr)
			if !ok || method.Sel.Name != "Scan" {
				return true
			}
			var expr ast.Expr = method.X
			seen, pinned := false, false
			for {
				call, ok := expr.(*ast.CallExpr)
				if !ok {
					break
				}
				method, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					break
				}
				if method.Sel.Name == "Consistency" && !seen {
					seen = true
					if len(call.Args) == 1 {
						arg, ok := call.Args[0].(*ast.Ident)
						pinned = ok && arg.Name == parameter
					}
				}
				if method.Sel.Name == "Query" && len(call.Args) > 0 {
					if lit, ok := call.Args[0].(*ast.BasicLit); ok && pinned {
						query, _ := strconv.Unquote(lit.Value)
						words := strings.Fields(strings.ToLower(query))
						for i, word := range words {
							if word == "from" && i+1 < len(words) {
								if _, ok := tables[words[i+1]]; ok {
									tables[words[i+1]]++
								}
							}
						}
					}
					break
				}
				expr = method.X
			}
			return true
		})
		return true
	})
	if wrappers != 5 || queries != 5 {
		return fmt.Errorf("scoped fallback wrappers=%d queries=%d", wrappers, queries)
	}
	for table, count := range tables {
		if count != 1 {
			return fmt.Errorf("%s callback-bound reads=%d", table, count)
		}
	}
	return nil
}

func TestHistoricalAdmissionReadFallback(t *testing.T) {
	transport := errors.New("transport")
	for _, tc := range []struct {
		name                string
		local, global, want error
		levels              []gocql.Consistency
	}{
		{"local-hit", nil, transport, nil, []gocql.Consistency{gocql.LocalQuorum}},
		{"local-error", transport, nil, transport, []gocql.Consistency{gocql.LocalQuorum}},
		{"global-hit", gocql.ErrNotFound, nil, nil, []gocql.Consistency{gocql.LocalQuorum, gocql.EachQuorum}},
		{"global-miss", gocql.ErrNotFound, gocql.ErrNotFound, gocql.ErrNotFound, []gocql.Consistency{gocql.LocalQuorum, gocql.EachQuorum}},
		{"global-error", gocql.ErrNotFound, transport, transport, []gocql.Consistency{gocql.LocalQuorum, gocql.EachQuorum}},
		{"wrapped-local-miss", fmt.Errorf("lookup: %w", gocql.ErrNotFound), nil, nil, []gocql.Consistency{gocql.LocalQuorum, gocql.EachQuorum}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var levels []gocql.Consistency
			err := readHistoricalAdmissionRow(func(c gocql.Consistency) error {
				levels = append(levels, c)
				if len(levels) == 1 {
					return tc.local
				}
				return tc.global
			})
			if !errors.Is(err, tc.want) || !reflect.DeepEqual(levels, tc.levels) {
				t.Fatalf("err=%v levels=%v want err=%v levels=%v", err, levels, tc.want, tc.levels)
			}
		})
	}
}

func TestHistoricalAdmissionReadsUseScopedFallback(t *testing.T) {
	raw, err := os.ReadFile(r3SourcePath("internal", "api", "v2", "resurrection_admission.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := historicalAdmissionReadContract(raw); err != nil {
		t.Fatal(err)
	}
	for _, bypass := range []string{"resolveStoredBlockIDs", "captureCopiedBlockPlacements", "ResolveBlockRepresentationID", "GetBlockIDMappingContext", "ReadFSObjectIdentitySourceRow"} {
		if strings.Contains(string(raw), bypass+"(") {
			t.Fatalf("local resolver bypass: %s", bypass)
		}
	}
	// Mutate each actual read independently, including a later override.
	pin := ".Consistency(consistency)"
	offset := 0
	for i := 0; i < 5; i++ {
		pos := strings.Index(string(raw[offset:]), pin)
		if pos < 0 {
			t.Fatal("missing target pin")
		}
		pos += offset
		for name, replacement := range map[string]string{"inherited": "", "ONE": ".Consistency(gocql.One)", "always-EQ": ".Consistency(gocql.EachQuorum)", "local-only": ".Consistency(gocql.LocalQuorum)", "override": pin + ".Consistency(gocql.One)"} {
			t.Run(fmt.Sprintf("read-%d/%s", i, name), func(t *testing.T) {
				mutated := string(raw[:pos]) + replacement + string(raw[pos+len(pin):])
				if _, err := parser.ParseFile(token.NewFileSet(), "mutation.go", mutated, 0); err != nil {
					t.Fatal(err)
				}
				if err := historicalAdmissionReadContract([]byte(mutated)); err == nil {
					t.Fatal("admission consistency bypass accepted")
				}
			})
		}
		offset = pos + len(pin)
	}
}
