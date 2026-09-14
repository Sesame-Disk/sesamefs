package db

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// CQL table/IF folding for the HEAD SERIAL-domain inventory matches R12
// (internal/integration/r12_serial_domain_guard_test.go). That scanner is
// behind //go:build integration and targets blocks/orphans, so the folding
// is duplicated here rather than imported. Discovery walks Query/Bind, not
// a raw BasicLit scan: const/ident/concat/append resolve to CQL, and an
// unresolvable first argument fails closed. A name-literal regex that
// requires `DELETE FROM libraries` then `IF head_commit_id` as the first
// predicate is a false green: `IF created_at = ? AND head_commit_id`,
// `sesamefs.libraries`, and quoted identifiers would leave discovery.
const pc0CQLIdentifierPattern = `(?:"(?:[^"]|"")+"|[A-Za-z_][A-Za-z0-9_]*)`

var pc0DeleteFromPattern = regexp.MustCompile(
	`(?is)\bDELETE(?:\s+[\w"',\[\]\s.]+?)?\s+FROM\s+(` +
		pc0CQLIdentifierPattern + `)(?:\s*\.\s*(` + pc0CQLIdentifierPattern + `))?`)

var pc0UpdatePattern = regexp.MustCompile(
	`(?is)\bUPDATE\s+(` + pc0CQLIdentifierPattern + `)(?:\s*\.\s*(` + pc0CQLIdentifierPattern + `))?`)

var pc0IFKeywordPattern = regexp.MustCompile(`(?i)\bIF\b`)

var pc0HeadCommitIDColumnPattern = regexp.MustCompile(`(?i)(?:"head_commit_id"|\bhead_commit_id\b)`)

var pc0IFExistsOnlyPattern = regexp.MustCompile(`(?is)^(?:NOT\s+)?EXISTS\b`)

func pc0PreparedCQL(query string) string {
	return pc0StripCQLStringLiterals(pc0StripCQLComments(query))
}

func pc0CQLMatchTable(matches []string) string {
	if len(matches) >= 3 && matches[2] != "" {
		return pc0NormalizeCQLIdentifier(matches[2])
	}
	if len(matches) >= 2 {
		return pc0NormalizeCQLIdentifier(matches[1])
	}
	return ""
}

func pc0NormalizeCQLIdentifier(identifier string) string {
	identifier = strings.TrimSpace(identifier)
	if len(identifier) >= 2 && strings.HasPrefix(identifier, `"`) && strings.HasSuffix(identifier, `"`) {
		return strings.ReplaceAll(identifier[1:len(identifier)-1], `""`, `"`)
	}
	return strings.ToLower(identifier)
}

func pc0CQLIFNamesHeadCommitID(fragment string) bool {
	loc := pc0IFKeywordPattern.FindStringIndex(fragment)
	if loc == nil {
		return false
	}
	rest := strings.TrimSpace(fragment[loc[1]:])
	if pc0IFExistsOnlyPattern.MatchString(rest) && !pc0HeadCommitIDColumnPattern.MatchString(rest) {
		return false
	}
	return pc0HeadCommitIDColumnPattern.MatchString(rest)
}

// pc0CQLIsLibrariesHeadIFDelete reports whether query contains a conditional
// DELETE of the libraries relation whose IF clause names head_commit_id
// (any predicate position, qualified/quoted table spellings included).
func pc0CQLSubmatchTable(prepared string, loc []int) string {
	matches := make([]string, 3)
	matches[0] = prepared[loc[0]:loc[1]]
	if len(loc) > 3 && loc[2] >= 0 {
		matches[1] = prepared[loc[2]:loc[3]]
	}
	if len(loc) > 5 && loc[4] >= 0 {
		matches[2] = prepared[loc[4]:loc[5]]
	}
	return pc0CQLMatchTable(matches)
}

func pc0CQLIsLibrariesHeadIFDelete(query string) bool {
	prepared := pc0PreparedCQL(query)
	for _, loc := range pc0DeleteFromPattern.FindAllStringSubmatchIndex(prepared, -1) {
		if pc0CQLSubmatchTable(prepared, loc) != "libraries" {
			continue
		}
		if pc0CQLIFNamesHeadCommitID(prepared[loc[0]:]) {
			return true
		}
	}
	return false
}

