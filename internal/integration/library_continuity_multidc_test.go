//go:build integration

package integration

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

const libraryContinuityEvidenceEnv = "SESAMEFS_REQUIRE_LIBRARY_CONTINUITY_EVIDENCE"

var libraryContinuityEvidence bool

func libraryContinuity3DCReady(t *testing.T) map[string]string {
	t.Helper()
	if os.Getenv(libraryContinuityEvidenceEnv) != "1" {
		t.Skipf("%s is not set", libraryContinuityEvidenceEnv)
	}
	if strings.TrimSpace(os.Getenv(w2PostHeadMultidcEndpoints)) == "" {
		t.Fatalf("%s=1 requires %s", libraryContinuityEvidenceEnv, w2PostHeadMultidcEndpoints)
	}
	return w2PostHead3DCEndpoints(t)
}

func readLibraryContinuityState(t *testing.T, database *dbpkg.DB, orgID, libraryID string) (string, *string, *string) {
	t.Helper()
	var head string
	var certified *string
	var version *string
	err := database.Session().Query(`
		SELECT head_commit_id, continuity_certified_head_commit_id, continuity_contract_version
		FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, libraryID).Consistency(gocql.Serial).Scan(&head, &certified, &version)
	if err != nil {
		t.Fatalf("read continuity state: %v", err)
	}
	return head, certified, version
}

func seedLibraryContinuityRow(t *testing.T, database *dbpkg.DB, orgID, libraryID, head string) {
	t.Helper()
	now := time.Now().UTC()
	w2PostHeadRetryEachQuorum(t, "seed continuity library", func() error {
		return database.Session().Query(`
			INSERT INTO libraries (org_id, library_id, name, head_commit_id, size_bytes, file_count, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, orgID, libraryID, "pc-d1a-continuity-3dc", head, int64(0), int64(0), now, now).Consistency(gocql.EachQuorum).Exec()
	})
}

