//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestCreateDirectory(t *testing.T) {
	name := fmt.Sprintf("inttest-dir-%d", time.Now().UnixNano())
	repoID := createTestLibrary(t, adminClient, name)

	// Create directory
	resp := adminClient.PostJSON(t, fmt.Sprintf("/api/v2.1/repos/%s/dir/?p=/test-dir", repoID), map[string]string{})
	expectStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	// List root and verify directory exists
	listResp := adminClient.Get(t, fmt.Sprintf("/api/v2.1/repos/%s/dir/?p=/", repoID))
	expectStatus(t, listResp, http.StatusOK)

	var dirList map[string]interface{}
	decodeJSON(t, listResp, &dirList)

	entries, ok := dirList["dirent_list"].([]interface{})
	if !ok {
		t.Fatal("expected dirent_list array in response")
	}

	if !containsEntry(entries, "name", "test-dir") {
		t.Error("created directory 'test-dir' not found in listing")
	}
}

func TestFileUpload(t *testing.T) {
	name := fmt.Sprintf("inttest-upload-%d", time.Now().UnixNano())
	repoID := createTestLibrary(t, adminClient, name)

	// Create a test file using the api2 create endpoint
	values := url.Values{}
	resp := adminClient.PostForm(t, fmt.Sprintf("/api2/repos/%s/file/?p=/upload-test.txt&operation=create", repoID), values)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 or 201 for file create, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Verify file appears in directory listing
	listResp := adminClient.Get(t, fmt.Sprintf("/api/v2.1/repos/%s/dir/?p=/", repoID))
	expectStatus(t, listResp, http.StatusOK)

	var dirList map[string]interface{}
	decodeJSON(t, listResp, &dirList)

	entries, ok := dirList["dirent_list"].([]interface{})
	if !ok {
		t.Fatal("expected dirent_list array in response")
	}

	if !containsEntry(entries, "name", "upload-test.txt") {
		t.Error("created file 'upload-test.txt' not found in listing")
	}
}

func TestFileDownload(t *testing.T) {
	name := fmt.Sprintf("inttest-download-%d", time.Now().UnixNano())
	repoID := createTestLibrary(t, adminClient, name)

	// Create a test file
	values := url.Values{}
	createResp := adminClient.PostForm(t, fmt.Sprintf("/api2/repos/%s/file/?p=/download-test.txt&operation=create", repoID), values)
	if createResp.StatusCode != http.StatusCreated && createResp.StatusCode != http.StatusOK {
		t.Fatalf("failed to create test file, got %d", createResp.StatusCode)
	}
	createResp.Body.Close()

	// Get download link
	dlResp := adminClient.Get(t, fmt.Sprintf("/api2/repos/%s/file/?p=/download-test.txt", repoID))
	if dlResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for download link, got %d", dlResp.StatusCode)
	}
	dlResp.Body.Close()
}

