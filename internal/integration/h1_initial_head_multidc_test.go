//go:build integration

package integration

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	apipkg "github.com/Sesame-Disk/sesamefs/internal/api"
	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

// Real 3-DC evidence for ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01 (multi-DC
// reversion variant), driven by scripts/h1-initial-head-multidc-validation.sh
// on docker-compose.cassandra-3dc.yaml. Handler-level counterpart of
// scripts/pc0-initial-head-xdc-probe.sh: the legs below call the production
// initializers (FSHelper.InitializeLibraryFS and Sync createInitialCommit)
// through a *db.DB connected to a specific datacenter, exactly the way
// sync_w2_putblock_xdc_provenance_multidc_test.go drives its scope gate.
//
// Each initializer gets its OWN library and its OWN stop/restart cycle of
// dc-eu, and blindness is asserted immediately before the initializer runs.
// A shared library or a shared blind window cannot prove the second
// initializer started blind: the first Paxos round / read repair reconciles
// that library, and post-restart replay (batchlog, Paxos commit delivery)
// reconciles other partitions on its own schedule — observed on the real
// fixture, where a second library was already visible ~0.3 s after the first
// leg.
//
// Sequence per initializer (the script stops/starts nodes between legs):
//  1. seed a library row with a null HEAD, visible in every DC;
//  2. dc-eu stopped: dc-na initializes HEAD = C1 through the production path;
//  3. dc-eu restarted blind (hints off): assert dc-eu is blind for that
//     library, run the initializer under test from dc-eu, require it to keep
//     and return C1, leave exactly one commit row, and leave C1's commit
//     servable from dc-eu at the consistency GET /commit/:id uses.

const h1InitialHeadMultiDCEvidenceEnv = "SESAMEFS_REQUIRE_H1_INITIAL_HEAD_MULTIDC_EVIDENCE"

var h1InitialHeadMultiDCEvidence bool

type h1Session interface {
	Session() *gocql.Session
}

func h1Env(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("%s is required", name)
	}
	return value
}

func h1ReadHead(t *testing.T, database h1Session, orgID, repoID string, cl gocql.Consistency) string {
	t.Helper()
	var head string
	err := database.Session().Query(`SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, repoID).Consistency(cl).Scan(&head)
	if err != nil {
		t.Fatalf("read head at %s: %v", cl, err)
	}
	return head
}

// h1CommitServableLocally reads commits(repo, commit) the way
// SyncHandler.GetCommit does: session consistency, no override.
func h1CommitServableLocally(t *testing.T, database h1Session, repoID, commitID string) bool {
	t.Helper()
	var found string
	err := database.Session().Query(`SELECT commit_id FROM commits WHERE library_id = ? AND commit_id = ?`, repoID, commitID).Scan(&found)
	if errors.Is(err, gocql.ErrNotFound) {
		return false
	}
	if err != nil {
		t.Fatalf("read commit %s locally: %v", commitID, err)
	}
	return true
}

func h1SeedLibrary(t *testing.T, database h1Session, orgID, ownerID, name string) string {
	t.Helper()
	repoID := uuid.NewString()
	now := time.Now().UTC()
	if err := database.Session().Query(`
		INSERT INTO libraries (org_id, library_id, owner_id, name, size_bytes, file_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, orgID, repoID, ownerID, name, int64(0), int64(0), now, now).Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("seed library row %s: %v", name, err)
	}
	if err := database.Session().Query(`
		INSERT INTO libraries_by_id (library_id, org_id, owner_id, name)
		VALUES (?, ?, ?, ?)
	`, repoID, orgID, ownerID, name).Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("seed libraries_by_id row %s: %v", name, err)
	}
	return repoID
}

// TestH1InitialHeadSeedFor3DC writes one library row with a null HEAD (the
// state every creation path leaves before initialization) so it is visible
// in every datacenter, and prints the ids the later legs need. The script
// runs it once per initializer under test (H1_LEG=sync|v2).
func TestH1InitialHeadSeedFor3DC(t *testing.T) {
	if os.Getenv("H1_SEED") != "1" {
		t.Skip("H1_SEED is not set")
	}
	leg := h1Env(t, "H1_LEG")
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-na", endpoints)
	orgID, ownerID := uuid.NewString(), uuid.NewString()
	repoID := h1SeedLibrary(t, database, orgID, ownerID, "h1-initial-head-3dc-"+leg)
	t.Logf("H1_ORG=%s", orgID)
	t.Logf("H1_OWNER=%s", ownerID)
	t.Logf("H1_REPO=%s", repoID)
}

