//go:build integration

package integration

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	v2api "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

// E1-15F: the #275 SUPERSEDED settlement on real three-DC Cassandra. The
// runner script (scripts/e115f-superseded-3dc-validation.sh) invokes one phase
// per process from a named DC and passes ids through the log. Only identity
// and content rows are CQL fixtures; commits, HEAD moves, repairs and refs use
// production functions, and classification/settlement come only from the
// production sweep.
const e115f3DCPhaseEnv = "E115F_3DC_PHASE"

type e115fIDs struct {
	org, repo, user, parent, c1, fs, block, h1, c2, fs2, block2, h2 string
}

func e115fEnvIDs(t *testing.T, need ...string) e115fIDs {
	t.Helper()
	get := func(name string) string { return strings.TrimSpace(os.Getenv("E115F_" + name)) }
	ids := e115fIDs{org: get("ORG"), repo: get("REPO"), user: get("USER"), parent: get("PARENT"), c1: get("C1"), fs: get("FS"), block: get("BLOCK"), h1: get("H1"), c2: get("C2"), fs2: get("FS2"), block2: get("BLOCK2"), h2: get("H2")}
	for _, name := range need {
		if get(name) == "" {
			t.Fatalf("E115F_%s is required for phase %s", name, os.Getenv(e115f3DCPhaseEnv))
		}
	}
	return ids
}

func e115fHex(n int) string {
	sum := sha256.Sum256([]byte(uuid.NewString()))
	if n == 40 {
		short := sha1.Sum(sum[:])
		return hex.EncodeToString(short[:])
	}
	return hex.EncodeToString(sum[:])
}

// e115fCommit writes a commit through the production identity-authorized
// commit path (the one v2 insertCommit uses).
func e115fCommit(t *testing.T, database *dbpkg.DB, repo, user, commit, parent string) {
	t.Helper()
	w2PostHeadRetryEachQuorum(t, "authorize+materialize commit "+commit, func() error {
		authorized, err := dbpkg.AuthorizeCommitProjection(context.Background(), database.Session(), dbpkg.CommitProjection{
			LibraryID: repo, CommitID: commit, ParentID: parent, RootFSID: "e115f-root-" + commit, CreatorID: user, Description: "E1-15F " + commit, CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			return err
		}
		return dbpkg.MaterializeAuthorizedCommit(database.Session(), authorized)
	})
}

// e115fAdvanceHead moves HEAD from expected to next through the production
// SERIAL CAS (FSHelper.UpdateLibraryHeadFromSnapshot).
func e115fAdvanceHead(t *testing.T, database *dbpkg.DB, repo, expected, next string) {
	t.Helper()
	helper := v2api.NewFSHelper(database)
	snapshot, err := helper.GetLibraryHeadSnapshot(repo)
	if err != nil {
		t.Fatalf("HEAD snapshot: %v", err)
	}
	if snapshot.HeadCommitID != expected {
		t.Fatalf("snapshot HEAD %s, want %s", snapshot.HeadCommitID, expected)
	}
	if err := helper.UpdateLibraryHeadFromSnapshot(snapshot, repo, next, expected); err != nil {
		t.Fatalf("production HEAD CAS %s -> %s: %v", expected, next, err)
	}
}

func e115fSerialHead(t *testing.T, database *dbpkg.DB, org, repo string) string {
	t.Helper()
	var head string
	w2PostHeadRetryEachQuorum(t, "SERIAL HEAD read", func() error {
		return database.Session().Query(`SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?`, org, repo).Consistency(gocql.Serial).Scan(&head)
	})
	return head
}

// e115fRefs lists L's referrers at EACH_QUORUM (cross-DC verification domain).
func e115fRefs(t *testing.T, database *dbpkg.DB, org, block string) map[string]bool {
	t.Helper()
	refs := map[string]bool{}
	w2PostHeadRetryEachQuorum(t, "EACH_QUORUM referrers", func() error {
		refs = map[string]bool{}
		iter := database.Session().Query(`SELECT referrer FROM block_references WHERE org_id = ? AND block_id = ?`, org, block).Consistency(gocql.EachQuorum).Iter()
		var ref string
		for iter.Scan(&ref) {
			refs[ref] = true
		}
		return iter.Close()
	})
	return refs
}

func e115fRepairPresent(t *testing.T, database *dbpkg.DB, consistency gocql.Consistency, org, repo, commit, fs string) bool {
	t.Helper()
	var stored string
	var err error
	op := func() error {
		err = database.Session().Query(`SELECT fs_id FROM published_block_reference_repairs WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?`,
			publishRepairIntegrationBucket(org, repo, commit, fs), org, repo, commit, fs).Consistency(consistency).Scan(&stored)
		if errors.Is(err, gocql.ErrNotFound) {
			return nil
		}
		return err
	}
	if consistency == gocql.EachQuorum {
		w2PostHeadRetryEachQuorum(t, "EACH_QUORUM repair row", op)
	} else if err := op(); err != nil {
		t.Fatalf("repair row read: %v", err)
	}
	return err == nil && stored == fs
}

func e115fCommitParent(t *testing.T, database *dbpkg.DB, repo, commit string) string {
	t.Helper()
	var parent string
	w2PostHeadRetryEachQuorum(t, "EACH_QUORUM commit "+commit, func() error {
		return database.Session().Query(`SELECT parent_id FROM commits WHERE library_id = ? AND commit_id = ?`, repo, commit).Consistency(gocql.EachQuorum).Scan(&parent)
	})
	return parent
}

// e115fSweep runs one production sweep from this DC's session and returns the
// classifier outcomes observed for this library. visit orders repeated sweeps
// in one process: each runs 7h after the previous one, past the maximum
// process-local retry backoff (6h) a failed visit records.
func e115fSweep(t *testing.T, database *dbpkg.DB, repo string, visit int) ([]string, error) {
	t.Helper()
	var mu sync.Mutex
	var outcomes []string
	restore := v2api.SetRepairAfterClassifyForIntegration(database, repo, func(outcome string, err error) {
		mu.Lock()
		defer mu.Unlock()
		outcomes = append(outcomes, fmt.Sprintf("%s/%v", outcome, err))
	})
	defer restore()
	err := v2api.RunPublishedBlockReferenceRepairSweepAtForIntegration(database, time.Now().Add(2*time.Hour+time.Duration(visit)*7*time.Hour))
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), outcomes...), err
}