func TestFileMoveAndCopy(t *testing.T) {
	name := fmt.Sprintf("inttest-movecopy-%d", time.Now().UnixNano())
	repoID := createTestLibrary(t, adminClient, name)

	// Create source and target dirs
	resp := adminClient.PostJSON(t, fmt.Sprintf("/api/v2.1/repos/%s/dir/?p=/src-dir", repoID), map[string]string{})
	expectStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	resp = adminClient.PostJSON(t, fmt.Sprintf("/api/v2.1/repos/%s/dir/?p=/dst-dir", repoID), map[string]string{})
	expectStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	// Create a test item in source dir
	resp = adminClient.PostJSON(t, fmt.Sprintf("/api/v2.1/repos/%s/dir/?p=/src-dir/item-to-move", repoID), map[string]string{})
	expectStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	resp = adminClient.PostJSON(t, fmt.Sprintf("/api/v2.1/repos/%s/dir/?p=/src-dir/item-to-copy", repoID), map[string]string{})
	expectStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	t.Run("move item", func(t *testing.T) {
		moveBody := map[string]interface{}{
			"src_repo_id":    repoID,
			"src_parent_dir": "/src-dir",
			"dst_repo_id":    repoID,
			"dst_parent_dir": "/dst-dir",
			"src_dirents":    []string{"item-to-move"},
		}
		moveResp := adminClient.PostJSON(t, "/api/v2.1/repos/sync-batch-move-item/", moveBody)
		expectStatus(t, moveResp, http.StatusOK)
		moveResp.Body.Close()

		// Verify item is in dst
		listResp := adminClient.Get(t, fmt.Sprintf("/api/v2.1/repos/%s/dir/?p=/dst-dir", repoID))
		expectStatus(t, listResp, http.StatusOK)

		var dirList map[string]interface{}
		decodeJSON(t, listResp, &dirList)
		entries, _ := dirList["dirent_list"].([]interface{})
		if !containsEntry(entries, "name", "item-to-move") {
			t.Error("moved item not found in dst-dir")
		}
	})

	t.Run("copy item", func(t *testing.T) {
		copyBody := map[string]interface{}{
			"src_repo_id":    repoID,
			"src_parent_dir": "/src-dir",
			"dst_repo_id":    repoID,
			"dst_parent_dir": "/dst-dir",
			"src_dirents":    []string{"item-to-copy"},
		}
		copyResp := adminClient.PostJSON(t, "/api/v2.1/repos/sync-batch-copy-item/", copyBody)
		expectStatus(t, copyResp, http.StatusOK)
		copyResp.Body.Close()

		// Verify original still exists
		srcResp := adminClient.Get(t, fmt.Sprintf("/api/v2.1/repos/%s/dir/?p=/src-dir", repoID))
		expectStatus(t, srcResp, http.StatusOK)

		var srcList map[string]interface{}
		decodeJSON(t, srcResp, &srcList)
		srcEntries, _ := srcList["dirent_list"].([]interface{})
		if !containsEntry(srcEntries, "name", "item-to-copy") {
			t.Error("original item missing from src-dir after copy")
		}

		// Verify copy exists in dst
		dstResp := adminClient.Get(t, fmt.Sprintf("/api/v2.1/repos/%s/dir/?p=/dst-dir", repoID))
		expectStatus(t, dstResp, http.StatusOK)

		var dstList map[string]interface{}
		decodeJSON(t, dstResp, &dstList)
		dstEntries, _ := dstList["dirent_list"].([]interface{})
		if !containsEntry(dstEntries, "name", "item-to-copy") {
			t.Error("copied item not found in dst-dir")
		}
	})
}

func TestFileDelete(t *testing.T) {
	name := fmt.Sprintf("inttest-filedelete-%d", time.Now().UnixNano())
	repoID := createTestLibrary(t, adminClient, name)

	// Create a file
	values := url.Values{}
	createResp := adminClient.PostForm(t, fmt.Sprintf("/api2/repos/%s/file/?p=/delete-me.txt&operation=create", repoID), values)
	if createResp.StatusCode != http.StatusCreated && createResp.StatusCode != http.StatusOK {
		t.Fatalf("failed to create test file, got %d", createResp.StatusCode)
	}
	createResp.Body.Close()

	// Delete the file
	delResp := adminClient.Delete(t, fmt.Sprintf("/api2/repos/%s/file/?p=/delete-me.txt", repoID))
	expectStatus(t, delResp, http.StatusOK)
	delResp.Body.Close()

	// Verify file is gone from listing
	listResp := adminClient.Get(t, fmt.Sprintf("/api/v2.1/repos/%s/dir/?p=/", repoID))
	expectStatus(t, listResp, http.StatusOK)

	var dirList map[string]interface{}
	decodeJSON(t, listResp, &dirList)
	entries, _ := dirList["dirent_list"].([]interface{})
	if containsEntry(entries, "name", "delete-me.txt") {
		t.Error("deleted file still found in listing")
	}
}
