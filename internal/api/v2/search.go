package v2

import (
	"fmt"
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
	Type     string `json:"type"` // "file" or "dir" or "repo"
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
// Uses Cassandra SASI indexes on obj_name and library name for case-insensitive CONTAINS search
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
func (h *SearchHandler) searchLibraries(orgID, query string) ([]SearchResult, error) {
	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		return nil, err
	}

	var results []SearchResult

	// Use SASI index on library name (case-insensitive CONTAINS)
	queryStr := fmt.Sprintf("SELECT library_id, name FROM libraries WHERE org_id = ? AND name LIKE '%%%s%%' ALLOW FILTERING", strings.ToLower(query))
	iter := h.db.Session().Query(queryStr, orgUUID).Iter()

	var libraryID uuid.UUID
	var name string

	for iter.Scan(&libraryID, &name) {
		results = append(results, SearchResult{
			RepoID:   libraryID.String(),
			RepoName: name,
			Name:     name,
			Type:     "repo",
		})
	}

	if err := iter.Close(); err != nil {
		return nil, err
	}

	return results, nil
}

// searchFiles searches for files and directories by name
func (h *SearchHandler) searchFiles(orgID, query, repoIDParam, typeFilter string) ([]SearchResult, error) {
	var results []SearchResult

	// Build query based on filters
	var queryStr string
	var args []interface{}

	if repoIDParam != "" {
		// Search within specific library
		repoUUID, err := uuid.Parse(repoIDParam)
		if err != nil {
			return nil, err
		}

		if typeFilter != "" {
			// Filter by library and type
			queryStr = fmt.Sprintf("SELECT library_id, fs_id, obj_type, obj_name, size_bytes, mtime FROM fs_objects WHERE library_id = ? AND obj_name LIKE '%%%s%%' AND obj_type = ? ALLOW FILTERING", strings.ToLower(query))
			args = []interface{}{repoUUID, typeFilter}
		} else {
			// Filter by library only
			queryStr = fmt.Sprintf("SELECT library_id, fs_id, obj_type, obj_name, size_bytes, mtime FROM fs_objects WHERE library_id = ? AND obj_name LIKE '%%%s%%' ALLOW FILTERING", strings.ToLower(query))
			args = []interface{}{repoUUID}
		}
	} else {
		// Search across all libraries (more expensive)
		// Note: This will scan all partitions - consider pagination for large datasets
		if typeFilter != "" {
			queryStr = fmt.Sprintf("SELECT library_id, fs_id, obj_type, obj_name, size_bytes, mtime FROM fs_objects WHERE obj_name LIKE '%%%s%%' AND obj_type = ? ALLOW FILTERING", strings.ToLower(query))
			args = []interface{}{typeFilter}
		} else {
			queryStr = fmt.Sprintf("SELECT library_id, fs_id, obj_type, obj_name, size_bytes, mtime FROM fs_objects WHERE obj_name LIKE '%%%s%%' ALLOW FILTERING", strings.ToLower(query))
			args = []interface{}{}
		}
	}

	iter := h.db.Session().Query(queryStr, args...).Iter()

	var libraryID uuid.UUID
	var fsID, objType, objName string
	var sizeBytes, mtime int64

	for iter.Scan(&libraryID, &fsID, &objType, &objName, &sizeBytes, &mtime) {
		// Skip if type filter doesn't match
		if typeFilter != "" && objType != typeFilter {
			continue
		}

		results = append(results, SearchResult{
			RepoID: libraryID.String(),
			Name:   objName,
			Type:   objType,
			Size:   sizeBytes,
			Mtime:  mtime,
		})
	}

	if err := iter.Close(); err != nil {
		return nil, err
	}

	return results, nil
}