func e115fAvailability(err error) bool {
	if err == nil {
		return false
	}
	var unavailable *gocql.RequestErrUnavailable
	var readTimeout *gocql.RequestErrReadTimeout
	var writeTimeout *gocql.RequestErrWriteTimeout
	msg := strings.ToLower(err.Error())
	return errors.As(err, &unavailable) || errors.As(err, &readTimeout) || errors.As(err, &writeTimeout) ||
		strings.Contains(msg, "timed out") || strings.Contains(msg, "unavailable") || strings.Contains(msg, "received only") || strings.Contains(msg, "no response")
}

// e115fVerifySettled checks M1 from one DC at EACH_QUORUM.
func e115fVerifySettled(t *testing.T, dc string, ids e115fIDs) {
	t.Helper()
	database := w2PostHead3DCConnect(t, dc, w2PostHead3DCEndpoints(t))
	owned := v2api.PublishedBlockReferenceRepairLivenessReferrerForIntegration(ids.repo, ids.c1, ids.fs)
	if e115fRepairPresent(t, database, gocql.EachQuorum, ids.org, ids.repo, ids.c1, ids.fs) {
		t.Fatalf("%s: R row still present at EACH_QUORUM", dc)
	}
	refs := e115fRefs(t, database, ids.org, ids.block)
	if refs[owned] {
		t.Fatalf("%s: R's own %s survived settlement: %v", dc, owned, refs)
	}
	if !refs[dbpkg.BlockReferrerForPublishAttempt(ids.c1)] || !refs[dbpkg.BlockReferrerForFSObject(ids.repo, ids.fs)] {
		t.Fatalf("%s: settlement touched another referrer: %v", dc, refs)
	}
	if head := e115fSerialHead(t, database, ids.org, ids.repo); head != ids.h1 {
		t.Fatalf("%s: HEAD %s, want H1 %s", dc, head, ids.h1)
	}
	if e115fCommitParent(t, database, ids.repo, ids.c1) != ids.parent || e115fCommitParent(t, database, ids.repo, ids.h1) != ids.parent {
		t.Fatalf("%s: commits c1/H1 changed", dc)
	}
	t.Logf("E1-15F M1 verified from %s at EACH_QUORUM: R row and %s gone; staging pub:<c1> and legitimate fs: kept; HEAD=H1; commits intact", dc, owned)
}

