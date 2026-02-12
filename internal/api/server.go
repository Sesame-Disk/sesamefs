package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/api/v2"
	"github.com/Sesame-Disk/sesamefs/internal/config"
	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/Sesame-Disk/sesamefs/internal/health"
	"github.com/Sesame-Disk/sesamefs/internal/logging"
	"github.com/Sesame-Disk/sesamefs/internal/metrics"
	"github.com/Sesame-Disk/sesamefs/internal/middleware"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server represents the HTTP API server
type Server struct {
	config         *config.Config
	db             *db.DB
	storage        *storage.S3Store    // Legacy single S3 store
	storageManager *storage.Manager    // Multi-backend storage manager
	blockStore     *storage.BlockStore // Legacy single block store
	tokenStore     TokenStore
	permMiddleware *middleware.PermissionMiddleware
	authHandler    *v2.AuthHandler // OIDC authentication handler
	gcService      *gc.Service     // Garbage collection service
	version        string          // Build version string
	router         *gin.Engine
	server         *http.Server
}

// NewServer creates a new API server
func NewServer(cfg *config.Config, database *db.DB, version string) *Server {
	// Set Gin mode based on dev mode
	if !cfg.Auth.DevMode {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	// Disable trailing slash redirect - Seafile clients send POST to /api2/repos/
	// and Gin's 307 redirect breaks POST requests
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false
	router.HandleMethodNotAllowed = true

	router.Use(gin.Recovery())
	router.Use(logging.GinMiddleware())

	// Register Prometheus metrics and add metrics middleware
	if cfg.Monitoring.MetricsEnabled {
		metrics.Register()
		router.Use(metrics.GinMiddleware())
	}

	// CORS middleware for frontend access
	corsConfig := cors.Config{
		AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders: []string{
			"Origin",
			"Content-Type",
			"Content-Length",
			"Content-Range",       // Required for resumable.js chunked uploads
			"Content-Disposition", // Required for filename in uploads
			"Accept",
			"Authorization",
			"Seafile-Repo-Token",
			"X-Requested-With", // Common AJAX header
		},
		ExposeHeaders:    []string{"Content-Length", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}
	// In dev mode, allow all origins; in production, use configured origins
	if cfg.Auth.DevMode {
		corsConfig.AllowAllOrigins = true
	} else if len(cfg.CORS.AllowedOrigins) > 0 {
		corsConfig.AllowOrigins = cfg.CORS.AllowedOrigins
	} else {
		// Default to allowing all origins if not configured
		corsConfig.AllowAllOrigins = true
	}
	router.Use(cors.New(corsConfig))

	// Initialize storage manager with multi-backend support
	storageManager := initStorageManager(cfg)

	// Initialize legacy S3 storage (for backward compatibility)
	s3Store, err := initS3Storage(cfg)
	if err != nil {
		slog.Warn("Failed to initialize legacy S3 storage", "error", err)
		// Continue without S3 - file operations will fail gracefully
	}

	// Initialize token store for seafhttp
	// Use Cassandra-backed store if database is available (stateless, distributed)
	// Fall back to in-memory store if database is not available
	var tokenStore TokenStore
	if database != nil {
		dbTokenStore := db.NewTokenStore(database, cfg.SeafHTTP.TokenTTL)
		tokenStore = NewCassandraTokenAdapter(dbTokenStore)
		slog.Info("Using Cassandra-backed token store (stateless, distributed)")
	} else {
		tokenStore = NewTokenManager(cfg.SeafHTTP.TokenTTL)
		slog.Info("Using in-memory token store (not distributed)")
	}

	// Initialize block store for content-addressable storage
	var blockStore *storage.BlockStore
	if s3Store != nil {
		blockStore = storage.NewBlockStore(s3Store, "blocks/")
	}

	// Initialize permission middleware
	permMiddleware := middleware.NewPermissionMiddleware(database)

	// Initialize OIDC auth handler
	authHandler := v2.NewAuthHandler(database, cfg)

	// Initialize GC service
	var gcService *gc.Service
	if database != nil {
		store := gc.NewCassandraStore(database)
		var storageProvider gc.StorageProvider
		if storageManager != nil {
			storageProvider = gc.NewStorageManagerAdapter(storageManager)
		}
		gcService = gc.NewService(store, storageProvider, cfg.GC)
	}

	s := &Server{
		config:         cfg,
		db:             database,
		storage:        s3Store,
		storageManager: storageManager,
		blockStore:     blockStore,
		tokenStore:     tokenStore,
		permMiddleware: permMiddleware,
		authHandler:    authHandler,
		gcService:      gcService,
		version:        version,
		router:         router,
	}

	s.setupRoutes()

	// Start GC service after routes are set up
	if gcService != nil {
		gcService.Start()
	}

	return s
}

// initStorageManager initializes the multi-backend storage manager
func initStorageManager(cfg *config.Config) *storage.Manager {
	manager := storage.NewManager()

	// Set default class
	if cfg.Storage.DefaultClass != "" {
		manager.SetDefaultClass(cfg.Storage.DefaultClass)
	}

	// Set endpoint to region mapping
	if cfg.Storage.EndpointRegions != nil {
		manager.SetEndpointRegions(cfg.Storage.EndpointRegions)
	}

	// Set region to class mapping
	if cfg.Storage.RegionClasses != nil {
		regionClasses := make(map[string]storage.RegionClassConfig)
		for region, classes := range cfg.Storage.RegionClasses {
			regionClasses[region] = storage.RegionClassConfig{
				Hot:  classes.Hot,
				Cold: classes.Cold,
			}
		}
		manager.SetRegionClasses(regionClasses)
	}

	// Initialize storage classes from config
	for className, classCfg := range cfg.Storage.Classes {
		s3Store, err := initStorageClass(className, classCfg)
		if err != nil {
			slog.Warn("Failed to initialize storage class", "class", className, "error", err)
			continue
		}
		manager.RegisterBackend(className, s3Store, classCfg.FailoverClass)
		slog.Info("Registered storage class", "class", className, "type", classCfg.Type, "tier", classCfg.Tier, "bucket", classCfg.Bucket)
	}

	// Log summary
	backends := manager.ListBackends()
	slog.Info("Storage manager initialized", "backend_count", len(backends), "backends", backends)

	return manager
}

// initStorageClass creates an S3Store for a storage class config
func initStorageClass(name string, cfg config.StorageClassConfig) (*storage.S3Store, error) {
	// Get credentials from config or environment
	accessKey := cfg.AccessKey
	secretKey := cfg.SecretKey
	if accessKey == "" {
		accessKey = os.Getenv("AWS_ACCESS_KEY_ID")
	}
	if secretKey == "" {
		secretKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	}

	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	// Determine access type from tier
	accessType := storage.AccessImmediate
	if cfg.Tier == "cold" {
		accessType = storage.AccessDelayed
	}

	s3Cfg := storage.S3Config{
		Endpoint:        cfg.Endpoint,
		Bucket:          cfg.Bucket,
		Region:          region,
		AccessKeyID:     accessKey,
		SecretAccessKey: secretKey,
		UsePathStyle:    cfg.UsePathStyle || cfg.Endpoint != "",
		AccessType:      accessType,
	}

	return storage.NewS3Store(context.Background(), s3Cfg)
}

// initS3Storage initializes the S3 storage backend (legacy, single backend)
func initS3Storage(cfg *config.Config) (*storage.S3Store, error) {
	// Get S3 configuration from environment or config
	endpoint := os.Getenv("S3_ENDPOINT")
	bucket := os.Getenv("S3_BUCKET")
	region := os.Getenv("AWS_REGION")
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")

	// Fall back to config if not in environment
	if bucket == "" {
		// Try new storage classes first
		if defaultClass, ok := cfg.Storage.Classes[cfg.Storage.DefaultClass]; ok {
			if endpoint == "" {
				endpoint = defaultClass.Endpoint
			}
			bucket = defaultClass.Bucket
			if region == "" {
				region = defaultClass.Region
			}
			if accessKey == "" {
				accessKey = defaultClass.AccessKey
			}
			if secretKey == "" {
				secretKey = defaultClass.SecretKey
			}
		} else if hotBackend, ok := cfg.Storage.Backends["hot"]; ok {
			// Fall back to legacy backends
			if endpoint == "" {
				endpoint = hotBackend.Endpoint
			}
			bucket = hotBackend.Bucket
			if region == "" {
				region = hotBackend.Region
			}
		}
	}

	if bucket == "" {
		return nil, fmt.Errorf("S3 bucket not configured")
	}

	if region == "" {
		region = "us-east-1"
	}

	s3Cfg := storage.S3Config{
		Endpoint:        endpoint,
		Bucket:          bucket,
		Region:          region,
		AccessKeyID:     accessKey,
		SecretAccessKey: secretKey,
		UsePathStyle:    endpoint != "", // Use path style for custom endpoints (MinIO)
		AccessType:      storage.AccessImmediate,
	}

	return storage.NewS3Store(context.Background(), s3Cfg)
}

// setupRoutes configures all API routes
func (s *Server) setupRoutes() {
	// ========================================================================
	// PERMISSION SYSTEM OVERVIEW
	// ========================================================================
	//
	// Permission checks are enforced at multiple levels:
	//
	// 1. AUTHENTICATION (applied via authMiddleware at group level)
	//    - Validates user token and sets user_id + org_id in context
	//    - All protected routes require authentication
	//
	// 2. ORGANIZATION ROLES (checked in handlers where needed)
	//    - admin: Full organization access
	//    - user: Can create libraries, upload files, share
	//    - readonly: Can only view content
	//    - guest: Limited access to shared content only
	//
	// 3. LIBRARY PERMISSIONS (checked in handlers)
	//    - owner: Full control (delete library, manage shares)
	//    - rw: Read and write files
	//    - r: Read-only access
	//    - Includes permissions inherited from group shares
	//
	// 4. GROUP ROLES (checked in group handlers)
	//    - owner: Created the group, can delete, manage all members
	//    - admin: Can add/remove members (except owner)
	//    - member: Regular member, no management privileges
	//
	// Permission middleware is available via s.permMiddleware but most checks
	// are done inside handlers to allow for flexible logic (e.g., owner OR rw permission).
	//
	// See: internal/middleware/permissions.go for implementation details
	//      internal/middleware/README.md for usage examples
	//
	// ========================================================================

	// Set up GC hooks so that v2 handlers can enqueue blocks/libraries for garbage collection
	if s.gcService != nil {
		v2.SetGCHooks(
			&gcBlockEnqueuer{service: s.gcService},
			&gcLibraryEnqueuer{service: s.gcService},
		)
	}

	// Health check endpoints
	s.router.GET("/ping", s.handlePing)

	// Build health checker for liveness and readiness probes
	var storageChecker health.StorageChecker
	if s.storage != nil {
		storageChecker = s.storage
	}
	healthChecker := health.NewChecker(s.db, storageChecker, s.config.Monitoring.HealthTimeout, s.version)
	s.router.GET("/health", healthChecker.HandleLiveness)
	s.router.GET("/ready", healthChecker.HandleReadiness)

	// Prometheus metrics endpoint
	if s.config.Monitoring.MetricsEnabled {
		s.router.GET(s.config.Monitoring.MetricsPath, gin.WrapH(promhttp.Handler()))
	}

	// Logout endpoint - clears token and redirects to home
	s.router.GET("/accounts/logout", s.handleLogout)
	s.router.GET("/accounts/logout/", s.handleLogout)

	// Auto-login endpoint for desktop client "View on Cloud" feature
	s.router.GET("/client-login", s.handleAutoLogin)
	s.router.GET("/client-login/", s.handleAutoLogin)

	// Determine server URL for generating seafhttp URLs
	// FILE_SERVER_ROOT takes highest priority (like Seahub's FILE_SERVER_ROOT setting)
	// SERVER_URL is second priority
	// Auto-detection from request Host is the fallback
	serverURL := os.Getenv("FILE_SERVER_ROOT")
	if serverURL != "" {
		// FILE_SERVER_ROOT should be the full base URL (e.g., http://localhost:8080)
		// Strip trailing /seafhttp if present — we append it ourselves
		serverURL = strings.TrimSuffix(serverURL, "/seafhttp")
		serverURL = strings.TrimSuffix(serverURL, "/")
	} else if v := os.Getenv("SERVER_URL"); v != "" {
		serverURL = strings.TrimSuffix(v, "/")
	} else {
		// Default to empty string — getBrowserURL will auto-detect from request
		serverURL = ""
	}

	// API v2 routes
	apiV2 := s.router.Group("/api/v2")
	{
		// Public endpoints
		apiV2.GET("/ping", s.handlePing)

		// OIDC auth endpoints (public - no auth required for login flow)
		v2.RegisterAuthRoutes(apiV2, s.db, s.config)

		// Protected endpoints - require authentication
		protected := apiV2.Group("")
		protected.Use(s.authMiddleware())
		{
			// Library endpoints (with token creator for sync token generation)
			v2.RegisterLibraryRoutesWithToken(protected, s.db, s.config, s.tokenStore)

			// File endpoints (with Seafile-compatible URL generation)
			v2.RegisterFileRoutes(protected, s.db, s.config, s.storage, s.tokenStore, serverURL)

			// Block endpoints (content-addressable storage)
			if s.blockStore != nil || s.storageManager != nil {
				v2.RegisterBlockRoutes(protected, s.blockStore, s.storageManager, s.config)
			}

			// Share link endpoints
			v2.RegisterShareRoutes(protected, s.db, s.config)

			// Restore job endpoints (Glacier)
			v2.RegisterRestoreRoutes(protected, s.db, s.config)
		}
	}

	// Legacy /api2/ routes for Seafile CLI compatibility
	// The Seafile CLI uses /api2/ prefix (no version in path)
	// Routes registered WITHOUT trailing slashes since our wrapper strips them from requests
	api2 := s.router.Group("/api2")
	{
		// Auth token endpoint (used by seaf-cli for login)
		api2.POST("/auth-token", s.handleAuthToken)

		// Client login (desktop client "View on Cloud" auto-login)
		api2.POST("/client-login", s.handleClientLogin)

		// Ping/server info
		api2.GET("/ping", s.handlePing)
		api2.GET("/server-info", s.handleServerInfo)

		// Account info
		api2.GET("/account/info", s.authMiddleware(), s.handleAccountInfo)

		// Starred files API
		v2.RegisterStarredRoutes(api2.Group("", s.authMiddleware()), s.db)

		// User avatars (stub - returns placeholder)
		api2.GET("/avatars/user/:email/resized/:size", s.handleUserAvatar)
		api2.GET("/avatars/user/:email/resized/:size/", s.handleUserAvatar)

		// Protected endpoints
		protected := api2.Group("")
		protected.Use(s.authMiddleware())
		{
			// Library endpoints (same handlers as v2, with token creator)
			v2.RegisterLibraryRoutesWithToken(protected, s.db, s.config, s.tokenStore)

			// File endpoints
			v2.RegisterFileRoutes(protected, s.db, s.config, s.storage, s.tokenStore, serverURL)

			// File sharing endpoints (shared-repos, beshared-repos)
			v2.RegisterFileShareRoutes(protected, s.db)

			// User search endpoint (used by transfer dialog, share dialog)
			protected.GET("/search-user", s.handleSearchUser)
			protected.GET("/search-user/", s.handleSearchUser)

			// Search routes (seafile-js calls /api2/search/)
			v2.RegisterSearchRoutes(protected, s.db)

			// File/folder trash (recycle bin) routes
			v2.RegisterTrashRoutes(protected, s.db)

			// Deleted libraries (library recycle bin) — list endpoint for /api2/
			v2.RegisterDeletedLibraryRoutes(protected, s.db, nil)

			// Repo tokens endpoint (for getting sync tokens for multiple repos)
			protected.GET("/repo-tokens", s.handleRepoTokens)

			// History limit settings (GET/PUT /api2/repos/:id/history-limit/)
			v2.RegisterHistoryLimitRoutes(protected, s.db, s.config)

			// Library transfer (PUT /api2/repos/:id/owner/)
			v2.RegisterLibraryTransferRoutes(protected, s.db, s.config)

			// Stub: Linked devices (not yet implemented)
			protected.GET("/devices", s.handleEmptyDevices)
			protected.GET("/devices/", s.handleEmptyDevices)
		}
	}

	// API v2.1 routes for Seahub frontend compatibility
	// The Seahub frontend uses /api/v2.1/ prefix with different response format
	apiV21 := s.router.Group("/api/v2.1")
	{
		// OIDC auth endpoints (public - no auth required for login flow)
		v2.RegisterAuthRoutes(apiV21, s.db, s.config)

		// Protected endpoints
		protected := apiV21.Group("")
		protected.Use(s.authMiddleware())
		{
			// Admin API endpoints (superadmin and tenant admin)
			v2.RegisterAdminRoutes(protected, s.db, s.config, s.permMiddleware)

			// GC admin endpoints (staff only)
			if s.gcService != nil {
				admin := protected.Group("/admin")
				admin.GET("/gc/status", s.handleGCStatus)
				admin.GET("/gc/status/", s.handleGCStatus)
				admin.POST("/gc/run", s.handleGCRun)
				admin.POST("/gc/run/", s.handleGCRun)
			}

			// Library endpoints with v2.1 response format
			v2.RegisterV21LibraryRoutes(protected, s.db, s.config, s.tokenStore, s.storage, s.blockStore, serverURL)

			// Batch delete endpoint for files/folders
			fileHandler := v2.NewFileHandler(s.db, s.config, s.storage, s.tokenStore, serverURL, s.permMiddleware)
			fileHandler.SetGCEnqueuer(v2.GetBlockEnqueuerFunc())
			protected.DELETE("/repos/batch-delete-item/", fileHandler.BatchDeleteItems)
			protected.DELETE("/repos/batch-delete-item", fileHandler.BatchDeleteItems)

			// Batch move/copy endpoints (sync and async)
			v2.RegisterBatchOperationRoutes(protected, s.db, s.config)

			// File history endpoint (seafile-js uses /api/v2.1/repos/:id/file/new_history/)
			protected.GET("/repos/:repo_id/file/new_history/", fileHandler.GetFileHistoryV21)
			protected.GET("/repos/:repo_id/file/new_history", fileHandler.GetFileHistoryV21)
			protected.GET("/repos/:repo_id/file/history/", fileHandler.GetFileHistoryV21)
			protected.GET("/repos/:repo_id/file/history", fileHandler.GetFileHistoryV21)

			// Repository (commit) history endpoint
			protected.GET("/repos/:repo_id/history/", fileHandler.GetRepoHistory)
			protected.GET("/repos/:repo_id/history", fileHandler.GetRepoHistory)

			// OnlyOffice integration endpoints
			v2.RegisterOnlyOfficeRoutes(protected, s.db, s.config, s.storage, s.tokenStore, serverURL)

			// Starred items for v2.1 API (uses "starred-items" instead of "starredfiles")
			v2.RegisterV21StarredRoutes(protected, s.db)

			// Share links for v2.1 API
			v2.RegisterShareLinkRoutes(protected, s.db, serverURL)

			// Groups for v2.1 API
			v2.RegisterGroupRoutes(protected, s.db)

			// Monitored repos (watch/unwatch libraries)
			v2.RegisterMonitoredRepoRoutes(protected, s.db)

			// Search routes (Cassandra SASI-based search)
			v2.RegisterSearchRoutes(protected, s.db)

			// Stub handlers for optional Seahub features (return empty results instead of 404)
			protected.GET("/activities", s.handleEmptyActivities)
			protected.GET("/activities/", s.handleEmptyActivities)
			protected.GET("/notifications", s.handleEmptyNotifications)
			protected.GET("/notifications/", s.handleEmptyNotifications)
			protected.GET("/shared-repos", s.handleEmptySharedRepos)
			protected.GET("/shared-repos/", s.handleEmptySharedRepos)
			protected.GET("/shared-folders", s.handleEmptySharedFolders)
			protected.GET("/shared-folders/", s.handleEmptySharedFolders)
			protected.GET("/wikis", s.handleEmptyWikis)
			protected.GET("/wikis/", s.handleEmptyWikis)
			protected.GET("/repo-folder-share-info", s.handleEmptyFolderShareInfo)
			protected.GET("/repo-folder-share-info/", s.handleEmptyFolderShareInfo)

			// User search endpoint (used by transfer dialog, share dialog)
			protected.GET("/search-user", s.handleSearchUser)
			protected.GET("/search-user/", s.handleSearchUser)

			// Departments (hierarchical groups managed by org admins)
			v2.RegisterDepartmentRoutes(protected, s.db, s.permMiddleware)

			// Library settings endpoints (auto-delete, API tokens)
			v2.RegisterV21LibrarySettingsRoutes(protected, s.db, s.config)

			// Share links per repo - handled by RegisterShareLinkRoutes

			// Tag routes (fully implemented)
			v2.RegisterTagRoutes(protected, s.db)
		}
	}

	// OnlyOffice callback endpoints (not behind auth - OnlyOffice server calls these)
	onlyoffice := s.router.Group("/onlyoffice")
	{
		v2.RegisterOnlyOfficeCallbackRoutes(onlyoffice, s.db, s.config, s.storage, serverURL)
	}

	// Public share link view (no auth middleware - validated by share link token)
	slv := v2.NewShareLinkViewHandler(s.db, s.config, s.storage, s.storageManager, s.tokenStore, serverURL)
	s.router.GET("/d/:token", slv.ServeShareLinkPage)

	// Share link directory listing API (public, token-validated internally)
	s.router.GET("/api/v2.1/share-links/:token/dirents/", slv.ListShareLinkDirents)
	s.router.GET("/api/v2.1/share-links/:token/dirents", slv.ListShareLinkDirents)

	// Share link repo tags API (returns empty - tags are user-specific organization)
	s.router.GET("/api/v2.1/share-links/:token/repo-tags/", slv.GetShareLinkRepoTags)
	s.router.GET("/api/v2.1/share-links/:token/repo-tags", slv.GetShareLinkRepoTags)

	// Office document conversion stub (no converter configured)
	officeConvertStub := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ERROR"})
	}
	s.router.GET("/office-convert/status/", officeConvertStub)
	s.router.GET("/office-convert/status", officeConvertStub)

	// File viewer routes (for viewing files in browser, including OnlyOffice editor)
	v2.RegisterFileViewRoutes(s.router, s.db, s.config, s.storage, s.storageManager, s.tokenStore, serverURL, s.authMiddleware())

	// Seafile-compatible file transfer endpoints (seafhttp)
	// These endpoints handle the actual file uploads/downloads
	seafHTTPHandler := NewSeafHTTPHandler(s.storage, s.storageManager, s.db, s.tokenStore, s.permMiddleware)
	seafHTTPHandler.RegisterSeafHTTPRoutes(s.router)

	// Seafile sync protocol endpoints (for Desktop client)
	// These endpoints handle repository synchronization
	// Uses a different auth middleware that accepts repo tokens
	syncHandler := NewSyncHandler(s.db, s.storage, s.blockStore, s.storageManager, s.permMiddleware)
	syncHandler.SetTokenCreator(s.tokenStore) // Enable download-info endpoint
	syncHandler.RegisterSyncRoutes(s.router, s.syncAuthMiddleware())

	// Serve static files from frontend build
	s.router.Static("/static", "./frontend/build/static")
	s.router.Static("/media", "./frontend/public/media")

	// SPA catch-all: serve appropriate HTML for non-API routes
	// - /sys/* routes → sysadmin.html (admin panel, separate webpack entry)
	// - everything else → index.html (main app)
	s.router.NoRoute(func(c *gin.Context) {
		// Don't serve HTML for API routes
		if strings.HasPrefix(c.Request.URL.Path, "/api") ||
			strings.HasPrefix(c.Request.URL.Path, "/seafhttp") ||
			strings.HasPrefix(c.Request.URL.Path, "/onlyoffice") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		// Serve admin panel for /sys/* routes
		if strings.HasPrefix(c.Request.URL.Path, "/sys/") || c.Request.URL.Path == "/sys" {
			c.File("./frontend/build/sysadmin.html")
			return
		}
		c.File("./frontend/build/index.html")
	})
}

// authMiddleware validates authentication tokens
func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Helper to use anonymous access (first dev token)
		useAnonymous := func() bool {
			if s.config.Auth.AllowAnonymous && s.config.Auth.DevMode && len(s.config.Auth.DevTokens) > 0 {
				c.Set("user_id", s.config.Auth.DevTokens[0].UserID)
				c.Set("org_id", s.config.Auth.DevTokens[0].OrgID)
				c.Next()
				return true
			}
			return false
		}

		// Get token from header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			// Allow anonymous access if configured (FOR TESTING ONLY)
			if useAnonymous() {
				return
			}
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			c.Abort()
			return
		}

		// Parse "Token <token>" format (Seafile compatible)
		var token string
		if _, err := fmt.Sscanf(authHeader, "Token %s", &token); err != nil {
			// Try "Bearer <token>" format
			if _, err := fmt.Sscanf(authHeader, "Bearer %s", &token); err != nil {
				// Invalid format - try anonymous fallback
				if useAnonymous() {
					return
				}
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header format"})
				c.Abort()
				return
			}
		}

		// Check for empty/invalid token values
		if token == "" || token == "undefined" || token == "null" {
			if useAnonymous() {
				return
			}
			c.JSON(http.StatusUnauthorized, gin.H{"error": "empty or invalid token"})
			c.Abort()
			return
		}

		// In dev mode, check dev tokens first
		if s.config.Auth.DevMode {
			for _, devToken := range s.config.Auth.DevTokens {
				if devToken.Token == token {
					c.Set("user_id", devToken.UserID)
					c.Set("org_id", devToken.OrgID)
					if devToken.Role != "" {
						c.Set("role", devToken.Role)
					}
					c.Next()
					return
				}
			}
		}

		// Try to validate as OIDC session token
		if s.authHandler != nil {
			sessionMgr := s.authHandler.GetSessionManager()
			if sessionMgr != nil {
				session, err := sessionMgr.ValidateSession(token)
				if err == nil {
					c.Set("user_id", session.UserID)
					c.Set("org_id", session.OrgID)
					c.Set("email", session.Email)
					c.Set("role", session.Role)
					c.Next()
					return
				}
			}
		}

		// Try to validate as a repo API token (library-scoped access)
		if s.db != nil {
			var repoID, permission, generatedBy string
			err := s.db.Session().Query(`
				SELECT repo_id, permission, generated_by FROM repo_api_tokens_by_token WHERE api_token = ?
			`, token).Scan(&repoID, &permission, &generatedBy)
			if err == nil {
				// Repo API token found — look up the library's org_id
				var orgID string
				if err := s.db.Session().Query(`
					SELECT org_id FROM libraries_by_id WHERE library_id = ?
				`, repoID).Scan(&orgID); err == nil {
					c.Set("user_id", generatedBy)
					c.Set("org_id", orgID)
					c.Set("repo_api_token", true)
					c.Set("repo_api_token_repo_id", repoID)
					c.Set("repo_api_token_permission", permission)
					c.Next()
					return
				}
			}
		}

		// Token not found - try anonymous fallback before rejecting
		if useAnonymous() {
			return
		}

		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		c.Abort()
	}
}

