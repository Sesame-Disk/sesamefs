package v2

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/config"
	"github.com/Sesame-Disk/sesamefs/internal/crypto"
	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	"github.com/gin-gonic/gin"
)

// ShareLinkViewHandler serves the public share link pages and APIs
type ShareLinkViewHandler struct {
	db             *db.DB
	config         *config.Config
	storage        *storage.S3Store
	storageManager *storage.Manager
	tokenCreator   TokenCreator
	serverURL      string
	// bundleMap maps entry point names (e.g. "sharedDirView") to hashed filenames
	jsBundleMap  map[string]string
	cssBundleMap map[string]string
}

// NewShareLinkViewHandler creates a new ShareLinkViewHandler and scans frontend bundles
func NewShareLinkViewHandler(database *db.DB, cfg *config.Config, s3Store *storage.S3Store, storageManager *storage.Manager, tokenCreator TokenCreator, serverURL string) *ShareLinkViewHandler {
	h := &ShareLinkViewHandler{
		db:             database,
		config:         cfg,
		storage:        s3Store,
		storageManager: storageManager,
		tokenCreator:   tokenCreator,
		serverURL:      serverURL,
	}
	h.jsBundleMap = scanBundles("./frontend/build/static/js", ".js")
	h.cssBundleMap = scanBundles("./frontend/build/static/css", ".css")
	return h
}

// scanBundles scans a directory for hashed bundle files and returns a map
// of entry name -> hashed filename (e.g. "sharedDirView" -> "sharedDirView.ef3d8149.js")
func scanBundles(dir, ext string) map[string]string {
	result := make(map[string]string)
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Warn("Failed to scan bundle directory, using fallback bundle names", "dir", dir, "error", err)
		// Return fallback bundle names when directory scan fails
		// These are the bundle names from the frontend build
		if ext == ".js" {
			return getJSBundleFallbacks()
		}
		if ext == ".css" {
			return getCSSBundleFallbacks()
		}
		return result
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ext) && !strings.HasSuffix(name, ".map") && !strings.HasSuffix(name, ".LICENSE.txt") {
			// Extract entry name: "sharedDirView.ef3d8149.js" -> "sharedDirView"
			parts := strings.SplitN(name, ".", 2)
			if len(parts) >= 2 {
				result[parts[0]] = name
			}
		}
	}
	return result
}

// getJSBundleFallbacks returns hardcoded JS bundle filenames
// These should be updated when the frontend is rebuilt
func getJSBundleFallbacks() map[string]string {
	return map[string]string{
		"runtime":                   "runtime.b5726b5c.js",
		"commons":                   "commons.e950012e.js",
		"sharedDirView":             "sharedDirView.ef3d8149.js",
		"sharedFileViewAudio":       "sharedFileViewAudio.cedd033e.js",
		"sharedFileViewDocument":    "sharedFileViewDocument.c3f72eff.js",
		"sharedFileViewImage":       "sharedFileViewImage.9d0dda04.js",
		"sharedFileViewMarkdown":    "sharedFileViewMarkdown.f8135e49.js",
		"sharedFileViewPDF":         "sharedFileViewPDF.a00415f0.js",
		"sharedFileViewSdoc":        "sharedFileViewSdoc.00bab9a5.js",
		"sharedFileViewSpreadsheet": "sharedFileViewSpreadsheet.ea813efa.js",
		"sharedFileViewSVG":         "sharedFileViewSVG.5fd43385.js",
		"sharedFileViewText":        "sharedFileViewText.757e8d1a.js",
		"sharedFileViewUnknown":     "sharedFileViewUnknown.a0e468e0.js",
		"sharedFileViewVideo":       "sharedFileViewVideo.6af2fa31.js",
		"uploadLink":                "uploadLink.5d49e522.js",
	}
}

// getCSSBundleFallbacks returns hardcoded CSS bundle filenames
func getCSSBundleFallbacks() map[string]string {
	return map[string]string{
		"commons":                   "commons.82d1af8c.css",
		"sharedDirView":             "sharedDirView.b715f1e6.css",
		"sharedFileViewSpreadsheet": "sharedFileViewSpreadsheet.ff1ddac7.css",
		"uploadLink":                "uploadLink.d59e882a.css",
	}
}

// shareLinkData holds the resolved share link info for rendering
type shareLinkData struct {
	token       string
	orgID       string
	libraryID   string
	filePath    string
	permission  string
	createdBy   string
	creatorName string
	isExpired   bool
	repoName    string
	commitID    string
	isDir       bool
	targetEntry *FSEntry
	// Parsed permissions (handles both string and JSON formats)
	canEdit     bool
	canDownload bool
	canUpload   bool
	// isDirShareLink indicates this file is being accessed via a directory share link
	// (i.e., /d/:token/files/?p=path rather than /d/:token directly)
	isDirShareLink bool
	// fileSubPath is the relative path within the shared directory (the ?p= parameter)
	fileSubPath string
}

