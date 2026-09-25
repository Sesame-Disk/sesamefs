package db

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

const blockMappingAuthorityClaimsTable = "block_mapping_authority_claims"

var (
	blockMappingAuthorityInsertPattern = regexp.MustCompile(`(?is)^INSERT\s+INTO\s+block_mapping_authority_claims\s*\([^)]*\)\s*VALUES\s*\([^)]*\)\s*IF\s+NOT\s+EXISTS$`)
	blockMappingAuthoritySelectPattern = regexp.MustCompile(`(?is)^SELECT\s+.+\s+FROM\s+block_mapping_authority_claims\s+WHERE\s+.+$`)
	blockMappingAuthorityCreatePattern = regexp.MustCompile(`(?is)^CREATE\s+TABLE\s+IF\s+NOT\s+EXISTS\s+block_mapping_authority_claims\b`)
	blockMappingTableMentionPattern    = regexp.MustCompile(`(?i)\bblock_id_mappings\b`)
	blockMappingCQLKeywordPattern      = regexp.MustCompile(`(?i)\b(?:SELECT|INSERT|UPDATE|DELETE|FROM|INTO)\b`)
	blockMappingSelectPattern          = regexp.MustCompile(`(?is)^SELECT\s+.+\s+FROM\s+block_id_mappings\b`)
	blockMappingInsertPattern          = regexp.MustCompile(`(?is)^INSERT\s+INTO\s+block_id_mappings\s*\(org_id,\s*representation_id,\s*external_id,\s*internal_id,\s*created_at\)\s*VALUES\s*\(\?,\s*\?,\s*\?,\s*\?,\s*\?\)$`)
	blockMappingFreezePattern          = regexp.MustCompile(`(?is)^UPDATE\s+block_id_mappings\s+USING\s+TIMESTAMP\s+\?\s+SET\s+internal_id\s*=\s*\?\s+WHERE\s+org_id\s*=\s*\?\s+AND\s+representation_id\s*=\s*\?\s+AND\s+external_id\s*=\s*\?$`)
)

// Symbols that acquire or write mapping authority with explicit source-file
// allowlists. The freeze primitive stays confined to its defining file;
// acquisition primitives and integration hooks are listed separately below.
var blockMappingAuthorityAcquisitionSymbols = map[string]map[string]bool{
	"freezeBlockMappingProjection": {
		"internal/db/block_mapping_authority.go": true,
	},
	"claimBlockMappingAuthority": {
		"internal/db/block_mapping_authority.go":             true,
		"internal/db/block_mapping_authority_integration.go": true,
	},
	"promoteBlockMappingAuthority": {
		"internal/db/block_mapping_authority.go":             true,
		"internal/db/block_mapping_authority_integration.go": true,
	},
	"PromoteBlockMappingAuthority": {
		"internal/db/block_mapping_authority.go":      true,
		"internal/db/library_continuity_certifier.go": true,
	},
}

type blockMappingResolvedString struct {
	text     string
	constant bool
}

// blockMappingSourceStrings follows string values used as Query arguments,
// including local bindings, constant concatenation, strings.Join and local
// helper returns. It deliberately does not try to execute Go code: unknown
// pieces keep the known fragments so a split table name still taints the
// statement and fails closed.
type blockMappingSourceStrings struct {
	functions     map[string]*ast.FuncDecl
	functionPaths map[string]string
	globals       map[string]ast.Expr
	dynamicValues map[ast.Expr]bool
}

// These production CQL builders use dynamic fragments but have fixed table
// identity in their own source. Keep their exact call-site inventory explicit;
// any new unresolved Query must be reviewed and classified before the guard can
// pass. The hard-delete lock builders are additionally pinned to their three
// known table/partition-key pairs below.
type blockMappingDynamicQueryContract struct {
	count       int
	tableMarker string
}

var blockMappingDynamicQueryAllowlist = map[string]map[string]blockMappingDynamicQueryContract{
	"internal/api/v2/admin.go": {
		"UpdateOrganization": {count: 1, tableMarker: "UPDATE organizations SET"}, // validated update columns.
	},
	"internal/api/v2/admin_link_helpers.go": {
		"listAdminLinkProjectionCursorPage":          {count: 1, tableMarker: "FROM admin_links_by_created"},
		"listAdminLinkProjectionCursorPageByOrg":     {count: 1, tableMarker: "FROM admin_links_by_org_created"},
		"listAdminLinkProjectionCursorPageByCreator": {count: 1, tableMarker: "FROM admin_links_by_org_created"},
	},
	"internal/api/v2/libraries.go": {
		"UpdateLibrary": {count: 1, tableMarker: "UPDATE libraries SET"}, // dynamic SET fields come from a closed request mapping.
	},
	"internal/api/v2/org_admin.go": {
		"updateOrgSetting": {count: 1, tableMarker: "UPDATE organizations SET settings["}, // key is checked against allowedOrgSettingKeys.
	},
	"internal/gc/store_cassandra.go": {
		"ListFailedItems":       {count: 2, tableMarker: "FROM gc_failed_items"},  // optional LIMIT only.
		"PendingItemExists":     {count: 1, tableMarker: "FROM gc_pending_items"}, // optional clustering-key restriction.
		"acquireHardDeleteLock": {count: 2},                                       // fixed lock tables, enforced by the invocation inventory below.
		"renewHardDeleteLock":   {count: 1},
		"releaseHardDeleteLock": {count: 1},
	},
}

func blockMappingCallName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		return value.Sel.Name
	}
	return ""
}

func blockMappingResolveAlias(expr ast.Expr, locals map[string]ast.Expr, seen map[string]bool) ast.Expr {
	for {
		switch value := expr.(type) {
		case *ast.ParenExpr:
			expr = value.X
		case *ast.Ident:
			if seen[value.Name] {
				return expr
			}
			next, ok := locals[value.Name]
			if !ok || next == nil {
				return expr
			}
			seen[value.Name] = true
			expr = next
		default:
			return expr
		}
	}
}

