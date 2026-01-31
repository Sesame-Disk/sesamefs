package db

import (
	"context"
	"fmt"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/config"
	"github.com/apache/cassandra-gocql-driver/v2"
)

// DB wraps the Cassandra session
type DB struct {
	session *gocql.Session
	config  config.DatabaseConfig
}

// New creates a new database connection.
// It first connects without a keyspace to ensure the keyspace can be created
// by Migrate(), then reconnects with the keyspace set.
func New(cfg config.DatabaseConfig) (*DB, error) {
	// First, connect without keyspace to bootstrap (create keyspace if needed)
	bootstrapCluster := newCluster(cfg)
	// Don't set keyspace — it may not exist yet
	bootstrapSession, err := bootstrapCluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Cassandra: %w", err)
	}

	// Create keyspace if it doesn't exist (idempotent)
	if err := bootstrapSession.Query(migrationCreateKeyspace).Exec(); err != nil {
		bootstrapSession.Close()
		return nil, fmt.Errorf("failed to create keyspace: %w", err)
	}
	bootstrapSession.Close()

	// Now connect with keyspace
	cluster := newCluster(cfg)
	cluster.Keyspace = cfg.Keyspace
	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Cassandra keyspace %s: %w", cfg.Keyspace, err)
	}

	return &DB{
		session: session,
		config:  cfg,
	}, nil
}

// newCluster creates a gocql ClusterConfig from our config (without keyspace).
func newCluster(cfg config.DatabaseConfig) *gocql.ClusterConfig {
	cluster := gocql.NewCluster(cfg.Hosts...)
	cluster.Consistency = parseConsistency(cfg.Consistency)
	cluster.Timeout = 10 * time.Second
	cluster.ConnectTimeout = 10 * time.Second

	// Set local DC for multi-DC deployments
	if cfg.LocalDC != "" {
		cluster.PoolConfig.HostSelectionPolicy = gocql.DCAwareRoundRobinPolicy(cfg.LocalDC)
	}

	// Authentication
	if cfg.Username != "" && cfg.Password != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: cfg.Username,
			Password: cfg.Password,
		}
	}

	return cluster
}

// Close closes the database connection
func (db *DB) Close() {
	if db.session != nil {
		db.session.Close()
	}
}

// Session returns the underlying gocql session
func (db *DB) Session() *gocql.Session {
	return db.session
}

// Ping verifies database connectivity by executing a lightweight query.
func (db *DB) Ping(ctx context.Context) error {
	return db.session.Query(`SELECT now() FROM system.local`).WithContext(ctx).Exec()
}

