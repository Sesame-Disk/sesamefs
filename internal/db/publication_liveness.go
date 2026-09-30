package db

import (
	"fmt"
	"github.com/apache/cassandra-gocql-driver/v2"
)

// PublishedBlockReferenceRepairBuckets is the existing repair discovery domain.
// Writers and destructive readers must use the same immutable bucket count.
const PublishedBlockReferenceRepairBuckets = 32

// blockHasPendingPublicationGlobal is a fail-closed destructive guard, not an
// advisory discovery scan. Rows survive TTL expiry and are removed only by the
// existing settlement paths. Scan this organization's clustering prefix and
// consume every page. LQ acquisition intersects EACH_QUORUM in the writer DC.
func (db *DB) blockHasPendingPublicationGlobal(orgID, blockID string) (bool, error) {
	for bucket := 0; bucket < PublishedBlockReferenceRepairBuckets; bucket++ {
		iter := db.Session().Query(`
   SELECT staged_block_ids FROM published_block_reference_repairs
   WHERE bucket = ? AND org_id = ?
  `, bucket, orgID).Consistency(gocql.EachQuorum).PageSize(256).Iter()
		var ids []string
		for iter.Scan(&ids) {
			for _, id := range ids {
				if id == blockID {
					if err := iter.Close(); err != nil {
						return false, fmt.Errorf("read publication guard bucket %d: %w", bucket, err)
					}
					return true, nil
				}
			}
			ids = nil
		}
		if err := iter.Close(); err != nil {
			return false, fmt.Errorf("read publication guard bucket %d: %w", bucket, err)
		}
	}
	return false, nil
}

// BlockPublicationLiveness distinguishes real references from a pending writer.
// Unknown is never destructive authority. RepairGuardOnly vetoes a NEW D, but
// is neither a real reference nor a contradiction against an already committed D.
type BlockPublicationLiveness uint8

const (
	BlockPublicationUnknown BlockPublicationLiveness = iota
	BlockPublicationZero
	BlockPublicationRealReference
	BlockPublicationRepairGuardOnly
)

// BlockPublicationLivenessGlobal is the pre-handoff decision, after GC's settled
// claim. Errors fail closed. It must not be used as a post-COMMITTED contradiction
// read: a late repair has no authority to revoke the irreversible handoff.
func (db *DB) BlockPublicationLivenessGlobal(orgID, blockID string) (BlockPublicationLiveness, error) {
	return publicationLivenessBeforeDestruction(func() (bool, error) {
		return db.BlockHasReferencesGlobal(orgID, blockID)
	}, func() (bool, error) {
		return db.blockHasPendingPublicationGlobal(orgID, blockID)
	})
}

// Re-read real refs after a negative repair scan: promotion writes permanent fs:
// before settlement deletes the guard. Late acquisition after a scanned bucket
// must observe GC's settled claim in the writer's final exact-P check.
func publicationLivenessBeforeDestruction(readRefs, readPending func() (bool, error)) (BlockPublicationLiveness, error) {
	if live, err := readRefs(); err != nil {
		return BlockPublicationUnknown, err
	} else if live {
		return BlockPublicationRealReference, nil
	}
	if pending, err := readPending(); err != nil {
		return BlockPublicationUnknown, err
	} else if pending {
		return BlockPublicationRepairGuardOnly, nil
	}
	if live, err := readRefs(); err != nil {
		return BlockPublicationUnknown, err
	} else if live {
		return BlockPublicationRealReference, nil
	}
	return BlockPublicationZero, nil
}