// syncAuthMiddleware validates authentication for sync protocol endpoints
// It accepts multiple auth methods:
// 1. Seafile-Repo-Token header (repo-specific token from download-info)
// 2. Authorization: Token header (standard API token)
// 3. token query parameter
func (s *Server) syncAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		var token string

		// Try Seafile-Repo-Token header first (used by desktop client)
		token = c.GetHeader("Seafile-Repo-Token")

		// Try Authorization header if Seafile-Repo-Token not present
		if token == "" {
			authHeader := c.GetHeader("Authorization")
			if authHeader != "" {
				fmt.Sscanf(authHeader, "Token %s", &token)
				if token == "" {
					fmt.Sscanf(authHeader, "Bearer %s", &token)
				}
			}
		}

		// Try query parameter as last resort
		if token == "" {
			token = c.Query("token")
		}

		// No token provided — reject
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			c.Abort()
			return
		}

		// Check if it's a dev token (only in dev mode)
		if s.config.Auth.DevMode {
			for _, devToken := range s.config.Auth.DevTokens {
				if devToken.Token == token {
					c.Set("user_id", devToken.UserID)
					c.Set("org_id", devToken.OrgID)
					c.Next()
					return
				}
			}
		}

		// Check if it's a valid repo token (from download-info)
		if accessToken, valid := s.tokenStore.GetToken(token, TokenTypeDownload); valid {
			c.Set("user_id", accessToken.UserID)
			c.Set("org_id", accessToken.OrgID)
			c.Set("repo_id", accessToken.RepoID)
			c.Next()
			return
		}

		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		c.Abort()
	}
}