// resolveShareLink looks up and validates a share link token
func (h *ShareLinkViewHandler) resolveShareLink(token string) (*shareLinkData, error) {
	var orgID, libraryID, filePath, permission, createdBy string
	var expiresAt *time.Time
	var downloadCount int
	var maxDownloads *int

	err := h.db.Session().Query(`
		SELECT org_id, library_id, file_path, permission, created_by, expires_at, download_count, max_downloads
		FROM share_links WHERE share_token = ?
	`, token).Scan(&orgID, &libraryID, &filePath, &permission, &createdBy, &expiresAt, &downloadCount, &maxDownloads)
	if err != nil {
		return nil, fmt.Errorf("share link not found")
	}

	// Check expiration
	isExpired := false
	if expiresAt != nil && time.Now().After(*expiresAt) {
		isExpired = true
	}

	// Check max downloads
	if maxDownloads != nil && downloadCount >= *maxDownloads {
		isExpired = true
	}

	// Get library name and head commit ID from libraries table (requires org_id)
	var repoName, commitID string
	h.db.Session().Query(`SELECT name, head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, libraryID).Scan(&repoName, &commitID)
	if repoName == "" {
		repoName = "Shared"
	}

	// Parse permissions - handle both string and JSON formats
	canEdit, canDownload, canUpload := parseShareLinkPermission(permission)

	// Look up creator name for "Shared by" display
	var creatorName, creatorEmail string
	h.db.Session().Query(`SELECT name, email FROM users WHERE org_id = ? AND user_id = ?`, orgID, createdBy).Scan(&creatorName, &creatorEmail)
	if creatorName == "" {
		creatorName = creatorEmail // fallback to email if name is empty
	}
	if creatorName == "" {
		creatorName = createdBy // fallback to UUID if nothing found
	}

	return &shareLinkData{
		token:       token,
		orgID:       orgID,
		libraryID:   libraryID,
		filePath:    filePath,
		permission:  permission,
		createdBy:   createdBy,
		creatorName: creatorName,
		isExpired:   isExpired,
		repoName:    repoName,
		commitID:    commitID,
		canEdit:     canEdit,
		canDownload: canDownload,
		canUpload:   canUpload,
	}, nil
}

// parseShareLinkPermission parses permission which can be either:
// - A simple string: "download", "preview_download", "preview_only", "upload", "edit"
// - A JSON object: {"can_edit":false,"can_download":true,"can_upload":false}
func parseShareLinkPermission(permission string) (canEdit, canDownload, canUpload bool) {
	// Try parsing as JSON first
	if strings.HasPrefix(permission, "{") {
		var perms struct {
			CanEdit     bool `json:"can_edit"`
			CanDownload bool `json:"can_download"`
			CanUpload   bool `json:"can_upload"`
		}
		if err := json.Unmarshal([]byte(permission), &perms); err == nil {
			return perms.CanEdit, perms.CanDownload, perms.CanUpload
		}
	}

	// Handle string format
	switch permission {
	case "edit":
		return true, true, true
	case "upload":
		return false, false, true
	case "download", "preview_download":
		return false, true, false
	case "preview_only":
		return false, false, false
	default:
		// Default to download allowed for backwards compatibility
		return false, true, false
	}
}

// ServeShareLinkPage handles GET /d/:token
func (h *ShareLinkViewHandler) ServeShareLinkPage(c *gin.Context) {
	token := c.Param("token")

	sl, err := h.resolveShareLink(token)
	if err != nil {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusNotFound, errorPageHTML("Not Found", "This share link does not exist."))
		return
	}

	if sl.isExpired {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusGone, errorPageHTML("Link Expired", "This share link has expired or reached its download limit."))
		return
	}

	// Determine if this is a file or directory share
	fsHelper := NewFSHelper(h.db)
	rootFSID, _, err := fsHelper.GetRootFSID(sl.libraryID)
	if err != nil {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusInternalServerError, errorPageHTML("Error", "Failed to access the shared library."))
		return
	}

	sharePath := sl.filePath
	if sharePath == "" {
		sharePath = "/"
	}

	isDir := false

	if sharePath == "/" {
		isDir = true
	} else {
		result, err := fsHelper.TraverseToPathFromRoot(sl.libraryID, rootFSID, sharePath)
		if err != nil {
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.String(http.StatusNotFound, errorPageHTML("Not Found", "The shared file or folder could not be found."))
			return
		}
		if result.TargetEntry != nil {
			sl.targetEntry = result.TargetEntry
			isDir = result.TargetEntry.Mode == ModeDir || result.TargetEntry.Mode&0170000 == 040000
		} else {
			// Path not found in the FS tree
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.String(http.StatusNotFound, errorPageHTML("Not Found", "The shared file or folder could not be found."))
			return
		}
	}

	sl.isDir = isDir

	// Handle direct download (?dl=1)
	if c.Query("dl") == "1" && !isDir {
		h.handleShareLinkDownload(c, sl, fsHelper, rootFSID)
		return
	}

	// Handle raw file content (?raw=1) for inline preview (images, PDFs, etc.)
	if c.Query("raw") == "1" && !isDir {
		h.handleShareLinkRaw(c, sl)
		return
	}

	// Serve the appropriate HTML page
	if isDir {
		h.serveSharedDirPage(c, sl)
	} else {
		h.serveSharedFilePage(c, sl)
	}
}

// handleShareLinkDownload handles ?dl=1 for file share links
func (h *ShareLinkViewHandler) handleShareLinkDownload(c *gin.Context, sl *shareLinkData, fsHelper *FSHelper, rootFSID string) {
	// Check download permission
	if !sl.canDownload {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusForbidden, errorPageHTML("Download Disabled", "Downloading is not allowed for this share link."))
		return
	}

	filename := filepath.Base(sl.filePath)

	// Generate download token using the share link creator's user ID
	downloadToken, err := h.tokenCreator.CreateDownloadToken(sl.orgID, sl.libraryID, sl.filePath, sl.createdBy)
	if err != nil {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusInternalServerError, errorPageHTML("Download Error", "Failed to generate download link."))
		return
	}

	downloadURL := getBrowserURL(c, h.serverURL) + "/seafhttp/files/" + downloadToken + "/" + filename
	c.Redirect(http.StatusFound, downloadURL)
}

// handleShareLinkRaw serves the raw file content for inline preview (images, PDFs, videos, etc.)
func (h *ShareLinkViewHandler) handleShareLinkRaw(c *gin.Context, sl *shareLinkData) {
	if sl.targetEntry == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}

	filename := filepath.Base(sl.filePath)
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))

	// Get the file's block IDs and size from the fs_object
	var blockIDs []string
	var fileSize int64
	err := h.db.Session().Query(`
		SELECT block_ids, size_bytes FROM fs_objects WHERE library_id = ? AND fs_id = ?
	`, sl.libraryID, sl.targetEntry.ID).Scan(&blockIDs, &fileSize)
	if err != nil {
		slog.Error("Failed to get file block IDs for share link raw", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get file metadata"})
		return
	}

	blockStore, _, err := h.storageManager.GetHealthyBlockStore("")
	if err != nil {
		slog.Error("Block store not available for share link raw", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage not available"})
		return
	}

	// Check if library is encrypted
	var encrypted bool
	h.db.Session().Query(`SELECT encrypted FROM libraries WHERE org_id = ? AND library_id = ?`,
		sl.orgID, sl.libraryID).Scan(&encrypted)

	var fileKey []byte
	if encrypted {
		fileKey = GetDecryptSessions().GetFileKey(sl.createdBy, sl.libraryID)
		if fileKey == nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "library is encrypted but not unlocked"})
			return
		}
	}

	// Determine MIME type from extension
	mimeType := mime.TypeByExtension("." + ext)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	// Stream blocks directly to response — O(block_size) RAM
	c.Header("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, filename))
	c.Header("Cache-Control", "private, max-age=3600")
	c.Header("Content-Type", mimeType)
	if fileSize > 0 && !encrypted {
		c.Header("Content-Length", strconv.FormatInt(fileSize, 10))
	}
	c.Status(http.StatusOK)

	ctx := c.Request.Context()
	for _, blockID := range blockIDs {
		internalID := resolveBlockIDFileView(h.db, sl.orgID, blockID)

		if encrypted && fileKey != nil {
			blockData, err := blockStore.GetBlock(ctx, internalID)
			if err != nil {
				slog.Error("Failed to read block for share link raw", "blockID", internalID, "error", err)
				return
			}
			blockData, err = crypto.DecryptBlock(blockData, fileKey)
			if err != nil {
				slog.Error("Failed to decrypt block", "error", err)
				return
			}
			c.Writer.Write(blockData)
		} else {
			reader, err := blockStore.GetBlockReader(ctx, internalID)
			if err != nil {
				slog.Error("Failed to read block for share link raw", "blockID", internalID, "error", err)
				return
			}
			io.Copy(c.Writer, reader)
			reader.Close()
		}
		c.Writer.Flush()
	}
}

// serveSharedDirPage renders the shared directory view
func (h *ShareLinkViewHandler) serveSharedDirPage(c *gin.Context, sl *shareLinkData) {
	dirName := filepath.Base(sl.filePath)
	if sl.filePath == "/" || sl.filePath == "" {
		dirName = sl.repoName
	}

	// Get browsing path from query parameter (for navigating into subdirectories)
	relativePath := c.DefaultQuery("p", "/")
	if relativePath == "" {
		relativePath = "/"
	}
	mode := c.DefaultQuery("mode", "list")
	thumbnailSize := 48

	// Build the zipped breadcrumb path array
	// zipped is [{name, path}, ...] for breadcrumb navigation
	zippedJSON := buildZippedPath(dirName, relativePath)

	// dirPath is the full filesystem path: sharePath + relativePath
	dirPath := sl.filePath
	if dirPath == "" || dirPath == "/" {
		dirPath = relativePath
	} else if relativePath != "/" {
		dirPath = strings.TrimSuffix(sl.filePath, "/") + "/" + strings.TrimPrefix(relativePath, "/")
	}

	pageOptions := fmt.Sprintf(`{
		"token": %q,
		"repoID": %q,
		"repoName": %q,
		"path": %q,
		"dirName": %q,
		"dirPath": %q,
		"relativePath": %q,
		"mode": %q,
		"thumbnailSize": %d,
		"zipped": %s,
		"canDownload": %t,
		"canUpload": %t,
		"sharedBy": %q,
		"noPassword": true,
		"noQuota": false,
		"trafficOverLimit": false,
		"enableVideoThumbnail": false,
		"permissions": {"can_edit": %t, "can_download": %t, "can_upload": %t}
	}`,
		sl.token,
		sl.libraryID,
		html.EscapeString(sl.repoName),
		sl.filePath,
		html.EscapeString(dirName),
		dirPath,
		relativePath,
		mode,
		thumbnailSize,
		zippedJSON,
		sl.canDownload,
		sl.canUpload,
		html.EscapeString(sl.creatorName),
		sl.canEdit,
		sl.canDownload,
		sl.canUpload,
	)

	htmlPage := h.buildSharePageHTML("sharedDirView", dirName+" - SesameFS", pageOptions)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, htmlPage)
}

// buildZippedPath builds the breadcrumb JSON array for shared dir navigation
// Returns JSON like [{"name":"Root","path":"/"},{"name":"subfolder","path":"/subfolder/"}]
func buildZippedPath(rootName, relativePath string) string {
	type pathSegment struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}

	segments := []pathSegment{{Name: rootName, Path: "/"}}

	if relativePath != "/" && relativePath != "" {
		// Split path and build cumulative breadcrumbs
		parts := strings.Split(strings.Trim(relativePath, "/"), "/")
		cumPath := "/"
		for _, part := range parts {
			if part == "" {
				continue
			}
			cumPath += part + "/"
			segments = append(segments, pathSegment{Name: part, Path: cumPath})
		}
	}

	data, err := json.Marshal(segments)
	if err != nil {
		return `[{"name":"Root","path":"/"}]`
	}
	return string(data)
}

// readFileContentAsText reads the file content from block storage and returns it as a string.
// Used for embedding text file content directly in page options (for the text/markdown React views).
// Returns empty string on any error. Limited to 1MB to avoid huge page payloads.
func (h *ShareLinkViewHandler) readFileContentAsText(sl *shareLinkData) string {
	if sl.targetEntry == nil {
		return ""
	}

	const maxTextSize = 1 * 1024 * 1024 // 1MB limit for inline text content

	var blockIDs []string
	var fileSize int64
	err := h.db.Session().Query(`
		SELECT block_ids, size_bytes FROM fs_objects WHERE library_id = ? AND fs_id = ?
	`, sl.libraryID, sl.targetEntry.ID).Scan(&blockIDs, &fileSize)
	if err != nil {
		slog.Error("Failed to get file block IDs for text content", "error", err)
		return ""
	}

	if fileSize > maxTextSize {
		return ""
	}

	blockStore, _, err := h.storageManager.GetHealthyBlockStore("")
	if err != nil {
		return ""
	}

	// Check if library is encrypted
	var encrypted bool
	h.db.Session().Query(`SELECT encrypted FROM libraries WHERE org_id = ? AND library_id = ?`,
		sl.orgID, sl.libraryID).Scan(&encrypted)

	var fileKey []byte
	if encrypted {
		fileKey = GetDecryptSessions().GetFileKey(sl.createdBy, sl.libraryID)
		if fileKey == nil {
			return ""
		}
	}

	ctx := context.Background()
	var buf strings.Builder
	for _, blockID := range blockIDs {
		internalID := resolveBlockIDFileView(h.db, sl.orgID, blockID)

		if encrypted && fileKey != nil {
			blockData, err := blockStore.GetBlock(ctx, internalID)
			if err != nil {
				return ""
			}
			blockData, err = crypto.DecryptBlock(blockData, fileKey)
			if err != nil {
				return ""
			}
			buf.Write(blockData)
		} else {
			blockData, err := blockStore.GetBlock(ctx, internalID)
			if err != nil {
				return ""
			}
			buf.Write(blockData)
		}
	}

	return buf.String()
}

// serveSharedFilePage renders the shared file view
func (h *ShareLinkViewHandler) serveSharedFilePage(c *gin.Context, sl *shareLinkData) {
	filename := filepath.Base(sl.filePath)
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))

	// Build raw file path for preview (serves actual file content with correct MIME type)
	// For files inside a shared directory, we need /d/{token}/files/?p={path}&raw=1
	// For direct file share links, we use /d/{token}?raw=1
	var rawPath string
	if sl.isDirShareLink {
		rawPath = fmt.Sprintf("/d/%s/files/?p=%s&raw=1", sl.token, url.QueryEscape(sl.fileSubPath))
	} else {
		rawPath = fmt.Sprintf("/d/%s?raw=1", sl.token)
	}

	var fileSize int64
	if sl.targetEntry != nil {
		fileSize = sl.targetEntry.Size
	}

	// For PDFs and certain file types, use server-rendered preview page with embedded viewer
	if useEmbeddedPreview(ext) {
		htmlPage := h.buildEmbeddedPreviewPage(filename, ext, rawPath, fileSize, sl)
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, htmlPage)
		return
	}

	// For document types (docx, xlsx, pptx, etc.), embed OnlyOffice viewer in the preview page
	if h.config.OnlyOffice.Enabled && isOnlyOfficeViewable(ext) {
		htmlPage, err := h.buildOnlyOfficePreviewPage(filename, ext, fileSize, sl)
		if err == nil {
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.String(http.StatusOK, htmlPage)
			return
		}
		// Fall through to React bundle if OnlyOffice preview fails
		slog.Warn("OnlyOffice preview failed, falling back to React bundle", "file", filename, "error", err)
	}

	bundleName := extensionToBundleName(ext)

	// For unknown file types (no preview), show a clean download page instead of a broken React bundle
	if bundleName == "sharedFileViewUnknown" {
		htmlPage := h.buildEmbeddedPreviewPage(filename, ext, "", fileSize, sl)
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, htmlPage)
		return
	}

	// For text files, read file content and embed it directly (the React component expects fileContent)
	var fileContentJSON string
	if bundleName == "sharedFileViewText" || bundleName == "sharedFileViewMarkdown" {
		content := h.readFileContentAsText(sl)
		contentBytes, err := json.Marshal(content)
		if err != nil {
			fileContentJSON = `""`
		} else {
			fileContentJSON = string(contentBytes)
		}
	} else {
		fileContentJSON = `""`
	}

	pageOptions := fmt.Sprintf(`{
		"sharedToken": %q,
		"repoID": %q,
		"commitID": %q,
		"filePath": %q,
		"fileName": %q,
		"fileSize": %d,
		"rawPath": %q,
		"canDownload": %t,
		"canEdit": %t,
		"sharedBy": %q,
		"noPassword": true,
		"trafficOverLimit": false,
		"fileExt": %q,
		"siteName": "SesameFS",
		"enableWatermark": false,
		"zipped": null,
		"enableShareLinkReportAbuse": false,
		"fileContent": %s,
		"err": ""
	}`,
		sl.token,
		sl.libraryID,
		sl.commitID,
		sl.filePath,
		html.EscapeString(filename),
		fileSize,
		rawPath,
		sl.canDownload,
		sl.canEdit,
		html.EscapeString(sl.creatorName),
		ext,
		fileContentJSON,
	)

	htmlPage := h.buildSharePageHTML(bundleName, filename+" - SesameFS", pageOptions)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, htmlPage)
}

// useEmbeddedPreview returns true for file types that should use the server-rendered
// embedded preview page instead of React bundles
func useEmbeddedPreview(ext string) bool {
	switch ext {
	case "pdf":
		return true
	case "png", "jpg", "jpeg", "gif", "bmp", "webp", "svg", "ico", "tiff", "tif":
		return true
	case "mp4", "webm", "ogg", "mov":
		return true
	case "mp3", "wav", "flac", "aac":
		return true
	}
	return false
}

// buildEmbeddedPreviewPage generates a clean HTML page with embedded file preview
func (h *ShareLinkViewHandler) buildEmbeddedPreviewPage(filename, ext, rawPath string, fileSize int64, sl *shareLinkData) string {
	safeFilename := html.EscapeString(filename)
	safeSharedBy := html.EscapeString(sl.creatorName)
	// For files inside a shared directory, download link needs the file path
	var downloadLink string
	if sl.isDirShareLink {
		downloadLink = fmt.Sprintf("/d/%s/files/?p=%s&dl=1", sl.token, url.QueryEscape(sl.fileSubPath))
	} else {
		downloadLink = fmt.Sprintf("/d/%s?dl=1", sl.token)
	}
	fileSizeStr := formatFileSize(fileSize)

	// Build the preview content based on file type
	var previewContent string
	switch {
	case ext == "pdf":
		previewContent = fmt.Sprintf(`<embed src="%s" type="application/pdf" width="100%%" height="100%%" style="border:none;" />`, html.EscapeString(rawPath))
	case ext == "png" || ext == "jpg" || ext == "jpeg" || ext == "gif" || ext == "bmp" || ext == "webp" || ext == "svg" || ext == "ico" || ext == "tiff" || ext == "tif":
		previewContent = fmt.Sprintf(`<div style="text-align:center;padding:20px;overflow:auto;height:100%%;">
			<img src="%s" alt="%s" style="max-width:100%%;max-height:100%%;object-fit:contain;" />
		</div>`, html.EscapeString(rawPath), safeFilename)
	case ext == "mp4" || ext == "webm" || ext == "ogg" || ext == "mov":
		previewContent = fmt.Sprintf(`<div style="display:flex;align-items:center;justify-content:center;height:100%%;background:#000;">
			<video controls style="max-width:100%%;max-height:100%%;" src="%s">Your browser does not support video playback.</video>
		</div>`, html.EscapeString(rawPath))
	case ext == "mp3" || ext == "wav" || ext == "flac" || ext == "aac":
		previewContent = fmt.Sprintf(`<div style="display:flex;align-items:center;justify-content:center;height:100%%;background:#f8f9fa;">
			<audio controls src="%s" style="width:80%%;max-width:600px;">Your browser does not support audio playback.</audio>
		</div>`, html.EscapeString(rawPath))
	default:
		previewContent = `<div style="display:flex;align-items:center;justify-content:center;height:100%;color:#666;">
			<p>Preview not available for this file type.</p>
		</div>`
	}

	// Build download button HTML
	var downloadBtn string
	if sl.canDownload {
		downloadBtn = fmt.Sprintf(`<a href="%s" class="btn-download">Download (%s)</a>`, html.EscapeString(downloadLink), fileSizeStr)
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>%s - SesameFS</title>
    <link rel="icon" type="image/x-icon" href="/favicon.png">
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif; height: 100vh; display: flex; flex-direction: column; background: #f5f5f5; color: #333; }
        .header { background: #fff; border-bottom: 1px solid #e0e0e0; padding: 12px 24px; display: flex; align-items: center; justify-content: space-between; flex-shrink: 0; box-shadow: 0 1px 3px rgba(0,0,0,0.05); }
        .header-left { display: flex; align-items: center; gap: 16px; min-width: 0; }
        .logo { height: 28px; flex-shrink: 0; }
        .file-info { min-width: 0; }
        .file-name { font-size: 16px; font-weight: 600; color: #1a1a1a; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 600px; }
        .shared-by { font-size: 13px; color: #666; margin-top: 2px; }
        .header-right { display: flex; align-items: center; gap: 12px; flex-shrink: 0; }
        .btn-download { display: inline-flex; align-items: center; padding: 8px 20px; background: #f7931e; color: #fff; text-decoration: none; border-radius: 6px; font-size: 14px; font-weight: 500; transition: background 0.15s; }
        .btn-download:hover { background: #e8850f; }
        .preview-container { flex: 1; overflow: hidden; }
        .preview-container embed, .preview-container iframe { display: block; }
        @media (max-width: 768px) {
            .header { padding: 10px 16px; flex-wrap: wrap; gap: 8px; }
            .file-name { max-width: 100%%; font-size: 14px; }
        }
    </style>
</head>
<body>
    <div class="header">
        <div class="header-left">
            <a href="/"><img src="/static/img/logo.png" alt="SesameFS" class="logo" onerror="this.style.display='none'" /></a>
            <div class="file-info">
                <div class="file-name" title="%s">%s</div>
                <div class="shared-by">Shared by %s</div>
            </div>
        </div>
        <div class="header-right">
            %s
        </div>
    </div>
    <div class="preview-container">
        %s
    </div>
</body>
</html>`,
		safeFilename,
		safeFilename, safeFilename,
		safeSharedBy,
		downloadBtn,
		previewContent,
	)
}

