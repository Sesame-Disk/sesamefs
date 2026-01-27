package db

import (
	"log"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

// SeedDatabase creates default organization and admin user if they don't exist
// This runs automatically on application startup
func (db *DB) SeedDatabase(devMode bool) error {
	// Check if default org exists (idempotent check)
	var orgID uuid.UUID
	defaultOrgID := uuid.MustParse("00000000-0000-0000-0000-000000000001")

	err := db.Session().Query(`
		SELECT org_id FROM organizations WHERE org_id = ?
	`, defaultOrgID).Scan(&orgID)

	if err == nil {
		log.Println("✓ Database already seeded, skipping")
		return nil
	}

	log.Println("→ Seeding database with default data...")

	// Create default organization
	if err := db.createDefaultOrganization(defaultOrgID); err != nil {
		return err
	}

	// Create default admin user
	if err := db.createDefaultAdmin(defaultOrgID); err != nil {
		return err
	}

	// Create test users in dev mode only
	if devMode {
		log.Println("→ Dev mode: Creating test users")
		if err := db.createTestUsers(defaultOrgID); err != nil {
			return err
		}
	}

	log.Println("✓ Database seeding completed successfully")
	return nil
}

// createDefaultOrganization creates the default organization
func (db *DB) createDefaultOrganization(orgID uuid.UUID) error {
	now := time.Now()

	query := `
		INSERT INTO organizations (
			org_id, name, settings, storage_quota, storage_used,
			chunking_polynomial, storage_config, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	// Default settings
	settings := map[string]string{
		"theme":    "default",
		"features": "all",
	}

	storageConfig := map[string]string{
		"default_backend": "s3",
	}

	err := db.Session().Query(query,
		orgID.String(), // Convert UUID to string
		"Default Organization",
		settings,
		int64(1099511627776), // 1TB default quota
		int64(0),             // 0 bytes used
		int64(17592186044415), // Default Rabin polynomial
		storageConfig,
		now,
	).Exec()

	if err != nil {
		log.Printf("✗ Failed to create default organization: %v", err)
		return err
	}

	log.Printf("✓ Created default organization: %s", orgID)
	return nil
}

// createDefaultAdmin creates the default admin user
func (db *DB) createDefaultAdmin(orgID uuid.UUID) error {
	adminUserID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	adminEmail := "admin@sesamefs.local"
	now := time.Now()

	// Use batch for atomic dual-write to users and users_by_email
	batch := db.Session().NewBatch(gocql.LoggedBatch)

	// Insert into users table
	batch.Query(`
		INSERT INTO users (
			org_id, user_id, email, name, role,
			quota_bytes, used_bytes, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		orgID.String(),      // Convert UUID to string
		adminUserID.String(), // Convert UUID to string
		adminEmail,
		"System Administrator",
		"admin", // ← CRITICAL: admin role for full permissions
		int64(107374182400), // 100GB personal quota
		int64(0),
		now,
	)

	// Insert into users_by_email lookup table
	batch.Query(`
		INSERT INTO users_by_email (email, user_id, org_id)
		VALUES (?, ?, ?)
	`,
		adminEmail,
		adminUserID.String(), // Convert UUID to string
		orgID.String(),       // Convert UUID to string
	)

	if err := db.Session().ExecuteBatch(batch); err != nil {
		log.Printf("✗ Failed to create admin user: %v", err)
		return err
	}

	log.Printf("✓ Created admin user: %s (%s)", adminEmail, adminUserID)
	return nil
}

// createTestUsers creates test users for development/testing
func (db *DB) createTestUsers(orgID uuid.UUID) error {
	now := time.Now()

	testUsers := []struct {
		userID uuid.UUID
		email  string
		name   string
		role   string
	}{
		{
			userID: uuid.MustParse("00000000-0000-0000-0000-000000000002"),
			email:  "user@sesamefs.local",
			name:   "Test User",
			role:   "user",
		},
		{
			userID: uuid.MustParse("00000000-0000-0000-0000-000000000003"),
			email:  "readonly@sesamefs.local",
			name:   "Read-Only User",
			role:   "readonly",
		},
		{
			userID: uuid.MustParse("00000000-0000-0000-0000-000000000004"),
			email:  "guest@sesamefs.local",
			name:   "Guest User",
			role:   "guest",
		},
	}

	for _, user := range testUsers {
		batch := db.Session().NewBatch(gocql.LoggedBatch)

		// Insert into users table
		batch.Query(`
			INSERT INTO users (
				org_id, user_id, email, name, role,
				quota_bytes, used_bytes, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`,
			orgID.String(),        // Convert UUID to string
			user.userID.String(),  // Convert UUID to string
			user.email,
			user.name,
			user.role,
			int64(53687091200), // 50GB for test users
			int64(0),
			now,
		)

		// Insert into users_by_email lookup table
		batch.Query(`
			INSERT INTO users_by_email (email, user_id, org_id)
			VALUES (?, ?, ?)
		`,
			user.email,
			user.userID.String(),  // Convert UUID to string
			orgID.String(),        // Convert UUID to string
		)

		if err := db.Session().ExecuteBatch(batch); err != nil {
			log.Printf("✗ Failed to create test user %s: %v", user.email, err)
			return err
		}

		log.Printf("✓ Created test user: %s (%s) with role '%s'", user.email, user.userID, user.role)
	}

	return nil
}