// handlePing returns a simple pong response
func (s *Server) handlePing(c *gin.Context) {
	c.String(http.StatusOK, "pong")
}

// handleNotImplemented returns a 501 Not Implemented response
func (s *Server) handleNotImplemented(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented yet"})
}

// handleAuthToken handles the Seafile CLI auth-token endpoint
// POST /api2/auth-token/ with username and password
func (s *Server) handleAuthToken(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")

	if username == "" || password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password required"})
		return
	}

	// In dev mode, check dev tokens by matching username
	if s.config.Auth.DevMode {
		for _, devToken := range s.config.Auth.DevTokens {
			// In dev mode, accept multiple username formats:
			// 1. UUID directly (e.g., "00000000-0000-0000-0000-000000000001")
			// 2. UUID@sesamefs.local (e.g., "00000000-0000-0000-0000-000000000001@sesamefs.local")
			// 3. Friendly email matching this specific devToken (e.g., "admin@sesamefs.local" only for admin token)
			// 4. Token as password (Seafile CLI compatibility)

			expectedEmail := devToken.UserID + "@sesamefs.local"

			// Check if username matches THIS specific devToken
			// Note: devToken.Email is the friendly email like "admin@sesamefs.local"
			usernameMatches := (devToken.UserID == username ||
				expectedEmail == username ||
				(devToken.Email != "" && devToken.Email == username))

			if usernameMatches || devToken.Token == password {
				c.JSON(http.StatusOK, gin.H{
					"token": devToken.Token,
				})
				return
			}
		}
	}

	// TODO: Implement OIDC password grant or redirect to OIDC flow
	c.JSON(http.StatusUnauthorized, gin.H{
		"non_field_errors": "Unable to login with provided credentials.",
	})
}

