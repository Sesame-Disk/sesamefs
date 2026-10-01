//go:build integration

package v2

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/config"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/google/uuid"
)

// Directed 3-DC legs for ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01. The old
// owner runs in dc-na and the new owner in dc-eu; both sessions default to
// LOCAL_SERIAL, so the stale generation is rejected only because the lifecycle
// LWTs pin global SERIAL. Seeding and assertions use an ALL-consistency session
// through dc-asia. Run by scripts/library-hard-delete-lease-nonfencing-multidc-validation.sh.

const (
	nonfencing3DCHostsEnv    = "W2_POST_HEAD_3DC_HOSTS"
	nonfencing3DCEvidenceEnv = "SESAMEFS_REQUIRE_LIBRARY_HARD_DELETE_NONFENCING_3DC_EVIDENCE"
)

func nonfencing3DCConnect(t *testing.T, dc, consistency, serial string) *dbpkg.DB {
	t.Helper()
	endpoints := map[string]string{}
	for _, entry := range strings.Split(os.Getenv(nonfencing3DCHostsEnv), ",") {
		if name, host, ok := strings.Cut(strings.TrimSpace(entry), "="); ok {
			endpoints[strings.TrimSpace(name)] = strings.TrimSpace(host)
		}
	}
	if endpoints[dc] == "" {
		t.Fatalf("%s is missing %s", nonfencing3DCHostsEnv, dc)
	}
	database, err := dbpkg.New(config.DatabaseConfig{
		Hosts:             []string{endpoints[dc]},
		Keyspace:          restoreGuardEnv("CASSANDRA_KEYSPACE", "sesamefs"),
		Consistency:       consistency,
		SerialConsistency: serial,
		LocalDC:           dc,
		ReplicationClass:  "NetworkTopologyStrategy",
		ReplicationDCs:    map[string]int{"dc-na": 1, "dc-eu": 1, "dc-asia": 1},
		Username:          os.Getenv("CASSANDRA_USERNAME"),
		Password:          os.Getenv("CASSANDRA_PASSWORD"),
	})
	if err != nil {
		t.Fatalf("connect to %s: %v", dc, err)
	}
	t.Cleanup(database.Close)
	return database
}

func nonfencing3DCReady(t *testing.T) (na, eu, observer *dbpkg.DB) {
	t.Helper()
	if strings.TrimSpace(os.Getenv(nonfencing3DCHostsEnv)) == "" {
		if os.Getenv(nonfencing3DCEvidenceEnv) == "1" {
			t.Fatalf("%s=1 requires %s", nonfencing3DCEvidenceEnv, nonfencing3DCHostsEnv)
		}
		t.Skip(nonfencing3DCHostsEnv + " is not set")
	}
	na, eu, observer = nonfencing3DCConnect(t, "dc-na", "LOCAL_QUORUM", "LOCAL_SERIAL"),
		nonfencing3DCConnect(t, "dc-eu", "LOCAL_QUORUM", "LOCAL_SERIAL"),
		nonfencing3DCConnect(t, "dc-asia", "ALL", "SERIAL")
	nonfencing3DCWarmUp(t, observer)
	return na, eu, observer
}

