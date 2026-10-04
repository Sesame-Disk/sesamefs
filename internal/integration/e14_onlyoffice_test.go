//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	"github.com/Sesame-Disk/sesamefs/internal/config"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const e14EvidenceEnv = "SESAMEFS_REQUIRE_E14_ONLYOFFICE_EVIDENCE"

var e14Evidence = map[string]bool{}

func e14Missing(observed map[string]bool) []string {
	var missing []string
	for _, leg := range []string{"normal", "committed", "terminal"} {
		if !observed[leg] {
			missing = append(missing, leg)
		}
	}
	return missing
}

// The HTTP source models the document server, not a live OnlyOffice editor.
// JWT, permission lookup, download, materialization and callback publication are
// productive code. Own up: removal is expiry-state control, not elapsed TTL.
func TestE14OnlyOfficeCallbackPublicationSafety(t *testing.T) {
	requireCassandra(t)
	for _, leg := range []string{"normal", "committed", "terminal"} {
		t.Run(leg, func(t *testing.T) {
			database := shareProjectionDBForTest(t)
			upload := newW2UploadFileFixture(t, database, newBorrowedFSHeadHandler(t, database, x1StorageClass(t)))
			fx := &w2CreateFileFixture{w2UploadFileFixture: upload}
			trace := e13Observer(fx.orgID, fx.blockID, "writer")
			writerDB := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], trace)
			var downloads atomic.Int32
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				downloads.Add(1)
				_, _ = w.Write(fx.content)
			}))
			t.Cleanup(source.Close)
			cfg := &config.Config{Storage: config.StorageConfig{DefaultClass: x1StorageClass(t)}, OnlyOffice: config.OnlyOfficeConfig{Enabled: true, MaxDocumentBytes: 1048576, JWTSecret: "e14-isolated-test-secret", InternalURL: source.URL, APIJSURL: source.URL + "/web-apps/apps/api/documents/api.js"}}
			s3 := newVerificationS3Store(t)
			manager := storage.NewManager()
			manager.SetDefaultClass(x1StorageClass(t))
			manager.RegisterBackend(x1StorageClass(t), s3, "")
			router := gin.New()
			v2pkg.RegisterOnlyOfficeCallbackRoutes(router.Group("/onlyoffice"), writerDB, cfg, s3, manager, "")
			docKey := "e14-" + uuid.NewString()
			if err := database.Session().Query(`INSERT INTO onlyoffice_doc_keys (doc_key,user_id,repo_id,file_path,created_at) VALUES (?,?,?,?,?)`, docKey, fx.userID, fx.repoID, "/"+fx.filename, time.Now()).Exec(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = database.Session().Query(`DELETE FROM onlyoffice_doc_keys WHERE doc_key = ?`, docKey).Exec()
				iter := database.Session().Query(`SELECT operation_id FROM onlyoffice_pending_blocks WHERE org_id = ?`, fx.orgID).Iter()
				var operation string
				var own []string
				for iter.Scan(&operation) {
					own = append(own, operation)
				}
				if err := iter.Close(); err != nil {
					t.Error(err)
					return
				}
				// Only rows for this fixture library may be cleared.
				for _, operation := range own {
					var repo string
					if err := database.Session().Query(`SELECT repo_id FROM onlyoffice_pending_blocks WHERE org_id = ? AND operation_id = ?`, fx.orgID, operation).Scan(&repo); err == nil && repo == fx.repoID {
						_ = database.Session().Query(`DELETE FROM onlyoffice_pending_blocks WHERE org_id = ? AND operation_id = ?`, fx.orgID, operation).Exec()
					}
				}
				for _, r := range w2Repairs(t, fx) {
					_ = v2pkg.ClearPublishedFSObjectBlockReferenceRepair(database, fx.orgID, fx.repoID, r.commitID, r.fsID)
				}
			})
			callback := func(status int) int {
				t.Helper()
				payload := map[string]any{"status": status, "key": docKey, "url": source.URL + "/edited.docx"}
				token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"payload": payload}).SignedString([]byte(cfg.OnlyOffice.JWTSecret))
				if err != nil {
					t.Fatal(err)
				}
				body, err := json.Marshal(map[string]string{"token": token})
				if err != nil {
					t.Fatal(err)
				}
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/onlyoffice/editor-callback/", bytes.NewReader(body)))
				var result struct {
					Error int `json:"error"`
				}
				if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &result) != nil || !strings.Contains(rec.Body.String(), `"error"`) {
					t.Fatalf("callback response: %d %s", rec.Code, rec.Body.String())
				}
				return result.Error
			}
			assertMapping := func(want bool) {
				t.Helper()
				var repo string
				err := database.Session().Query(`SELECT repo_id FROM onlyoffice_doc_keys WHERE doc_key = ?`, docKey).Scan(&repo)
				if want {
					if err != nil || repo != fx.repoID {
						t.Fatalf("doc key missing/wrong: %s %v", repo, err)
					}
				} else if err != gocql.ErrNotFound {
					t.Fatalf("closed doc key survived: %s %v", repo, err)
				}
			}
			assertPendingAbsent := func() {
				t.Helper()
				iter := database.Session().Query(`SELECT repo_id FROM onlyoffice_pending_blocks WHERE org_id = ?`, fx.orgID).Iter()
				var repo string
				own := 0
				for iter.Scan(&repo) {
					if repo == fx.repoID {
						own++
					}
				}
				if err := iter.Close(); err != nil || own != 0 {
					t.Fatalf("pending cleanup rows=%d err=%v", own, err)
				}
			}
			assertPublished := func() {
				t.Helper()
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
						t.Fatalf("settled callback retained pub: %v", refs)
					}
				}
				if file == "" {
					t.Fatalf("callback lacks exact permanent fs: %v", refs)
				}
				w24AssertHeadReaches(t, fx, borrowedFSReadHead(t, database, fx.orgID, fx.repoID), file)
				current := fx.readTarget(t)
				data, err := newVerificationBlockStore(t, fx.orgID).GetBlockByStorageKey(context.Background(), current.StorageKey)
				if err != nil || !bytes.Equal(data, fx.content) {
					t.Fatalf("published exact bytes: %v", err)
				}
				if rows := w2Repairs(t, fx); len(rows) != 0 {
					t.Fatalf("unsettled callback repairs: %+v", rows)
				}
				assertPendingAbsent()
			}
			var attempt gcpkg.BlockDeleteAuthority
			visited := 0
			restore := v2pkg.SetOnlyOfficeAfterMaterializedBarrierForTest(fx.repoID, func() {
				visited++
				fx.target = fx.readTarget(t)
				refs, err := database.ListBlockReferrers(fx.orgID, fx.blockID)
				if err != nil || len(refs) != 1 || !strings.HasPrefix(refs[0], "up:") {
					t.Fatalf("materialized callback must own exactly up: refs=%v err=%v", refs, err)
				}
				if rows := w2Repairs(t, fx); len(rows) != 0 {
					t.Fatalf("repair exists before materialized boundary: %+v", rows)
				}
				var pendingRepo, pendingBlock, pendingClass string
				if err := database.Session().Query(`SELECT repo_id, internal_block_id, storage_class FROM onlyoffice_pending_blocks WHERE org_id = ? AND operation_id = ?`, fx.orgID, strings.TrimPrefix(refs[0], "up:")).Scan(&pendingRepo, &pendingBlock, &pendingClass); err != nil || pendingRepo != fx.repoID || pendingBlock != fx.blockID || pendingClass != fx.target.StorageClass {
					t.Fatalf("real pending operation mismatch: repo=%s block=%s class=%s err=%v", pendingRepo, pendingBlock, pendingClass, err)
				}
				if leg == "normal" {
					return
				}
				fx.dropOwnUploadRefs(t)
				store := gcpkg.NewCassandraStore(database)
				attempt = x1Attempt(fx.target, "e14-"+leg)
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
			response := callback(6)
			restore() // Replays execute without the forced interleaving.
			if visited != 1 || downloads.Load() != 1 {
				t.Fatalf("real materialization/download missing: visited=%d downloads=%d", visited, downloads.Load())
			}
			trace.mu.Lock()
			repairWrites := trace.repairWrites
			events := append([]string(nil), trace.settlementWrites...)
			queryErrors := append([]string(nil), trace.queryErrors...)
			trace.mu.Unlock()
			if len(queryErrors) != 0 {
				t.Fatalf("publication query errors: %v", queryErrors)
			}
			if repairWrites != 1 {
				t.Fatalf("callback did not acquire actual durable repair: %d", repairWrites)
			}
			assertMapping(true) // Force-save keeps the key, including definite rejection.
			if leg == "normal" {
				if strings.Join(events, ",") != "fs:,repair-delete" {
					t.Fatalf("settlement acknowledgement order: %v", events)
				}
				if response != 0 {
					t.Fatalf("normal callback error=%d", response)
				}
				assertPublished()
				if fx.readTarget(t) != fx.target {
					t.Fatal("normal save changed P1")
				}
				e12AssertNoDeleteLifecycle(t, fx)
			} else {
				head := borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
				if response != 1 || head != fx.headBefore || fx.hasOwnFSReferrer(t) {
					t.Fatalf("E1-4 VIOLATION: callback published retired P1: leg=%s error=%d HEAD=%s before=%s fs=%v P1=%+v events=%v", leg, response, head, fx.headBefore, fx.hasOwnFSReferrer(t), fx.target, events)
				}
				fx.assertPubCount(t, 0, "rejected callback must clean staged pub:")
				if rows := w2Repairs(t, fx); len(rows) != 0 {
					t.Fatalf("rejected callback retained repair: %+v", rows)
				}
				assertPendingAbsent()
				if leg == "committed" {
					fx.assertDUnrevoked(t, attempt)
					replayError := callback(6)
					switch replayError {
					case 1:
						fx.assertHeadUnchanged(t)
					case 0:
						// The real background recovery worker may finish D1 during the
						// materialization retry. Success is valid only at proven new P2.
						assertPublished()
						e14AssertTerminalP1AndNewP2(t, fx, attempt)
					default:
						t.Fatalf("unexpected replay error=%d", replayError)
					}
					assertMapping(true)
					assertPendingAbsent()
					e14Evidence[leg] = true
					return
				}
				x1AssertCanonicalAbsent(t, gcpkg.NewCassandraStore(database), fx.orgUUID, fx.blockID)
				if exists, err := newVerificationBlockStore(t, fx.orgID).ObjectExists(t.Context(), fx.target.StorageKey); err != nil || exists {
					t.Fatalf("K1 reappeared after rejected callback: %v %v", exists, err)
				}
			}
			if callback(2) != 0 {
				t.Fatal("close/replay callback should publish live placement")
			}
			assertPublished()
			if leg == "terminal" {
				e14AssertTerminalP1AndNewP2(t, fx, attempt)
				p2 := fx.readTarget(t)
				if p2 == fx.target {
					t.Fatal("terminal replay resurrected P1 instead of P2")
				}
				if exists, err := newVerificationBlockStore(t, fx.orgID).ObjectExists(t.Context(), fx.target.StorageKey); err != nil || exists {
					t.Fatalf("P2 replay revived K1: %v %v", exists, err)
				}
				t.Logf("terminal replay: P1=%+v P2=%+v", fx.target, p2)
			}
			assertMapping(false)
			head := borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
			count := downloads.Load()
			if callback(2) != 1 || borrowedFSReadHead(t, database, fx.orgID, fx.repoID) != head || downloads.Load() != count {
				t.Fatal("closed-key replay unexpectedly changed HEAD/downloaded content")
			}
			e14Evidence[leg] = true
			t.Logf("leg=%s callback=%d repair INSERTs=%d ack events=%v P1=%+v", leg, response, repairWrites, events, fx.target)
		})
	}
}