// pc0CQLCompetesForLibraryHead reports whether query is an UPDATE or DELETE
// of libraries whose IF clause names head_commit_id. Used to pin the
// MapScanCAS chain of inventoried HEAD-authority LWTs.
func pc0CQLCompetesForLibraryHead(query string) bool {
	if pc0CQLIsLibrariesHeadIFDelete(query) {
		return true
	}
	prepared := pc0PreparedCQL(query)
	for _, loc := range pc0UpdatePattern.FindAllStringSubmatchIndex(prepared, -1) {
		if pc0CQLSubmatchTable(prepared, loc) != "libraries" {
			continue
		}
		if pc0CQLIFNamesHeadCommitID(prepared[loc[0]:]) {
			return true
		}
	}
	return false
}

func pc0StripCQLStringLiterals(query string) string {
	var out strings.Builder
	out.Grow(len(query))
	inString := false
	for index := 0; index < len(query); index++ {
		char := query[index]
		if !inString {
			out.WriteByte(char)
			if char == '\'' {
				inString = true
			}
			continue
		}
		if char == '\'' {
			if index+1 < len(query) && query[index+1] == '\'' {
				out.WriteString("  ")
				index++
				continue
			}
			out.WriteByte(char)
			inString = false
			continue
		}
		if char == '\r' || char == '\n' {
			out.WriteByte(char)
			continue
		}
		out.WriteByte(' ')
	}
	return out.String()
}

func pc0StripCQLComments(query string) string {
	var out strings.Builder
	out.Grow(len(query))
	inSingleQuote := false
	inDoubleQuote := false
	for index := 0; index < len(query); {
		char := query[index]
		if inSingleQuote {
			out.WriteByte(char)
			index++
			if char == '\'' {
				if index < len(query) && query[index] == '\'' {
					out.WriteByte(query[index])
					index++
					continue
				}
				inSingleQuote = false
			}
			continue
		}
		if inDoubleQuote {
			out.WriteByte(char)
			index++
			if char == '"' {
				if index < len(query) && query[index] == '"' {
					out.WriteByte(query[index])
					index++
					continue
				}
				inDoubleQuote = false
			}
			continue
		}
		if char == '\'' {
			inSingleQuote = true
			out.WriteByte(char)
			index++
			continue
		}
		if char == '"' {
			inDoubleQuote = true
			out.WriteByte(char)
			index++
			continue
		}
		if char == '-' && index+1 < len(query) && query[index+1] == '-' {
			out.WriteString("  ")
			index += 2
			for index < len(query) && query[index] != '\r' && query[index] != '\n' {
				index++
			}
			continue
		}
		if char == '/' && index+1 < len(query) && query[index+1] == '*' {
			out.WriteString("  ")
			index += 2
			for index < len(query) {
				if query[index] == '*' && index+1 < len(query) && query[index+1] == '/' {
					out.WriteString("  ")
					index += 2
					break
				}
				if query[index] == '\r' || query[index] == '\n' {
					out.WriteByte(query[index])
				} else {
					out.WriteByte(' ')
				}
				index++
			}
			continue
		}
		out.WriteByte(char)
		index++
	}
	return out.String()
}

