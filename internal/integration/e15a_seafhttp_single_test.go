//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	apipkg "github.com/Sesame-Disk/sesamefs/internal/api"
	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	"github.com/Sesame-Disk/sesamefs/internal/config"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/Sesame-Disk/sesamefs/internal/middleware"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/gin-gonic/gin"
)

const e15aEvidenceEnv = "SESAMEFS_REQUIRE_E15A_SEAFHTTP_SINGLE_EVIDENCE"

var e15aEvidence = map[string]bool{}

func e15aMissing(observed map[string]bool) []string {
	var missing []string
	for _, leg := range []string{"normal", "committed", "terminal", "head-conflict"} {
		if !observed[leg] {
			missing = append(missing, leg)
		}
	}
	return missing
}

// Real HTTP HandleUpload, Cassandra token/permission lookup and physical store.
// Own up: removal controls expiry state; no elapsed TTL or candidate scan proof.
func TestE15ASeafHTTPSinglePublicationSafety(t *testing.T) {
	requireCassandra(t)
	for _, leg := range []string{"normal", "committed", "terminal", "head-conflict"} {
		t.Run(leg, func(t *testing.T) {
			database := shareProjectionDBForTest(t)
			fx := &w2CreateFileFixture{w2UploadFileFixture: newW2UploadFileFixture(t, database, newBorrowedFSHeadHandler(t, database, x1StorageClass(t)))}
			trace := e13Observer(fx.orgID, fx.blockID, "writer")
			authorityTrace := &e15aQueryObserver{e13ProofObserver: trace}
			writerDB := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], authorityTrace)
			cfg := &config.Config{Storage: config.StorageConfig{DefaultClass: x1StorageClass(t)}}
			s3 := newVerificationS3Store(t)
			manager := storage.NewManager()
			manager.SetDefaultClass(x1StorageClass(t))
			manager.RegisterBackend(x1StorageClass(t), s3, "")
			tokens := dbpkg.NewTokenStore(writerDB, time.Hour)
			token, err := tokens.CreateUploadToken(fx.orgID, fx.repoID, "/", fx.userID)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := tokens.DeleteToken(token); err != nil {
					t.Error(err)
				}
			})
			handler := apipkg.NewSeafHTTPHandler(s3, manager, writerDB, apipkg.NewCassandraTokenAdapter(tokens), cfg, middleware.NewPermissionMiddleware(writerDB))
			router := gin.New()
			handler.RegisterSeafHTTPRoutes(router)
			t.Cleanup(func() {
				for _, row := range w2Repairs(t, fx) {
					if err := v2pkg.ClearPublishedFSObjectBlockReferenceRepair(database, fx.orgID, fx.repoID, row.commitID, row.fsID); err != nil {
						t.Error(err)
					}
				}
			})
			upload := func() *httptest.ResponseRecorder {
				t.Helper()
				var body bytes.Buffer
				form := multipart.NewWriter(&body)
				if err := form.WriteField("parent_dir", "/"); err != nil {
					t.Fatal(err)
				}
				if err := form.WriteField("replace", "0"); err != nil {
					t.Fatal(err)
				}
				part, err := form.CreateFormFile("file", fx.filename)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := part.Write(fx.content); err != nil {
					t.Fatal(err)
				}
				if err := form.Close(); err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest(http.MethodPost, "/seafhttp/upload-api/"+token, &body)
				req.Header.Set("Content-Type", form.FormDataContentType())
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				return rec
			}
			assertPublished := func(rec *httptest.ResponseRecorder) {
				t.Helper()
				if rec.Code != http.StatusOK || rec.Body.String() != fx.sha1ID {
					t.Fatalf("upload response: %d %s", rec.Code, rec.Body.String())
				}
				fx.assertHeadAdvanced(t)
				refs, err := database.ListBlockReferrers(fx.orgID, fx.blockID)
				if err != nil {
					t.Fatal(err)
				}
				var file string
				for _, ref := range refs {
					if strings.HasPrefix(ref, "fs:"+fx.repoID+":") {
						file = strings.TrimPrefix(ref, "fs:"+fx.repoID+":")
					}
					if strings.HasPrefix(ref, "pub:") {
						t.Fatalf("settled upload retained pub: %v", refs)
					}
				}
				if file == "" {
					t.Fatalf("upload lacks permanent fs: %v", refs)
				}
				w24AssertHeadReaches(t, fx, borrowedFSReadHead(t, database, fx.orgID, fx.repoID), file)
				current := fx.readTarget(t)
				data, err := newVerificationBlockStore(t, fx.orgID).GetBlockByStorageKey(context.Background(), current.StorageKey)
				if err != nil || !bytes.Equal(data, fx.content) {
					t.Fatalf("published bytes: %v", err)
				}
				if rows := w2Repairs(t, fx); len(rows) != 0 {
					t.Fatalf("unsettled repairs: %+v", rows)
				}
				e15aAssertNoPendingOwner(t, fx, file)
			}
			var attempt gcpkg.BlockDeleteAuthority
			visited := 0
			restore := apipkg.SetSeafHTTPSingleAfterMaterializedBarrierForTest(fx.repoID, func(block string, location dbpkg.BlockPhysicalLocation, operation string) {
				visited++
				fx.target = fx.readTarget(t)
				if block != fx.blockID || location.StorageClass != fx.target.StorageClass || location.StorageKey != fx.target.StorageKey || operation == "" {
					t.Fatalf("captured P mismatch: block=%s P=%+v op=%s", block, location, operation)
				}
				refs, err := database.ListBlockReferrers(fx.orgID, fx.blockID)
				if err != nil || len(refs) != 1 || refs[0] != "up:"+operation {
					t.Fatalf("materialized own up: refs=%v err=%v", refs, err)
				}
				fx.assertHeadUnchanged(t)
				if rows := w2Repairs(t, fx); len(rows) != 0 {
					t.Fatalf("repair before publication: %+v", rows)
				}
				if leg == "normal" || leg == "head-conflict" {
					return
				}
				if err := database.RemoveBlockReference(fx.orgID, fx.blockID, "up:"+operation); err != nil {
					t.Fatal(err)
				}
				store := gcpkg.NewCassandraStore(database)
				attempt = x1Attempt(fx.target, "e15a-"+leg)
				x1ClaimAcquired(t, store, fx.orgUUID, fx.blockID, attempt)
				live, err := store.BlockPublicationLivenessGlobal(fx.orgUUID, fx.blockID)
				if err != nil || live != dbpkg.BlockPublicationZero {
					t.Fatalf("pre-D proof not zero: %v %v", live, err)
				}
				prepared := store.PrepareBlockDeleteOrphan(fx.orgUUID, fx.blockID, attempt, fx.sha1ID, time.Now().UTC())
				if prepared.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
					t.Fatalf("prepare orphan: %s %v", prepared.Outcome, prepared.Cause)
				}
				handoff, err := store.CommitBlockDeleteOrphanHandoff(fx.orgUUID, fx.blockID, attempt)
				if err != nil || handoff.Outcome != gcpkg.BlockDeleteHandoffCommitted {
					t.Fatalf("commit handoff: %s %v", handoff.Outcome, err)
				}
				if !handoff.Authority.IsZero() {
					attempt = handoff.Authority.Authority()
				}
				fx.assertDUnrevoked(t, attempt)
				committed := gcpkg.CommittedBlockDeleteAuthorityForTest(attempt)
				promotion := store.PromoteBlockDeleteOrphan(fx.orgUUID, fx.blockID, committed)
				if promotion.Outcome != gcpkg.StartBlockDeleteOrphanSameAuthority && promotion.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
					t.Fatalf("promote orphan: %s %v", promotion.Outcome, promotion.Cause)
				}
				orphan, found, err := store.GetS3OrphanExact(fx.orgUUID, fx.blockID, attempt)
				if err != nil || !found || orphan.RecoveryState != gcpkg.S3OrphanRecoveryStateCommitted || orphan.StorageKey != fx.target.StorageKey {
					t.Fatalf("COMMITTED orphan: %+v found=%v err=%v", orphan, found, err)
				}
				if _, found, err := store.GetS3OrphanRecoveryRootExact(fx.orgUUID, fx.blockID, attempt); err != nil || !found {
					t.Fatalf("COMMITTED root missing: %v %v", found, err)
				}
				if leg == "committed" {
					if exists, err := newVerificationBlockStore(t, fx.orgID).ObjectExists(t.Context(), fx.target.StorageKey); err != nil || !exists {
						t.Fatalf("COMMITTED K1 missing: %v %v", exists, err)
					}
					return
				}
				finalized, err := store.FinalizeBlockDelete(fx.orgUUID, fx.blockID, committed)
				if err != nil || finalized.Outcome != gcpkg.BlockDeleteFinalized {
					t.Fatalf("finalize: %s %v", finalized.Outcome, err)
				}
				w2AssertCommittedContinuation(t, store, fx.orgUUID, fx.blockID, fx.target.StorageClass, fx.target.StorageKey, newVerificationBlockStore(t, fx.orgID))
			})
			t.Cleanup(restore)
			beforeHeadVisits := 0
			competingHead := ""
			restoreHead := apipkg.SetSeafHTTPSingleBeforeHeadBarrierForTest(fx.repoID, func(block string, location dbpkg.BlockPhysicalLocation) {
				beforeHeadVisits++
				if block != fx.blockID || location.StorageClass != fx.target.StorageClass || location.StorageKey != fx.target.StorageKey {
					t.Fatalf("retry recaptured original P: block=%s location=%+v original=%+v", block, location, fx.target)
				}
				authorityTrace.mu.Lock()
				reads := authorityTrace.authorityReads
				authorityTrace.mu.Unlock()
				if leg == "head-conflict" && reads != beforeHeadVisits {
					t.Fatalf("each HEAD attempt needs a real authority read: reads=%d visits=%d", reads, beforeHeadVisits)
				}
				if rows := w2Repairs(t, fx); len(rows) != 1 {
					t.Fatalf("exact-P validated without owned repair: %+v", rows)
				}
				if leg == "head-conflict" && beforeHeadVisits == 1 {
					// A real productive empty-file writer wins HEAD; do not fake CAS failure.
					competitor := *fx
					uploadCopy := *fx.w2UploadFileFixture
					inner := *uploadCopy.borrowedFSHeadFixture
					inner.filename = "e15a-competing-empty.txt"
					uploadCopy.borrowedFSHeadFixture = &inner
					competitor.w2UploadFileFixture = &uploadCopy
					rec := competitor.create(t)
					if rec.Code != http.StatusCreated {
						t.Fatalf("competing CreateFile: %d %s", rec.Code, rec.Body.String())
					}
					competingHead = borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
					if competingHead == fx.headBefore {
						t.Fatal("competing writer did not change HEAD")
					}
				}
			})
			t.Cleanup(restoreHead)
			rec := upload()
			restore()
			restoreHead()
			if visited != 1 {
				t.Fatalf("materialization visits=%d", visited)
			}
			trace.mu.Lock()
			repairWrites := trace.repairWrites
			events := append([]string(nil), trace.settlementWrites...)
			queryErrors := append([]string(nil), trace.queryErrors...)
			trace.mu.Unlock()
			authorityTrace.mu.Lock()
			commits := append([]string(nil), authorityTrace.attemptCommits...)
			authorityTrace.mu.Unlock()
			expectedAttempts := 1
			if leg == "head-conflict" {
				expectedAttempts = 2
			}
			if len(queryErrors) != 0 || repairWrites != expectedAttempts {
				t.Fatalf("productive repair INSERTs=%d errors=%v", repairWrites, queryErrors)
			}
			if leg == "normal" || leg == "head-conflict" {
				assertPublished(rec)
				if fx.readTarget(t) != fx.target {
					t.Fatal("normal changed original P")
				}
				expectedEvents := "fs:,repair-delete"
				if leg == "head-conflict" {
					expectedEvents = "repair-delete,fs:,repair-delete"
				}
				if strings.Join(events, ",") != expectedEvents {
					t.Fatalf("settlement acknowledgement order: %v", events)
				}
				if beforeHeadVisits != expectedAttempts {
					t.Fatalf("HEAD visits=%d expected=%d", beforeHeadVisits, expectedAttempts)
				}
				if leg == "head-conflict" {
					var parent string
					head := borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
					if err := database.Session().Query(`SELECT parent_id FROM commits WHERE library_id = ? AND commit_id = ?`, fx.repoID, head).Scan(&parent); err != nil || parent != competingHead {
						t.Fatalf("retry did not use new HEAD snapshot: parent=%s competitor=%s err=%v", parent, competingHead, err)
					}
				}
				e12AssertNoDeleteLifecycle(t, fx)
			} else {
				head := borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
				if rec.Code != http.StatusConflict || head != fx.headBefore || fx.hasOwnFSReferrer(t) {
					t.Fatalf("E1-5a VIOLATION: upload published retired P1: leg=%s status=%d HEAD=%s before=%s fs=%v P1=%+v events=%v", leg, rec.Code, head, fx.headBefore, fx.hasOwnFSReferrer(t), fx.target, events)
				}
				for _, commit := range commits {
					var parent string
					if err := database.Session().Query(`SELECT parent_id FROM commits WHERE library_id = ? AND commit_id = ?`, fx.repoID, commit).Scan(&parent); err != gocql.ErrNotFound {
						t.Fatalf("rejected commit survived: commit=%s err=%v", commit, err)
					}
				}
				if beforeHeadVisits != 0 {
					t.Fatalf("rejected P reached HEAD boundary %d times", beforeHeadVisits)
				}
				fx.assertPubCount(t, 0, "rejected upload staged references")
				if rows := w2Repairs(t, fx); len(rows) != 0 {
					t.Fatalf("rejected upload retained repair: %+v", rows)
				}
				e15aAssertNoOwnPendingOwners(t, fx)
				if leg == "committed" {
					e14AssertDNotRevoked(t, fx, attempt)
				} else {
					x1AssertCanonicalAbsent(t, gcpkg.NewCassandraStore(database), fx.orgUUID, fx.blockID)
					if exists, err := newVerificationBlockStore(t, fx.orgID).ObjectExists(t.Context(), fx.target.StorageKey); err != nil || exists {
						t.Fatalf("K1 after rejected upload: %v %v", exists, err)
					}
				}
			}
			replay := upload()
			if leg == "committed" && replay.Code != http.StatusOK {
				if replay.Code != http.StatusConflict {
					t.Fatalf("committed replay response: %d %s", replay.Code, replay.Body.String())
				}
				fx.assertHeadUnchanged(t)
				fx.assertPubCount(t, 0, "rejected replay references")
				if rows := w2Repairs(t, fx); len(rows) != 0 {
					t.Fatalf("rejected replay retained repair: %+v", rows)
				}
			} else {
				assertPublished(replay)
				if leg != "normal" && leg != "head-conflict" {
					e14AssertTerminalP1AndNewP2(t, fx, attempt)
				} else if fx.readTarget(t) != fx.target {
					t.Fatal("normal replay changed P1")
				}
			}
			e15aEvidence[leg] = true
			t.Logf("leg=%s response=%d replay=%d repair INSERTs=%d ack=%v P1=%+v", leg, rec.Code, replay.Code, repairWrites, events, fx.target)
		})
	}
}

