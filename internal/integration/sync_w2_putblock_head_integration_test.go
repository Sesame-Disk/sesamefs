//go:build integration

package integration

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	apipkg "github.com/Sesame-Disk/sesamefs/internal/api"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/google/uuid"
)

// W2 Sync PutBlock -> HEAD publication continuity — real Cassandra+MinIO
// evidence for the scoped PutBlock-provenanced subset described in
// docs/R3-LIVENESS-CONTINUITY.md: "Sync `PutBlock` followed by HEAD" and
// "Sync retry from another pod within the same provisional TTL", for the
// PutBlock-provenanced subset described there. It does not claim W2/R31,
// "Sync commit whose block had no associated PutBlock", `recv-fs-before-put`,
// G1, or X1 are closed. `GC_ENABLED=false` remains required.
//
// Gate: SESAMEFS_REQUIRE_W2_SYNC_PUTBLOCK_HEAD_EVIDENCE=1. See
// sync_w2_putblock_head_evidence_test.go for the named-leg completeness gate
// wired into TestMain (internal/integration/integration_test.go).

// syncW2PutFileCommit drives the real Sync protocol end to end for one
// single-file commit (PutCommit -> RecvFS -> PutBlock) without touching
// HEAD, mirroring TestSyncRecvFSBeforePutBlockPublishesDownloadableFile's
// sequence. It returns everything a caller needs to then drive HEAD and to
// inspect the resulting canonical state.
type syncW2FileCommit struct {
	commitID        string
	rootFSID        string
	fileFSID        string
	fileName        string
	fileData        []byte
	externalBlockID string // Seafile SHA-1
	internalBlockID string // canonical SHA-256
}

func syncW2PutFileCommit(t *testing.T, client *testClient, repoID, parentHead, fileName string, fileData []byte) syncW2FileCommit {
	t.Helper()

	externalBlockID := syncSHA1HexForTest(fileData)
	internalBlockID := syncSHA256HexForTest(fileData)
	mtime := time.Now().Unix()

	fileObjectJSON := mustMarshalSyncObjectForTest(t, map[string]interface{}{
		"block_ids": []string{externalBlockID},
		"size":      int64(len(fileData)),
		"type":      1,
		"version":   1,
	})
	fileFSID := syncSHA1HexForTest(fileObjectJSON)

	rootObjectJSON := mustMarshalSyncObjectForTest(t, map[string]interface{}{
		"dirents": []apipkg.FSEntry{{
			ID:    fileFSID,
			Mode:  33188,
			Mtime: mtime,
			Name:  fileName,
			Size:  int64(len(fileData)),
		}},
		"type":    3,
		"version": 1,
	})
	rootFSID := syncSHA1HexForTest(rootObjectJSON)
	commitID := syncSHA1HexForTest([]byte(fmt.Sprintf("sync-w2-putblock-head-%s-%s-%d", repoID, fileName, time.Now().UnixNano())))

	commitPayload := map[string]interface{}{
		"commit_id":   commitID,
		"repo_id":     repoID,
		"root_id":     rootFSID,
		"parent_id":   parentHead,
		"description": "integration sync W2 putblock->head",
		"ctime":       time.Now().Unix(),
		"version":     1,
	}

	resp := syncW2DoRequest(t, client, http.MethodPut, fmt.Sprintf("/seafhttp/repo/%s/commit/%s", repoID, commitID), mustMarshalSyncObjectForTest(t, commitPayload), "application/json")
	expectStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	packedFS := packSyncFSObjectsForTest(t,
		syncPackedFSObject{fsID: fileFSID, jsonData: fileObjectJSON},
		syncPackedFSObject{fsID: rootFSID, jsonData: rootObjectJSON},
	)
	resp = syncW2DoRequest(t, client, http.MethodPost, fmt.Sprintf("/seafhttp/repo/%s/recv-fs", repoID), packedFS, "application/octet-stream")
	expectStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	resp = syncW2DoRequest(t, client, http.MethodPut, fmt.Sprintf("/seafhttp/repo/%s/block/%s", repoID, externalBlockID), fileData, "application/octet-stream")
	expectStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	return syncW2FileCommit{
		commitID:        commitID,
		rootFSID:        rootFSID,
		fileFSID:        fileFSID,
		fileName:        fileName,
		fileData:        fileData,
		externalBlockID: externalBlockID,
		internalBlockID: internalBlockID,
	}
}

