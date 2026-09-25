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
	functions map[string]*ast.FuncDecl
	globals   map[string]ast.Expr
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

func blockMappingLocalStrings(body ast.Node, parameters *ast.FieldList, arguments []ast.Expr, outer map[string]ast.Expr, source *blockMappingSourceStrings) map[string]ast.Expr {
	locals := make(map[string]ast.Expr, len(outer)+8)
	for name, expr := range outer {
		locals[name] = expr
	}
	if parameters != nil {
		argumentIndex := 0
		for _, field := range parameters.List {
			for _, name := range field.Names {
				if argumentIndex < len(arguments) {
					resolved := source.resolve(arguments[argumentIndex], outer, map[string]bool{}, map[string]bool{}, 0)
					if resolved.constant {
						locals[name.Name] = &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(resolved.text)}
					} else {
						locals[name.Name] = nil
					}
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
		return blockMappingResolvedString{text: text, constant: err == nil}
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
		functionLocals := blockMappingLocalStrings(function.Body, function.Type.Params, value.Args, locals, source)
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
		return blockMappingLocalStrings(function.Body, function.Type.Params, nil, source.globals, source)
	}
	if functionLiteral != nil {
		return blockMappingLocalStrings(functionLiteral.Body, functionLiteral.Type.Params, nil, source.globals, source)
	}
	return source.globals
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
		ast.Inspect(blockMappingAuthorityParse(t, path), func(node ast.Node) bool {
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
			if !ok || len(call.Args) != 1 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Consistency" {
				return true
			}
			argument, ok := call.Args[0].(*ast.Ident)
			if ok && argument.Name == "BlockMappingProjectionReadConsistency" {
				pinsConsistency = true
			}
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
		path        string
		consistency string
		preGC       bool
	}
	readers := map[string]readerContract{
		"GetBlockIDMappingContext": {
			path: "internal/db/block_references.go", consistency: "BlockMappingProjectionReadConsistency",
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
			source = &blockMappingSourceStrings{functions: map[string]*ast.FuncDecl{}, globals: map[string]ast.Expr{}}
			packages[packageRoot] = source
		}
		for _, declaration := range parsed.Decls {
			switch value := declaration.(type) {
			case *ast.FuncDecl:
				source.functions[value.Name.Name] = value
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
	checkReader := func(relPath, owner string, body ast.Node, statement string) {
		contract, allowed := readers[owner]
		if !allowed || contract.path != relPath {
			violations = append(violations, relPath+": unclassified production block_id_mappings SELECT in "+owner)
			return
		}
		readerCounts[owner]++
		var consistencies []string
		ast.Inspect(body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Consistency" {
				return true
			}
			argument := call.Args[0]
			switch value := argument.(type) {
			case *ast.Ident:
				consistencies = append(consistencies, value.Name)
			case *ast.SelectorExpr:
				if pkg, ok := value.X.(*ast.Ident); ok {
					consistencies = append(consistencies, pkg.Name+"."+value.Sel.Name)
				}
			}
			return true
		})
		if contract.preGC {
			if len(consistencies) != 0 {
				violations = append(violations, relPath+":"+owner+" PRE-GC exception unexpectedly changed its consistency contract")
			}
			if !strings.Contains(statement, "SELECT internal_id FROM block_id_mappings") {
				violations = append(violations, relPath+":"+owner+" PRE-GC query shape changed without reclassification")
			}
			return
		}
		if len(consistencies) != 1 || consistencies[0] != contract.consistency {
			violations = append(violations, relPath+":"+owner+" block mapping SELECT consistency="+strings.Join(consistencies, ",")+", want "+contract.consistency)
		}
	}
	checkQuery := func(source *blockMappingSourceStrings, node ast.Node, owner, relPath string, body ast.Node, locals map[string]ast.Expr) {
		ast.Inspect(node, func(child ast.Node) bool {
			call, ok := child.(*ast.CallExpr)
			if !ok || blockMappingCallName(call.Fun) != "Query" || len(call.Args) == 0 {
				return true
			}
			resolved := source.resolve(call.Args[0], locals, map[string]bool{}, map[string]bool{}, 0)
			normalized := strings.Join(strings.Fields(resolved.text), " ")
			lower := strings.ToLower(normalized)
			potentialMapping := strings.Contains(lower, "block_id_") || (strings.Contains(lower, "blockid") && strings.Contains(lower, "mapping"))
			ast.Inspect(call.Args[0], func(argumentNode ast.Node) bool {
				if identifier, ok := argumentNode.(*ast.Ident); ok {
					name := strings.ToLower(identifier.Name)
					if strings.Contains(name, "blockmapping") || strings.Contains(name, "block_mapping") || strings.Contains(name, "blockidmapping") {
						potentialMapping = true
					}
				}
				return true
			})
			if !blockMappingTableMentionPattern.MatchString(normalized) {
				if potentialMapping {
					violations = append(violations, relPath+": unresolved/dynamic block_id_mappings Query argument in "+owner)
				}
				return true
			}
			if !resolved.constant {
				violations = append(violations, relPath+": unresolved/dynamic block_id_mappings Query argument in "+owner)
				return true
			}
			upper := strings.ToUpper(normalized)
			switch {
			case blockMappingSelectPattern.MatchString(normalized):
				checkReader(relPath, owner, body, normalized)
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
				checkQuery(source, value.Body, value.Name.Name, relPath, value.Body, source.queryLocals(value, nil))
			case *ast.GenDecl:
				for _, spec := range value.Specs {
					if valueSpec, ok := spec.(*ast.ValueSpec); ok {
						for index, expression := range valueSpec.Values {
							owner := ""
							if index < len(valueSpec.Names) {
								owner = valueSpec.Names[index].Name
							}
							if functionLiteral, ok := expression.(*ast.FuncLit); ok {
								checkQuery(source, functionLiteral.Body, owner, relPath, functionLiteral.Body, source.queryLocals(nil, functionLiteral))
							} else {
								resolved := source.resolve(expression, source.globals, map[string]bool{}, map[string]bool{}, 0)
								checkStandaloneCQL(relPath, owner, resolved.text)
								checkQuery(source, expression, owner, relPath, expression, source.globals)
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
