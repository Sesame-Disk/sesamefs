//go:build integration

package integration

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

const libraryHeadSerialDomainEvidenceEnv = "SESAMEFS_REQUIRE_LIBRARY_HEAD_SERIAL_DOMAIN_EVIDENCE"

type libraryHeadSerialDomainEvidenceState struct {
	advance bool
	initial bool
}

var libraryHeadSerialDomainEvidence libraryHeadSerialDomainEvidenceState

func (e *libraryHeadSerialDomainEvidenceState) complete() bool {
	return e.advance && e.initial
}

type libraryHeadSerialDomainDC struct {
	db     *dbpkg.DB
	helper *v2pkg.FSHelper
}

func libraryHeadSerialDomain3DCReady(t *testing.T) map[string]string {
	t.Helper()
	require := os.Getenv(libraryHeadSerialDomainEvidenceEnv) == "1"
	if strings.TrimSpace(os.Getenv(w2PostHeadMultidcEndpoints)) == "" {
		if require {
			t.Fatalf("%s=1 requires %s (session default LOCAL_SERIAL 3-DC fixture)", libraryHeadSerialDomainEvidenceEnv, w2PostHeadMultidcEndpoints)
		}
		t.Skip(w2PostHeadMultidcEndpoints + " is not set")
	}
	libraryHeadSerialDomainRequireProductionPin(t)
	return w2PostHead3DCEndpoints(t)
}

func libraryHeadSerialDomainRequireProductionPin(t *testing.T) {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	for _, site := range []struct {
		rel string
		fn  string
		pin string
	}{
		{rel: "internal/api/v2/fs_helpers.go", fn: "func (h *FSHelper) UpdateLibraryHead(", pin: "SerialConsistency(db.LibraryHeadSerialConsistency)"},
		{rel: "internal/api/sync.go", fn: "func (h *SyncHandler) updateLibraryHeadWithStats(", pin: "SerialConsistency(db.LibraryHeadSerialConsistency)"},
		{rel: "internal/api/v2/fs_helpers.go", fn: "func (h *FSHelper) InitializeLibraryHeadIfUnset(", pin: "SerialConsistency(db.LibraryHeadSerialConsistency)"},
		{rel: "internal/api/v2/write_helpers.go", fn: "func deleteUnpublishedLibraryRow(", pin: "SerialConsistency(dbpkg.LibraryHeadSerialConsistency)"},
	} {
		src := libraryHeadSerialDomainFunctionSource(t, filepath.Join(root, site.rel), site.fn)
		if !strings.Contains(src, site.pin) {
			t.Fatalf("RED: %s lost %s; session default LOCAL_SERIAL must not become the HEAD Paxos domain", site.fn, site.pin)
		}
		if strings.Contains(src, "SerialConsistency(gocql.LocalSerial)") {
			t.Fatalf("RED: %s pins SerialConsistency(gocql.LocalSerial); canonical HEAD authority must stay global SERIAL", site.fn)
		}
	}
}

func libraryHeadSerialDomainFunctionSource(t *testing.T, path, signature string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	src := string(raw)
	start := strings.Index(src, signature)
	if start < 0 {
		t.Fatalf("%s: %s not found", path, signature)
	}
	rest := src[start:]
	next := strings.Index(rest[len(signature):], "\nfunc ")
	if next < 0 {
		return rest
	}
	return rest[:len(signature)+next]
}

func libraryHeadSerialDomainConnectLocalSerial(t *testing.T, dc string, endpoints map[string]string) libraryHeadSerialDomainDC {
	t.Helper()
	database := w2PostHead3DCConnectSerial(t, dc, endpoints, "LOCAL_SERIAL")
	return libraryHeadSerialDomainDC{db: database, helper: v2pkg.NewFSHelper(database)}
}