// Migrate runs database migrations
func (db *DB) Migrate() error {
	migrations := []string{
		migrationCreateKeyspace,
		migrationCreateOrganizations,
		migrationCreateUsers,
		migrationCreateUsersByEmail,
		migrationCreateUsersByOIDC,
		migrationCreateLibraries,
		migrationCreateCommits,
		migrationCreateFSObjects,
		migrationCreateBlocks,
		migrationCreateBlockIDMappings,
		migrationCreateShareLinks,
		migrationCreateShares,
		migrationCreateRestoreJobs,
		migrationCreateAccessTokens,
		migrationCreateHostnameMappings,
		migrationCreateOnlyOfficeDocKeys,
		migrationCreateStarredFiles,
		migrationCreateLockedFiles,
		migrationCreateRepoTags,
		migrationCreateFileTags,
		migrationCreateRepoTagCounters,
		migrationCreateFileTagCounters,
		migrationCreateFileTagsById,
		migrationCreateLibrariesByID,
		migrationCreateShareLinksByCreator,
		migrationCreateRepoTagFileCounts,
		migrationCreateGroups,
		migrationCreateGroupMembers,
		migrationCreateGroupsByMember,
		migrationCreateSessions,
		migrationCreateRepoAPITokens,
		migrationCreateRepoAPITokensByToken,
		migrationCreateMonitoredRepos,
		migrationCreateGCQueue,
		migrationCreateGCStats,
	}

	for _, migration := range migrations {
		if err := db.session.Query(migration).Exec(); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	// ALTER TABLE migrations (ignore errors if columns already exist)
	// Also includes index creation (ignore errors if indexes already exist)
	alterMigrations := []string{
		migrationAddEncryptionColumns,
		migrationAddEncryptionColumns2,
		migrationAddEncryptionColumns3,
		migrationCreateSearchIndex,
		migrationCreateLibrarySearchIndex,
		migrationAddAutoDeleteDays,
		migrationAddGroupParentID,
		migrationAddGroupIsDepartment,
	}
	for _, migration := range alterMigrations {
		// Ignore errors for ALTER TABLE - columns may already exist
		db.session.Query(migration).Exec()
	}

	return nil
}

// parseConsistency converts string to gocql.Consistency
func parseConsistency(s string) gocql.Consistency {
	switch s {
	case "ONE":
		return gocql.One
	case "QUORUM":
		return gocql.Quorum
	case "LOCAL_QUORUM":
		return gocql.LocalQuorum
	case "EACH_QUORUM":
		return gocql.EachQuorum
	case "ALL":
		return gocql.All
	default:
		return gocql.LocalQuorum
	}
}

// Migration statements
const migrationCreateKeyspace = `
CREATE KEYSPACE IF NOT EXISTS sesamefs WITH replication = {
	'class': 'SimpleStrategy',
	'replication_factor': 1
}`

const migrationCreateOrganizations = `
CREATE TABLE IF NOT EXISTS organizations (
	org_id UUID PRIMARY KEY,
	name TEXT,
	settings MAP<TEXT, TEXT>,
	storage_quota BIGINT,
	storage_used BIGINT,
	chunking_polynomial BIGINT,
	storage_config MAP<TEXT, TEXT>,
	created_at TIMESTAMP
)`

const migrationCreateUsers = `
CREATE TABLE IF NOT EXISTS users (
	org_id UUID,
	user_id UUID,
	email TEXT,
	name TEXT,
	role TEXT,
	oidc_sub TEXT,
	quota_bytes BIGINT,
	used_bytes BIGINT,
	created_at TIMESTAMP,
	PRIMARY KEY ((org_id), user_id)
)`

const migrationCreateUsersByEmail = `
CREATE TABLE IF NOT EXISTS users_by_email (
	email TEXT PRIMARY KEY,
	user_id UUID,
	org_id UUID
)`

const migrationCreateUsersByOIDC = `
CREATE TABLE IF NOT EXISTS users_by_oidc (
	oidc_issuer TEXT,
	oidc_sub TEXT,
	user_id UUID,
	org_id UUID,
	PRIMARY KEY ((oidc_issuer), oidc_sub)
)`

const migrationCreateLibraries = `
CREATE TABLE IF NOT EXISTS libraries (
	org_id UUID,
	library_id UUID,
	owner_id UUID,
	name TEXT,
	description TEXT,
	encrypted BOOLEAN,
	enc_version INT,
	magic TEXT,
	random_key TEXT,
	salt TEXT,
	magic_strong TEXT,
	random_key_strong TEXT,
	root_commit_id TEXT,
	head_commit_id TEXT,
	storage_class TEXT,
	size_bytes BIGINT,
	file_count BIGINT,
	version_ttl_days INT,
	created_at TIMESTAMP,
	updated_at TIMESTAMP,
	PRIMARY KEY ((org_id), library_id)
)`

// Migration to add encryption columns to existing libraries table
const migrationAddEncryptionColumns = `
ALTER TABLE libraries ADD salt TEXT`

const migrationAddEncryptionColumns2 = `
ALTER TABLE libraries ADD magic_strong TEXT`

const migrationAddEncryptionColumns3 = `
ALTER TABLE libraries ADD random_key_strong TEXT`

const migrationCreateCommits = `
CREATE TABLE IF NOT EXISTS commits (
	library_id UUID,
	commit_id TEXT,
	parent_id TEXT,
	root_fs_id TEXT,
	creator_id UUID,
	description TEXT,
	created_at TIMESTAMP,
	PRIMARY KEY ((library_id), commit_id)
)`

const migrationCreateFSObjects = `
CREATE TABLE IF NOT EXISTS fs_objects (
	library_id UUID,
	fs_id TEXT,
	obj_type TEXT,
	obj_name TEXT,
	dir_entries TEXT,
	block_ids LIST<TEXT>,
	size_bytes BIGINT,
	mtime BIGINT,
	PRIMARY KEY ((library_id), fs_id)
)`

const migrationCreateBlocks = `
CREATE TABLE IF NOT EXISTS blocks (
	org_id UUID,
	block_id TEXT,
	size_bytes INT,
	storage_class TEXT,
	storage_key TEXT,
	ref_count INT,
	created_at TIMESTAMP,
	last_accessed TIMESTAMP,
	PRIMARY KEY ((org_id), block_id)
)`

// Block ID mappings for SHA-1 to SHA-256 translation
// Allows Seafile clients (SHA-1) to work with internal SHA-256 storage
const migrationCreateBlockIDMappings = `
CREATE TABLE IF NOT EXISTS block_id_mappings (
	org_id UUID,
	external_id TEXT,
	internal_id TEXT,
	created_at TIMESTAMP,
	PRIMARY KEY ((org_id), external_id)
)`

const migrationCreateShareLinks = `
CREATE TABLE IF NOT EXISTS share_links (
	share_token TEXT PRIMARY KEY,
	org_id UUID,
	library_id UUID,
	file_path TEXT,
	created_by UUID,
	permission TEXT,
	password_hash TEXT,
	expires_at TIMESTAMP,
	download_count INT,
	max_downloads INT,
	created_at TIMESTAMP
)`

const migrationCreateShares = `
CREATE TABLE IF NOT EXISTS shares (
	library_id UUID,
	share_id UUID,
	shared_by UUID,
	shared_to UUID,
	shared_to_type TEXT,
	permission TEXT,
	created_at TIMESTAMP,
	expires_at TIMESTAMP,
	PRIMARY KEY ((library_id), share_id)
)`

const migrationCreateRestoreJobs = `
CREATE TABLE IF NOT EXISTS restore_jobs (
	org_id UUID,
	job_id UUID,
	library_id UUID,
	block_ids LIST<TEXT>,
	glacier_job_id TEXT,
	status TEXT,
	requested_at TIMESTAMP,
	completed_at TIMESTAMP,
	expires_at TIMESTAMP,
	PRIMARY KEY ((org_id), library_id, job_id)
)`

// Access tokens for stateless file operations
// Uses Cassandra TTL for automatic expiration
// Note: "token" is quoted because it's a reserved keyword in CQL
const migrationCreateAccessTokens = `
CREATE TABLE IF NOT EXISTS access_tokens (
	"token" TEXT PRIMARY KEY,
	token_type TEXT,
	org_id UUID,
	repo_id UUID,
	file_path TEXT,
	user_id UUID,
	created_at TIMESTAMP
)`

// Hostname mappings for multi-tenant routing
// Maps hostnames/domains to organizations
const migrationCreateHostnameMappings = `
CREATE TABLE IF NOT EXISTS hostname_mappings (
	hostname TEXT PRIMARY KEY,
	org_id UUID,
	settings MAP<TEXT, TEXT>,
	created_at TIMESTAMP,
	updated_at TIMESTAMP
)`

// OnlyOffice document key mappings for callback lookups
// Stores temporary mappings between doc_key and file info for OnlyOffice callbacks
// Uses TTL for automatic cleanup (24 hours)
const migrationCreateOnlyOfficeDocKeys = `
CREATE TABLE IF NOT EXISTS onlyoffice_doc_keys (
	doc_key TEXT PRIMARY KEY,
	user_id TEXT,
	repo_id TEXT,
	file_path TEXT,
	created_at TIMESTAMP
)`

// Starred files for user favorites
// Partition by user_id for efficient querying of user's starred items
const migrationCreateStarredFiles = `
CREATE TABLE IF NOT EXISTS starred_files (
	user_id UUID,
	repo_id UUID,
	path TEXT,
	starred_at TIMESTAMP,
	PRIMARY KEY ((user_id), repo_id, path)
)`

// Locked files for file locking feature
// Partition by repo_id for efficient querying when listing directory
const migrationCreateLockedFiles = `
CREATE TABLE IF NOT EXISTS locked_files (
	repo_id UUID,
	path TEXT,
	locked_by UUID,
	locked_at TIMESTAMP,
	PRIMARY KEY ((repo_id), path)
)`

// Repository-level tags
// Partition by repo_id for efficient listing of all tags in a repo
const migrationCreateRepoTags = `
CREATE TABLE IF NOT EXISTS repo_tags (
	repo_id UUID,
	tag_id INT,
	name TEXT,
	color TEXT,
	created_at TIMESTAMP,
	PRIMARY KEY ((repo_id), tag_id)
)`

// File tags - associates files with repo tags
// Partition by repo_id for efficient listing
// Includes file_tag_id for efficient lookups (eliminates ALLOW FILTERING)
const migrationCreateFileTags = `
CREATE TABLE IF NOT EXISTS file_tags (
	repo_id UUID,
	file_path TEXT,
	tag_id INT,
	file_tag_id INT,
	created_at TIMESTAMP,
	PRIMARY KEY ((repo_id), file_path, tag_id)
)`

// Counter for generating tag IDs per repo
const migrationCreateRepoTagCounters = `
CREATE TABLE IF NOT EXISTS repo_tag_counters (
	repo_id UUID PRIMARY KEY,
	next_tag_id INT
)`

// Counter for generating unique file_tag_id values per repo
const migrationCreateFileTagCounters = `
CREATE TABLE IF NOT EXISTS file_tag_counters (
	repo_id UUID PRIMARY KEY,
	next_file_tag_id INT
)`

// Lookup table to find file tags by their unique ID
// This enables DELETE /repos/:repo_id/file-tags/:file_tag_id/
const migrationCreateFileTagsById = `
CREATE TABLE IF NOT EXISTS file_tags_by_id (
	repo_id UUID,
	file_tag_id INT,
	file_path TEXT,
	tag_id INT,
	created_at TIMESTAMP,
	PRIMARY KEY ((repo_id), file_tag_id)
)`

// Lookup table for libraries by library_id
// Eliminates ALLOW FILTERING when querying by library_id alone
// Dual-write pattern: update both libraries and libraries_by_id
const migrationCreateLibrariesByID = `
CREATE TABLE IF NOT EXISTS libraries_by_id (
	library_id UUID PRIMARY KEY,
	org_id UUID,
	owner_id UUID,
	head_commit_id TEXT,
	encrypted BOOLEAN,
	enc_version INT,
	magic TEXT,
	random_key TEXT,
	salt TEXT,
	magic_strong TEXT,
	random_key_strong TEXT
)`

// Lookup table for share links by creator
// Eliminates ALLOW FILTERING when listing user's share links
// Dual-write pattern: update both share_links and share_links_by_creator
const migrationCreateShareLinksByCreator = `
CREATE TABLE IF NOT EXISTS share_links_by_creator (
	org_id UUID,
	created_by UUID,
	share_token TEXT,
	library_id UUID,
	file_path TEXT,
	permission TEXT,
	expires_at TIMESTAMP,
	download_count INT,
	max_downloads INT,
	created_at TIMESTAMP,
	PRIMARY KEY ((org_id, created_by), share_token)
)`

// Counter for number of files tagged with each tag
// Eliminates ALLOW FILTERING when counting files per tag
// Update pattern: increment/decrement when tags added/removed
const migrationCreateRepoTagFileCounts = `
CREATE TABLE IF NOT EXISTS repo_tag_file_counts (
	repo_id UUID,
	tag_id INT,
	file_count COUNTER,
	PRIMARY KEY ((repo_id), tag_id)
)`

// Groups table for team collaboration
// Stores group information (name, creator, settings)
// parent_group_id: NULL for top-level groups, set for departments (hierarchical)
// is_department: true for departments (admin-created), false for regular groups (user-created)
const migrationCreateGroups = `
CREATE TABLE IF NOT EXISTS groups (
	org_id UUID,
	group_id UUID,
	name TEXT,
	creator_id UUID,
	parent_group_id UUID,
	is_department BOOLEAN,
	created_at TIMESTAMP,
	updated_at TIMESTAMP,
	PRIMARY KEY ((org_id), group_id)
)`

// Group members table
// Partition by group_id for efficient member listing
const migrationCreateGroupMembers = `
CREATE TABLE IF NOT EXISTS group_members (
	group_id UUID,
	user_id UUID,
	role TEXT,
	added_at TIMESTAMP,
	PRIMARY KEY ((group_id), user_id)
)`

// Lookup table for finding groups by member
// Partition by org_id + user_id for efficient "my groups" queries
// Dual-write pattern: update both group_members and groups_by_member
const migrationCreateGroupsByMember = `
CREATE TABLE IF NOT EXISTS groups_by_member (
	org_id UUID,
	user_id UUID,
	group_id UUID,
	group_name TEXT,
	role TEXT,
	added_at TIMESTAMP,
	PRIMARY KEY ((org_id, user_id), group_id)
)`

// SASI index for searching files and directories by name
// Supports LIKE queries with case-insensitive search
const migrationCreateSearchIndex = `
CREATE CUSTOM INDEX IF NOT EXISTS fs_objects_name_idx ON fs_objects (obj_name)
USING 'org.apache.cassandra.index.sasi.SASIIndex'
WITH OPTIONS = {
	'mode': 'CONTAINS',
	'analyzer_class': 'org.apache.cassandra.index.sasi.analyzer.StandardAnalyzer',
	'case_sensitive': 'false'
}`

// SASI index for searching libraries by name
const migrationCreateLibrarySearchIndex = `
CREATE CUSTOM INDEX IF NOT EXISTS libraries_name_idx ON libraries (name)
USING 'org.apache.cassandra.index.sasi.SASIIndex'
WITH OPTIONS = {
	'mode': 'CONTAINS',
	'analyzer_class': 'org.apache.cassandra.index.sasi.analyzer.StandardAnalyzer',
	'case_sensitive': 'false'
}`

// Sessions table for OIDC authentication
// token_hash is SHA-256 hash of the session token (we don't store raw tokens)
// TTL is set on insert to auto-expire sessions
const migrationCreateSessions = `
CREATE TABLE IF NOT EXISTS sessions (
	token_hash TEXT PRIMARY KEY,
	user_id UUID,
	org_id UUID,
	email TEXT,
	role TEXT,
	created_at TIMESTAMP,
	expires_at TIMESTAMP
)`

// Repo API tokens for programmatic access to individual libraries
// Partition by repo_id for efficient listing of tokens per repo
const migrationCreateRepoAPITokens = `
CREATE TABLE IF NOT EXISTS repo_api_tokens (
	repo_id UUID,
	app_name TEXT,
	api_token TEXT,
	permission TEXT,
	generated_by UUID,
	created_at TIMESTAMP,
	PRIMARY KEY ((repo_id), app_name)
)`

// Reverse-lookup table for repo API tokens by token value
// Allows O(1) authentication: given a token string, find which repo it grants access to
const migrationCreateRepoAPITokensByToken = `
CREATE TABLE IF NOT EXISTS repo_api_tokens_by_token (
	api_token TEXT PRIMARY KEY,
	repo_id UUID,
	app_name TEXT,
	permission TEXT,
	generated_by UUID
)`

// Monitored repos for watch/unwatch feature
// Partition by user_id for efficient querying of user's monitored libraries
const migrationCreateMonitoredRepos = `
CREATE TABLE IF NOT EXISTS monitored_repos (
	user_id UUID,
	repo_id UUID,
	monitored_at TIMESTAMP,
	PRIMARY KEY ((user_id), repo_id)
)`

// Add auto_delete_days column to libraries table
const migrationAddAutoDeleteDays = `
ALTER TABLE libraries ADD auto_delete_days INT`

// Add parent_group_id column to groups table for department hierarchy
const migrationAddGroupParentID = `
ALTER TABLE groups ADD parent_group_id UUID`

// Add is_department column to groups table
const migrationAddGroupIsDepartment = `
ALTER TABLE groups ADD is_department BOOLEAN`

// GC queue for items pending deletion
// Partitioned by org_id for natural sharding across workers
// 7-day TTL auto-cleans stale items
const migrationCreateGCQueue = `
CREATE TABLE IF NOT EXISTS gc_queue (
	org_id UUID,
	queued_at TIMESTAMP,
	item_type TEXT,
	item_id TEXT,
	library_id UUID,
	storage_class TEXT,
	retry_count INT,
	PRIMARY KEY ((org_id), queued_at, item_type, item_id)
) WITH default_time_to_live = 604800
  AND CLUSTERING ORDER BY (queued_at ASC)`

// GC run statistics for admin API
const migrationCreateGCStats = `
CREATE TABLE IF NOT EXISTS gc_stats (
	stat_key TEXT PRIMARY KEY,
	stat_value TEXT,
	updated_at TIMESTAMP
)`
