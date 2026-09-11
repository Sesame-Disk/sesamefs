package v2

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// The conditional initial-HEAD publish (ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01)
// is split into a Cassandra call and pure decision functions so the contract
// can be pinned without a cluster: a caller always ends up with the head it
// published or the head someone else published, never with an empty head or
// a phantom row; UNKNOWN never cleans up, a demonstrated KNOWN_LOSER may
// discard its own attempt-unique commit, and so may a definitive rejection
// (ErrLibraryHeadNotFound / ErrLibraryHeadUninitializable) whose CAS
// demonstrably never published it.

func TestClassifyInitialHeadCAS(t *testing.T) {
	createdAt := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name        string
		applied     bool
		state       map[string]interface{}
		wantHead    string
		wantOutcome InitialHeadOutcome
		wantErrIs   error
		wantErrText string
	}{
		{name: "applied wins with own commit", applied: true, state: map[string]interface{}{}, wantHead: "c1", wantOutcome: InitialHeadApplied},
		{name: "another writer already initialized is a KNOWN_LOSER", applied: false, state: map[string]interface{}{"head_commit_id": "other", "created_at": createdAt}, wantHead: "other", wantOutcome: InitialHeadAlreadyInitialized},
		// created_at only gates an UNINITIALIZED row: an existing non-empty HEAD
		// is adopted whatever created_at holds, never refused (it must not be
		// overwritten and there is nothing to initialize).
		{name: "existing head with null created_at is adopted, not refused", applied: false, state: map[string]interface{}{"head_commit_id": "other", "created_at": time.Time{}}, wantHead: "other", wantOutcome: InitialHeadAlreadyInitialized},
		{name: "row does not exist: no columns returned", applied: false, state: map[string]interface{}{}, wantErrIs: ErrLibraryHeadNotFound},
		{name: "empty-string head is refused, not repaired", applied: false, state: map[string]interface{}{"head_commit_id": "", "created_at": createdAt}, wantErrIs: ErrLibraryHeadUninitializable, wantErrText: "empty string"},
		{name: "null created_at is refused", applied: false, state: map[string]interface{}{"head_commit_id": "", "created_at": time.Time{}}, wantErrIs: ErrLibraryHeadUninitializable, wantErrText: "created_at is null"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			head, outcome, err := classifyInitialHeadCAS(tc.applied, tc.state, "c1")
			if tc.wantErrIs != nil {
				if !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("err = %v, want %v", err, tc.wantErrIs)
				}
				if tc.wantErrText != "" && !strings.Contains(err.Error(), tc.wantErrText) {
					t.Fatalf("err = %v, want text %q", err, tc.wantErrText)
				}
				if head != "" || outcome != "" {
					t.Fatalf("on error head=%q outcome=%q, want empty", head, outcome)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if head != tc.wantHead || outcome != tc.wantOutcome {
				t.Fatalf("got head=%q outcome=%q, want head=%q outcome=%q", head, outcome, tc.wantHead, tc.wantOutcome)
			}
		})
	}
}

// TestResolveInitialHeadAmbiguity pins the ambiguity matrix. The load-bearing
// row is "ambiguous and another head is visible": the CAS may have applied
// and HEAD may already have advanced past our commit, so that is UNKNOWN,
// never a loss that authorizes cleanup.
func TestResolveInitialHeadAmbiguity(t *testing.T) {
	ambiguous := &gocql.RequestErrCASWriteUnknown{}
	cases := []struct {
		name        string
		err         error
		confirm     func() (string, bool, error)
		wantHead    string
		wantOutcome InitialHeadOutcome
		wantErrIs   error
		wantErr     bool
	}{
		{
			name:    "non-ambiguous error is returned as failure without confirmation",
			err:     errors.New("invalid query"),
			confirm: func() (string, bool, error) { t.Fatal("confirm must not run"); return "", false, nil },
			wantErr: true,
		},
		{
			name:        "ambiguous but our commit is visible: APPLIED",
			err:         ambiguous,
			confirm:     func() (string, bool, error) { return "c1", true, nil },
			wantHead:    "c1",
			wantOutcome: InitialHeadApplied,
		},
		{
			name:        "ambiguous and another head is visible: UNKNOWN, adopt it, never cleanup",
			err:         fmt.Errorf("wrapped: %w", gocql.ErrTimeoutNoResponse),
			confirm:     func() (string, bool, error) { return "c2-advanced-past-c1", false, nil },
			wantHead:    "c2-advanced-past-c1",
			wantOutcome: InitialHeadUnknown,
		},
		{
			name:      "ambiguous and confirmation fails: unknown error",
			err:       ambiguous,
			confirm:   func() (string, bool, error) { return "", false, errors.New("read failed") },
			wantErrIs: ErrLibraryHeadPublicationUnknown,
		},
		{
			name:      "ambiguous and no head visible: unknown error, never empty success",
			err:       ambiguous,
			confirm:   func() (string, bool, error) { return "", false, nil },
			wantErrIs: ErrLibraryHeadPublicationUnknown,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			head, outcome, err := resolveInitialHeadAmbiguity("repo", "c1", tc.err, tc.confirm)
			if tc.wantErrIs != nil {
				if !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("err = %v, want %v", err, tc.wantErrIs)
				}
				return
			}
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				if errors.Is(err, ErrLibraryHeadPublicationUnknown) {
					t.Fatalf("non-ambiguous failure must not be classified UNKNOWN: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if head != tc.wantHead || outcome != tc.wantOutcome {
				t.Fatalf("got head=%q outcome=%q, want head=%q outcome=%q", head, outcome, tc.wantHead, tc.wantOutcome)
			}
		})
	}
}