func e15aFileFSID(t *testing.T, fx *w2CreateFileFixture) string {
	t.Helper()
	content, err := json.Marshal(map[string]any{"version": 1, "type": 1, "block_ids": []string{fx.sha1ID}, "size": int64(len(fx.content))})
	if err != nil {
		t.Fatal(err)
	}
	return sha1hex(content)
}
func e15aAssertNoPendingOwner(t *testing.T, fx *w2CreateFileFixture, fsID string) {
	t.Helper()
	if exists, err := fx.database.PendingPublishedFSObjectOwnerExists(fx.repoID, fsID); err != nil || exists {
		t.Fatalf("pending owner survived: file=%s exists=%v err=%v", fsID, exists, err)
	}
}
func e15aAssertNoOwnPendingOwners(t *testing.T, fx *w2CreateFileFixture) {
	t.Helper()
	e15aAssertNoPendingOwner(t, fx, e15aFileFSID(t, fx))
}

// Query acknowledgements supplement the scheduling hooks: every pre-HEAD read
// is the productive authority SELECT, after real durable repair acquisition.
type e15aQueryObserver struct {
	*e13ProofObserver
	mu             sync.Mutex
	authorityReads int
	attemptCommits []string
}

func (o *e15aQueryObserver) ObserveQuery(ctx context.Context, q gocql.ObservedQuery) {
	o.e13ProofObserver.ObserveQuery(ctx, q)
	statement := strings.ToLower(strings.Join(strings.Fields(q.Statement), " "))
	if strings.HasPrefix(statement, "insert into published_block_reference_repairs ") && len(q.Values) >= 6 && fmt.Sprint(q.Values[1]) == o.org && q.Err == nil {
		o.mu.Lock()
		o.attemptCommits = append(o.attemptCommits, fmt.Sprint(q.Values[3]))
		o.mu.Unlock()
	}
	o.e13ProofObserver.mu.Lock()
	repaired := o.repairWrites > 0
	o.e13ProofObserver.mu.Unlock()
	if repaired && strings.HasPrefix(statement, "select representation_id, sha1, size_bytes, storage_class, storage_key, gc_state, gc_claim_id, gc_claimed_at, created_at from blocks ") && len(q.Values) == 2 && fmt.Sprint(q.Values[0]) == o.org && fmt.Sprint(q.Values[1]) == o.block && q.Query.GetConsistency() == gocql.LocalQuorum && q.Err == nil {
		o.mu.Lock()
		o.authorityReads++
		o.mu.Unlock()
	}
}