// nonfencing3DCWarmUp waits until an ALL-consistency write to the lifecycle
// tables succeeds: right after the migrations a replica can still time out, and
// the legs seed at ALL so that every DC starts from the same state.
func nonfencing3DCWarmUp(t *testing.T, observer *dbpkg.DB) {
	t.Helper()
	probeOrg, probeLib := uuid.NewString(), uuid.NewString()
	deadline := time.Now().Add(90 * time.Second)
	for {
		err := observer.Session().Query(`INSERT INTO libraries (org_id, library_id, name, created_at) VALUES (?, ?, ?, ?)`,
			probeOrg, probeLib, "nonfencing-3dc-warmup", time.Now().UTC()).Exec()
		if err == nil {
			err = observer.Session().Query(`INSERT INTO deleted_libraries (library_id, org_id, deleted_at) VALUES (?, ?, ?)`,
				probeLib, probeOrg, time.Now().UTC()).Exec()
		}
		if err == nil {
			_ = observer.Session().Query(`DELETE FROM deleted_libraries WHERE library_id = ?`, probeLib).Exec()
			_ = observer.Session().Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ?`, probeOrg, probeLib).Exec()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("3-DC fixture did not accept ALL-consistency writes: %v", err)
		}
		time.Sleep(2 * time.Second)
	}
}

// Race A across DCs: a permanent delete in dc-na renews, pauses, loses the
// lease to a restore in dc-eu, and resumes. The restored library survives.
func TestNonfencing3DCStaleDeleteAfterRestore(t *testing.T) {
	na, eu, observer := nonfencing3DCReady(t)
	lib := nonfencingSeedTrashedLibrary(t, observer)
	pause := pauseDeleteAfterRenew(t, lib)

	oldOwner := runOwner(func() error { return nonfencingPermanentDelete(na, lib) })
	tokenA := pause.wait(t)
	nonfencingAgeLease(t, observer, lib, tokenA)
	if err := nonfencingRestore(eu, lib); err != nil {
		t.Fatalf("dc-eu restore after stale takeover: %v", err)
	}

	pause.resumeOwner()
	errA := awaitOwner(t, oldOwner)

	if present, deletedAt := nonfencingCanonical(t, observer, lib); !present || !deletedAt.IsZero() {
		t.Fatalf("NONFENCING 3DC RED: stale dc-na delete (err=%v) changed the library restored in dc-eu: present=%v deleted_at=%v", errA, present, deletedAt)
	}
	if !errors.Is(errA, errPermanentDeleteCandidateStale) {
		t.Fatalf("stale dc-na delete returned %v, want errPermanentDeleteCandidateStale", errA)
	}
	if marker := nonfencingReadMarker(t, observer, lib); marker.Present {
		t.Fatalf("stale dc-na delete re-created a purge marker for the restored library: %+v", marker)
	}
}

// Race B across DCs: a restore in dc-na renews, pauses, loses the lease to a
// permanent delete in dc-eu, and resumes. The library stays deleted and keeps
// its purge marker.
func TestNonfencing3DCStaleRestoreAfterDelete(t *testing.T) {
	na, eu, observer := nonfencing3DCReady(t)
	lib := nonfencingSeedTrashedLibrary(t, observer)
	pause := pauseRestoreAfterRenew(t, lib)

	oldOwner := runOwner(func() error { return nonfencingRestore(na, lib) })
	tokenA := pause.wait(t)
	nonfencingAgeLease(t, observer, lib, tokenA)
	if err := nonfencingPermanentDelete(eu, lib); err != nil {
		t.Fatalf("dc-eu permanent delete after stale takeover: %v", err)
	}

	pause.resumeOwner()
	errA := awaitOwner(t, oldOwner)

	if present, deletedAt := nonfencingCanonical(t, observer, lib); present {
		t.Fatalf("NONFENCING 3DC RED: stale dc-na restore (err=%v) resurrected the library deleted in dc-eu: deleted_at=%v", errA, deletedAt)
	}
	marker := nonfencingReadMarker(t, observer, lib)
	if !marker.Present || !marker.DeletedAt.Equal(lib.DeletedAt) || marker.PurgeRequestedAt.IsZero() {
		t.Fatalf("NONFENCING 3DC RED: stale dc-na restore (err=%v) dropped the permanent-delete marker: %+v", errA, marker)
	}
	if errA == nil {
		t.Fatal("stale dc-na restore reported success")
	}
}

// R3/R4: a lifecycle transition commits in dc-na and its completion fails;
// recovery runs from dc-eu. While dc-na is unreachable (the harness pauses its
// node between phases), recovery must fail rather than report success, since
// derived rows written only in dc-na cannot be observed; once dc-na is back the
// same recovery converges. Phases share state through a file.
const nonfencing3DCPhaseEnv = "NONFENCING_3DC_PHASE"

type nonfencing3DCRecoveryState struct {
	SoftDeleted, Restored, Purged nonfencingLibrary
}

const nonfencing3DCStateFile = "/tmp/nonfencing-3dc-recovery.json"

func TestNonfencing3DCRecoveryFailsClosed(t *testing.T) {
	phase := os.Getenv(nonfencing3DCPhaseEnv)
	if phase == "" {
		t.Skip(nonfencing3DCPhaseEnv + " is not set (run by the 3-DC harness)")
	}
	eu := nonfencing3DCConnect(t, "dc-eu", "LOCAL_QUORUM", "LOCAL_SERIAL")
	switch phase {
	case "prepare":
		na := nonfencing3DCConnect(t, "dc-na", "LOCAL_QUORUM", "LOCAL_SERIAL")
		nonfencingKeepSeeds = true // later phases use the rows
		observer := nonfencing3DCConnect(t, "dc-asia", "ALL", "SERIAL")
		nonfencing3DCWarmUp(t, observer)
		var state nonfencing3DCRecoveryState
		state.SoftDeleted = nonfencingSeedActiveLibrary(t, observer)
		state.Restored = nonfencingSeedTrashedLibrary(t, observer)
		state.Purged = nonfencingSeedTrashedLibrary(t, observer)
		restore := failLibraryLifecycleCompletions(t)
		_ = softDeleteLibrary(na, state.SoftDeleted.OrgID, state.SoftDeleted.OwnerID, state.SoftDeleted.OwnerID, state.SoftDeleted.LibraryID)
		_ = nonfencingRestore(na, state.Restored)
		_ = nonfencingPermanentDelete(na, state.Purged)
		restore()
		_, state.SoftDeleted.DeletedAt = nonfencingCanonical(t, observer, state.SoftDeleted)
		raw, _ := json.Marshal(state)
		if err := os.WriteFile(nonfencing3DCStateFile, raw, 0o600); err != nil {
			t.Fatalf("save state: %v", err)
		}
	case "na-down":
		state := nonfencing3DCLoadState(t)
		if err := repairLibraryLifecycleDerivedState(eu, state.SoftDeleted.OrgID, state.SoftDeleted.LibraryID); err == nil {
			t.Fatal("NONFENCING 3DC RED: soft-delete repair from dc-eu reported success while dc-na is unreachable")
		}
		if err := repairLibraryLifecycleDerivedState(eu, state.Restored.OrgID, state.Restored.LibraryID); err == nil {
			t.Fatal("NONFENCING 3DC RED: restore repair from dc-eu reported success while dc-na is unreachable")
		}
		if _, _, resumed, err := resumeCommittedPermanentDelete(eu, state.Purged.OrgID, state.Purged.LibraryID); err == nil {
			t.Fatalf("NONFENCING 3DC RED: permanent-delete resume from dc-eu reported resumed=%v without error while dc-na is unreachable", resumed)
		}
		if err := RecoverPendingLibraryLifecycles(context.Background(), eu); err == nil {
			t.Fatal("NONFENCING 3DC RED: lifecycle reaper in dc-eu reported success while dc-na is unreachable")
		}
		// Bulk discovery reads the continuations at global QUORUM (reachable without
		// dc-na) and must then fail closed on the strong resume, never report the
		// committed delete as nothing to do.
		if _, failed := resumeCommittedPermanentDeletes(eu, []string{state.Purged.OrgID}); failed == 0 {
			t.Fatal("NONFENCING 3DC RED: bulk permanent-delete discovery from dc-eu reported nothing to do while dc-na is unreachable")
		}
	case "after":
		state := nonfencing3DCLoadState(t)
		if err := RecoverPendingLibraryLifecycles(context.Background(), eu); err != nil {
			t.Fatalf("lifecycle reaper from dc-eu after dc-na returned: %v", err)
		}
		observer := nonfencing3DCConnect(t, "dc-asia", "ALL", "SERIAL")
		assertNonfencingTrashedDerivedState(t, observer, state.SoftDeleted, state.SoftDeleted.DeletedAt, "soft delete recovered from dc-eu")
		if marker := nonfencingReadMarker(t, observer, state.Restored); marker.Present {
			t.Fatalf("restore recovered from dc-eu left its marker: %+v", marker)
		}
		marker := nonfencingReadMarker(t, observer, state.Purged)
		if !marker.Present || marker.PurgeRequestedAt.IsZero() {
			t.Fatalf("permanent delete recovered from dc-eu: marker %+v, want a purge request", marker)
		}
		var orgID string
		if err := observer.Session().Query(`SELECT org_id FROM libraries_by_id WHERE library_id = ?`, state.Purged.LibraryID).Scan(&orgID); err == nil {
			t.Fatal("permanent delete recovered from dc-eu left libraries_by_id")
		}
	default:
		t.Fatalf("unknown phase %q", phase)
	}
}

func nonfencing3DCLoadState(t *testing.T) nonfencing3DCRecoveryState {
	t.Helper()
	raw, err := os.ReadFile(nonfencing3DCStateFile)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	var state nonfencing3DCRecoveryState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	return state
}