// formatFileSize formats bytes into a human-readable string
func formatFileSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	}
	if size < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	}
	return fmt.Sprintf("%.1f GB", float64(size)/(1024*1024*1024))
}

// isOnlyOfficeViewable checks if a file extension can be viewed with OnlyOffice
func isOnlyOfficeViewable(ext string) bool {
	switch ext {
	case "doc", "docx", "odt", "fodt", "rtf",
		"xls", "xlsx", "ods", "fods", "csv",
		"ppt", "pptx", "odp", "fodp",
		"pdf":
		return true
	}
	return false
}

// buildOnlyOfficePreviewPage generates an embedded preview page with the OnlyOffice viewer
// inside the standard share link layout (header + preview container), not a full-page editor.
func (h *ShareLinkViewHandler) buildOnlyOfficePreviewPage(filename, ext string, fileSize int64, sl *shareLinkData) (string, error) {
	// Generate download token so OnlyOffice server can fetch the document
	downloadToken, err := h.tokenCreator.CreateDownloadToken(sl.orgID, sl.libraryID, sl.filePath, sl.createdBy)
	if err != nil {
		return "", fmt.Errorf("failed to create download token: %w", err)
	}

	// Use OnlyOffice-specific server URL for download (URL that OnlyOffice can reach)
	ooServerURL := h.config.OnlyOffice.ServerURL
	if ooServerURL == "" {
		ooServerURL = h.serverURL
	}
	downloadURL := ooServerURL + "/seafhttp/files/" + downloadToken + "/" + filename

	// Generate document key
	fileID := ""
	if sl.targetEntry != nil {
		fileID = sl.targetEntry.ID
	}
	docKey := generateDocKey(sl.libraryID, sl.filePath, fileID)

	// Build OnlyOffice config in view-only mode
	docConfig := OnlyOfficeConfig{
		Document: OnlyOfficeDocument{
			FileType: ext,
			Key:      docKey,
			Title:    filename,
			URL:      downloadURL,
			Permissions: &OnlyOfficePermissions{
				Edit:      false,
				Download:  sl.canDownload,
				Print:     sl.canDownload,
				Copy:      true,
				Review:    false,
				Comment:   false,
				FillForms: false,
			},
		},
		DocumentType: getDocumentType(filename),
		EditorConfig: OnlyOfficeEditorConfig{
			Mode: "view",
			User: OnlyOfficeUser{
				ID:   "anonymous",
				Name: "Anonymous",
			},
			Customization: &OnlyOfficeCustomization{
				Forcesave:  false,
				SubmitForm: false,
			},
		},
	}

	// Sign JWT if secret is configured
	if h.config.OnlyOffice.JWTSecret != "" {
		ooHandler := &OnlyOfficeHandler{
			db:     h.db,
			config: h.config,
		}
		token, signErr := ooHandler.signJWT(docConfig)
		if signErr == nil {
			docConfig.Token = token
		}
	}

	configJSON, err := json.Marshal(docConfig)
	if err != nil {
		return "", fmt.Errorf("failed to marshal OnlyOffice config: %w", err)
	}

	safeFilename := html.EscapeString(filename)
	safeSharedBy := html.EscapeString(sl.creatorName)
	safeAPIJSURL := html.EscapeString(h.config.OnlyOffice.APIJSURL)
	downloadLink := fmt.Sprintf("/d/%s?dl=1", sl.token)
	fileSizeStr := formatFileSize(fileSize)

	var downloadBtn string
	if sl.canDownload {
		downloadBtn = fmt.Sprintf(`<a href="%s" class="btn-download">Download (%s)</a>`, html.EscapeString(downloadLink), fileSizeStr)
	}

	htmlPage := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>%s - SesameFS</title>
    <link rel="icon" type="image/x-icon" href="/favicon.png">
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif; height: 100vh; display: flex; flex-direction: column; background: #f5f5f5; color: #333; }
        .header { background: #fff; border-bottom: 1px solid #e0e0e0; padding: 12px 24px; display: flex; align-items: center; justify-content: space-between; flex-shrink: 0; box-shadow: 0 1px 3px rgba(0,0,0,0.05); }
        .header-left { display: flex; align-items: center; gap: 16px; min-width: 0; }
        .logo { height: 28px; flex-shrink: 0; }
        .file-info { min-width: 0; }
        .file-name { font-size: 16px; font-weight: 600; color: #1a1a1a; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 600px; }
        .shared-by { font-size: 13px; color: #666; margin-top: 2px; }
        .header-right { display: flex; align-items: center; gap: 12px; flex-shrink: 0; }
        .btn-download { display: inline-flex; align-items: center; padding: 8px 20px; background: #f7931e; color: #fff; text-decoration: none; border-radius: 6px; font-size: 14px; font-weight: 500; transition: background 0.15s; }
        .btn-download:hover { background: #e8850f; }
        .preview-container { flex: 1; overflow: hidden; }
        .loading { display: flex; justify-content: center; align-items: center; height: 100%%; font-family: inherit; color: #666; }
        .loading-spinner { width: 32px; height: 32px; border: 3px solid #f3f3f3; border-top: 3px solid #3498db; border-radius: 50%%; animation: spin 1s linear infinite; margin-right: 12px; }
        @keyframes spin { 0%% { transform: rotate(0deg); } 100%% { transform: rotate(360deg); } }
        .error { color: #c0392b; text-align: center; padding: 40px 20px; }
        @media (max-width: 768px) {
            .header { padding: 10px 16px; flex-wrap: wrap; gap: 8px; }
            .file-name { max-width: 100%%; font-size: 14px; }
        }
    </style>
</head>
<body>
    <div class="header">
        <div class="header-left">
            <a href="/"><img src="/static/img/logo.png" alt="SesameFS" class="logo" onerror="this.style.display='none'" /></a>
            <div class="file-info">
                <div class="file-name" title="%s">%s</div>
                <div class="shared-by">Shared by %s</div>
            </div>
        </div>
        <div class="header-right">
            %s
        </div>
    </div>
    <div class="preview-container" id="oo-preview">
        <div class="loading">
            <div class="loading-spinner"></div>
            <span>Loading document preview...</span>
        </div>
    </div>

    <script src="%s"></script>
    <script>
        (function() {
            var config = %s;

            function initEditor() {
                if (typeof DocsAPI === 'undefined') {
                    setTimeout(initEditor, 100);
                    return;
                }
                try {
                    document.getElementById('oo-preview').innerHTML = '';
                    new DocsAPI.DocEditor("oo-preview", config);
                } catch (e) {
                    console.error('Failed to initialize document preview:', e);
                    document.getElementById('oo-preview').innerHTML =
                        '<div class="error"><h2>Preview unavailable</h2><p>' + e.message + '</p></div>';
                }
            }

            if (document.readyState === 'loading') {
                document.addEventListener('DOMContentLoaded', initEditor);
            } else {
                initEditor();
            }
        })();
    </script>
</body>
</html>`,
		safeFilename,
		safeFilename, safeFilename,
		safeSharedBy,
		downloadBtn,
		safeAPIJSURL,
		string(configJSON),
	)

	return htmlPage, nil
}

// serveSharedFileOnlyOffice renders the OnlyOffice viewer for a shared file
func (h *ShareLinkViewHandler) serveSharedFileOnlyOffice(c *gin.Context, sl *shareLinkData, filename, ext string) {
	// Generate download token so OnlyOffice server can fetch the document
	downloadToken, err := h.tokenCreator.CreateDownloadToken(sl.orgID, sl.libraryID, sl.filePath, sl.createdBy)
	if err != nil {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusInternalServerError, errorPageHTML("Error", "Failed to generate document access token."))
		return
	}

	// Use OnlyOffice-specific server URL for download (URL that OnlyOffice can reach)
	ooServerURL := h.config.OnlyOffice.ServerURL
	if ooServerURL == "" {
		ooServerURL = h.serverURL
	}
	downloadURL := ooServerURL + "/seafhttp/files/" + downloadToken + "/" + filename

	// Generate document key
	fileID := ""
	if sl.targetEntry != nil {
		fileID = sl.targetEntry.ID
	}
	docKey := generateDocKey(sl.libraryID, sl.filePath, fileID)

	// Build OnlyOffice config in view-only mode
	docConfig := OnlyOfficeConfig{
		Document: OnlyOfficeDocument{
			FileType: ext,
			Key:      docKey,
			Title:    filename,
			URL:      downloadURL,
			Permissions: &OnlyOfficePermissions{
				Edit:      false,
				Download:  sl.canDownload,
				Print:     sl.canDownload,
				Copy:      true,
				Review:    false,
				Comment:   false,
				FillForms: false,
			},
		},
		DocumentType: getDocumentType(filename),
		EditorConfig: OnlyOfficeEditorConfig{
			Mode: "view",
			User: OnlyOfficeUser{
				ID:   "anonymous",
				Name: "Anonymous",
			},
			Customization: &OnlyOfficeCustomization{
				Forcesave:  false,
				SubmitForm: false,
			},
		},
	}

	// Sign JWT if secret is configured
	if h.config.OnlyOffice.JWTSecret != "" {
		ooHandler := &OnlyOfficeHandler{
			db:     h.db,
			config: h.config,
		}
		token, err := ooHandler.signJWT(docConfig)
		if err == nil {
			docConfig.Token = token
		}
	}

	htmlPage := onlyOfficeEditorHTML(h.config.OnlyOffice.APIJSURL, docConfig, filename)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, htmlPage)
}

// buildSharePageHTML generates the HTML page that loads the appropriate bundle
func (h *ShareLinkViewHandler) buildSharePageHTML(bundleName, title, pageOptionsJSON string) string {
	// Resolve bundle filenames
	runtimeJS := h.resolveJSBundle("runtime")
	commonsJS := h.resolveJSBundle("commons")
	entryJS := h.resolveJSBundle(bundleName)

	commonsCSS := h.resolveCSSBundle("commons")
	entryCSS := h.resolveCSSBundle(bundleName)
	seahubCSS := "/static/css/seahub.css"

	// Build CSS links
	var cssLinks string
	cssLinks += fmt.Sprintf(`    <link rel="stylesheet" href="%s">`+"\n", seahubCSS)
	if commonsCSS != "" {
		cssLinks += fmt.Sprintf(`    <link rel="stylesheet" href="/static/css/%s">`+"\n", commonsCSS)
	}
	if entryCSS != "" {
		cssLinks += fmt.Sprintf(`    <link rel="stylesheet" href="/static/css/%s">`+"\n", entryCSS)
	}

	// Build script tags
	var scriptTags string
	if runtimeJS != "" {
		scriptTags += fmt.Sprintf(`    <script src="/static/js/%s"></script>`+"\n", runtimeJS)
	}
	if commonsJS != "" {
		scriptTags += fmt.Sprintf(`    <script src="/static/js/%s"></script>`+"\n", commonsJS)
	}
	if entryJS != "" {
		scriptTags += fmt.Sprintf(`    <script src="/static/js/%s"></script>`+"\n", entryJS)
	}

	safeTitle := html.EscapeString(title)

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>%s</title>
    <link rel="icon" id="favicon" type="image/x-icon" href="/favicon.png">
%s</head>
<body>
    <div id="wrapper"></div>
    <div id="modal-wrapper"></div>

    <script>
    // i18n functions - pass-through for English
    window.gettext = function(s) { return s; };
    window.ngettext = function(s, p, n) { return n === 1 ? s : p; };
    window.pgettext = function(c, s) { return s; };
    window.interpolate = function(fmt, obj, named) {
        if (named) {
            return fmt.replace(/%%\((\w+)\)s/g, function(m, k) { return obj[k] !== undefined ? obj[k] : m; });
        }
        return fmt.replace(/%%s/g, function() { return obj.shift(); });
    };

    window.app = window.app || {};
    window.app.config = {
        serviceURL: "",
        mediaUrl: "/static/",
        siteRoot: "/",
        staticUrl: "/static/",
        logoPath: "img/logo.png",
        logoWidth: 128,
        logoHeight: 40,
        siteTitle: "SesameFS",
        fileServerRoot: "/seafhttp/",
        useGoFileserver: true,
        lang: "en"
    };
    window.app.pageOptions = {
        name: "",
        contactEmail: ""
    };
    window.shared = {
        pageOptions: %s
    };
    </script>

%s</body>
</html>`, safeTitle, cssLinks, pageOptionsJSON, scriptTags)
}

func (h *ShareLinkViewHandler) resolveJSBundle(name string) string {
	if f, ok := h.jsBundleMap[name]; ok {
		return f
	}
	return ""
}

func (h *ShareLinkViewHandler) resolveCSSBundle(name string) string {
	if f, ok := h.cssBundleMap[name]; ok {
		return f
	}
	return ""
}

// extensionToBundleName maps a file extension to the appropriate shared view bundle
func extensionToBundleName(ext string) string {
	switch ext {
	case "md", "markdown":
		return "sharedFileViewMarkdown"
	case "txt", "py", "js", "css", "html", "json", "xml", "yaml", "yml",
		"sh", "go", "rs", "java", "c", "cpp", "h", "rb", "php", "sql",
		"conf", "ini", "log", "csv", "tsv":
		return "sharedFileViewText"
	case "png", "jpg", "jpeg", "gif", "bmp", "webp", "ico", "tiff", "tif":
		return "sharedFileViewImage"
	case "mp4", "webm", "ogg", "mov", "avi", "mkv":
		return "sharedFileViewVideo"
	case "mp3", "wav", "flac", "aac", "wma":
		return "sharedFileViewAudio"
	case "pdf":
		return "sharedFileViewPDF"
	case "svg":
		return "sharedFileViewSVG"
	case "doc", "docx", "ppt", "pptx":
		// Office documents require a converter (LibreOffice/OnlyOffice) for in-browser preview.
		// Without one configured, use the download-only view.
		return "sharedFileViewUnknown"
	case "xls", "xlsx":
		return "sharedFileViewUnknown"
	default:
		return "sharedFileViewUnknown"
	}
}

// ListShareLinkDirents lists directory entries for a shared directory
// GET /api/v2.1/share-links/:token/dirents/
func (h *ShareLinkViewHandler) ListShareLinkDirents(c *gin.Context) {
	token := c.Param("token")

	sl, err := h.resolveShareLink(token)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "share link not found"})
		return
	}

	if sl.isExpired {
		c.JSON(http.StatusGone, gin.H{"error": "share link has expired"})
		return
	}

	// Get the requested sub-path within the shared directory
	requestedPath := c.DefaultQuery("path", "/")
	if requestedPath == "" {
		requestedPath = "/"
	}

	// Build the full path: share link's base path + requested sub-path
	var fullPath string
	if sl.filePath == "/" || sl.filePath == "" {
		fullPath = requestedPath
	} else if requestedPath == "/" {
		fullPath = sl.filePath
	} else {
		fullPath = strings.TrimSuffix(sl.filePath, "/") + "/" + strings.TrimPrefix(requestedPath, "/")
	}

	// Traverse to the directory
	fsHelper := NewFSHelper(h.db)
	rootFSID, _, err := fsHelper.GetRootFSID(sl.libraryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to access library"})
		return
	}

	var entries []FSEntry
	if fullPath == "/" {
		entries, err = fsHelper.GetDirectoryEntries(sl.libraryID, rootFSID)
	} else {
		result, traverseErr := fsHelper.TraverseToPathFromRoot(sl.libraryID, rootFSID, fullPath)
		if traverseErr != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "directory not found"})
			return
		}
		// If we traversed to a directory, get its entries
		if result.TargetEntry != nil && (result.TargetEntry.Mode == ModeDir || result.TargetEntry.Mode&0170000 == 040000) {
			entries, err = fsHelper.GetDirectoryEntries(sl.libraryID, result.TargetFSID)
		} else if result.TargetEntry == nil && result.TargetFSID != "" {
			// TraverseToPath for root returns TargetEntry=nil but TargetFSID set
			entries, err = fsHelper.GetDirectoryEntries(sl.libraryID, result.TargetFSID)
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "path is not a directory"})
			return
		}
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list directory"})
		return
	}

	// Build response in Seafile format
	// The frontend (shared-dir-view.js) expects:
	//   For files: file_name, file_path, size, last_modified
	//   For dirs:  folder_name, folder_path, last_modified
	type DirentResponse struct {
		FileName     string `json:"file_name,omitempty"`
		FolderName   string `json:"folder_name,omitempty"`
		FilePath     string `json:"file_path,omitempty"`
		FolderPath   string `json:"folder_path,omitempty"`
		FileSize     int64  `json:"file_size"`
		Size         int64  `json:"size"`
		IsDir        bool   `json:"is_dir"`
		LastModified int64  `json:"last_modified"`
	}

	dirents := make([]DirentResponse, 0, len(entries))
	for _, entry := range entries {
		isDir := entry.Mode == ModeDir || entry.Mode&0170000 == 040000

		// Build the path relative to the share link root
		var entryRelPath string
		if requestedPath == "/" {
			entryRelPath = "/" + entry.Name
		} else {
			entryRelPath = strings.TrimSuffix(requestedPath, "/") + "/" + entry.Name
		}

		// Convert Unix seconds to milliseconds for moment.js compatibility
		// moment(number) interprets as milliseconds, so raw Unix seconds would show wrong dates
		lastModifiedMs := entry.MTime * 1000

		d := DirentResponse{
			FileSize:     entry.Size,
			Size:         entry.Size,
			IsDir:        isDir,
			LastModified: lastModifiedMs,
		}
		if isDir {
			d.FolderName = entry.Name
			d.FolderPath = entryRelPath + "/"
			d.FileName = entry.Name // also set for compatibility
		} else {
			d.FileName = entry.Name
			d.FilePath = entryRelPath
		}
		dirents = append(dirents, d)
	}

	c.JSON(http.StatusOK, gin.H{
		"dirent_list": dirents,
	})
}

