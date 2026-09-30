//go:build integration

package integration

import (
	"errors"
	"testing"
	"time"
)

func TestAdminProjectionCleanupSettlesLateOrphan(t *testing.T) {
	pending := true
	cleanups := 0
	err := settleAdminProjectionCleanup(time.Second, time.Millisecond,
		func() error {
			if pending {
				return errors.New("late orphan after cleanup snapshot")
			}
			return nil
		},
		func() (int, error) { cleanups++; pending = false; return 1, nil })
	if err != nil || cleanups != 1 {
		t.Fatalf("err=%v cleanup=%d", err, cleanups)
	}
}
func TestAdminProjectionCleanupStillFailsClosed(t *testing.T) {
	problem := errors.New("persistent orphan")
	if err := settleAdminProjectionCleanup(0, 0, func() error { return problem }, func() (int, error) { return 0, nil }); !errors.Is(err, problem) {
		t.Fatalf("verification suppressed: %v", err)
	}
	outage := errors.New("cleanup unavailable")
	if err := settleAdminProjectionCleanup(0, 0, func() error { return problem }, func() (int, error) { return 0, outage }); !errors.Is(err, outage) {
		t.Fatalf("cleanup outage suppressed: %v", err)
	}
}