// This certificate is checked against old D1 even when canonical L now holds
// P2. It must not use canonical absence as proof after a legitimate replay.
func e14AssertTerminalP1AndNewP2(t *testing.T, fx *w2CreateFileFixture, attempt gcpkg.BlockDeleteAuthority) {
	t.Helper()
	p2 := fx.readTarget(t)
	if p2.StorageKey == fx.target.StorageKey {
		t.Fatal("successful replay reused retired K1")
	}
	var phase, class, key string
	var claimedAt time.Time
	err := fx.database.Session().Query(`SELECT phase, storage_class, storage_key, claimed_at FROM gc_block_delete_lifecycles WHERE org_id = ? AND block_id = ? AND claim_id = ?`, fx.orgID, fx.blockID, attempt.ClaimID).Consistency(gocql.EachQuorum).Scan(&phase, &class, &key, &claimedAt)
	if err != nil || phase != gcpkg.BlockDeleteLifecyclePhaseTerminal || class != fx.target.StorageClass || key != fx.target.StorageKey || !claimedAt.Equal(attempt.ClaimedAt) {
		t.Fatalf("old D1 not exact TERMINAL certificate: phase=%s class=%s key=%s claimedAt=%s err=%v", phase, class, key, claimedAt, err)
	}
	store := gcpkg.NewCassandraStore(fx.database)
	if _, found, err := store.GetS3OrphanExact(fx.orgUUID, fx.blockID, attempt); err != nil || found {
		t.Fatalf("old D1 orphan survived P2 replay: %v %v", found, err)
	}
	if _, found, err := store.GetS3OrphanRecoveryRootExact(fx.orgUUID, fx.blockID, attempt); err != nil || found {
		t.Fatalf("old D1 root survived P2 replay: %v %v", found, err)
	}
	if exists, err := newVerificationBlockStore(t, fx.orgID).ObjectExists(t.Context(), fx.target.StorageKey); err != nil || exists {
		t.Fatalf("P2 replay revived K1: %v %v", exists, err)
	}
	t.Logf("exact terminal D1=%s P1=%+v P2=%+v", attempt.ClaimID, fx.target, p2)
}
