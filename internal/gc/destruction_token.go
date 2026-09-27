package gc

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const destructionTokenNamespaceV1 = "6ba7b811-9dad-11d1-80b4-00c04fd430c8"

var destructionTokenDomainV1 = []byte("sesamefs/pcd1b4/destruction-token/v1")

// DestructionTokenV1 derives the durable logical destruction token from a
// hydrated QueueItem. The caller must use the row read from gc_queue so
// IdentityAt is the persisted identity timestamp, not an in-memory retry
// fallback. Retry-only fields such as QueuedAt and RetryCount are excluded.
func DestructionTokenV1(item QueueItem) (uuid.UUID, error) {
	if item.OrgID == uuid.Nil || item.ItemType != ItemBlock && item.LibraryID == uuid.Nil || item.ItemType == "" || item.ItemID == "" || item.IdentityAt.IsZero() {
		return uuid.Nil, fmt.Errorf("destruction token requires durable org, library, type, item and identity_at")
	}
	blockCandidate := []byte{}
	if item.ItemType == ItemBlock {
		candidate := item.BlockGCCandidateIdentity
		if candidate.Target.StorageClass == "" || candidate.Target.StorageKey == "" || candidate.CandidateAt.IsZero() {
			return uuid.Nil, fmt.Errorf("block destruction token requires its persisted candidate identity")
		}
		blockCandidate = encodeDestructionTokenFieldsV1(
			[]byte(candidate.Target.StorageClass),
			[]byte(candidate.Target.StorageKey),
			encodeDestructionTokenMillis(candidate.CandidateAt),
		)
	}
	name := encodeDestructionTokenFieldsV1(
		destructionTokenDomainV1,
		item.OrgID[:],
		item.LibraryID[:],
		[]byte(item.ItemType),
		[]byte(item.ItemID),
		encodeDestructionTokenMillis(item.IdentityAt),
		blockCandidate,
	)
	namespace := uuid.MustParse(destructionTokenNamespaceV1)
	return uuid.NewSHA1(namespace, name), nil
}

func encodeDestructionTokenMillis(at time.Time) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(at.UTC().UnixMilli()))
	return encoded[:]
}

func encodeDestructionTokenFieldsV1(fields ...[]byte) []byte {
	var encoded bytes.Buffer
	for _, field := range fields {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(field)))
		_, _ = encoded.Write(length[:])
		_, _ = encoded.Write(field)
	}
	return encoded.Bytes()
}