// handleServerInfo returns server information for Seafile clients
// GET /api2/server-info/
func (s *Server) handleServerInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version":                     "10.0.0",  // Seafile version we're compatible with
		"encrypted_library_version":  2,
		"enable_encrypted_library":   true,
		"enable_repo_history_setting": true,
		"enable_reset_encrypted_repo_password": false,
	})
}

// handleClientLogin generates a one-time login token for desktop client auto-login
// POST /api2/client-login/
func (s *Server) handleClientLogin(c *gin.Context) {
	// User must be authenticated
	token := c.GetHeader("Authorization")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	// Remove "Token " prefix if present
	token = strings.TrimPrefix(token, "Token ")
	token = strings.TrimSpace(token)

	// Validate the token and get user info
	var userID, orgID string

	// In dev mode, check dev tokens
	if s.config.Auth.DevMode {
		for _, devToken := range s.config.Auth.DevTokens {
			if devToken.Token == token {
				userID = devToken.UserID
				orgID = devToken.OrgID
				break
			}
		}
	}

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	// Generate one-time login token
	oneTimeToken, err := s.tokenStore.CreateOneTimeLoginToken(userID, orgID, token)
	if err != nil {
		slog.Error("Failed to create one-time login token", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create login token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": oneTimeToken,
	})
}

