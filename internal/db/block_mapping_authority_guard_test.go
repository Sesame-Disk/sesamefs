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

// Symbols that acquire or write mapping authority. Only the primitive, the
// certifier and the integration-only hook file may reference them.
var blockMappingAuthorityAcquisitionSymbols = map[string]map[string]bool{
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

// TestBlockMappingMutationsAreRepositoryWideInventoried scans production Go
// literals for every block_id_mappings mutation. The plain upload INSERT is
// the only ordinary write and may not set a timestamp; the authority freeze
// is the only explicit-timestamp mutation. Deletes remain prohibited by R11a.
func TestBlockMappingMutationsAreRepositoryWideInventoried(t *testing.T) {
	repoRoot := r3RepositoryRoot(t)
	const ordinaryWriter = "insertBlockIDMappingForWriteCheckFn"
	const freezeWriter = "freezeBlockMappingProjection"
	const primitivePath = "internal/db/block_mapping_authority.go"
	var violations []string
	insertCount, freezeCount := 0, 0
	productionFiles := identityAuthorityProductionGoFiles(t, repoRoot)
	if len(productionFiles) == 0 {
		t.Fatal("scanned no production Go sources; mapping mutation inventory would pass vacuously")
	}

	for _, path := range productionFiles {
		relPath, err := filepath.Rel(repoRoot, path)
		if err != nil {
			t.Fatalf("relative path %s: %v", path, err)
		}
		relPath = filepath.ToSlash(relPath)
		parsed := blockMappingAuthorityParse(t, path)
		inspect := func(node ast.Node, owner string) {
			ast.Inspect(node, func(child ast.Node) bool {
				value, ok := child.(*ast.BasicLit)
				if !ok || value.Kind != token.STRING {
					return true
				}
				statement, err := strconv.Unquote(value.Value)
				if err != nil || !blockMappingTableMentionPattern.MatchString(statement) {
					return true
				}
				normalized := strings.Join(strings.Fields(statement), " ")
				upper := strings.ToUpper(normalized)
				switch {
				case blockMappingSelectPattern.MatchString(normalized):
					return true
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
		for _, declaration := range parsed.Decls {
			switch value := declaration.(type) {
			case *ast.FuncDecl:
				inspect(value.Body, value.Name.Name)
			case *ast.GenDecl:
				for _, spec := range value.Specs {
					if valueSpec, ok := spec.(*ast.ValueSpec); ok {
						for index, expression := range valueSpec.Values {
							owner := ""
							if index < len(valueSpec.Names) {
								owner = valueSpec.Names[index].Name
							}
							inspect(expression, owner)
						}
					} else {
						inspect(spec, "")
					}
				}
			}
		}
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
