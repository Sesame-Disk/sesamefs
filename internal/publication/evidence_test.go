package publication

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"sort"
	"strings"
	"testing"
)

// TestWorkSetScopeDeclaresOnlyTheCandidateScope pins that PC-1 froze nothing:
// the only declared scope is today's candidate. Adding a scope (for example an
// inherited-dependency scope) is the additive path and must arrive together
// with the decision recorded for
// ISSUE-PC0-INHERITED-DEPENDENCY-CONTINUITY-01; update this test then.
func TestWorkSetScopeDeclaresOnlyTheCandidateScope(t *testing.T) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseDir(fset, ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse publication package: %v", err)
	}
	pkgAST, ok := parsed["publication"]
	if !ok {
		t.Fatal("parsed publication package not found")
	}
	files := make([]*ast.File, 0, len(pkgAST.Files))
	for _, file := range pkgAST.Files {
		files = append(files, file)
	}
	checked, err := (&types.Config{Importer: importer.Default()}).Check(
		"github.com/Sesame-Disk/sesamefs/internal/publication",
		fset,
		files,
		nil,
	)
	if err != nil {
		t.Fatalf("type-check publication package: %v", err)
	}
	scopeType := checked.Scope().Lookup("WorkSetScope")
	if scopeType == nil {
		t.Fatal("WorkSetScope type not found")
	}
	var declared []string
	for _, name := range checked.Scope().Names() {
		constant, ok := checked.Scope().Lookup(name).(*types.Const)
		if !ok || !types.Identical(constant.Type(), scopeType.Type()) {
			continue
		}
		declared = append(declared, name)
	}
	sort.Strings(declared)
	if len(declared) != 1 || declared[0] != "WorkSetScopeNewlyLive" {
		t.Fatalf("WorkSetScope constants = %v; PC-1 declares only the candidate WorkSetScopeNewlyLive, widening the work set needs the inherited-dependency decision", declared)
	}
	if WorkSetScopeNewlyLive == "" {
		t.Fatal("candidate scope must not be the zero value")
	}
}

type staticEvidence struct{ scope WorkSetScope }

func (e staticEvidence) WorkSetScope() WorkSetScope { return e.scope }

type staticInput struct {
	attempt  AttemptIdentity
	evidence DependencyEvidence
}

func (i staticInput) Attempt() AttemptIdentity         { return i.attempt }
func (i staticInput) Dependencies() DependencyEvidence { return i.evidence }

// The boundary is opaque: an adapter can satisfy it with a test double that
// exposes no block list, which is what keeps the work set unfrozen.
func TestPublishableInputIsSatisfiableWithoutABlockList(t *testing.T) {
	var input PublishableInput = staticInput{
		attempt:  validAttempt(),
		evidence: staticEvidence{scope: WorkSetScopeNewlyLive},
	}
	if err := input.Attempt().Validate(); err != nil {
		t.Fatalf("attempt: %v", err)
	}
	if got := input.Dependencies().WorkSetScope(); got != WorkSetScopeNewlyLive {
		t.Fatalf("scope = %q, want %q", got, WorkSetScopeNewlyLive)
	}
}