// handleAutoLogin handles the browser auto-login flow
// GET /client-login/?token=xxx&next=/library/...
func (s *Server) handleAutoLogin(c *gin.Context) {
	oneTimeToken := c.Query("token")
	nextURL := c.Query("next")

	if oneTimeToken == "" {
		c.Redirect(http.StatusFound, "/login/?error=missing_token")
		return
	}

	// Validate and consume the one-time token
	authToken, err := s.tokenStore.ConsumeOneTimeLoginToken(oneTimeToken)
	if err != nil {
		slog.Warn("Invalid or expired one-time token", "error", err)
		c.Redirect(http.StatusFound, "/login/?error=invalid_token")
		return
	}

	// Set the auth token as a cookie for the browser session
	c.SetCookie(
		"seahub_auth",           // name
		authToken,               // value
		3600*24*7,              // maxAge (7 days)
		"/",                    // path
		"",                     // domain (empty = current domain)
		false,                  // secure (false for localhost)
		true,                   // httpOnly
	)

	// Redirect to the requested page or default to home
	if nextURL == "" {
		nextURL = "/"
	}
	c.Redirect(http.StatusFound, nextURL)
}

// handleAccountInfo returns account information for the authenticated user
// GET /api2/account/info/
func (s *Server) handleAccountInfo(c *gin.Context) {
	userID := c.GetString("user_id")
	orgID := c.GetString("org_id")

	// Fetch actual user data from database
	var email, name, role string
	var quotaBytes, usedBytes int64
	err := s.db.Session().Query(`
		SELECT email, name, role, quota_bytes, used_bytes
		FROM users WHERE org_id = ? AND user_id = ?
	`, orgID, userID).Scan(&email, &name, &role, &quotaBytes, &usedBytes)

	if err != nil {
		// Fallback to defaults if user not found (shouldn't happen for authenticated user)
		email = userID + "@sesamefs.local"
		name = userID
		role = "user"
		quotaBytes = -2 // unlimited
		usedBytes = 0
	}

	// Use email username as display name if name is empty
	if name == "" {
		if atIdx := strings.Index(email, "@"); atIdx > 0 {
			name = email[:atIdx]
		} else {
			name = email
		}
	}

	// Determine permissions based on role
	// Roles: superadmin, admin, user, readonly, guest
	isStaff := role == "admin" || role == "superadmin"
	canAddRepo := role == "superadmin" || role == "admin" || role == "user"
	canShareRepo := role == "superadmin" || role == "admin" || role == "user"
	canAddGroup := role == "superadmin" || role == "admin" || role == "user"
	canGenerateShareLink := role == "superadmin" || role == "admin" || role == "user"
	canGenerateUploadLink := role == "superadmin" || role == "admin" || role == "user"

	// Calculate space usage
	spaceUsage := "0%"
	if quotaBytes > 0 && usedBytes > 0 {
		percentage := float64(usedBytes) / float64(quotaBytes) * 100
		spaceUsage = fmt.Sprintf("%.1f%%", percentage)
	}

	// Return basic account info matching stock Seafile format
	// CRITICAL: Field names and types must match exactly for desktop client compatibility
	// Verified against stock Seafile (app.nihaoconsult.com)
	c.JSON(http.StatusOK, gin.H{
		"email":                        email,
		"name":                         name,
		"login_id":                     email,
		"contact_email":                email,
		"department":                   "",
		"institution":                  orgID,
		"is_staff":                     isStaff,
		"is_org_staff":                 0, // Integer 0 (not boolean false)
		"usage":                        usedBytes,
		"total":                        quotaBytes,
		"space_usage":                  spaceUsage,
		"avatar_url":                   "http://" + c.Request.Host + "/media/avatars/default.png",
		"enable_subscription":          false,
		"file_updates_email_interval":  0,
		"collaborate_email_interval":   0,
		// SesameFS extensions for permission control
		"role":                         role,
		"can_add_repo":                 canAddRepo,
		"can_share_repo":               canShareRepo,
		"can_add_group":                canAddGroup,
		"can_generate_share_link":      canGenerateShareLink,
		"can_generate_upload_link":     canGenerateUploadLink,
	})
}

