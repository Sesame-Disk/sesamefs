package v2

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// The rollback policy of a group-owned library creation is a pure decision
// over (fresh|resumed, which step failed, how it failed), pinned here without
// Cassandra. The invariants under test (H1 second review round):
//
//   - UNKNOWN / not-locally-visible from the initializer never rolls back,
//     fresh or resumed, and keeps the claim so the retry resumes.
//   - a RESUMED attempt never rolls back on ANY initializer failure: the
//     prior attempt preserved the library because it may be published, and
//     this attempt's failure grants no new authority over it.
//   - only a FRESH attempt with a DEFINITE initializer failure rolls back.
//   - once the initializer succeeded the HEAD is published: a share failure
//     preserves (pending), never destroys.
//   - completion releases the claim; a release failure is best effort.

type recordedSteps struct {
	initialized int
	shared      int
	abandoned   int
	completed   int
}

func fakeSteps(rec *recordedSteps, initErr, shareErr, abandonErr, completeErr error) groupLibraryCreationSteps {
	return groupLibraryCreationSteps{
		initialize: func(groupLibraryCreationClaim) error { rec.initialized++; return initErr },
		share:      func(groupLibraryCreationClaim) error { rec.shared++; return shareErr },
		abandon:    func(groupLibraryCreation) error { rec.abandoned++; return abandonErr },
		complete:   func(groupLibraryCreationClaim) error { rec.completed++; return completeErr },
	}
}

func TestFinishGroupLibraryCreationRollbackPolicy(t *testing.T) {
	definite := errors.New("failed to persist initial fs state: boom")
	unknown := errors.Join(ErrLibraryHeadPublicationUnknown, errors.New("cas timeout"))
	notVisible := errors.Join(ErrLibraryHeadCommitNotVisibleLocally, errors.New("commit abc"))
	shareErr := errors.New("share batch failed")

	cases := []struct {
		name        string
		resumed     bool
		initErr     error
		shareErr    error
		wantOutcome groupLibraryCreationOutcome
		wantAbandon int
		wantShare   int
		wantDone    int
	}{
		{name: "fresh, all good: completed, claim released", wantOutcome: groupLibraryCreationCompleted, wantShare: 1, wantDone: 1},
		{name: "resumed, all good: completed, claim released", resumed: true, wantOutcome: groupLibraryCreationCompleted, wantShare: 1, wantDone: 1},
		{name: "fresh + definite init failure: rolled back", initErr: definite, wantOutcome: groupLibraryCreationFailed, wantAbandon: 1},
		{name: "fresh + UNKNOWN: preserved, pending", initErr: unknown, wantOutcome: groupLibraryCreationPending},
		{name: "fresh + adopted head not visible: preserved, pending", initErr: notVisible, wantOutcome: groupLibraryCreationPending},
		{name: "resumed + definite init failure: retained, NO rollback", resumed: true, initErr: definite, wantOutcome: groupLibraryCreationFailed},
		{name: "resumed + UNKNOWN: preserved, pending", resumed: true, initErr: unknown, wantOutcome: groupLibraryCreationPending},
		{name: "fresh + share failure after publish: preserved, pending, NO rollback", shareErr: shareErr, wantOutcome: groupLibraryCreationPending, wantShare: 1},
		{name: "resumed + share failure after publish: preserved, pending, NO rollback", resumed: true, shareErr: shareErr, wantOutcome: groupLibraryCreationPending, wantShare: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recordedSteps{}
			creation := groupLibraryCreation{Claim: groupLibraryCreationClaim{LibraryID: "lib"}, Resumed: tc.resumed}
			outcome, err := finishGroupLibraryCreation(creation, fakeSteps(rec, tc.initErr, tc.shareErr, nil, nil))
			if outcome != tc.wantOutcome {
				t.Fatalf("outcome = %v, want %v (err=%v)", outcome, tc.wantOutcome, err)
			}
			if (err == nil) != (tc.wantOutcome == groupLibraryCreationCompleted) {
				t.Fatalf("err = %v for outcome %v", err, outcome)
			}
			if rec.initialized != 1 {
				t.Fatalf("initialize calls = %d, want 1", rec.initialized)
			}
			if rec.abandoned != tc.wantAbandon {
				t.Fatalf("abandon calls = %d, want %d", rec.abandoned, tc.wantAbandon)
			}
			if rec.shared != tc.wantShare {
				t.Fatalf("share calls = %d, want %d", rec.shared, tc.wantShare)
			}
			if rec.completed != tc.wantDone {
				t.Fatalf("complete calls = %d, want %d", rec.completed, tc.wantDone)
			}
			if tc.initErr != nil && !errors.Is(err, tc.initErr) {
				t.Fatalf("err %v does not wrap the initializer error %v", err, tc.initErr)
			}
			if tc.shareErr != nil && !errors.Is(err, tc.shareErr) {
				t.Fatalf("err %v does not wrap the share error %v", err, tc.shareErr)
			}
		})
	}
}