func TestE115FSupersededCrossDC3DC(t *testing.T) {
	phase := strings.TrimSpace(os.Getenv(e115f3DCPhaseEnv))
	if phase == "" {
		t.Skipf("%s is not set", e115f3DCPhaseEnv)
	}
	endpoints := w2PostHead3DCEndpoints(t)
	dc := strings.TrimSpace(os.Getenv("CASSANDRA_LOCAL_DC"))
	if dc == "" {
		t.Fatal("CASSANDRA_LOCAL_DC names the worker DC for this phase")
	}
	database := w2PostHead3DCConnect(t, dc, endpoints)
	switch phase {
	case "seed":
		ids := e115fIDs{org: uuid.NewString(), repo: uuid.NewString(), user: uuid.NewString(), parent: e115fHex(40), c1: e115fHex(40), fs: e115fHex(40), block: e115fHex(64)}
		e115fCommit(t, database, ids.repo, ids.user, ids.parent, "")
		now := time.Now().UTC()
		w2PostHeadRetryEachQuorum(t, "fixture library row", func() error {
			return database.Session().Query(`INSERT INTO libraries (org_id, library_id, owner_id, name, encrypted, block_representation_id, head_commit_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				ids.org, ids.repo, ids.user, "e115f-3dc", false, dbpkg.PlainBlockRepresentationID, ids.parent, now, now).Consistency(gocql.EachQuorum).Exec()
		})
		w2PostHeadRetryEachQuorum(t, "fixture libraries_by_id row", func() error {
			return database.Session().Query(`INSERT INTO libraries_by_id (library_id, org_id, owner_id, name, head_commit_id, encrypted) VALUES (?, ?, ?, ?, ?, ?)`,
				ids.repo, ids.org, ids.user, "e115f-3dc", ids.parent, false).Consistency(gocql.EachQuorum).Exec()
		})
		w2PostHeadRetryEachQuorum(t, "fixture fs_object", func() error {
			return database.Session().Query(`INSERT INTO fs_objects (library_id, fs_id, obj_type, obj_name, block_ids, size_bytes) VALUES (?, ?, ?, ?, ?, ?)`,
				ids.repo, ids.fs, "file", "e115f.bin", []string{ids.block}, int64(1)).Consistency(gocql.EachQuorum).Exec()
		})
		// The abandoned attempt, written from this DC: c1 (parent P), its
		// staging pub:<c1>, and the durable repair R. A legitimate publication
		// of the same content-addressed fs_id holds fs:<repo:fs>.
		e115fCommit(t, database, ids.repo, ids.user, ids.c1, ids.parent)
		if err := dbpkg.AddPublishAttemptReferences(database, ids.org, ids.repo, ids.c1, []string{ids.block}); err != nil {
			t.Fatalf("staging pub:<c1>: %v", err)
		}
		if err := v2api.QueuePublishedFSObjectBlockReferenceRepair(database, ids.org, ids.repo, ids.c1, ids.fs, []string{ids.block}); err != nil {
			t.Fatalf("queue R: %v", err)
		}
		if err := v2api.NewFSHelper(database).RegisterFSObjectBlockReferences(ids.org, ids.repo, ids.fs, []string{ids.block}); err != nil {
			t.Fatalf("legitimate fs: %v", err)
		}
		for k, v := range map[string]string{"ORG": ids.org, "REPO": ids.repo, "USER": ids.user, "PARENT": ids.parent, "C1": ids.c1, "FS": ids.fs, "BLOCK": ids.block} {
			t.Logf("E115F_%s=%s", k, v)
		}
	case "m2":
		ids := e115fEnvIDs(t, "ORG", "REPO", "PARENT", "C1", "FS", "BLOCK")
		if head := e115fSerialHead(t, database, ids.org, ids.repo); head != ids.parent || e115fCommitParent(t, database, ids.repo, ids.c1) != ids.parent {
			t.Fatalf("M2 needs HEAD = c1.parent: HEAD=%s", head)
		}
		outcomes, err := e115fSweep(t, database, ids.repo, 0)
		if len(outcomes) != 1 || outcomes[0] != "unknown/<nil>" || err == nil || !strings.Contains(err.Error(), "unknown; retain queued repair") {
			t.Fatalf("M2 sweep from %s: outcomes=%v err=%v", dc, outcomes, err)
		}
		other := w2PostHead3DCConnect(t, "dc-eu", endpoints)
		owned := v2api.PublishedBlockReferenceRepairLivenessReferrerForIntegration(ids.repo, ids.c1, ids.fs)
		if !e115fRepairPresent(t, other, gocql.EachQuorum, ids.org, ids.repo, ids.c1, ids.fs) || !e115fRefs(t, other, ids.org, ids.block)[owned] {
			t.Fatal("M2: R or its renewed pub: not visible from dc-eu at EACH_QUORUM")
		}
		t.Logf("E1-15F M2: HEAD = c1.parent; sweep from %s classified natively UNKNOWN; from dc-eu at EACH_QUORUM R is kept and %s renewed", dc, owned)
	case "advance":
		ids := e115fEnvIDs(t, "ORG", "REPO", "USER", "PARENT", "C1")
		h1 := e115fHex(40)
		e115fCommit(t, database, ids.repo, ids.user, h1, ids.parent)
		e115fAdvanceHead(t, database, ids.repo, ids.parent, h1)
		// Before the outage, the competitor's commit and HEAD must be visible in
		// the other DCs' own replicas (LOCAL_QUORUM there), not only via eu.
		for _, peer := range []string{"dc-na", "dc-asia"} {
			peerDB := w2PostHead3DCConnect(t, peer, endpoints)
			deadline := time.Now().Add(60 * time.Second)
			for {
				var parent, head string
				errC := peerDB.Session().Query(`SELECT parent_id FROM commits WHERE library_id = ? AND commit_id = ?`, ids.repo, h1).Consistency(gocql.LocalQuorum).Scan(&parent)
				errH := peerDB.Session().Query(`SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?`, ids.org, ids.repo).Consistency(gocql.LocalQuorum).Scan(&head)
				if errC == nil && errH == nil && parent == ids.parent && head == h1 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("H1 not replicated to %s: parent=%q head=%q errs=%v/%v", peer, parent, head, errC, errH)
				}
				time.Sleep(time.Second)
			}
		}
		t.Logf("E115F_H1=%s", h1)
		t.Logf("E1-15F competitor from %s: production commit H1 (parent P) and SERIAL HEAD CAS P -> H1; replicated to dc-na and dc-asia", dc)
	case "m4a":
		ids := e115fEnvIDs(t, "ORG", "REPO", "C1", "FS", "BLOCK", "H1")
		// One DC is down: SERIAL (2/3) works, EACH_QUORUM (3/3) cannot.
		owned := v2api.PublishedBlockReferenceRepairLivenessReferrerForIntegration(ids.repo, ids.c1, ids.fs)
		deadline := time.Now().Add(60 * time.Second)
		for visit := 0; ; visit++ {
			outcomes, err := e115fSweep(t, database, ids.repo, visit)
			t.Logf("M4a sweep from %s with a DC down: outcomes=%v err=%v", dc, outcomes, err)
			for _, o := range outcomes {
				if strings.HasPrefix(o, "superseded") || strings.HasPrefix(o, "reachable") {
					t.Fatalf("M4a: %s without complete evidence", o)
				}
			}
			if !e115fRepairPresent(t, database, gocql.LocalQuorum, ids.org, ids.repo, ids.c1, ids.fs) {
				t.Fatal("M4a: R removed while a DC was unavailable")
			}
			if len(outcomes) == 1 && strings.HasPrefix(outcomes[0], "unknown/") && outcomes[0] != "unknown/<nil>" && e115fAvailability(err) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("M4a: no availability-attributed UNKNOWN: outcomes=%v err=%v", outcomes, err)
			}
			time.Sleep(2 * time.Second)
		}
		var ref string
		if err := database.Session().Query(`SELECT referrer FROM block_references WHERE org_id = ? AND block_id = ? AND referrer = ?`, ids.org, ids.block, owned).Consistency(gocql.LocalQuorum).Scan(&ref); err != nil || ref != owned {
			t.Fatalf("M4a: R's own pub: not retained locally: %q %v", ref, err)
		}
		t.Logf("E1-15F M4a: with one DC down, sweeps from %s stayed UNKNOWN with an availability error; R and %s retained", dc, owned)
	case "m4a-recovery":
		ids := e115fEnvIDs(t, "ORG", "REPO", "C1", "FS", "BLOCK", "H1")
		deadline := time.Now().Add(90 * time.Second)
		for visit := 0; ; visit++ {
			outcomes, err := e115fSweep(t, database, ids.repo, visit)
			t.Logf("recovery sweep from %s: outcomes=%v err=%v", dc, outcomes, err)
			if len(outcomes) == 1 && outcomes[0] == "superseded/<nil>" {
				break
			}
			if len(outcomes) > 1 || (len(outcomes) == 1 && !strings.HasPrefix(outcomes[0], "unknown/")) || (len(outcomes) == 1 && !e115fAvailability(err)) || time.Now().After(deadline) {
				t.Fatalf("recovery from %s did not settle: outcomes=%v err=%v", dc, outcomes, err)
			}
			time.Sleep(3 * time.Second)
		}
		t.Logf("E1-15F M4a recovery: after the DC returned, a sweep from %s classified R natively SUPERSEDED and settled it", dc)
	case "m1-verify":
		ids := e115fEnvIDs(t, "ORG", "REPO", "PARENT", "C1", "FS", "BLOCK", "H1")
		e115fVerifySettled(t, dc, ids)
	case "m3-setup":
		ids := e115fEnvIDs(t, "ORG", "REPO", "USER", "H1")
		ids.c2, ids.fs2, ids.block2 = e115fHex(40), e115fHex(40), e115fHex(64)
		w2PostHeadRetryEachQuorum(t, "fixture fs_object for c2", func() error {
			return database.Session().Query(`INSERT INTO fs_objects (library_id, fs_id, obj_type, obj_name, block_ids, size_bytes) VALUES (?, ?, ?, ?, ?, ?)`,
				ids.repo, ids.fs2, "file", "e115f-2.bin", []string{ids.block2}, int64(1)).Consistency(gocql.EachQuorum).Exec()
		})
		e115fCommit(t, database, ids.repo, ids.user, ids.c2, ids.h1)
		if err := dbpkg.AddPublishAttemptReferences(database, ids.org, ids.repo, ids.c2, []string{ids.block2}); err != nil {
			t.Fatalf("staging pub:<c2>: %v", err)
		}
		if err := v2api.QueuePublishedFSObjectBlockReferenceRepair(database, ids.org, ids.repo, ids.c2, ids.fs2, []string{ids.block2}); err != nil {
			t.Fatalf("queue R2: %v", err)
		}
		t.Logf("E115F_C2=%s", ids.c2)
		t.Logf("E115F_FS2=%s", ids.fs2)
		t.Logf("E115F_BLOCK2=%s", ids.block2)
	case "m3-publish":
		ids := e115fEnvIDs(t, "ORG", "REPO", "USER", "H1", "C2")
		e115fAdvanceHead(t, database, ids.repo, ids.h1, ids.c2)
		h2 := e115fHex(40)
		e115fCommit(t, database, ids.repo, ids.user, h2, ids.c2)
		e115fAdvanceHead(t, database, ids.repo, ids.c2, h2)
		t.Logf("E115F_H2=%s", h2)
		t.Logf("E1-15F M3 publish from %s: production SERIAL CAS H1 -> c2 -> H2", dc)
	case "m3":
		ids := e115fEnvIDs(t, "ORG", "REPO", "C2", "FS2", "BLOCK2", "H2")
		if head := e115fSerialHead(t, database, ids.org, ids.repo); head != ids.h2 {
			t.Fatalf("M3 needs HEAD = H2: %s", head)
		}
		outcomes, err := e115fSweep(t, database, ids.repo, 0)
		if len(outcomes) != 1 || outcomes[0] != "reachable/<nil>" {
			t.Fatalf("M3 sweep from %s: outcomes=%v err=%v", dc, outcomes, err)
		}
		other := w2PostHead3DCConnect(t, "dc-asia", endpoints)
		owned := v2api.PublishedBlockReferenceRepairLivenessReferrerForIntegration(ids.repo, ids.c2, ids.fs2)
		refs := e115fRefs(t, other, ids.org, ids.block2)
		if e115fRepairPresent(t, other, gocql.EachQuorum, ids.org, ids.repo, ids.c2, ids.fs2) || !refs[dbpkg.BlockReferrerForFSObject(ids.repo, ids.fs2)] || refs[owned] {
			t.Fatalf("M3: REACHABLE settlement not visible from dc-asia: refs=%v", refs)
		}
		t.Logf("E1-15F M3: c2 published and an ancestor of HEAD; sweep from %s classified natively REACHABLE (never SUPERSEDED); from dc-asia at EACH_QUORUM fs: is promoted and R2 settled", dc)
	default:
		t.Fatalf("unknown %s=%q", e115f3DCPhaseEnv, phase)
	}
	t.Logf("E115F_PHASE_DONE=%s", phase)
}