// handleSearchUser searches for users within the same organization
// GET /api2/search-user/?q=<query>
// Returns users matching the query string (by email or name)
func (s *Server) handleSearchUser(c *gin.Context) {
	query := c.Query("q")
	orgID := c.GetString("org_id")

	if query == "" {
		c.JSON(http.StatusOK, gin.H{"users": []gin.H{}})
		return
	}

	// Query all users in the organization
	iter := s.db.Session().Query(`
		SELECT user_id, email, name, role FROM users WHERE org_id = ?
	`, orgID).Iter()

	var users []gin.H
	var userID, email, name, role string
	queryLower := strings.ToLower(query)

	for iter.Scan(&userID, &email, &name, &role) {
		// Skip deactivated users
		if role == "deactivated" {
			continue
		}
		// Match against email or name (case-insensitive)
		if strings.Contains(strings.ToLower(email), queryLower) ||
			strings.Contains(strings.ToLower(name), queryLower) {
			displayName := name
			if displayName == "" {
				if atIdx := strings.Index(email, "@"); atIdx > 0 {
					displayName = email[:atIdx]
				} else {
					displayName = email
				}
			}
			users = append(users, gin.H{
				"email":         email,
				"name":          displayName,
				"avatar_url":    "http://" + c.Request.Host + "/media/avatars/default.png",
				"contact_email": email,
				"login_id":      email,
			})
		}
	}
	if err := iter.Close(); err != nil {
		slog.Error("search-user query failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "search failed"})
		return
	}

	if users == nil {
		users = []gin.H{}
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}

// handleUserAvatar returns an avatar for a user
// GET /api2/avatars/user/:email/resized/:size/
func (s *Server) handleUserAvatar(c *gin.Context) {
	// Return a default avatar URL
	// In production, this would return actual user avatars
	c.JSON(http.StatusOK, gin.H{
		"url":     "",
		"is_default": true,
		"mtime":   0,
	})
}

// handleRepoTokens returns sync tokens for the specified repositories
// GET /api2/repo-tokens?repos=uuid1,uuid2,...
func (s *Server) handleRepoTokens(c *gin.Context) {
	userID := c.GetString("user_id")
	orgID := c.GetString("org_id")
	reposParam := c.Query("repos")

	if reposParam == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repos parameter required"})
		return
	}

	// Parse repo IDs (comma-separated)
	repoIDs := strings.Split(reposParam, ",")

	// Generate tokens for each repo
	tokens := make(map[string]string)
	for _, repoID := range repoIDs {
		repoID = strings.TrimSpace(repoID)
		if repoID == "" {
			continue
		}

		// Verify the repo exists and user has access
		var libID string
		err := s.db.Session().Query(`
			SELECT library_id FROM libraries
			WHERE org_id = ? AND library_id = ?
		`, orgID, repoID).Scan(&libID)
		if err != nil {
			// Skip repos that don't exist or user doesn't have access to
			continue
		}

		// Generate a sync token for this repo
		token, err := s.tokenStore.CreateDownloadToken(orgID, repoID, "/", userID)
		if err != nil {
			continue
		}
		tokens[repoID] = token
	}

	c.JSON(http.StatusOK, tokens)
}

// trailingSlashHandler wraps a handler and strips trailing slashes from requests
type trailingSlashHandler struct {
	handler http.Handler
}

func (h *trailingSlashHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Strip trailing slash (except for root path)
	// This ensures /api2/repos/ is handled the same as /api2/repos
	if len(r.URL.Path) > 1 && r.URL.Path[len(r.URL.Path)-1] == '/' {
		r.URL.Path = r.URL.Path[:len(r.URL.Path)-1]
	}
	h.handler.ServeHTTP(w, r)
}

// Run starts the HTTP server
func (s *Server) Run() error {
	// Wrap router to strip trailing slashes before gin routing
	// This prevents gin's 307 redirect which breaks POST requests from Seafile clients
	handler := &trailingSlashHandler{handler: s.router}

	s.server = &http.Server{
		Addr:         s.config.Server.Port,
		Handler:      handler,
		ReadTimeout:  s.config.Server.ReadTimeout,
		WriteTimeout: s.config.Server.WriteTimeout,
	}

	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	// Stop GC service first
	if s.gcService != nil {
		s.gcService.Stop()
	}

	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// handleEmptyActivities returns empty activities list (stub)
// GET /api/v2.1/activities/
func (s *Server) handleEmptyActivities(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"events": []interface{}{},
	})
}