func blockMappingLocalStrings(body ast.Node, parameters *ast.FieldList, arguments []ast.Expr, outer map[string]ast.Expr, source *blockMappingSourceStrings, activeFunctions map[string]bool, depth int) map[string]ast.Expr {
	locals := make(map[string]ast.Expr, len(outer)+8)
	for name, expr := range outer {
		locals[name] = expr
	}
	if parameters != nil {
		argumentIndex := 0
		for _, field := range parameters.List {
			for _, name := range field.Names {
				if argumentIndex < len(arguments) {
					resolved := source.resolve(arguments[argumentIndex], outer, map[string]bool{}, activeFunctions, depth+1)
					if resolved.constant {
						locals[name.Name] = &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(resolved.text)}
					} else if resolved.text != "" {
						dynamic := &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(resolved.text)}
						locals[name.Name] = dynamic
						if source.dynamicValues == nil {
							source.dynamicValues = map[ast.Expr]bool{}
						}
						source.dynamicValues[dynamic] = true
					} else {
						locals[name.Name] = nil
					}
				} else {
					locals[name.Name] = nil
				}
				argumentIndex++
			}
		}
	}
	if body == nil {
		return locals
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.AssignStmt:
			for index, left := range value.Lhs {
				name, ok := left.(*ast.Ident)
				if !ok {
					continue
				}
				if index < len(value.Rhs) {
					locals[name.Name] = value.Rhs[index]
				} else {
					locals[name.Name] = nil
				}
			}
		case *ast.ValueSpec:
			for index, name := range value.Names {
				if index < len(value.Values) {
					locals[name.Name] = value.Values[index]
				}
			}
		}
		return true
	})
	return locals
}

func (source *blockMappingSourceStrings) resolve(expr ast.Expr, locals map[string]ast.Expr, seenLocals, activeFunctions map[string]bool, depth int) blockMappingResolvedString {
	if expr == nil || depth > 24 {
		return blockMappingResolvedString{}
	}
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return blockMappingResolvedString{}
		}
		text, err := strconv.Unquote(value.Value)
		return blockMappingResolvedString{text: text, constant: err == nil && !source.dynamicValues[value]}
	case *ast.Ident:
		if seenLocals[value.Name] {
			return blockMappingResolvedString{}
		}
		if next, ok := locals[value.Name]; ok {
			if next == nil {
				return blockMappingResolvedString{}
			}
			seen := make(map[string]bool, len(seenLocals)+1)
			for name, active := range seenLocals {
				seen[name] = active
			}
			seen[value.Name] = true
			return source.resolve(next, locals, seen, activeFunctions, depth+1)
		}
		if next, ok := source.globals[value.Name]; ok {
			return source.resolve(next, source.globals, seenLocals, activeFunctions, depth+1)
		}
	case *ast.ParenExpr:
		return source.resolve(value.X, locals, seenLocals, activeFunctions, depth+1)
	case *ast.BinaryExpr:
		if value.Op != token.ADD {
			return blockMappingResolvedString{}
		}
		left := source.resolve(value.X, locals, seenLocals, activeFunctions, depth+1)
		right := source.resolve(value.Y, locals, seenLocals, activeFunctions, depth+1)
		return blockMappingResolvedString{text: left.text + right.text, constant: left.constant && right.constant}
	case *ast.CompositeLit:
		var combined blockMappingResolvedString
		combined.constant = true
		for _, element := range value.Elts {
			resolved := source.resolve(element, locals, seenLocals, activeFunctions, depth+1)
			combined.text += resolved.text
			combined.constant = combined.constant && resolved.constant
		}
		return combined
	case *ast.CallExpr:
		name := blockMappingCallName(value.Fun)
		if name == "Join" && len(value.Args) == 2 {
			separator := source.resolve(value.Args[1], locals, seenLocals, activeFunctions, depth+1)
			itemsExpr := blockMappingResolveAlias(value.Args[0], locals, map[string]bool{})
			items, ok := itemsExpr.(*ast.CompositeLit)
			if !ok {
				return blockMappingResolvedString{text: separator.text, constant: false}
			}
			combined := blockMappingResolvedString{constant: separator.constant}
			for index, element := range items.Elts {
				if index > 0 {
					combined.text += separator.text
				}
				resolved := source.resolve(element, locals, seenLocals, activeFunctions, depth+1)
				combined.text += resolved.text
				combined.constant = combined.constant && resolved.constant
			}
			return combined
		}
		if name == "Sprintf" && len(value.Args) > 0 {
			format := source.resolve(value.Args[0], locals, seenLocals, activeFunctions, depth+1)
			if len(value.Args) == 1 {
				return format
			}
			format.constant = false
			return format
		}
		function := source.functions[name]
		if function == nil || function.Body == nil || activeFunctions[name] {
			return blockMappingResolvedString{}
		}
		active := make(map[string]bool, len(activeFunctions)+1)
		for current, running := range activeFunctions {
			active[current] = running
		}
		active[name] = true
		functionLocals := blockMappingLocalStrings(function.Body, function.Type.Params, value.Args, locals, source, active, depth)
		var result blockMappingResolvedString
		found := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if returned, ok := node.(*ast.ReturnStmt); ok && len(returned.Results) > 0 {
				result = source.resolve(returned.Results[0], functionLocals, map[string]bool{}, active, depth+1)
				found = true
				return false
			}
			return true
		})
		if found {
			return result
		}
	}
	return blockMappingResolvedString{}
}

func (source *blockMappingSourceStrings) queryLocals(function *ast.FuncDecl, functionLiteral *ast.FuncLit) map[string]ast.Expr {
	if function != nil {
		return blockMappingLocalStrings(function.Body, function.Type.Params, nil, source.globals, source, nil, 0)
	}
	if functionLiteral != nil {
		return blockMappingLocalStrings(functionLiteral.Body, functionLiteral.Type.Params, nil, source.globals, source, nil, 0)
	}
	return source.globals
}