// TestH1InitialHeadPublishInNA3DC runs the production v2 initializer from
// dc-na while dc-eu is stopped, so dc-eu's replica never sees the resulting
// HEAD or commit row. Prints the winning head.
func TestH1InitialHeadPublishInNA3DC(t *testing.T) {
	if os.Getenv("H1_PUBLISH_NA") != "1" {
		t.Skip("H1_PUBLISH_NA is not set")
	}
	orgID, ownerID, repoID, leg := h1Env(t, "H1_ORG"), h1Env(t, "H1_OWNER"), h1Env(t, "H1_REPO"), h1Env(t, "H1_LEG")
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-na", endpoints)
	if err := v2pkg.NewFSHelper(database).InitializeLibraryFS(orgID, repoID, ownerID, "h1-initial-head-3dc-"+leg); err != nil {
		t.Fatalf("InitializeLibraryFS from dc-na: %v", err)
	}
	head := h1ReadHead(t, database, orgID, repoID, gocql.LocalQuorum)
	if head == "" {
		t.Fatal("dc-na did not publish an initial HEAD")
	}
	t.Logf("H1_C1=%s", head)
}

// TestH1InitialHeadBlindDCDoesNotRevert3DC is the evidence leg, run once per
// initializer (H1_LEG=sync|v2) in its own dc-eu stop/restart cycle. dc-eu has
// been restarted with hinted handoff disabled, so its local replica still
// holds a null HEAD and no commit row while the canonical HEAD is C1. dc-eu
// must be blind right before the initializer runs; the initializer must be
// rejected by the conditional publish, hand back C1, leave no dangling
// commit, and C1's commit must be servable from dc-eu at the consistency
// GET /commit/:id uses (a HEAD whose commit 404s locally is not recovery).
// Before the fix, the Sync path overwrote C1 here.
func TestH1InitialHeadBlindDCDoesNotRevert3DC(t *testing.T) {
	if os.Getenv("H1_BLIND_EU") != "1" {
		t.Skip("H1_BLIND_EU is not set")
	}
	orgID, ownerID, repoID, c1, leg := h1Env(t, "H1_ORG"), h1Env(t, "H1_OWNER"), h1Env(t, "H1_REPO"), h1Env(t, "H1_C1"), h1Env(t, "H1_LEG")
	endpoints := w2PostHead3DCEndpoints(t)
	dbEU := w2PostHead3DCConnect(t, "dc-eu", endpoints)
	dbNA := w2PostHead3DCConnect(t, "dc-na", endpoints)

	// Precondition, fail-closed: dc-eu must really be blind for THIS library,
	// for both the HEAD and the commit row, right before the initializer.
	if local := h1ReadHead(t, dbEU, orgID, repoID, gocql.LocalQuorum); local != "" {
		t.Fatalf("%s: dc-eu is not blind (LOCAL_QUORUM head=%q); the divergence precondition did not hold", leg, local)
	}
	if h1CommitServableLocally(t, dbEU, repoID, c1) {
		t.Fatalf("%s: commit %s is already visible in dc-eu before the initializer ran; the blind precondition did not hold", leg, c1)
	}

	switch leg {
	case "sync":
		// GET /commit/HEAD reads "" here and calls createInitialCommit.
		head, err := apipkg.CreateInitialCommitForIntegration(dbEU, repoID, orgID, ownerID)
		if err != nil {
			t.Fatalf("RED (sync): createInitialCommit from blind dc-eu: %v", err)
		}
		if head != c1 {
			t.Fatalf("RED (sync): createInitialCommit from blind dc-eu settled on %s, want the canonical HEAD %s", head, c1)
		}
	case "v2":
		if err := v2pkg.NewFSHelper(dbEU).InitializeLibraryFS(orgID, repoID, ownerID, "h1-initial-head-3dc-v2"); err != nil {
			t.Fatalf("RED (v2): InitializeLibraryFS from blind dc-eu: %v", err)
		}
	default:
		t.Fatalf("unknown H1_LEG %q", leg)
	}

	if serial := h1ReadHead(t, dbNA, orgID, repoID, gocql.Serial); serial != c1 {
		t.Fatalf("RED (%s): canonical HEAD is %s after the blind initializer ran, want %s (HEAD reverted)", leg, serial, c1)
	}
	if !h1CommitServableLocally(t, dbEU, repoID, c1) {
		t.Fatalf("RED (%s): adopted HEAD %s is not servable from dc-eu at session consistency; GET /commit/HEAD would return a HEAD that GET /commit/:id 404s", leg, c1)
	}
	var commits int
	if err := dbNA.Session().Query(`SELECT count(*) FROM commits WHERE library_id = ?`, repoID).Consistency(gocql.EachQuorum).Scan(&commits); err != nil {
		t.Fatalf("%s: count commits: %v", leg, err)
	}
	if commits != 1 {
		t.Fatalf("%s: commits rows = %d, want 1: the KNOWN_LOSER initializer must discard its own commit row", leg, commits)
	}
	h1InitialHeadMultiDCEvidence = true
	t.Logf("GREEN (%s): the production initializer, from a blind dc-eu on its own library, kept and returned the canonical HEAD %s and left it servable locally", leg, c1)
}
