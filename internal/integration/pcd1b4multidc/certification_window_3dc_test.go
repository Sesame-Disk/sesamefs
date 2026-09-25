//go:build integration

// Package pcd1b4multidc holds the isolated three-datacenter characterization
// for PC-D1B.4 (docs/PC-D1B-CERTIFICATION-WINDOW-FENCE.md, race R12). It lives
// outside internal/integration because that package's TestMain requires a
// healthy SesameFS backend, and the degrade phase deliberately stops the
// datacenters a backend would use. This package needs only Cassandra.
//
// Run it only through scripts/pc-d1b4-certification-window-multidc-characterization.sh,
// which owns an isolated 3-DC fixture and drives the phases in order. Without a
// phase the test skips, so the standard integration profile is unaffected.
package pcd1b4multidc

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/config"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

const (
	phaseEnv = "SESAMEFS_PCD1B4_3DC_PHASE"
	runIDEnv = "SESAMEFS_PCD1B4_3DC_RUN_ID"
	hostsEnv = "SESAMEFS_PCD1B4_3DC_HOSTS"
)

type fixture struct {
	orgID, libraryID, ownerID, head string
	rootFSID, fileFSID, entries     string
}

func testFSID(label string) string {
	sum := sha1.Sum([]byte(label))
	return hex.EncodeToString(sum[:])
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	runID := strings.TrimSpace(os.Getenv(runIDEnv))
	namespace, err := uuid.Parse(runID)
	if err != nil {
		t.Fatalf("%s must be a UUID: %v", runIDEnv, err)
	}
	stable := func(name string) string {
		return uuid.NewSHA1(namespace, []byte("pc-d1b4-3dc-"+name)).String()
	}
	f := fixture{
		orgID:     stable("org"),
		libraryID: stable("library"),
		ownerID:   stable("owner"),
		head:      "pc-d1b4-3dc-head-" + strings.ReplaceAll(runID, "-", ""),
		rootFSID:  testFSID("pc-d1b4-3dc-root-" + runID),
		fileFSID:  testFSID("pc-d1b4-3dc-file-" + runID),
	}
	entries, err := json.Marshal([]map[string]interface{}{{"id": f.fileFSID, "mode": 33188, "mtime": int64(1_700_000_000), "name": "covered.txt"}})
	if err != nil {
		t.Fatalf("marshal root entries: %v", err)
	}
	f.entries = string(entries)
	return f
}

func endpoints(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, entry := range strings.Split(os.Getenv(hostsEnv), ",") {
		dc, host, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if ok && dc != "" && host != "" {
			out[dc] = host
		}
	}
	for _, dc := range []string{"dc-na", "dc-eu", "dc-asia"} {
		if out[dc] == "" {
			t.Fatalf("%s must name dc-na, dc-eu and dc-asia", hostsEnv)
		}
	}
	return out
}

// connect opens a session whose coordinator and LOCAL_* levels are in dc. The
// session inherits LOCAL_SERIAL on purpose: every PC-D1A/B primitive pins
// global SERIAL itself, and this fixture proves that pin is what matters.
func connect(t *testing.T, dc string) *dbpkg.DB {
	t.Helper()
	database, err := dbpkg.New(config.DatabaseConfig{
		Hosts:             []string{endpoints(t)[dc]},
		Keyspace:          "sesamefs",
		Consistency:       "LOCAL_QUORUM",
		SerialConsistency: "LOCAL_SERIAL",
		LocalDC:           dc,
		ReplicationClass:  "NetworkTopologyStrategy",
		ReplicationDCs:    map[string]int{"dc-na": 1, "dc-eu": 1, "dc-asia": 1},
	})
	if err != nil {
		t.Fatalf("connect to %s: %v", dc, err)
	}
	t.Cleanup(database.Close)
	return database
}

func retry(t *testing.T, what string, op func() error) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		err := op()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %v", what, err)
		}
		time.Sleep(2 * time.Second)
	}
}

