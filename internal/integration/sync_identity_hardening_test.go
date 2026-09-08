//go:build integration

package integration

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	apipkg "github.com/Sesame-Disk/sesamefs/internal/api"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

func syncIdentityCommitPayload(t *testing.T, repoID, commitID, parentID, rootID, description string) []byte {
	t.Helper()
	return mustMarshalSyncObjectForTest(t, map[string]interface{}{
		"commit_id":   commitID,
		"repo_id":     repoID,
		"root_id":     rootID,
		"parent_id":   parentID,
		"description": description,
		"ctime":       time.Now().Unix(),
		"version":     1,
	})
}

func syncIdentityPutCommit(t *testing.T, client *testClient, repoID, commitID string, body []byte) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, client.baseURL+fmt.Sprintf("/seafhttp/repo/%s/commit/%s", repoID, commitID), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create PutCommit request: %v", err)
	}
	req.Header.Set("Authorization", "Token "+client.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.http.Do(req)
	if err != nil {
		t.Fatalf("PutCommit request failed: %v", err)
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(responseBody)
}

type syncIdentityConcurrentResult struct {
	status int
	body   string
	err    error
}

func syncIdentityPutCommitConcurrent(client *testClient, repoID, commitID string, body []byte, start <-chan struct{}, results chan<- syncIdentityConcurrentResult) {
	<-start
	req, err := http.NewRequest(http.MethodPut, client.baseURL+fmt.Sprintf("/seafhttp/repo/%s/commit/%s", repoID, commitID), bytes.NewReader(body))
	if err != nil {
		results <- syncIdentityConcurrentResult{err: err}
		return
	}
	req.Header.Set("Authorization", "Token "+client.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.http.Do(req)
	if err != nil {
		results <- syncIdentityConcurrentResult{err: err}
		return
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(resp.Body)
	results <- syncIdentityConcurrentResult{status: resp.StatusCode, body: string(responseBody)}
}

func TestSyncCommitIdentityIsWriteOnceAndRaceSafe(t *testing.T) {
	requireCassandra(t)
	repoID := createTestLibrary(t, adminClient, fmt.Sprintf("inttest-sync-commit-identity-%d", time.Now().UnixNano()))
	session := shareProjectionDBForTest(t).Session()
	initial := readLibrarySyncHeadState(t, session, repoID)
	rootA := strings.Repeat("a", 40)
	rootB := strings.Repeat("b", 40)
	parentB := strings.Repeat("c", 40)

	commitID := fmt.Sprintf("%040x", time.Now().UnixNano())
	if status, body := syncIdentityPutCommit(t, adminClient, repoID, commitID, syncIdentityCommitPayload(t, repoID, commitID, initial.HeadCommitID, rootA, "first")); status != http.StatusOK {
		t.Fatalf("first PutCommit status=%d body=%s, want 200", status, body)
	}
	if status, body := syncIdentityPutCommit(t, adminClient, repoID, commitID, syncIdentityCommitPayload(t, repoID, commitID, initial.HeadCommitID, rootA, "idempotent retry")); status != http.StatusOK {
		t.Fatalf("identical PutCommit retry status=%d body=%s, want 200", status, body)
	}
	if status, body := syncIdentityPutCommit(t, adminClient, repoID, commitID, syncIdentityCommitPayload(t, repoID, commitID, initial.HeadCommitID, rootB, "conflicting root")); status != http.StatusConflict {
		t.Fatalf("conflicting root status=%d body=%s, want 409", status, body)
	}

	parentConflictID := fmt.Sprintf("%040x", time.Now().UnixNano()+1)
	if status, body := syncIdentityPutCommit(t, adminClient, repoID, parentConflictID, syncIdentityCommitPayload(t, repoID, parentConflictID, initial.HeadCommitID, rootA, "parent baseline")); status != http.StatusOK {
		t.Fatalf("parent baseline status=%d body=%s, want 200", status, body)
	}
	if status, body := syncIdentityPutCommit(t, adminClient, repoID, parentConflictID, syncIdentityCommitPayload(t, repoID, parentConflictID, parentB, rootA, "conflicting parent")); status != http.StatusConflict {
		t.Fatalf("conflicting parent status=%d body=%s, want 409", status, body)
	}

	concurrentID := fmt.Sprintf("%040x", time.Now().UnixNano()+2)
	start := make(chan struct{})
	results := make(chan syncIdentityConcurrentResult, 2)
	var wg sync.WaitGroup
	for _, root := range []string{rootA, rootB} {
		wg.Add(1)
		go func(root string) {
			defer wg.Done()
			syncIdentityPutCommitConcurrent(adminClient, repoID, concurrentID, syncIdentityCommitPayload(t, repoID, concurrentID, initial.HeadCommitID, root, "concurrent"), start, results)
		}(root)
	}
	close(start)
	wg.Wait()
	close(results)
	statusCounts := map[int]int{}
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent PutCommit failed: %v", result.err)
		}
		statusCounts[result.status]++
	}
	if statusCounts[http.StatusOK] != 1 || statusCounts[http.StatusConflict] != 1 {
		t.Fatalf("concurrent statuses = %v, want exactly one 200 and one 409", statusCounts)
	}

	var storedRoot string
	var storedParent string
	if err := session.Query(`SELECT parent_id, root_fs_id FROM commits WHERE library_id = ? AND commit_id = ?`, repoID, commitID).Scan(&storedParent, &storedRoot); err != nil {
		t.Fatalf("read stored commit identity: %v", err)
	}
	if storedParent != initial.HeadCommitID || storedRoot != rootA {
		t.Fatalf("stored commit identity = parent %q root %q, want parent %q root %q", storedParent, storedRoot, initial.HeadCommitID, rootA)
	}
	if err := session.Query(`SELECT root_fs_id FROM commits WHERE library_id = ? AND commit_id = ?`, repoID, concurrentID).Scan(&storedRoot); err != nil {
		t.Fatalf("read concurrent winner identity: %v", err)
	}
	if storedRoot != rootA && storedRoot != rootB {
		t.Fatalf("concurrent winner root = %q, want %q or %q", storedRoot, rootA, rootB)
	}
}

func TestSyncFSObjectIdentityIsContentAddressedAndWriteOnce(t *testing.T) {
	requireCassandra(t)
	repoID := createTestLibrary(t, adminClient, fmt.Sprintf("inttest-sync-fs-identity-%d", time.Now().UnixNano()))
	session := shareProjectionDBForTest(t).Session()
	blockA := syncSHA1HexForTest([]byte("block-a"))
	fileObjectJSON := mustMarshalSyncObjectForTest(t, map[string]interface{}{
		"block_ids": []string{blockA},
		"size":      int64(7),
		"type":      1,
		"version":   1,
	})
	fileFSID := syncSHA1HexForTest(fileObjectJSON)

	packed := packSyncFSObjectsForTest(t, syncPackedFSObject{fsID: fileFSID, jsonData: fileObjectJSON})
	resp := doSyncProtocolRequestForTest(t, http.MethodPost, fmt.Sprintf("/seafhttp/repo/%s/recv-fs", repoID), packed, "application/octet-stream")
	expectStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doSyncProtocolRequestForTest(t, http.MethodPost, fmt.Sprintf("/seafhttp/repo/%s/recv-fs", repoID), packed, "application/octet-stream")
	expectStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	blockB := syncSHA1HexForTest([]byte("block-b"))
	conflictingPayload := mustMarshalSyncObjectForTest(t, map[string]interface{}{
		"block_ids": []string{blockB},
		"size":      int64(7),
		"type":      1,
		"version":   1,
	})
	resp = doSyncProtocolRequestForTest(t, http.MethodPost, fmt.Sprintf("/seafhttp/repo/%s/recv-fs", repoID), packSyncFSObjectsForTest(t, syncPackedFSObject{fsID: fileFSID, jsonData: conflictingPayload}), "application/octet-stream")
	expectStatus(t, resp, http.StatusBadRequest)
	resp.Body.Close()

	wrongFSID := strings.Repeat("f", 40)
	if wrongFSID == fileFSID {
		wrongFSID = strings.Repeat("e", 40)
	}
	resp = doSyncProtocolRequestForTest(t, http.MethodPost, fmt.Sprintf("/seafhttp/repo/%s/recv-fs", repoID), packSyncFSObjectsForTest(t, syncPackedFSObject{fsID: wrongFSID, jsonData: fileObjectJSON}), "application/octet-stream")
	expectStatus(t, resp, http.StatusBadRequest)
	resp.Body.Close()

	var storedBlocks []string
	if err := session.Query(`SELECT block_ids FROM fs_objects WHERE library_id = ? AND fs_id = ?`, repoID, fileFSID).Scan(&storedBlocks); err != nil {
		t.Fatalf("read stored fs object: %v", err)
	}
	if len(storedBlocks) != 1 || storedBlocks[0] != blockA {
		t.Fatalf("stored block_ids = %v, want [%s] after conflicting replays", storedBlocks, blockA)
	}
	var missingType string
	if err := session.Query(`SELECT obj_type FROM fs_objects WHERE library_id = ? AND fs_id = ?`, repoID, wrongFSID).Scan(&missingType); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("wrong fs_id row lookup error = %v, want not found", err)
	}
}