// GetShareLinkRepoTags returns the repository tags for a shared directory
// GET /api/v2.1/share-links/:token/repo-tags/
func (h *ShareLinkViewHandler) GetShareLinkRepoTags(c *gin.Context) {
	token := c.Param("token")

	sl, err := h.resolveShareLink(token)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "share link not found"})
		return
	}

	if sl.isExpired {
		c.JSON(http.StatusGone, gin.H{"error": "share link has expired"})
		return
	}

	// Return empty repo_tags array - tags are not typically shown for share links
	// as they're used for personal organization, not sharing
	c.JSON(http.StatusOK, gin.H{
		"repo_tags": []interface{}{},
	})
}

// ServeShareLinkFilePage handles GET /d/:token/files/
// This is the route used when clicking a file inside a shared directory.
// The frontend constructs URLs like: /d/{token}/files/?p=/path/to/file.txt
func (h *ShareLinkViewHandler) ServeShareLinkFilePage(c *gin.Context) {
	token := c.Param("token")
	filePath := c.Query("p")

	sl, err := h.resolveShareLink(token)
	if err != nil {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusNotFound, errorPageHTML("Not Found", "This share link does not exist."))
		return
	}

	if sl.isExpired {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusGone, errorPageHTML("Link Expired", "This share link has expired or reached its download limit."))
		return
	}

	// Build full path from share link base + requested file path
	if filePath == "" {
		filePath = "/"
	}
	var fullPath string
	if sl.filePath == "/" || sl.filePath == "" {
		fullPath = filePath
	} else if filePath == "/" {
		fullPath = sl.filePath
	} else {
		fullPath = strings.TrimSuffix(sl.filePath, "/") + "/" + strings.TrimPrefix(filePath, "/")
	}

	// Override the share link's file path with the specific file
	sl.filePath = fullPath
	sl.isDirShareLink = true
	sl.fileSubPath = filePath

	fsHelper := NewFSHelper(h.db)
	rootFSID, _, err := fsHelper.GetRootFSID(sl.libraryID)
	if err != nil {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusInternalServerError, errorPageHTML("Error", "Failed to access the shared library."))
		return
	}

	result, err := fsHelper.TraverseToPathFromRoot(sl.libraryID, rootFSID, fullPath)
	if err != nil || result.TargetEntry == nil {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusNotFound, errorPageHTML("Not Found", "The shared file could not be found."))
		return
	}
	sl.targetEntry = result.TargetEntry
	sl.isDir = false

	// Handle direct download (?dl=1)
	if c.Query("dl") == "1" {
		h.handleShareLinkDownload(c, sl, fsHelper, rootFSID)
		return
	}

	// Handle raw file content (?raw=1)
	if c.Query("raw") == "1" {
		h.handleShareLinkRaw(c, sl)
		return
	}

	// Serve the file view page
	h.serveSharedFilePage(c, sl)
}

