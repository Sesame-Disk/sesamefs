package gc

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/google/uuid"
)

// PC-D1B.4 freezes one destruction token per durable QueueItem
// (docs/PC-D1B-CERTIFICATION-WINDOW-FENCE.md §10.2). These characterizations
// record why QueueItem.Identity() on main cannot be that token, so PC-D1B.5
// derives it from the explicit tuple the ADR freezes instead.

// Identity() carries only IdentityAt and the block candidate: distinct
// destruction units enqueued in the same cascade collapse onto one identity.
func TestPCD1B4Characterization_QueueItemIdentityCollidesAcrossUnits(t *testing.T) {
	orgID, libraryA, libraryB := uuid.New(), uuid.New(), uuid.New()
	identityAt := time.Unix(1_700_000_000, 0).UTC()
	units := []QueueItem{
		{OrgID: orgID, LibraryID: libraryA, ItemType: ItemCommit, ItemID: "commit-1", IdentityAt: identityAt, QueuedAt: identityAt},
		{OrgID: orgID, LibraryID: libraryA, ItemType: ItemCommit, ItemID: "commit-2", IdentityAt: identityAt, QueuedAt: identityAt},
		{OrgID: orgID, LibraryID: libraryA, ItemType: ItemFSObject, ItemID: "commit-1", IdentityAt: identityAt, QueuedAt: identityAt},
		{OrgID: orgID, LibraryID: libraryB, ItemType: ItemFSObject, ItemID: "fs-1", IdentityAt: identityAt, QueuedAt: identityAt},
	}
	for i := 1; i < len(units); i++ {
		if units[i].Identity() != units[0].Identity() {
			t.Fatalf("CURRENT: distinct destruction units share Identity() on main; unit %d now differs (%+v vs %+v). Update the PC-D1B.4 token tuple rationale", i, units[i].Identity(), units[0].Identity())
		}
	}
}

// Before persistence, a zero IdentityAt makes Identity() fall back to
// QueuedAt. This is not evidence that durable retries lose identity: enqueue
// persists the effective identity_at and the requeue path keeps that stored
// value. It does show why PC-D1B.5 must derive a token from a hydrated durable
// row rather than a pre-persistence QueueItem.
func TestPCD1B4Characterization_PrePersistenceIdentityFallsBackToQueuedAt(t *testing.T) {
	first := QueueItem{OrgID: uuid.New(), LibraryID: uuid.New(), ItemType: ItemCommit, ItemID: "commit-1", QueuedAt: time.Unix(1_700_000_000, 0).UTC()}
	retried := first
	retried.QueuedAt = first.QueuedAt.Add(time.Minute)
	retried.RetryCount++
	if first.Identity() == retried.Identity() {
		t.Fatal("CURRENT: a retry of an item without IdentityAt is expected to change Identity(); update the PC-D1B.4 token tuple rationale")
	}
	stamped := first
	stamped.IdentityAt = first.QueuedAt
	stampedRetry := stamped
	stampedRetry.QueuedAt = retried.QueuedAt
	stampedRetry.RetryCount++
	if stamped.Identity() != stampedRetry.Identity() {
		t.Fatal("a stamped IdentityAt must stay stable across retries")
	}
}

func TestPCD1B4Characterization_PersistedIdentityAtSurvivesRetry(t *testing.T) {
	orgID, libraryID := uuid.New(), uuid.New()
	queuedAt := time.Unix(1_700_000_000, 0).UTC()
	store := NewMockStore()
	queue := NewQueue(store)
	items := []QueueItem{
		{OrgID: orgID, LibraryID: libraryID, ItemType: ItemCommit, ItemID: "commit-1", QueuedAt: queuedAt, BlockRepresentationID: dbpkg.PlainBlockRepresentationID},
		{OrgID: orgID, LibraryID: libraryID, ItemType: ItemCommit, ItemID: "commit-2", QueuedAt: queuedAt, BlockRepresentationID: dbpkg.PlainBlockRepresentationID},
	}
	if err := queue.EnqueueBatch(items); err != nil {
		t.Fatalf("enqueue durable items: %v", err)
	}
	persisted := store.QueueItems(orgID)
	if len(persisted) != 2 {
		t.Fatalf("persisted queue rows = %d, want 2", len(persisted))
	}
	for _, item := range persisted {
		if item.IdentityAt.IsZero() || !item.IdentityAt.Equal(queuedAt) {
			t.Fatalf("persisted identity_at = %v, want durable fallback %v", item.IdentityAt, queuedAt)
		}
	}
	if persisted[0].IdentityAt != persisted[1].IdentityAt || persisted[0].ItemID == persisted[1].ItemID {
		t.Fatal("test precondition: distinct durable items share identity_at but retain distinct item identities")
	}

	firstIdentityAt := persisted[0].IdentityAt
	if err := queue.IncrementRetry(persisted[0]); err != nil {
		t.Fatalf("retry persisted queue item: %v", err)
	}
	afterRetry := store.QueueItems(orgID)
	if len(afterRetry) != 2 {
		t.Fatalf("queue rows after retry = %d, want 2", len(afterRetry))
	}
	for _, item := range afterRetry {
		if item.ItemID == "commit-1" && (!item.IdentityAt.Equal(firstIdentityAt) || item.QueuedAt.Equal(queuedAt)) {
			t.Fatalf("retry must move queued_at but preserve durable identity_at; got %+v", item)
		}
	}
}

