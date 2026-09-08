package api

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSyncCommitStoredIdentityMatchesSnapshot(t *testing.T) {
	parent := "parent-1"
	cases := []struct {
		name     string
		existing map[string]interface{}
		want     *string
		root     string
		matches  bool
	}{
		{name: "same parent and root", existing: map[string]interface{}{"parent_id": parent, "root_fs_id": "root-1"}, want: &parent, root: "root-1", matches: true},
		{name: "nil parent matches empty stored parent", existing: map[string]interface{}{"parent_id": nil, "root_fs_id": "root-1"}, root: "root-1", matches: true},
		{name: "byte values match", existing: map[string]interface{}{"parent_id": []byte(parent), "root_fs_id": []byte("root-1")}, want: &parent, root: "root-1", matches: true},
		{name: "different root conflicts", existing: map[string]interface{}{"parent_id": parent, "root_fs_id": "root-2"}, want: &parent, root: "root-1", matches: false},
		{name: "different parent conflicts", existing: map[string]interface{}{"parent_id": "parent-2", "root_fs_id": "root-1"}, want: &parent, root: "root-1", matches: false},
		{name: "missing identity conflicts", existing: map[string]interface{}{"root_fs_id": "root-1"}, want: &parent, root: "root-1", matches: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := syncCommitStoredIdentityMatches(tc.existing, tc.want, tc.root); got != tc.matches {
				t.Fatalf("syncCommitStoredIdentityMatches = %v, want %v", got, tc.matches)
			}
		})
	}
}

func TestClassifySyncFSObjectRow(t *testing.T) {
	expected := syncFSObjectIdentity{
		objType:      "file",
		sizeBytes:    7,
		dirEntries:   "[]",
		wireBlockIDs: []string{"block-a"},
	}
	cases := []struct {
		name string
		row  map[string]interface{}
		want syncFSObjectRowState
	}{
		{
			name: "metadata-only placeholder",
			row:  map[string]interface{}{"obj_name": "child.txt", "full_path": "/child.txt"},
			want: syncFSObjectRowPlaceholder,
		},
		{
			name: "complete legacy file",
			row:  map[string]interface{}{"obj_type": "file", "size_bytes": int64(7), "block_ids": []string{"block-a"}},
			want: syncFSObjectRowComplete,
		},
		{
			name: "complete canonical file uses logical sha1 list",
			row:  map[string]interface{}{"obj_type": "file", "size_bytes": int64(7), "block_ids": []string{"sha256-a"}, "seafile_block_ids_sha1": []string{"block-a"}},
			want: syncFSObjectRowComplete,
		},

		{
			name: "partial matching file needs completion",
			row:  map[string]interface{}{"obj_type": "file", "size_bytes": int64(7)},
			want: syncFSObjectRowNeedsCompletion,
		},
		{
			name: "conflicting canonical logical list",
			row:  map[string]interface{}{"obj_type": "file", "size_bytes": int64(7), "block_ids": []string{"sha256-a"}, "seafile_block_ids_sha1": []string{"block-b"}},
			want: syncFSObjectRowConflict,
		},
		{
			name: "conflicting partial row",
			row:  map[string]interface{}{"obj_type": "file", "size_bytes": int64(8)},
			want: syncFSObjectRowConflict,
		},
	}
	dirExpected := syncFSObjectIdentity{objType: "dir", dirEntries: "[]"}
	if got := classifySyncFSObjectRow(map[string]interface{}{"obj_type": "dir", "size_bytes": int64(0), "dir_entries": "[]", "block_ids": []string{"not-an-identity-field"}}, dirExpected); got != syncFSObjectRowComplete {
		t.Fatalf("classifySyncFSObjectRow directory = %v, want %v", got, syncFSObjectRowComplete)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifySyncFSObjectRow(tc.row, expected); got != tc.want {
				t.Fatalf("classifySyncFSObjectRow = %v, want %v", got, tc.want)
			}
		})
	}
}

func packSyncFSObjectForUnit(t *testing.T, fsID string, jsonData []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(jsonData); err != nil {
		t.Fatalf("compress fs object: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close compressed fs object: %v", err)
	}
	body := make([]byte, 0, 44+compressed.Len())
	body = append(body, []byte(fsID)...)
	size := make([]byte, 4)
	binary.BigEndian.PutUint32(size, uint32(compressed.Len()))
	body = append(body, size...)
	body = append(body, compressed.Bytes()...)
	return body
}

func TestRecvFSStoreFailureFailsClosed(t *testing.T) {
	old := storeSyncFSObjectFn
	storeSyncFSObjectFn = func(_ *SyncHandler, _, _ string, _ syncFSObjectIdentity) error {
		return errors.New("database unavailable")
	}
	t.Cleanup(func() { storeSyncFSObjectFn = old })

	jsonData := []byte(`{"block_ids":["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"],"size":1,"type":1,"version":1}`)
	hash := sha1.Sum(jsonData)
	fsID := hex.EncodeToString(hash[:])
	r := setupSyncTestRouter()
	r.POST("/seafhttp/repo/:repo_id/recv-fs", (&SyncHandler{}).RecvFS)
	req := httptest.NewRequest(http.MethodPost, "/seafhttp/repo/repo/recv-fs", bytes.NewReader(packSyncFSObjectForUnit(t, fsID, jsonData)))
	req.Header.Set("Content-Type", "application/octet-stream")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("RecvFS storage failure status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

func TestRecvFSRejectsUppercaseClaimedFSID(t *testing.T) {
	storeCalled := false
	old := storeSyncFSObjectFn
	storeSyncFSObjectFn = func(_ *SyncHandler, _, _ string, _ syncFSObjectIdentity) error {
		storeCalled = true
		return nil
	}
	t.Cleanup(func() { storeSyncFSObjectFn = old })

	jsonData := []byte(`{"block_ids":["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"],"size":1,"type":1,"version":1}`)
	hash := sha1.Sum(jsonData)
	fsID := hex.EncodeToString(hash[:])
	upperFSID := strings.ToUpper(fsID)
	if upperFSID == fsID {
		t.Skip("computed test fs_id has no alphabetic hex digit")
	}
	r := setupSyncTestRouter()
	r.POST("/seafhttp/repo/:repo_id/recv-fs", (&SyncHandler{}).RecvFS)
	req := httptest.NewRequest(http.MethodPost, "/seafhttp/repo/repo/recv-fs", bytes.NewReader(packSyncFSObjectForUnit(t, upperFSID, jsonData)))
	req.Header.Set("Content-Type", "application/octet-stream")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("uppercase claimed fs_id status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if storeCalled {
		t.Fatal("uppercase claimed fs_id reached storage")
	}
}