// queryLocalsAt resolves assignments that precede a particular Query call.
// Assignments in a control-flow region are invalidated conservatively because
// walking both branches in source order cannot prove which value reaches the
// call site.
func (source *blockMappingSourceStrings) queryLocalsAt(function *ast.FuncDecl, functionLiteral *ast.FuncLit, arguments []ast.Expr, outer map[string]ast.Expr, at token.Pos) map[string]ast.Expr {
	var body ast.Node
	var parameters *ast.FieldList
	if function != nil {
		body, parameters = function.Body, function.Type.Params
	} else if functionLiteral != nil {
		body, parameters = functionLiteral.Body, functionLiteral.Type.Params
	}
	locals := blockMappingLocalStrings(nil, parameters, arguments, outer, source, nil, 0)
	if body == nil {
		return locals
	}
	ast.Inspect(body, func(node ast.Node) bool {
		if node == nil || node.Pos() >= at {
			return true
		}
		switch value := node.(type) {
		case *ast.AssignStmt:
			for index, left := range value.Lhs {
				name, ok := left.(*ast.Ident)
				if !ok {
					continue
				}
				if index < len(value.Rhs) {
					locals[name.Name] = value.Rhs[index]
				} else {
					locals[name.Name] = nil
				}
			}
		case *ast.ValueSpec:
			for index, name := range value.Names {
				if index < len(value.Values) {
					locals[name.Name] = value.Values[index]
				}
			}
		}
		return true
	})
	ast.Inspect(body, func(node ast.Node) bool {
		if node == nil || node.Pos() >= at {
			return true
		}
		switch value := node.(type) {
		case *ast.IfStmt:
			blockMappingInvalidateControlAssignments(value, at, locals)
		case *ast.ForStmt:
			blockMappingInvalidateControlAssignments(value, at, locals)
		case *ast.RangeStmt:
			blockMappingInvalidateControlAssignments(value, at, locals)
		case *ast.SwitchStmt:
			blockMappingInvalidateControlAssignments(value, at, locals)
		case *ast.TypeSwitchStmt:
			blockMappingInvalidateControlAssignments(value, at, locals)
		case *ast.SelectStmt:
			blockMappingInvalidateControlAssignments(value, at, locals)
		}
		return true
	})
	return locals
}

func blockMappingInvalidateControlAssignments(control ast.Node, at token.Pos, locals map[string]ast.Expr) {
	ast.Inspect(control, func(node ast.Node) bool {
		if node == nil || node.Pos() >= at {
			return true
		}
		switch value := node.(type) {
		case *ast.AssignStmt:
			for _, left := range value.Lhs {
				if name, ok := left.(*ast.Ident); ok {
					locals[name.Name] = nil
				}
			}
		case *ast.ValueSpec:
			for _, name := range value.Names {
				locals[name.Name] = nil
			}
		}
		return true
	})
}

func blockMappingQueryChainContains(expr ast.Expr, query *ast.CallExpr) bool {
	switch value := expr.(type) {
	case *ast.CallExpr:
		if value == query {
			return true
		}
		if selector, ok := value.Fun.(*ast.SelectorExpr); ok {
			return blockMappingQueryChainContains(selector.X, query)
		}
	case *ast.SelectorExpr:
		return blockMappingQueryChainContains(value.X, query)
	case *ast.ParenExpr:
		return blockMappingQueryChainContains(value.X, query)
	}
	return false
}

func blockMappingQueryConsistencyNames(body ast.Node, query *ast.CallExpr) []string {
	var consistencies []string
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Consistency" || !blockMappingQueryChainContains(selector.X, query) {
			return true
		}
		switch value := call.Args[0].(type) {
		case *ast.Ident:
			consistencies = append(consistencies, value.Name)
		case *ast.SelectorExpr:
			if pkg, ok := value.X.(*ast.Ident); ok {
				consistencies = append(consistencies, pkg.Name+"."+value.Sel.Name)
			}
		}
		return true
	})
	return consistencies
}

func blockMappingHasDirectQuery(body ast.Node) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok && blockMappingCallName(call.Fun) == "Query" {
			found = true
			return false
		}
		return !found
	})
	return found
}

func blockMappingMigrationQueryAllowlisted(relPath, owner string, argument ast.Expr) bool {
	// Migration statements come from the checked-in migration inventory and are
	// deliberately executed as parsed statements rather than inline CQL.
	identifier, ok := argument.(*ast.Ident)
	return ok && relPath == "internal/db/migrator.go" && owner == "apply" && identifier.Name == "stmt"
}

func blockMappingPotentialDynamicQuery(statement string, argument ast.Expr) bool {
	lower := strings.ToLower(statement)
	if strings.Contains(lower, "block_id_") || (strings.Contains(lower, "blockid") && strings.Contains(lower, "mapping")) {
		return true
	}
	trimmed := strings.TrimSpace(strings.ToUpper(statement))
	if strings.HasSuffix(trimmed, " FROM") || strings.HasSuffix(trimmed, " INTO") || strings.HasSuffix(trimmed, " UPDATE") || strings.Contains(trimmed, " FROM WHERE") || strings.Contains(trimmed, " FROM ORDER BY") || strings.Contains(trimmed, " FROM LIMIT") || strings.Contains(trimmed, " INTO VALUES") || strings.Contains(trimmed, " UPDATE WHERE") {
		return true
	}
	potential := false
	ast.Inspect(argument, func(node ast.Node) bool {
		if identifier, ok := node.(*ast.Ident); ok {
			name := strings.ToLower(identifier.Name)
			if strings.Contains(name, "blockmapping") || strings.Contains(name, "block_mapping") || strings.Contains(name, "blockidmapping") {
				potential = true
			}
		}
		return true
	})
	return potential
}

func blockMappingParentMap(root ast.Node) map[ast.Node]ast.Node {
	parents := make(map[ast.Node]ast.Node)
	var stack []ast.Node
	ast.Inspect(root, func(node ast.Node) bool {
		if node == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return true
		}
		if len(stack) > 0 {
			parents[node] = stack[len(stack)-1]
		}
		stack = append(stack, node)
		return true
	})
	return parents
}

func blockMappingAuthorityParse(t *testing.T, path string) *ast.File {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("PCD1B3 MAPPING AUTHORITY: parse %s: %v", path, err)
	}
	return parsed
}

