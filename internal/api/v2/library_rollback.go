package v2

import (
	"errors"
	"fmt"
	"log"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// persistLibraryRollbackPendingFn / deleteLibraryRollbackPendingFn /
// cleanupRolledBackLibraryDerivedStateFn are production helpers with
// test-replaceable vars so fault injection can fail one step without
// inventing a second cleanup path. AfterMarkerPersist and AfterAuthority
// simulate a crash between durable steps; they are nil in production.
var (
	persistLibraryRollbackPendingFn        = dbpkg.InsertLibraryRollbackPending
	deleteLibraryRollbackPendingFn         = dbpkg.DeleteLibraryRollbackPending
	cleanupRolledBackLibraryDerivedStateFn = cleanupRolledBackLibraryDerivedState
	libraryRollbackAfterMarkerPersistFn    func() error
	libraryRollbackAfterAuthorityFn        func() error
)

// rollbackNewLibrary tears down a library a creation handler minted but could
// not finish.
//
// Sequence:
//  1. persist library_rollback_pending (discovery, not authority)
//  2. deleteUnpublishedLibraryRow — the HEAD Paxos LWT
//  3. idempotent derived cleanup
//  4. delete the marker
//
// If the marker write fails, the LWT is not executed: we must not reopen the
// crash window this marker exists to close. If the LWT finds a published HEAD,
// derived state is preserved and the marker is settled. If the LWT is
// inconclusive, the marker is retained for RecoverPendingLibraryRollbacks.
func rollbackNewLibrary(db interface{ Session() *gocql.Session }, projectionRow dbpkg.AdminLibraryProjectionRow) error {
	pending := dbpkg.LibraryRollbackPendingFromProjection(projectionRow)
	if err := persistLibraryRollbackPendingFn(db.Session(), pending); err != nil {
		return fmt.Errorf("persist library rollback marker: %w", err)
	}
	log.Printf("[library_rollback] marker created org=%s library=%s", pending.OrgID, pending.LibraryID)
	if fn := libraryRollbackAfterMarkerPersistFn; fn != nil {
		if err := fn(); err != nil {
			return err
		}
	}
	return applyAuthorizedLibraryRollbackCleanup(db.Session(), pending, false)
}

// applyAuthorizedLibraryRollbackCleanup runs the HEAD LWT and, if cleanup is
// authorized, the derived-state batch, then settles the marker. recovery=true
// uses recovery log lines and treats a settled HEAD-refused marker as success
// so a sweep can continue.
func applyAuthorizedLibraryRollbackCleanup(session *gocql.Session, pending dbpkg.LibraryRollbackPending, recovery bool) error {
	if err := runAuthorizedLibraryRollbackCleanup(session, pending); err != nil {
		if errors.Is(err, ErrLibraryRollbackRefusedHeadPublished) {
			if delErr := deleteLibraryRollbackPendingFn(session, pending); delErr != nil {
				if recovery {
					log.Printf("[library_rollback] recovery refused because HEAD exists org=%s library=%s; marker settle failed: %v", pending.OrgID, pending.LibraryID, delErr)
					return delErr
				}
				log.Printf("[library_rollback] rollback refused because HEAD exists org=%s library=%s; marker settle failed: %v", pending.OrgID, pending.LibraryID, delErr)
				return err
			}
			if recovery {
				log.Printf("[library_rollback] recovery refused because HEAD exists org=%s library=%s", pending.OrgID, pending.LibraryID)
				return nil
			}
			return err
		}
		if recovery {
			log.Printf("[library_rollback] recovery deferred org=%s library=%s: %v", pending.OrgID, pending.LibraryID, err)
		}
		return err
	}
	if err := deleteLibraryRollbackPendingFn(session, pending); err != nil {
		if recovery {
			log.Printf("[library_rollback] recovery deferred org=%s library=%s: cleanup finished but marker settle failed: %v", pending.OrgID, pending.LibraryID, err)
		}
		return fmt.Errorf("delete library rollback marker: %w", err)
	}
	if recovery {
		log.Printf("[library_rollback] recovery completed org=%s library=%s", pending.OrgID, pending.LibraryID)
	}
	return nil
}

// runAuthorizedLibraryRollbackCleanup is the shared authority+cleanup body.
// It always consults deleteUnpublishedLibraryRow; a marker never authorizes
// cleanup by itself.
func runAuthorizedLibraryRollbackCleanup(session *gocql.Session, pending dbpkg.LibraryRollbackPending) error {
	if err := deleteUnpublishedLibraryRow(session, pending.OrgID, pending.LibraryID); err != nil {
		return err
	}
	if fn := libraryRollbackAfterAuthorityFn; fn != nil {
		if err := fn(); err != nil {
			return err
		}
	}
	return cleanupRolledBackLibraryDerivedStateFn(session, pending)
}

// cleanupRolledBackLibraryDerivedState deletes exactly the derived rows
// rollbackNewLibrary has always torn down. Idempotent over missing rows.
func cleanupRolledBackLibraryDerivedState(session *gocql.Session, pending dbpkg.LibraryRollbackPending) error {
	projectionRow := pending.ProjectionRow()
	batch := session.Batch(gocql.LoggedBatch)
	dbpkg.AddDeleteLibraryPolicyQuery(batch, dbpkg.GCLibraryPolicyVersionTTL, projectionRow.OrgID, projectionRow.LibraryID)
	dbpkg.AddDeleteLibraryPolicyQuery(batch, dbpkg.GCLibraryPolicyAutoDelete, projectionRow.OrgID, projectionRow.LibraryID)
	// Tear down the same projection keys written during creation. Using the
	// original snapshot avoids a fresh Cassandra read during rollback, so a
	// transient lookup failure cannot leave phantom admin/global projections
	// behind after the canonical row is gone.
	dbpkg.AddDeleteAdminLibraryReadModelQuery(batch, projectionRow)
	batch.Query(`
		DELETE FROM libraries_by_id WHERE library_id = ?
	`, projectionRow.LibraryID)
	batch.Query(`
		DELETE FROM fs_objects WHERE library_id = ?
	`, projectionRow.LibraryID)
	batch.Query(`
		DELETE FROM commits WHERE library_id = ?
	`, projectionRow.LibraryID)
	if err := batch.Exec(); err != nil {
		return fmt.Errorf("cleanup rolled-back library derived state: %w", err)
	}
	return nil
}
