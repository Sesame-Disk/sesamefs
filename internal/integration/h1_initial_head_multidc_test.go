//go:build integration

package integration

import (
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
// Sequence (the script stops/starts nodes between legs):
//  1. seed a library row with a null HEAD, visible in every DC;
//  2. dc-eu stopped: dc-na initializes HEAD = C1 through the production path;
//  3. dc-eu restarted blind (hints off): both production initializers, driven
//     from dc-eu, must keep C1 and hand it back — never overwrite it.

const h1InitialHeadMultiDCEvidenceEnv = "SESAMEFS_REQUIRE_H1_INITIAL_HEAD_MULTIDC_EVIDENCE"

var h1InitialHeadMultiDCEvidence bool

func h1IDs(t *testing.T) (orgID, repoID string) {
	t.Helper()
	orgID, repoID = strings.TrimSpace(os.Getenv("H1_ORG")), strings.TrimSpace(os.Getenv("H1_REPO"))
	if orgID == "" || repoID == "" {
		t.Fatal("H1_ORG and H1_REPO are required")
	}
	return orgID, repoID
}

func h1ReadHead(t *testing.T, database interface {
	Session() *gocql.Session
}, orgID, repoID string, cl gocql.Consistency) string {
	t.Helper()
	var head string
	err := database.Session().Query(`SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, repoID).Consistency(cl).Scan(&head)
	if err != nil {
		t.Fatalf("read head at %s: %v", cl, err)
	}
	return head
}

// TestH1InitialHeadSeedFor3DC writes a library row with a null HEAD (the
// state every creation path leaves before initialization) so it is visible in
// every datacenter, and prints the ids the later legs need.
func TestH1InitialHeadSeedFor3DC(t *testing.T) {
	if os.Getenv("H1_SEED") != "1" {
		t.Skip("H1_SEED is not set")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-na", endpoints)
	orgID, repoID, ownerID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	now := time.Now().UTC()
	if err := database.Session().Query(`
		INSERT INTO libraries (org_id, library_id, owner_id, name, size_bytes, file_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, orgID, repoID, ownerID, "h1-initial-head-3dc", int64(0), int64(0), now, now).Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("seed library row: %v", err)
	}
	if err := database.Session().Query(`
		INSERT INTO libraries_by_id (library_id, org_id, owner_id, name)
		VALUES (?, ?, ?, ?)
	`, repoID, orgID, ownerID, "h1-initial-head-3dc").Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("seed libraries_by_id row: %v", err)
	}
	t.Logf("H1_ORG=%s", orgID)
	t.Logf("H1_REPO=%s", repoID)
	t.Logf("H1_OWNER=%s", ownerID)
}

// TestH1InitialHeadPublishInNA3DC runs the production v2 initializer from
// dc-na while dc-eu is stopped, so dc-eu's replica never sees the resulting
// HEAD. Prints the winning head.
func TestH1InitialHeadPublishInNA3DC(t *testing.T) {
	if os.Getenv("H1_PUBLISH_NA") != "1" {
		t.Skip("H1_PUBLISH_NA is not set")
	}
	orgID, repoID := h1IDs(t)
	ownerID := strings.TrimSpace(os.Getenv("H1_OWNER"))
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-na", endpoints)
	if err := v2pkg.NewFSHelper(database).InitializeLibraryFS(orgID, repoID, ownerID, "h1-initial-head-3dc"); err != nil {
		t.Fatalf("InitializeLibraryFS from dc-na: %v", err)
	}
	head := h1ReadHead(t, database, orgID, repoID, gocql.LocalQuorum)
	if head == "" {
		t.Fatal("dc-na did not publish an initial HEAD")
	}
	t.Logf("H1_C1=%s", head)
}

// TestH1InitialHeadBlindDCDoesNotRevert3DC is the evidence leg. dc-eu has
// been restarted with hinted handoff disabled, so its local replica still
// holds a null HEAD while the canonical HEAD is C1. Both production
// initializers, driven from dc-eu, must be rejected by the conditional
// publish, hand back C1, and leave no dangling commit. Before the fix, the
// Sync path overwrote C1 with a fresh empty initial commit here.
func TestH1InitialHeadBlindDCDoesNotRevert3DC(t *testing.T) {
	if os.Getenv("H1_BLIND_EU") != "1" {
		t.Skip("H1_BLIND_EU is not set")
	}
	orgID, repoID := h1IDs(t)
	ownerID := strings.TrimSpace(os.Getenv("H1_OWNER"))
	c1 := strings.TrimSpace(os.Getenv("H1_C1"))
	if c1 == "" {
		t.Fatal("H1_C1 is required")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	dbEU := w2PostHead3DCConnect(t, "dc-eu", endpoints)
	dbNA := w2PostHead3DCConnect(t, "dc-na", endpoints)

	// Precondition, fail-closed: dc-eu must really be blind. If its replica
	// already converged this leg cannot prove anything.
	if local := h1ReadHead(t, dbEU, orgID, repoID, gocql.LocalQuorum); local != "" {
		t.Fatalf("dc-eu is not blind (LOCAL_QUORUM head=%q); the divergence precondition did not hold", local)
	}

	// Sync path: GET /commit/HEAD would read "" here and call createInitialCommit.
	syncHead, err := apipkg.CreateInitialCommitForIntegration(dbEU, repoID, orgID, ownerID)
	if err != nil {
		t.Fatalf("createInitialCommit from blind dc-eu: %v", err)
	}
	if syncHead != c1 {
		t.Fatalf("RED: createInitialCommit from blind dc-eu settled on %s, want the canonical HEAD %s", syncHead, c1)
	}

	// v2 path: InitializeLibraryFS from the blind datacenter.
	if err := v2pkg.NewFSHelper(dbEU).InitializeLibraryFS(orgID, repoID, ownerID, "h1-initial-head-3dc"); err != nil {
		t.Fatalf("InitializeLibraryFS from blind dc-eu: %v", err)
	}

	// Canonical truth is the Paxos domain, not any single replica.
	if serial := h1ReadHead(t, dbNA, orgID, repoID, gocql.Serial); serial != c1 {
		t.Fatalf("RED: canonical HEAD is %s after blind initializers ran, want %s (HEAD reverted)", serial, c1)
	}
	var commits int
	if err := dbNA.Session().Query(`SELECT count(*) FROM commits WHERE library_id = ?`, repoID).Consistency(gocql.EachQuorum).Scan(&commits); err != nil {
		t.Fatalf("count commits: %v", err)
	}
	if commits != 1 {
		t.Fatalf("commits rows = %d, want 1: losing initializers must discard their commit rows", commits)
	}
	h1InitialHeadMultiDCEvidence = true
	t.Logf("GREEN: both production initializers driven from blind dc-eu kept and returned the canonical HEAD %s", c1)
}