// TestBlockMappingAuthorityClaimsAreImmutableRepositoryWide freezes every
// production/schema reference to the claim table: one INSERT IF NOT EXISTS and
// one SELECT in the primitive, one CREATE TABLE without TTL, nothing else. In
// particular no mapping delete or GC path can retire or rewrite a claim.
func TestBlockMappingAuthorityClaimsAreImmutableRepositoryWide(t *testing.T) {
	repoRoot := r3RepositoryRoot(t)
	relPrimitive := "internal/db/block_mapping_authority.go"
	var violations []string
	insertCount, selectCount := 0, 0
	for _, path := range identityAuthorityProductionGoFiles(t, repoRoot) {
		relPath, err := filepath.Rel(repoRoot, path)
		if err != nil {
			t.Fatalf("relative path %s: %v", path, err)
		}
		relPath = filepath.ToSlash(relPath)
		var joined strings.Builder
		var references []string
		for _, value := range identityAuthorityGoStrings(t, path) {
			joined.WriteString(value)
			if strings.Contains(strings.ToLower(value), blockMappingAuthorityClaimsTable) {
				references = append(references, value)
			}
		}
		if relPath != relPrimitive {
			if strings.Contains(strings.ToLower(joined.String()), blockMappingAuthorityClaimsTable) {
				violations = append(violations, relPath+": unauthorized production reference")
			}
			continue
		}
		for _, statement := range references {
			normalized := strings.TrimSpace(statement)
			switch {
			case blockMappingAuthorityInsertPattern.MatchString(normalized) && !strings.Contains(strings.ToUpper(normalized), "USING TTL"):
				insertCount++
			case blockMappingAuthoritySelectPattern.MatchString(normalized):
				selectCount++
			default:
				violations = append(violations, relPath+": unauthorized operation on "+blockMappingAuthorityClaimsTable)
			}
		}
	}
	if insertCount != 1 || selectCount != 1 {
		violations = append(violations, "runtime INSERT IF NOT EXISTS="+strconv.Itoa(insertCount)+" SELECT="+strconv.Itoa(selectCount)+", want 1/1")
	}

	createCount := 0
	walkErr := filepath.WalkDir(filepath.Join(repoRoot, "internal", "db", "migrations"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".cql") {
			return err
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		content = identityAuthorityCQLComments.ReplaceAll(content, nil)
		for _, statement := range strings.Split(string(content), ";") {
			normalized := strings.TrimSpace(statement)
			if !strings.Contains(strings.ToLower(normalized), blockMappingAuthorityClaimsTable) {
				continue
			}
			if !strings.HasSuffix(filepath.ToSlash(path), "/027_block_mapping_authority_claims.cql") ||
				!blockMappingAuthorityCreatePattern.MatchString(normalized) ||
				strings.Contains(strings.ToLower(normalized), "default_time_to_live") {
				violations = append(violations, filepath.ToSlash(path)+": unauthorized schema operation on "+blockMappingAuthorityClaimsTable)
				continue
			}
			createCount++
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk migrations: %v", walkErr)
	}
	if createCount != 1 {
		violations = append(violations, "schema CREATE TABLE count="+strconv.Itoa(createCount)+", want 1")
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("PCD1B3 MAPPING AUTHORITY IMMUTABILITY: %v", violations)
	}
}

// TestBlockMappingAuthorityAcquisitionIsColdPathOnly is the performance gate:
// the upload hot path keeps its plain read-before-write mapping and gains no
// Paxos. Acquisition symbols may appear only in the primitive and the
// certifier, never in API, sync, GC or upload code.
func TestBlockMappingAuthorityAcquisitionIsColdPathOnly(t *testing.T) {
	repoRoot := r3RepositoryRoot(t)
	var violations []string
	for _, path := range identityAuthorityProductionGoFiles(t, repoRoot) {
		relPath, err := filepath.Rel(repoRoot, path)
		if err != nil {
			t.Fatalf("relative path %s: %v", path, err)
		}
		relPath = filepath.ToSlash(relPath)
		parsed := blockMappingAuthorityParse(t, path)
		parents := blockMappingParentMap(parsed)
		ast.Inspect(parsed, func(node ast.Node) bool {
			var name string
			switch value := node.(type) {
			case *ast.Ident:
				name = value.Name
			case *ast.SelectorExpr:
				name = value.Sel.Name
			default:
				return true
			}
			if allowed, tracked := blockMappingAuthorityAcquisitionSymbols[name]; tracked && !allowed[relPath] {
				violations = append(violations, relPath+": references "+name)
			}
			if name == "freezeBlockMappingProjection" {
				if _, declarationName := parents[node].(*ast.FuncDecl); declarationName {
					return true
				}
				if call, directCall := parents[node].(*ast.CallExpr); directCall && call.Fun == node {
					return true
				}
				violations = append(violations, relPath+": freezeBlockMappingProjection may not be taken as a function value or aliased")
			}
			return true
		})
	}

	source := filepath.Join(repoRoot, "internal", "db", "block_references.go")
	parsed := blockMappingAuthorityParse(t, source)
	hotPath := map[string]bool{
		"WriteBlockIDMapping":                 true,
		"WriteVerifiedWebBlockMapping":        true,
		"writeCheckedBlockIDMapping":          true,
		"insertBlockIDMappingForWriteCheckFn": true,
		"getBlockIDMappingForWriteCheckFn":    true,
	}
	seen := map[string]bool{}
	checkHotPath := func(name string, body ast.Node) {
		seen[name] = true
		ast.Inspect(body, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.SelectorExpr:
				if value.Sel.Name == "SerialConsistency" || value.Sel.Name == "MapScanCAS" || value.Sel.Name == "ScanCAS" {
					violations = append(violations, "upload mapping writer "+name+" runs an LWT ("+value.Sel.Name+")")
				}
			case *ast.BasicLit:
				if text, ok := constantIdentityAuthorityString(value); ok {
					lower := strings.ToLower(text)
					if strings.Contains(lower, "if not exists") || strings.Contains(lower, blockMappingAuthorityClaimsTable) || strings.Contains(lower, " if ") {
						violations = append(violations, "upload mapping writer "+name+" issues conditional/authority CQL")
					}
				}
			}
			return true
		})
	}
	for _, decl := range parsed.Decls {
		switch value := decl.(type) {
		case *ast.FuncDecl:
			if hotPath[value.Name.Name] && value.Body != nil {
				checkHotPath(value.Name.Name, value.Body)
			}
		case *ast.GenDecl:
			for _, spec := range value.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, name := range valueSpec.Names {
					if hotPath[name.Name] && index < len(valueSpec.Values) {
						checkHotPath(name.Name, valueSpec.Values[index])
					}
				}
			}
		}
	}
	for name := range hotPath {
		if !seen[name] {
			violations = append(violations, "hot-path mapping writer "+name+" not found; this guard is vacuous")
		}
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("PCD1B3 MAPPING AUTHORITY HOT PATH: %v", violations)
	}
}