// handleEmptyNotifications returns empty notifications list
// GET /api/v2.1/notifications/
func (s *Server) handleEmptyNotifications(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"notification_list": []interface{}{},
		"unseen_count":      0,
	})
}

// handleEmptySharedRepos returns empty shared repos list (stub)
// GET /api/v2.1/shared-repos/
func (s *Server) handleEmptySharedRepos(c *gin.Context) {
	c.JSON(http.StatusOK, []interface{}{})
}

// handleEmptySharedFolders returns empty shared folders list (stub)
// GET /api/v2.1/shared-folders/
func (s *Server) handleEmptySharedFolders(c *gin.Context) {
	c.JSON(http.StatusOK, []interface{}{})
}

// handleEmptyDevices returns empty devices list (stub)
// GET /api2/devices/
func (s *Server) handleEmptyDevices(c *gin.Context) {
	c.JSON(http.StatusOK, []interface{}{})
}

// handleEmptyWikis returns empty published libraries/wikis list (stub)
// GET /api/v2.1/wikis/
func (s *Server) handleEmptyWikis(c *gin.Context) {
	c.JSON(http.StatusOK, []interface{}{})
}

// handleEmptyRepoTags returns empty repo tags list
// GET /api/v2.1/repos/:repo_id/repo-tags/
func (s *Server) handleEmptyRepoTags(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"repo_tags": []interface{}{},
	})
}

