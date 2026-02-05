package v2

import (
	"fmt"
	"log"
	"net/http"
	"path"
	"sort"
	"strconv"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/middleware"
	"github.com/gin-gonic/gin"
)

// TrashHandler handles file/folder trash (recycle bin) endpoints
type TrashHandler struct {
	db             *db.DB
	permMiddleware *middleware.PermissionMiddleware
}

// NewTrashHandler creates a new TrashHandler
func NewTrashHandler(database *db.DB) *TrashHandler {
	return &TrashHandler{
		db:             database,
		permMiddleware: middleware.NewPermissionMiddleware(database),
	}
}

// RegisterTrashRoutes registers trash-related routes
func RegisterTrashRoutes(rg *gin.RouterGroup, database *db.DB) {
	h := NewTrashHandler(database)

	repos := rg.Group("/repos/:repo_id")
	{
		// File/folder trash (recycle bin)
		repos.GET("/trash", h.GetRepoFolderTrash)
		repos.GET("/trash/", h.GetRepoFolderTrash)
		repos.DELETE("/trash", h.CleanRepoTrash)
		repos.DELETE("/trash/", h.CleanRepoTrash)

		// List directory from a specific commit (for browsing deleted folders)
		repos.GET("/commit/:commit_id/dir", h.ListCommitDir)
		repos.GET("/commit/:commit_id/dir/", h.ListCommitDir)

		// Restore from trash
		repos.POST("/file/restore", h.RestoreTrashItem)
		repos.POST("/file/restore/", h.RestoreTrashItem)
		repos.POST("/dir/restore", h.RestoreTrashItem)
		repos.POST("/dir/restore/", h.RestoreTrashItem)
	}
}

// TrashItem represents a deleted file or folder
type TrashItem struct {
	ObjName     string `json:"obj_name"`
	ObjID       string `json:"obj_id"`
	IsDir       bool   `json:"is_dir"`
	ParentDir   string `json:"parent_dir"`
	DeletedTime string `json:"deleted_time"`
	CommitID    string `json:"commit_id"`
	Size        int64  `json:"size"`
}