// TestBlockMappingAuthorityPinsGlobalSerial requires the claim and its read to
// use the explicit global SERIAL constants, never the configurable session
// default that a multi-DC deployment may set to LOCAL_SERIAL.
func TestBlockMappingAuthorityPinsGlobalSerial(t *testing.T) {
	sourceBytes, err := os.ReadFile(filepath.Join(r3RepositoryRoot(t), "internal", "db", "block_mapping_authority.go"))
	if err != nil {
		t.Fatalf("read primitive: %v", err)
	}
	source := string(sourceBytes)
	claim := blockMappingAuthorityFuncBody(t, source, "func claimBlockMappingAuthority(")
	if !strings.Contains(claim, "SerialConsistency(LibraryHeadSerialConsistency)") || strings.Contains(claim, "LocalSerial") {
		t.Fatal("mapping authority claim must pin global SERIAL explicitly")
	}
	read := blockMappingAuthorityFuncBody(t, source, "func readBlockMappingAuthority(")
	if !strings.Contains(read, "\tConsistency(IdentityAuthorityReadConsistency)") || strings.Contains(read, "LocalSerial") {
		t.Fatal("mapping authority read must use the global SERIAL read consistency")
	}
	if LibraryHeadSerialConsistency.String() != "SERIAL" || IdentityAuthorityReadConsistency.String() != "SERIAL" {
		t.Fatalf("mapping authority serial domain = %s/%s, want SERIAL/SERIAL", LibraryHeadSerialConsistency, IdentityAuthorityReadConsistency)
	}
	freeze := blockMappingAuthorityFuncBody(t, source, "func freezeBlockMappingProjection(")
	if !strings.Contains(freeze, "UPDATE block_id_mappings USING TIMESTAMP ? SET internal_id = ?") ||
		!strings.Contains(freeze, "BlockMappingProjectionFrozenTimestamp, authority,") ||
		!strings.Contains(freeze, "Consistency(gocql.EachQuorum)") {
		t.Fatal("projection freeze must rewrite the ordinary row at the dominant frozen timestamp and EACH_QUORUM")
	}
	projection := blockMappingAuthorityFuncBody(t, source, "func readBlockMappingProjection(")
	if !strings.Contains(projection, "WRITETIME(internal_id)") || !strings.Contains(projection, "Consistency(gocql.EachQuorum)") {
		t.Fatal("projection reads must observe the write timestamp at EACH_QUORUM")
	}
	promoteBody := blockMappingAuthorityFuncBody(t, source, "func promoteBlockMappingAuthority(")
	if !strings.Contains(promoteBody, "ports.freeze(ctx, identity, authority)") {
		t.Fatal("promotion must freeze the ordinary projection to the durable authority")
	}
	promote := blockMappingAuthorityFuncBody(t, source, "func acquireBlockMappingClaim(")
	proveAt := strings.Index(promote, "ports.prove(")
	claimAt := strings.Index(promote, "ports.claim(")
	if proveAt < 0 || claimAt < 0 || proveAt > claimAt {
		t.Fatal("promotion must obtain provenance before it claims")
	}
}

// TestBlockMappingProjectionReadsPinLocalQuorum protects the reader side of
// the temporal projection. EACH_QUORUM freezes a quorum in each DC, which
// cannot prevent a ONE read from selecting a stale replica in an RF>1 DC.
func TestBlockMappingProjectionReadsPinLocalQuorum(t *testing.T) {
	repoRoot := r3RepositoryRoot(t)
	source, err := os.ReadFile(filepath.Join(repoRoot, "internal", "db", "block_references.go"))
	if err != nil {
		t.Fatalf("read mapping reader: %v", err)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), "internal/db/block_references.go", source, 0)
	if err != nil {
		t.Fatalf("parse mapping reader: %v", err)
	}
	foundReader, pinsConsistency := false, false
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "GetBlockIDMappingContext" || function.Body == nil {
			continue
		}
		foundReader = true
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || blockMappingCallName(call.Fun) != "Query" || len(call.Args) == 0 {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok {
				return true
			}
			query, ok := constantIdentityAuthorityString(literal)
			if !ok || !blockMappingSelectPattern.MatchString(strings.Join(strings.Fields(query), " ")) {
				return true
			}
			consistencies := blockMappingQueryConsistencyNames(function.Body, call)
			pinsConsistency = len(consistencies) == 1 && consistencies[0] == "BlockMappingProjectionReadConsistency"
			return true
		})
	}
	if !foundReader {
		t.Fatal("GetBlockIDMappingContext was not found; projection reader contract would be vacuous")
	}
	if !pinsConsistency {
		t.Fatal("productive block mapping resolution must pin BlockMappingProjectionReadConsistency")
	}
	if BlockMappingProjectionReadConsistency.String() != "LOCAL_QUORUM" {
		t.Fatalf("productive block mapping consistency = %s, want LOCAL_QUORUM", BlockMappingProjectionReadConsistency)
	}
}

func TestBlockMappingAuthoritySerialReadRetriesOnlyAmbiguousCAS(t *testing.T) {
	t.Run("ambiguous result is retried", func(t *testing.T) {
		calls := 0
		err := retryAmbiguousGlobalSerialRead(context.Background(), func() error {
			calls++
			if calls == 1 {
				return &gocql.RequestErrCASWriteUnknown{}
			}
			return nil
		})
		if err != nil || calls != 2 {
			t.Fatalf("ambiguous SERIAL read = %v after %d attempts, want success after 2", err, calls)
		}
	})

	t.Run("other errors fail without retry", func(t *testing.T) {
		wantErr := errors.New("unavailable")
		calls := 0
		err := retryAmbiguousGlobalSerialRead(context.Background(), func() error {
			calls++
			return wantErr
		})
		if !errors.Is(err, wantErr) || calls != 1 {
			t.Fatalf("non-CAS read error = %v after %d attempts, want immediate original error", err, calls)
		}
	})

	t.Run("retry count is bounded", func(t *testing.T) {
		calls := 0
		err := retryAmbiguousGlobalSerialRead(context.Background(), func() error {
			calls++
			return &gocql.RequestErrCASWriteUnknown{}
		})
		if err == nil || calls != blockMappingSerialReadRetryLimit+1 {
			t.Fatalf("persistent ambiguous SERIAL read = %v after %d attempts, want bounded failure after %d", err, calls, blockMappingSerialReadRetryLimit+1)
		}
	})
}

