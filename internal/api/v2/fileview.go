package v2

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/Sesame-Disk/sesamefs/internal/config"
	"github.com/Sesame-Disk/sesamefs/internal/crypto"
	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	"github.com/gin-gonic/gin"
)

// FileViewHandler handles file viewing pages
type FileViewHandler struct {
	db             *db.DB
	config         *config.Config
	storage        *storage.S3Store
	storageManager *storage.Manager
	tokenCreator   TokenCreator
	serverURL      string
}

// RegisterFileViewRoutes registers routes for file viewing
func RegisterFileViewRoutes(router *gin.Engine, database *db.DB, cfg *config.Config, s3Store *storage.S3Store, storageManager *storage.Manager, tokenCreator TokenCreator, serverURL string, authMiddleware gin.HandlerFunc) {
	h := &FileViewHandler{
		db:             database,
		config:         cfg,
		storage:        s3Store,
		storageManager: storageManager,
		tokenCreator:   tokenCreator,
		serverURL:      serverURL,
	}

	// File view uses a wrapper that promotes ?token= query param to Authorization header,
	// then delegates to the server's standard auth middleware (which supports dev tokens,
	// OIDC sessions, and anonymous access).
	fileViewAuth := fileViewAuthWrapper(authMiddleware)

	libGroup := router.Group("/lib")
	libGroup.Use(fileViewAuth)
	{
		libGroup.GET("/:repo_id/file/*filepath", h.ViewFile)
	}

	// Raw file endpoint for serving files inline (images, etc.)
	repoGroup := router.Group("/repo")
	repoGroup.Use(fileViewAuth)
	{
		repoGroup.GET("/:repo_id/raw/*filepath", h.ServeRawFile)
		repoGroup.GET("/:repo_id/history/download", h.DownloadHistoricFile)
	}
}

// fileViewAuthWrapper wraps the server's standard auth middleware to also accept
// tokens from the ?token= query parameter. The frontend opens file viewer in a
// new tab via window.open(), so it can't set Authorization headers - it passes
// the token in the URL instead.
func fileViewAuthWrapper(serverAuth gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		// If no Authorization header but token is in query param, promote it to header
		if c.GetHeader("Authorization") == "" {
			if token := c.Query("token"); token != "" {
				c.Request.Header.Set("Authorization", "Token "+token)
			}
		}

		// Delegate to the server's standard auth middleware
		serverAuth(c)
	}
}

// ViewFile serves the file viewer page
// For OnlyOffice-supported files, it renders an HTML page with the OnlyOffice editor
// For previewable files (PDF, images, video, audio, text), it renders an inline preview
// For other files, it redirects to download
// If dl=1 query parameter is present, always download instead of opening in editor
func (h *FileViewHandler) ViewFile(c *gin.Context) {
	repoID := c.Param("repo_id")
	filePath := c.Param("filepath")

	// Clean the file path
	if !strings.HasPrefix(filePath, "/") {
		filePath = "/" + filePath
	}

	filename := filepath.Base(filePath)
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))

	// Check if download is explicitly requested (dl=1 parameter)
	if c.Query("dl") == "1" {
		h.redirectToDownload(c, repoID, filePath, filename)
		return
	}

	// Check if OnlyOffice is enabled and file is supported
	if h.config.OnlyOffice.Enabled && h.isOnlyOfficeFile(ext) {
		h.serveOnlyOfficeEditor(c, repoID, filePath, filename)
		return
	}

	// For previewable files, serve an inline preview page
	if isInlinePreviewable(ext) {
		h.serveInlinePreview(c, repoID, filePath, filename, ext)
		return
	}

	// For other files, redirect to download
	h.redirectToDownload(c, repoID, filePath, filename)
}