func libraryHeadSerialDomainReadHead(t *testing.T, database *dbpkg.DB, orgID, repoID string, cl gocql.Consistency) string {
	t.Helper()
	var head string
	err := database.Session().Query(`SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, repoID).Consistency(cl).Scan(&head)
	if err != nil {
		t.Fatalf("read head at %s: %v", cl, err)
	}
	return head
}

// TestLibraryHeadSerialDomainConcurrentAdvance3DC is Leg A of
// ISSUE-LIBRARY-HEAD-SERIAL-DOMAIN-01: two DCs race H0→H1 and H0→H2 through
// production UpdateLibraryHead while the session default is LOCAL_SERIAL.
// Exactly one writer must apply; the pin to global SERIAL is what makes that
// true. The source check above turns RED if the pin is removed or downgraded
// without waiting for the race to dual-apply.
func TestLibraryHeadSerialDomainConcurrentAdvance3DC(t *testing.T) {
	endpoints := libraryHeadSerialDomain3DCReady(t)
	na := libraryHeadSerialDomainConnectLocalSerial(t, "dc-na", endpoints)
	eu := libraryHeadSerialDomainConnectLocalSerial(t, "dc-eu", endpoints)
	asia := libraryHeadSerialDomainConnectLocalSerial(t, "dc-asia", endpoints)

	orgID, repoID := uuid.NewString(), uuid.NewString()
	h0, h1, h2 := "head-serial-h0-"+uuid.NewString(), "head-serial-h1-"+uuid.NewString(), "head-serial-h2-"+uuid.NewString()
	now := time.Now().UTC()
	w2PostHeadRetryEachQuorum(t, "seed library HEAD=H0", func() error {
		return na.db.Session().Query(`
			INSERT INTO libraries (org_id, library_id, name, head_commit_id, size_bytes, file_count, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, orgID, repoID, "head-serial-advance-3dc", h0, int64(0), int64(0), now, now).Consistency(gocql.EachQuorum).Exec()
	})
	for _, commitID := range []string{h0, h1, h2} {
		id := commitID
		w2PostHeadRetryEachQuorum(t, "seed commit "+id, func() error {
			return na.db.Session().Query(`
				INSERT INTO commits (library_id, commit_id, parent_id, root_fs_id, description, created_at)
				VALUES (?, ?, ?, ?, ?, ?)
			`, repoID, id, "", "head-serial-root-"+id, "head serial domain", now).Consistency(gocql.EachQuorum).Exec()
		})
	}

	type result struct {
		commit string
		err    error
	}
	results := make([]result, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		results[0] = result{commit: h1, err: na.helper.UpdateLibraryHead(orgID, repoID, h1, h0)}
	}()
	go func() {
		defer wg.Done()
		<-start
		results[1] = result{commit: h2, err: eu.helper.UpdateLibraryHead(orgID, repoID, h2, h0)}
	}()
	close(start)
	wg.Wait()

	applied, conflicted := []string{}, []string{}
	for _, r := range results {
		switch {
		case r.err == nil:
			applied = append(applied, r.commit)
		case errors.Is(r.err, v2pkg.ErrLibraryHeadConflict):
			conflicted = append(conflicted, r.commit)
		default:
			t.Fatalf("HEAD advance %s: unexpected error %v", r.commit, r.err)
		}
	}
	if len(applied) != 1 || len(conflicted) != 1 {
		t.Fatalf("RED: concurrent H0 advances applied=%v conflicted=%v; want exactly one winner (session default was LOCAL_SERIAL; productive pin must keep global SERIAL)", applied, conflicted)
	}
	winner := applied[0]
	if winner != h1 && winner != h2 {
		t.Fatalf("winner %s is neither H1 nor H2", winner)
	}

	for _, node := range []libraryHeadSerialDomainDC{na, eu, asia} {
		got := libraryHeadSerialDomainReadHead(t, node.db, orgID, repoID, gocql.Serial)
		if got != winner {
			t.Fatalf("canonical HEAD at SERIAL = %s, want the unique winner %s", got, winner)
		}
	}
	libraryHeadSerialDomainEvidence.advance = true
	t.Logf("GREEN: session default LOCAL_SERIAL; exactly one of H0→H1 / H0→H2 applied (%s); the other observed conflict", winner)
}