// handleEmptyFolderShareInfo returns empty folder share info
// GET /api/v2.1/repo-folder-share-info/
func (s *Server) handleEmptyFolderShareInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"share_info_list": []interface{}{},
	})
}

// handleEmptyGroups returns empty groups list
// GET /api/v2.1/groups/
func (s *Server) handleEmptyGroups(c *gin.Context) {
	c.JSON(http.StatusOK, []interface{}{})
}

// handleAutoDeleteSettings and handleEmptyRepoAPITokens removed -
// replaced by LibrarySettingsHandler in v2/library_settings.go

// handleEmptyRepoShareLinks removed - replaced by ShareLinkHandler.ListRepoShareLinks

// handleHistoryLimit removed - replaced by LibrarySettingsHandler in v2/library_settings.go

// handleLogout clears the user's session and redirects to home
// GET /accounts/logout/
func (s *Server) handleLogout(c *gin.Context) {
	// In a real implementation, we would invalidate the token in the database
	// For now, we just return an HTML page that clears localStorage and redirects
	html := `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Logging out...</title>
    <script>
        // Clear the auth token from localStorage
        localStorage.removeItem('sesamefs_auth_token');
        localStorage.removeItem('seahub_token');
        // Redirect to home page
        window.location.href = '/';
    </script>
</head>
<body>
    <p>Logging out...</p>
</body>
</html>`
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, html)
}

// handleCreateRepoTag returns a stub response for tag creation
// POST /api/v2.1/repos/:repo_id/repo-tags/
func (s *Server) handleCreateRepoTag(c *gin.Context) {
	// Return stub success - full implementation would create tag in database
	c.JSON(http.StatusOK, gin.H{
		"repo_tag": gin.H{
			"id":    1,
			"name":  c.PostForm("name"),
			"color": c.PostForm("color"),
		},
	})
}

// handleGCStatus returns the current GC status
// GET /api/v2.1/admin/gc/status
func (s *Server) handleGCStatus(c *gin.Context) {
	// Check staff permission
	role := c.GetString("role")
	if role != "admin" && role != "superadmin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin access required"})
		return
	}

	if s.gcService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "GC service not available"})
		return
	}

	c.JSON(http.StatusOK, s.gcService.Status())
}

// handleGCRun triggers a manual GC run
// POST /api/v2.1/admin/gc/run
func (s *Server) handleGCRun(c *gin.Context) {
	// Check staff permission
	role := c.GetString("role")
	if role != "admin" && role != "superadmin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin access required"})
		return
	}

	if s.gcService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "GC service not available"})
		return
	}

	var req struct {
		Type   string `json:"type"`    // "worker" or "scanner"
		DryRun *bool  `json:"dry_run"` // optional override
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		// Default to worker
		req.Type = "worker"
	}

	if req.DryRun != nil {
		s.gcService.SetDryRun(*req.DryRun)
	}

	switch req.Type {
	case "scanner":
		s.gcService.TriggerScanner()
		c.JSON(http.StatusOK, gin.H{"started": true, "message": "GC scanner triggered"})
	default:
		s.gcService.TriggerWorker()
		c.JSON(http.StatusOK, gin.H{"started": true, "message": "GC worker triggered"})
	}
}
