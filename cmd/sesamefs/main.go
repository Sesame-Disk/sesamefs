package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/Sesame-Disk/sesamefs/internal/api"
	"github.com/Sesame-Disk/sesamefs/internal/config"
	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/logging"
	"github.com/joho/godotenv"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		// Will be logged properly after logging.Setup, but that requires config first.
		// Use fmt here since slog isn't configured yet.
		fmt.Println("No .env file found, using environment variables")
	}

	// Parse command
	if len(os.Args) < 2 {
		os.Args = append(os.Args, "serve")
	}

	command := os.Args[1]

	switch command {
	case "serve":
		runServer()
	case "health":
		runHealthCheck()
	case "migrate":
		runMigrations()
	case "version":
		printVersion()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		fmt.Println("Available commands: serve, health, migrate, version")
		os.Exit(1)
	}
}

func runServer() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Set up structured logging (must happen early)
	logging.Setup(cfg.Auth.DevMode)

	// Initialize database connection
	database, err := db.New(cfg.Database)
	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	// Run database migrations (idempotent)
	slog.Info("Running database migrations...")
	if err := database.Migrate(); err != nil {
		slog.Error("Migration failed", "error", err)
		os.Exit(1)
	}
	slog.Info("Migrations completed successfully")

	// Seed database with default data (idempotent)
	slog.Info("Checking database seed status...")
	if err := database.SeedDatabase(cfg.Auth.DevMode); err != nil {
		slog.Error("Failed to seed database", "error", err)
		os.Exit(1)
	}

	// Create and start the API server
	server := api.NewServer(cfg, database, Version)

	slog.Info("SesameFS starting", "version", Version, "port", cfg.Server.Port)
	if err := server.Run(); err != nil {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
}

func runHealthCheck() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Println("UNHEALTHY: Failed to load config")
		os.Exit(1)
	}

	// TODO: Check database connection
	// TODO: Check storage connection

	fmt.Printf("HEALTHY: SesameFS on port %s\n", cfg.Server.Port)
	os.Exit(0)
}

func runMigrations() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	logging.Setup(cfg.Auth.DevMode)

	database, err := db.New(cfg.Database)
	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	slog.Info("Running database migrations...")
	if err := database.Migrate(); err != nil {
		slog.Error("Migration failed", "error", err)
		os.Exit(1)
	}
	slog.Info("Migrations completed successfully")

	// Seed database with default data (idempotent)
	slog.Info("Seeding database with default data...")
	if err := database.SeedDatabase(cfg.Auth.DevMode); err != nil {
		slog.Error("Failed to seed database", "error", err)
		os.Exit(1)
	}
}

func printVersion() {
	fmt.Printf("SesameFS %s\n", Version)
	fmt.Printf("  Build Time: %s\n", BuildTime)
	fmt.Printf("  Git Commit: %s\n", GitCommit)
}