// GetShareLinkZipTask handles GET /api/v2.1/share-link-zip-task/
// Creates a zip download task for a shared directory and returns a zip token.
func (h *ShareLinkViewHandler) GetShareLinkZipTask(c *gin.Context) {
	token := c.Query("share_link_token")
	path := c.DefaultQuery("path", "/")

	sl, err := h.resolveShareLink(token)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "share link not found"})
		return
	}

	if sl.isExpired {
		c.JSON(http.StatusGone, gin.H{"error": "share link has expired"})
		return
	}

	if !sl.canDownload {
		c.JSON(http.StatusForbidden, gin.H{"error": "download not permitted"})
		return
	}

	// Build the full path
	var fullPath string
	if sl.filePath == "/" || sl.filePath == "" {
		fullPath = path
	} else if path == "/" {
		fullPath = sl.filePath
	} else {
		fullPath = strings.TrimSuffix(sl.filePath, "/") + "/" + strings.TrimPrefix(path, "/")
	}

	// Generate a download token for the zip
	// We reuse the download token mechanism — the zip will be created on-the-fly
	zipToken, err := h.tokenCreator.CreateDownloadToken(sl.orgID, sl.libraryID, fullPath, sl.createdBy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create zip download token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"zip_token": zipToken,
	})
}

// PostShareLinkZipTask handles POST /api/v2.1/share-link-zip-task/
// Creates a zip download task for specific items in a shared directory.
func (h *ShareLinkViewHandler) PostShareLinkZipTask(c *gin.Context) {
	// Same behavior as GET for now — the token approach handles both cases
	h.GetShareLinkZipTask(c)
}

