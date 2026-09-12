package publication

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestWorkSetScopeDeclaresOnlyTheCandidateScope pins that PC-1 froze nothing:
// the only declared scope is today's candidate. Adding a scope (for example an
// inherited-dependency scope) is the additive path and must arrive together
// with the decision recorded for
// ISSUE-PC0-INHERITED-DEPENDENCY-CONTINUITY-01; update this test then.
func TestWorkSetScopeDeclaresOnlyTheCandidateScope(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "evidence.go", nil, 0)
	if err != nil {
		t.Fatalf("parse evidence.go: %v", err)
	}
	var declared []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if ident, ok := value.Type.(*ast.Ident); !ok || ident.Name != "WorkSetScope" {
				continue
			}
			for _, name := range value.Names {
				declared = append(declared, name.Name)
			}
		}
	}
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
