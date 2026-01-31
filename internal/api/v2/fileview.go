package v2

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/Sesame-Disk/sesamefs/internal/config"
	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	"github.com/gin-gonic/gin"
)

// FileViewHandler handles file viewing pages
type FileViewHandler struct {
	db           *db.DB
	config       *config.Config
	storage      *storage.S3Store
	tokenCreator TokenCreator
	serverURL    string
}

// RegisterFileViewRoutes registers routes for file viewing
func RegisterFileViewRoutes(router *gin.Engine, database *db.DB, cfg *config.Config, s3Store *storage.S3Store, tokenCreator TokenCreator, serverURL string, authMiddleware gin.HandlerFunc) {
	h := &FileViewHandler{
		db:           database,
		config:       cfg,
		storage:      s3Store,
		tokenCreator: tokenCreator,
		serverURL:    serverURL,
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

	// For other files, redirect to download
	h.redirectToDownload(c, repoID, filePath, filename)
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
// Used for images, videos, etc. that need to be displayed in the browser
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

	// Generate download token for inline display
	token, err := h.tokenCreator.CreateDownloadToken(orgID, repoID, filePath, userID)
	if err != nil {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusInternalServerError, errorPageHTML("Download Error", "Failed to generate download link."))
		return
	}

	// Redirect to seafhttp files endpoint
	// The seafhttp handler will serve the file with appropriate Content-Type
	downloadURL := h.serverURL + "/seafhttp/files/" + token + "/" + filename
	c.Redirect(http.StatusFound, downloadURL)
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