// ServeUploadLinkPage handles GET /u/d/:token
// Renders the upload link page that allows anonymous file uploads.
func (h *ShareLinkViewHandler) ServeUploadLinkPage(c *gin.Context) {
	token := c.Param("token")

	// Resolve the upload link from DB
	var orgID, libraryID, filePath, createdBy, passwordHash string
	var expiresAt *time.Time

	err := h.db.Session().Query(`
		SELECT org_id, library_id, file_path, created_by, password_hash, expires_at
		FROM upload_links WHERE upload_token = ?
	`, token).Scan(&orgID, &libraryID, &filePath, &createdBy, &passwordHash, &expiresAt)
	if err != nil {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusNotFound, errorPageHTML("Not Found", "This upload link does not exist."))
		return
	}

	// Check expiration
	if expiresAt != nil && time.Now().After(*expiresAt) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusGone, errorPageHTML("Link Expired", "This upload link has expired."))
		return
	}

	// TODO: Handle password-protected upload links (check cookie/header)
	_ = passwordHash

	// Get library name
	var repoName string
	h.db.Session().Query(`SELECT name FROM libraries_by_id WHERE library_id = ?`, libraryID).Scan(&repoName)
	if repoName == "" {
		repoName = "Shared folder"
	}

	// Get uploader display name
	dirName := filepath.Base(filePath)
	if filePath == "/" || filePath == "" {
		dirName = repoName
	}

	// Get creator info
	var creatorName, creatorEmail string
	h.db.Session().Query(`SELECT name, email FROM users WHERE org_id = ? AND user_id = ?`, orgID, createdBy).Scan(&creatorName, &creatorEmail)
	if creatorName == "" {
		creatorName = creatorEmail
	}
	if creatorName == "" {
		creatorName = "Unknown"
	}

	// Build shared_by object matching frontend expectations
	sharedByJSON := fmt.Sprintf(`{"name": %q, "avatar": ""}`, creatorName)

	// Build pageOptions for the uploadLink bundle
	pageOptions := fmt.Sprintf(`{
		"token": %q,
		"repoID": %q,
		"path": %q,
		"dirName": %q,
		"sharedBy": %s,
		"noQuota": false,
		"maxUploadFileSize": 0
	}`,
		token,
		libraryID,
		filePath,
		html.EscapeString(dirName),
		sharedByJSON,
	)

	// Use buildSharePageHTML but with "uploadLink" bundle and window.uploadLink instead of window.shared.pageOptions
	htmlPage := h.buildUploadLinkPageHTML("uploadLink", dirName+" - Upload - SesameFS", pageOptions)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, htmlPage)
}