// TestAmbiguousInitialCASThatAppliedThenAdvancedRetainsCommit is the
// required regression: initial CAS C0 applies, the response is lost, HEAD
// advances C0 -> C1, the confirmation read observes C1. The outcome must be
// UNKNOWN and C0 (now canonical ancestry) must never be discarded.
func TestAmbiguousInitialCASThatAppliedThenAdvancedRetainsCommit(t *testing.T) {
	head, outcome, err := resolveInitialHeadAmbiguity("repo", "c0", &gocql.RequestErrCASWriteUnknown{}, func() (string, bool, error) {
		return "c1", false, nil // HEAD already advanced past our (applied) c0
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if outcome != InitialHeadUnknown || head != "c1" {
		t.Fatalf("got head=%q outcome=%q, want c1/UNKNOWN", head, outcome)
	}
	if ShouldDiscardLosingInitialCommit(outcome, "c0", head) {
		t.Fatal("UNKNOWN must never authorize discarding c0: it may be c1's parent")
	}
}

func TestShouldDiscardLosingInitialCommit(t *testing.T) {
	cases := []struct {
		name    string
		outcome InitialHeadOutcome
		losing  string
		winner  string
		want    bool
	}{
		{"known loser with its own attempt-unique commit", InitialHeadAlreadyInitialized, "mine", "theirs", true},
		{"known loser whose id equals the winner (never delete the canonical head)", InitialHeadAlreadyInitialized, "same", "same", false},
		{"unknown never cleans up", InitialHeadUnknown, "mine", "theirs", false},
		{"applied never cleans up", InitialHeadApplied, "mine", "mine", false},
		{"empty losing id is a no-op", InitialHeadAlreadyInitialized, "", "theirs", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldDiscardLosingInitialCommit(tc.outcome, tc.losing, tc.winner); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestInitializationErrorForbidsRollback pins the creation-path rule: an
// outcome that may already be published (UNKNOWN, or an adopted HEAD whose
// commit is not yet locally visible) must never trigger rollbackNewLibrary;
// a definitive pre-publication failure still may.
func TestInitializationErrorForbidsRollback(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"publication unknown", fmt.Errorf("failed to initialize library head: %w", errors.Join(ErrLibraryHeadPublicationUnknown, errors.New("x"))), true},
		{"adopted head commit not visible locally", fmt.Errorf("adopted existing library head h: %w", ErrLibraryHeadCommitNotVisibleLocally), true},
		{"row not found is definitive", ErrLibraryHeadNotFound, false},
		{"invalid row is definitive", ErrLibraryHeadUninitializable, false},
		// Definitive for THIS attempt (it may roll back) — but rollbackNewLibrary
		// still takes authority in the HEAD domain first, so another
		// initializer's published HEAD survives (rollback_new_library_integration_test.go).
		{"generic pre-publication failure is definitive", errors.New("failed to persist initial fs state"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := InitializationErrorForbidsRollback(tc.err); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestInitialCommitIDIsAttemptUnique: same repo, root, and instant must not
// collide (the Sync initializer used to derive its id from the second, so a
// loser could delete a winner's row).
func TestInitialCommitIDIsAttemptUnique(t *testing.T) {
	now := time.Now()
	a := InitialCommitID("repo", "root", now)
	b := InitialCommitID("repo", "root", now)
	if a == b {
		t.Fatalf("two attempts at the same instant produced the same initial commit id %s", a)
	}
	if len(a) != 40 {
		t.Fatalf("initial commit id must stay 40 hex chars (Seafile-compatible), got %d", len(a))
	}
}
