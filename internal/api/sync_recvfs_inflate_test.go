package api

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/Sesame-Disk/sesamefs/internal/config"
)

// recvFSInflateDirObject builds a well-formed directory fs object whose JSON is at
// least minBytes long, together with its content-addressed fs_id. The dirents are
// identical, so the object compresses at roughly DEFLATE's best ratio — the shape
// ISSUE-RECVFS-DECOMPRESSION-AMPLIFICATION-01 measured — while still passing the
// handler's fs_id == SHA-1(JSON) check and parsing as a directory. Anything RecvFS
// rejects about it is therefore the size, not the content.
func recvFSInflateDirObject(t *testing.T, minBytes int) (string, []byte) {
	t.Helper()
	const dirent = `{"id":"0123456789abcdef0123456789abcdef01234567","mode":33188,"modifier":"user@example.com","mtime":1768543179,"name":"f.txt","size":1}`
	const head, tail = `{"dirents":[`, `],"type":3,"version":1}`
	n := (minBytes-len(head)-len(tail))/(len(dirent)+1) + 1
	if n < 1 {
		n = 1
	}
	var b strings.Builder
	b.Grow(len(head) + n*(len(dirent)+1) + len(tail))
	b.WriteString(head)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(dirent)
	}
	b.WriteString(tail)
	jsonData := []byte(b.String())
	hash := sha1.Sum(jsonData)
	return hex.EncodeToString(hash[:]), jsonData
}

// TestRecvFSRejectsOversizedObjectBeforeMaterializing is the headline case of
// ISSUE-RECVFS-DECOMPRESSION-AMPLIFICATION-01 under the default configuration: one
// valid, correctly addressed directory object whose compressed form is a few
// hundred KiB and whose JSON is 256 MiB. Before the fix RecvFS inflated all of it,
// parsed it and handed it to storage.
//
// The allocation figure is the canary, not the contract (the contract is the 413
// and the object never reaching storage): TotalAlloc is process-global, so this
// test must never be made parallel. Measured on go1.25.14: 1950.6 MiB before the
// fix; 102.9 MiB after it, almost all of it io.ReadAll growing a buffer to the
// 16 MiB default cap. The threshold sits well clear of both.
func TestRecvFSRejectsOversizedObjectBeforeMaterializing(t *testing.T) {
	const inflated = 256 * 1024 * 1024
	fsID, jsonData := recvFSInflateDirObject(t, inflated)
	body := packSyncFSObjectForUnit(t, fsID, jsonData)
	jsonData = nil
	t.Logf("compressed body %d bytes inflates to >= %d bytes", len(body), inflated)

	stored := 0
	old := storeSyncFSObjectFn
	storeSyncFSObjectFn = func(_ *SyncHandler, _, _ string, _ syncFSObjectIdentity) error {
		stored++
		return nil
	}
	t.Cleanup(func() { storeSyncFSObjectFn = old })

	r := setupSyncTestRouter()
	r.POST("/seafhttp/repo/:repo_id/recv-fs", (&SyncHandler{}).RecvFS)
	req := httptest.NewRequest(http.MethodPost, "/seafhttp/repo/repo/recv-fs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/octet-stream")
	w := httptest.NewRecorder()

	runtime.GC()
	var m1, m2 runtime.MemStats
	runtime.ReadMemStats(&m1)
	r.ServeHTTP(w, req)
	runtime.ReadMemStats(&m2)
	allocated := m2.TotalAlloc - m1.TotalAlloc
	t.Logf("status %d, allocated %.1f MiB, stored %d", w.Code, float64(allocated)/(1024*1024), stored)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413; body=%s", w.Code, w.Body.String())
	}
	if stored != 0 {
		t.Errorf("oversized object reached storage %d time(s), want 0", stored)
	}
	const maxAllocBytes = 256 * 1024 * 1024
	if allocated > maxAllocBytes {
		t.Errorf("allocated %.1f MiB handling a rejected object, want < %d MiB: the inflate is not bounded",
			float64(allocated)/(1024*1024), maxAllocBytes/(1024*1024))
	}
}