// buildUploadLinkPageHTML is similar to buildSharePageHTML but injects window.uploadLink
func (h *ShareLinkViewHandler) buildUploadLinkPageHTML(bundleName, title, pageOptionsJSON string) string {
	// Resolve bundle filenames
	runtimeJS := h.resolveJSBundle("runtime")
	commonsJS := h.resolveJSBundle("commons")
	entryJS := h.resolveJSBundle(bundleName)

	commonsCSS := h.resolveCSSBundle("commons")
	entryCSS := h.resolveCSSBundle(bundleName)
	seahubCSS := "/static/css/seahub.css"

	// Build CSS links
	var cssLinks string
	cssLinks += fmt.Sprintf(`    <link rel="stylesheet" href="%s">`+"\n", seahubCSS)
	if commonsCSS != "" {
		cssLinks += fmt.Sprintf(`    <link rel="stylesheet" href="/static/css/%s">`+"\n", commonsCSS)
	}
	if entryCSS != "" {
		cssLinks += fmt.Sprintf(`    <link rel="stylesheet" href="/static/css/%s">`+"\n", entryCSS)
	}

	// Build script tags
	var scriptTags string
	if runtimeJS != "" {
		scriptTags += fmt.Sprintf(`    <script src="/static/js/%s"></script>`+"\n", runtimeJS)
	}
	if commonsJS != "" {
		scriptTags += fmt.Sprintf(`    <script src="/static/js/%s"></script>`+"\n", commonsJS)
	}
	if entryJS != "" {
		scriptTags += fmt.Sprintf(`    <script src="/static/js/%s"></script>`+"\n", entryJS)
	}

	safeTitle := html.EscapeString(title)

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>%s</title>
    <link rel="icon" id="favicon" type="image/x-icon" href="/favicon.png">
%s</head>
<body>
    <div id="wrapper"></div>
    <div id="modal-wrapper"></div>

    <script>
    // i18n functions
    window.gettext = function(s) { return s; };
    window.ngettext = function(s, p, n) { return n === 1 ? s : p; };
    window.pgettext = function(c, s) { return s; };
    window.interpolate = function(fmt, obj, named) {
        if (named) {
            return fmt.replace(/%%\((\w+)\)s/g, function(m, k) { return obj[k] !== undefined ? obj[k] : m; });
        }
        return fmt.replace(/%%s/g, function() { return obj.shift(); });
    };

    window.app = window.app || {};
    window.app.config = {
        serviceURL: "",
        mediaUrl: "/static/",
        siteRoot: "/",
        staticUrl: "/static/",
        logoPath: "img/logo.png",
        logoWidth: 128,
        logoHeight: 40,
        siteTitle: "SesameFS",
        fileServerRoot: "/seafhttp/",
        useGoFileserver: true,
        lang: "en"
    };
    window.app.pageOptions = {
        name: "",
        username: "",
        contactEmail: ""
    };
    // Upload link data — consumed by frontend/src/pages/upload-link/index.js
    window.uploadLink = %s;
    </script>

%s</body>
</html>`, safeTitle, cssLinks, pageOptionsJSON, scriptTags)
}

// GetUploadLinkUploadURL handles GET /api/v2.1/upload-links/:token/upload/
// Returns the upload URL for an upload link.
func (h *ShareLinkViewHandler) GetUploadLinkUploadURL(c *gin.Context) {
	token := c.Param("token")

	// Resolve upload link
	var orgID, libraryID, filePath, createdBy string
	var expiresAt *time.Time
	err := h.db.Session().Query(`
		SELECT org_id, library_id, file_path, created_by, expires_at
		FROM upload_links WHERE upload_token = ?
	`, token).Scan(&orgID, &libraryID, &filePath, &createdBy, &expiresAt)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "upload link not found"})
		return
	}

	// Check expiration
	if expiresAt != nil && time.Now().After(*expiresAt) {
		c.JSON(http.StatusGone, gin.H{"error": "upload link has expired"})
		return
	}

	// Generate an upload URL using the seafhttp upload mechanism
	// Create a token that the file-upload handler will accept
	uploadToken, err := h.tokenCreator.CreateUploadToken(orgID, libraryID, filePath, createdBy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate upload URL"})
		return
	}

	uploadURL := getBrowserURL(c, h.serverURL) + "/seafhttp/upload-api/" + uploadToken
	c.JSON(http.StatusOK, gin.H{
		"upload_link": uploadURL,
	})
}

// PostUploadLinkDone handles POST /api/v2.1/upload-links/:token/upload-done/
// Notification that a file upload has been completed via an upload link.
func (h *ShareLinkViewHandler) PostUploadLinkDone(c *gin.Context) {
	// For now, just acknowledge — could be used for notifications, audit logs, etc.
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetShareLinkUploadURL handles GET /api/v2.1/share-links/:token/upload/
// Returns the upload URL for a share link with upload permissions.
func (h *ShareLinkViewHandler) GetShareLinkUploadURL(c *gin.Context) {
	token := c.Param("token")
	path := c.DefaultQuery("path", "/")

	sl, err := h.resolveShareLink(token)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "share link not found"})
		return
	}

	if sl.isExpired {
		c.JSON(http.StatusGone, gin.H{"error": "share link has expired"})
		return
	}

	// Check if upload is allowed for this share link
	if !sl.canUpload {
		c.JSON(http.StatusForbidden, gin.H{"error": "upload not permitted"})
		return
	}

	// Build the full path for upload destination
	var fullPath string
	if sl.filePath == "/" || sl.filePath == "" {
		fullPath = path
	} else if path == "/" {
		fullPath = sl.filePath
	} else {
		fullPath = strings.TrimSuffix(sl.filePath, "/") + "/" + strings.TrimPrefix(path, "/")
	}

	// Generate an upload URL using the seafhttp upload mechanism
	// Create a token that the file-upload handler will accept
	uploadToken, err := h.tokenCreator.CreateUploadToken(sl.orgID, sl.libraryID, fullPath, sl.createdBy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate upload URL"})
		return
	}

	uploadURL := getBrowserURL(c, h.serverURL) + "/seafhttp/upload-api/" + uploadToken
	c.JSON(http.StatusOK, gin.H{
		"upload_link": uploadURL,
	})
}

// PostShareLinkUploadDone handles POST /api/v2.1/share-links/:token/upload-done/
// Notification that a file upload has been completed via a share link.
func (h *ShareLinkViewHandler) PostShareLinkUploadDone(c *gin.Context) {
	token := c.Param("token")

	// Validate the share link exists and has upload permissions
	sl, err := h.resolveShareLink(token)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "share link not found"})
		return
	}

	if sl.isExpired {
		c.JSON(http.StatusGone, gin.H{"error": "share link has expired"})
		return
	}

	if !sl.canUpload {
		c.JSON(http.StatusForbidden, gin.H{"error": "upload not permitted"})
		return
	}

	// Acknowledge upload completion
	// Could be used for notifications, audit logs, etc.
	c.JSON(http.StatusOK, gin.H{"success": true})
}
