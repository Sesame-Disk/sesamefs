package v2

import (
	"net/http"
	"strings"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/middleware"
	"github.com/gin-gonic/gin"
)

// SearchHandler handles search endpoints
type SearchHandler struct {
	db             *db.DB
	permMiddleware *middleware.PermissionMiddleware
}

// NewSearchHandler creates a new search handler
func NewSearchHandler(database *db.DB) *SearchHandler {
	return &SearchHandler{
		db:             database,
		permMiddleware: middleware.NewPermissionMiddleware(database),
	}
}

// RegisterSearchRoutes registers search routes
func RegisterSearchRoutes(r *gin.RouterGroup, db *db.DB) {
	h := NewSearchHandler(db)

	r.GET("/search", h.Search)
	r.GET("/search/", h.Search)
}

// SearchResult represents a search result item
type SearchResult struct {
	RepoID   string `json:"repo_id"`
	RepoName string `json:"repo_name,omitempty"`
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	Fullpath string `json:"fullpath,omitempty"` // Required by frontend
	IsDir    bool   `json:"is_dir"`             // Required by frontend
	Type     string `json:"type"`               // "file" or "dir" or "repo"
	Size     int64  `json:"size,omitempty"`
	Mtime    int64  `json:"mtime,omitempty"`
}

// Search handles search requests
// GET /api/v2.1/search/?q=query&repo_id=xxx&type=file
//
// Query parameters:
//   - q (required): search query string
//   - repo_id (optional): limit search to specific library
//   - type (optional): filter by "file", "dir", or "repo"
//
// Uses in-memory filtering for case-insensitive CONTAINS search.
// Only returns results from libraries the user owns or that are shared with them.
// Excludes soft-deleted libraries.
func (h *SearchHandler) Search(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query parameter 'q' is required"})
		return
	}

	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")
	if orgID == "" || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	repoIDParam := c.Query("repo_id")
	typeFilter := c.Query("type")

	// Get all libraries the user has access to (owned + shared + group-shared)
	accessibleLibs, err := h.permMiddleware.GetUserLibraries(orgID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get accessible libraries"})
		return
	}

	// Build map of accessible library IDs for quick lookup
	accessibleMap := make(map[string]bool)
	for _, lib := range accessibleLibs {
		accessibleMap[lib.LibraryID.String()] = true
	}

	if len(accessibleMap) == 0 {
		c.JSON(http.StatusOK, gin.H{"results": []SearchResult{}, "total": 0})
		return
	}

	// If searching a specific repo, verify the user has access
	if repoIDParam != "" {
		if !accessibleMap[repoIDParam] {
			c.JSON(http.StatusOK, gin.H{"results": []SearchResult{}, "total": 0})
			return
		}
	}

	var results []SearchResult

	// Search in libraries (repos) if type is "repo" or not specified
	if typeFilter == "" || typeFilter == "repo" {
		libraryResults, err := h.searchLibraries(orgID, query, accessibleMap)
		if err == nil {
			results = append(results, libraryResults...)
		}
	}

	// Search in files and directories if type is "file", "dir", or not specified
	if typeFilter == "" || typeFilter == "file" || typeFilter == "dir" {
		fileResults, err := h.searchFiles(orgID, query, repoIDParam, typeFilter, accessibleMap)
		if err == nil {
			results = append(results, fileResults...)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"results": results,
		"total":   len(results),
	})
}

// searchLibraries searches for libraries by name
// Only returns libraries the user has access to and that are not soft-deleted.
func (h *SearchHandler) searchLibraries(orgID, query string, accessibleMap map[string]bool) ([]SearchResult, error) {
	var results []SearchResult
	queryLower := strings.ToLower(query)

	iter := h.db.Session().Query("SELECT library_id, name, deleted_at FROM libraries WHERE org_id = ?", orgID).Iter()

	var libraryID string
	var name string
	var deletedAt time.Time

	for iter.Scan(&libraryID, &name, &deletedAt) {
		// Skip soft-deleted libraries
		if !deletedAt.IsZero() {
			continue
		}

		// Skip libraries the user doesn't have access to
		if !accessibleMap[libraryID] {
			continue
		}

		// Case-insensitive contains check
		if !strings.Contains(strings.ToLower(name), queryLower) {
			continue
		}

		results = append(results, SearchResult{
			RepoID:   libraryID,
			RepoName: name,
			Name:     name,
			Fullpath: "/",
			IsDir:    true,
			Type:     "repo",
		})
	}

	if err := iter.Close(); err != nil {
		return nil, err
	}

	return results, nil
}

// searchFiles searches for files and directories by name
// Only searches within libraries the user has access to (owned + shared + group-shared).
// Iterates per-library using partition key to avoid full table scans.
func (h *SearchHandler) searchFiles(orgID, query, repoIDParam, typeFilter string, accessibleMap map[string]bool) ([]SearchResult, error) {
	var results []SearchResult
	queryLower := strings.ToLower(query)

	// Build a map of libraryID -> libraryName for display
	libraryNames := make(map[string]string)
	libIter := h.db.Session().Query("SELECT library_id, name, deleted_at FROM libraries WHERE org_id = ?", orgID).Iter()
	var libID, libName string
	var deletedAt time.Time
	for libIter.Scan(&libID, &libName, &deletedAt) {
		// Only include non-deleted, accessible libraries
		if !deletedAt.IsZero() || !accessibleMap[libID] {
			continue
		}
		libraryNames[libID] = libName
	}
	if err := libIter.Close(); err != nil {
		return nil, err
	}

	if len(libraryNames) == 0 {
		return results, nil
	}

	// Determine which libraries to search
	var libsToSearch []string
	if repoIDParam != "" {
		// Only search the specified library if user has access
		if _, ok := libraryNames[repoIDParam]; ok {
			libsToSearch = []string{repoIDParam}
		} else {
			return results, nil
		}
	} else {
		for id := range libraryNames {
			libsToSearch = append(libsToSearch, id)
		}
	}

	const maxResults = 100

	// Search each accessible library individually (uses partition key, no full scan)
	for _, libraryID := range libsToSearch {
		if len(results) >= maxResults {
			break
		}

		iter := h.db.Session().Query(
			"SELECT fs_id, obj_type, obj_name, full_path, size_bytes, mtime FROM fs_objects WHERE library_id = ?",
			libraryID,
		).Iter()

		var fsID, objType, objName string
		var fullPath *string
		var sizeBytes, mtime int64

		for iter.Scan(&fsID, &objType, &objName, &fullPath, &sizeBytes, &mtime) {
			// Skip if type filter doesn't match
			if typeFilter != "" && objType != typeFilter {
				continue
			}

			// Case-insensitive contains check
			if !strings.Contains(strings.ToLower(objName), queryLower) {
				continue
			}

			// Use full_path from DB if available, otherwise fallback to just the name
			resultPath := "/" + objName
			if fullPath != nil && *fullPath != "" {
				resultPath = *fullPath
			}

			results = append(results, SearchResult{
				RepoID:   libraryID,
				RepoName: libraryNames[libraryID],
				Name:     objName,
				Fullpath: resultPath,
				IsDir:    objType == "dir",
				Type:     objType,
				Size:     sizeBytes,
				Mtime:    mtime,
			})

			if len(results) >= maxResults {
				break
			}
		}

		if err := iter.Close(); err != nil {
			return nil, err
		}
	}

	return results, nil
}