// isInlinePreviewable returns true for file types that can be previewed inline
func isInlinePreviewable(ext string) bool {
	switch ext {
	// PDF
	case "pdf":
		return true
	// Images
	case "png", "jpg", "jpeg", "gif", "bmp", "webp", "svg", "ico", "tiff", "tif":
		return true
	// Video
	case "mp4", "webm", "ogg", "mov":
		return true
	// Audio
	case "mp3", "wav", "flac", "aac":
		return true
	// Text / code files
	case "txt", "md", "markdown", "json", "yaml", "yml", "xml", "csv",
		"html", "htm", "css", "js", "ts", "jsx", "tsx",
		"py", "go", "rs", "java", "c", "cpp", "h", "hpp",
		"sh", "bash", "zsh", "fish",
		"toml", "ini", "cfg", "conf", "env",
		"sql", "graphql", "proto",
		"dockerfile", "makefile",
		"rb", "php", "swift", "kt", "scala", "r", "lua", "pl",
		"log", "diff", "patch":
		return true
	}
	return false
}

// isTextFile returns true for file types that should be displayed as text
func isTextFile(ext string) bool {
	switch ext {
	case "txt", "md", "markdown", "json", "yaml", "yml", "xml", "csv",
		"html", "htm", "css", "js", "ts", "jsx", "tsx",
		"py", "go", "rs", "java", "c", "cpp", "h", "hpp",
		"sh", "bash", "zsh", "fish",
		"toml", "ini", "cfg", "conf", "env",
		"sql", "graphql", "proto",
		"dockerfile", "makefile",
		"rb", "php", "swift", "kt", "scala", "r", "lua", "pl",
		"log", "diff", "patch":
		return true
	}
	return false
}

