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

// Re-read refs after the guard scan: permanent fs: is written before repair
// settlement deletes the guard. Acquisition after a scanned bucket is safe
// because GC's settled claim precedes zero-proof and final exact-P follows
// repair acquisition. A pause between any of these calls is allowed.
func publicationLivenessBeforeDestruction(readRefs, readPending func() (bool, error)) (bool, error) {
	if live, err := readRefs(); err != nil || live {
		return live, err
	}
	if pending, err := readPending(); err != nil || pending {
		return pending, err
	}
	return readRefs()
}
