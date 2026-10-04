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

const e15bEvidenceEnv = "SESAMEFS_REQUIRE_E15B_SEAFHTTP_STREAMING_EVIDENCE"

var e15bEvidence = map[string]bool{}

func e15bMissing(observed map[string]bool) []string {
	var missing []string
	for _, leg := range []string{"normal", "committed", "terminal", "terminal-retry", "head-conflict"} {
		if !observed[leg] {
			missing = append(missing, leg)
		}
	}
	return missing
}

// Actual chunked HandleUpload + token/permissions + Cassandra + SILO. Only the
// victim's owned up: is removed, an explicit expiry-state scheduling control.
func TestE15BSeafHTTPStreamingPublicationSafety(t *testing.T) {
	requireCassandra(t)
	for _, leg := range []string{"normal", "committed", "terminal", "head-conflict"} {
		t.Run(leg, func(t *testing.T) {
			database := shareProjectionDBForTest(t)
			fx := &w2CreateFileFixture{w2UploadFileFixture: newW2UploadFileFixture(t, database, newBorrowedFSHeadHandler(t, database, x1StorageClass(t)))}
			seed := fx.blockID
			fx.content = bytes.Repeat([]byte(seed), 8*1024*1024/len(seed))
			fx.blockID, fx.sha1ID = sha256hex(fx.content), sha1hex(fx.content)
			inner := *fx.borrowedFSHeadFixture
			inner.content = []byte("e15b-second-block-" + seed)
			inner.blockID, inner.sha1ID = sha256hex(inner.content), sha1hex(inner.content)
			other := &w2CreateFileFixture{w2UploadFileFixture: &w2UploadFileFixture{borrowedFSHeadFixture: &inner}}
			blocks := []*w2CreateFileFixture{fx, other}
			content := append(append([]byte(nil), fx.content...), other.content...)
			fileID := sha1hex(content)
			fileJSON, _ := json.Marshal(map[string]any{"version": 1, "type": 1, "block_ids": []string{fx.sha1ID, other.sha1ID}, "size": int64(len(content))})
			fileFSID := sha1hex(fileJSON)
			observers := &e15bObserver{}
			for _, b := range blocks {
				x1Cleanup(t, database, b.orgUUID, b.blockID)
				cleanupUploadedBlockArtifactsForTest(t, b.orgID, b.repoID, b.blockID, b.sha1ID)
				observers.blocks = append(observers.blocks, &e15aQueryObserver{e13ProofObserver: e13Observer(b.orgID, b.blockID, "writer")})
			}
			writerDB := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], observers)
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
				for _, row := range w2Repairs(t, fx) {
					if err := v2pkg.ClearPublishedFSObjectBlockReferenceRepair(database, fx.orgID, fx.repoID, row.commitID, row.fsID); err != nil {
						t.Error(err)
					}
				}
			})
			handler := apipkg.NewSeafHTTPHandler(s3, manager, writerDB, apipkg.NewCassandraTokenAdapter(tokens), cfg, middleware.NewPermissionMiddleware(writerDB))
			router := gin.New()
			handler.RegisterSeafHTTPRoutes(router)
			upload := func() *httptest.ResponseRecorder {
				t.Helper()
				var body bytes.Buffer
				form := multipart.NewWriter(&body)
				for key, value := range map[string]string{"parent_dir": "/", "replace": "0", "resumableIdentifier": "e15b-" + seed} {
					if err := form.WriteField(key, value); err != nil {
						t.Fatal(err)
					}
				}
				part, err := form.CreateFormFile("file", fx.filename)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := part.Write(content); err != nil {
					t.Fatal(err)
				}
				if err := form.Close(); err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest(http.MethodPost, "/seafhttp/upload-api/"+token, &body)
				req.Header.Set("Content-Type", form.FormDataContentType())
				req.Header.Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(content)-1, len(content)))
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				return rec
			}
			assertPublished := func(rec *httptest.ResponseRecorder) {
				t.Helper()
				if rec.Code != http.StatusOK || rec.Body.String() != fileID {
					t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
				}
				fx.assertHeadAdvanced(t)
				var ids, external []string
				if err := database.Session().Query(`SELECT block_ids, seafile_block_ids_sha1 FROM fs_objects WHERE library_id = ? AND fs_id = ?`, fx.repoID, fileFSID).Scan(&ids, &external); err != nil || strings.Join(ids, ",") != fx.blockID+","+other.blockID || strings.Join(external, ",") != fx.sha1ID+","+other.sha1ID {
					t.Fatalf("published paired layout: internal=%v external=%v err=%v", ids, external, err)
				}
				w24AssertHeadReaches(t, fx, borrowedFSReadHead(t, database, fx.orgID, fx.repoID), fileFSID)
				for _, b := range blocks {
					refs, err := database.ListBlockReferrers(b.orgID, b.blockID)
					if err != nil {
						t.Fatal(err)
					}
					found := false
					for _, ref := range refs {
						if ref == "fs:"+b.repoID+":"+fileFSID {
							found = true
						}
						if strings.HasPrefix(ref, "pub:") {
							t.Fatalf("unsettled pub: %v", refs)
						}
					}
					if !found {
						t.Fatalf("missing exact file fs: %v", refs)
					}
					current := b.readTarget(t)
					data, err := newVerificationBlockStore(t, b.orgID).GetBlockByStorageKey(context.Background(), current.StorageKey)
					if err != nil || !bytes.Equal(data, b.content) {
						t.Fatalf("published bytes: %v", err)
					}
				}
				if rows := w2Repairs(t, fx); len(rows) != 0 {
					t.Fatalf("unsettled repairs: %+v", rows)
				}
				e15aAssertNoPendingOwner(t, fx, fileFSID)
			}
			var attempt gcpkg.BlockDeleteAuthority
			var hookMu sync.Mutex
			captures := map[int]int{}
			locations := map[int]dbpkg.BlockPhysicalLocation{}
			operations := map[int]string{}
			var tracker *apipkg.ChunkUpload
			materializedVisits, headVisits := 0, 0
			competingHead := ""
			restore := apipkg.SetSeafHTTPStreamingBarriersForTest(fx.repoID, func(index int, block string, p dbpkg.BlockPhysicalLocation, operation string) {
				hookMu.Lock()
				defer hookMu.Unlock()
				if index < 0 || index >= len(blocks) || block != blocks[index].blockID || operation == "" {
					t.Errorf("unexpected capture index=%d block=%s op=%s", index, block, operation)
					return
				}
				captures[index]++
				locations[index] = p
				operations[index] = operation
			}, func(current *apipkg.ChunkUpload) {
				materializedVisits++
				if tracker == nil {
					tracker = current
				} else if tracker != current {
					t.Fatal("retry replaced ChunkUpload")
				}
				if materializedVisits > 1 {
					if locations[0].StorageKey == fx.target.StorageKey || locations[1].StorageKey != other.target.StorageKey {
						t.Fatal("retry did not retain healthy original P and acquire a new victim P")
					}
					return
				}
				for i, b := range blocks {
					b.target = b.readTarget(t)
					if captures[i] != 1 || locations[i].StorageClass != b.target.StorageClass || locations[i].StorageKey != b.target.StorageKey {
						t.Fatalf("original capture mismatch index=%d P=%+v", i, locations[i])
					}
					refs, err := database.ListBlockReferrers(b.orgID, b.blockID)
					if err != nil || len(refs) != 1 || refs[0] != "up:"+operations[i] {
						t.Fatalf("own up: %v %v", refs, err)
					}
				}
				fx.assertHeadUnchanged(t)
				if rows := w2Repairs(t, fx); len(rows) != 0 {
					t.Fatalf("repair before retirement: %+v", rows)
				}
				if leg == "normal" || leg == "head-conflict" {
					return
				}
				if err := database.RemoveBlockReference(fx.orgID, fx.blockID, "up:"+operations[0]); err != nil {
					t.Fatal(err)
				}
				attempt = e15bRetire(t, fx, leg)
				refs, err := database.ListBlockReferrers(other.orgID, other.blockID)
				if err != nil || len(refs) != 1 || refs[0] != "up:"+operations[1] {
					t.Fatalf("victim retirement touched healthy refs: %v %v", refs, err)
				}
			}, func() {
				headVisits++
				if leg == "head-conflict" {
					for i, o := range observers.blocks {
						o.mu.Lock()
						reads := o.authorityReads
						o.mu.Unlock()
						if reads != headVisits {
							t.Fatalf("block %d exact-P reads=%d HEAD attempts=%d", i, reads, headVisits)
						}
					}
					if rows := w2Repairs(t, fx); len(rows) != 1 {
						t.Fatalf("HEAD without durable repair: %+v", rows)
					}
					if leg == "head-conflict" && headVisits == 1 {
						competitor := *fx
						up := *fx.w2UploadFileFixture
						in := *up.borrowedFSHeadFixture
						in.filename = "e15b-competing-empty.txt"
						up.borrowedFSHeadFixture = &in
						competitor.w2UploadFileFixture = &up
						rec := competitor.create(t)
						if rec.Code != http.StatusCreated {
							t.Fatalf("competing CreateFile: %d %s", rec.Code, rec.Body.String())
						}
						competingHead = borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
					}
				}
			})
			t.Cleanup(restore)
			rec := upload()
			if materializedVisits != 1 {
				t.Fatalf("materialized visits=%d", materializedVisits)
			}
			if leg == "normal" || leg == "head-conflict" {
				assertPublished(rec)
				for _, b := range blocks {
					if b.readTarget(t) != b.target {
						t.Fatal("live original placement changed")
					}
				}
				expected := 1
				if leg == "head-conflict" {
					expected = 2
					var parent string
					head := borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
					if err := database.Session().Query(`SELECT parent_id FROM commits WHERE library_id = ? AND commit_id = ?`, fx.repoID, head).Scan(&parent); err != nil || parent != competingHead {
						t.Fatalf("retry parent=%s competitor=%s err=%v", parent, competingHead, err)
					}
				}
				if headVisits != expected {
					t.Fatalf("HEAD visits=%d want=%d", headVisits, expected)
				}
			} else {
				head := borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
				if rec.Code != http.StatusConflict || head != fx.headBefore || fx.hasOwnFSReferrer(t) || other.hasOwnFSReferrer(t) {
					t.Fatalf("E1-5b VIOLATION: retired victim published leg=%s status=%d HEAD=%s before=%s fs=%v/%v", leg, rec.Code, head, fx.headBefore, fx.hasOwnFSReferrer(t), other.hasOwnFSReferrer(t))
				}
				if headVisits != 0 {
					t.Fatalf("rejected P reached HEAD: %d", headVisits)
				}
				for _, b := range blocks {
					b.assertPubCount(t, 0, "rejected attempt")
				}
				if rows := w2Repairs(t, fx); len(rows) != 0 {
					t.Fatalf("repair survived rejection: %+v", rows)
				}
				e15aAssertNoPendingOwner(t, fx, fileFSID)
				o := observers.blocks[0]
				o.mu.Lock()
				commits := append([]string(nil), o.attemptCommits...)
				o.mu.Unlock()
				for _, commit := range commits {
					var parent string
					if err := database.Session().Query(`SELECT parent_id FROM commits WHERE library_id = ? AND commit_id = ?`, fx.repoID, commit).Scan(&parent); err != gocql.ErrNotFound {
						t.Fatalf("rejected commit survived: %s %v", commit, err)
					}
				}
				for _, o := range observers.blocks {
					o.mu.Lock()
					reads := o.authorityReads
					o.mu.Unlock()
					if reads != 1 {
						t.Fatalf("rejection did not check every placement: reads=%d", reads)
					}
				}
				if leg == "committed" {
					e14AssertDNotRevoked(t, fx, attempt)
				} else {
					x1AssertCanonicalAbsent(t, gcpkg.NewCassandraStore(database), fx.orgUUID, fx.blockID)
					if exists, err := newVerificationBlockStore(t, fx.orgID).ObjectExists(t.Context(), fx.target.StorageKey); err != nil || exists {
						t.Fatalf("TERMINAL K1 survived rejection: %v %v", exists, err)
					}
					replay := upload()
					assertPublished(replay)
					if materializedVisits != 2 || captures[0] != 2 || captures[1] != 1 {
						t.Fatalf("retry did not rematerialize only victim: visits=%d captures=%v", materializedVisits, captures)
					}
					if operations[0] != operations[1] {
						t.Fatal("retry changed operation identity")
					}
					e14AssertTerminalP1AndNewP2(t, fx, attempt)
					if other.readTarget(t) != other.target {
						t.Fatal("retry changed healthy P")
					}
					e15bEvidence["terminal-retry"] = true
				}
			}
			for _, o := range observers.blocks {
				o.e13ProofObserver.mu.Lock()
				errs := append([]string(nil), o.queryErrors...)
				writes := o.repairWrites
				o.e13ProofObserver.mu.Unlock()
				expectedWrites := 1
				expectedEvents := "fs:,repair-delete"
				if leg == "head-conflict" || leg == "terminal" {
					expectedWrites = 2
					expectedEvents = "repair-delete,fs:,repair-delete"
				}
				if leg == "committed" {
					expectedEvents = "repair-delete"
				}
				o.e13ProofObserver.mu.Lock()
				events := append([]string(nil), o.settlementWrites...)
				o.e13ProofObserver.mu.Unlock()
				if strings.Join(events, ",") != expectedEvents {
					t.Fatalf("settlement ack order: %v want=%s", events, expectedEvents)
				}
				if len(errs) != 0 || writes != expectedWrites {
					t.Fatalf("productive repair trace writes=%d errors=%v", writes, errs)
				}
			}
			e15bEvidence[leg] = true
			t.Logf("leg=%s status=%d captures=%v materialized=%d HEAD attempts=%d P1=%+v other=%+v", leg, rec.Code, captures, materializedVisits, headVisits, fx.target, other.target)
		})
	}
}

type e15bObserver struct{ blocks []*e15aQueryObserver }

func (o *e15bObserver) ObserveQuery(ctx context.Context, q gocql.ObservedQuery) {
	for _, b := range o.blocks {
		b.ObserveQuery(ctx, q)
	}
}

func e15bRetire(t *testing.T, fx *w2CreateFileFixture, leg string) gcpkg.BlockDeleteAuthority {
	t.Helper()
	database := fx.database
	store := gcpkg.NewCassandraStore(database)
	attempt := x1Attempt(fx.target, "e15b-"+leg)
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
		return attempt
	}
	finalized, err := store.FinalizeBlockDelete(fx.orgUUID, fx.blockID, committed)
	if err != nil || finalized.Outcome != gcpkg.BlockDeleteFinalized {
		t.Fatalf("finalize: %s %v", finalized.Outcome, err)
	}
	w2AssertCommittedContinuation(t, store, fx.orgUUID, fx.blockID, fx.target.StorageClass, fx.target.StorageKey, newVerificationBlockStore(t, fx.orgID))
	return attempt
}