func TestPC0HeadAuthorityDeleteCQLRecognition(t *testing.T) {
	// The #221-era name-literal regex: IF must be the first predicate and the
	// table must be the bare identifier libraries. Kept as a witness that the
	// forms below used to be false greens.
	old := regexp.MustCompile(`(?is)\bdelete\s+from\s+libraries\b[^;]*?\bif\s+head_commit_id\b`)

	cases := []struct {
		name    string
		cql     string
		want    bool
		oldMiss bool
	}{
		{
			name: "production rollback",
			cql:  "DELETE FROM libraries WHERE org_id = ? AND library_id = ? IF head_commit_id = null",
			want: true,
		},
		{
			name:    "IF another column then head_commit_id",
			cql:     "DELETE FROM libraries WHERE org_id = ? AND library_id = ? IF created_at = ? AND head_commit_id = null",
			want:    true,
			oldMiss: true,
		},
		{
			name:    "keyspace-qualified table",
			cql:     "DELETE FROM sesamefs.libraries WHERE org_id = ? AND library_id = ? IF head_commit_id = null",
			want:    true,
			oldMiss: true,
		},
		{
			name:    "quoted qualified table",
			cql:     `DELETE FROM "sesamefs"."libraries" WHERE org_id = ? AND library_id = ? IF head_commit_id = null`,
			want:    true,
			oldMiss: true,
		},
		{
			name:    "quoted table identifier",
			cql:     `DELETE FROM "libraries" WHERE org_id = ? AND library_id = ? IF head_commit_id = null`,
			want:    true,
			oldMiss: true,
		},
		{
			name:    "quoted head_commit_id column",
			cql:     `DELETE FROM libraries WHERE org_id = ? AND library_id = ? IF "head_commit_id" = null`,
			want:    true,
			oldMiss: true,
		},
		{
			name:    "cell delete of head_commit_id",
			cql:     "DELETE head_commit_id FROM libraries WHERE org_id = ? AND library_id = ? IF head_commit_id = null",
			want:    true,
			oldMiss: true,
		},
		{
			name: "unconditional delete is not a HEAD guard",
			cql:  "DELETE FROM libraries WHERE org_id = ? AND library_id = ?",
			want: false,
		},
		{
			name: "IF EXISTS is not a HEAD guard",
			cql:  "DELETE FROM libraries WHERE org_id = ? AND library_id = ? IF EXISTS",
			want: false,
		},
		{
			name: "libraries_by_id is not the canonical relation",
			cql:  "DELETE FROM libraries_by_id WHERE library_id = ? IF head_commit_id = null",
			want: false,
		},
		{
			name: "IF only inside a string value",
			cql:  "DELETE FROM libraries WHERE name = 'IF head_commit_id = null'",
			want: false,
		},
		{
			name: "UPDATE is not a DELETE guard",
			cql:  "UPDATE libraries SET name = ? WHERE org_id = ? AND library_id = ? IF head_commit_id = ?",
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pc0CQLIsLibrariesHeadIFDelete(tc.cql)
			if got != tc.want {
				t.Fatalf("pc0CQLIsLibrariesHeadIFDelete(%q) = %v, want %v", tc.cql, got, tc.want)
			}
			if tc.oldMiss && old.MatchString(tc.cql) {
				t.Fatalf("witness regex unexpectedly matches %q; the oldMiss classification is stale", tc.cql)
			}
			if tc.want && !tc.oldMiss && !old.MatchString(tc.cql) {
				t.Fatalf("production-shaped CQL %q must still match the old regex", tc.cql)
			}
		})
	}
}

func TestPC0CQLCompetesForLibraryHeadRecognizesQualifiedUpdate(t *testing.T) {
	if !pc0CQLCompetesForLibraryHead("UPDATE sesamefs.libraries SET head_commit_id = ? WHERE org_id = ? AND library_id = ? IF created_at != null AND head_commit_id = ?") {
		t.Fatal("qualified UPDATE with head_commit_id not as the first IF predicate must still compete for HEAD")
	}
	if pc0CQLCompetesForLibraryHead("UPDATE libraries SET name = ? WHERE org_id = ? AND library_id = ?") {
		t.Fatal("unconditional UPDATE of libraries must not be classified as a HEAD LWT")
	}
}

func pc0UnquoteStringLit(lit *ast.BasicLit) (string, bool) {
	if lit == nil || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

func pc0ResolveStringExpr(expr ast.Expr, bindings map[string]string) (string, bool) {
	switch node := expr.(type) {
	case *ast.BasicLit:
		return pc0UnquoteStringLit(node)
	case *ast.Ident:
		value, ok := bindings[node.Name]
		return value, ok
	case *ast.ParenExpr:
		return pc0ResolveStringExpr(node.X, bindings)
	case *ast.BinaryExpr:
		if node.Op != token.ADD {
			return "", false
		}
		left, ok := pc0ResolveStringExpr(node.X, bindings)
		if !ok {
			return "", false
		}
		right, ok := pc0ResolveStringExpr(node.Y, bindings)
		if !ok {
			return "", false
		}
		return left + right, true
	default:
		return "", false
	}
}

func pc0PackageConstStrings(file *ast.File) map[string]string {
	bindings := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range valueSpec.Names {
				if i >= len(valueSpec.Values) {
					continue
				}
				if value, ok := pc0ResolveStringExpr(valueSpec.Values[i], bindings); ok {
					bindings[name.Name] = value
				}
			}
		}
	}
	return bindings
}