// serveInlinePreview renders an HTML page with inline file preview
func (h *FileViewHandler) serveInlinePreview(c *gin.Context, repoID, filePath, filename, ext string) {
	// Build the raw file URL (served inline with correct MIME type)
	// Pass the auth token so the raw endpoint can authenticate
	token := c.Query("token")
	if token == "" {
		// Extract from Authorization header
		auth := c.GetHeader("Authorization")
		token = strings.TrimPrefix(auth, "Token ")
	}
	rawURL := fmt.Sprintf("/repo/%s/raw%s?token=%s", repoID, filePath, url.QueryEscape(token))
	downloadURL := fmt.Sprintf("/lib/%s/file%s?dl=1&token=%s", repoID, filePath, url.QueryEscape(token))

	safeFilename := html.EscapeString(filename)

	// Build preview content based on file type
	var previewContent string
	switch {
	case ext == "pdf":
		previewContent = fmt.Sprintf(`<embed src="%s" type="application/pdf" width="100%%" height="100%%" style="border:none;" />`,
			html.EscapeString(rawURL))

	case ext == "png" || ext == "jpg" || ext == "jpeg" || ext == "gif" || ext == "bmp" || ext == "webp" || ext == "svg" || ext == "ico" || ext == "tiff" || ext == "tif":
		previewContent = fmt.Sprintf(`<div style="display:flex;align-items:center;justify-content:center;height:100%%;padding:20px;overflow:auto;">
			<img src="%s" alt="%s" style="max-width:100%%;max-height:100%%;object-fit:contain;" />
		</div>`, html.EscapeString(rawURL), safeFilename)

	case ext == "mp4" || ext == "webm" || ext == "ogg" || ext == "mov":
		previewContent = fmt.Sprintf(`<div style="display:flex;align-items:center;justify-content:center;height:100%%;background:#000;">
			<video controls style="max-width:100%%;max-height:100%%;" src="%s">Your browser does not support video playback.</video>
		</div>`, html.EscapeString(rawURL))

	case ext == "mp3" || ext == "wav" || ext == "flac" || ext == "aac":
		previewContent = fmt.Sprintf(`<div style="display:flex;align-items:center;justify-content:center;height:100%%;background:#f8f9fa;">
			<audio controls src="%s" style="width:80%%;max-width:600px;">Your browser does not support audio playback.</audio>
		</div>`, html.EscapeString(rawURL))

	case isTextFile(ext):
		previewContent = fmt.Sprintf(`<div id="text-preview" style="height:100%%;overflow:auto;background:#1e1e1e;padding:0;">
			<pre style="margin:0;padding:20px;color:#d4d4d4;font-family:'SF Mono',Monaco,'Cascadia Code','Roboto Mono',Consolas,'Courier New',monospace;font-size:13px;line-height:1.6;tab-size:4;white-space:pre-wrap;word-wrap:break-word;"><code>Loading...</code></pre>
		</div>
		<script>
		fetch('%s').then(function(r){return r.text()}).then(function(text){
			var el=document.querySelector('#text-preview code');
			el.textContent=text;
		}).catch(function(e){
			document.querySelector('#text-preview code').textContent='Failed to load file: '+e.message;
		});
		</script>`, html.EscapeString(rawURL))

	default:
		previewContent = `<div style="display:flex;align-items:center;justify-content:center;height:100%;color:#666;">
			<p>Preview not available for this file type.</p>
		</div>`
	}

	htmlPage := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>%s - SesameFS</title>
    <link rel="icon" type="image/x-icon" href="/static/img/favicon.ico">
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif; height: 100vh; display: flex; flex-direction: column; background: #f5f5f5; color: #333; }
        .header { background: #fff; border-bottom: 1px solid #e0e0e0; padding: 12px 24px; display: flex; align-items: center; justify-content: space-between; flex-shrink: 0; box-shadow: 0 1px 3px rgba(0,0,0,0.05); }
        .header-left { display: flex; align-items: center; gap: 16px; min-width: 0; }
        .logo { height: 28px; flex-shrink: 0; }
        .file-info { min-width: 0; }
        .file-name { font-size: 16px; font-weight: 600; color: #1a1a1a; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 600px; }
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
            </div>
        </div>
        <div class="header-right">
            <a href="%s" class="btn-download">Download</a>
        </div>
    </div>
    <div class="preview-container">
        %s
    </div>
</body>
</html>`,
		safeFilename,
		safeFilename, safeFilename,
		html.EscapeString(downloadURL),
		previewContent,
	)

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, htmlPage)
}

// redirectToDownload generates a download token and redirects to the seafhttp download endpoint
func (h *FileViewHandler) redirectToDownload(c *gin.Context, repoID, filePath, filename string) {
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	// Generate download token
	token, err := h.tokenCreator.CreateDownloadToken(orgID, repoID, filePath, userID)
	if err != nil {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusInternalServerError, errorPageHTML("Download Error", "Failed to generate download link."))
		return
	}

	// Redirect to seafhttp download endpoint which sets Content-Disposition: attachment
	// Use browser-reachable URL (not internal serverURL which may be on a different port)
	downloadURL := getBrowserURL(c, h.serverURL) + "/seafhttp/files/" + token + "/" + filename
	c.Redirect(http.StatusFound, downloadURL)
}

// isOnlyOfficeFile checks if the file extension is supported by OnlyOffice
func (h *FileViewHandler) isOnlyOfficeFile(ext string) bool {
	for _, viewExt := range h.config.OnlyOffice.ViewExtensions {
		if ext == viewExt {
			return true
		}
	}
	return false
}

// serveOnlyOfficeEditor renders the OnlyOffice editor page
func (h *FileViewHandler) serveOnlyOfficeEditor(c *gin.Context, repoID, filePath, filename string) {
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	// Get OnlyOffice handler to generate config
	ooHandler := &OnlyOfficeHandler{
		db:           h.db,
		config:       h.config,
		storage:      h.storage,
		tokenCreator: h.tokenCreator,
		serverURL:    h.serverURL,
	}

	// Get file ID
	fileID, err := ooHandler.getFileID(repoID, orgID, filePath)
	if err != nil {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusNotFound, errorPageHTML("File Not Found", "The requested file could not be found."))
		return
	}

	// Generate document key
	docKey := generateDocKey(repoID, filePath, fileID)

	// Determine edit mode
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	mode := "view"
	if ooHandler.canEditFile(filename) {
		mode = "edit"
	}

	// Generate download URL
	downloadToken, err := h.tokenCreator.CreateDownloadToken(orgID, repoID, filePath, userID)
	if err != nil {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusInternalServerError, errorPageHTML("Internal Error", "Failed to generate download token."))
		return
	}

	// Use OnlyOffice-specific server URL if configured, otherwise fall back to general server URL
	// This allows configuring a public URL that OnlyOffice server can reach
	ooServerURL := h.config.OnlyOffice.ServerURL
	if ooServerURL == "" {
		ooServerURL = h.serverURL
	}
	downloadURL := ooServerURL + "/seafhttp/files/" + downloadToken + "/" + filename

	// Generate callback URL (URL-encode file_path to handle spaces and special chars)
	callbackURL := fmt.Sprintf("%s/onlyoffice/editor-callback/?repo_id=%s&file_path=%s&doc_key=%s",
		ooServerURL, repoID, url.QueryEscape(filePath), docKey)

	// Get user info
	userName := strings.Split(userID, "@")[0]
	if userName == userID {
		userName = userID
	}

	// Build OnlyOffice configuration (minimal, like Seahub)
	canEdit := mode == "edit"
	docConfig := OnlyOfficeConfig{
		Document: OnlyOfficeDocument{
			FileType: ext,
			Key:      docKey,
			Title:    filename,
			URL:      downloadURL,
			Permissions: &OnlyOfficePermissions{
				Edit:      canEdit,
				Download:  true,
				Print:     true,
				Copy:      true,
				Review:    canEdit,
				Comment:   canEdit,
				FillForms: canEdit,
			},
		},
		DocumentType: getDocumentType(filename),
		EditorConfig: OnlyOfficeEditorConfig{
			CallbackURL: callbackURL,
			Mode:        mode,
			User: OnlyOfficeUser{
				ID:   userID,
				Name: userName,
			},
			Customization: &OnlyOfficeCustomization{
				Forcesave:  canEdit,
				SubmitForm: canEdit,
			},
		},
	}

	// Sign JWT if secret is configured
	if h.config.OnlyOffice.JWTSecret != "" {
		token, err := ooHandler.signJWT(docConfig)
		if err == nil {
			docConfig.Token = token
		}
	}

	// Save doc key mapping
	_ = ooHandler.saveDocKeyMapping(docKey, userID, repoID, filePath)

	// Render the OnlyOffice editor page
	html := onlyOfficeEditorHTML(h.config.OnlyOffice.APIJSURL, docConfig, filename)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, html)
}

// onlyOfficeEditorHTML generates the HTML page for OnlyOffice editor.
// Uses json.Marshal for the config to guarantee the JavaScript config object
// exactly matches the JWT payload (html/template escaping can cause mismatches).
func onlyOfficeEditorHTML(apiJSURL string, cfg OnlyOfficeConfig, filename string) string {
	// Serialize config as JSON - this produces the same output as json.Marshal
	// used to sign the JWT, ensuring the config object matches the token payload.
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return "<html><body><h1>Config Error</h1><p>" + html.EscapeString(err.Error()) + "</p></body></html>"
	}

	// HTML-escape the filename for the title (XSS protection)
	safeFilename := html.EscapeString(filename)
	// API JS URL comes from server config, not user input
	safeAPIJSURL := html.EscapeString(apiJSURL)

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>%s - SesameFS</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        html, body {
            height: 100%%;
            width: 100%%;
            overflow: hidden;
        }
        #editor-container {
            width: 100%%;
            height: 100%%;
        }
        .loading {
            display: flex;
            justify-content: center;
            align-items: center;
            height: 100%%;
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
            color: #666;
        }
        .loading-spinner {
            width: 40px;
            height: 40px;
            border: 3px solid #f3f3f3;
            border-top: 3px solid #3498db;
            border-radius: 50%%;
            animation: spin 1s linear infinite;
            margin-right: 12px;
        }
        @keyframes spin {
            0%% { transform: rotate(0deg); }
            100%% { transform: rotate(360deg); }
        }
        .error {
            color: #c0392b;
            text-align: center;
            padding: 20px;
        }
    </style>
</head>
<body>
    <div id="editor-container">
        <div class="loading">
            <div class="loading-spinner"></div>
            <span>Loading document...</span>
        </div>
    </div>

    <script src="%s"></script>
    <script>
        (function() {
            var config = %s;

            // Wait for DocsAPI to be available
            function initEditor() {
                if (typeof DocsAPI === 'undefined') {
                    setTimeout(initEditor, 100);
                    return;
                }

                try {
                    document.getElementById('editor-container').innerHTML = '';
                    new DocsAPI.DocEditor("editor-container", config);
                } catch (e) {
                    console.error('Failed to initialize OnlyOffice editor:', e);
                    document.getElementById('editor-container').innerHTML =
                        '<div class="error"><h2>Failed to load editor</h2><p>' + e.message + '</p></div>';
                }
            }

            // Start initialization
            if (document.readyState === 'loading') {
                document.addEventListener('DOMContentLoaded', initEditor);
            } else {
                initEditor();
            }
        })();
    </script>
</body>
</html>`, safeFilename, safeAPIJSURL, string(configJSON))
}

// ServeRawFile serves a file directly (inline) for embedding in pages
// Used for images, videos, PDFs, text files, etc. that need to be displayed in the browser
// Serves with Content-Disposition: inline and correct MIME type
func (h *FileViewHandler) ServeRawFile(c *gin.Context) {
	repoID := c.Param("repo_id")
	filePath := c.Param("filepath")
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	// Clean the file path
	if !strings.HasPrefix(filePath, "/") {
		filePath = "/" + filePath
	}

	filename := filepath.Base(filePath)
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))

	// Traverse to file to get block IDs
	fsHelper := NewFSHelper(h.db)
	result, err := fsHelper.TraverseToPath(repoID, filePath)
	if err != nil || result.TargetEntry == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}

	// Get block IDs and file size from the fs_object
	var blockIDs []string
	var fileSize int64
	err = h.db.Session().Query(`
		SELECT block_ids, size_bytes FROM fs_objects WHERE library_id = ? AND fs_id = ?
	`, repoID, result.TargetEntry.ID).Scan(&blockIDs, &fileSize)
	if err != nil {
		log.Printf("[ServeRawFile] Failed to get block IDs: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read file metadata"})
		return
	}

	// Guard against loading very large files into memory
	if fileSize > maxRawFileSize {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"error": fmt.Sprintf("file too large for inline preview (%d bytes, max %d)", fileSize, maxRawFileSize),
		})
		return
	}

	// Get block store
	if h.storageManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage not available"})
		return
	}
	blockStore, _, err := h.storageManager.GetHealthyBlockStore("")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage not available"})
		return
	}

	// Check if library is encrypted
	var encrypted bool
	h.db.Session().Query(`SELECT encrypted FROM libraries WHERE org_id = ? AND library_id = ?`,
		orgID, repoID).Scan(&encrypted)

	var fileKey []byte
	if encrypted {
		fileKey = GetDecryptSessions().GetFileKey(userID, repoID)
		if fileKey == nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "library is encrypted but not unlocked"})
			return
		}
	}

	ctx := c.Request.Context()

	// Retrieve and concatenate blocks
	var content bytes.Buffer
	for _, blockID := range blockIDs {
		// Translate SHA-1 to SHA-256 if needed
		internalID := blockID
		if len(blockID) == 40 {
			var mappedID string
			h.db.Session().Query(`SELECT internal_id FROM block_id_mappings WHERE org_id = ? AND external_id = ?`,
				orgID, blockID).Scan(&mappedID)
			if mappedID != "" {
				internalID = mappedID
			}
		}

		reader, err := blockStore.GetBlockReader(ctx, internalID)
		if err != nil {
			log.Printf("[ServeRawFile] Failed to get block %s: %v", internalID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read file"})
			return
		}
		blockData, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read file"})
			return
		}

		// Decrypt if needed
		if encrypted && fileKey != nil {
			blockData, err = crypto.DecryptBlock(blockData, fileKey)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "decryption failed"})
				return
			}
		}

		content.Write(blockData)
	}

	// For Apple iWork files (.pages, .numbers, .key), extract the embedded preview
	if c.Query("preview") == "1" && isAppleIWorkFile(ext) {
		previewData, err := extractIWorkPreviewPDF(content.Bytes())
		if err != nil {
			log.Printf("[ServeRawFile] Failed to extract iWork preview: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no preview available for this file"})
			return
		}
		// Detect if the extracted content is PDF or an image
		previewMIME := "application/pdf"
		previewExt := "pdf"
		if len(previewData) > 3 && previewData[0] == 0xFF && previewData[1] == 0xD8 {
			previewMIME = "image/jpeg"
			previewExt = "jpg"
		} else if len(previewData) > 8 && string(previewData[:4]) == "\x89PNG" {
			previewMIME = "image/png"
			previewExt = "png"
		}
		baseName := strings.TrimSuffix(filename, "."+ext)
		c.Header("Content-Disposition", fmt.Sprintf(`inline; filename="%s.%s"`, sanitizeFilename(baseName), previewExt))
		c.Header("Cache-Control", "private, max-age=3600")
		c.Data(http.StatusOK, previewMIME, previewData)
		return
	}

	// Determine MIME type from extension
	mimeType := mime.TypeByExtension("." + ext)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	// Serve with inline disposition so the browser displays the content
	c.Header("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, sanitizeFilename(filename)))
	c.Header("Cache-Control", "private, max-age=3600")
	c.Data(http.StatusOK, mimeType, content.Bytes())
}

