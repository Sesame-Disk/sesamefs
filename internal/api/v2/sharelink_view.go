package v2

import (
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/config"
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
	}
}

// getCSSBundleFallbacks returns hardcoded CSS bundle filenames
func getCSSBundleFallbacks() map[string]string {
	return map[string]string{
		"commons":                   "commons.82d1af8c.css",
		"sharedDirView":             "sharedDirView.b715f1e6.css",
		"sharedFileViewSpreadsheet": "sharedFileViewSpreadsheet.ff1ddac7.css",
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
	isExpired   bool
	repoName    string
	commitID    string
	isDir       bool
	targetEntry *FSEntry
	// Parsed permissions (handles both string and JSON formats)
	canEdit     bool
	canDownload bool
	canUpload   bool
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

	return &shareLinkData{
		token:       token,
		orgID:       orgID,
		libraryID:   libraryID,
		filePath:    filePath,
		permission:  permission,
		createdBy:   createdBy,
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

// serveSharedDirPage renders the shared directory view
func (h *ShareLinkViewHandler) serveSharedDirPage(c *gin.Context, sl *shareLinkData) {
	dirName := filepath.Base(sl.filePath)
	if sl.filePath == "/" || sl.filePath == "" {
		dirName = sl.repoName
	}

	pageOptions := fmt.Sprintf(`{
		"token": %q,
		"repoID": %q,
		"repoName": %q,
		"path": %q,
		"dirName": %q,
		"canDownload": %t,
		"canUpload": %t,
		"sharedBy": "",
		"noPassword": true,
		"trafficOverLimit": false,
		"permissions": {"can_edit": %t, "can_download": %t, "can_upload": %t}
	}`,
		sl.token,
		sl.libraryID,
		html.EscapeString(sl.repoName),
		sl.filePath,
		html.EscapeString(dirName),
		sl.canDownload,
		sl.canUpload,
		sl.canEdit,
		sl.canDownload,
		sl.canUpload,
	)

	htmlPage := h.buildSharePageHTML("sharedDirView", dirName+" - SesameFS", pageOptions)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, htmlPage)
}

// serveSharedFilePage renders the shared file view
func (h *ShareLinkViewHandler) serveSharedFilePage(c *gin.Context, sl *shareLinkData) {
	filename := filepath.Base(sl.filePath)
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))

	bundleName := extensionToBundleName(ext)

	// Build raw file path for preview
	rawPath := fmt.Sprintf("/d/%s", sl.token)

	var fileSize int64
	if sl.targetEntry != nil {
		fileSize = sl.targetEntry.Size
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
		"sharedBy": "",
		"noPassword": true,
		"trafficOverLimit": false,
		"fileExt": %q,
		"siteName": "SesameFS",
		"enableWatermark": false,
		"zipped": null,
		"enableShareLinkReportAbuse": false,
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
		ext,
	)

	htmlPage := h.buildSharePageHTML(bundleName, filename+" - SesameFS", pageOptions)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, htmlPage)
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
    <link rel="icon" id="favicon" type="image/x-icon" href="/static/img/favicon.ico">
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
        staticUrl: "/static/"
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
	type DirentResponse struct {
		FileName    string `json:"file_name"`
		FileSize    int64  `json:"file_size"`
		IsDir       bool   `json:"is_dir"`
		LastModified int64 `json:"last_modified"`
	}

	dirents := make([]DirentResponse, 0, len(entries))
	for _, entry := range entries {
		isDir := entry.Mode == ModeDir || entry.Mode&0170000 == 040000
		dirents = append(dirents, DirentResponse{
			FileName:     entry.Name,
			FileSize:     entry.Size,
			IsDir:        isDir,
			LastModified: entry.MTime,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"dirent_list": dirents,
	})
}
