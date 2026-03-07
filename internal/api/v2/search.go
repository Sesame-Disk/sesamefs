package v2

import (
	"net/http"
	"strings"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// SearchHandler handles search endpoints
type SearchHandler struct {
	db *db.DB
}

// NewSearchHandler creates a new search handler
func NewSearchHandler(database *db.DB) *SearchHandler {
	return &SearchHandler{db: database}
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
// Uses in-memory filtering for case-insensitive CONTAINS search
func (h *SearchHandler) Search(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query parameter 'q' is required"})
		return
	}

	orgID := c.GetString("org_id")
	if orgID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	repoIDParam := c.Query("repo_id")
	typeFilter := c.Query("type")

	var results []SearchResult

	// Search in libraries (repos) if type is "repo" or not specified
	if typeFilter == "" || typeFilter == "repo" {
		libraryResults, err := h.searchLibraries(orgID, query)
		if err == nil {
			results = append(results, libraryResults...)
		}
	}

	// Search in files and directories if type is "file", "dir", or not specified
	if typeFilter == "" || typeFilter == "file" || typeFilter == "dir" {
		fileResults, err := h.searchFiles(orgID, query, repoIDParam, typeFilter)
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
// Uses in-memory filtering since Cassandra 5 SAI doesn't support wildcard LIKE queries
func (h *SearchHandler) searchLibraries(orgID, query string) ([]SearchResult, error) {
	// Validate UUID format
	if _, err := uuid.Parse(orgID); err != nil {
		return nil, err
	}

	var results []SearchResult
	queryLower := strings.ToLower(query)

	// Get all libraries for the org and filter in memory
	// Pass orgID as string - gocql will handle UUID conversion
	iter := h.db.Session().Query("SELECT library_id, name FROM libraries WHERE org_id = ?", orgID).Iter()

	var libraryID string
	var name string

	for iter.Scan(&libraryID, &name) {
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
// Uses in-memory filtering since Cassandra 5 SAI doesn't support wildcard LIKE queries
// Only returns files from libraries the user has access to (same org)
func (h *SearchHandler) searchFiles(orgID, query, repoIDParam, typeFilter string) ([]SearchResult, error) {
	var results []SearchResult
	queryLower := strings.ToLower(query)

	// First, get all accessible libraries for this org (security check)
	// Build a map of libraryID -> libraryName for filtering and display
	accessibleLibraries := make(map[string]string)
	libIter := h.db.Session().Query("SELECT library_id, name FROM libraries WHERE org_id = ?", orgID).Iter()
	var libID, libName string
	for libIter.Scan(&libID, &libName) {
		accessibleLibraries[libID] = libName
	}
	if err := libIter.Close(); err != nil {
		return nil, err
	}

	// If no accessible libraries, return empty results
	if len(accessibleLibraries) == 0 {
		return results, nil
	}

	// If searching specific library, verify access
	if repoIDParam != "" {
		if _, err := uuid.Parse(repoIDParam); err != nil {
			return nil, err
		}
		if _, hasAccess := accessibleLibraries[repoIDParam]; !hasAccess {
			return results, nil // No access to this library
		}
	}

	// Search files - we'll filter by accessible libraries in the loop
	// Include full_path for correct navigation
	var queryStr string
	var args []interface{}

	if repoIDParam != "" {
		// Search within specific library
		if typeFilter != "" {
			queryStr = "SELECT library_id, fs_id, obj_type, obj_name, full_path, size_bytes, mtime FROM fs_objects WHERE library_id = ? AND obj_name > '' AND obj_type = ? ALLOW FILTERING"
			args = []interface{}{repoIDParam, typeFilter}
		} else {
			queryStr = "SELECT library_id, fs_id, obj_type, obj_name, full_path, size_bytes, mtime FROM fs_objects WHERE library_id = ? AND obj_name > '' ALLOW FILTERING"
			args = []interface{}{repoIDParam}
		}
	} else {
		// Search across all libraries - will filter by access in loop
		if typeFilter != "" {
			queryStr = "SELECT library_id, fs_id, obj_type, obj_name, full_path, size_bytes, mtime FROM fs_objects WHERE obj_name > '' AND obj_type = ? ALLOW FILTERING"
			args = []interface{}{typeFilter}
		} else {
			queryStr = "SELECT library_id, fs_id, obj_type, obj_name, full_path, size_bytes, mtime FROM fs_objects WHERE obj_name > '' ALLOW FILTERING"
			args = []interface{}{}
		}
	}

	iter := h.db.Session().Query(queryStr, args...).Iter()

	var libraryID string
	var fsID, objType, objName string
	var fullPath *string // nullable - may not be populated yet
	var sizeBytes, mtime int64

	const maxResults = 100 // Limit results to prevent large response
	for iter.Scan(&libraryID, &fsID, &objType, &objName, &fullPath, &sizeBytes, &mtime) {
		// Security check: only include files from accessible libraries
		repoName, hasAccess := accessibleLibraries[libraryID]
		if !hasAccess {
			continue
		}

		// Case-insensitive contains check
		if !strings.Contains(strings.ToLower(objName), queryLower) {
			continue
		}

		// Skip if type filter doesn't match
		if typeFilter != "" && objType != typeFilter {
			continue
		}

		// Use full_path from DB if available, otherwise fallback to just the name
		resultPath := "/" + objName
		if fullPath != nil && *fullPath != "" {
			resultPath = *fullPath
		}

		results = append(results, SearchResult{
			RepoID:   libraryID,
			RepoName: repoName,
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

	return results, nil
}