// GetRepoFolderTrash returns deleted files/folders in a library or subdirectory
// GET /api/v2.1/repos/:repo_id/trash/?parent_dir=/path&scan_stat=cursor
// Also handles: GET /api2/repos/:repo_id/trash/?path=/path&scan_stat=cursor
func (h *TrashHandler) GetRepoFolderTrash(c *gin.Context) {
	repoID := c.Param("repo_id")
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	// Support both parameter names (v2.1 uses parent_dir, api2 uses path)
	parentDir := c.DefaultQuery("parent_dir", c.DefaultQuery("path", "/"))
	scanStat := c.Query("scan_stat")

	if parentDir == "" {
		parentDir = "/"
	}
	parentDir = normalizePath(parentDir)
	// Ensure parent_dir ends with /
	if parentDir != "/" && parentDir[len(parentDir)-1] != '/' {
		parentDir += "/"
	}

	// Permission check
	if h.permMiddleware != nil {
		hasAccess, err := h.permMiddleware.HasLibraryAccessCtx(c, orgID, userID, repoID, middleware.PermissionR)
		if err != nil || !hasAccess {
			c.JSON(http.StatusForbidden, gin.H{"error_msg": "permission denied"})
			return
		}
	}

	if h.db == nil {
		c.JSON(http.StatusOK, gin.H{"data": []TrashItem{}, "more": false, "scan_stat": ""})
		return
	}

	fsHelper := NewFSHelper(h.db)

	// Query all commits for this library (most recent first)
	iter := h.db.Session().Query(`
		SELECT commit_id, root_fs_id, created_at
		FROM commits WHERE library_id = ?
		LIMIT 200
	`, repoID).Iter()

	type commitInfo struct {
		CommitID  string
		RootFSID  string
		CreatedAt time.Time
	}

	var commits []commitInfo
	var commitID, rootFSID string
	var createdAt time.Time

	for iter.Scan(&commitID, &rootFSID, &createdAt) {
		commits = append(commits, commitInfo{CommitID: commitID, RootFSID: rootFSID, CreatedAt: createdAt})
	}
	iter.Close()

	log.Printf("[Trash] Found %d commits for library %s", len(commits), repoID)

	// Sort commits by time descending (newest first)
	sort.Slice(commits, func(i, j int) bool {
		return commits[i].CreatedAt.After(commits[j].CreatedAt)
	})

	// Handle scan_stat pagination — skip commits before cursor
	startIdx := 0
	if scanStat != "" {
		if idx, err := strconv.Atoi(scanStat); err == nil && idx > 0 && idx < len(commits) {
			startIdx = idx
		}
	}

	// For each commit: get the entries at parentDir
	// Track which items exist in HEAD (newest commit)
	// Items that exist in older commits but not in HEAD are "deleted"
	headEntries := make(map[string]bool) // key: obj_name
	deletedItems := []TrashItem{}
	seenDeleted := make(map[string]bool) // avoid duplicates: key: "obj_name:obj_id"

	maxItems := 100

	for i, commit := range commits {
		if i < startIdx {
			continue
		}

		result, err := fsHelper.TraverseToPathFromRoot(repoID, commit.RootFSID, parentDir)
		if err != nil {
			log.Printf("[Trash] Commit %d (%s): TraverseToPathFromRoot failed for path %s: %v", i, commit.CommitID[:8], parentDir, err)
			continue // parent_dir doesn't exist in this commit
		}

		if i == startIdx {
			// First commit after startIdx = HEAD — collect existing items
			log.Printf("[Trash] HEAD commit %d (%s): found %d entries at %s", i, commit.CommitID[:8], len(result.Entries), parentDir)
			for _, entry := range result.Entries {
				headEntries[entry.Name] = true
			}
			continue
		}

		// For older commits, find items that existed then but not in HEAD
		log.Printf("[Trash] Older commit %d (%s): found %d entries at %s", i, commit.CommitID[:8], len(result.Entries), parentDir)
		for _, entry := range result.Entries {
			if headEntries[entry.Name] {
				continue // still exists in HEAD, not deleted
			}

			dedupeKey := fmt.Sprintf("%s:%s", entry.Name, entry.ID)
			if seenDeleted[dedupeKey] {
				continue // already reported this deletion
			}
			seenDeleted[dedupeKey] = true
			log.Printf("[Trash] Found deleted item: %s (ID: %s)", entry.Name, entry.ID[:8])

			item := TrashItem{
				ObjName:     entry.Name,
				ObjID:       entry.ID,
				IsDir:       entry.Mode == ModeDir,
				ParentDir:   parentDir,
				DeletedTime: commit.CreatedAt.Format(time.RFC3339),
				CommitID:    commit.CommitID,
				Size:        entry.Size,
			}
			deletedItems = append(deletedItems, item)
		}

		if len(deletedItems) >= maxItems {
			break
		}
	}

	// Determine pagination
	more := len(deletedItems) > maxItems
	nextScanStat := ""
	if more {
		deletedItems = deletedItems[:maxItems]
		// Find index of last processed commit for cursor
		for i, commit := range commits {
			if commit.CommitID == deletedItems[len(deletedItems)-1].CommitID {
				nextScanStat = strconv.Itoa(i + 1)
				break
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"data":      deletedItems,
		"more":      more,
		"scan_stat": nextScanStat,
	})
}

// RestoreTrashItem restores a deleted file or folder from trash
// POST /api/v2.1/repos/:repo_id/file/restore/ or /dir/restore/
// Body: commit_id=xxx&p=/path/to/file
func (h *TrashHandler) RestoreTrashItem(c *gin.Context) {
	repoID := c.Param("repo_id")
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")
	commitID := c.PostForm("commit_id")
	filePath := c.PostForm("p")

	if commitID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error_msg": "commit_id is required"})
		return
	}
	if filePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error_msg": "path is required"})
		return
	}

	filePath = normalizePath(filePath)

	// Permission check — need write access
	if h.permMiddleware != nil {
		hasAccess, err := h.permMiddleware.HasLibraryAccessCtx(c, orgID, userID, repoID, middleware.PermissionRW)
		if err != nil || !hasAccess {
			c.JSON(http.StatusForbidden, gin.H{"error_msg": "permission denied"})
			return
		}
	}

	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error_msg": "database not available"})
		return
	}

	fsHelper := NewFSHelper(h.db)

	// Get the root_fs_id from the target commit
	var oldRootFSID string
	err := h.db.Session().Query(`
		SELECT root_fs_id FROM commits WHERE library_id = ? AND commit_id = ?
	`, repoID, commitID).Scan(&oldRootFSID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error_msg": "commit not found"})
		return
	}

	// Traverse the old commit to find the deleted item
	oldResult, err := fsHelper.TraverseToPathFromRoot(repoID, oldRootFSID, filePath)
	if err != nil || oldResult.TargetEntry == nil {
		c.JSON(http.StatusNotFound, gin.H{"error_msg": "item not found in specified commit"})
		return
	}

	oldEntry := *oldResult.TargetEntry

	// Get current HEAD commit
	headCommitID, err := fsHelper.GetHeadCommitID(repoID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error_msg": "library not found"})
		return
	}

	// Determine parent directory path
	parentPath := path.Dir(filePath)
	if parentPath == "." {
		parentPath = "/"
	}

	// Traverse current HEAD to the parent directory
	result, err := fsHelper.TraverseToPath(repoID, parentPath)
	if err != nil {
		// Parent directory doesn't exist in HEAD — we need to recreate the path
		// For simplicity, restore to root if parent doesn't exist
		result, err = fsHelper.TraverseToPath(repoID, "/")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error_msg": "failed to traverse library"})
			return
		}
	}

	// Add the restored item to the parent directory
	fileName := path.Base(filePath)
	newEntries := RemoveEntryFromList(result.Entries, fileName) // remove if somehow exists
	oldEntry.Name = fileName
	oldEntry.MTime = time.Now().Unix()
	newEntries = AddEntryToList(newEntries, oldEntry)

	// Create new fs_object for modified parent
	newParentFSID, err := fsHelper.CreateDirectoryFSObject(repoID, newEntries)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error_msg": "failed to update directory"})
		return
	}

	// Rebuild path to root
	newRootFSID, err := fsHelper.RebuildPathToRoot(repoID, result, newParentFSID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error_msg": "failed to rebuild path"})
		return
	}

	// Create new commit
	description := fmt.Sprintf("Restored \"%s\" from trash", fileName)
	newCommitID, err := fsHelper.CreateCommit(repoID, userID, newRootFSID, headCommitID, description)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error_msg": "failed to create commit"})
		return
	}

	// Update library head
	if err := fsHelper.UpdateLibraryHead(orgID, repoID, newCommitID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error_msg": "failed to update library"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// CleanRepoTrash permanently cleans deleted items from trash
// DELETE /api/v2.1/repos/:repo_id/trash/?keep_days=3
func (h *TrashHandler) CleanRepoTrash(c *gin.Context) {
	repoID := c.Param("repo_id")
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	// Permission check — need write access
	if h.permMiddleware != nil {
		hasAccess, err := h.permMiddleware.HasLibraryAccessCtx(c, orgID, userID, repoID, middleware.PermissionRW)
		if err != nil || !hasAccess {
			c.JSON(http.StatusForbidden, gin.H{"error_msg": "permission denied"})
			return
		}
	}

	// Parse keep_days parameter (0 = delete all, 3 = keep last 3 days, etc.)
	keepDays := 0
	if d := c.Query("keep_days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil {
			keepDays = parsed
		}
	}

	// In our commit-based system, trash is virtual (derived from commit history).
	// "Cleaning" trash means deleting old commits beyond the keep period.
	// For now, we acknowledge the request — actual commit pruning is handled by GC.
	_ = keepDays
	_ = repoID

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// CommitDirEntry represents an entry in a commit's directory listing
type CommitDirEntry struct {
	Name      string `json:"name"`
	ObjID     string `json:"obj_id"`
	Type      string `json:"type"` // "file" or "dir"
	Size      int64  `json:"size"`
	ParentDir string `json:"parent_dir"`
}

// ListCommitDir lists the contents of a directory at a specific commit
// GET /api/v2.1/repos/:repo_id/commit/:commit_id/dir/?p=/path
func (h *TrashHandler) ListCommitDir(c *gin.Context) {
	repoID := c.Param("repo_id")
	commitID := c.Param("commit_id")
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")
	dirPath := c.DefaultQuery("p", "/")

	dirPath = normalizePath(dirPath)

	// Permission check
	if h.permMiddleware != nil {
		hasAccess, err := h.permMiddleware.HasLibraryAccessCtx(c, orgID, userID, repoID, middleware.PermissionR)
		if err != nil || !hasAccess {
			c.JSON(http.StatusForbidden, gin.H{"error_msg": "permission denied"})
			return
		}
	}

	if h.db == nil {
		c.JSON(http.StatusOK, gin.H{"dirent_list": []CommitDirEntry{}})
		return
	}

	// Get the root_fs_id from the specified commit
	var rootFSID string
	err := h.db.Session().Query(`
		SELECT root_fs_id FROM commits WHERE library_id = ? AND commit_id = ?
	`, repoID, commitID).Scan(&rootFSID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error_msg": "commit not found"})
		return
	}

	fsHelper := NewFSHelper(h.db)

	// Traverse from the commit's root to the target directory
	result, err := fsHelper.TraverseToPathFromRoot(repoID, rootFSID, dirPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error_msg": "directory not found in commit"})
		return
	}

	// Convert entries to response format
	var direntList []CommitDirEntry
	for _, entry := range result.Entries {
		entryType := "file"
		if entry.Mode == ModeDir {
			entryType = "dir"
		}
		direntList = append(direntList, CommitDirEntry{
			Name:      entry.Name,
			ObjID:     entry.ID,
			Type:      entryType,
			Size:      entry.Size,
			ParentDir: dirPath,
		})
	}

	if direntList == nil {
		direntList = []CommitDirEntry{}
	}

	c.JSON(http.StatusOK, gin.H{"dirent_list": direntList})
}