// recvFSExactDirObject builds a well-formed directory object whose JSON is exactly
// size bytes, so the cap boundaries can be pinned to the byte. Objects built with
// different fill bytes have different content and therefore different fs_ids.
func recvFSExactDirObject(t *testing.T, size int, fill byte) (string, []byte) {
	t.Helper()
	const head = `{"dirents":[{"id":"0123456789abcdef0123456789abcdef01234567","mode":33188,"modifier":"user@example.com","mtime":1768543179,"name":"`
	const tail = `","size":1}],"type":3,"version":1}`
	pad := size - len(head) - len(tail)
	if pad < 1 {
		t.Fatalf("size %d is below the %d-byte minimum object", size, len(head)+len(tail)+1)
	}
	jsonData := []byte(head + strings.Repeat(string(fill), pad) + tail)
	hash := sha1.Sum(jsonData)
	return hex.EncodeToString(hash[:]), jsonData
}

// recvFSCorruptObject packs a well-formed object and then breaks the zlib adler32
// trailer, so the stream inflates all of its bytes and only then fails.
func recvFSCorruptObject(t *testing.T, size int, fill byte) []byte {
	t.Helper()
	fsID, jsonData := recvFSExactDirObject(t, size, fill)
	packed := packSyncFSObjectForUnit(t, fsID, jsonData)
	packed[len(packed)-1] ^= 0xff
	return packed
}

// captureRecvFSStores replaces storage for the test and returns what reached it.
func captureRecvFSStores(t *testing.T) *map[string]syncFSObjectIdentity {
	t.Helper()
	stored := map[string]syncFSObjectIdentity{}
	old := storeSyncFSObjectFn
	storeSyncFSObjectFn = func(_ *SyncHandler, _, fsID string, identity syncFSObjectIdentity) error {
		stored[fsID] = identity
		return nil
	}
	t.Cleanup(func() { storeSyncFSObjectFn = old })
	return &stored
}

func recvFSHandlerWithCaps(maxObject, maxInflated int64) *SyncHandler {
	h := &SyncHandler{config: &config.Config{}}
	h.config.SeafHTTP.RecvFSMaxObjectBytes = maxObject
	h.config.SeafHTTP.RecvFSMaxInflatedBytes = maxInflated
	return h
}

func postRecvFSBody(t *testing.T, h *SyncHandler, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	r := setupSyncTestRouter()
	r.POST("/seafhttp/repo/:repo_id/recv-fs", h.RecvFS)
	req := httptest.NewRequest(http.MethodPost, "/seafhttp/repo/repo/recv-fs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/octet-stream")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// assertRecvFS413 pins the client-visible 413 schema: the error text and the
// name and value of the cap that fired.
func assertRecvFS413(t *testing.T, w *httptest.ResponseRecorder, wantError, capField string, capValue int64) {
	t.Helper()
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", w.Code, w.Body.String())
	}
	var got map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("413 body is not JSON: %v; body=%s", err, w.Body.String())
	}
	if got["error"] != wantError {
		t.Errorf("error = %q, want %q", got["error"], wantError)
	}
	if v, ok := got[capField].(float64); !ok || int64(v) != capValue {
		t.Errorf("%s = %v, want %d", capField, got[capField], capValue)
	}
}

func TestRecvFSInflateCapResolvers(t *testing.T) {
	h := &SyncHandler{}
	if got := h.syncRecvFSMaxObjectBytes(); got != config.DefaultRecvFSMaxObjectBytes {
		t.Errorf("nil config object cap = %d, want the %d default", got, config.DefaultRecvFSMaxObjectBytes)
	}
	if got := h.syncRecvFSMaxInflatedBytes(); got != config.DefaultRecvFSMaxInflatedBytes {
		t.Errorf("nil config batch cap = %d, want the %d default", got, config.DefaultRecvFSMaxInflatedBytes)
	}
	h = recvFSHandlerWithCaps(1234, 5678)
	if got := h.syncRecvFSMaxObjectBytes(); got != 1234 {
		t.Errorf("configured object cap = %d, want 1234", got)
	}
	if got := h.syncRecvFSMaxInflatedBytes(); got != 5678 {
		t.Errorf("configured batch cap = %d, want 5678", got)
	}
}

