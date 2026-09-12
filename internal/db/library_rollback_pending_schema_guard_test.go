package db

import (
	"strings"
	"testing"
)

func TestLibraryRollbackPendingMigrationExists(t *testing.T) {
	m := &Migrator{}
	files, err := m.loadFiles()
	if err != nil {
		t.Fatalf("loadFiles: %v", err)
	}
	for _, file := range files {
		if file.Version != 23 {
			continue
		}
		text := strings.ToLower(file.Content)
		if !strings.Contains(text, "create table if not exists library_rollback_pending") {
			t.Fatal("migration 023 does not create library_rollback_pending")
		}
		return
	}
	t.Fatal("migration 023 is missing")
}

func TestLibraryRollbackPendingHasExactPerLibraryIdentity(t *testing.T) {
	keys := effectivePrimaryKeys(t, map[string]bool{"library_rollback_pending": true})
	got, ok := keys["library_rollback_pending"]
	if !ok {
		t.Fatal("the migration set does not create library_rollback_pending")
	}
	want := []string{"recovery_bucket", "org_id", "library_id"}
	if len(got) != len(want) {
		t.Fatalf("library_rollback_pending PRIMARY KEY = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("library_rollback_pending PRIMARY KEY = %v, want %v", got, want)
		}
	}
	for _, column := range got {
		if column == "recorded_at" || column == "created_at" {
			t.Fatalf("library_rollback_pending PRIMARY KEY = %v includes %s; identity must be one row per (org_id, library_id), with recovery_bucket only for discovery", got, column)
		}
	}
}

func TestLibraryRollbackPendingHasNoTTL(t *testing.T) {
	m := &Migrator{}
	files, err := m.loadFiles()
	if err != nil {
		t.Fatalf("loadFiles: %v", err)
	}
	for _, file := range files {
		if file.Version != 23 {
			continue
		}
		text := strings.ToLower(file.Content)
		if strings.Contains(text, "default_time_to_live = 0") || strings.Contains(text, "default_time_to_live=0") {
			if strings.Contains(text, "using ttl") {
				t.Fatal("migration 023 must not attach USING TTL; pending rollback work is permanently recoverable until settlement")
			}
			return
		}
		t.Fatal("migration 023 must set default_time_to_live = 0 so pending rollback work cannot expire")
	}
	t.Fatal("migration 023 is missing")
}

func TestLibraryRollbackPendingRecoveryIsBucketedNotGlobal(t *testing.T) {
	keys := effectivePrimaryKeys(t, map[string]bool{"library_rollback_pending": true})
	got, ok := keys["library_rollback_pending"]
	if !ok {
		t.Fatal("the migration set does not create library_rollback_pending")
	}
	if got[0] != "recovery_bucket" {
		t.Fatalf("library_rollback_pending partition key must start with recovery_bucket so recovery can scan a bounded set of partitions, got %v", got)
	}
}
