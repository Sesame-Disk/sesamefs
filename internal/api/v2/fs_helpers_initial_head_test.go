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
// is split into a Cassandra call and two pure decision functions so the
// contract can be pinned without a cluster: a caller must always end up with
// either the head it published or the head someone else published, and must
// never be handed an empty head or a phantom row.

func TestClassifyInitialHeadCAS(t *testing.T) {
	createdAt := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name        string
		applied     bool
		state       map[string]interface{}
		wantHead    string
		wantWon     bool
		wantErrIs   error
		wantErrText string
	}{
		{name: "applied wins with own commit", applied: true, state: map[string]interface{}{}, wantHead: "c1", wantWon: true},
		{name: "another writer already initialized", applied: false, state: map[string]interface{}{"head_commit_id": "other", "created_at": createdAt}, wantHead: "other", wantWon: false},
		{name: "row does not exist: no columns returned", applied: false, state: map[string]interface{}{}, wantErrIs: ErrLibraryHeadNotFound},
		{name: "empty-string head is refused, not repaired", applied: false, state: map[string]interface{}{"head_commit_id": "", "created_at": createdAt}, wantErrIs: ErrLibraryHeadUninitializable, wantErrText: "empty string"},
		{name: "null created_at is refused", applied: false, state: map[string]interface{}{"head_commit_id": "", "created_at": time.Time{}}, wantErrIs: ErrLibraryHeadUninitializable, wantErrText: "created_at is null"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			head, won, err := classifyInitialHeadCAS(tc.applied, tc.state, "c1")
			if tc.wantErrIs != nil {
				if !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("err = %v, want %v", err, tc.wantErrIs)
				}
				if tc.wantErrText != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErrText)) {
					t.Fatalf("err = %v, want text %q", err, tc.wantErrText)
				}
				if head != "" || won {
					t.Fatalf("on error head=%q won=%v, want empty/false", head, won)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if head != tc.wantHead || won != tc.wantWon {
				t.Fatalf("got head=%q won=%v, want head=%q won=%v", head, won, tc.wantHead, tc.wantWon)
			}
		})
	}
}

func TestResolveInitialHeadAmbiguity(t *testing.T) {
	ambiguous := gocql.RequestErrCASWriteUnknown{}
	cases := []struct {
		name      string
		err       error
		confirm   func() (string, bool, error)
		wantHead  string
		wantWon   bool
		wantErrIs error
		wantErr   bool
	}{
		{
			name:    "non-ambiguous error is returned as failure without confirmation",
			err:     errors.New("invalid query"),
			confirm: func() (string, bool, error) { t.Fatal("confirm must not run"); return "", false, nil },
			wantErr: true,
		},
		{
			name:     "ambiguous but our commit is visible: we won",
			err:      ambiguous,
			confirm:  func() (string, bool, error) { return "c1", true, nil },
			wantHead: "c1", wantWon: true,
		},
		{
			name:     "ambiguous and another head is visible: adopt it",
			err:      fmt.Errorf("wrapped: %w", gocql.ErrTimeoutNoResponse),
			confirm:  func() (string, bool, error) { return "other", false, nil },
			wantHead: "other", wantWon: false,
		},
		{
			name:      "ambiguous and confirmation fails: unknown",
			err:       ambiguous,
			confirm:   func() (string, bool, error) { return "", false, errors.New("read failed") },
			wantErrIs: ErrLibraryHeadPublicationUnknown,
		},
		{
			name:      "ambiguous and no head visible: unknown, never empty success",
			err:       ambiguous,
			confirm:   func() (string, bool, error) { return "", false, nil },
			wantErrIs: ErrLibraryHeadPublicationUnknown,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			head, won, err := resolveInitialHeadAmbiguity("repo", "c1", tc.err, tc.confirm)
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
			if head != tc.wantHead || won != tc.wantWon {
				t.Fatalf("got head=%q won=%v, want head=%q won=%v", head, won, tc.wantHead, tc.wantWon)
			}
		})
	}
}
