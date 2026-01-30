package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration for SesameFS
type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Database      DatabaseConfig      `yaml:"database"`
	Storage       StorageConfig       `yaml:"storage"`
	Auth          AuthConfig          `yaml:"auth"`
	Chunking      ChunkingConfig      `yaml:"chunking"`
	Versioning    VersioningConfig    `yaml:"versioning"`
	GC            GCConfig            `yaml:"gc"`
	SeafHTTP      SeafHTTPConfig      `yaml:"seafhttp"`
	CORS          CORSConfig          `yaml:"cors"`
	OnlyOffice    OnlyOfficeConfig    `yaml:"onlyoffice"`
	Elasticsearch ElasticsearchConfig `yaml:"elasticsearch"`
}

// OnlyOfficeConfig holds OnlyOffice Document Server integration settings
// See: https://manual.seafile.com/deploy/only_office/
type OnlyOfficeConfig struct {
	Enabled           bool     `yaml:"enabled"`
	APIJSURL          string   `yaml:"api_js_url"`          // URL to api.js loaded by browser (e.g., http://localhost:8088/web-apps/apps/api/documents/api.js)
	JWTSecret         string   `yaml:"jwt_secret"`          // JWT secret for signing tokens
	VerifyCertificate bool     `yaml:"verify_certificate"`  // Whether to verify OnlyOffice SSL cert
	ForceSave         bool     `yaml:"force_save"`          // Enable force save on user action
	ViewExtensions    []string `yaml:"view_extensions"`     // Extensions that can be viewed (doc, docx, ppt, etc.)
	EditExtensions    []string `yaml:"edit_extensions"`     // Extensions that can be edited (docx, pptx, xlsx)
	ServerURL         string   `yaml:"server_url"`          // URL for OnlyOffice to reach SesameFS (e.g., http://sesamefs:8080)
	InternalURL       string   `yaml:"internal_url"`        // URL for SesameFS to reach OnlyOffice internally (e.g., http://onlyoffice:80)
}

// ElasticsearchConfig holds Elasticsearch search backend settings
type ElasticsearchConfig struct {
	Enabled bool     `yaml:"enabled"`
	URLs    []string `yaml:"urls"`  // Elasticsearch cluster URLs
	Index   string   `yaml:"index"` // Index name (default: sesamefs-files)
}

// CORSConfig holds CORS settings for frontend access
type CORSConfig struct {
	AllowedOrigins []string `yaml:"allowed_origins"`
}

// SeafHTTPConfig holds Seafile-compatible file transfer settings
type SeafHTTPConfig struct {
	TokenTTL time.Duration `yaml:"token_ttl"` // How long upload/download tokens are valid
}

