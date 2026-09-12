package publication

import (
	"errors"
	"testing"
)

func validAttempt() AttemptIdentity {
	return AttemptIdentity{
		OrgID:          "org-1",
		RepoID:         "repo-1",
		Attempt:        "attempt-1",
		TargetCommitID: "commit-1",
		ExpectedHead:   "commit-0",
	}
}

func TestAttemptIdentityValidateAcceptsCompleteIdentity(t *testing.T) {
	if err := validAttempt().Validate(); err != nil {
		t.Fatalf("complete identity rejected: %v", err)
	}
}

// v2/SeafHTTP/OnlyOffice use the new commit id as the attempt id; Sync mints a
// fresh UUID that is never the target commit id. Both shapes are explicit and
// both are valid.
func TestAttemptIdentityAllowsAttemptEqualOrDistinctFromTargetCommit(t *testing.T) {
	v2Shape := validAttempt()
	v2Shape.Attempt = AttemptID(v2Shape.TargetCommitID)
	if err := v2Shape.Validate(); err != nil {
		t.Fatalf("attempt == target commit (v2 shape) rejected: %v", err)
	}
	syncShape := validAttempt()
	syncShape.Attempt = "5e5f4c9a-0000-4000-8000-000000000000"
	if err := syncShape.Validate(); err != nil {
		t.Fatalf("attempt != target commit (Sync shape) rejected: %v", err)
	}
}

func TestAttemptIdentityAllowsEmptyExpectedHeadForInitialPublish(t *testing.T) {
	initial := validAttempt()
	initial.ExpectedHead = ""
	if err := initial.Validate(); err != nil {
		t.Fatalf("initial-HEAD identity rejected: %v", err)
	}
}

func TestAttemptIdentityValidateRejectsMissingComponents(t *testing.T) {
	cases := map[string]func(*AttemptIdentity){
		"org":     func(a *AttemptIdentity) { a.OrgID = "" },
		"repo":    func(a *AttemptIdentity) { a.RepoID = "" },
		"attempt": func(a *AttemptIdentity) { a.Attempt = "" },
		"commit":  func(a *AttemptIdentity) { a.TargetCommitID = "" },
	}
	for name, clear := range cases {
		identity := validAttempt()
		clear(&identity)
		if err := identity.Validate(); !errors.Is(err, ErrInvalidAttemptIdentity) {
			t.Fatalf("missing %s: error = %v, want ErrInvalidAttemptIdentity", name, err)
		}
	}
	if err := (AttemptIdentity{}).Validate(); !errors.Is(err, ErrInvalidAttemptIdentity) {
		t.Fatalf("zero identity: error = %v, want ErrInvalidAttemptIdentity", err)
	}
}