func TestPublishedSyncTreeCannotBeMutatedByIdentityReplay(t *testing.T) {
	requireCassandra(t)
	repoID := createTestLibrary(t, adminClient, fmt.Sprintf("inttest-sync-identity-published-%d", time.Now().UnixNano()))
	session := shareProjectionDBForTest(t).Session()
	initial := readLibrarySyncHeadState(t, session, repoID)
	fileData := []byte("content-addressed identity A")
	externalBlockID := syncSHA1HexForTest(fileData)
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
			Mtime: time.Now().Unix(),
			Name:  "identity.txt",
			Size:  int64(len(fileData)),
		}},
		"type":    3,
		"version": 1,
	})
	rootFSID := syncSHA1HexForTest(rootObjectJSON)
	commitID := fmt.Sprintf("%040x", time.Now().UnixNano())

	status, body := syncIdentityPutCommit(t, adminClient, repoID, commitID, syncIdentityCommitPayload(t, repoID, commitID, initial.HeadCommitID, rootFSID, "published identity"))
	if status != http.StatusOK {
		t.Fatalf("published commit status=%d body=%s, want 200", status, body)
	}
	resp := doSyncProtocolRequestForTest(t, http.MethodPost, fmt.Sprintf("/seafhttp/repo/%s/recv-fs", repoID), packSyncFSObjectsForTest(t,
		syncPackedFSObject{fsID: fileFSID, jsonData: fileObjectJSON},
		syncPackedFSObject{fsID: rootFSID, jsonData: rootObjectJSON},
	), "application/octet-stream")
	expectStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doSyncProtocolRequestForTest(t, http.MethodPut, fmt.Sprintf("/seafhttp/repo/%s/block/%s", repoID, externalBlockID), fileData, "application/octet-stream")
	expectStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = doSyncProtocolRequestForTest(t, http.MethodPut, fmt.Sprintf("/seafhttp/repo/%s/commit/HEAD?head=%s", repoID, commitID), nil, "")
	expectStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	conflictingRoot := strings.Repeat("d", 40)
	if status, body := syncIdentityPutCommit(t, adminClient, repoID, commitID, syncIdentityCommitPayload(t, repoID, commitID, initial.HeadCommitID, conflictingRoot, "mutate commit")); status != http.StatusConflict {
		t.Fatalf("replayed conflicting PutCommit status=%d body=%s, want 409", status, body)
	}
	conflictingFS := mustMarshalSyncObjectForTest(t, map[string]interface{}{
		"block_ids": []string{syncSHA1HexForTest([]byte("content-addressed identity B"))},
		"size":      int64(len(fileData)),
		"type":      1,
		"version":   1,
	})
	resp = doSyncProtocolRequestForTest(t, http.MethodPost, fmt.Sprintf("/seafhttp/repo/%s/recv-fs", repoID), packSyncFSObjectsForTest(t, syncPackedFSObject{fsID: fileFSID, jsonData: conflictingFS}), "application/octet-stream")
	expectStatus(t, resp, http.StatusBadRequest)
	resp.Body.Close()

	current := readLibrarySyncHeadState(t, session, repoID)
	if current.HeadCommitID != commitID {
		t.Fatalf("published HEAD = %q, want %q after identity replays", current.HeadCommitID, commitID)
	}
	var storedRoot string
	if err := session.Query(`SELECT root_fs_id FROM commits WHERE library_id = ? AND commit_id = ?`, repoID, commitID).Scan(&storedRoot); err != nil {
		t.Fatalf("read published commit root: %v", err)
	}
	if storedRoot != rootFSID {
		t.Fatalf("published commit root = %q, want %q", storedRoot, rootFSID)
	}
	var storedBlocks []string
	if err := session.Query(`SELECT block_ids FROM fs_objects WHERE library_id = ? AND fs_id = ?`, repoID, fileFSID).Scan(&storedBlocks); err != nil {
		t.Fatalf("read published file object: %v", err)
	}
	if len(storedBlocks) != 1 || storedBlocks[0] != externalBlockID {
		t.Fatalf("published file block_ids = %v, want [%s]", storedBlocks, externalBlockID)
	}
}
