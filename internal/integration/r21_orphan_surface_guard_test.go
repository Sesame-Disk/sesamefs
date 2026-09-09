package integration

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func stripCQLComments(query string) string {
	var out strings.Builder
	out.Grow(len(query))
	inSingleQuote := false
	inDoubleQuote := false
	for i := 0; i < len(query); {
		char := query[i]
		if inSingleQuote {
			out.WriteByte(char)
			i++
			if char == '\'' {
				if i < len(query) && query[i] == '\'' {
					out.WriteByte(query[i])
					i++
					continue
				}
				inSingleQuote = false
			}
			continue
		}
		if inDoubleQuote {
			out.WriteByte(char)
			i++
			if char == '"' {
				if i < len(query) && query[i] == '"' {
					out.WriteByte(query[i])
					i++
					continue
				}
				inDoubleQuote = false
			}
			continue
		}
		if char == '\'' {
			inSingleQuote = true
			out.WriteByte(char)
			i++
			continue
		}
		if char == '"' {
			inDoubleQuote = true
			out.WriteByte(char)
			i++
			continue
		}
		if char == '-' && i+1 < len(query) && query[i+1] == '-' {
			out.WriteString("  ")
			i += 2
			for i < len(query) && query[i] != '\r' && query[i] != '\n' {
				i++
			}
			continue
		}
		if char == '/' && i+1 < len(query) && query[i+1] == '*' {
			out.WriteString("  ")
			i += 2
			for i < len(query) {
				if query[i] == '*' && i+1 < len(query) && query[i+1] == '/' {
					out.WriteString("  ")
					i += 2
					break
				}
				if query[i] == '\r' || query[i] == '\n' {
					out.WriteByte(query[i])
				} else {
					out.WriteByte(' ')
				}
				i++
			}
			continue
		}
		out.WriteByte(char)
		i++
	}
	return out.String()
}

// TestR21OrphanAuthoritySurface is an untagged source gate. It runs with the
// normal unit suite so a future refactor cannot quietly add an unguarded orphan
// creator, bypass the conditional orphan mutations, move G2 PREPARED publication
// away from the authorized worker path, or restore either removed authority
// surface. StartBlockDeleteOrphan remains the committed/G3 publication surface;
// PrepareBlockDeleteOrphan is the G2 publication surface.
func TestR21OrphanAuthoritySurface(t *testing.T) {
	root := filepath.Join("..", "..")
	skipDirs := map[string]bool{
		".git": true, "frontend": true, "mobile-frontend": true,
		"node_modules": true, "vendor": true,
	}
	creatorPattern := regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+gc_s3_orphans\b`)
	updatePattern := regexp.MustCompile(`(?i)\bUPDATE\s+gc_s3_orphans\b`)
	// Match the three predicate shapes this package actually writes, after CQL
	// comments have been removed so comment text cannot satisfy the gate.
	conditionalUpdatePattern := regexp.MustCompile(`(?is)(?:\bWHERE\b.*\bIF\s+(?:NOT\s+)?EXISTS\b|\bWHERE\b.*\bIF\s+\w+\s*=\s*\?)`)
	forbiddenIdentifiers := map[string]bool{
		"RecordS3Orphan":      true,
		"DeleteBlockS3Orphan": true,
	}

	stringLiterals := func(node ast.Node) []string {
		values := []string{}
		ast.Inspect(node, func(n ast.Node) bool {
			literal, ok := n.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				value = literal.Value
			}
			values = append(values, value)
			return true
		})
		return values
	}
	matchingLiterals := func(node ast.Node, pattern *regexp.Regexp) []string {
		matched := []string{}
		for _, value := range stringLiterals(node) {
			if pattern.MatchString(value) {
				matched = append(matched, value)
			}
		}
		return matched
	}
	prepareCallsiteFunctions := []string{}
	startCallsiteFunctions := []string{}
	functionName := func(fn *ast.FuncDecl) string {
		if fn.Recv == nil || len(fn.Recv.List) == 0 {
			return fn.Name.Name
		}
		switch receiver := fn.Recv.List[0].Type.(type) {
		case *ast.StarExpr:
			if ident, ok := receiver.X.(*ast.Ident); ok {
				return "(*" + ident.Name + ")." + fn.Name.Name
			}
		case *ast.Ident:
			return "(" + receiver.Name + ")." + fn.Name.Name
		}
		return fn.Name.Name
	}
	recordCallsites := func(node ast.Node, caller string) {
		ast.Inspect(node, func(n ast.Node) bool {
			selector, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "PrepareBlockDeleteOrphan":
				prepareCallsiteFunctions = append(prepareCallsiteFunctions, caller)
			case "StartBlockDeleteOrphan":
				startCallsiteFunctions = append(startCallsiteFunctions, caller)
			}
			return true
		})
	}

	totalCreators := 0
	creatorFunctions := []string{}
	scanned := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, src, 0)
		if err != nil {
			t.Errorf("%s: parse: %v", path, err)
			return nil
		}

		ast.Inspect(file, func(n ast.Node) bool {
			ident, ok := n.(*ast.Ident)
			if ok && forbiddenIdentifiers[ident.Name] {
				t.Errorf("%s: forbidden R21 identifier %q returned to production Go code", path, ident.Name)
			}
			return true
		})

		fileCreators := matchingLiterals(file, creatorPattern)
		totalCreators += len(fileCreators)
		for _, query := range matchingLiterals(file, updatePattern) {
			if !conditionalUpdatePattern.MatchString(stripCQLComments(query)) {
				t.Errorf("%s: canonical orphan UPDATE must be conditional with IF", path)
			}
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				recordCallsites(decl, "<package>")
				continue
			}
			if len(matchingLiterals(fn, creatorPattern)) > 0 {
				creatorFunctions = append(creatorFunctions, fn.Name.Name)
				if fn.Name.Name != "PrepareBlockDeleteOrphan" && fn.Name.Name != "StartBlockDeleteOrphan" {
					t.Errorf("%s: gc_s3_orphans creator is %s, want PrepareBlockDeleteOrphan or StartBlockDeleteOrphan", path, fn.Name.Name)
				}
			}
			if fn.Body == nil {
				continue
			}
			// Any selector naming the method counts, not only a direct call: a
			// method value (`start := store.StartBlockDeleteOrphan`) hands the
			// creator to an unauthorized path just as effectively. Package-level
			// method expressions are recorded as <package> above for the same reason.
			recordCallsites(fn.Body, functionName(fn))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan production Go sources: %v", err)
	}
	if scanned == 0 {
		t.Fatal("scanned no production Go sources")
	}
	if len(creatorFunctions) != 2 || !containsString(creatorFunctions, "PrepareBlockDeleteOrphan") || !containsString(creatorFunctions, "StartBlockDeleteOrphan") {
		t.Fatalf("expected exactly one G2 creator and one committed creator, got %v", creatorFunctions)
	}
	if totalCreators != 2 {
		t.Fatalf("found %d production INSERT INTO gc_s3_orphans statements, want exactly 2; creators=%v", totalCreators, creatorFunctions)
	}
	if len(prepareCallsiteFunctions) != 1 || prepareCallsiteFunctions[0] != "(*Worker).processBlock" {
		t.Fatalf("expected exactly one authorized PrepareBlockDeleteOrphan callsite in (*Worker).processBlock, got %v", prepareCallsiteFunctions)
	}
	if len(startCallsiteFunctions) != 0 {
		t.Fatalf("expected no production StartBlockDeleteOrphan callsites; G2 must use PrepareBlockDeleteOrphan, got %v", startCallsiteFunctions)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