// sanitizeFilename removes characters that could cause header injection in Content-Disposition.
func sanitizeFilename(name string) string {
	return strings.NewReplacer(`"`, `'`, "\r", "", "\n", "").Replace(name)
}

// isAppleIWorkFile returns true for Apple iWork file extensions
func isAppleIWorkFile(ext string) bool {
	return ext == "pages" || ext == "numbers" || ext == "key"
}

// extractIWorkPreview extracts the embedded preview from an Apple iWork file.
// iWork files (.pages, .numbers, .key) are ZIP archives containing preview images.
// Older versions (pre-2013) use QuickLook/Preview.pdf, modern versions use preview.jpg.
func extractIWorkPreviewPDF(data []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a valid zip archive: %w", err)
	}

	// Known preview file locations in order of preference (best quality first)
	candidates := []string{
		"preview.pdf",
		"preview.jpg",
		"preview.jpeg",
		"preview-web.jpg",
		"preview.png",
		"QuickLook/Preview.pdf",
		"QuickLook/preview.pdf",
		"QuickLook/Thumbnail.jpg",
		"QuickLook/Thumbnail.png",
	}
	for _, candidate := range candidates {
		for _, f := range reader.File {
			if strings.EqualFold(f.Name, candidate) {
				return readZipEntry(f)
			}
		}
	}

	// Fallback: find any PDF in the archive
	for _, f := range reader.File {
		if strings.HasSuffix(strings.ToLower(f.Name), ".pdf") {
			return readZipEntry(f)
		}
	}

	// Log all files in the archive for debugging
	var names []string
	for _, f := range reader.File {
		names = append(names, f.Name)
	}
	return nil, fmt.Errorf("no preview found in iWork archive (files: %v)", names)
}