// syncW2DoRequest is doSyncProtocolRequestForTest generalized to a specific
// client/node, for the cross-node leg.
func syncW2DoRequest(t *testing.T, client *testClient, method, path string, body []byte, contentType string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, client.baseURL+path, reader)
	if err != nil {
		t.Fatalf("failed to create %s %s request: %v", method, path, err)
	}
	req.Header.Set("Authorization", "Token "+client.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := client.http.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	return resp
}

func syncW2PutHead(t *testing.T, client *testClient, repoID, targetHead string) *http.Response {
	t.Helper()
	return syncW2DoRequest(t, client, http.MethodPut, fmt.Sprintf("/seafhttp/repo/%s/commit/HEAD?head=%s", repoID, url.QueryEscape(targetHead)), nil, "")
}

type syncW2HeadRequestResult struct {
	resp *http.Response
	err  error
}

func TestW2SyncPutBlockHeadEvidence(t *testing.T) {
	requireCassandra(t)
	gate := w2SyncPutBlockHeadRequireEvidence(t)
	crashGate := w2SyncPutBlockHeadRequireCrashEvidence(t)
	database := shareProjectionDBForTest(t)
	session := database.Session()

	t.Run("normalFlowRenewsLivenessAndSettles", func(t *testing.T) {
		repoID := createTestLibrary(t, adminClient, fmt.Sprintf("inttest-w2-sync-normal-%d", time.Now().UnixNano()))
		orgID := resolveOrgID(t, repoID)
		initial := readLibrarySyncHeadState(t, session, repoID)

		fileData := []byte("W2 sync putblock->head normal flow payload\n")
		fc := syncW2PutFileCommit(t, adminClient, repoID, initial.HeadCommitID, "w2-normal.txt", fileData)

		upReferrer := dbpkg.BlockReferrerForUpload("sync:" + repoID + ":" + fc.internalBlockID)

		// Force the up: reference's Cassandra-native TTL down to near-immediate
		// expiry, simulating a block whose own liveness is nearly lapsed at the
		// moment HEAD runs. If HEAD does not renew it, this row would be gone
		// within seconds regardless of what HEAD does.
		if err := session.Query(`
			INSERT INTO block_references (org_id, block_id, referrer, library_id, created_at)
			VALUES (?, ?, ?, ?, ?) USING TTL 5
		`, orgID, fc.internalBlockID, upReferrer, repoID, time.Now().UTC()).Exec(); err != nil {
			t.Fatalf("force near-expiry TTL on up: reference: %v", err)
		}
		var nearExpiryTTL int
		if err := session.Query(`SELECT TTL(created_at) FROM block_references WHERE org_id = ? AND block_id = ? AND referrer = ?`,
			orgID, fc.internalBlockID, upReferrer).Scan(&nearExpiryTTL); err != nil {
			t.Fatalf("read forced near-expiry TTL: %v", err)
		}
		if nearExpiryTTL <= 0 || nearExpiryTTL > 30 {
			t.Fatalf("forced near-expiry TTL = %ds, want a small positive value", nearExpiryTTL)
		}

		resp := syncW2PutHead(t, adminClient, repoID, fc.commitID)
		expectStatus(t, resp, http.StatusOK)
		resp.Body.Close()

		var renewedTTL int
		if err := session.Query(`SELECT TTL(created_at) FROM block_references WHERE org_id = ? AND block_id = ? AND referrer = ?`,
			orgID, fc.internalBlockID, upReferrer).Scan(&renewedTTL); err != nil {
			t.Fatalf("read renewed up: TTL after HEAD: %v", err)
		}
		// db.ProvisionalBlockReferenceTTLSeconds is 48h (172800s); require the
		// renewed TTL to be unambiguously far from the forced near-expiry value.
		if renewedTTL < 170000 {
			t.Fatalf("renewed up: TTL = %ds after HEAD, want close to 172800s (48h) — own liveness was not renewed before HEAD", renewedTTL)
		}

		permanentRef := dbpkg.BlockReferrerForFSObject(repoID, fc.fileFSID)
		exists, err := database.BlockReferenceExists(orgID, fc.internalBlockID, permanentRef)
		if err != nil || !exists {
			t.Fatalf("permanent fs: reference exists = %v, %v; want true after finalize", exists, err)
		}

		bucket := publishRepairIntegrationBucket(orgID, repoID, fc.commitID, fc.fileFSID)
		if publishRepairIntegrationRepairRowExists(t, bucket, orgID, repoID, fc.commitID, fc.fileFSID) {
			t.Fatal("durable repair row still present after a successful normal-flow finalize")
		}

		linkResp := adminClient.Get(t, fmt.Sprintf("/api2/repos/%s/file/?p=/%s", repoID, url.PathEscape(fc.fileName)))
		expectStatus(t, linkResp, http.StatusOK)
		linkResp.Body.Close()

		markW2SyncPutBlockHeadEvidence(t, "normalFlowRenewsLivenessAndSettles")
		gate.observed = true
	})

	t.Run("crossNodeIdempotentRepairSettlesWithoutProcessMemory", func(t *testing.T) {
		clients := multiInstanceRequireAdminClients(t, 2)
		nodeA, nodeB := clients[0], clients[1]

		repoID := createTestLibrary(t, adminClient, fmt.Sprintf("inttest-w2-sync-crossnode-%d", time.Now().UnixNano()))
		orgID := resolveOrgID(t, repoID)
		initial := readLibrarySyncHeadState(t, session, repoID)

		fileData := []byte("W2 sync putblock->head cross-node payload\n")
		fc := syncW2PutFileCommit(t, nodeA, repoID, initial.HeadCommitID, "w2-crossnode.txt", fileData)

		resp := syncW2PutHead(t, nodeA, repoID, fc.commitID)
		expectStatus(t, resp, http.StatusOK)
		resp.Body.Close()

		permanentRef := dbpkg.BlockReferrerForFSObject(repoID, fc.fileFSID)
		if exists, err := database.BlockReferenceExists(orgID, fc.internalBlockID, permanentRef); err != nil || !exists {
			t.Fatalf("permanent fs: reference exists = %v, %v; want true after node A finalize", exists, err)
		}

		// Simulate "crashed before finalize completed": strip the permanent
		// fs: reference this exact node A request just registered, as if the
		// process had died between the HEAD CAS and finalize. Node A's own
		// finalizedBlockDeltas memo (in-process, per node.go instance) is why
		// this must be replayed from a DIFFERENT node: node A would otherwise
		// short-circuit its own idempotent retry without re-running the repair
		// this leg is proving.
		if err := database.RemoveBlockReference(orgID, fc.internalBlockID, permanentRef); err != nil {
			t.Fatalf("simulate crash-before-finalize (remove fs: ref): %v", err)
		}
		t.Cleanup(func() {
			_ = database.AddBlockReference(orgID, fc.internalBlockID, permanentRef, repoID, 0)
		})

		// Idempotent retry of the SAME already-applied target HEAD from node B:
		// handleSyncHeadIdempotentSuccess -> repairPublishedSyncCommitBlockDelta.
		resp = syncW2PutHead(t, nodeB, repoID, fc.commitID)
		expectStatus(t, resp, http.StatusOK)
		resp.Body.Close()

		waitForIntegrationCondition(t, "node B's idempotent retry to re-promote the permanent fs: reference", func() bool {
			exists, err := database.BlockReferenceExists(orgID, fc.internalBlockID, permanentRef)
			return err == nil && exists
		})

		bucket := publishRepairIntegrationBucket(orgID, repoID, fc.commitID, fc.fileFSID)
		if publishRepairIntegrationRepairRowExists(t, bucket, orgID, repoID, fc.commitID, fc.fileFSID) {
			t.Fatal("durable repair row still present after node B's idempotent-retry repair settled")
		}

		markW2SyncPutBlockHeadEvidence(t, "crossNodeIdempotentRepairSettlesWithoutProcessMemory")
		gate.observed = true
	})

	t.Run("activeGCClaimBlocksHeadFailClosed", func(t *testing.T) {
		repoID := createTestLibrary(t, adminClient, fmt.Sprintf("inttest-w2-sync-gcfirst-%d", time.Now().UnixNano()))
		orgID := resolveOrgID(t, repoID)
		orgUUID := uuid.MustParse(orgID)
		initial := readLibrarySyncHeadState(t, session, repoID)

		fileData := []byte("W2 sync putblock->head GC-first payload\n")
		fc := syncW2PutFileCommit(t, adminClient, repoID, initial.HeadCommitID, "w2-gcfirst.txt", fileData)

		store := gcpkg.NewCassandraStore(database)
		info, err := store.GetBlockInfo(orgUUID, fc.internalBlockID)
		if err != nil {
			t.Fatalf("read canonical block info after PutBlock: %v", err)
		}
		target := gcpkg.BlockDeleteTarget{StorageClass: info.StorageClass, StorageKey: info.StorageKey}
		attempt := gcpkg.BlockDeleteAuthority{
			Target:    target,
			ClaimID:   "w2-sync-gcfirst-" + uuid.NewString(),
			ClaimedAt: time.Now().UTC().Truncate(time.Millisecond),
		}
		claim, err := store.ClaimBlockDelete(orgUUID, fc.internalBlockID, attempt)
		if err != nil || claim.Outcome != gcpkg.BlockClaimAcquired {
			t.Fatalf("ClaimBlockDelete = %s, %v; want acquired", claim.Outcome, err)
		}
		t.Cleanup(func() {
			_ = session.Query(`UPDATE blocks SET gc_state = null, gc_claim_id = null, gc_claimed_at = null WHERE org_id = ? AND block_id = ?`,
				orgID, fc.internalBlockID).Exec()
		})

		// A real, currently-active GC claim on this exact block's canonical
		// placement is now visible. HEAD must reject the commit rather than
		// publish a placement GC is actively working to reclaim.
		resp := syncW2PutHead(t, adminClient, repoID, fc.commitID)
		if resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			t.Fatal("HEAD published a commit whose block has an active GC delete claim; must fail closed")
		}
		if resp.StatusCode != http.StatusServiceUnavailable {
			body := responseBody(t, resp)
			t.Fatalf("HEAD with an active GC claim returned status=%d, want 503; body=%s", resp.StatusCode, body)
		} else {
			resp.Body.Close()
		}

		current := readLibrarySyncHeadState(t, session, repoID)
		if current.HeadCommitID != initial.HeadCommitID {
			t.Fatalf("HEAD advanced to %s despite the active GC claim rejection; want unchanged %s", current.HeadCommitID, initial.HeadCommitID)
		}
		bucket := publishRepairIntegrationBucket(orgID, repoID, fc.commitID, fc.fileFSID)
		if publishRepairIntegrationRepairRowExists(t, bucket, orgID, repoID, fc.commitID, fc.fileFSID) {
			t.Fatal("readiness rejected the commit but created a durable repair row before readiness")
		}

		markW2SyncPutBlockHeadEvidence(t, "activeGCClaimBlocksHeadFailClosed")
		gate.observed = true
	})

	t.Run("divergentCASLoserRetainsSharedRepairRow", func(t *testing.T) {
		repoID := createTestLibrary(t, adminClient, fmt.Sprintf("inttest-w2-sync-caslose-%d", time.Now().UnixNano()))
		orgID := resolveOrgID(t, repoID)
		initial := readLibrarySyncHeadState(t, session, repoID)

		winnerData := []byte("W2 sync putblock->head CAS winner payload\n")
		loserData := []byte("W2 sync putblock->head CAS loser payload (conflicting)\n")
		// Same file name at the same parent with different content: the two
		// commits are not auto-mergeable (a real content conflict at one path,
		// not the non-overlapping-entries shape autoMergeProductionPathSettles
		// already covers), so exactly one is a genuine, non-recoverable CAS loss.
		const conflictFileName = "w2-cas-conflict.txt"
		winner := syncW2PutFileCommit(t, adminClient, repoID, initial.HeadCommitID, conflictFileName, winnerData)
		loser := syncW2PutFileCommit(t, adminClient, repoID, initial.HeadCommitID, conflictFileName, loserData)

		start := make(chan struct{})
		var wg sync.WaitGroup
		results := make(chan syncW2HeadRequestResult, 2)
		for _, target := range []string{winner.commitID, loser.commitID} {
			target := target
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				resp, err := syncW2PutHeadResult(adminClient, repoID, target)
				results <- syncW2HeadRequestResult{resp: resp, err: err}
			}()
		}
		close(start)
		wg.Wait()
		close(results)

		okCount, conflictCount := 0, 0
		for result := range results {
			if result.err != nil {
				t.Error(result.err)
				continue
			}
			resp := result.resp
			switch resp.StatusCode {
			case http.StatusOK:
				okCount++
			case http.StatusServiceUnavailable:
				conflictCount++
			default:
				t.Errorf("unexpected status %d for a concurrent divergent HEAD attempt", resp.StatusCode)
			}
			resp.Body.Close()
		}
		if okCount != 1 {
			t.Fatalf("exactly one concurrent divergent HEAD attempt should win, got %d successes (retry budget may need a second pass)", okCount)
		}

		current := readLibrarySyncHeadState(t, session, repoID)
		var winnerFC, loserFC syncW2FileCommit
		if current.HeadCommitID == winner.commitID {
			winnerFC, loserFC = winner, loser
		} else if current.HeadCommitID == loser.commitID {
			winnerFC, loserFC = loser, winner
		} else {
			t.Fatalf("HEAD %s is neither concurrent commit; auto-merge should not trigger for two same-parent divergent direct commits without an ancestry relation", current.HeadCommitID)
		}

		winnerPermanentRef := dbpkg.BlockReferrerForFSObject(repoID, winnerFC.fileFSID)
		if exists, err := database.BlockReferenceExists(orgID, winnerFC.internalBlockID, winnerPermanentRef); err != nil || !exists {
			t.Fatalf("winner permanent fs: reference exists = %v, %v; want true", exists, err)
		}
		loserPermanentRef := dbpkg.BlockReferrerForFSObject(repoID, loserFC.fileFSID)
		if exists, err := database.BlockReferenceExists(orgID, loserFC.internalBlockID, loserPermanentRef); err == nil && exists {
			t.Fatalf("loser's fs_object %s must not have gained a permanent reference; loser's attempt never finalized", loserFC.fileFSID)
		}

		winnerBucket := publishRepairIntegrationBucket(orgID, repoID, winnerFC.commitID, winnerFC.fileFSID)
		if publishRepairIntegrationRepairRowExists(t, winnerBucket, orgID, repoID, winnerFC.commitID, winnerFC.fileFSID) {
			t.Fatal("winner's durable repair row still present after settlement")
		}
		loserBucket := publishRepairIntegrationBucket(orgID, repoID, loserFC.commitID, loserFC.fileFSID)
		if !publishRepairIntegrationRepairRowExists(t, loserBucket, orgID, repoID, loserFC.commitID, loserFC.fileFSID) {
			t.Fatal("loser's shared durable repair row was removed by request-local divergent-CAS cleanup")
		}
		// Production correctly retains this row forever (loserFC.commitID never
		// settles). Test-only cleanup so repeated runs don't accumulate orphaned
		// rows in the shared dev Cassandra instance.
		t.Cleanup(func() {
			_ = session.Query(`DELETE FROM published_block_reference_repairs WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?`,
				loserBucket, orgID, repoID, loserFC.commitID, loserFC.fileFSID).Exec()
		})
		if conflictCount == 0 {
			t.Log("both requests observed 200 on this pass (idempotent-success race); the shared-row ownership assertions above still hold")
		}

		markW2SyncPutBlockHeadEvidence(t, "divergentCASLoserRetainsSharedRepairRow")
		gate.observed = true
	})

	t.Run("crashAfterHeadCASBeforeFinalizeReplays", func(t *testing.T) {
		if os.Getenv(w2SyncPutBlockHeadCrashEvidenceEnv) != "1" {
			t.Skipf("%s is not enabled", w2SyncPutBlockHeadCrashEvidenceEnv)
		}
		clients := multiInstanceRequireAdminClients(t, 2)
		nodeA, nodeB := clients[0], clients[1]
		repoID := createTestLibrary(t, adminClient, fmt.Sprintf("inttest-w2-sync-crash-%d", time.Now().UnixNano()))
		orgID := resolveOrgID(t, repoID)
		initial := readLibrarySyncHeadState(t, session, repoID)
		fc := syncW2PutFileCommit(t, nodeB, repoID, initial.HeadCommitID, "w2-crash.txt", []byte("W2 sync post-CAS crash payload\n"))

		headers := make(http.Header)
		headers.Set("X-SesameFS-Test-Crash-After-Head-CAS", "1")
		resp, err := syncW2PutHeadResultWithHeaders(nodeB, repoID, fc.commitID, headers)
		if err == nil {
			if resp != nil {
				resp.Body.Close()
			}
			t.Fatal("crash failpoint request returned without terminating the node")
		}

		waitForIntegrationCondition(t, "crashed node to restart and answer health", func() bool {
			healthResp, healthErr := nodeB.http.Get(nodeB.baseURL + "/health")
			if healthErr != nil {
				return false
			}
			defer healthResp.Body.Close()
			return healthResp.StatusCode == http.StatusOK
		})
		current := readLibrarySyncHeadState(t, session, repoID)
		if current.HeadCommitID != fc.commitID {
			t.Fatalf("HEAD after crash = %s, want CAS target %s", current.HeadCommitID, fc.commitID)
		}
		bucket := publishRepairIntegrationBucket(orgID, repoID, fc.commitID, fc.fileFSID)
		if !publishRepairIntegrationRepairRowExists(t, bucket, orgID, repoID, fc.commitID, fc.fileFSID) {
			t.Fatal("post-CAS crash did not leave the durable repair row for replay")
		}

		resp = syncW2PutHead(t, nodeA, repoID, fc.commitID)
		expectStatus(t, resp, http.StatusOK)
		resp.Body.Close()
		permanentRef := dbpkg.BlockReferrerForFSObject(repoID, fc.fileFSID)
		waitForIntegrationCondition(t, "retry to replay the permanent fs reference", func() bool {
			exists, refErr := database.BlockReferenceExists(orgID, fc.internalBlockID, permanentRef)
			return refErr == nil && exists
		})
		if publishRepairIntegrationRepairRowExists(t, bucket, orgID, repoID, fc.commitID, fc.fileFSID) {
			t.Fatal("durable repair row still present after crash replay settled")
		}
		markW2SyncPutBlockHeadCrashEvidence(t, crashGate)
	})
	t.Run("autoMergeProductionPathSettlesWithRealBlocks", func(t *testing.T) {
		repoID := createTestLibrary(t, adminClient, fmt.Sprintf("inttest-w2-sync-automerge-%d", time.Now().UnixNano()))
		orgID := resolveOrgID(t, repoID)
		initial := readLibrarySyncHeadState(t, session, repoID)

		currentData := []byte("W2 sync putblock->head auto-merge current payload\n")
		targetData := []byte("W2 sync putblock->head auto-merge target payload\n")
		currentFC := syncW2PutFileCommit(t, adminClient, repoID, initial.HeadCommitID, "w2-automerge-current.txt", currentData)
		targetFC := syncW2PutFileCommit(t, adminClient, repoID, initial.HeadCommitID, "w2-automerge-target.txt", targetData)

		resp := syncW2PutHead(t, adminClient, repoID, currentFC.commitID)
		expectStatus(t, resp, http.StatusOK)
		resp.Body.Close()

		waitForIntegrationCondition(t, "current commit to become authoritative before auto-merge", func() bool {
			current := readLibrarySyncHeadState(t, session, repoID)
			return current.HeadCommitID == currentFC.commitID
		})

		// targetFC's parent is the now-stale initial head; this is exactly the
		// non-overlapping-entries auto-merge case, driven here with real
		// PutBlock-provenanced blocks so the new pre-HEAD readiness gate
		// actually processes canonical blocks on this path, not synthetic
		// empty fs_objects.
		resp = syncW2PutHead(t, adminClient, repoID, targetFC.commitID)
		expectStatus(t, resp, http.StatusOK)
		resp.Body.Close()

		var mergedCommitID string
		waitForIntegrationCondition(t, "stale publish to auto-merge into a new HEAD carrying both files", func() bool {
			current := readLibrarySyncHeadState(t, session, repoID)
			if current.HeadCommitID == currentFC.commitID || current.HeadCommitID == targetFC.commitID {
				return false
			}
			mergedCommitID = current.HeadCommitID
			return mergedCommitID != ""
		})

		for _, fc := range []syncW2FileCommit{currentFC, targetFC} {
			permanentRef := dbpkg.BlockReferrerForFSObject(repoID, fc.fileFSID)
			if exists, err := database.BlockReferenceExists(orgID, fc.internalBlockID, permanentRef); err != nil || !exists {
				t.Fatalf("merged commit missing permanent fs: reference for %s: exists=%v err=%v", fc.fileName, exists, err)
			}
			bucket := publishRepairIntegrationBucket(orgID, repoID, mergedCommitID, fc.fileFSID)
			if publishRepairIntegrationRepairRowExists(t, bucket, orgID, repoID, mergedCommitID, fc.fileFSID) {
				t.Fatalf("durable repair row still present for %s after auto-merge settlement", fc.fileName)
			}
			linkResp := adminClient.Get(t, fmt.Sprintf("/api2/repos/%s/file/?p=/%s", repoID, url.PathEscape(fc.fileName)))
			expectStatus(t, linkResp, http.StatusOK)
			linkResp.Body.Close()
		}

		markW2SyncPutBlockHeadEvidence(t, "autoMergeProductionPathSettlesWithRealBlocks")
		gate.observed = true
	})
}

func syncW2DoRequestResultWithHeaders(client *testClient, method, path string, body []byte, contentType string, headers http.Header) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, client.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s %s request: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Token "+client.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	return client.http.Do(req)
}

func syncW2PutHeadResult(client *testClient, repoID, targetHead string) (*http.Response, error) {
	return syncW2DoRequestResultWithHeaders(client, http.MethodPut, fmt.Sprintf("/seafhttp/repo/%s/commit/HEAD?head=%s", repoID, url.QueryEscape(targetHead)), nil, "", nil)
}

func syncW2PutHeadResultWithHeaders(client *testClient, repoID, targetHead string, headers http.Header) (*http.Response, error) {
	return syncW2DoRequestResultWithHeaders(client, http.MethodPut, fmt.Sprintf("/seafhttp/repo/%s/commit/HEAD?head=%s", repoID, url.QueryEscape(targetHead)), nil, "", headers)
}