// TestLibraryHeadSerialDomainInitialHead3DC is Leg B: two DCs race
// InitializeLibraryHeadIfUnset on a null-HEAD library with session default
// LOCAL_SERIAL. First-writer-wins must still be global.
func TestLibraryHeadSerialDomainInitialHead3DC(t *testing.T) {
	endpoints := libraryHeadSerialDomain3DCReady(t)
	na := libraryHeadSerialDomainConnectLocalSerial(t, "dc-na", endpoints)
	eu := libraryHeadSerialDomainConnectLocalSerial(t, "dc-eu", endpoints)
	asia := libraryHeadSerialDomainConnectLocalSerial(t, "dc-asia", endpoints)

	orgID, repoID := uuid.NewString(), uuid.NewString()
	c1, c2 := "head-serial-init-1-"+uuid.NewString(), "head-serial-init-2-"+uuid.NewString()
	now := time.Now().UTC()
	w2PostHeadRetryEachQuorum(t, "seed unpublished library", func() error {
		return na.db.Session().Query(`
			INSERT INTO libraries (org_id, library_id, name, size_bytes, file_count, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, orgID, repoID, "head-serial-init-3dc", int64(0), int64(0), now, now).Consistency(gocql.EachQuorum).Exec()
	})
	for _, commitID := range []string{c1, c2} {
		id := commitID
		w2PostHeadRetryEachQuorum(t, "seed initial commit "+id, func() error {
			return na.db.Session().Query(`
				INSERT INTO commits (library_id, commit_id, root_fs_id, description, created_at)
				VALUES (?, ?, ?, ?, ?)
			`, repoID, id, "head-serial-init-root-"+id, "head serial initial", now).Consistency(gocql.EachQuorum).Exec()
		})
	}

	type initResult struct {
		commit  string
		head    string
		outcome v2pkg.InitialHeadOutcome
		err     error
	}
	results := make([]initResult, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		head, outcome, err := na.helper.InitializeLibraryHeadIfUnset(orgID, repoID, c1, now)
		results[0] = initResult{commit: c1, head: head, outcome: outcome, err: err}
	}()
	go func() {
		defer wg.Done()
		<-start
		head, outcome, err := eu.helper.InitializeLibraryHeadIfUnset(orgID, repoID, c2, now)
		results[1] = initResult{commit: c2, head: head, outcome: outcome, err: err}
	}()
	close(start)
	wg.Wait()

	applied, adopted := []string{}, []string{}
	for _, r := range results {
		if r.err != nil {
			t.Fatalf("initializer %s: %v", r.commit, r.err)
		}
		switch r.outcome {
		case v2pkg.InitialHeadApplied:
			if r.head != r.commit {
				t.Fatalf("applied initializer %s reported head %s", r.commit, r.head)
			}
			applied = append(applied, r.commit)
		case v2pkg.InitialHeadAlreadyInitialized:
			adopted = append(adopted, r.head)
		default:
			t.Fatalf("initializer %s: unexpected outcome %s", r.commit, r.outcome)
		}
	}
	if len(applied) != 1 || len(adopted) != 1 {
		t.Fatalf("RED: concurrent initial HEAD applied=%v adopted=%v; want exactly one global winner under session default LOCAL_SERIAL", applied, adopted)
	}
	winner := applied[0]
	if adopted[0] != winner {
		t.Fatalf("loser adopted %s, want the unique winner %s", adopted[0], winner)
	}
	for _, node := range []libraryHeadSerialDomainDC{na, eu, asia} {
		got := libraryHeadSerialDomainReadHead(t, node.db, orgID, repoID, gocql.Serial)
		if got != winner {
			t.Fatalf("canonical initial HEAD at SERIAL = %s, want %s", got, winner)
		}
	}
	libraryHeadSerialDomainEvidence.initial = true
	t.Logf("GREEN: session default LOCAL_SERIAL; exactly one initial HEAD winner (%s)", winner)
}