func TestFinishGroupLibraryCreationReportsRollbackFailure(t *testing.T) {
	rec := &recordedSteps{}
	initErr := errors.New("definite")
	rollbackErr := errors.New("rollback boom")
	outcome, err := finishGroupLibraryCreation(groupLibraryCreation{}, fakeSteps(rec, initErr, nil, rollbackErr, nil))
	if outcome != groupLibraryCreationFailed {
		t.Fatalf("outcome = %v, want failed", outcome)
	}
	if !errors.Is(err, initErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("err = %v, want both the initializer and the rollback error", err)
	}
}

func TestFinishGroupLibraryCreationCompletionReleaseIsBestEffort(t *testing.T) {
	rec := &recordedSteps{}
	outcome, err := finishGroupLibraryCreation(groupLibraryCreation{}, fakeSteps(rec, nil, nil, nil, errors.New("release timeout")))
	if outcome != groupLibraryCreationCompleted || err != nil {
		t.Fatalf("outcome=%v err=%v, want completed with nil error (a stale claim is healed by the next same-operation request)", outcome, err)
	}
	if rec.completed != 1 || rec.abandoned != 0 {
		t.Fatalf("completed=%d abandoned=%d", rec.completed, rec.abandoned)
	}
}

func TestWriteGroupLibraryCreationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name       string
		outcome    groupLibraryCreationOutcome
		err        error
		wantStatus int
		wantRetry  bool
	}{
		{name: "gate rejection carries its own status", outcome: groupLibraryCreationFailed, err: &groupLibraryCreationRejection{Status: http.StatusForbidden, Body: gin.H{"error": "limit"}}, wantStatus: http.StatusForbidden},
		{name: "explicit storage mismatch is a conflict", outcome: groupLibraryCreationFailed, err: errors.Join(ErrGroupLibraryCreationConflict, errors.New("s1 vs s2")), wantStatus: http.StatusConflict},
		{name: "pending is 503 with Retry-After", outcome: groupLibraryCreationPending, err: errors.New("pending"), wantStatus: http.StatusServiceUnavailable, wantRetry: true},
		{name: "definite failure is 500", outcome: groupLibraryCreationFailed, err: errors.New("boom"), wantStatus: http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			writeGroupLibraryCreationError(c, "[test]", tc.outcome, tc.err)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if got := w.Header().Get("Retry-After") != ""; got != tc.wantRetry {
				t.Fatalf("Retry-After present = %v, want %v", got, tc.wantRetry)
			}
		})
	}
}

func TestGroupLibraryCreationRejectionIsAnError(t *testing.T) {
	var err error = &groupLibraryCreationRejection{Status: 403}
	var rejection *groupLibraryCreationRejection
	if !errors.As(err, &rejection) || rejection.Status != 403 {
		t.Fatalf("rejection must round-trip through errors.As")
	}
}