func seedLibraryContinuityCommit(t *testing.T, database *dbpkg.DB, libraryID, commitID string) {
	t.Helper()
	now := time.Now().UTC()
	w2PostHeadRetryEachQuorum(t, "seed continuity commit "+commitID, func() error {
		return database.Session().Query(`
			INSERT INTO commits (library_id, commit_id, parent_id, root_fs_id, description, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, libraryID, commitID, "", "pc-d1a-continuity-root-"+commitID, "pc-d1a continuity", now).Consistency(gocql.EachQuorum).Exec()
	})
}

// TestLibraryContinuityCertifiedFrontier3DC exercises PC-D1A against the real
// three-DC Cassandra fixture while every session defaults to LOCAL_SERIAL.
// The new primitives must still pin global SERIAL themselves.
func TestLibraryContinuityCertifiedFrontier3DC(t *testing.T) {
	endpoints := libraryContinuity3DCReady(t)
	na := w2PostHead3DCConnectSerial(t, "dc-na", endpoints, "LOCAL_SERIAL")
	eu := w2PostHead3DCConnectSerial(t, "dc-eu", endpoints, "LOCAL_SERIAL")
	asia := w2PostHead3DCConnectSerial(t, "dc-asia", endpoints, "LOCAL_SERIAL")

	orgID, libraryID := uuid.NewString(), uuid.NewString()
	h0 := "pc-d1a-h0-" + uuid.NewString()
	h1 := "pc-d1a-h1-" + uuid.NewString()
	h2 := "pc-d1a-h2-" + uuid.NewString()
	seedLibraryContinuityRow(t, na, orgID, libraryID, h0)
	for _, commitID := range []string{h1, h2} {
		seedLibraryContinuityCommit(t, na, libraryID, commitID)
	}

	state, err := dbpkg.ReadLibraryState(na.Session(), orgID, libraryID)
	if err != nil {
		t.Fatalf("read initial LibraryState: %v", err)
	}
	if state.ContinuityCertifiedHeadCommitID != nil || state.ContinuityContractVersion != nil {
		t.Fatalf("new library row is not null/null: certified=%v version=%v", state.ContinuityCertifiedHeadCommitID, state.ContinuityContractVersion)
	}
	if state.ContinuityWitnessValidFor(dbpkg.SupportedContinuityContractVersion) {
		t.Fatal("null/null witness was accepted as certified")
	}

	type certifyResult struct {
		result dbpkg.LibraryContinuityCASResult
		err    error
	}
	results := make(chan certifyResult, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		result, err := dbpkg.CommitLibraryContinuityWitness(na.Session(), orgID, libraryID, h0, dbpkg.SupportedContinuityContractVersion)
		results <- certifyResult{result: result, err: err}
	}()
	go func() {
		defer wg.Done()
		result, err := dbpkg.CommitLibraryContinuityWitness(eu.Session(), orgID, libraryID, h0, dbpkg.SupportedContinuityContractVersion)
		results <- certifyResult{result: result, err: err}
	}()
	wg.Wait()
	close(results)
	for result := range results {
		if result.err != nil || result.result.Outcome != dbpkg.LibraryContinuityCASApplied {
			t.Fatalf("competing baseline certificates did not converge: result=%+v err=%v", result.result, result.err)
		}
	}

	for _, database := range []*dbpkg.DB{na, eu, asia} {
		head, certified, version := readLibraryContinuityState(t, database, orgID, libraryID)
		if head != h0 || certified == nil || *certified != h0 || version == nil || *version != dbpkg.SupportedContinuityContractVersion {
			t.Fatalf("baseline witness did not converge: head=%s certified=%v version=%v", head, certified, version)
		}
	}

	retry, err := dbpkg.CommitLibraryContinuityWitness(asia.Session(), orgID, libraryID, h0, dbpkg.SupportedContinuityContractVersion)
	if err != nil || retry.Outcome != dbpkg.LibraryContinuityCASApplied {
		t.Fatalf("same witness retry was not idempotently accepted: result=%+v err=%v", retry, err)
	}

	if err := v2pkg.NewFSHelper(na).UpdateLibraryHead(orgID, libraryID, h1, h0); err != nil {
		t.Fatalf("legacy HEAD writer H0->H1: %v", err)
	}
	head, certified, version := readLibraryContinuityState(t, asia, orgID, libraryID)
	if head != h1 || certified == nil || *certified != h0 || version == nil || *version != dbpkg.SupportedContinuityContractVersion {
		t.Fatalf("legacy HEAD move did not leave the expected stale witness: head=%s certified=%v version=%v", head, certified, version)
	}

	stale, err := dbpkg.CommitLibraryContinuityWitness(asia.Session(), orgID, libraryID, h0, dbpkg.SupportedContinuityContractVersion)
	if err != nil || stale.Outcome != dbpkg.LibraryContinuityCASNotApplied {
		t.Fatalf("stale certifier outcome=%+v err=%v, want NOT_APPLIED", stale, err)
	}
	head, certified, version = readLibraryContinuityState(t, eu, orgID, libraryID)
	if head != h1 || certified == nil || *certified != h0 || version == nil || *version != dbpkg.SupportedContinuityContractVersion {
		t.Fatalf("stale certifier changed authority: head=%s certified=%v version=%v", head, certified, version)
	}

	fresh, err := dbpkg.CommitLibraryContinuityWitness(eu.Session(), orgID, libraryID, h1, dbpkg.SupportedContinuityContractVersion)
	if err != nil || fresh.Outcome != dbpkg.LibraryContinuityCASApplied {
		t.Fatalf("fresh H1 certificate outcome=%+v err=%v", fresh, err)
	}

	advanced, err := dbpkg.AdvanceLibraryCertifiedFrontier(asia.Session(), orgID, libraryID, h1, h2, dbpkg.SupportedContinuityContractVersion)
	if err != nil || advanced.Outcome != dbpkg.LibraryContinuityCASApplied {
		t.Fatalf("atomic H1->H2 advance outcome=%+v err=%v", advanced, err)
	}
	for _, database := range []*dbpkg.DB{na, eu, asia} {
		head, certified, version = readLibraryContinuityState(t, database, orgID, libraryID)
		if head != h2 || certified == nil || *certified != h2 || version == nil || *version != dbpkg.SupportedContinuityContractVersion {
			t.Fatalf("atomic advance did not converge: head=%s certified=%v version=%v", head, certified, version)
		}
	}

	staleAdvance, err := dbpkg.AdvanceLibraryCertifiedFrontier(na.Session(), orgID, libraryID, h1, h1+"-unexpected", dbpkg.SupportedContinuityContractVersion)
	if err != nil || staleAdvance.Outcome != dbpkg.LibraryContinuityCASNotApplied {
		t.Fatalf("stale frontier predecessor outcome=%+v err=%v, want NOT_APPLIED", staleAdvance, err)
	}
	wrongVersion, err := dbpkg.AdvanceLibraryCertifiedFrontier(na.Session(), orgID, libraryID, h2, h2+"-wrong-version", "V0")
	if err == nil || !errors.Is(err, dbpkg.ErrUnsupportedContinuityContract) || wrongVersion.Outcome != dbpkg.LibraryContinuityCASUnknown {
		t.Fatalf("wrong-version advance outcome=%+v err=%v, want UNKNOWN plus unsupported-contract error", wrongVersion, err)
	}
	head, certified, version = readLibraryContinuityState(t, na, orgID, libraryID)
	if head != h2 || certified == nil || *certified != h2 || version == nil || *version != dbpkg.SupportedContinuityContractVersion {
		t.Fatalf("rejected advances changed the certified frontier: head=%s certified=%v version=%v", head, certified, version)
	}

	missingOrgID, missingLibraryID := uuid.NewString(), uuid.NewString()
	missingHead := "pc-d1a-missing-witness-" + uuid.NewString()
	seedLibraryContinuityRow(t, na, missingOrgID, missingLibraryID, missingHead)
	missing, err := dbpkg.AdvanceLibraryCertifiedFrontier(eu.Session(), missingOrgID, missingLibraryID, missingHead, h2, dbpkg.SupportedContinuityContractVersion)
	if err != nil || missing.Outcome != dbpkg.LibraryContinuityCASNotApplied {
		t.Fatalf("missing predecessor witness outcome=%+v err=%v, want NOT_APPLIED", missing, err)
	}
	head, certified, version = readLibraryContinuityState(t, asia, missingOrgID, missingLibraryID)
	if head != missingHead || certified != nil || version != nil {
		t.Fatalf("missing-witness advance performed a fallback mutation: head=%s certified=%v version=%v", head, certified, version)
	}

	deletedOrgID, deletedLibraryID := uuid.NewString(), uuid.NewString()
	deletedHead := "pc-d1a-deleted-" + uuid.NewString()
	seedLibraryContinuityRow(t, na, deletedOrgID, deletedLibraryID, deletedHead)
	deletedCertificate, err := dbpkg.CommitLibraryContinuityWitness(na.Session(), deletedOrgID, deletedLibraryID, deletedHead, dbpkg.SupportedContinuityContractVersion)
	if err != nil || deletedCertificate.Outcome != dbpkg.LibraryContinuityCASApplied {
		t.Fatalf("deleted-library setup certificate outcome=%+v err=%v", deletedCertificate, err)
	}
	deletedAt := time.Now().UTC()
	w2PostHeadRetryEachQuorum(t, "soft-delete continuity library", func() error {
		return na.Session().Query(`
			UPDATE libraries SET deleted_at = ?
			WHERE org_id = ? AND library_id = ?
		`, deletedAt, deletedOrgID, deletedLibraryID).Consistency(gocql.EachQuorum).Exec()
	})
	deletedState, err := dbpkg.ReadLibraryState(na.Session(), deletedOrgID, deletedLibraryID)
	if err != nil {
		t.Fatalf("read soft-deleted LibraryState: %v", err)
	}
	if deletedState.DeletedAt == nil {
		t.Fatalf("soft-deleted setup did not preserve deleted state: %+v", deletedState)
	}
	if deletedState.ContinuityWitnessValidFor(dbpkg.SupportedContinuityContractVersion) {
		t.Fatal("soft-deleted witness was accepted as live")
	}
	deletedCommit, err := dbpkg.CommitLibraryContinuityWitness(asia.Session(), deletedOrgID, deletedLibraryID, deletedHead, dbpkg.SupportedContinuityContractVersion)
	if err != nil || deletedCommit.Outcome != dbpkg.LibraryContinuityCASNotApplied {
		t.Fatalf("deleted-library baseline witness outcome=%+v err=%v, want NOT_APPLIED", deletedCommit, err)
	}
	deletedAdvance, err := dbpkg.AdvanceLibraryCertifiedFrontier(eu.Session(), deletedOrgID, deletedLibraryID, deletedHead, deletedHead+"-next", dbpkg.SupportedContinuityContractVersion)
	if err != nil || deletedAdvance.Outcome != dbpkg.LibraryContinuityCASNotApplied {
		t.Fatalf("deleted-library frontier outcome=%+v err=%v, want NOT_APPLIED", deletedAdvance, err)
	}
	head, certified, version = readLibraryContinuityState(t, asia, deletedOrgID, deletedLibraryID)
	if head != deletedHead || certified == nil || *certified != deletedHead || version == nil || *version != dbpkg.SupportedContinuityContractVersion {
		t.Fatalf("deleted-library authority changed despite rejected LWTs: head=%s certified=%v version=%v", head, certified, version)
	}

	libraryContinuityEvidence = true
	t.Logf("GREEN: competing baseline certificates converged; stale H0 certification lost after H0->H1; atomic H1->H2 preserved head=witness under LOCAL_SERIAL sessions with explicit global SERIAL primitives")
}
