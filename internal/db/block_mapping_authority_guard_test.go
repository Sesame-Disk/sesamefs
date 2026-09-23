package db

import (
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
)

const blockMappingAuthorityClaimsTable = "block_mapping_authority_claims"

var (
	blockMappingAuthorityInsertPattern = regexp.MustCompile(`(?is)^INSERT\s+INTO\s+block_mapping_authority_claims\s*\([^)]*\)\s*VALUES\s*\([^)]*\)\s*IF\s+NOT\s+EXISTS$`)
	blockMappingAuthoritySelectPattern = regexp.MustCompile(`(?is)^SELECT\s+.+\s+FROM\s+block_mapping_authority_claims\s+WHERE\s+.+$`)
	blockMappingAuthorityCreatePattern = regexp.MustCompile(`(?is)^CREATE\s+TABLE\s+IF\s+NOT\s+EXISTS\s+block_mapping_authority_claims\b`)
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
