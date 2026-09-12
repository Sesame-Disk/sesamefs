package db

import (
	"testing"
	"time"
)

func TestLibraryRollbackPendingFromProjectionKeepsDeleteKeysOnly(t *testing.T) {
	created := time.Date(2026, 9, 11, 15, 4, 5, 0, time.UTC)
	row := AdminLibraryProjectionRow{
		OrgID:        "org-1",
		LibraryID:    "lib-1",
		OwnerID:      "owner-1",
		OwnerEmail:   "owner@example.com",
		OwnerName:    "Owner",
		Name:         "display-name",
		Encrypted:    true,
		StorageClass: "hot",
		SizeBytes:    99,
		FileCount:    7,
		CreatedAt:    created,
		UpdatedAt:    created.Add(time.Hour),
	}
	pending := LibraryRollbackPendingFromProjection(row)
	if pending.OrgID != row.OrgID || pending.LibraryID != row.LibraryID || pending.OwnerID != row.OwnerID {
		t.Fatalf("pending identity = %+v, want org/library/owner from projection", pending)
	}
	if !pending.CreatedAt.Equal(created) {
		t.Fatalf("pending CreatedAt = %v, want %v (admin global bucket_day)", pending.CreatedAt, created)
	}
	wantBucket := GCDiscoveryBucket(row.OrgID, row.LibraryID)
	if pending.RecoveryBucket != wantBucket {
		t.Fatalf("RecoveryBucket = %d, want %d", pending.RecoveryBucket, wantBucket)
	}
	if pending.RecoveryBucket < 0 || pending.RecoveryBucket >= GCDiscoveryBucketCount {
		t.Fatalf("RecoveryBucket %d outside [0, %d)", pending.RecoveryBucket, GCDiscoveryBucketCount)
	}
	if pending.RecordedAt.IsZero() {
		t.Fatal("RecordedAt must be set when the marker is built")
	}

	projected := pending.ProjectionRow()
	if projected.OrgID != row.OrgID || projected.LibraryID != row.LibraryID || projected.OwnerID != row.OwnerID {
		t.Fatalf("ProjectionRow identity = %+v", projected)
	}
	if !projected.CreatedAt.Equal(created) {
		t.Fatalf("ProjectionRow CreatedAt = %v, want %v", projected.CreatedAt, created)
	}
	if projected.Name != "" || projected.OwnerEmail != "" || projected.SizeBytes != 0 || projected.DeletedAt != nil {
		t.Fatalf("ProjectionRow must not carry display/trash fields unused by the delete helper: %+v", projected)
	}
}

func TestLibraryRollbackPendingBucketIsDiscoveryOnlyAndStable(t *testing.T) {
	a := LibraryRollbackPendingBucket("org-a", "lib-a")
	b := LibraryRollbackPendingBucket("org-a", "lib-a")
	if a != b {
		t.Fatalf("bucket must be stable, got %d then %d", a, b)
	}
	if a != GCDiscoveryBucket("org-a", "lib-a") {
		t.Fatalf("bucket helper must reuse GCDiscoveryBucket, got %d want %d", a, GCDiscoveryBucket("org-a", "lib-a"))
	}
}