func TestRecvFSObjectCapBoundary(t *testing.T) {
	const maxObject = 64 * 1024
	h := recvFSHandlerWithCaps(maxObject, 1024*1024)

	t.Run("an object exactly at the cap is stored", func(t *testing.T) {
		stored := captureRecvFSStores(t)
		fsID, jsonData := recvFSExactDirObject(t, maxObject, 'a')
		w := postRecvFSBody(t, h, packSyncFSObjectForUnit(t, fsID, jsonData))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
		}
		if _, ok := (*stored)[fsID]; !ok || len(*stored) != 1 {
			t.Fatalf("stored %d object(s), want exactly %s", len(*stored), fsID)
		}
	})

	t.Run("one byte over the cap is rejected, not truncated", func(t *testing.T) {
		stored := captureRecvFSStores(t)
		fsID, jsonData := recvFSExactDirObject(t, maxObject+1, 'a')
		w := postRecvFSBody(t, h, packSyncFSObjectForUnit(t, fsID, jsonData))
		assertRecvFS413(t, w, "fs object too large when decompressed", "max_object_bytes", maxObject)
		if len(*stored) != 0 {
			t.Fatalf("stored %d object(s), want 0", len(*stored))
		}
	})
}

func TestRecvFSBatchCap(t *testing.T) {
	const maxObject = 64 * 1024
	const maxInflated = 160 * 1024
	h := recvFSHandlerWithCaps(maxObject, maxInflated)

	pack := func(sizes ...int) ([]byte, []string) {
		var body []byte
		var ids []string
		for i, size := range sizes {
			fsID, jsonData := recvFSExactDirObject(t, size, byte('a'+i))
			body = append(body, packSyncFSObjectForUnit(t, fsID, jsonData)...)
			ids = append(ids, fsID)
		}
		return body, ids
	}

	t.Run("a batch exactly at the cap is stored", func(t *testing.T) {
		stored := captureRecvFSStores(t)
		body, ids := pack(maxObject, maxObject, maxInflated-2*maxObject)
		w := postRecvFSBody(t, h, body)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
		}
		if len(*stored) != len(ids) {
			t.Fatalf("stored %d object(s), want %d", len(*stored), len(ids))
		}
	})

	// Every object is within the per-object cap; only the batch total is over.
	// Objects before the crossing one were already stored, as with any other
	// mid-batch rejection; the crossing object and anything after it are not.
	t.Run("a batch over the cap is rejected at the crossing object", func(t *testing.T) {
		stored := captureRecvFSStores(t)
		body, ids := pack(maxObject, maxObject, maxObject, maxObject)
		w := postRecvFSBody(t, h, body)
		assertRecvFS413(t, w, "recv-fs batch too large when decompressed", "max_inflated_bytes", maxInflated)
		if len(*stored) != 2 {
			t.Fatalf("stored %d object(s), want the 2 before the batch cap", len(*stored))
		}
		for _, id := range ids[2:] {
			if _, ok := (*stored)[id]; ok {
				t.Fatalf("object %s past the batch cap reached storage", id)
			}
		}
	})
}

// An object whose stream inflates fully and then fails its checksum is skipped,
// as before. Its inflated bytes still count toward the batch cap; otherwise a body
// of such objects would get unbounded inflate work while staying under the cap.
func TestRecvFSBatchCapCountsObjectsThatFailToDecompress(t *testing.T) {
	const maxObject = 64 * 1024
	const maxInflated = 160 * 1024
	h := recvFSHandlerWithCaps(maxObject, maxInflated)

	t.Run("under the cap they are skipped as before", func(t *testing.T) {
		stored := captureRecvFSStores(t)
		body := append(recvFSCorruptObject(t, maxObject, 'a'), recvFSCorruptObject(t, maxObject, 'b')...)
		w := postRecvFSBody(t, h, body)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
		}
		if len(*stored) != 0 {
			t.Fatalf("stored %d corrupt object(s), want 0", len(*stored))
		}
	})

	t.Run("their bytes count toward the cap", func(t *testing.T) {
		stored := captureRecvFSStores(t)
		var body []byte
		for i := 0; i < 3; i++ {
			body = append(body, recvFSCorruptObject(t, maxObject, byte('a'+i))...)
		}
		w := postRecvFSBody(t, h, body)
		assertRecvFS413(t, w, "recv-fs batch too large when decompressed", "max_inflated_bytes", maxInflated)
		if len(*stored) != 0 {
			t.Fatalf("stored %d corrupt object(s), want 0", len(*stored))
		}
	})
}