// TestBlockMappingMutationsAreRepositoryWideInventoried resolves production
// Query arguments through constant concatenation, strings.Join, local
// bindings, and same-package helper returns. It inventories every mapping
// mutation and every SELECT reader, including the explicitly pre-GC exception.
func TestBlockMappingMutationsAreRepositoryWideInventoried(t *testing.T) {
	repoRoot := r3RepositoryRoot(t)
	const ordinaryWriter = "insertBlockIDMappingForWriteCheckFn"
	const freezeWriter = "freezeBlockMappingProjection"
	const primitivePath = "internal/db/block_mapping_authority.go"
	const gcReaderMarker = "PCD1B3-PRE-GC-SESSION-CONSISTENCY-EXCEPTION"
	type readerContract struct {
		path               string
		consistency        string
		sessionConsistency bool
		preGC              bool
	}
	readers := map[string]readerContract{
		"GetBlockIDMappingContext": {
			path: "internal/db/block_references.go", consistency: "BlockMappingProjectionReadConsistency",
		},
		"getBlockIDMappingForWriteCheck": {
			path: "internal/db/block_references.go", sessionConsistency: true,
		},
		"readBlockMappingProjection": {
			path: primitivePath, consistency: "gocql.EachQuorum",
		},
		"lookupBlockMapping": {
			path: "internal/gc/store_cassandra.go", preGC: true,
		},
	}
	var violations []string
	insertCount, freezeCount := 0, 0
	productionFiles := identityAuthorityProductionGoFiles(t, repoRoot)
	if len(productionFiles) == 0 {
		t.Fatal("scanned no production Go sources; mapping mutation inventory would pass vacuously")
	}
	type parsedProductionFile struct {
		path string
		file *ast.File
	}
	parsedFiles := make([]parsedProductionFile, 0, len(productionFiles))
	packages := map[string]*blockMappingSourceStrings{}
	for _, path := range productionFiles {
		parsed := blockMappingAuthorityParse(t, path)
		parsedFiles = append(parsedFiles, parsedProductionFile{path: path, file: parsed})
		packageRoot := filepath.Dir(path)
		source := packages[packageRoot]
		if source == nil {
			source = &blockMappingSourceStrings{
				functions:     map[string]*ast.FuncDecl{},
				functionPaths: map[string]string{},
				globals:       map[string]ast.Expr{},
				dynamicValues: map[ast.Expr]bool{},
			}
			packages[packageRoot] = source
		}
		relFunctionPath, err := filepath.Rel(repoRoot, path)
		if err != nil {
			t.Fatalf("relative path %s: %v", path, err)
		}
		for _, declaration := range parsed.Decls {
			switch value := declaration.(type) {
			case *ast.FuncDecl:
				source.functions[value.Name.Name] = value
				source.functionPaths[value.Name.Name] = filepath.ToSlash(relFunctionPath)
			case *ast.GenDecl:
				for _, spec := range value.Specs {
					valueSpec, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for index, name := range valueSpec.Names {
						if index < len(valueSpec.Values) {
							source.globals[name.Name] = valueSpec.Values[index]
						}
					}
				}
			}
		}
	}
	readerCounts := map[string]int{}
	readerCallsites := map[string]map[string]bool{}
	insertCallsites := map[string]bool{}
	freezeCallsites := map[string]bool{}
	dynamicQueryCallsites := map[string]bool{}
	dynamicQueryCounts := map[string]int{}
	checkStandaloneCQL := func(relPath, owner, statement string) {
		normalized := strings.Join(strings.Fields(statement), " ")
		if !blockMappingTableMentionPattern.MatchString(normalized) || !blockMappingCQLKeywordPattern.MatchString(normalized) {
			return
		}
		upper := strings.ToUpper(normalized)
		switch {
		case blockMappingSelectPattern.MatchString(normalized):
			violations = append(violations, relPath+": unclassified production block_id_mappings SELECT in "+owner)
		case strings.HasPrefix(upper, "DELETE FROM BLOCK_ID_MAPPINGS"):
			violations = append(violations, relPath+": production DELETE from block_id_mappings is prohibited by R11a")
		case strings.HasPrefix(upper, "INSERT INTO BLOCK_ID_MAPPINGS"):
			if strings.Contains(upper, "USING TIMESTAMP") {
				violations = append(violations, relPath+": ordinary block_id_mappings INSERT must not specify USING TIMESTAMP")
			}
			if relPath != "internal/db/block_references.go" || owner != ordinaryWriter || !blockMappingInsertPattern.MatchString(normalized) {
				violations = append(violations, relPath+": unauthorized or malformed ordinary block_id_mappings INSERT in "+owner)
			} else {
				insertCount++
			}
		case strings.HasPrefix(upper, "UPDATE BLOCK_ID_MAPPINGS"):
			if relPath != primitivePath || owner != freezeWriter || !blockMappingFreezePattern.MatchString(normalized) {
				violations = append(violations, relPath+": only freezeBlockMappingProjection may use explicit-timestamp block_id_mappings UPDATE")
			} else {
				freezeCount++
			}
		default:
			violations = append(violations, relPath+": unrecognized/dynamic block_id_mappings CQL mutation in "+owner)
		}
	}
	checkReader := func(relPath, owner string, query *ast.CallExpr, body ast.Node, statement string) {
		contract, allowed := readers[owner]
		if !allowed || contract.path != relPath {
			violations = append(violations, relPath+": unclassified production block_id_mappings SELECT in "+owner)
			return
		}
		querySite := relPath + ":" + strconv.Itoa(int(query.Pos()))
		if readerCallsites[owner] == nil {
			readerCallsites[owner] = map[string]bool{}
		}
		if !readerCallsites[owner][querySite] {
			readerCallsites[owner][querySite] = true
			readerCounts[owner]++
		}
		consistencies := blockMappingQueryConsistencyNames(body, query)
		if contract.preGC {
			if len(consistencies) != 0 {
				violations = append(violations, relPath+":"+owner+" PRE-GC exception unexpectedly changed its consistency contract")
			}
			if !strings.Contains(statement, "SELECT internal_id FROM block_id_mappings") {
				violations = append(violations, relPath+":"+owner+" PRE-GC query shape changed without reclassification")
			}
			return
		}
		if contract.sessionConsistency {
			if len(consistencies) != 0 {
				violations = append(violations, relPath+":"+owner+" writer pre-check must inherit session consistency, found "+strings.Join(consistencies, ","))
			}
			return
		}
		if len(consistencies) != 1 || consistencies[0] != contract.consistency {
			violations = append(violations, relPath+":"+owner+" block mapping SELECT consistency="+strings.Join(consistencies, ",")+", want "+contract.consistency)
		}
	}

	var checkQuery func(source *blockMappingSourceStrings, node ast.Node, owner, relPath string, body ast.Node, function *ast.FuncDecl, functionLiteral *ast.FuncLit, arguments []ast.Expr, outer map[string]ast.Expr, active map[string]bool)
	checkQuery = func(source *blockMappingSourceStrings, node ast.Node, owner, relPath string, body ast.Node, function *ast.FuncDecl, functionLiteral *ast.FuncLit, arguments []ast.Expr, outer map[string]ast.Expr, active map[string]bool) {
		ast.Inspect(node, func(child ast.Node) bool {
			call, ok := child.(*ast.CallExpr)
			if !ok {
				return true
			}
			callName := blockMappingCallName(call.Fun)
			locals := source.queryLocalsAt(function, functionLiteral, arguments, outer, call.Pos())
			if callName != "Query" {
				var helper *ast.FuncDecl
				if identifier, ok := call.Fun.(*ast.Ident); ok {
					helper = source.functions[identifier.Name]
				}
				if helper != nil && helper.Body != nil && blockMappingHasDirectQuery(helper.Body) && !active[callName] {
					nestedActive := make(map[string]bool, len(active)+1)
					for name, inUse := range active {
						nestedActive[name] = inUse
					}
					nestedActive[callName] = true
					helperPath := source.functionPaths[callName]
					if helperPath == "" {
						helperPath = relPath
					}
					checkQuery(source, helper.Body, callName, helperPath, helper.Body, helper, nil, call.Args, locals, nestedActive)
				}
				return true
			}
			if len(call.Args) == 0 {
				return true
			}
			resolved := source.resolve(call.Args[0], locals, map[string]bool{}, map[string]bool{}, 0)
			normalized := strings.Join(strings.Fields(resolved.text), " ")
			if !resolved.constant {
				if blockMappingMigrationQueryAllowlisted(relPath, owner, call.Args[0]) {
					return true
				}
				if blockMappingPotentialDynamicQuery(normalized, call.Args[0]) {
					violations = append(violations, relPath+": unresolved/dynamic block_id_mappings Query argument in "+owner)
					return true
				}
				if _, allowed := blockMappingDynamicQueryAllowlist[relPath][owner]; allowed {
					querySite := relPath + ":" + strconv.Itoa(int(call.Pos()))
					if !dynamicQueryCallsites[querySite] {
						dynamicQueryCallsites[querySite] = true
						dynamicQueryCounts[relPath+"::"+owner]++
					}
					return true
				}
				violations = append(violations, relPath+": unresolved/dynamic Query argument in "+owner+"; classify or allowlist this call site")
				return true
			}
			if !blockMappingTableMentionPattern.MatchString(normalized) {
				return true
			}
			upper := strings.ToUpper(normalized)
			switch {
			case blockMappingSelectPattern.MatchString(normalized):
				checkReader(relPath, owner, call, body, normalized)
			case strings.HasPrefix(upper, "DELETE FROM BLOCK_ID_MAPPINGS"):
				violations = append(violations, relPath+": production DELETE from block_id_mappings is prohibited by R11a")
			case strings.HasPrefix(upper, "INSERT INTO BLOCK_ID_MAPPINGS"):
				if strings.Contains(upper, "USING TIMESTAMP") {
					violations = append(violations, relPath+": ordinary block_id_mappings INSERT must not specify USING TIMESTAMP")
				}
				if relPath != "internal/db/block_references.go" || owner != ordinaryWriter || !blockMappingInsertPattern.MatchString(normalized) {
					violations = append(violations, relPath+": unauthorized or malformed ordinary block_id_mappings INSERT in "+owner)
				} else {
					querySite := relPath + ":" + strconv.Itoa(int(call.Pos()))
					if !insertCallsites[querySite] {
						insertCallsites[querySite] = true
						insertCount++
					}
				}
			case strings.HasPrefix(upper, "UPDATE BLOCK_ID_MAPPINGS"):
				if relPath != primitivePath || owner != freezeWriter || !blockMappingFreezePattern.MatchString(normalized) {
					violations = append(violations, relPath+": only freezeBlockMappingProjection may use explicit-timestamp block_id_mappings UPDATE")
				} else {
					querySite := relPath + ":" + strconv.Itoa(int(call.Pos()))
					if !freezeCallsites[querySite] {
						freezeCallsites[querySite] = true
						freezeCount++
					}
				}
			default:
				if blockMappingCQLKeywordPattern.MatchString(normalized) || strings.TrimSpace(normalized) == "block_id_mappings" {
					violations = append(violations, relPath+": unrecognized/dynamic block_id_mappings CQL mutation in "+owner)
				}
			}
			return true
		})
	}

	for _, parsedSource := range parsedFiles {
		path := parsedSource.path
		relPath, err := filepath.Rel(repoRoot, path)
		if err != nil {
			t.Fatalf("relative path %s: %v", path, err)
		}
		relPath = filepath.ToSlash(relPath)
		parsed := parsedSource.file
		source := packages[filepath.Dir(path)]
		for _, declaration := range parsed.Decls {
			switch value := declaration.(type) {
			case *ast.FuncDecl:
				checkQuery(source, value.Body, value.Name.Name, relPath, value.Body, value, nil, nil, source.globals, map[string]bool{value.Name.Name: true})
			case *ast.GenDecl:
				for _, spec := range value.Specs {
					if valueSpec, ok := spec.(*ast.ValueSpec); ok {
						for index, expression := range valueSpec.Values {
							owner := ""
							if index < len(valueSpec.Names) {
								owner = valueSpec.Names[index].Name
							}
							if functionLiteral, ok := expression.(*ast.FuncLit); ok {
								checkQuery(source, functionLiteral.Body, owner, relPath, functionLiteral.Body, nil, functionLiteral, nil, source.globals, map[string]bool{})
							} else {
								resolved := source.resolve(expression, source.globals, map[string]bool{}, map[string]bool{}, 0)
								checkStandaloneCQL(relPath, owner, resolved.text)
								checkQuery(source, expression, owner, relPath, expression, nil, nil, nil, source.globals, map[string]bool{})
							}
						}
					}
				}
			}
		}
	}
	for owner, contract := range readers {
		if readerCounts[owner] != 1 {
			violations = append(violations, contract.path+":"+owner+" SELECT inventory count="+strconv.Itoa(readerCounts[owner])+", want 1")
		}
	}
	for relPath, owners := range blockMappingDynamicQueryAllowlist {
		parsed := blockMappingAuthorityParse(t, filepath.Join(repoRoot, filepath.FromSlash(relPath)))
		functions := map[string]*ast.FuncDecl{}
		for _, declaration := range parsed.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok {
				functions[function.Name.Name] = function
			}
		}
		for owner, contract := range owners {
			key := relPath + "::" + owner
			if actual := dynamicQueryCounts[key]; actual != contract.count {
				violations = append(violations, relPath+":"+owner+" dynamic Query inventory count="+strconv.Itoa(actual)+", want "+strconv.Itoa(contract.count))
			}
			if contract.tableMarker != "" {
				function := functions[owner]
				foundMarker := false
				if function != nil {
					ast.Inspect(function.Body, func(node ast.Node) bool {
						literal, ok := node.(*ast.BasicLit)
						if !ok {
							return true
						}
						text, ok := constantIdentityAuthorityString(literal)
						if ok && strings.Contains(text, contract.tableMarker) {
							foundMarker = true
							return false
						}
						return true
					})
				}
				if !foundMarker {
					violations = append(violations, relPath+":"+owner+" dynamic Query lost fixed table marker "+contract.tableMarker)
				}
			}
			delete(dynamicQueryCounts, key)
		}
	}
	for key, count := range dynamicQueryCounts {
		violations = append(violations, key+" dynamic Query inventory count="+strconv.Itoa(count)+" is not classified")
	}
	lockSourcePath := filepath.Join(repoRoot, "internal", "gc", "store_cassandra.go")
	lockSource := blockMappingAuthorityParse(t, lockSourcePath)
	lockCalls := map[string]map[string]int{
		"acquireHardDeleteLock": {},
		"renewHardDeleteLock":   {},
		"releaseHardDeleteLock": {},
	}
	ast.Inspect(lockSource, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name, tracked := lockCalls[blockMappingCallName(call.Fun)]
		if !tracked {
			return true
		}
		if len(call.Args) < 3 {
			violations = append(violations, "internal/gc/store_cassandra.go: hard-delete lock helper has no literal table identity")
			return true
		}
		table, tableOK := call.Args[1].(*ast.BasicLit)
		column, columnOK := call.Args[2].(*ast.BasicLit)
		if !tableOK || !columnOK {
			violations = append(violations, "internal/gc/store_cassandra.go: hard-delete lock table and key column must be literals")
			return true
		}
		tableName, tableErr := strconv.Unquote(table.Value)
		columnName, columnErr := strconv.Unquote(column.Value)
		if tableErr != nil || columnErr != nil {
			violations = append(violations, "internal/gc/store_cassandra.go: invalid hard-delete lock table identity literal")
			return true
		}
		identity := tableName + "/" + columnName
		allowed := map[string]bool{
			"gc_library_hard_delete_locks/library_id": true,
			"gc_user_hard_delete_locks/user_id":       true,
			"gc_org_hard_delete_locks/org_id":         true,
		}
		if !allowed[identity] {
			violations = append(violations, "internal/gc/store_cassandra.go: unclassified hard-delete lock table identity "+identity)
		}
		name[identity]++
		return true
	})
	for name, identities := range lockCalls {
		for _, identity := range []string{
			"gc_library_hard_delete_locks/library_id",
			"gc_user_hard_delete_locks/user_id",
			"gc_org_hard_delete_locks/org_id",
		} {
			if actual := identities[identity]; actual != 1 {
				violations = append(violations, "internal/gc/store_cassandra.go:"+name+" "+identity+" call count="+strconv.Itoa(actual)+", want 1")
			}
		}
	}
	gcSource, err := os.ReadFile(filepath.Join(repoRoot, "internal", "gc", "store_cassandra.go"))
	if err != nil {
		t.Fatalf("read pre-GC reader exception: %v", err)
	}
	if !strings.Contains(string(gcSource), gcReaderMarker) {
		violations = append(violations, "GC mapping reader is missing its explicit PRE-GC session-consistency exception marker")
	}
	gcContract, err := os.ReadFile(filepath.Join(repoRoot, "docs", "PC-D1B-METADATA-IDENTITY-AUTHORITY.md"))
	if err != nil {
		t.Fatalf("read GC activation contract: %v", err)
	}
	if !strings.Contains(string(gcContract), "GC_ENABLED=false") {
		violations = append(violations, "PRE-GC mapping reader exception no longer documents GC_ENABLED=false")
	}
	if insertCount != 1 {
		violations = append(violations, "ordinary block_id_mappings INSERT inventory count="+strconv.Itoa(insertCount)+", want 1")
	}
	if freezeCount != 1 {
		violations = append(violations, "dominant-timestamp block_id_mappings freeze inventory count="+strconv.Itoa(freezeCount)+", want 1")
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("PCD1B3 BLOCK MAPPING MUTATION INVENTORY: %v", violations)
	}
}

func blockMappingAuthorityFuncBody(t *testing.T, source, signature string) string {
	t.Helper()
	start := strings.Index(source, signature)
	if start < 0 {
		t.Fatalf("%s not found", signature)
	}
	body := source[start:]
	if next := strings.Index(body[len(signature):], "\nfunc "); next >= 0 {
		body = body[:len(signature)+next]
	}
	return body
}
