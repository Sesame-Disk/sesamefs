package gc

import (
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/config"
)

func TestStats_BlocksDeleted(t *testing.T) {
	s := &Stats{}

	if got := s.BlocksDeleted(); got != 0 {
		t.Errorf("initial BlocksDeleted() = %d, want 0", got)
	}

	s.IncrBlocksDeleted()
	s.IncrBlocksDeleted()
	s.IncrBlocksDeleted()

	if got := s.BlocksDeleted(); got != 3 {
		t.Errorf("BlocksDeleted() = %d, want 3", got)
	}
}

func TestStats_LastWorkerRun(t *testing.T) {
	s := &Stats{}

	if got := s.LastWorkerRun(); !got.IsZero() {
		t.Errorf("initial LastWorkerRun() = %v, want zero time", got)
	}

	now := time.Now()
	s.SetLastWorkerRun(now)

	if got := s.LastWorkerRun(); !got.Equal(now) {
		t.Errorf("LastWorkerRun() = %v, want %v", got, now)
	}
}

func TestStats_LastScanRun(t *testing.T) {
	s := &Stats{}

	if got := s.LastScanRun(); !got.IsZero() {
		t.Errorf("initial LastScanRun() = %v, want zero time", got)
	}

	now := time.Now()
	s.SetLastScanRun(now)

	if got := s.LastScanRun(); !got.Equal(now) {
		t.Errorf("LastScanRun() = %v, want %v", got, now)
	}
}

func TestStats_Concurrent(t *testing.T) {
	s := &Stats{}
	done := make(chan struct{})

	// Concurrent increments
	for i := 0; i < 100; i++ {
		go func() {
			s.IncrBlocksDeleted()
			done <- struct{}{}
		}()
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	if got := s.BlocksDeleted(); got != 100 {
		t.Errorf("BlocksDeleted() = %d after 100 concurrent increments, want 100", got)
	}
}

func TestGCStatus_Formatting(t *testing.T) {
	status := GCStatus{
		Enabled:            true,
		DryRun:             false,
		LastWorkerRun:      "never",
		LastScanRun:        "never",
		QueueSize:          0,
		BlocksDeletedTotal: 0,
	}

	if !status.Enabled {
		t.Error("expected Enabled=true")
	}
	if status.DryRun {
		t.Error("expected DryRun=false")
	}
	if status.LastWorkerRun != "never" {
		t.Errorf("LastWorkerRun = %q, want %q", status.LastWorkerRun, "never")
	}
}

func TestNewService_NilInputs(t *testing.T) {
	cfg := config.GCConfig{
		Enabled:        true,
		WorkerInterval: 30 * time.Second,
		ScanInterval:   24 * time.Hour,
		BatchSize:      100,
		GracePeriod:    1 * time.Hour,
		DryRun:         false,
	}

	// NewService should not panic with nil db/storage
	// (it won't be started, but should be creatable)
	svc := NewService(nil, nil, cfg)

	if svc == nil {
		t.Fatal("NewService returned nil")
	}
	if svc.queue == nil {
		t.Error("queue should be initialized")
	}
	if svc.worker == nil {
		t.Error("worker should be initialized")
	}
	if svc.scanner == nil {
		t.Error("scanner should be initialized")
	}
	if svc.stats == nil {
		t.Error("stats should be initialized")
	}
}

func TestNewService_ConfigPropagation(t *testing.T) {
	cfg := config.GCConfig{
		Enabled:        true,
		WorkerInterval: 45 * time.Second,
		ScanInterval:   12 * time.Hour,
		BatchSize:      50,
		GracePeriod:    2 * time.Hour,
		DryRun:         true,
	}

	svc := NewService(nil, nil, cfg)

	if svc.config.BatchSize != 50 {
		t.Errorf("config.BatchSize = %d, want 50", svc.config.BatchSize)
	}
	if svc.config.GracePeriod != 2*time.Hour {
		t.Errorf("config.GracePeriod = %v, want 2h", svc.config.GracePeriod)
	}
	if svc.config.DryRun != true {
		t.Error("config.DryRun should be true")
	}
	if svc.worker.dryRun != true {
		t.Error("worker.dryRun should propagate from config")
	}
}

func TestService_SetDryRun(t *testing.T) {
	cfg := config.GCConfig{
		Enabled: true,
		DryRun:  false,
	}

	svc := NewService(nil, nil, cfg)

	if svc.config.DryRun {
		t.Error("initial DryRun should be false")
	}

	svc.SetDryRun(true)

	if !svc.config.DryRun {
		t.Error("config.DryRun should be true after SetDryRun(true)")
	}
	if !svc.worker.dryRun {
		t.Error("worker.dryRun should be true after SetDryRun(true)")
	}

	svc.SetDryRun(false)

	if svc.config.DryRun {
		t.Error("config.DryRun should be false after SetDryRun(false)")
	}
}

func TestService_DisabledDoesNotStart(t *testing.T) {
	cfg := config.GCConfig{
		Enabled: false,
	}

	svc := NewService(nil, nil, cfg)
	svc.Start()

	if svc.started {
		t.Error("service should not start when disabled")
	}

	// Stop should be safe on a non-started service
	svc.Stop()
}

func TestService_TriggerChannels(t *testing.T) {
	cfg := config.GCConfig{
		Enabled: true,
	}

	svc := NewService(nil, nil, cfg)

	// Triggers should not block even when service is not running
	svc.TriggerWorker()
	svc.TriggerScanner()

	// Double-trigger should not block (buffered channel size 1)
	svc.TriggerWorker()
	svc.TriggerWorker()
	svc.TriggerScanner()
	svc.TriggerScanner()
}

func TestService_StatusWhenDisabled(t *testing.T) {
	cfg := config.GCConfig{
		Enabled: false,
		DryRun:  true,
	}

	svc := NewService(nil, nil, cfg)
	status := svc.Status()

	if status.Enabled {
		t.Error("status.Enabled should be false")
	}
	if !status.DryRun {
		t.Error("status.DryRun should be true")
	}
	if status.LastWorkerRun != "never" {
		t.Errorf("LastWorkerRun = %q, want 'never'", status.LastWorkerRun)
	}
	if status.LastScanRun != "never" {
		t.Errorf("LastScanRun = %q, want 'never'", status.LastScanRun)
	}
}

func TestService_Queue(t *testing.T) {
	cfg := config.GCConfig{}
	svc := NewService(nil, nil, cfg)

	if svc.Queue() == nil {
		t.Error("Queue() should not return nil")
	}

	if svc.Queue() != svc.queue {
		t.Error("Queue() should return the internal queue")
	}
}