// TestRecvFSPreservesLegitimateBatch sends a batch shaped like a stock Seafile
// client's under the default caps: objects packed until the body reaches 1 MiB
// compressed (MAX_OBJECT_PACK_SIZE in seafile's http-tx-mgr.c), then one large
// file object, the block list of a 1 TiB file at the client's 6 MiB minimum CDC
// block. Random ids keep it as incompressible as real fs objects. Every object
// must be stored, with its dirents bytes and block ids exactly as sent.
func TestRecvFSPreservesLegitimateBatch(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	randomID := func() string {
		b := make([]byte, 20)
		rng.Read(b)
		return hex.EncodeToString(b)
	}

	type sent struct {
		dirents  string
		blockIDs []string
	}
	want := map[string]sent{}
	var body []byte
	add := func(jsonData []byte, s sent) {
		hash := sha1.Sum(jsonData)
		fsID := hex.EncodeToString(hash[:])
		want[fsID] = s
		body = append(body, packSyncFSObjectForUnit(t, fsID, jsonData)...)
	}
	fileObject := func(blocks int) {
		ids := make([]string, blocks)
		for i := range ids {
			ids[i] = randomID()
		}
		blockJSON, _ := json.Marshal(ids)
		add([]byte(`{"block_ids":`+string(blockJSON)+`,"size":1,"type":1,"version":1}`), sent{blockIDs: ids})
	}

	for len(body) < 1<<20 {
		var dirents strings.Builder
		dirents.WriteByte('[')
		for i := 0; i < 50; i++ {
			if i > 0 {
				dirents.WriteByte(',')
			}
			dirents.WriteString(`{"id":"` + randomID() + `","mode":33188,"modifier":"user@example.com","mtime":1768543179,"name":"` + randomID()[:12] + `.txt","size":4096}`)
		}
		dirents.WriteByte(']')
		add([]byte(`{"dirents":`+dirents.String()+`,"type":3,"version":1}`), sent{dirents: dirents.String()})
		fileObject(200)
	}
	fileObject(int((1 << 40) / (6 << 20)))

	stored := captureRecvFSStores(t)
	w := postRecvFSBody(t, &SyncHandler{}, body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	t.Logf("%d objects, %d compressed bytes", len(want), len(body))
	if len(*stored) != len(want) {
		t.Fatalf("stored %d object(s), want %d", len(*stored), len(want))
	}
	for fsID, s := range want {
		got := (*stored)[fsID]
		if s.blockIDs == nil {
			if got.dirEntries != s.dirents {
				t.Fatalf("object %s dirents changed in storage", fsID)
			}
			continue
		}
		if !syncStringSlicesEqual(got.wireBlockIDs, s.blockIDs) {
			t.Fatalf("object %s block ids changed in storage", fsID)
		}
	}
}

// TestRecvFSMaxInt64CapsDoNotOverflow pins the sentinel read at the top of the
// int64 range. Validate accepts math.MaxInt64 for both caps; limit+1 then
// overflowed to a negative LimitReader that read nothing, so a valid object
// came back empty and failed its fs_id check instead of being stored.
func TestRecvFSMaxInt64CapsDoNotOverflow(t *testing.T) {
	fsID, jsonData := recvFSExactDirObject(t, 4096, 'a')
	body := packSyncFSObjectForUnit(t, fsID, jsonData)

	got, err := inflateRecvFSObject(body[44:], math.MaxInt64)
	if err != nil || !bytes.Equal(got, jsonData) {
		t.Fatalf("inflate at MaxInt64 = (%d bytes, %v), want the %d-byte object", len(got), err, len(jsonData))
	}

	stored := captureRecvFSStores(t)
	w := postRecvFSBody(t, recvFSHandlerWithCaps(math.MaxInt64, math.MaxInt64), body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if _, ok := (*stored)[fsID]; !ok || len(*stored) != 1 {
		t.Fatalf("stored %d object(s), want exactly %s", len(*stored), fsID)
	}
}
