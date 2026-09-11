package v2

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Group-owned library creation is a multi-step operation (mint library rows →
// publish the first HEAD → create the group share) whose middle step can end
// UNKNOWN (H1: an ambiguous CAS, or an adopted HEAD not yet visible locally).
// UNKNOWN is never cleanup authority, so the library is preserved and the
// client is told to retry. The retry must finish THAT library — not mint a
// second one — and it may arrive concurrently, in another datacenter, or
// after the previous attempt crashed anywhere in the sequence.
//
// group_library_creation_claims (migration 023) is the durable authority for
// one logical create:
//
//   - identity: (org, owner, group, name). The group is part of the identity,
//     so a same-named create for another group is a different operation and
//     can never resume (and share) this one.
//   - single owner: the claim is acquired with INSERT ... IF NOT EXISTS. Paxos
//     decides exactly one winner for concurrent same-operation requests, and a
//     blind datacenter observes an existing claim through the same serial
//     domain instead of a local-quorum miss ("local miss ≠ absent", PC-0 §14).
//     The library rows are written BEFORE the claim, so a claim that exists
//     always points at rows that were durably written; a loser rolls back its
//     own never-published, never-shared rows, which nobody else can address.
//   - ownership-checked release: DELETE ... IF library_id = ? so an attempt
//     can never clear another attempt's claim, whether on completion, on a
//     definitive rollback, or when releasing a stale claim.
//   - fixed parameters: the claim records the share_id and created_at the
//     group share is written with (the share projections cluster on
//     created_at, so a retry using a fresh timestamp would duplicate the
//     projection row) and the storage class the library was minted with.
//     Every resumed step is idempotent — the initializer adopts an existing
//     HEAD, the share upserts the same rows — so a crash after the share but
//     before the claim is released does not re-share on retry.
//   - no TTL: the preserved library has none either. A claim whose library
//     row is gone or trashed is stale and released by the next same-operation
//     request before it mints afresh.
//
// Rollback policy (finishGroupLibraryCreation): only a FRESH attempt whose
// initializer failed definitively (not InitializationErrorForbidsRollback)
// rolls its library back. A resumed attempt is finishing a library a prior
// attempt preserved precisely because it may already be published; a later
// failure in this attempt grants no authority over that state, so it is
// retained. Once the initializer returned success the HEAD is published and
// nothing is ever rolled back: a share failure preserves and retries.

// ErrGroupLibraryCreationConflict reports a retry that explicitly asked for a
// storage class different from the one the pending operation was created
// with. A defaulted (empty) request follows the pending library's canonical
// class instead — in flexible residency the default depends on the routing
// hostname, so a retry landing in another datacenter must not be refused.
var ErrGroupLibraryCreationConflict = errors.New("a pending creation of this group library exists with a different storage class")

// errGroupLibraryClaimUnresolved is returned when the claim could be neither
// acquired nor resumed within groupLibraryClaimAttempts (every attempt found
// a stale claim that another request released and re-acquired first, or an
// ambiguous acquire that resolved as not applied).
var errGroupLibraryClaimUnresolved = errors.New("could not acquire or resume the group library creation claim")

const groupLibraryClaimAttempts = 3

// groupLibraryCreationClaim is one row of group_library_creation_claims: the
// identity of the logical create plus every parameter a retry must reproduce.
type groupLibraryCreationClaim struct {
	OrgID        string
	OwnerID      string
	GroupID      string
	Name         string
	LibraryID    string
	ShareID      string
	StorageClass string
	CreatedAt    time.Time
}

// groupLibraryCreationRequest is what a creation handler resolved from the
// HTTP request before touching the database.
type groupLibraryCreationRequest struct {
	OrgID   string
	OwnerID string
	GroupID string
	Name    string
	// RequestedStorageClass is the client's explicit storage_id/storage_class,
	// "" when the class was defaulted by policy or routing hostname.
	RequestedStorageClass string
	// ResolvedStorageClass is the class a freshly minted library gets.
	ResolvedStorageClass string
	Now                  time.Time
}

// groupLibraryCreation is an acquired (fresh) or resumed creation.
type groupLibraryCreation struct {
	Claim   groupLibraryCreationClaim
	Resumed bool
	// ProjectionRow is the canonical libraries row: the one just minted, or
	// the preserved one being resumed (its StorageClass is the class the
	// response must report).
	ProjectionRow dbpkg.AdminLibraryProjectionRow
}

// groupLibraryCreationRejection is returned by a handler's admission gate
// (plan feature flag, MaxLibraries) and carries the HTTP answer. The gate is
// only consulted for a fresh mint: a resumed creation already counts against
// every limit and must be allowed to finish.
type groupLibraryCreationRejection struct {
	Status int
	Body   gin.H
}

func (r *groupLibraryCreationRejection) Error() string {
	return fmt.Sprintf("group library creation rejected by admission gate (HTTP %d)", r.Status)
}

// groupLibraryCreationOutcome classifies finishGroupLibraryCreation's result.
type groupLibraryCreationOutcome int

const (
	// groupLibraryCreationCompleted: HEAD published, group share created,
	// claim released (best effort).
	groupLibraryCreationCompleted groupLibraryCreationOutcome = iota
	// groupLibraryCreationPending: the library is preserved and the claim
	// retained; the client must retry and will resume this same library.
	groupLibraryCreationPending
	// groupLibraryCreationFailed: definite failure. A fresh attempt was rolled
	// back and its claim released; a resumed attempt was retained.
	groupLibraryCreationFailed
)

func readGroupLibraryCreationClaim(session *gocql.Session, orgID, ownerID, groupID, name string, consistency gocql.Consistency) (groupLibraryCreationClaim, bool, error) {
	claim := groupLibraryCreationClaim{OrgID: orgID, OwnerID: ownerID, GroupID: groupID, Name: name}
	err := session.Query(`
		SELECT library_id, share_id, storage_class, created_at
		FROM group_library_creation_claims
		WHERE org_id = ? AND owner_id = ? AND group_id = ? AND name = ?
	`, orgID, ownerID, groupID, name).Consistency(consistency).Scan(&claim.LibraryID, &claim.ShareID, &claim.StorageClass, &claim.CreatedAt)
	if errors.Is(err, gocql.ErrNotFound) {
		return groupLibraryCreationClaim{}, false, nil
	}
	if err != nil {
		return groupLibraryCreationClaim{}, false, err
	}
	return claim, true, nil
}

// acquireGroupLibraryCreationClaim tries to become the single owner of the
// logical create. Returns (current, applied, found, err):
//
//   - applied: this attempt now owns the claim (current == claim).
//   - !applied && found: another attempt owns it; current is that claim.
//   - !applied && !found: an ambiguous CAS that the SERIAL confirmation read
//     shows did not apply and left no other owner either; the caller retries.
//
// An ambiguous CAS (write-unknown / timeout / connection loss) is settled by
// a SERIAL read of the row: ours ⇒ applied; another library ⇒ that owner;
// none ⇒ not applied (a SERIAL read completes any in-flight proposal, so an
// absent row means this proposal was never accepted).
func acquireGroupLibraryCreationClaim(session *gocql.Session, claim groupLibraryCreationClaim) (groupLibraryCreationClaim, bool, bool, error) {
	applied, err := session.Query(`
		INSERT INTO group_library_creation_claims (org_id, owner_id, group_id, name, library_id, share_id, storage_class, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		IF NOT EXISTS
	`, claim.OrgID, claim.OwnerID, claim.GroupID, claim.Name, claim.LibraryID, claim.ShareID, claim.StorageClass, claim.CreatedAt).MapScanCAS(map[string]interface{}{})
	if err != nil && !isAmbiguousLibraryHeadUpdateError(err) {
		return groupLibraryCreationClaim{}, false, false, err
	}
	if err == nil && applied {
		return claim, true, true, nil
	}
	// Definite loss (applied=false: the CAS result carries the existing row,
	// but reading it back through the same serial domain avoids parsing driver
	// column types out of the CAS map) or an ambiguous CAS: the SERIAL read
	// is the authority either way.
	current, found, readErr := readGroupLibraryCreationClaim(session, claim.OrgID, claim.OwnerID, claim.GroupID, claim.Name, gocql.Serial)
	if readErr != nil {
		if err != nil {
			return groupLibraryCreationClaim{}, false, false, errors.Join(fmt.Errorf("ambiguous claim acquire: %w", err), fmt.Errorf("confirmation read failed: %w", readErr))
		}
		return groupLibraryCreationClaim{}, false, false, fmt.Errorf("claim owner read failed: %w", readErr)
	}
	if !found {
		if err != nil {
			log.Printf("[GroupLibraryCreation] INFO: ambiguous claim acquire for org=%s owner=%s group=%s name=%q confirmed not applied and no other owner; retrying", claim.OrgID, claim.OwnerID, claim.GroupID, claim.Name)
		}
		return groupLibraryCreationClaim{}, false, false, nil
	}
	if current.LibraryID == claim.LibraryID {
		return claim, true, true, nil
	}
	return current, false, true, nil
}

// releaseGroupLibraryCreationClaim removes the claim only while it is still
// held by claim.LibraryID (DELETE ... IF library_id = ?). A claim that is
// absent or owned by another attempt is left alone: there is nothing of ours
// to release. Used for completion, for a fresh attempt's definitive rollback,
// and for releasing a stale claim whose library is gone.
func releaseGroupLibraryCreationClaim(session *gocql.Session, claim groupLibraryCreationClaim) error {
	for attempt := 0; attempt < 2; attempt++ {
		applied, err := session.Query(`
			DELETE FROM group_library_creation_claims
			WHERE org_id = ? AND owner_id = ? AND group_id = ? AND name = ?
			IF library_id = ?
		`, claim.OrgID, claim.OwnerID, claim.GroupID, claim.Name, claim.LibraryID).MapScanCAS(map[string]interface{}{})
		if err == nil {
			if !applied {
				log.Printf("[GroupLibraryCreation] INFO: claim for org=%s owner=%s group=%s name=%q is not held by library %s (already released or superseded); nothing to release", claim.OrgID, claim.OwnerID, claim.GroupID, claim.Name, claim.LibraryID)
			}
			return nil
		}
		if !isAmbiguousLibraryHeadUpdateError(err) {
			return err
		}
		current, found, readErr := readGroupLibraryCreationClaim(session, claim.OrgID, claim.OwnerID, claim.GroupID, claim.Name, gocql.Serial)
		if readErr != nil {
			return errors.Join(fmt.Errorf("ambiguous claim release: %w", err), fmt.Errorf("confirmation read failed: %w", readErr))
		}
		if !found || current.LibraryID != claim.LibraryID {
			return nil
		}
	}
	return fmt.Errorf("claim release for library %s remained ambiguous after retry", claim.LibraryID)
}

// mintGroupLibraryRows writes a fresh library's canonical rows and projections
// atomically. The rows are unpublished (no HEAD) and unshared; until a claim
// names this library_id nobody else can address it, so rolling them back on
// a lost claim or a definitive initializer failure is a definite cleanup.
func mintGroupLibraryRows(session *gocql.Session, req groupLibraryCreationRequest, libraryID string) (dbpkg.AdminLibraryProjectionRow, error) {
	batch := session.Batch(gocql.LoggedBatch)
	blockRepresentationID := dbpkg.NewLibraryBlockRepresentationID(libraryID, false)
	batch.Query(`
		INSERT INTO libraries (org_id, library_id, owner_id, name, encrypted, block_representation_id, storage_class, size_bytes, file_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, req.OrgID, libraryID, req.OwnerID, req.Name, false, blockRepresentationID, req.ResolvedStorageClass, int64(0), int64(0), req.Now, req.Now)
	batch.Query(`
		INSERT INTO libraries_by_id (library_id, org_id, owner_id, name, encrypted, block_representation_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, libraryID, req.OrgID, req.OwnerID, req.Name, false, blockRepresentationID)
	projectionRow := addNewLibraryProjectionQueries(session, batch, req.OrgID, libraryID, req.OwnerID, req.Name, false, req.ResolvedStorageClass, 0, 0, req.Now, req.Now)
	if err := batch.Exec(); err != nil {
		return dbpkg.AdminLibraryProjectionRow{}, err
	}
	return projectionRow, nil
}

// locateClaimedGroupLibrary finds the canonical libraries row a claim points
// at: locally first, then at EACH_QUORUM (the rows were written before the
// claim, at the writer's local quorum; an EACH_QUORUM read overlaps that
// quorum from any datacenter). stale is true when the row is gone or trashed:
// the claim outlived its library and must not be resumed.
func locateClaimedGroupLibrary(session *gocql.Session, claim groupLibraryCreationClaim) (dbpkg.AdminLibraryProjectionRow, bool, error) {
	row, err := dbpkg.ReadAdminLibraryProjectionRow(session, claim.OrgID, claim.LibraryID)
	if errors.Is(err, gocql.ErrNotFound) {
		row, err = dbpkg.ReadAdminLibraryProjectionRowAt(session, claim.OrgID, claim.LibraryID, gocql.EachQuorum)
	}
	if errors.Is(err, gocql.ErrNotFound) {
		return dbpkg.AdminLibraryProjectionRow{}, true, nil
	}
	if err != nil {
		return dbpkg.AdminLibraryProjectionRow{}, false, err
	}
	if row.DeletedAt != nil {
		return row, true, nil
	}
	return row, false, nil
}

// resumeGroupLibraryCreation turns an existing claim into a creation to
// finish. stale reports a claim whose library no longer exists (or is in the
// trash); the caller releases it. An explicit storage class that differs from
// the one the pending operation was created with is a different operation
// and is refused (ErrGroupLibraryCreationConflict) rather than silently
// re-parameterized; a defaulted request follows the pending library.
func resumeGroupLibraryCreation(session *gocql.Session, req groupLibraryCreationRequest, claim groupLibraryCreationClaim) (groupLibraryCreation, bool, error) {
	row, stale, err := locateClaimedGroupLibrary(session, claim)
	if err != nil {
		return groupLibraryCreation{}, false, fmt.Errorf("failed to locate pending library %s: %w", claim.LibraryID, err)
	}
	if stale {
		return groupLibraryCreation{}, true, nil
	}
	if req.RequestedStorageClass != "" && req.RequestedStorageClass != claim.StorageClass {
		return groupLibraryCreation{}, false, fmt.Errorf("%w: pending library %s was created with %q, request asked for %q", ErrGroupLibraryCreationConflict, claim.LibraryID, claim.StorageClass, req.RequestedStorageClass)
	}
	if req.ResolvedStorageClass != row.StorageClass {
		log.Printf("[GroupLibraryCreation] INFO: resuming pending library %s with its canonical storage class %q (this request resolved %q)", claim.LibraryID, row.StorageClass, req.ResolvedStorageClass)
	}
	return groupLibraryCreation{Claim: claim, Resumed: true, ProjectionRow: row}, false, nil
}

// beginGroupLibraryCreation resolves a request to exactly one creation: the
// preserved attempt for the same logical operation, or a fresh library. The
// fresh-vs-resumed decision is made once, by the claim CAS, and the
// admission gate is applied only to the path that mints: a request the gate
// rejects is still allowed to resume an existing claim (read at SERIAL, the
// domain that decided it), and a fresh library is never minted without
// having passed the gate.
func beginGroupLibraryCreation(database *dbpkg.DB, req groupLibraryCreationRequest, gate func() error) (groupLibraryCreation, error) {
	session := database.Session()
	if gate != nil {
		if gateErr := gate(); gateErr != nil {
			claim, found, err := readGroupLibraryCreationClaim(session, req.OrgID, req.OwnerID, req.GroupID, req.Name, gocql.Serial)
			if err != nil {
				return groupLibraryCreation{}, fmt.Errorf("failed to check pending group library creation: %w", err)
			}
			if !found {
				return groupLibraryCreation{}, gateErr
			}
			creation, stale, err := resumeGroupLibraryCreation(session, req, claim)
			if err != nil {
				return groupLibraryCreation{}, err
			}
			if stale {
				if err := releaseGroupLibraryCreationClaim(session, claim); err != nil {
					log.Printf("[GroupLibraryCreation] WARNING: failed to release stale claim for library %s: %v", claim.LibraryID, err)
				}
				return groupLibraryCreation{}, gateErr
			}
			return creation, nil
		}
	}

	fresh := groupLibraryCreationClaim{
		OrgID:        req.OrgID,
		OwnerID:      req.OwnerID,
		GroupID:      req.GroupID,
		Name:         req.Name,
		LibraryID:    uuid.NewString(),
		ShareID:      uuid.NewString(),
		StorageClass: req.ResolvedStorageClass,
		CreatedAt:    req.Now,
	}
	projectionRow, err := mintGroupLibraryRows(session, req, fresh.LibraryID)
	if err != nil {
		return groupLibraryCreation{}, fmt.Errorf("failed to create library: %w", err)
	}
	abandon := func() {
		if err := rollbackNewLibrary(database, projectionRow); err != nil {
			log.Printf("[GroupLibraryCreation] WARNING: rollback of unclaimed library %s failed: %v", fresh.LibraryID, err)
		}
	}
	for attempt := 0; attempt < groupLibraryClaimAttempts; attempt++ {
		current, applied, found, err := acquireGroupLibraryCreationClaim(session, fresh)
		if err != nil {
			abandon()
			return groupLibraryCreation{}, fmt.Errorf("failed to claim group library creation: %w", err)
		}
		if applied {
			return groupLibraryCreation{Claim: fresh, ProjectionRow: projectionRow}, nil
		}
		if !found {
			continue
		}
		creation, stale, err := resumeGroupLibraryCreation(session, req, current)
		if err != nil {
			abandon()
			return groupLibraryCreation{}, err
		}
		if !stale {
			abandon()
			return creation, nil
		}
		log.Printf("[GroupLibraryCreation] INFO: claim for org=%s owner=%s group=%s name=%q points at library %s which no longer exists; releasing stale claim", current.OrgID, current.OwnerID, current.GroupID, current.Name, current.LibraryID)
		if err := releaseGroupLibraryCreationClaim(session, current); err != nil {
			abandon()
			return groupLibraryCreation{}, fmt.Errorf("failed to release stale claim for library %s: %w", current.LibraryID, err)
		}
	}
	abandon()
	return groupLibraryCreation{}, errGroupLibraryClaimUnresolved
}

// groupLibraryCreationSteps are the side-effecting steps
// finishGroupLibraryCreation sequences; injected so the rollback policy is
// unit-testable without Cassandra.
type groupLibraryCreationSteps struct {
	initialize func(claim groupLibraryCreationClaim) error
	share      func(claim groupLibraryCreationClaim) error
	// abandon rolls a FRESH attempt back (library rows + claim). Never called
	// for a resumed creation or after the initializer succeeded.
	abandon func(creation groupLibraryCreation) error
	// complete releases the claim after the share exists. Best effort: a
	// failure leaves a stale claim that the next same-operation request
	// resumes idempotently and releases.
	complete func(claim groupLibraryCreationClaim) error
}

// finishGroupLibraryCreation runs initialize → share → complete under the
// rollback policy described at the top of this file.
func finishGroupLibraryCreation(creation groupLibraryCreation, steps groupLibraryCreationSteps) (groupLibraryCreationOutcome, error) {
	claim := creation.Claim
	if err := steps.initialize(claim); err != nil {
		if InitializationErrorForbidsRollback(err) {
			// The HEAD may already be published: preserve everything, keep
			// the claim, let the client retry and resume this library.
			return groupLibraryCreationPending, fmt.Errorf("library initialization pending: %w", err)
		}
		if creation.Resumed {
			// A prior attempt preserved this library because its HEAD may be
			// published. This attempt's own failure says nothing about that:
			// no cleanup authority, retain library and claim.
			return groupLibraryCreationFailed, fmt.Errorf("initialization of preserved library %s failed (retained, no cleanup authority): %w", claim.LibraryID, err)
		}
		// Fresh attempt, definitive pre-publication failure: the rows are
		// ours alone (unpublished, unshared, addressed only by our claim).
		if rollbackErr := steps.abandon(creation); rollbackErr != nil {
			return groupLibraryCreationFailed, errors.Join(fmt.Errorf("library initialization failed: %w", err), fmt.Errorf("rollback failed: %w", rollbackErr))
		}
		return groupLibraryCreationFailed, fmt.Errorf("library initialization failed: %w", err)
	}
	if err := steps.share(claim); err != nil {
		// HEAD is published: never destructive past this point. The claim
		// stays so the retry re-shares idempotently (same share_id and
		// created_at).
		return groupLibraryCreationPending, fmt.Errorf("group share creation failed after HEAD publish (library preserved): %w", err)
	}
	if err := steps.complete(claim); err != nil {
		log.Printf("[GroupLibraryCreation] WARNING: library %s created and shared but its claim could not be released (best effort; the next same-operation request resumes idempotently and releases it): %v", claim.LibraryID, err)
	}
	return groupLibraryCreationCompleted, nil
}

// runGroupLibraryCreation is the one entry point for the three group-owned
// library creation handlers: begin (claim) + finish (initialize, share,
// complete) with the real database steps.
func runGroupLibraryCreation(database *dbpkg.DB, req groupLibraryCreationRequest, gate func() error) (groupLibraryCreation, groupLibraryCreationOutcome, error) {
	creation, err := beginGroupLibraryCreation(database, req, gate)
	if err != nil {
		return groupLibraryCreation{}, groupLibraryCreationFailed, err
	}
	session := database.Session()
	outcome, err := finishGroupLibraryCreation(creation, groupLibraryCreationSteps{
		initialize: func(claim groupLibraryCreationClaim) error {
			return NewFSHelper(database).InitializeLibraryFS(req.OrgID, claim.LibraryID, req.OwnerID, req.Name)
		},
		share: func(claim groupLibraryCreationClaim) error {
			return createLibraryShare(database, claim.LibraryID, claim.ShareID, req.OwnerID, req.GroupID, "group", "rw", claim.CreatedAt, nil)
		},
		abandon: func(creation groupLibraryCreation) error {
			if err := rollbackNewLibrary(database, creation.ProjectionRow); err != nil {
				return err
			}
			return releaseGroupLibraryCreationClaim(session, creation.Claim)
		},
		complete: func(claim groupLibraryCreationClaim) error {
			return releaseGroupLibraryCreationClaim(session, claim)
		},
	})
	return creation, outcome, err
}

// writeGroupLibraryCreationError maps a failed runGroupLibraryCreation to the
// HTTP answer shared by the three handlers.
func writeGroupLibraryCreationError(c *gin.Context, logTag string, outcome groupLibraryCreationOutcome, err error) {
	var rejection *groupLibraryCreationRejection
	switch {
	case errors.As(err, &rejection):
		c.JSON(rejection.Status, rejection.Body)
	case errors.Is(err, ErrGroupLibraryCreationConflict):
		log.Printf("%s %v", logTag, err)
		c.JSON(http.StatusConflict, gin.H{"error": "a pending creation of this library exists with a different storage class; retry with the same storage class to complete it"})
	case outcome == groupLibraryCreationPending:
		log.Printf("%s library creation outcome pending, preserving library: %v", logTag, err)
		c.Header("Retry-After", "1")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "library creation pending; retry"})
	default:
		log.Printf("%s %v", logTag, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create library"})
	}
}