func encodePCD1B4LengthDelimitedV1(fields ...[]byte) []byte {
	var encoded bytes.Buffer
	for _, field := range fields {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(field)))
		_, _ = encoded.Write(length[:])
		_, _ = encoded.Write(field)
	}
	return encoded.Bytes()
}

func encodePCD1B4TimestampMillis(at time.Time) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(at.UTC().UnixMilli()))
	return encoded[:]
}

const pcd1b4DestructionTokenNamespaceV1 = "6ba7b811-9dad-11d1-80b4-00c04fd430c8"

func pcd1b4DestructionTokenV1(item QueueItem) string {
	blockCandidate := []byte{}
	if !item.BlockGCCandidateIdentity.Target.IsZero() || !item.BlockGCCandidateIdentity.CandidateAt.IsZero() {
		blockCandidate = encodePCD1B4LengthDelimitedV1(
			[]byte(item.BlockGCCandidateIdentity.Target.StorageClass),
			[]byte(item.BlockGCCandidateIdentity.Target.StorageKey),
			encodePCD1B4TimestampMillis(item.BlockGCCandidateIdentity.CandidateAt),
		)
	}
	name := encodePCD1B4LengthDelimitedV1(
		[]byte("sesamefs/pcd1b4/destruction-token/v1"),
		item.OrgID[:],
		item.LibraryID[:],
		[]byte(item.ItemType),
		[]byte(item.ItemID),
		encodePCD1B4TimestampMillis(item.IdentityAt),
		blockCandidate,
	)
	return uuid.NewSHA1(uuid.MustParse(pcd1b4DestructionTokenNamespaceV1), name).String()
}

// TestPCD1B4DestructionTokenV1KnownVector freezes the namespace UUID, domain
// tag, field order, UUID/time encoding, zero-length non-block candidate branch,
// and nested block-candidate encoding. The runtime derivation must match every
// vector so commit/fs_object QueueItems cannot drift on the production branch.
func TestPCD1B4DestructionTokenV1KnownVector(t *testing.T) {
	identityAt := time.Date(2024, time.January, 2, 3, 4, 5, 6_000_000, time.UTC)
	orgID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	libraryID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	block := QueueItem{
		OrgID: orgID, LibraryID: libraryID, ItemType: ItemBlock,
		ItemID: "block-42", IdentityAt: identityAt,
		BlockGCCandidateIdentity: BlockGCCandidateIdentity{
			Target:      BlockDeleteTarget{StorageClass: "hot-s3-na", StorageKey: "org/blocks/abc123"},
			CandidateAt: identityAt,
		},
	}
	commit := QueueItem{OrgID: orgID, LibraryID: libraryID, ItemType: ItemCommit, ItemID: "commit-42", IdentityAt: identityAt}
	fsObject := QueueItem{OrgID: orgID, LibraryID: libraryID, ItemType: ItemFSObject, ItemID: "fs-42", IdentityAt: identityAt}

	vectors := []struct {
		name string
		item QueueItem
		want string
	}{
		{name: "block_candidate", item: block, want: "6af2fa07-6d4b-565e-b9f7-29a17ebd5476"},
		{name: "commit_zero_candidate", item: commit, want: "e69245e7-9a55-5765-be8c-46c519d4f359"},
		{name: "fs_object_zero_candidate", item: fsObject, want: "dc3ef498-f802-53a1-a05b-178c0a7d36d0"},
	}
	for _, vector := range vectors {
		vector := vector
		t.Run(vector.name, func(t *testing.T) {
			if got := pcd1b4DestructionTokenV1(vector.item); got != vector.want {
				t.Errorf("CW-M30 destruction-token vector = %s, want %s", got, vector.want)
			}
			retried := vector.item
			retried.QueuedAt = identityAt.Add(time.Hour)
			retried.RetryCount++
			if got, want := pcd1b4DestructionTokenV1(retried), pcd1b4DestructionTokenV1(vector.item); got != want {
				t.Errorf("durable token changed across retry: got %s, want %s", got, want)
			}
		})
	}
}