// ServerConfig holds HTTP server settings
type ServerConfig struct {
	Port         string        `yaml:"port"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	MaxUploadMB  int64         `yaml:"max_upload_mb"`
}

// DatabaseConfig holds Cassandra connection settings
type DatabaseConfig struct {
	Hosts       []string `yaml:"hosts"`
	Keyspace    string   `yaml:"keyspace"`
	Consistency string   `yaml:"consistency"`
	LocalDC     string   `yaml:"local_dc"`
	Username    string   `yaml:"username"`
	Password    string   `yaml:"password"`
}

// StorageConfig holds storage backend settings
type StorageConfig struct {
	DefaultClass    string                        `yaml:"default_class"`
	Classes         map[string]StorageClassConfig `yaml:"classes"`
	EndpointRegions map[string]string             `yaml:"endpoint_regions"` // hostname → region
	RegionClasses   map[string]RegionClassConfig  `yaml:"region_classes"`   // region → {hot, cold}

	// Legacy support (deprecated, use Classes instead)
	Backends map[string]BackendConfig `yaml:"backends"`
}

// StorageClassConfig holds configuration for a storage class (e.g., hot-s3-usa)
type StorageClassConfig struct {
	Type          string `yaml:"type"`           // s3, glacier, disk
	Tier          string `yaml:"tier"`           // hot, cold
	Endpoint      string `yaml:"endpoint"`       // Primary endpoint
	Bucket        string `yaml:"bucket"`         // S3 bucket name
	Region        string `yaml:"region"`         // AWS region
	AccessKey     string `yaml:"access_key"`     // AWS access key (optional, can use env)
	SecretKey     string `yaml:"secret_key"`     // AWS secret key (optional, can use env)
	UsePathStyle  bool   `yaml:"use_path_style"` // For MinIO compatibility
	FailoverClass string `yaml:"failover_class"` // Fallback class if this one is down
}

// RegionClassConfig maps a region to its hot and cold storage classes
type RegionClassConfig struct {
	Hot  string `yaml:"hot"`
	Cold string `yaml:"cold"`
}

// BackendConfig holds configuration for a storage backend (legacy, deprecated)
type BackendConfig struct {
	Type         string `yaml:"type"`          // s3, glacier, filesystem
	Endpoint     string `yaml:"endpoint"`      // S3 endpoint
	Bucket       string `yaml:"bucket"`        // S3 bucket name
	Region       string `yaml:"region"`        // AWS region
	StorageClass string `yaml:"storage_class"` // S3 storage class
	Vault        string `yaml:"vault"`         // Glacier vault name
	Path         string `yaml:"path"`          // Filesystem path
}

// AuthConfig holds authentication settings
type AuthConfig struct {
	DevMode        bool            `yaml:"dev_mode"`
	AllowAnonymous bool            `yaml:"allow_anonymous"` // Allow unauthenticated access (uses first dev token) - FOR TESTING ONLY
	DevTokens      []DevTokenEntry `yaml:"dev_tokens"`
	OIDC           OIDCConfig      `yaml:"oidc"`
}

// DevTokenEntry holds a development token for testing
type DevTokenEntry struct {
	Token  string `yaml:"token"`
	UserID string `yaml:"user_id"`
	OrgID  string `yaml:"org_id"`
	Email  string `yaml:"email"` // Optional friendly email like "admin@sesamefs.local"
	Role   string `yaml:"role"`  // Optional role (superadmin, admin, user, readonly, guest)
}

// OIDCConfig holds OIDC provider settings
type OIDCConfig struct {
	// Enabled toggles OIDC authentication on/off
	Enabled bool `yaml:"enabled"`

	// Provider settings
	Issuer       string `yaml:"issuer"`        // OIDC provider URL (e.g., https://t-accounts.sesamedisk.com/)
	ClientID     string `yaml:"client_id"`     // OAuth client ID
	ClientSecret string `yaml:"client_secret"` // OAuth client secret

	// Redirect URIs - supports multiple for different environments
	// All URIs must also be registered with the OIDC provider
	RedirectURIs []string `yaml:"redirect_uris"`

	// Scopes to request from the OIDC provider
	Scopes []string `yaml:"scopes"`

	// Claim mappings for organization and role extraction
	OrgClaim   string `yaml:"org_claim"`   // Custom claim for organization/tenant ID (e.g., "tenant_id")
	RolesClaim string `yaml:"roles_claim"` // Custom claim for user roles (e.g., "roles")

	// User provisioning
	AutoProvision    bool   `yaml:"auto_provision"`     // Auto-create users on first login
	DefaultRole      string `yaml:"default_role"`       // Default role for new users (user, readonly, guest)
	DefaultOrgID     string `yaml:"default_org_id"`     // Default org for users without org claim
	DefaultOrgName   string `yaml:"default_org_name"`   // Default org name for new orgs
	AllowedOrgClaims string `yaml:"allowed_org_claims"` // Comma-separated list of allowed org claim values (empty = allow all)

	// Platform org settings
	PlatformOrgID         string `yaml:"platform_org_id"`          // UUID for the platform org (default: all zeros)
	PlatformOrgClaimValue string `yaml:"platform_org_claim_value"` // OIDC claim value that maps to the platform org

	// Session settings
	SessionTTL        time.Duration `yaml:"session_ttl"`         // How long sessions last (default: 24h)
	RefreshTokenTTL   time.Duration `yaml:"refresh_token_ttl"`   // How long refresh tokens last (default: 7d)
	JWTSigningKey     string        `yaml:"jwt_signing_key"`     // Secret key for signing JWT session tokens
	AllowOfflineToken bool          `yaml:"allow_offline_token"` // Allow refresh tokens for offline access

	// Security settings
	RequirePKCE       bool `yaml:"require_pkce"`        // Require PKCE for authorization flow
	ValidateAudience  bool `yaml:"validate_audience"`   // Validate token audience claim
	AllowedClockSkew  time.Duration `yaml:"allowed_clock_skew"` // Allowed clock skew for token validation
}

// ChunkingConfig holds FastCDC chunking settings
type ChunkingConfig struct {
	Algorithm     string         `yaml:"algorithm"`      // fastcdc
	HashAlgorithm string         `yaml:"hash_algorithm"` // sha256
	Adaptive      AdaptiveConfig `yaml:"adaptive"`       // Adaptive chunk sizing
	Probe         ProbeConfig    `yaml:"probe"`          // Speed probe settings
	Retry         RetryConfig    `yaml:"retry"`          // Retry settings
}

// AdaptiveConfig holds adaptive chunk sizing settings
type AdaptiveConfig struct {
	Enabled       bool  `yaml:"enabled"`        // Enable adaptive chunking
	AbsoluteMin   int64 `yaml:"absolute_min"`   // 2 MB floor (terrible connections)
	AbsoluteMax   int64 `yaml:"absolute_max"`   // 256 MB ceiling (datacenter)
	InitialSize   int64 `yaml:"initial_size"`   // 16 MB starting point (if probe skipped)
	TargetSeconds int   `yaml:"target_seconds"` // Target seconds per chunk (8s default)
}

// ProbeConfig holds speed probe settings
type ProbeConfig struct {
	Size    int64         `yaml:"size"`    // Probe size in bytes (1 MB default)
	Timeout time.Duration `yaml:"timeout"` // Probe timeout (30s default)
}

// RetryConfig holds retry and timeout settings
type RetryConfig struct {
	ChunkTimeout     time.Duration `yaml:"chunk_timeout"`      // Per-chunk timeout (60s default)
	MaxRetries       int           `yaml:"max_retries"`        // Max retry attempts (5 default)
	ReduceOnTimeout  float64       `yaml:"reduce_on_timeout"`  // Reduce to this fraction on timeout (0.5)
	ReduceOnFailure  float64       `yaml:"reduce_on_failure"`  // Reduce to this fraction on failure (0.5)
	BackoffBase      time.Duration `yaml:"backoff_base"`       // Base backoff duration (1s default)
	BackoffMaxJitter time.Duration `yaml:"backoff_max_jitter"` // Max jitter to add (500ms default)
}

// VersioningConfig holds file versioning settings
type VersioningConfig struct {
	DefaultTTLDays int           `yaml:"default_ttl_days"`
	MinTTLDays     int           `yaml:"min_ttl_days"`
	GCInterval     time.Duration `yaml:"gc_interval"`
}

// GCConfig holds garbage collection settings
type GCConfig struct {
	Enabled        bool          `yaml:"enabled"`          // default: true
	WorkerInterval time.Duration `yaml:"worker_interval"`  // default: 30s (queue poll)
	ScanInterval   time.Duration `yaml:"scan_interval"`    // default: 24h (full scan)
	BatchSize      int           `yaml:"batch_size"`       // default: 100 (items per tick)
	GracePeriod    time.Duration `yaml:"grace_period"`     // default: 1h (delay before S3 delete)
	DryRun         bool          `yaml:"dry_run"`          // default: false
}

// Load reads configuration from config.yaml and environment variables
func Load() (*Config, error) {
	cfg := DefaultConfig()

	// Try to load config file
	configPath := getEnv("CONFIG_PATH", "config.yaml")
	if data, err := os.ReadFile(configPath); err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	// Override with environment variables
	cfg.applyEnvOverrides()

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// DefaultConfig returns sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:         ":8080",
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 300 * time.Second,
			MaxUploadMB:  10240, // 10 GB
		},
		Database: DatabaseConfig{
			Hosts:       []string{"localhost:9042"},
			Keyspace:    "sesamefs",
			Consistency: "LOCAL_QUORUM",
			LocalDC:     "datacenter1",
		},
		Storage: StorageConfig{
			DefaultClass: "hot",
			Backends: map[string]BackendConfig{
				"hot": {
					Type:   "s3",
					Bucket: "sesamefs-blocks",
					Region: "us-east-1",
				},
			},
		},
		Auth: AuthConfig{
			DevMode: true,
			DevTokens: []DevTokenEntry{
				{
					Token:  "dev-token-123",
					UserID: "00000000-0000-0000-0000-000000000001",
					OrgID:  "00000000-0000-0000-0000-000000000001",
				},
			},
			OIDC: OIDCConfig{
				Enabled:          false, // Disabled by default, use dev tokens
				Scopes:           []string{"openid", "profile", "email"},
				AutoProvision:    true,
				DefaultRole:      "user",
				PlatformOrgID:    "00000000-0000-0000-0000-000000000000",
				SessionTTL:       24 * time.Hour,
				RefreshTokenTTL:  7 * 24 * time.Hour,
				RequirePKCE:      true,
				ValidateAudience: true,
				AllowedClockSkew: 2 * time.Minute,
			},
		},
		Chunking: ChunkingConfig{
			Algorithm:     "fastcdc",
			HashAlgorithm: "sha256",
			Adaptive: AdaptiveConfig{
				Enabled:       true,
				AbsoluteMin:   2 * 1024 * 1024,   // 2 MB
				AbsoluteMax:   256 * 1024 * 1024, // 256 MB
				InitialSize:   16 * 1024 * 1024,  // 16 MB
				TargetSeconds: 8,                 // 8 seconds per chunk
			},
			Probe: ProbeConfig{
				Size:    1 * 1024 * 1024, // 1 MB probe
				Timeout: 30 * time.Second,
			},
			Retry: RetryConfig{
				ChunkTimeout:     60 * time.Second,
				MaxRetries:       5,
				ReduceOnTimeout:  0.5,
				ReduceOnFailure:  0.5,
				BackoffBase:      1 * time.Second,
				BackoffMaxJitter: 500 * time.Millisecond,
			},
		},
		Versioning: VersioningConfig{
			DefaultTTLDays: 90,
			MinTTLDays:     7,
			GCInterval:     24 * time.Hour,
		},
		GC: GCConfig{
			Enabled:        true,
			WorkerInterval: 30 * time.Second,
			ScanInterval:   24 * time.Hour,
			BatchSize:      100,
			GracePeriod:    1 * time.Hour,
			DryRun:         false,
		},
		SeafHTTP: SeafHTTPConfig{
			TokenTTL: 1 * time.Hour,
		},
		OnlyOffice: OnlyOfficeConfig{
			Enabled:           false,
			VerifyCertificate: true,
			ForceSave:         true,
			ViewExtensions:    []string{"doc", "docx", "ppt", "pptx", "xls", "xlsx", "odt", "fodt", "odp", "fodp", "ods", "fods"},
			EditExtensions:    []string{"docx", "pptx", "xlsx"},
		},
		Elasticsearch: ElasticsearchConfig{
			Enabled: true,
			URLs:    []string{"http://localhost:9200"},
			Index:   "sesamefs-files",
		},
	}
}

// applyEnvOverrides applies environment variable overrides
func (c *Config) applyEnvOverrides() {
	// Server
	if v := os.Getenv("PORT"); v != "" {
		c.Server.Port = ":" + v
	}
	if v := os.Getenv("SERVER_PORT"); v != "" {
		c.Server.Port = v
	}

	// Database
	if v := os.Getenv("CASSANDRA_HOSTS"); v != "" {
		c.Database.Hosts = []string{v}
	}
	if v := os.Getenv("CASSANDRA_KEYSPACE"); v != "" {
		c.Database.Keyspace = v
	}
	if v := os.Getenv("CASSANDRA_USERNAME"); v != "" {
		c.Database.Username = v
	}
	if v := os.Getenv("CASSANDRA_PASSWORD"); v != "" {
		c.Database.Password = v
	}
	if v := os.Getenv("CASSANDRA_LOCAL_DC"); v != "" {
		c.Database.LocalDC = v
	}

	// Storage
	if v := os.Getenv("S3_BUCKET"); v != "" {
		if hot, ok := c.Storage.Backends["hot"]; ok {
			hot.Bucket = v
			c.Storage.Backends["hot"] = hot
		}
	}
	if v := os.Getenv("S3_REGION"); v != "" {
		if hot, ok := c.Storage.Backends["hot"]; ok {
			hot.Region = v
			c.Storage.Backends["hot"] = hot
		}
	}
	if v := os.Getenv("S3_ENDPOINT"); v != "" {
		if hot, ok := c.Storage.Backends["hot"]; ok {
			hot.Endpoint = v
			c.Storage.Backends["hot"] = hot
		}
	}

	// Auth
	if v := os.Getenv("AUTH_DEV_MODE"); v != "" {
		c.Auth.DevMode = v == "true" || v == "1"
	}
	if v := os.Getenv("AUTH_ALLOW_ANONYMOUS"); v != "" {
		c.Auth.AllowAnonymous = v == "true" || v == "1"
	}

	// SeafHTTP
	if v := os.Getenv("SEAFHTTP_TOKEN_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.SeafHTTP.TokenTTL = d
		}
	}
	// OIDC settings
	if v := os.Getenv("OIDC_ENABLED"); v != "" {
		c.Auth.OIDC.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("OIDC_ISSUER"); v != "" {
		c.Auth.OIDC.Issuer = v
	}
	if v := os.Getenv("OIDC_CLIENT_ID"); v != "" {
		c.Auth.OIDC.ClientID = v
	}
	if v := os.Getenv("OIDC_CLIENT_SECRET"); v != "" {
		c.Auth.OIDC.ClientSecret = v
	}
	if v := os.Getenv("OIDC_REDIRECT_URIS"); v != "" {
		c.Auth.OIDC.RedirectURIs = strings.Split(v, ",")
	}
	if v := os.Getenv("OIDC_SCOPES"); v != "" {
		c.Auth.OIDC.Scopes = strings.Split(v, ",")
	}
	if v := os.Getenv("OIDC_ORG_CLAIM"); v != "" {
		c.Auth.OIDC.OrgClaim = v
	}
	if v := os.Getenv("OIDC_ROLES_CLAIM"); v != "" {
		c.Auth.OIDC.RolesClaim = v
	}
	if v := os.Getenv("OIDC_AUTO_PROVISION"); v != "" {
		c.Auth.OIDC.AutoProvision = v == "true" || v == "1"
	}
	if v := os.Getenv("OIDC_DEFAULT_ROLE"); v != "" {
		c.Auth.OIDC.DefaultRole = v
	}
	if v := os.Getenv("OIDC_DEFAULT_ORG_ID"); v != "" {
		c.Auth.OIDC.DefaultOrgID = v
	}
	if v := os.Getenv("OIDC_SESSION_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.Auth.OIDC.SessionTTL = d
		}
	}
	if v := os.Getenv("OIDC_JWT_SIGNING_KEY"); v != "" {
		c.Auth.OIDC.JWTSigningKey = v
	}
	if v := os.Getenv("OIDC_REQUIRE_PKCE"); v != "" {
		c.Auth.OIDC.RequirePKCE = v == "true" || v == "1"
	}
	if v := os.Getenv("OIDC_PLATFORM_ORG_ID"); v != "" {
		c.Auth.OIDC.PlatformOrgID = v
	}
	if v := os.Getenv("OIDC_PLATFORM_ORG_CLAIM_VALUE"); v != "" {
		c.Auth.OIDC.PlatformOrgClaimValue = v
	}

	// OnlyOffice
	if v := os.Getenv("ONLYOFFICE_ENABLED"); v != "" {
		c.OnlyOffice.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("ONLYOFFICE_API_JS_URL"); v != "" {
		c.OnlyOffice.APIJSURL = v
	}
	if v := os.Getenv("ONLYOFFICE_JWT_SECRET"); v != "" {
		c.OnlyOffice.JWTSecret = v
	}

	// Elasticsearch
	if v := os.Getenv("ELASTICSEARCH_ENABLED"); v != "" {
		c.Elasticsearch.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("ELASTICSEARCH_URL"); v != "" {
		c.Elasticsearch.URLs = []string{v}
	}
	if v := os.Getenv("ELASTICSEARCH_INDEX"); v != "" {
		c.Elasticsearch.Index = v
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Server.Port == "" {
		return fmt.Errorf("server port is required")
	}
	if len(c.Database.Hosts) == 0 {
		return fmt.Errorf("at least one database host is required")
	}
	if c.Database.Keyspace == "" {
		return fmt.Errorf("database keyspace is required")
	}
	return nil
}

// getEnv returns environment variable or default
func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// getEnvInt returns environment variable as int or default
func getEnvInt(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultValue
}