func pc0FunctionStringBindings(fn *ast.FuncDecl, pkgBindings map[string]string) map[string]string {
	bindings := map[string]string{}
	for name, value := range pkgBindings {
		bindings[name] = value
	}
	poisoned := map[string]bool{}
	opened := map[string]bool{}
	poison := func(name string) {
		if name == "" || name == "_" {
			return
		}
		poisoned[name] = true
		delete(bindings, name)
	}
	open := func(name string, value ast.Expr) {
		if name == "" || name == "_" || poisoned[name] {
			return
		}
		if opened[name] {
			poison(name)
			return
		}
		opened[name] = true
		resolved, ok := pc0ResolveStringExpr(value, bindings)
		if !ok {
			poison(name)
			return
		}
		bindings[name] = resolved
	}
	appendFragment := func(name string, value ast.Expr) {
		if name == "" || name == "_" || poisoned[name] {
			return
		}
		existing, ok := bindings[name]
		if !ok || !opened[name] {
			poison(name)
			return
		}
		fragment, ok := pc0ResolveStringExpr(value, bindings)
		if !ok {
			poison(name)
			return
		}
		bindings[name] = existing + fragment
	}
	if fn.Body == nil {
		return bindings
	}
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch stmt := node.(type) {
		case *ast.ValueSpec:
			for i, name := range stmt.Names {
				if i >= len(stmt.Values) {
					poison(name.Name)
					continue
				}
				open(name.Name, stmt.Values[i])
			}
		case *ast.AssignStmt:
			if len(stmt.Lhs) != len(stmt.Rhs) {
				for _, lhs := range stmt.Lhs {
					if ident, ok := lhs.(*ast.Ident); ok {
						poison(ident.Name)
					}
				}
				return true
			}
			for i, lhs := range stmt.Lhs {
				ident, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}
				switch stmt.Tok {
				case token.ADD_ASSIGN:
					appendFragment(ident.Name, stmt.Rhs[i])
				case token.DEFINE, token.ASSIGN:
					if binary, ok := stmt.Rhs[i].(*ast.BinaryExpr); ok && binary.Op == token.ADD {
						if left, ok := binary.X.(*ast.Ident); ok && left.Name == ident.Name {
							appendFragment(ident.Name, binary.Y)
							continue
						}
					}
					open(ident.Name, stmt.Rhs[i])
				default:
					poison(ident.Name)
				}
			}
		}
		return true
	})
	return bindings
}

func pc0IsCQLEntryPoint(method string, arguments int) bool {
	switch method {
	case "Query":
		return arguments > 0
	case "Bind":
		return arguments >= 2
	default:
		return false
	}
}

func TestPC0HeadAuthorityQueryResolvesIndirectCQL(t *testing.T) {
	src := `package example
func deleteUnpublished(session interface{ Query(string, ...interface{}) interface{ MapScanCAS(map[string]interface{}) (bool, error) } }, orgID, libraryID string) {
	const deleteUnpublished = ` + "`DELETE FROM libraries WHERE org_id = ? AND library_id = ? IF head_commit_id = null`" + `
	session.Query(deleteUnpublished, orgID, libraryID).MapScanCAS(map[string]interface{}{})
	stmt := "DELETE FROM libraries " + "WHERE org_id = ? AND library_id = ? IF head_commit_id = null"
	session.Query(stmt, orgID, libraryID).MapScanCAS(map[string]interface{}{})
	built := "DELETE FROM libraries WHERE org_id = ? AND library_id = ?"
	built += " IF head_commit_id = null"
	session.Query(built, orgID, libraryID).MapScanCAS(map[string]interface{}{})
	session.Query(fmt.Sprintf("DELETE FROM libraries WHERE org_id = ? AND library_id = ? IF head_commit_id = null"), orgID, libraryID).MapScanCAS(map[string]interface{}{})
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "example.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg := pc0PackageConstStrings(file)
	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		if f, ok := decl.(*ast.FuncDecl); ok && f.Name.Name == "deleteUnpublished" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("deleteUnpublished not found")
	}
	bindings := pc0FunctionStringBindings(fn, pkg)
	var resolvedDeletes, unresolved int
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !pc0IsCQLEntryPoint(sel.Sel.Name, len(call.Args)) {
			return true
		}
		cql, ok := pc0ResolveStringExpr(call.Args[0], bindings)
		if !ok {
			unresolved++
			return true
		}
		if pc0CQLIsLibrariesHeadIFDelete(cql) {
			resolvedDeletes++
		}
		return true
	})
	if resolvedDeletes != 3 {
		t.Fatalf("resolved HEAD DELETE IF via const/concat/append = %d, want 3", resolvedDeletes)
	}
	if unresolved != 1 {
		t.Fatalf("unresolved constructed Query CQL = %d, want 1 (fmt.Sprintf)", unresolved)
	}
}
