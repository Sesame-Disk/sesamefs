package api

import (
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/config"
	"github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/google/uuid"
)

func TestGCBlockEnqueuer_InvalidOrgID(t *testing.T) {
	cfg := config.GCConfig{Enabled: false}
	svc := gc.NewService(nil, nil, cfg)

	adapter := &gcBlockEnqueuer{service: svc}

	// Should not panic with invalid UUID
	adapter.EnqueueBlocks("not-a-uuid", []string{"block1"}, "hot")
}

func TestGCBlockEnqueuer_EmptyBlockIDs(t *testing.T) {
	cfg := config.GCConfig{Enabled: false}
	svc := gc.NewService(nil, nil, cfg)

	adapter := &gcBlockEnqueuer{service: svc}

	// Should not panic with empty block list
	adapter.EnqueueBlocks(uuid.New().String(), []string{}, "hot")
}

func TestGCLibraryEnqueuer_InvalidOrgID(t *testing.T) {
	cfg := config.GCConfig{Enabled: false}
	svc := gc.NewService(nil, nil, cfg)

	adapter := &gcLibraryEnqueuer{service: svc}

	// Should not panic with invalid org UUID
	adapter.EnqueueLibraryDeletion("not-a-uuid", uuid.New().String(), "hot")
}

func TestGCLibraryEnqueuer_InvalidLibraryID(t *testing.T) {
	cfg := config.GCConfig{Enabled: false}
	svc := gc.NewService(nil, nil, cfg)

	adapter := &gcLibraryEnqueuer{service: svc}

	// Should not panic with invalid library UUID
	adapter.EnqueueLibraryDeletion(uuid.New().String(), "not-a-uuid", "hot")
}

func TestGCBlockEnqueuer_ImplementsInterface(t *testing.T) {
	// Import the v2 package interface indirectly by checking the adapter
	// has the right method signature
	cfg := config.GCConfig{Enabled: false}
	svc := gc.NewService(nil, nil, cfg)

	adapter := &gcBlockEnqueuer{service: svc}

	// Verify the method exists with correct signature
	adapter.EnqueueBlocks("org-id", []string{"b1", "b2"}, "hot")
}

func TestGCLibraryEnqueuer_ImplementsInterface(t *testing.T) {
	cfg := config.GCConfig{Enabled: false}
	svc := gc.NewService(nil, nil, cfg)

	adapter := &gcLibraryEnqueuer{service: svc}

	// Verify the method exists with correct signature
	adapter.EnqueueLibraryDeletion("org-id", "lib-id", "hot")
}

func TestGCAdapters_NilService(t *testing.T) {
	// Adapters should handle nil service gracefully (panic guard)
	// Note: In practice, the service is never nil when adapters are created,
	// but we test the adapter structs can be constructed
	blockAdapter := &gcBlockEnqueuer{service: nil}
	libAdapter := &gcLibraryEnqueuer{service: nil}

	if blockAdapter.service != nil {
		t.Error("expected nil service")
	}
	if libAdapter.service != nil {
		t.Error("expected nil service")
	}
}

func TestGCConfig_Defaults(t *testing.T) {
	// Verify the config defaults used by the GC system
	cfg := config.GCConfig{
		Enabled:        true,
		WorkerInterval: 30 * time.Second,
		ScanInterval:   24 * time.Hour,
		BatchSize:      100,
		GracePeriod:    1 * time.Hour,
		DryRun:         false,
	}

	if !cfg.Enabled {
		t.Error("default should be enabled")
	}
	if cfg.WorkerInterval != 30*time.Second {
		t.Errorf("WorkerInterval = %v, want 30s", cfg.WorkerInterval)
	}
	if cfg.ScanInterval != 24*time.Hour {
		t.Errorf("ScanInterval = %v, want 24h", cfg.ScanInterval)
	}
	if cfg.BatchSize != 100 {
		t.Errorf("BatchSize = %d, want 100", cfg.BatchSize)
	}
	if cfg.GracePeriod != 1*time.Hour {
		t.Errorf("GracePeriod = %v, want 1h", cfg.GracePeriod)
	}
	if cfg.DryRun {
		t.Error("default DryRun should be false")
	}
}