func seed(t *testing.T, database *dbpkg.DB, f fixture) {
	t.Helper()
	ctx := context.Background()
	session := database.Session()
	now := time.Now().UTC().Truncate(time.Millisecond)
	retry(t, "seed library", func() error {
		return session.Query(`
			INSERT INTO libraries (org_id, library_id, owner_id, name, encrypted, block_representation_id, storage_class, size_bytes, file_count, head_commit_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, f.orgID, f.libraryID, f.ownerID, "pc-d1b4-3dc", false, dbpkg.PlainBlockRepresentationID, "evidence", int64(0), int64(1), f.head, now, now).Consistency(gocql.EachQuorum).Exec()
	})
	creator, description := uuid.NewString(), "pc-d1b4 3dc"
	retry(t, "seed commit", func() error {
		return session.Query(`
			INSERT INTO commits (library_id, commit_id, parent_id, root_fs_id, creator_id, description, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, f.libraryID, f.head, "", f.rootFSID, creator, description, now).Consistency(gocql.EachQuorum).Exec()
	})
	if _, err := dbpkg.AuthorizeCommitProjection(ctx, session, dbpkg.CommitProjection{
		LibraryID: f.libraryID, CommitID: f.head, RootFSID: f.rootFSID, CreatorID: creator, Description: description, CreatedAt: now,
	}); err != nil {
		t.Fatalf("claim commit identity: %v", err)
	}
	retry(t, "seed root", func() error {
		return session.Query(`INSERT INTO fs_objects (library_id, fs_id, obj_type, dir_entries, mtime) VALUES (?, ?, ?, ?, ?)`,
			f.libraryID, f.rootFSID, "dir", f.entries, now.Unix()).Consistency(gocql.EachQuorum).Exec()
	})
	if _, err := dbpkg.AuthorizeFSObjectProjection(ctx, session, dbpkg.FSObjectProjection{
		LibraryID: f.libraryID, FSID: f.rootFSID, ObjectType: "dir", DirectoryEntries: f.entries,
	}); err != nil {
		t.Fatalf("claim root identity: %v", err)
	}
	retry(t, "seed zero-block file", func() error {
		return session.Query(`INSERT INTO fs_objects (library_id, fs_id, obj_type, size_bytes, mtime) VALUES (?, ?, ?, ?, ?)`,
			f.libraryID, f.fileFSID, "file", int64(0), now.Unix()).Consistency(gocql.EachQuorum).Exec()
	})
	if _, err := dbpkg.AuthorizeFSObjectProjection(ctx, session, dbpkg.FSObjectProjection{
		LibraryID: f.libraryID, FSID: f.fileFSID, ObjectType: "file", SizeBytes: 0,
		FileLayout: dbpkg.FileStorageSHA1Only, LogicalSHA1IDs: []string{},
	}); err != nil {
		t.Fatalf("claim zero-block file identity: %v", err)
	}
}

type rowView struct {
	head, certified string
	deleted         bool
}

func readRow(t *testing.T, database *dbpkg.DB, f fixture, consistency gocql.Consistency) rowView {
	t.Helper()
	var head, certified *string
	var deletedAt time.Time
	if err := database.Session().Query(`
		SELECT head_commit_id, continuity_certified_head_commit_id, deleted_at
		FROM libraries WHERE org_id = ? AND library_id = ?
	`, f.orgID, f.libraryID).Consistency(consistency).Scan(&head, &certified, &deletedAt); err != nil {
		t.Fatalf("read library row at %s: %v", consistency, err)
	}
	view := rowView{deleted: !deletedAt.IsZero()}
	if head != nil {
		view.head = *head
	}
	if certified != nil {
		view.certified = *certified
	}
	return view
}

func (v rowView) validFor(head string) bool {
	return !v.deleted && v.head == head && v.certified == head
}

func fileVisible(t *testing.T, database *dbpkg.DB, f fixture) bool {
	t.Helper()
	_, err := dbpkg.ReadFSObjectIdentitySourceRow(context.Background(), database.Session(), f.libraryID, f.fileFSID)
	if errors.Is(err, gocql.ErrNotFound) {
		return false
	}
	if err != nil {
		t.Fatalf("read covered fs_object: %v", err)
	}
	return true
}

func pcd1b4CanonicalAbsenceProofConsistency() gocql.Consistency {
	return gocql.EachQuorum
}

// TestPCD1B4CertificationWindow3DC characterizes R12 on main@62a2c0e0:
//
//	prepare  all DCs up: a certifiable library (commit + root + zero-block file)
//	degrade  only dc-eu up: the production soft-delete is acknowledged in dc-eu;
//	         a blind gateway delete of a covered identity cannot proceed
//	certify  dc-eu down: dc-na certifies H under global SERIAL and the witness
//	         settles although the soft-delete was already acknowledged; a dc-asia
//	         LOCAL_QUORUM reader sees a valid witness on a live library
//	merge    all DCs up: the merged row is deleted (witness invalid while
//	         deleted); restore revives the same witness
func TestPCD1B4CertificationWindow3DC(t *testing.T) {
	phase := strings.TrimSpace(os.Getenv(phaseEnv))
	if phase == "" {
		t.Skipf("%s is not set; run scripts/pc-d1b4-certification-window-multidc-characterization.sh", phaseEnv)
	}
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	switch phase {
	case "prepare":
		na := connect(t, "dc-na")
		seed(t, na, f)
		if view := readRow(t, na, f, gocql.EachQuorum); view.head != f.head || view.certified != "" || view.deleted {
			t.Fatalf("prepare: unexpected row %+v", view)
		}

	case "degrade":
		eu := connect(t, "dc-eu")
		store := gcpkg.NewCassandraStore(eu)
		if err := store.SoftDeleteLibrary(uuid.MustParse(f.orgID), uuid.MustParse(f.libraryID), uuid.MustParse(f.ownerID)); err != nil {
			t.Fatalf("degrade: production soft-delete acknowledged in dc-eu: %v", err)
		}
		if view := readRow(t, eu, f, gocql.LocalQuorum); !view.deleted {
			t.Fatalf("degrade: dc-eu must acknowledge the soft-delete, got %+v", view)
		}
		// A destroyer isolated in one DC cannot verify authority: the gateway
		// delete needs a global SERIAL claim read before it writes anything.
		err := dbpkg.DeleteFSObjectIdentity(eu.Session(), f.libraryID, f.fileFSID)
		if !errors.Is(err, dbpkg.IdentityAuthorityUnavailable) {
			t.Fatalf("degrade: blind-DC gateway delete = %v, want IdentityAuthorityUnavailable", err)
		}
		if !fileVisible(t, eu, f) {
			t.Fatal("degrade: a refused gateway delete must not remove the covered fs_object")
		}

	case "certify":
		na := connect(t, "dc-na")
		if view := readRow(t, na, f, gocql.LocalQuorum); view.deleted {
			t.Fatalf("certify: dc-na must not have seen the dc-eu soft-delete (hinted handoff disabled), got %+v", view)
		}
		result := na.CertifyLibraryBaseline(ctx, storage.NewManager(), f.orgID, f.libraryID, f.head)
		if result.Outcome != dbpkg.LibraryBaselineCertificationCertified || result.Reason != dbpkg.LibraryBaselineReasonApplied {
			t.Fatalf("certify: CURRENT behavior is CERTIFIED/witness_applied after an acknowledged remote soft-delete, got %s/%s (%v)", result.Outcome, result.Reason, result.Diagnostic)
		}
		if view := readRow(t, na, f, gocql.Serial); !view.validFor(f.head) {
			t.Fatalf("certify: the global SERIAL view (dc-na+dc-asia) must show a valid witness, got %+v", view)
		}
		asia := connect(t, "dc-asia")
		if view := readRow(t, asia, f, gocql.LocalQuorum); !view.validFor(f.head) {
			t.Fatalf("certify: the dc-asia LOCAL_QUORUM reader must see a valid witness on a live library, got %+v", view)
		}
		if !fileVisible(t, na, f) {
			t.Fatal("certify: the certified tree is intact")
		}

	case "merge":
		na := connect(t, "dc-na")
		merged := readRow(t, na, f, gocql.All)
		if !merged.deleted || merged.certified != f.head || merged.head != f.head {
			t.Fatalf("merge: the converged row must be deleted and still carry witness H, got %+v", merged)
		}
		if view := readRow(t, na, f, gocql.Serial); view.validFor(f.head) {
			t.Fatalf("merge: validity must be false while deleted, got %+v", view)
		}
		batch := na.Session().Batch(gocql.LoggedBatch)
		batch.Query(`UPDATE libraries SET updated_at = ? WHERE org_id = ? AND library_id = ?`, time.Now().UTC(), f.orgID, f.libraryID)
		batch.Query(`DELETE deleted_at, deleted_by FROM libraries WHERE org_id = ? AND library_id = ?`, f.orgID, f.libraryID)
		batch.Query(`DELETE FROM deleted_libraries WHERE library_id = ?`, f.libraryID)
		batch.Consistency(gocql.EachQuorum)
		retry(t, "replay restore canonical statements", func() error { return batch.ExecContext(ctx) })
		if view := readRow(t, na, f, gocql.Serial); !view.validFor(f.head) {
			t.Fatalf("merge: CURRENT behavior is that restore revives the witness born after the soft-delete, got %+v", view)
		}
		if !fileVisible(t, na, f) {
			t.Fatal("merge: nothing destroyed the tree in this leg; the revived witness is true only by accident (see R10)")
		}

	case "rprepare", "rdegrade", "rverify", "m27-prepare", "m27-unknown", "m27-retry", "m27-verify", "a31-seed", "a31-verify":
		t.Skip("reaffirmation phases belong to TestPCD1B4ReaffirmationConsistency3DC")
	default:
		t.Fatalf("%s=%q, want prepare, degrade, certify or merge", phaseEnv, phase)
	}
}

// TestPCD1B4ReaffirmationConsistency3DC freezes, on real Cassandra, the
// consistency the PC-D1B.4 reaffirmation must use (ADR §10.3, CW-M23). Two
// identical covered rows start at T0 in every DC. With only dc-na up, row
// "local" is reaffirmed at T0+20 with LOCAL_QUORUM (acknowledged) and row
// "each" with EACH_QUORUM (refused: fail closed, no witness). With all DCs up,
// "each" is reaffirmed with EACH_QUORUM, then a superseded generation's
// tombstone at T0+10 is delivered with the destroyers' EACH_QUORUM. The
// LOCAL_QUORUM-reaffirmed row survives only in dc-na; the EACH_QUORUM one
// survives everywhere.
//
//	rprepare  all DCs up
//	rdegrade  only dc-na up
//	rverify   all DCs up
func TestPCD1B4ReaffirmationConsistency3DC(t *testing.T) {
	phase := strings.TrimSpace(os.Getenv(phaseEnv))
	if phase == "" {
		t.Skipf("%s is not set; run scripts/pc-d1b4-certification-window-multidc-characterization.sh", phaseEnv)
	}
	f := newFixture(t)
	libraryID := uuid.NewSHA1(uuid.MustParse(f.libraryID), []byte("reaffirmation")).String()
	localRow, eachRow := testFSID(libraryID+"-local"), testFSID(libraryID+"-each")
	// Explicit timestamps, as the frozen protocol uses: the fixture is
	// isolated and discarded, so a fixed base is enough.
	const base = int64(1_700_000_000_000_000)
	t0, stale, reaffirmTs := base, base+10, base+20
	write := func(database *dbpkg.DB, fsID string, ts int64, consistency gocql.Consistency) error {
		return database.Session().Query(`
			INSERT INTO fs_objects (library_id, fs_id, obj_type, size_bytes, mtime)
			VALUES (?, ?, 'file', 0, 1700000000) USING TIMESTAMP ?
		`, libraryID, fsID, ts).Consistency(consistency).Exec()
	}
	present := func(database *dbpkg.DB, fsID string) bool {
		var objType *string
		err := database.Session().Query(`SELECT obj_type FROM fs_objects WHERE library_id = ? AND fs_id = ?`, libraryID, fsID).
			Consistency(gocql.LocalQuorum).Scan(&objType)
		if errors.Is(err, gocql.ErrNotFound) {
			return false
		}
		if err != nil {
			t.Fatalf("LOCAL_QUORUM read of %s: %v", fsID, err)
		}
		return objType != nil
	}

	switch phase {
	case "rprepare":
		na := connect(t, "dc-na")
		for _, row := range []string{localRow, eachRow} {
			row := row
			retry(t, "seed covered row at T0", func() error { return write(na, row, t0, gocql.EachQuorum) })
		}
	case "rdegrade":
		na := connect(t, "dc-na")
		if err := write(na, localRow, reaffirmTs, gocql.LocalQuorum); err != nil {
			t.Fatalf("rdegrade: LOCAL_QUORUM reaffirmation must be acknowledged by dc-na alone: %v", err)
		}
		if err := write(na, eachRow, reaffirmTs, gocql.EachQuorum); err == nil {
			t.Fatal("rdegrade: EACH_QUORUM reaffirmation must fail while dc-eu and dc-asia are down; the certifier must then fail closed")
		}
	case "rverify":
		na := connect(t, "dc-na")
		retry(t, "EACH_QUORUM reaffirmation with all DCs up", func() error { return write(na, eachRow, reaffirmTs, gocql.EachQuorum) })
		for _, row := range []string{localRow, eachRow} {
			row := row
			retry(t, "superseded generation's stale tombstone", func() error {
				return na.Session().Query(`DELETE FROM fs_objects USING TIMESTAMP ? WHERE library_id = ? AND fs_id = ?`, stale, libraryID, row).
					Consistency(gocql.EachQuorum).Exec()
			})
		}
		for _, dc := range []string{"dc-na", "dc-eu", "dc-asia"} {
			database := connect(t, dc)
			if !present(database, eachRow) {
				t.Fatalf("rverify: the EACH_QUORUM-reaffirmed row must survive the stale tombstone in %s", dc)
			}
			wantLocal := dc == "dc-na"
			if got := present(database, localRow); got != wantLocal {
				t.Fatalf("rverify: LOCAL_QUORUM-reaffirmed row visible in %s = %v, want %v (CW-M23: it survives only where it was reaffirmed)", dc, got, wantLocal)
			}
		}
	case "prepare", "degrade", "certify", "merge", "a31-seed", "a31-verify", "m27-prepare", "m27-unknown", "m27-retry", "m27-verify":
		t.Skip("certification-window phases belong to TestPCD1B4CertificationWindow3DC")
	default:
		t.Fatalf("%s=%q is not a reaffirmation phase", phaseEnv, phase)
	}
}

// TestPCD1B4UnknownReaffirmationRetry3DC characterizes CW-M27 on real
// Cassandra. It tries EACH_QUORUM with dc-eu and dc-asia unavailable. Cassandra
// may reject before applying anything, so the fixture then constructs the same
// local-only post-UNKNOWN state with LOCAL_QUORUM when needed. The retry sees
// local WRITETIME>S but must still perform EACH_QUORUM; otherwise a stale
// generation's cross-DC tombstone removes the certified row outside dc-na.
func TestPCD1B4UnknownReaffirmationRetry3DC(t *testing.T) {
	phase := strings.TrimSpace(os.Getenv(phaseEnv))
	if phase == "" {
		t.Skipf("%s is not set; run scripts/pc-d1b4-certification-window-multidc-characterization.sh", phaseEnv)
	}
	f := newFixture(t)
	libraryID := uuid.NewSHA1(uuid.MustParse(f.libraryID), []byte("unknown-reaffirmation-retry")).String()
	fsID := testFSID(libraryID + "-covered-row")
	const base = int64(1_700_000_100_000_000)
	t0, stale, reaffirmTs := base, base+10, base+20
	write := func(database *dbpkg.DB, consistency gocql.Consistency) error {
		return database.Session().Query(`
			INSERT INTO fs_objects (library_id, fs_id, obj_type, size_bytes)
			VALUES (?, ?, 'file', 0) USING TIMESTAMP ?
		`, libraryID, fsID, reaffirmTs).Consistency(consistency).Exec()
	}
	localWriteTime := func(database *dbpkg.DB) int64 {
		t.Helper()
		var writeTime int64
		if err := database.Session().Query(`
			SELECT WRITETIME(obj_type) FROM fs_objects WHERE library_id = ? AND fs_id = ?
		`, libraryID, fsID).Consistency(gocql.LocalQuorum).Scan(&writeTime); err != nil {
			t.Fatalf("read local WRITETIME after ambiguous attempt: %v", err)
		}
		return writeTime
	}
	visible := func(database *dbpkg.DB) bool {
		t.Helper()
		var objType *string
		err := database.Session().Query(`
			SELECT obj_type FROM fs_objects WHERE library_id = ? AND fs_id = ?
		`, libraryID, fsID).Consistency(gocql.LocalQuorum).Scan(&objType)
		if errors.Is(err, gocql.ErrNotFound) {
			return false
		}
		if err != nil {
			t.Fatalf("read retry characterization row: %v", err)
		}
		return objType != nil
	}

	switch phase {
	case "m27-prepare":
		na := connect(t, "dc-na")
		retry(t, "seed CW-M27 row at T0 in every DC", func() error {
			return na.Session().Query(`
				INSERT INTO fs_objects (library_id, fs_id, obj_type, size_bytes)
				VALUES (?, ?, 'file', 0) USING TIMESTAMP ?
			`, libraryID, fsID, t0).Consistency(gocql.EachQuorum).Exec()
		})
	case "m27-unknown":
		na := connect(t, "dc-na")
		if err := write(na, gocql.EachQuorum); err == nil {
			t.Fatal("CW-M27 precondition: EACH_QUORUM must return an error while dc-eu and dc-asia are down")
		}
		// Cassandra may reject an unavailable EACH_QUORUM before dispatching
		// the mutation. In that case, create the exact partial replica state a
		// timed-out request can leave: dc-na has S+1, the other DCs remain T0.
		if got := localWriteTime(na); got <= stale {
			if err := write(na, gocql.LocalQuorum); err != nil {
				t.Fatalf("construct dc-na-only partial reaffirmation state: %v", err)
			}
		}
		if got := localWriteTime(na); got != reaffirmTs || got <= stale {
			t.Fatalf("CW-M27 requires a partial local application with WRITETIME>S; got %d, want %d > %d", got, reaffirmTs, stale)
		}
	case "m27-retry":
		na := connect(t, "dc-na")
		if got := localWriteTime(na); got != reaffirmTs || got <= stale {
			t.Fatalf("CW-M27 retry must start with local WRITETIME>S; got %d, want %d > %d", got, reaffirmTs, stale)
		}
		retry(t, "repeat global EACH_QUORUM reaffirmation after UNKNOWN", func() error {
			return write(na, gocql.EachQuorum)
		})
	case "m27-verify":
		na := connect(t, "dc-na")
		retry(t, "deliver stale generation tombstone globally", func() error {
			return na.Session().Query(`
				DELETE FROM fs_objects USING TIMESTAMP ? WHERE library_id = ? AND fs_id = ?
			`, stale, libraryID, fsID).Consistency(gocql.EachQuorum).Exec()
		})
		for _, dc := range []string{"dc-na", "dc-eu", "dc-asia"} {
			if database := connect(t, dc); !visible(database) {
				t.Fatalf("CW-M27: after UNKNOWN then successful EACH_QUORUM retry, stale tombstone removed the certified identity in %s", dc)
			}
		}
	case "prepare", "degrade", "certify", "merge", "rprepare", "rdegrade", "rverify", "a31-seed", "a31-verify":
		t.Skip("other certification-window phases belong to their dedicated 3-DC characterization")
	default:
		t.Fatalf("unexpected CW-M27 phase %q", phase)
	}
}

// TestPCD1B4CanonicalAbsenceProof3DC shows that a LOCAL_QUORUM miss cannot
// authorize a capability which bypasses the per-library fence. The test writes
// a canonical library row in dc-eu while dc-na is down, then compares dc-na's
// ordinary local absence observation with an EACH_QUORUM read that obtains a
// quorum in every DC. A failed/unavailable read cannot mint an absence
// capability.
func TestPCD1B4CanonicalAbsenceProof3DC(t *testing.T) {
	phase := strings.TrimSpace(os.Getenv(phaseEnv))
	if phase == "" {
		t.Skipf("%s is not set; run scripts/pc-d1b4-certification-window-multidc-characterization.sh", phaseEnv)
	}
	f := newFixture(t)
	orgID := uuid.NewSHA1(uuid.MustParse(f.orgID), []byte("canonical-absence-proof-org")).String()
	libraryID := uuid.NewSHA1(uuid.MustParse(f.libraryID), []byte("canonical-absence-proof-library")).String()

	switch phase {
	case "a31-seed":
		eu := connect(t, "dc-eu")
		now := time.Now().UTC()
		retry(t, "seed canonical library in dc-eu only", func() error {
			return eu.Session().Query(`
				INSERT INTO libraries (org_id, library_id, name, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?)
			`, orgID, libraryID, "pc-d1b4-canonical-absence", now, now).Consistency(gocql.LocalQuorum).Exec()
		})
	case "a31-verify":
		na, eu := connect(t, "dc-na"), connect(t, "dc-eu")
		localExists, err := gcpkg.NewCassandraStore(na).CanonicalLibraryExists(uuid.MustParse(orgID), uuid.MustParse(libraryID))
		if err != nil {
			t.Fatalf("local CanonicalLibraryExists in dc-na: %v", err)
		}
		remoteExists, err := gcpkg.NewCassandraStore(eu).CanonicalLibraryExists(uuid.MustParse(orgID), uuid.MustParse(libraryID))
		if err != nil {
			t.Fatalf("local CanonicalLibraryExists in dc-eu: %v", err)
		}
		if localExists || !remoteExists {
			t.Fatalf("CW-M31 precondition: want dc-na absent / dc-eu present; got na=%v eu=%v", localExists, remoteExists)
		}
		if pcd1b4CanonicalAbsenceProofConsistency() != gocql.EachQuorum {
			t.Fatal("CW-M31: global absence proof consistency is not EACH_QUORUM")
		}

		var found string
		err = na.Session().Query(`
			SELECT library_id FROM libraries WHERE org_id = ? AND library_id = ?
		`, orgID, libraryID).Consistency(pcd1b4CanonicalAbsenceProofConsistency()).Scan(&found)
		if errors.Is(err, gocql.ErrNotFound) {
			t.Fatal("CW-M31: the global proof read must see a canonical row retained in dc-eu")
		}
		if err != nil {
			t.Fatalf("global EACH_QUORUM canonical-row proof read: %v", err)
		}
		if found != libraryID {
			t.Fatalf("CW-M31: global proof read returned %q, want remotely present library %s", found, libraryID)
		}
		// No global absence proof exists, therefore this fixture must not
		// authorize the GlobalCanonicalAbsenceProof bypass or destroy anything.
	case "prepare", "degrade", "certify", "merge", "rprepare", "rdegrade", "rverify", "m27-prepare", "m27-unknown", "m27-retry", "m27-verify":
		t.Skip("other certification-window phases belong to their dedicated 3-DC characterization")
	default:
		t.Fatalf("unexpected CW-M31 phase %q", phase)
	}
}
