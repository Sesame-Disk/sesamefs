//go:build integration

package v2

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Sesame-Disk/sesamefs/internal/config"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
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
	return nonfencing3DCConnect(t, "dc-na", "LOCAL_QUORUM", "LOCAL_SERIAL"),
		nonfencing3DCConnect(t, "dc-eu", "LOCAL_QUORUM", "LOCAL_SERIAL"),
		nonfencing3DCConnect(t, "dc-asia", "ALL", "SERIAL")
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