// maxRawFileSize is the maximum file size ServeRawFile will load into memory (200MB).
// Larger files should be downloaded via the seafhttp endpoint instead.
const maxRawFileSize = 200 * 1024 * 1024

// maxPreviewSize is the maximum size for an extracted iWork preview (50MB).
const maxPreviewSize = 50 * 1024 * 1024

func readZipEntry(f *zip.File) ([]byte, error) {
	if f.UncompressedSize64 > maxPreviewSize {
		return nil, fmt.Errorf("entry %s too large: %d bytes (max %d)", f.Name, f.UncompressedSize64, maxPreviewSize)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", f.Name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxPreviewSize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", f.Name, err)
	}
	if int64(len(data)) > maxPreviewSize {
		return nil, fmt.Errorf("entry %s exceeds max preview size", f.Name)
	}
	return data, nil
}

// DownloadHistoricFile serves a file at a specific revision by its FS object ID.
// The frontend file history view calls this with ?obj_id=<fs_id>&p=<path>.
// Unlike normal downloads (which resolve from HEAD commit), this looks up the
// file's blocks directly from the FS object ID.
func (h *FileViewHandler) DownloadHistoricFile(c *gin.Context) {
	repoID := c.Param("repo_id")
	objID := c.Query("obj_id")
	filePath := c.Query("p")

	if objID == "" {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusBadRequest, errorPageHTML("Bad Request", "Missing obj_id parameter."))
		return
	}
	if filePath == "" {
		filePath = "/"
	}

	filename := filepath.Base(filePath)
	if filename == "" || filename == "." || filename == "/" {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusBadRequest, errorPageHTML("Bad Request", "Invalid file path."))
		return
	}

	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	// Check if library is encrypted and get file key
	var encrypted bool
	var fileKey []byte
	err := h.db.Session().Query(`
		SELECT encrypted FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, repoID).Scan(&encrypted)
	if err != nil {
		log.Printf("[DownloadHistoricFile] Failed to query library: %v", err)
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusNotFound, errorPageHTML("Not Found", "Library not found."))
		return
	}

	if encrypted {
		fileKey = GetDecryptSessions().GetFileKey(userID, repoID)
		if fileKey == nil {
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.String(http.StatusForbidden, errorPageHTML("Library Locked", "This library is encrypted. Please unlock it first."))
			return
		}
	}

	// Look up block IDs directly from the FS object ID (skip HEAD commit traversal)
	var blockIDs []string
	err = h.db.Session().Query(`
		SELECT block_ids FROM fs_objects WHERE library_id = ? AND fs_id = ?
	`, repoID, objID).Scan(&blockIDs)
	if err != nil {
		log.Printf("[DownloadHistoricFile] FS object not found: repo=%s obj=%s err=%v", repoID, objID, err)
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusNotFound, errorPageHTML("Not Found", "The requested file revision could not be found."))
		return
	}

	if h.storageManager == nil {
		log.Printf("[DownloadHistoricFile] Storage manager not available")
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusInternalServerError, errorPageHTML("Internal Error", "Storage not available."))
		return
	}

	blockStore, _, err := h.storageManager.GetHealthyBlockStore("")
	if err != nil {
		log.Printf("[DownloadHistoricFile] Block store not available: %v", err)
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusInternalServerError, errorPageHTML("Internal Error", "Block storage not available."))
		return
	}

	// Retrieve and concatenate blocks
	ctx := c.Request.Context()
	var content bytes.Buffer
	for _, blockID := range blockIDs {
		// Translate SHA-1 (40 chars) to SHA-256 (64 chars) if needed
		internalID := blockID
		if len(blockID) == 40 {
			var mappedID string
			err := h.db.Session().Query(`
				SELECT internal_id FROM block_id_mappings WHERE org_id = ? AND external_id = ?
			`, orgID, blockID).Scan(&mappedID)
			if err == nil && mappedID != "" {
				internalID = mappedID
			}
		}

		blockData, err := blockStore.GetBlock(ctx, internalID)
		if err != nil {
			log.Printf("[DownloadHistoricFile] Failed to get block %s: %v", blockID, err)
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.String(http.StatusInternalServerError, errorPageHTML("Download Error", "Failed to retrieve file data."))
			return
		}

		// Decrypt block if library is encrypted
		if fileKey != nil {
			blockData, err = crypto.DecryptBlock(blockData, fileKey)
			if err != nil {
				log.Printf("[DownloadHistoricFile] Failed to decrypt block %s: %v", blockID, err)
				c.Header("Content-Type", "text/html; charset=utf-8")
				c.String(http.StatusInternalServerError, errorPageHTML("Download Error", "Failed to decrypt file data."))
				return
			}
		}

		content.Write(blockData)
	}

	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, sanitizeFilename(filename)))
	c.Data(http.StatusOK, "application/octet-stream", content.Bytes())
}

// errorPageHTML generates a simple error page
func errorPageHTML(title, message string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>%s - SesameFS</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
            display: flex;
            justify-content: center;
            align-items: center;
            height: 100vh;
            margin: 0;
            background-color: #f5f5f5;
        }
        .error-container {
            text-align: center;
            padding: 40px;
            background: white;
            border-radius: 8px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
        }
        h1 { color: #c0392b; margin-bottom: 16px; }
        p { color: #666; }
    </style>
</head>
<body>
    <div class="error-container">
        <h1>%s</h1>
        <p>%s</p>
    </div>
</body>
</html>`, title, title, message)
}
