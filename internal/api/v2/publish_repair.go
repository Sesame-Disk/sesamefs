package v2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

const (
	publishedBlockReferenceRepairBuckets       = 32
	publishedBlockReferenceRepairSweepInterval = time.Minute
	publishedBlockReferenceRepairStaleAfter    = 30 * time.Second
	publishedBlockReferenceRepairPreCASLease   = 5 * time.Minute
	publishedBlockReferenceRepairRetryBase     = 5 * time.Minute
	publishedBlockReferenceRepairRetryMax      = 6 * time.Hour
	pendingPublishedFSObjectOwnerStaleAfter    = 24 * time.Hour
	pendingPublishedFSObjectOwnerSweepInterval = 15 * time.Minute
	pendingPublishedFSObjectOwnerLookbackDays  = db.PendingPublishedFSObjectOwnerTTLSeconds / (24 * 60 * 60)
	publishedCommitReachabilityMaxNodes        = 1024
	publishedCommitReachabilityTimeout         = 30 * time.Second
	// publishedCommitReachabilityMaxHeadObservations is the per-visit budget
	// of SERIAL canonical HEAD reads. It is enforced by the classifier, not
	// assumed: creating the anchor spends one, every re-anchor attempt spends
	// one, and a re-anchor CAS loser that would need a third read returns
	// UNKNOWN and lets the next visit resume from the durable newer snapshot.
	publishedCommitReachabilityMaxHeadObservations = 2
	// publishedBlockReferenceRepairLivenessLease bounds how long one visit
	// (one renewal producer) may keep writing repair-owned pub: refs after it
	// recorded its cleanup intent. The per-block fan-out stops before
	// lease - skew; the sweep may consume a still-preparing intent only after
	// the lease. The skew margin is the clock-skew budget between the
	// producer and any sweeper.
	publishedBlockReferenceRepairLivenessLease     = 10 * time.Minute
	publishedBlockReferenceRepairLivenessLeaseSkew = time.Minute
)

// publishedCommitReachabilityMaxNodes bounds one ancestry chunk, not the
// lifetime of an anchor and not an entire worker visit. SERIAL HEAD is
// observed when creating or replacing the durable anchor; later retries of
// that snapshot walk from the cursor and do not re-read HEAD. A visit that
// clean-walks to genesis, persists exhaustion, and re-anchors to a newer
// SERIAL HEAD may walk a second chunk in the same 30s context: at most
// publishedCommitReachabilityMaxHeadObservations SERIAL HEAD observations and
// 2*publishedCommitReachabilityMaxNodes parent reads. Timeout, bound,
// EACH_QUORUM error, cycle, and malformed ancestry do not re-anchor.

type publishedBlockReferenceRepair struct {
	Bucket         int
	OrgID          string
	RepoID         string
	CommitID       string
	FSID           string
	StagedBlockIDs []string
	CreatedAt      time.Time
	// LeaseExpiresAt remains persisted for scheduling/diagnostics compatibility;
	// it is never publication authority and never authorizes cleanup.
	LeaseExpiresAt time.Time
	// ReachabilityAnchorHeadCommitID is one SERIAL canonical HEAD observation.
	// It is potential positive evidence only: later live HEAD movement must not
	// restart the walk, and absence of the target from this chain is never
	// cleanup authority.
	ReachabilityAnchorHeadCommitID string
	// ReachabilityCursorCommitID is the next commit the bounded walk will visit.
	// Progress has no TTL while the repair row exists.
	ReachabilityCursorCommitID string
	// ReachabilityAnchorExhausted is true after a clean walk of this
	// snapshot reached genesis without the target. It is not negative
	// authority. It exists so a later SERIAL HEAD timeout cannot force the
	// same ancestry prefix to be replayed.
	ReachabilityAnchorExhausted bool
	// LivenessToken identifies one renewal producer (one visit) of this
	// row's repair-owned pub:. It keys the durable cleanup intent, so a
	// visit deletes only its own witness, never another producer's.
	LivenessToken string
	// LivenessArmed / LivenessLeaseExpiresAt are the producer fence of that
	// intent: not armed and lease not expired means the producer may still
	// be writing pins and the sweep must not consume the intent.
	LivenessArmed          bool
	LivenessLeaseExpiresAt time.Time
}

// publishedBlockReferenceRepairCommitOutcome is deliberately fail-closed.
// Only positive reachability is actionable in the background repair path.
// A false or incomplete reachability observation may be caused by a stale,
// locally blind, or otherwise ambiguous view of the canonical publication.
// It must retain the durable row and all artifacts for a later confirmation.
type publishedBlockReferenceRepairCommitOutcome uint8

const (
	publishedBlockReferenceRepairCommitUnknown publishedBlockReferenceRepairCommitOutcome = iota
	publishedBlockReferenceRepairCommitReachable
	// The current protocol has no durable, globally conclusive negative witness.
	// Keep this state explicit so callers cannot collapse an inconclusive read
	// into cleanup authority; the classifier deliberately has no emitter for it.
	publishedBlockReferenceRepairCommitDefinitelyNotReachable
	// The durable repair row is gone. This is not positive reachability and
	// never authorizes promote, cleanup, or pub: renewal.
	publishedBlockReferenceRepairCommitNoLongerPending
)

// errPublishedBlockReferenceRepairGone means this work is no longer queued.
// Row absence is not HEAD evidence.
var errPublishedBlockReferenceRepairGone = errors.New("queued publish repair is no longer pending")

var scheduledPublishedBlockReferenceRepairs sync.Map

// Retry backoff is process-local advisory state. The durable repair row is
// deliberately written and deleted only with ordinary mutations; it is not a
// Paxos state machine. Losing this hint on restart is safe: the next sweep may
// retry the durable row earlier, but cleanup authority remains unchanged.
// publishedBlockReferenceRepairRetryMax caps this hint only; it is not an
// upper bound on discovery visit interval
// (ISSUE-PUBLISH-REPAIR-DISCOVERY-SCALE-01).
var publishedBlockReferenceRepairNextRetryAt sync.Map

// prunePublishedBlockReferenceRepairRetryHints bounds process-local retry
// state to the lifetime of an actionable hint.
func prunePublishedBlockReferenceRepairRetryHints(now time.Time) {
	publishedBlockReferenceRepairNextRetryAt.Range(func(key, value any) bool {
		retryAt, ok := value.(time.Time)
		if !ok || !retryAt.After(now) {
			publishedBlockReferenceRepairNextRetryAt.CompareAndDelete(key, value)
		}
		return true
	})
}

var startPublishedBlockReferenceRepairWorkerOnce sync.Once

var schedulePublishedBlockReferenceRepairSleepFn = time.Sleep

var schedulePublishedBlockReferenceRepairRunFn = func(repair func()) {
	go repair()
}

var publishedBlockReferenceRepairNowFn = time.Now

var publishedBlockReferenceRepairTickerFn = func(interval time.Duration) *time.Ticker {
	return time.NewTicker(interval)
}

var insertPublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
	if database == nil {
		return fmt.Errorf("database not available")
	}
	if err := database.Session().Query(`
		INSERT INTO published_block_reference_repairs (bucket, org_id, repo_id, commit_id, fs_id, staged_block_ids, created_at, lease_expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, repair.Bucket, repair.OrgID, repair.RepoID, repair.CommitID, repair.FSID, repair.StagedBlockIDs, repair.CreatedAt, repair.LeaseExpiresAt).Exec(); err != nil {
		return err
	}
	publishedBlockReferenceRepairNextRetryAt.Delete(publishedBlockReferenceRepairRetryKey(repair))
	return nil
}

var deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
	if database == nil {
		return fmt.Errorf("database not available")
	}
	if err := database.Session().Query(`
		DELETE FROM published_block_reference_repairs
		WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
	`, repair.Bucket, repair.OrgID, repair.RepoID, repair.CommitID, repair.FSID).
		Exec(); err != nil {
		return err
	}
	publishedBlockReferenceRepairNextRetryAt.Delete(publishedBlockReferenceRepairRetryKey(repair))
	return nil
}

// schedulePublishedBlockReferenceRepairRetryFn records only process-local
// advisory backoff. It intentionally does not mutate the durable repair row:
// restart may forget this hint, and a retry can never resurrect a settled row.
var schedulePublishedBlockReferenceRepairRetryFn = func(database *db.DB, repair publishedBlockReferenceRepair, nextRetryAt time.Time) error {
	if database == nil {
		return fmt.Errorf("database not available")
	}
	if nextRetryAt.IsZero() {
		nextRetryAt = publishedBlockReferenceRepairNowFn().UTC()
	}
	publishedBlockReferenceRepairNextRetryAt.Store(publishedBlockReferenceRepairRetryKey(repair), nextRetryAt.UTC())
	return nil
}

var listPublishedBlockReferenceRepairsForBucketFn = func(database *db.DB, bucket int) ([]publishedBlockReferenceRepair, error) {
	if database == nil {
		return nil, fmt.Errorf("database not available")
	}
	iter := database.Session().Query(`
		SELECT org_id, repo_id, commit_id, fs_id, staged_block_ids, created_at, lease_expires_at, reachability_anchor_head_commit_id, reachability_cursor_commit_id, reachability_anchor_exhausted
		FROM published_block_reference_repairs WHERE bucket = ?
	`, bucket).Iter()

	var repairs []publishedBlockReferenceRepair
	var repair publishedBlockReferenceRepair
	for iter.Scan(&repair.OrgID, &repair.RepoID, &repair.CommitID, &repair.FSID, &repair.StagedBlockIDs, &repair.CreatedAt, &repair.LeaseExpiresAt, &repair.ReachabilityAnchorHeadCommitID, &repair.ReachabilityCursorCommitID, &repair.ReachabilityAnchorExhausted) {
		repair.Bucket = bucket
		repair.ReachabilityAnchorHeadCommitID = strings.TrimSpace(repair.ReachabilityAnchorHeadCommitID)
		repair.ReachabilityCursorCommitID = strings.TrimSpace(repair.ReachabilityCursorCommitID)
		repairs = append(repairs, repair)
		repair = publishedBlockReferenceRepair{}
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return repairs, nil
}

var loadPublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
	if database == nil {
		return publishedBlockReferenceRepair{}, fmt.Errorf("database not available")
	}
	if database.Session() == nil {
		return publishedBlockReferenceRepair{}, fmt.Errorf("database session not available")
	}
	loaded := publishedBlockReferenceRepair{
		Bucket: repair.Bucket,
	}
	err := database.Session().Query(`
		SELECT org_id, repo_id, commit_id, fs_id, staged_block_ids, created_at, lease_expires_at, reachability_anchor_head_commit_id, reachability_cursor_commit_id, reachability_anchor_exhausted
		FROM published_block_reference_repairs
		WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
	`, repair.Bucket, repair.OrgID, repair.RepoID, repair.CommitID, repair.FSID).
		Scan(&loaded.OrgID, &loaded.RepoID, &loaded.CommitID, &loaded.FSID, &loaded.StagedBlockIDs, &loaded.CreatedAt, &loaded.LeaseExpiresAt, &loaded.ReachabilityAnchorHeadCommitID, &loaded.ReachabilityCursorCommitID, &loaded.ReachabilityAnchorExhausted)
	if err != nil {
		return publishedBlockReferenceRepair{}, err
	}
	loaded.ReachabilityAnchorHeadCommitID = strings.TrimSpace(loaded.ReachabilityAnchorHeadCommitID)
	loaded.ReachabilityCursorCommitID = strings.TrimSpace(loaded.ReachabilityCursorCommitID)
	return loaded, nil
}

// insertPublishedBlockReferenceRepairLivenessCleanupFn writes the durable
// cleanup intent for one repair-owned pub:<repo:commit:fsID> BEFORE that pin
// is written (write-ahead), in the PREPARING state (armed = false) with the
// producer lease. It is keyed by the repair identity plus the visit's
// LivenessToken, so no other producer — a concurrent visit of the same row
// or a requeued visit of the same identity — can ever delete it. It carries
// NO TTL: the sweep bounds its life. Like
// renewPublishedBlockReferenceRepairLivenessFn, these primitives are no-ops
// without a session (unit tests); with a session every error is surfaced,
// and a missing token is an error (no pin is written).
var insertPublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
	if database == nil || database.Session() == nil {
		return nil
	}
	if strings.TrimSpace(repair.LivenessToken) == "" {
		return fmt.Errorf("cleanup intent requires the visit's liveness token")
	}
	return database.Session().Query(`
		INSERT INTO published_repair_liveness_cleanups (bucket, org_id, repo_id, commit_id, fs_id, producer_token, staged_block_ids, created_at, armed, lease_expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, false, ?)
	`, repair.Bucket, repair.OrgID, repair.RepoID, repair.CommitID, repair.FSID, repair.LivenessToken, repair.StagedBlockIDs, publishedBlockReferenceRepairNowFn().UTC(), repair.LivenessLeaseExpiresAt.UTC()).Exec()
}

// armPublishedBlockReferenceRepairLivenessCleanupFn marks the producer's
// fan-out as finished: from here the sweep may consume the intent as soon
// as the repair row is conclusively gone, because no further pin can be
// written under this token (and any late write of this token is shadowed
// by the lease timestamp). It is a SERIAL LWT conditioned on the intent
// still PREPARING, so it can never resurrect an intent the sweep already
// consumed (an unconditional UPDATE is an upsert), and it carries the full
// cleanup payload so a replica can never observe armed = true without the
// blocks and the lease the cleanup needs. Not applied means the witness is
// gone: the producer must compensate and stop.
var armPublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair) (bool, error) {
	if database == nil || database.Session() == nil {
		return true, nil
	}
	if strings.TrimSpace(repair.LivenessToken) == "" {
		return false, fmt.Errorf("cleanup intent requires the visit's liveness token")
	}
	return database.Session().Query(`
		UPDATE published_repair_liveness_cleanups
		SET armed = true, staged_block_ids = ?, lease_expires_at = ?
		WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ? AND producer_token = ?
		IF armed = false
	`, repair.StagedBlockIDs, repair.LivenessLeaseExpiresAt.UTC(), repair.Bucket, repair.OrgID, repair.RepoID, repair.CommitID, repair.FSID, repair.LivenessToken).
		SerialConsistency(gocql.Serial).
		MapScanCAS(map[string]interface{}{})
}

// extendPublishedBlockReferenceRepairLivenessCleanupFn renews the producer
// lease of a still-PREPARING intent so a long fan-out converges instead of
// being cut: a SERIAL LWT conditioned on the exact lease the producer holds.
// Not applied means the intent was consumed or is not the producer's any
// more: the producer must stop writing and compensate.
var extendPublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair, nextLease time.Time) (bool, error) {
	if database == nil || database.Session() == nil {
		return true, nil
	}
	if strings.TrimSpace(repair.LivenessToken) == "" {
		return false, fmt.Errorf("cleanup intent requires the visit's liveness token")
	}
	return database.Session().Query(`
		UPDATE published_repair_liveness_cleanups SET lease_expires_at = ?
		WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ? AND producer_token = ?
		IF armed = false AND lease_expires_at = ?
	`, nextLease.UTC(), repair.Bucket, repair.OrgID, repair.RepoID, repair.CommitID, repair.FSID, repair.LivenessToken, repair.LivenessLeaseExpiresAt.UTC()).
		SerialConsistency(gocql.Serial).
		MapScanCAS(map[string]interface{}{})
}

// deletePublishedBlockReferenceRepairLivenessCleanupFn deletes exactly the
// token the caller holds — never every intent of the identity.
var deletePublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
	if database == nil || database.Session() == nil {
		return nil
	}
	if strings.TrimSpace(repair.LivenessToken) == "" {
		return fmt.Errorf("cleanup intent requires the visit's liveness token")
	}
	return database.Session().Query(`
		DELETE FROM published_repair_liveness_cleanups
		WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ? AND producer_token = ?
	`, repair.Bucket, repair.OrgID, repair.RepoID, repair.CommitID, repair.FSID, repair.LivenessToken).Exec()
}

// listPublishedBlockReferenceRepairLivenessCleanupsForBucketFn returns one
// entry per (identity, token) with its producer fence, so the sweep can
// decide whether the intent may be consumed and delete exactly that row.
var listPublishedBlockReferenceRepairLivenessCleanupsForBucketFn = func(database *db.DB, bucket int) ([]publishedBlockReferenceRepair, error) {
	if database == nil || database.Session() == nil {
		return nil, nil
	}
	iter := database.Session().Query(`
		SELECT org_id, repo_id, commit_id, fs_id, producer_token, staged_block_ids, armed, lease_expires_at
		FROM published_repair_liveness_cleanups WHERE bucket = ?
	`, bucket).Iter()
	var intents []publishedBlockReferenceRepair
	var intent publishedBlockReferenceRepair
	for iter.Scan(&intent.OrgID, &intent.RepoID, &intent.CommitID, &intent.FSID, &intent.LivenessToken, &intent.StagedBlockIDs, &intent.LivenessArmed, &intent.LivenessLeaseExpiresAt) {
		intent.Bucket = bucket
		intents = append(intents, intent)
		intent = publishedBlockReferenceRepair{}
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return intents, nil
}

// publishedBlockReferenceRepairLivenessCleanupConsumable reports whether the
// sweep may act on a leftover intent: the producer has armed it (fan-out
// finished), or it is still preparing but its lease has expired, so the
// deadline-bounded fan-out can no longer write a pin under this token.
func publishedBlockReferenceRepairLivenessCleanupConsumable(intent publishedBlockReferenceRepair, now time.Time) bool {
	if intent.LivenessArmed {
		return true
	}
	if intent.LivenessLeaseExpiresAt.IsZero() {
		return false
	}
	return !now.Before(intent.LivenessLeaseExpiresAt)
}

// loadPublishedBlockReferenceRepairAuthorityFn is the destructive-authority
// read of the repair row: the only observation allowed to conclude that a
// repair identity is gone before its repair-owned pub: is removed. It is
// EACH_QUORUM, not the session's LOCAL_QUORUM: in a multi-DC deployment a
// cleanup intent can be visible in one DC before the repair row it belongs
// to has replicated there, and a local absence must never authorize
// removing liveness. An unavailable DC or timeout is an error (fail closed:
// keep the pin and the intent, retry on the next sweep).
var loadPublishedBlockReferenceRepairAuthorityFn = func(database *db.DB, repair publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
	if database == nil {
		return publishedBlockReferenceRepair{}, fmt.Errorf("database not available")
	}
	if database.Session() == nil {
		return publishedBlockReferenceRepair{}, fmt.Errorf("database session not available")
	}
	loaded := publishedBlockReferenceRepair{
		Bucket: repair.Bucket,
	}
	err := database.Session().Query(`
		SELECT org_id, repo_id, commit_id, fs_id, staged_block_ids, created_at, lease_expires_at, reachability_anchor_head_commit_id, reachability_cursor_commit_id, reachability_anchor_exhausted
		FROM published_block_reference_repairs
		WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
	`, repair.Bucket, repair.OrgID, repair.RepoID, repair.CommitID, repair.FSID).
		Consistency(gocql.EachQuorum).
		Scan(&loaded.OrgID, &loaded.RepoID, &loaded.CommitID, &loaded.FSID, &loaded.StagedBlockIDs, &loaded.CreatedAt, &loaded.LeaseExpiresAt, &loaded.ReachabilityAnchorHeadCommitID, &loaded.ReachabilityCursorCommitID, &loaded.ReachabilityAnchorExhausted)
	if err != nil {
		return publishedBlockReferenceRepair{}, err
	}
	loaded.ReachabilityAnchorHeadCommitID = strings.TrimSpace(loaded.ReachabilityAnchorHeadCommitID)
	loaded.ReachabilityCursorCommitID = strings.TrimSpace(loaded.ReachabilityCursorCommitID)
	return loaded, nil
}

// publishedBlockReferenceRepairGoneForCleanup decides whether the repair
// identity is conclusively gone before its repair-owned pub: is removed, for
// the visit and for the sweep alike. The session-consistency read is only
// ever a reason to RETAIN: if it still sees the row, nothing is removed and
// no cross-DC read is spent. A local absence is never authority — the
// visit's earlier observation may have been coordinated in another DC by
// the DC-aware host policy, a local quorum may not have read-repaired the
// row, and the sweep has no prior observation at all — so it is escalated
// to the EACH_QUORUM authority read, and only NotFound there (or a
// progress-only residue) reports gone. An unavailable DC or timeout is an
// error and the caller keeps the pin and the intent. With another DC down
// the common path is unaffected: the local read says pending and the visit
// carries on (its EACH_QUORUM ancestry reads fail closed on their own).
func publishedBlockReferenceRepairGoneForCleanup(database *db.DB, repair publishedBlockReferenceRepair) (bool, error) {
	pending, err := publishedBlockReferenceRepairStillPending(database, repair)
	if err != nil {
		return false, err
	}
	if pending {
		return false, nil
	}
	loaded, err := loadPublishedBlockReferenceRepairAuthorityFn(database, repair)
	if errors.Is(err, gocql.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		if database == nil {
			return false, nil
		}
		return false, fmt.Errorf("confirm repair row absence for fs_object %s at EACH_QUORUM: %w", repair.FSID, err)
	}
	return publishedBlockReferenceRepairIsProgressOnly(loaded), nil
}

func publishedBlockReferenceRepairProgressGeneration(repair publishedBlockReferenceRepair) (time.Time, error) {
	if repair.CreatedAt.IsZero() {
		return time.Time{}, fmt.Errorf("reachability progress requires the loaded repair created_at")
	}
	return repair.CreatedAt, nil
}

// persistPublishedBlockReferenceRepairAnchorFn records the first SERIAL HEAD
// observation. The LWT is monotonic progress only: it never authorizes cleanup.
// created_at = <loaded> binds the CAS to the hydrated TIMESTAMP. Existence
// alone (created_at != null) would allow a stale worker to mutate a DELETE +
// requeue whose Cassandra timestamp differs. CQL TIMESTAMP is millisecond
// precision (`ISSUE-PUBLISH-REPAIR-PROGRESS-PAXOS-DOMAIN-01`).
var persistPublishedBlockReferenceRepairAnchorFn = func(database *db.DB, repair publishedBlockReferenceRepair, anchorCommitID string) (bool, error) {
	if database == nil {
		return false, fmt.Errorf("database not available")
	}
	createdAt, err := publishedBlockReferenceRepairProgressGeneration(repair)
	if err != nil {
		return false, err
	}
	if database.Session() == nil {
		return false, fmt.Errorf("database session not available")
	}
	anchorCommitID = strings.TrimSpace(anchorCommitID)
	if anchorCommitID == "" {
		return false, fmt.Errorf("reachability anchor HEAD is required")
	}
	applied, err := database.Session().Query(`
		UPDATE published_block_reference_repairs
		SET reachability_anchor_head_commit_id = ?, reachability_cursor_commit_id = ?, reachability_anchor_exhausted = false
		WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
		IF created_at = ? AND reachability_anchor_head_commit_id = null
	`, anchorCommitID, anchorCommitID, repair.Bucket, repair.OrgID, repair.RepoID, repair.CommitID, repair.FSID, createdAt).
		SerialConsistency(gocql.Serial).
		MapScanCAS(map[string]interface{}{})
	return applied, err
}

// advancePublishedBlockReferenceRepairCursorFn moves the walk cursor forward
// only when the caller still holds the expected (anchor, cursor) snapshot.
var advancePublishedBlockReferenceRepairCursorFn = func(database *db.DB, repair publishedBlockReferenceRepair, expectedCursor, nextCursor string) (bool, error) {
	if database == nil {
		return false, fmt.Errorf("database not available")
	}
	createdAt, err := publishedBlockReferenceRepairProgressGeneration(repair)
	if err != nil {
		return false, err
	}
	if database.Session() == nil {
		return false, fmt.Errorf("database session not available")
	}
	expectedAnchor := strings.TrimSpace(repair.ReachabilityAnchorHeadCommitID)
	expectedCursor = strings.TrimSpace(expectedCursor)
	nextCursor = strings.TrimSpace(nextCursor)
	if expectedAnchor == "" || nextCursor == "" {
		return false, fmt.Errorf("reachability cursor advance requires an anchor and next cursor")
	}
	var applied bool
	if expectedCursor == "" {
		applied, err = database.Session().Query(`
			UPDATE published_block_reference_repairs
			SET reachability_cursor_commit_id = ?
			WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
			IF created_at = ? AND reachability_anchor_head_commit_id = ? AND reachability_cursor_commit_id = null AND reachability_anchor_exhausted != true
		`, nextCursor, repair.Bucket, repair.OrgID, repair.RepoID, repair.CommitID, repair.FSID, createdAt, expectedAnchor).
			SerialConsistency(gocql.Serial).
			MapScanCAS(map[string]interface{}{})
	} else {
		applied, err = database.Session().Query(`
			UPDATE published_block_reference_repairs
			SET reachability_cursor_commit_id = ?
			WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
			IF created_at = ? AND reachability_anchor_head_commit_id = ? AND reachability_cursor_commit_id = ? AND reachability_anchor_exhausted != true
		`, nextCursor, repair.Bucket, repair.OrgID, repair.RepoID, repair.CommitID, repair.FSID, createdAt, expectedAnchor, expectedCursor).
			SerialConsistency(gocql.Serial).
			MapScanCAS(map[string]interface{}{})
	}
	return applied, err
}

// markPublishedBlockReferenceRepairAnchorExhaustedFn records that this SERIAL
// snapshot was walked to genesis without the target. The LWT does not use the
// 30s ancestry context, so a later HEAD timeout cannot erase that work.
// Cassandra BOOLEAN unset is null, which is not equal to false, so the
// unexhausted predicate is `exhausted != true`.
var markPublishedBlockReferenceRepairAnchorExhaustedFn = func(database *db.DB, repair publishedBlockReferenceRepair, expectedCursor string) (bool, error) {
	if database == nil {
		return false, fmt.Errorf("database not available")
	}
	createdAt, err := publishedBlockReferenceRepairProgressGeneration(repair)
	if err != nil {
		return false, err
	}
	if database.Session() == nil {
		return false, fmt.Errorf("database session not available")
	}
	expectedAnchor := strings.TrimSpace(repair.ReachabilityAnchorHeadCommitID)
	expectedCursor = strings.TrimSpace(expectedCursor)
	if expectedAnchor == "" {
		return false, fmt.Errorf("reachability genesis exhaustion requires an anchor")
	}
	var applied bool
	if expectedCursor == "" {
		applied, err = database.Session().Query(`
			UPDATE published_block_reference_repairs
			SET reachability_anchor_exhausted = true
			WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
			IF created_at = ? AND reachability_anchor_head_commit_id = ? AND reachability_cursor_commit_id = null AND reachability_anchor_exhausted != true
		`, repair.Bucket, repair.OrgID, repair.RepoID, repair.CommitID, repair.FSID, createdAt, expectedAnchor).
			SerialConsistency(gocql.Serial).
			MapScanCAS(map[string]interface{}{})
	} else {
		applied, err = database.Session().Query(`
			UPDATE published_block_reference_repairs
			SET reachability_anchor_exhausted = true
			WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
			IF created_at = ? AND reachability_anchor_head_commit_id = ? AND reachability_cursor_commit_id = ? AND reachability_anchor_exhausted != true
		`, repair.Bucket, repair.OrgID, repair.RepoID, repair.CommitID, repair.FSID, createdAt, expectedAnchor, expectedCursor).
			SerialConsistency(gocql.Serial).
			MapScanCAS(map[string]interface{}{})
	}
	return applied, err
}

// replacePublishedBlockReferenceRepairAnchorFn replaces an exhausted SERIAL
// HEAD snapshot after a clean walk to genesis. Timeout, bound, EACH_QUORUM
// error, cycle, and malformed ancestry must not use this path: those keep the
// original anchor so a moving HEAD cannot restart work. The LWT is still
// progress only; created_at = <loaded> binds the CAS to the hydrated TIMESTAMP.
var replacePublishedBlockReferenceRepairAnchorFn = func(database *db.DB, repair publishedBlockReferenceRepair, expectedCursor, nextHEAD string) (bool, error) {
	if database == nil {
		return false, fmt.Errorf("database not available")
	}
	createdAt, err := publishedBlockReferenceRepairProgressGeneration(repair)
	if err != nil {
		return false, err
	}
	if database.Session() == nil {
		return false, fmt.Errorf("database session not available")
	}
	expectedAnchor := strings.TrimSpace(repair.ReachabilityAnchorHeadCommitID)
	expectedCursor = strings.TrimSpace(expectedCursor)
	nextHEAD = strings.TrimSpace(nextHEAD)
	if expectedAnchor == "" || nextHEAD == "" {
		return false, fmt.Errorf("reachability re-anchor requires the exhausted anchor and a newer HEAD")
	}
	var applied bool
	if expectedCursor == "" {
		applied, err = database.Session().Query(`
			UPDATE published_block_reference_repairs
			SET reachability_anchor_head_commit_id = ?, reachability_cursor_commit_id = ?, reachability_anchor_exhausted = false
			WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
			IF created_at = ? AND reachability_anchor_head_commit_id = ? AND reachability_cursor_commit_id = null AND reachability_anchor_exhausted = true
		`, nextHEAD, nextHEAD, repair.Bucket, repair.OrgID, repair.RepoID, repair.CommitID, repair.FSID, createdAt, expectedAnchor).
			SerialConsistency(gocql.Serial).
			MapScanCAS(map[string]interface{}{})
	} else {
		applied, err = database.Session().Query(`
			UPDATE published_block_reference_repairs
			SET reachability_anchor_head_commit_id = ?, reachability_cursor_commit_id = ?, reachability_anchor_exhausted = false
			WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
			IF created_at = ? AND reachability_anchor_head_commit_id = ? AND reachability_cursor_commit_id = ? AND reachability_anchor_exhausted = true
		`, nextHEAD, nextHEAD, repair.Bucket, repair.OrgID, repair.RepoID, repair.CommitID, repair.FSID, createdAt, expectedAnchor, expectedCursor).
			SerialConsistency(gocql.Serial).
			MapScanCAS(map[string]interface{}{})
	}
	return applied, err
}

// errPublishedBlockReferenceRepairLivenessWitnessLost is returned by the
// fan-out when the producer's cleanup intent is no longer its own (consumed
// by the sweep, or not PREPARING any more): the producer must stop writing.
var errPublishedBlockReferenceRepairLivenessWitnessLost = errors.New("repair-owned liveness cleanup intent is no longer held by this producer")

// publishedBlockReferenceRepairLeaseTimestamp is the write timestamp every
// pin of a producer carries and the tombstone timestamp every cleanup of
// that producer uses: microseconds of its lease expiry. See
// db.AddPublishAttemptReferenceAt for why that fences late writes.
func publishedBlockReferenceRepairLeaseTimestamp(lease time.Time) int64 {
	return publishedBlockReferenceRepairLeaseInstant(lease).UnixMicro()
}

// publishedBlockReferenceRepairLeaseInstant is the lease as Cassandra stores
// it (TIMESTAMP has millisecond precision): every lease the producer holds
// in memory, compares in a CAS, or derives a timestamp from is truncated the
// same way, so the stored lease and the pin timestamps always agree.
func publishedBlockReferenceRepairLeaseInstant(lease time.Time) time.Time {
	return lease.UTC().Truncate(time.Millisecond)
}

// writePublishedBlockReferenceRepairLivenessPinFn writes one timestamped
// repair-owned pin; hookable so the fan-out fence can be unit-tested, and a
// no-op without a session like the other liveness primitives.
var writePublishedBlockReferenceRepairLivenessPinFn = func(database *db.DB, orgID, repoID, attemptID, blockID string, timestampMicros int64) error {
	if database == nil || database.Session() == nil {
		return nil
	}
	return db.AddPublishAttemptReferenceAt(database, orgID, repoID, attemptID, blockID, timestampMicros)
}

// renewPublishedBlockReferenceRepairLivenessFn is the producer's per-block
// pub: fan-out. Every write carries USING TIMESTAMP = the producer's current
// lease, so any write of this producer that lands after its cleanup (a
// paused process, an in-flight request) is shadowed by the cleanup tombstone
// written at that same timestamp. Before each block it renews the lease
// through a conditional LWT once less than two skews remain, so an
// arbitrarily long fan-out converges (main had no cut here either) while the
// lease still bounds how far ahead any pin's timestamp can be; a producer
// that cannot renew (LWT not applied: its witness was consumed) stops
// without writing. The renewed lease is written back into repair.
var renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair *publishedBlockReferenceRepair) error {
	if database == nil {
		return nil
	}
	if !shouldQueuePublishedBlockReferenceRepair(repair.FSID, repair.StagedBlockIDs) {
		return nil
	}
	attemptID := publishedBlockReferenceRepairLivenessAttemptID(*repair)
	for _, blockID := range db.NormalizeBlockIDs(repair.StagedBlockIDs) {
		now := publishedBlockReferenceRepairNowFn().UTC()
		if !now.Add(2 * publishedBlockReferenceRepairLivenessLeaseSkew).Before(repair.LivenessLeaseExpiresAt) {
			nextLease := publishedBlockReferenceRepairLeaseInstant(now.Add(publishedBlockReferenceRepairLivenessLease))
			applied, err := extendPublishedBlockReferenceRepairLivenessCleanupFn(database, *repair, nextLease)
			if err != nil {
				return fmt.Errorf("extend repair-owned liveness lease for fs_object %s: %w", repair.FSID, err)
			}
			if !applied {
				return errPublishedBlockReferenceRepairLivenessWitnessLost
			}
			repair.LivenessLeaseExpiresAt = nextLease
		}
		if err := writePublishedBlockReferenceRepairLivenessPinFn(database, repair.OrgID, repair.RepoID, attemptID, blockID, publishedBlockReferenceRepairLeaseTimestamp(repair.LivenessLeaseExpiresAt)); err != nil {
			return err
		}
	}
	return nil
}

// publishedBlockReferenceRepairIsProgressOnly reports a row that carries no
// ordinary queue cells. The queue INSERT writes created_at, lease_expires_at,
// and staged_block_ids in one atomic mutation, so a listed row without all of
// them can only be the residue of a progress LWT that raced the ordinary
// settlement DELETE (ISSUE-PUBLISH-REPAIR-PROGRESS-PAXOS-DOMAIN-01). Such a
// row is never actionable: it has no staged blocks to renew or promote.
func publishedBlockReferenceRepairIsProgressOnly(repair publishedBlockReferenceRepair) bool {
	return repair.CreatedAt.IsZero() && repair.LeaseExpiresAt.IsZero() && len(repair.StagedBlockIDs) == 0
}

// reapPublishedBlockReferenceRepairProgressOnlyRowFn tombstones only the
// three reachability cells of a progress-only residue row. It must never
// delete the whole row: the requeue INSERT is an ordinary write outside Paxos,
// so this LWT can evaluate `created_at = null` against a quorum that has not
// yet seen an already-acknowledged requeue, and a row tombstone carrying the
// later ballot timestamp would then shadow that durable repair. Cell
// tombstones on columns the ordinary INSERT never writes cannot shadow
// anything it wrote; the worst case of that race is a fresh row losing
// progress it did not have (a replay, the already accepted class). A residue
// row has no row marker, so removing its cells removes it from the listing.
// The condition still avoids touching live progress in the common case. This
// is not settlement and not cleanup authority.
var reapPublishedBlockReferenceRepairProgressOnlyRowFn = func(database *db.DB, repair publishedBlockReferenceRepair) (bool, error) {
	if database == nil {
		return false, fmt.Errorf("database not available")
	}
	if database.Session() == nil {
		return false, fmt.Errorf("database session not available")
	}
	applied, err := database.Session().Query(`
		DELETE reachability_anchor_head_commit_id, reachability_cursor_commit_id, reachability_anchor_exhausted
		FROM published_block_reference_repairs
		WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
		IF created_at = null AND lease_expires_at = null
	`, repair.Bucket, repair.OrgID, repair.RepoID, repair.CommitID, repair.FSID).
		SerialConsistency(gocql.Serial).
		MapScanCAS(map[string]interface{}{})
	if err != nil {
		return false, err
	}
	publishedBlockReferenceRepairNextRetryAt.Delete(publishedBlockReferenceRepairRetryKey(repair))
	return applied, nil
}

var listPendingPublishedFSObjectOwnersByDayFn = func(database *db.DB, day time.Time, bucket int) ([]db.PendingPublishedFSObjectOwner, error) {
	if database == nil {
		return nil, fmt.Errorf("database not available")
	}
	return database.ListPendingPublishedFSObjectOwnersByDay(day, bucket)
}

var loadPendingPublishedFSObjectOwnerFn = func(database *db.DB, repoID, fsID, ownerID string) (db.PendingPublishedFSObjectOwner, error) {
	if database == nil {
		return db.PendingPublishedFSObjectOwner{}, fmt.Errorf("database not available")
	}
	return database.LoadPendingPublishedFSObjectOwner(repoID, fsID, ownerID)
}

// The shared repair/recovery classifier owns this authority read. It requests
// the SERIAL domain for the org-scoped canonical HEAD observation; this is
// recovery evidence, not a durable global-negative witness. Normal writer
// publication paths do not add this read or enter the SERIAL domain.
var publishedBlockReferenceRepairHeadCommitFn = func(ctx context.Context, database *db.DB, orgID, repoID string) (string, error) {
	if database == nil {
		return "", fmt.Errorf("database not available")
	}
	var headCommitID string
	err := database.Session().Query(`
		SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, repoID).Consistency(gocql.Serial).WithContext(ctx).Scan(&headCommitID)
	if err != nil {
		return "", fmt.Errorf("lookup canonical HEAD for repo %s: %w", repoID, err)
	}
	if strings.TrimSpace(headCommitID) == "" {
		return "", fmt.Errorf("canonical HEAD for repo %s is empty", repoID)
	}
	return strings.TrimSpace(headCommitID), nil
}

// Commit rows are immutable ordinary writes. EACH_QUORUM requires a response
// quorum in every replica DC, so classification does not rely on one DC's
// LOCAL_QUORUM view. It does not itself create a durable negative witness;
// missing or failed observations remain UNKNOWN. The bounded context applies
// to each cold-path read.
var publishedBlockReferenceRepairCommitParentFn = func(ctx context.Context, database *db.DB, repoID, commitID string) (string, error) {
	if database == nil {
		return "", fmt.Errorf("database not available")
	}
	var parentCommitID string
	err := database.Session().Query(`
		SELECT parent_id FROM commits WHERE library_id = ? AND commit_id = ?
	`, repoID, commitID).Consistency(gocql.EachQuorum).WithContext(ctx).Scan(&parentCommitID)
	if err != nil {
		return "", err
	}
	return parentCommitID, nil
}

// publishedCommitReachabilityWalk is one bounded ancestry observation.
// NextCursor is the next commit that has not yet been validated. It is set
// after a safely completed prefix: full maxNodes exhaustion, a timeout, or a
// parent-read error on a later node. Cycles and natural genesis leave it empty
// so the durable cursor does not become negative authority. It never skips an
// unread commit, and it never advances past a row whose read failed.
type publishedCommitReachabilityWalk struct {
	Outcome    publishedBlockReferenceRepairCommitOutcome
	NextCursor string
}

// publishedCommitReachabilityUnknownProgress retains UNKNOWN. nextUnread is
// persisted only when the walk has already left startCommitID, so a failure on
// the first node cannot look like progress.
func publishedCommitReachabilityUnknownProgress(startCommitID, nextUnreadCommitID string) publishedCommitReachabilityWalk {
	walk := publishedCommitReachabilityWalk{Outcome: publishedBlockReferenceRepairCommitUnknown}
	startCommitID = strings.TrimSpace(startCommitID)
	nextUnreadCommitID = strings.TrimSpace(nextUnreadCommitID)
	if nextUnreadCommitID != "" && nextUnreadCommitID != startCommitID {
		walk.NextCursor = nextUnreadCommitID
	}
	return walk
}

// walkPublishedCommitReachability answers only whether targetCommitID is
// reachable from startCommitID. It validates every visited commit row,
// including the target, and performs at most maxNodes sequential parent reads.
// Natural exhaustion is still UNKNOWN: without a durable global loser witness,
// absence from this observation cannot authorize cleanup.
func walkPublishedCommitReachability(ctx context.Context, targetCommitID, startCommitID string, maxNodes int, parentLookup func(context.Context, string) (string, error)) (publishedCommitReachabilityWalk, error) {
	return walkPublishedCommitReachabilitySeeded(ctx, targetCommitID, startCommitID, nil, maxNodes, parentLookup)
}

// walkPublishedCommitReachabilitySeeded is walkPublishedCommitReachability
// with commits already known to precede startCommitID in the anchored chain.
// A resumed chunk starts with an empty visited set, so a cycle longer than
// one chunk would otherwise rotate the cursor forever. Seeding the anchored
// HEAD makes any chain that leads back to HEAD a detected cycle: HEAD cannot
// be its own ancestor. This covers only cycles that return to the anchored
// HEAD; a corrupt cycle longer than one chunk that lies entirely below HEAD
// still rotates the cursor (UNKNOWN, retained, never cleanup authority;
// ISSUE-PUBLISH-REPAIR-CROSS-CHUNK-CYCLE-01). Seeds equal to startCommitID
// are ignored so a first chunk never reports itself as a cycle.
func walkPublishedCommitReachabilitySeeded(ctx context.Context, targetCommitID, startCommitID string, seedCommitIDs []string, maxNodes int, parentLookup func(context.Context, string) (string, error)) (publishedCommitReachabilityWalk, error) {
	targetCommitID = strings.TrimSpace(targetCommitID)
	startCommitID = strings.TrimSpace(startCommitID)
	unknown := publishedCommitReachabilityWalk{Outcome: publishedBlockReferenceRepairCommitUnknown}
	if targetCommitID == "" || startCommitID == "" {
		return unknown, fmt.Errorf("target commit and canonical HEAD are required to classify publication reachability")
	}
	if ctx == nil {
		return unknown, fmt.Errorf("reachability context is required")
	}
	if maxNodes <= 0 {
		return unknown, fmt.Errorf("reachability ancestry bound must be positive")
	}
	if parentLookup == nil {
		return unknown, fmt.Errorf("reachability parent lookup is required")
	}

	visited := make(map[string]struct{}, maxNodes)
	for _, seed := range seedCommitIDs {
		seed = strings.TrimSpace(seed)
		if seed != "" && seed != startCommitID {
			visited[seed] = struct{}{}
		}
	}
	currentCommitID := startCommitID
	for nodesRead := 0; nodesRead < maxNodes; nodesRead++ {
		if err := ctx.Err(); err != nil {
			return publishedCommitReachabilityUnknownProgress(startCommitID, currentCommitID), fmt.Errorf("reachability observation interrupted: %w", err)
		}
		if _, seen := visited[currentCommitID]; seen {
			return unknown, fmt.Errorf("detected commit ancestry cycle at %s", currentCommitID)
		}
		visited[currentCommitID] = struct{}{}

		parentCommitID, err := parentLookup(ctx, currentCommitID)
		if err != nil {
			return publishedCommitReachabilityUnknownProgress(startCommitID, currentCommitID), fmt.Errorf("lookup parent for commit %s: %w", currentCommitID, err)
		}

		rawParentCommitID := parentCommitID
		parentCommitID = strings.TrimSpace(rawParentCommitID)
		if rawParentCommitID != "" && parentCommitID == "" {
			return unknown, fmt.Errorf("malformed empty parent for commit %s", currentCommitID)
		}
		if currentCommitID == targetCommitID {
			// The target row itself is part of the evidence. A parent that
			// points back into the observed prefix (including the target) is
			// corrupt ancestry, not a positive reachability certificate.
			if parentCommitID != "" {
				if _, seen := visited[parentCommitID]; seen {
					return unknown, fmt.Errorf("detected commit ancestry cycle at %s", parentCommitID)
				}
			}
			return publishedCommitReachabilityWalk{Outcome: publishedBlockReferenceRepairCommitReachable}, nil
		}
		if parentCommitID == "" {
			// Clean genesis: the anchored chain is exhausted without the
			// target. Classify may re-observe SERIAL HEAD after this; the
			// walk itself still has no negative authority.
			return publishedCommitReachabilityWalk{Outcome: publishedBlockReferenceRepairCommitUnknown}, nil
		}
		currentCommitID = parentCommitID
	}

	return publishedCommitReachabilityWalk{
		Outcome:    publishedBlockReferenceRepairCommitUnknown,
		NextCursor: currentCommitID,
	}, fmt.Errorf("commit ancestry walk reached %d-node limit from HEAD %s toward target %s", maxNodes, startCommitID, targetCommitID)
}

// classifyPublishedCommitReachability answers only whether targetCommitID is
// reachable from one canonical HEAD observation. It validates every visited
// commit row, including the target, and performs at most maxNodes sequential
// parent reads. Natural exhaustion is still UNKNOWN: without a durable global
// loser witness, absence from this observation cannot authorize cleanup.
func classifyPublishedCommitReachability(ctx context.Context, targetCommitID, headCommitID string, maxNodes int, parentLookup func(context.Context, string) (string, error)) (publishedBlockReferenceRepairCommitOutcome, error) {
	progress, err := walkPublishedCommitReachability(ctx, targetCommitID, headCommitID, maxNodes, parentLookup)
	return progress.Outcome, err
}

func classifyPublishedBlockReferenceRepairCommitOutcome(ctx context.Context, commitID, headCommitID string, parentLookup func(context.Context, string) (string, error)) (publishedBlockReferenceRepairCommitOutcome, error) {
	commitID = strings.TrimSpace(commitID)
	headCommitID = strings.TrimSpace(headCommitID)
	if commitID == "" || headCommitID == "" {
		return publishedBlockReferenceRepairCommitUnknown, fmt.Errorf("commit and canonical HEAD are required to settle publication")
	}
	return classifyPublishedCommitReachability(ctx, commitID, headCommitID, publishedCommitReachabilityMaxNodes, parentLookup)
}

func classifyPublishedBlockReferenceRepairCommitFromStore(database *db.DB, orgID, repoID, commitID string) (publishedBlockReferenceRepairCommitOutcome, error) {
	ctx, cancel := context.WithTimeout(context.Background(), publishedCommitReachabilityTimeout)
	defer cancel()

	headCommitID, err := publishedBlockReferenceRepairHeadCommitFn(ctx, database, orgID, repoID)
	if err != nil {
		return publishedBlockReferenceRepairCommitUnknown, fmt.Errorf("lookup current head for repo %s: %w", repoID, err)
	}
	return classifyPublishedBlockReferenceRepairCommitOutcome(ctx, commitID, headCommitID, func(ctx context.Context, currentCommitID string) (string, error) {
		parentCommitID, err := publishedBlockReferenceRepairCommitParentFn(ctx, database, repoID, currentCommitID)
		if err != nil {
			return "", fmt.Errorf("lookup parent for commit %s: %w", currentCommitID, err)
		}
		return parentCommitID, nil
	})
}

// mergePublishedBlockReferenceRepairProgress overlays what Cassandra returned
// for this identity onto the caller's copy. The loaded ordinary cells are
// authoritative: a listed or request-supplied copy may be older than the row,
// and keeping its staged_block_ids/created_at when the row no longer carries
// them would let a progress-only residue impersonate a live repair. Callers
// must load through loadLivePublishedBlockReferenceRepair so residue never
// reaches this merge.
func mergePublishedBlockReferenceRepairProgress(dst, src publishedBlockReferenceRepair) publishedBlockReferenceRepair {
	dst.ReachabilityAnchorHeadCommitID = strings.TrimSpace(src.ReachabilityAnchorHeadCommitID)
	dst.ReachabilityCursorCommitID = strings.TrimSpace(src.ReachabilityCursorCommitID)
	dst.ReachabilityAnchorExhausted = src.ReachabilityAnchorExhausted
	dst.StagedBlockIDs = append([]string(nil), src.StagedBlockIDs...)
	dst.CreatedAt = src.CreatedAt
	dst.LeaseExpiresAt = src.LeaseExpiresAt
	if strings.TrimSpace(src.OrgID) != "" {
		dst.OrgID = src.OrgID
	}
	if strings.TrimSpace(src.RepoID) != "" {
		dst.RepoID = src.RepoID
	}
	if strings.TrimSpace(src.CommitID) != "" {
		dst.CommitID = src.CommitID
	}
	if strings.TrimSpace(src.FSID) != "" {
		dst.FSID = src.FSID
	}
	return dst
}

// loadLivePublishedBlockReferenceRepair is the only way the repair path reads
// its durable row. A missing row and a progress-only residue (primary key +
// reachability cells, no ordinary queue cells) are both
// errPublishedBlockReferenceRepairGone: a repair that was listed live can be
// settled by another worker and then survive as residue before this worker
// hydrates or revalidates it, and that residue must not be classified, must
// not renew pub:, and must not be promoted from the stale listed copy.
func loadLivePublishedBlockReferenceRepair(database *db.DB, repair publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
	loaded, err := loadPublishedBlockReferenceRepairFn(database, repair)
	if errors.Is(err, gocql.ErrNotFound) {
		return publishedBlockReferenceRepair{}, errPublishedBlockReferenceRepairGone
	}
	if err != nil {
		return publishedBlockReferenceRepair{}, err
	}
	if publishedBlockReferenceRepairIsProgressOnly(loaded) {
		return publishedBlockReferenceRepair{}, errPublishedBlockReferenceRepairGone
	}
	return loaded, nil
}

func hydratePublishedBlockReferenceRepair(database *db.DB, repair publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
	loaded, err := loadLivePublishedBlockReferenceRepair(database, repair)
	if errors.Is(err, errPublishedBlockReferenceRepairGone) {
		return publishedBlockReferenceRepair{}, err
	}
	if err != nil {
		if database == nil {
			return repair, nil
		}
		return publishedBlockReferenceRepair{}, err
	}
	return mergePublishedBlockReferenceRepairProgress(repair, loaded), nil
}

func publishedBlockReferenceRepairStillPending(database *db.DB, repair publishedBlockReferenceRepair) (bool, error) {
	_, err := loadLivePublishedBlockReferenceRepair(database, repair)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, errPublishedBlockReferenceRepairGone) {
		return false, nil
	}
	if database == nil {
		return true, nil
	}
	return false, err
}

// renewPublishedBlockReferenceRepairLivenessIfPending renews temporary liveness
// owned by this repair row (pub:<repo:commit:fsID>), not the original Sync
// pub:<publishAttemptID> and not v2's shared pub:<commitID>. It runs once per
// visit, after hydrate and before the bounded classifier
// (ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01). It does not renew after
// the durable row is gone: a row already settled before the write is
// reported as errPublishedBlockReferenceRepairGone without any pub: write,
// and a row settled during the write is compensated by removing that
// identity before reporting Gone. The per-block fan-out is not one atomic
// write: a failure part-way may have written some refs, so the failure path
// runs the same gone-check and compensation before returning the renewal
// error. A concurrent settler of this same row can still remove pub: then
// delete the row after this renewal
// (ISSUE-PUBLISH-REPAIR-OWNED-PUB-CLEANUP-RACE-01).
//
// The visit is one renewal producer. It mints a LivenessToken, records its
// cleanup intent PREPARING with a lease, runs the fan-out with every pin
// timestamped at that (renewable) lease, then ARMs the intent with a
// conditional LWT. The sweep consumes an intent only once armed or once the
// lease has expired, and every cleanup of this producer's pins is a
// tombstone at the lease timestamp, so a pin of this producer can never
// outlive its witness — not even a write that lands late. The token and
// lease are written back into repair for the rest of the visit.
func renewPublishedBlockReferenceRepairLivenessIfPending(database *db.DB, repair *publishedBlockReferenceRepair) error {
	pending, err := publishedBlockReferenceRepairStillPending(database, *repair)
	if err != nil {
		return err
	}
	if !pending {
		return errPublishedBlockReferenceRepairGone
	}
	repair.LivenessToken = uuid.NewString()
	repair.LivenessArmed = false
	repair.LivenessLeaseExpiresAt = publishedBlockReferenceRepairLeaseInstant(publishedBlockReferenceRepairNowFn().Add(publishedBlockReferenceRepairLivenessLease))
	// Write-ahead cleanup intent: once a pin exists for this identity, the
	// durable repair row may be cleared by a writer at any time, and every
	// in-visit compensation after that can fail (read error, per-block DELETE
	// fan-out, process loss). The intent survives all of those and is
	// processed by the sweep; if it cannot be written, no pin is written.
	if err := insertPublishedBlockReferenceRepairLivenessCleanupFn(database, *repair); err != nil {
		return fmt.Errorf("record repair-owned liveness cleanup intent for fs_object %s: %w", repair.FSID, err)
	}
	renewErr := renewPublishedBlockReferenceRepairLivenessFn(database, repair)
	if errors.Is(renewErr, errPublishedBlockReferenceRepairLivenessWitnessLost) {
		// The sweep consumed this producer's witness mid fan-out: the row was
		// conclusively gone. Its tombstones (at the lease timestamp) already
		// shadow every pin this producer wrote or will write; compensating
		// here is the producer's own idempotent share of that.
		if err := removePublishedBlockReferenceRepairOwnedPubFn(database, *repair); err != nil {
			return errors.Join(renewErr, err)
		}
		return errPublishedBlockReferenceRepairGone
	}
	// The fan-out is over (finished or failed): no further pin can be written
	// under this token, so the witness may now be consumed by whoever finds
	// the row gone. ARM is conditional on the intent still preparing; not
	// applied means the sweep consumed it (row gone) — never resurrect it,
	// compensate and stop. An arm that errors leaves the intent preparing;
	// the lease still bounds it.
	armed, err := armPublishedBlockReferenceRepairLivenessCleanupFn(database, *repair)
	if err != nil {
		return errors.Join(renewErr, fmt.Errorf("arm repair-owned liveness cleanup intent for fs_object %s: %w", repair.FSID, err))
	}
	if !armed {
		if err := removePublishedBlockReferenceRepairOwnedPubFn(database, *repair); err != nil {
			return errors.Join(renewErr, err)
		}
		return errPublishedBlockReferenceRepairGone
	}
	repair.LivenessArmed = true
	gone, compensateErr := compensatePublishedBlockReferenceRepairLivenessIfGone(database, *repair)
	if compensateErr != nil {
		return errors.Join(renewErr, compensateErr)
	}
	if gone {
		return errPublishedBlockReferenceRepairGone
	}
	return renewErr
}

// compensatePublishedBlockReferenceRepairLivenessIfGone re-reads the durable
// row and, when it is no longer pending, removes exactly the repair-owned
// pub:<repo:commit:fsID> this visit may have written (never pub:<commitID>,
// never a sibling repair's identity) and then its cleanup intent. It reports
// whether the row was gone. A row observed pending (an ordinary requeue of
// the same identity) is left alone together with its intent; the read and
// the DELETE are not atomic, so a requeue landing between them can lose this
// repair-owned identity — the writer-owned publication pin keeps protecting
// it and the requeued row renews on its own visit
// (ISSUE-PUBLISH-REPAIR-OWNED-PUB-CLEANUP-RACE-01).
//
// The absence that authorizes the DELETE is publishedBlockReferenceRepairGoneForCleanup
// (local read retains, EACH_QUORUM decides absence). Any failure here (read
// error or unavailable DC, per-block DELETE fan-out, process loss) leaves
// the write-ahead intent in place, and the sweep runs this same function
// against it until it succeeds. The intent deleted is exactly the token
// held by the caller, so it can never be another producer's witness.
func compensatePublishedBlockReferenceRepairLivenessIfGone(database *db.DB, repair publishedBlockReferenceRepair) (bool, error) {
	isGone, err := publishedBlockReferenceRepairGoneForCleanup(database, repair)
	if err != nil {
		return false, err
	}
	if !isGone {
		return false, nil
	}
	if err := removePublishedBlockReferenceRepairOwnedPubFn(database, repair); err != nil {
		return true, fmt.Errorf("remove repair-owned publish-attempt liveness for fs_object %s after its repair row was gone: %w", repair.FSID, err)
	}
	if err := deletePublishedBlockReferenceRepairLivenessCleanupFn(database, repair); err != nil {
		return true, fmt.Errorf("delete repair-owned liveness cleanup intent for fs_object %s: %w", repair.FSID, err)
	}
	return true, nil
}

// removePublishedBlockReferenceRepairOwnedPubFn removes the repair-owned
// pub:<repo:commit:fsID> refs of one producer with a tombstone at that
// producer's lease timestamp, so its own late writes are shadowed and a later
// producer's refs (later lease) are untouched. Without a producer lease (a
// settlement that never renewed in this visit) it falls back to the ordinary
// removal, which shadows every ref written up to now.
var removePublishedBlockReferenceRepairOwnedPubFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
	if !shouldQueuePublishedBlockReferenceRepair(repair.FSID, repair.StagedBlockIDs) {
		return nil
	}
	if strings.TrimSpace(repair.LivenessToken) == "" || repair.LivenessLeaseExpiresAt.IsZero() {
		return cleanupFailedPublishRemoveAttemptReferencesFn(database, repair.OrgID, publishedBlockReferenceRepairLivenessAttemptID(repair), repair.StagedBlockIDs)
	}
	return db.RemovePublishAttemptReferencesAt(database, repair.OrgID, publishedBlockReferenceRepairLivenessAttemptID(repair), repair.StagedBlockIDs, publishedBlockReferenceRepairLeaseTimestamp(repair.LivenessLeaseExpiresAt))
}

func publishedBlockReferenceRepairParentLookup(database *db.DB, repoID string) func(context.Context, string) (string, error) {
	return func(ctx context.Context, currentCommitID string) (string, error) {
		parentCommitID, err := publishedBlockReferenceRepairCommitParentFn(ctx, database, repoID, currentCommitID)
		if err != nil {
			return "", fmt.Errorf("lookup parent for commit %s: %w", currentCommitID, err)
		}
		return parentCommitID, nil
	}
}

func classifyPublishedBlockReferenceRepairCommitResumable(database *db.DB, repair *publishedBlockReferenceRepair) (publishedBlockReferenceRepairCommitOutcome, error) {
	if repair == nil {
		return publishedBlockReferenceRepairCommitUnknown, fmt.Errorf("queued publish repair is required to classify publication reachability")
	}
	ctx, cancel := context.WithTimeout(context.Background(), publishedCommitReachabilityTimeout)
	defer cancel()

	// First observation records one SERIAL HEAD. Later retries of that snapshot
	// walk from the cursor and do not re-read live HEAD unless the anchored
	// chain reaches genesis without the target. Clean genesis may then
	// re-observe HEAD and walk a second 1024-node chunk in this same 30s
	// context. headObservationBudget is the remaining SERIAL HEAD reads for
	// this visit; the anchor-creating read spends one of them.
	headObservationBudget := publishedCommitReachabilityMaxHeadObservations
	if strings.TrimSpace(repair.ReachabilityAnchorHeadCommitID) == "" {
		headObservationBudget--
		headCommitID, err := publishedBlockReferenceRepairHeadCommitFn(ctx, database, repair.OrgID, repair.RepoID)
		if err != nil {
			return publishedBlockReferenceRepairCommitUnknown, fmt.Errorf("lookup current head for repo %s: %w", repair.RepoID, err)
		}
		applied, err := persistPublishedBlockReferenceRepairAnchorFn(database, *repair, headCommitID)
		if err != nil {
			return publishedBlockReferenceRepairCommitUnknown, fmt.Errorf("persist reachability anchor for fs_object %s: %w", repair.FSID, err)
		}
		if applied {
			// The LWT set anchor and cursor together; mirror exactly that.
			repair.ReachabilityAnchorHeadCommitID = headCommitID
			repair.ReachabilityCursorCommitID = headCommitID
			repair.ReachabilityAnchorExhausted = false
		} else {
			loaded, loadErr := loadLivePublishedBlockReferenceRepair(database, *repair)
			if errors.Is(loadErr, errPublishedBlockReferenceRepairGone) {
				return publishedBlockReferenceRepairGoneClassification()
			}
			if loadErr != nil {
				return publishedBlockReferenceRepairCommitUnknown, loadErr
			}
			*repair = mergePublishedBlockReferenceRepairProgress(*repair, loaded)
			if strings.TrimSpace(repair.ReachabilityAnchorHeadCommitID) == "" {
				return publishedBlockReferenceRepairCommitUnknown, fmt.Errorf("reachability anchor was not durable for fs_object %s", repair.FSID)
			}
		}
	}

	if repair.ReachabilityAnchorExhausted {
		return reanchorPublishedBlockReferenceRepairAfterCleanGenesis(ctx, database, repair, headObservationBudget)
	}

	startCommitID := publishedBlockReferenceRepairProgressCursor(*repair)
	progress, err := walkPublishedCommitReachabilitySeeded(ctx, repair.CommitID, startCommitID, publishedBlockReferenceRepairWalkSeeds(*repair), publishedCommitReachabilityMaxNodes, publishedBlockReferenceRepairParentLookup(database, repair.RepoID))
	outcome, terminal, persistErr := persistPublishedBlockReferenceRepairWalkCursor(database, repair, startCommitID, progress, err)
	if terminal {
		return outcome, persistErr
	}
	// Clean genesis is not negative authority. Persist that this snapshot is
	// exhausted before the SERIAL HEAD re-read so a deadline on that read
	// cannot replay the same prefix.
	if publishedBlockReferenceRepairWalkExhaustedToGenesis(progress, err) {
		if persistErr := persistPublishedBlockReferenceRepairGenesisExhaustion(database, repair); persistErr != nil {
			if errors.Is(persistErr, errPublishedBlockReferenceRepairGone) {
				return publishedBlockReferenceRepairGoneClassification()
			}
			return publishedBlockReferenceRepairCommitUnknown, persistErr
		}
		return reanchorPublishedBlockReferenceRepairAfterCleanGenesis(ctx, database, repair, headObservationBudget)
	}
	return progress.Outcome, nil
}

// publishedBlockReferenceRepairWalkSeeds returns the commits a resumed chunk
// must treat as already visited. Only the anchored HEAD is durable, so it is
// the only cross-chunk cycle witness available without a persisted visited
// set (cycles below HEAD: ISSUE-PUBLISH-REPAIR-CROSS-CHUNK-CYCLE-01); the
// first chunk of a snapshot starts at that HEAD and seeds nothing.
func publishedBlockReferenceRepairWalkSeeds(repair publishedBlockReferenceRepair) []string {
	anchor := strings.TrimSpace(repair.ReachabilityAnchorHeadCommitID)
	if anchor == "" || anchor == publishedBlockReferenceRepairProgressCursor(repair) {
		return nil
	}
	return []string{anchor}
}

func publishedBlockReferenceRepairGoneClassification() (publishedBlockReferenceRepairCommitOutcome, error) {
	return publishedBlockReferenceRepairCommitNoLongerPending, errPublishedBlockReferenceRepairGone
}

func persistPublishedBlockReferenceRepairGenesisExhaustion(database *db.DB, repair *publishedBlockReferenceRepair) error {
	if repair == nil {
		return fmt.Errorf("queued publish repair is required to persist genesis exhaustion")
	}
	if repair.ReachabilityAnchorExhausted {
		return nil
	}
	expectedCursor := publishedBlockReferenceRepairProgressCursor(*repair)
	applied, err := markPublishedBlockReferenceRepairAnchorExhaustedFn(database, *repair, expectedCursor)
	if err != nil {
		return fmt.Errorf("persist genesis exhaustion for fs_object %s: %w", repair.FSID, err)
	}
	if applied {
		repair.ReachabilityAnchorExhausted = true
		return nil
	}
	loaded, loadErr := loadLivePublishedBlockReferenceRepair(database, *repair)
	if errors.Is(loadErr, errPublishedBlockReferenceRepairGone) {
		return loadErr
	}
	if loadErr != nil {
		return loadErr
	}
	*repair = mergePublishedBlockReferenceRepairProgress(*repair, loaded)
	if repair.ReachabilityAnchorExhausted {
		return nil
	}
	return fmt.Errorf("genesis exhaustion was not durable for fs_object %s", repair.FSID)
}

func publishedBlockReferenceRepairProgressCursor(repair publishedBlockReferenceRepair) string {
	cursor := strings.TrimSpace(repair.ReachabilityCursorCommitID)
	if cursor != "" {
		return cursor
	}
	return strings.TrimSpace(repair.ReachabilityAnchorHeadCommitID)
}

func publishedBlockReferenceRepairWalkExhaustedToGenesis(progress publishedCommitReachabilityWalk, err error) bool {
	return err == nil &&
		progress.Outcome == publishedBlockReferenceRepairCommitUnknown &&
		strings.TrimSpace(progress.NextCursor) == ""
}

func persistPublishedBlockReferenceRepairWalkCursor(database *db.DB, repair *publishedBlockReferenceRepair, startCommitID string, progress publishedCommitReachabilityWalk, walkErr error) (publishedBlockReferenceRepairCommitOutcome, bool, error) {
	if progress.Outcome == publishedBlockReferenceRepairCommitReachable {
		return publishedBlockReferenceRepairCommitReachable, true, nil
	}
	nextCursor := strings.TrimSpace(progress.NextCursor)
	if nextCursor != "" && nextCursor != startCommitID {
		applied, casErr := advancePublishedBlockReferenceRepairCursorFn(database, *repair, startCommitID, nextCursor)
		if casErr != nil {
			return publishedBlockReferenceRepairCommitUnknown, true, errors.Join(walkErr, fmt.Errorf("persist reachability cursor for fs_object %s: %w", repair.FSID, casErr))
		}
		if applied {
			repair.ReachabilityCursorCommitID = nextCursor
		} else {
			loaded, loadErr := loadLivePublishedBlockReferenceRepair(database, *repair)
			if errors.Is(loadErr, errPublishedBlockReferenceRepairGone) {
				outcome, goneErr := publishedBlockReferenceRepairGoneClassification()
				return outcome, true, goneErr
			}
			if loadErr != nil {
				return publishedBlockReferenceRepairCommitUnknown, true, errors.Join(walkErr, loadErr)
			}
			*repair = mergePublishedBlockReferenceRepairProgress(*repair, loaded)
		}
	}
	if walkErr != nil {
		return progress.Outcome, true, walkErr
	}
	return progress.Outcome, false, nil
}

func reanchorPublishedBlockReferenceRepairAfterCleanGenesis(ctx context.Context, database *db.DB, repair *publishedBlockReferenceRepair, headObservationBudget int) (publishedBlockReferenceRepairCommitOutcome, error) {
	// Same 30s context as the exhausted chunk. A newer HEAD is walked
	// immediately so a pre-HEAD repair can converge without waiting for the
	// next discovery visit. That second walk is a second 1024-node chunk,
	// not a violation of the per-chunk bound. A CAS loser that reloads an
	// already-exhausted newer snapshot must not replay it; it may re-anchor
	// once more only while the visit's SERIAL HEAD budget allows. When the
	// budget is spent the durable row already carries the newer exhausted
	// snapshot, so the next visit resumes there without losing work.
	if repair == nil {
		return publishedBlockReferenceRepairCommitUnknown, fmt.Errorf("queued publish repair is required to re-anchor publication reachability")
	}
	for {
		if headObservationBudget <= 0 {
			return publishedBlockReferenceRepairCommitUnknown, nil
		}
		headObservationBudget--
		exhaustedAnchor := strings.TrimSpace(repair.ReachabilityAnchorHeadCommitID)
		liveHEAD, err := publishedBlockReferenceRepairHeadCommitFn(ctx, database, repair.OrgID, repair.RepoID)
		if err != nil {
			return publishedBlockReferenceRepairCommitUnknown, fmt.Errorf("lookup current head after exhausting anchored ancestry for repo %s: %w", repair.RepoID, err)
		}
		liveHEAD = strings.TrimSpace(liveHEAD)
		if liveHEAD == "" {
			return publishedBlockReferenceRepairCommitUnknown, fmt.Errorf("canonical HEAD for repo %s is empty after exhausting anchored ancestry", repair.RepoID)
		}
		if liveHEAD == exhaustedAnchor {
			return publishedBlockReferenceRepairCommitUnknown, nil
		}
		expectedCursor := publishedBlockReferenceRepairProgressCursor(*repair)
		applied, casErr := replacePublishedBlockReferenceRepairAnchorFn(database, *repair, expectedCursor, liveHEAD)
		if casErr != nil {
			return publishedBlockReferenceRepairCommitUnknown, fmt.Errorf("re-anchor reachability after genesis for fs_object %s: %w", repair.FSID, casErr)
		}
		if applied {
			repair.ReachabilityAnchorHeadCommitID = liveHEAD
			repair.ReachabilityCursorCommitID = liveHEAD
			repair.ReachabilityAnchorExhausted = false
			break
		}
		loaded, loadErr := loadLivePublishedBlockReferenceRepair(database, *repair)
		if errors.Is(loadErr, errPublishedBlockReferenceRepairGone) {
			return publishedBlockReferenceRepairGoneClassification()
		}
		if loadErr != nil {
			return publishedBlockReferenceRepairCommitUnknown, loadErr
		}
		*repair = mergePublishedBlockReferenceRepairProgress(*repair, loaded)
		if strings.TrimSpace(repair.ReachabilityAnchorHeadCommitID) == "" {
			return publishedBlockReferenceRepairCommitUnknown, fmt.Errorf("reachability anchor was not durable for fs_object %s", repair.FSID)
		}
		if strings.TrimSpace(repair.ReachabilityAnchorHeadCommitID) == exhaustedAnchor {
			return publishedBlockReferenceRepairCommitUnknown, nil
		}
		if repair.ReachabilityAnchorExhausted {
			// A concurrent winner already exhausted a newer snapshot. Do not
			// replay that prefix; re-anchor against the loaded row if the
			// budget still allows another SERIAL HEAD observation.
			continue
		}
		// A concurrent winner replaced the anchor and is still walking it.
		// Continue that snapshot from its durable cursor; the cursor CAS keeps
		// both workers monotonic.
		break
	}

	startCommitID := publishedBlockReferenceRepairProgressCursor(*repair)
	progress, walkErr := walkPublishedCommitReachabilitySeeded(ctx, repair.CommitID, startCommitID, publishedBlockReferenceRepairWalkSeeds(*repair), publishedCommitReachabilityMaxNodes, publishedBlockReferenceRepairParentLookup(database, repair.RepoID))
	outcome, terminal, persistErr := persistPublishedBlockReferenceRepairWalkCursor(database, repair, startCommitID, progress, walkErr)
	if terminal {
		return outcome, persistErr
	}
	if publishedBlockReferenceRepairWalkExhaustedToGenesis(progress, walkErr) {
		if persistErr := persistPublishedBlockReferenceRepairGenesisExhaustion(database, repair); persistErr != nil {
			if errors.Is(persistErr, errPublishedBlockReferenceRepairGone) {
				return publishedBlockReferenceRepairGoneClassification()
			}
			return publishedBlockReferenceRepairCommitUnknown, persistErr
		}
	}
	return publishedBlockReferenceRepairCommitUnknown, nil
}

var publishedBlockReferenceRepairCommitReachableFn = classifyPublishedBlockReferenceRepairCommitFromStore

var publishedBlockReferenceRepairClassifyFn = classifyPublishedBlockReferenceRepairCommitResumable

var loadPublishedBlockReferenceRepairPendingFileFn = func(database *db.DB, repoID, fsID string) (*pendingPublishedFile, error) {
	if database == nil {
		return nil, fmt.Errorf("database not available")
	}
	var blockIDs []string
	err := database.Session().Query(`
		SELECT block_ids FROM fs_objects WHERE library_id = ? AND fs_id = ?
	`, repoID, fsID).Scan(&blockIDs)
	if err != nil {
		return nil, fmt.Errorf("lookup repair fs_object %s/%s: %w", repoID, fsID, err)
	}
	return &pendingPublishedFile{
		fsID:             fsID,
		externalBlockIDs: append([]string(nil), blockIDs...),
	}, nil
}

var cleanupFailedPublishDeleteCommitFn = func(database *db.DB, repoID, commitID string) error {
	if database == nil {
		return fmt.Errorf("database not available")
	}
	return database.Session().Query(`
		DELETE FROM commits WHERE library_id = ? AND commit_id = ?
	`, repoID, commitID).Exec()
}

var cleanupFailedPublishRemoveAttemptReferencesFn = db.RemovePublishAttemptReferences

var cleanupFailedPublishDeleteFSObjectFn = func(database *db.DB, repoID, fsID string) error {
	if database == nil {
		return fmt.Errorf("database not available")
	}
	return database.Session().Query(`
		DELETE FROM fs_objects WHERE library_id = ? AND fs_id = ?
	`, repoID, fsID).Exec()
}

var cleanupFailedPublishDeletePendingOwnerFn = func(database *db.DB, repoID, fsID, ownerID string, createdAt time.Time) error {
	if database == nil {
		return fmt.Errorf("database not available")
	}
	return database.DeletePendingPublishedFSObjectOwner(repoID, fsID, ownerID, createdAt)
}

var cleanupFailedPublishPendingOwnerExistsFn = func(database *db.DB, repoID, fsID string) (bool, error) {
	if database == nil {
		return false, fmt.Errorf("database not available")
	}
	return database.PendingPublishedFSObjectOwnerExists(repoID, fsID)
}

var cleanupFailedPublishFSObjectReachableFn = func(database *db.DB, repoID, fsID string) (bool, error) {
	return failedPublishFSObjectReachable(database, repoID, fsID)
}

var failedPublishReachabilityLoadFSObjectFn = func(database *db.DB, repoID, fsID string) (string, string, error) {
	if database == nil {
		return "", "", fmt.Errorf("database not available")
	}
	var objType, dirEntriesJSON string
	err := database.Session().Query(`
			SELECT obj_type, dir_entries FROM fs_objects WHERE library_id = ? AND fs_id = ?
		`, repoID, fsID).Scan(&objType, &dirEntriesJSON)
	return objType, dirEntriesJSON, err
}

var clearPendingPublishedFileOwnersFn = clearPendingPublishedFileOwners

var clearPendingPublishedFileOwnerFn = clearPendingPublishedFileOwner

var releasePendingPublishedFileOwnersFn = releasePendingPublishedFileOwners

var releasePendingPublishedFileOwnerFn = releasePendingPublishedFileOwner

var cleanupPendingPublishedFileOwnerAttemptFn = cleanupPendingPublishedFileOwnerAttempt

var cleanupPendingPublishedFileAttemptCommitReachableFn = publishedBlockReferenceRepairCommitReachableFn

var pendingPublishedFSObjectOwnerNowFn = time.Now

var publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
	return helper.promotePendingPublishedFiles(orgID, repoID, commitID, []*pendingPublishedFile{pending})
}

func CleanupFailedPublishArtifacts(database *db.DB, orgID, repoID, attemptID, commitID string, fsIDs, blockIDs []string) error {
	if database == nil {
		return nil
	}
	var cleanupErr error
	commitID = strings.TrimSpace(commitID)
	if commitID != "" {
		if err := cleanupFailedPublishDeleteCommitFn(database, repoID, commitID); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete failed publish commit %s: %w", commitID, err))
		}
	}
	attemptID = strings.TrimSpace(attemptID)
	blockIDs = db.NormalizeBlockIDs(blockIDs)
	if attemptID != "" && len(blockIDs) > 0 {
		if err := cleanupFailedPublishRemoveAttemptReferencesFn(database, orgID, attemptID, blockIDs); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove publish-attempt refs for %s: %w", attemptID, err))
		}
	}
	return cleanupErr
}

func CleanupFailedPublishAttempt(database *db.DB, orgID, repoID, attemptID, commitID string, pendingFiles []*pendingPublishedFile) error {
	if err := CleanupFailedPublishArtifacts(database, orgID, repoID, attemptID, commitID, pendingPublishedFileFSIDs(pendingFiles), pendingPublishedFileInternalBlockIDs(pendingFiles)); err != nil {
		return err
	}
	return releasePendingPublishedFileOwnersFn(database, repoID, pendingFiles)
}

func clearPendingPublishedFileOwners(database *db.DB, repoID string, pendingFiles []*pendingPublishedFile) error {
	if database == nil || strings.TrimSpace(repoID) == "" {
		return nil
	}
	var clearErr error
	for _, pending := range pendingFiles {
		if err := clearPendingPublishedFileOwnerFn(database, repoID, pending); err != nil {
			clearErr = errors.Join(clearErr, err)
		}
	}
	return clearErr
}

func clearPendingPublishedFileOwner(database *db.DB, repoID string, pending *pendingPublishedFile) error {
	if database == nil || pending == nil {
		return nil
	}
	fsID := strings.TrimSpace(pending.fsID)
	ownerID := strings.TrimSpace(pending.cleanupOwnerID)
	if fsID == "" || ownerID == "" {
		return nil
	}
	if err := cleanupFailedPublishDeletePendingOwnerFn(database, repoID, fsID, ownerID, pending.cleanupCreatedAt); err != nil {
		return fmt.Errorf("clear pending publish owner for fs_object %s: %w", fsID, err)
	}
	return nil
}

func releasePendingPublishedFileOwners(database *db.DB, repoID string, pendingFiles []*pendingPublishedFile) error {
	if database == nil || strings.TrimSpace(repoID) == "" {
		return nil
	}
	var releaseErr error
	for _, pending := range pendingFiles {
		if err := releasePendingPublishedFileOwnerFn(database, repoID, pending); err != nil {
			releaseErr = errors.Join(releaseErr, err)
		}
	}
	return releaseErr
}

func releasePendingPublishedFileOwner(database *db.DB, repoID string, pending *pendingPublishedFile) error {
	if database == nil || pending == nil {
		return nil
	}
	fsID := strings.TrimSpace(pending.fsID)
	ownerID := strings.TrimSpace(pending.cleanupOwnerID)
	if fsID == "" || ownerID == "" {
		return nil
	}
	if err := cleanupFailedPublishDeletePendingOwnerFn(database, repoID, fsID, ownerID, pending.cleanupCreatedAt); err != nil {
		return fmt.Errorf("release pending publish owner for fs_object %s: %w", fsID, err)
	}
	// fs_id is content-addressed and can be shared by concurrent publish attempts.
	// Removing fs_objects here can delete the metadata row another owner just began using.
	return nil
}

func ReleasePendingPublishedFSObjectOwner(database *db.DB, repoID, fsID, ownerID string, createdAt time.Time) error {
	return releasePendingPublishedFileOwner(database, repoID, &pendingPublishedFile{
		fsID:             fsID,
		cleanupOwnerID:   ownerID,
		cleanupCreatedAt: createdAt,
	})
}

func ClearPendingPublishedFSObjectOwner(database *db.DB, repoID, fsID, ownerID string, createdAt time.Time) error {
	return clearPendingPublishedFileOwner(database, repoID, &pendingPublishedFile{
		fsID:             fsID,
		cleanupOwnerID:   ownerID,
		cleanupCreatedAt: createdAt,
	})
}

func cleanupPendingPublishedFileOwnerAttempt(database *db.DB, repoID string, pending *pendingPublishedFile) error {
	if database == nil || pending == nil {
		return nil
	}
	fsID := strings.TrimSpace(pending.fsID)
	attemptID := strings.TrimSpace(pending.cleanupAttemptID)
	if attemptID == "" {
		return fmt.Errorf("pending publish owner for fs_object %s is missing cleanup attempt metadata", fsID)
	}
	orgID := strings.TrimSpace(pending.cleanupOrgID)
	if orgID == "" {
		return fmt.Errorf("pending publish owner for fs_object %s is missing cleanup org_id", fsID)
	}
	outcome, err := cleanupPendingPublishedFileAttemptCommitReachableFn(database, orgID, repoID, attemptID)
	if err != nil {
		return fmt.Errorf("check publish attempt commit %s reachability for fs_object %s: %w", attemptID, fsID, err)
	}
	switch outcome {
	case publishedBlockReferenceRepairCommitReachable:
		promotePending, err := loadPublishedBlockReferenceRepairPendingFileFn(database, repoID, fsID)
		if err != nil {
			return fmt.Errorf("load reachable published fs_object %s for commit %s: %w", fsID, attemptID, err)
		}
		promotePending.cleanupOwnerID = pending.cleanupOwnerID
		promotePending.cleanupCreatedAt = pending.cleanupCreatedAt
		promotePending.cleanupOrgID = orgID
		promotePending.cleanupAttemptID = attemptID
		promotePending.internalBlockIDs = append([]string(nil), db.NormalizeBlockIDs(pending.internalBlockIDs)...)
		helper := NewFSHelper(database)
		if err := publishedBlockReferenceRepairPromoteFn(helper, orgID, repoID, attemptID, promotePending); err != nil {
			return fmt.Errorf("promote reachable published fs_object %s for commit %s: %w", fsID, attemptID, err)
		}
		return clearPendingPublishedFileOwnerFn(database, repoID, pending)
	default:
		return fmt.Errorf("publication outcome for commit %s is unknown; retain pending fs_object owner", attemptID)
	}
}

func failedPublishFSObjectReachable(database *db.DB, repoID, targetFSID string) (bool, error) {
	if database == nil || strings.TrimSpace(repoID) == "" || strings.TrimSpace(targetFSID) == "" {
		return false, nil
	}
	iter := database.Session().Query(`
		SELECT root_fs_id FROM commits WHERE library_id = ?
	`, repoID).Iter()
	visited := make(map[string]bool)
	var rootFSID string
	for iter.Scan(&rootFSID) {
		reachable, err := failedPublishFSObjectReachableFromRoot(database, repoID, targetFSID, rootFSID, visited)
		if err != nil {
			_ = iter.Close()
			return false, err
		}
		if reachable {
			if err := iter.Close(); err != nil {
				return false, fmt.Errorf("close commit iterator for repo %s: %w", repoID, err)
			}
			return true, nil
		}
	}
	if err := iter.Close(); err != nil {
		return false, fmt.Errorf("list commits for repo %s: %w", repoID, err)
	}
	return false, nil
}

func failedPublishFSObjectReachableFromRoot(database *db.DB, repoID, targetFSID, rootFSID string, visited map[string]bool) (bool, error) {
	if rootFSID == "" {
		return false, nil
	}
	stack := []string{rootFSID}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if current == "" || visited[current] {
			continue
		}
		if current == targetFSID {
			return true, nil
		}
		visited[current] = true

		objType, dirEntriesJSON, err := failedPublishReachabilityLoadFSObjectFn(database, repoID, current)
		if err != nil {
			if errors.Is(err, gocql.ErrNotFound) {
				continue
			}
			return false, fmt.Errorf("load fs_object %s in repo %s: %w", current, repoID, err)
		}
		if objType != "dir" {
			continue
		}
		var entries []FSEntry
		if dirEntriesJSON != "" && dirEntriesJSON != "[]" {
			if err := json.Unmarshal([]byte(dirEntriesJSON), &entries); err != nil {
				return false, fmt.Errorf("decode dir_entries for fs_object %s in repo %s: %w", current, repoID, err)
			}
		}
		for i := len(entries) - 1; i >= 0; i-- {
			if childID := strings.TrimSpace(entries[i].ID); childID != "" && !visited[childID] {
				stack = append(stack, childID)
			}
		}
	}
	return false, nil
}

func publishedBlockReferenceRepairBucket(orgID, repoID, commitID, fsID string) int {
	if publishedBlockReferenceRepairBuckets <= 1 {
		return 0
	}
	hasher := fnv.New32a()
	for _, part := range []string{orgID, repoID, commitID, fsID} {
		_, _ = hasher.Write([]byte(part))
		_, _ = hasher.Write([]byte{0})
	}
	return int(hasher.Sum32() % uint32(publishedBlockReferenceRepairBuckets))
}

func newPublishedBlockReferenceRepair(orgID, repoID, commitID, fsID string, stagedBlockIDs []string) publishedBlockReferenceRepair {
	orgID = strings.TrimSpace(orgID)
	repoID = strings.TrimSpace(repoID)
	commitID = strings.TrimSpace(commitID)
	fsID = strings.TrimSpace(fsID)
	now := publishedBlockReferenceRepairNowFn().UTC()
	return publishedBlockReferenceRepair{
		Bucket:         publishedBlockReferenceRepairBucket(orgID, repoID, commitID, fsID),
		OrgID:          orgID,
		RepoID:         repoID,
		CommitID:       commitID,
		FSID:           fsID,
		StagedBlockIDs: append([]string(nil), db.NormalizeBlockIDs(stagedBlockIDs)...),
		CreatedAt:      now,
		LeaseExpiresAt: now.Add(publishedBlockReferenceRepairPreCASLease),
	}
}

// publishedBlockReferenceRepairRetryDelay is process-local advisory backoff
// derived from row age. It does not bound how soon the discovery sweep will
// visit the row again (ISSUE-PUBLISH-REPAIR-DISCOVERY-SCALE-01).
func publishedBlockReferenceRepairRetryDelay(now, createdAt time.Time) time.Duration {
	delay := now.Sub(createdAt)
	if delay < publishedBlockReferenceRepairRetryBase {
		return publishedBlockReferenceRepairRetryBase
	}
	if delay > publishedBlockReferenceRepairRetryMax {
		return publishedBlockReferenceRepairRetryMax
	}
	return delay
}

func shouldQueuePublishedBlockReferenceRepair(fsID string, blockIDs []string) bool {
	return strings.TrimSpace(fsID) != "" && len(blockIDs) > 0
}

func queuePublishedBlockReferenceRepair(database *db.DB, repair publishedBlockReferenceRepair) error {
	if strings.TrimSpace(repair.CommitID) == "" || strings.TrimSpace(repair.FSID) == "" {
		return nil
	}
	return insertPublishedBlockReferenceRepairFn(database, repair)
}

func QueuePublishedFSObjectBlockReferenceRepair(database *db.DB, orgID, repoID, commitID, fsID string, stagedBlockIDs []string) error {
	return queuePublishedBlockReferenceRepair(database, newPublishedBlockReferenceRepair(orgID, repoID, commitID, fsID, stagedBlockIDs))
}

func ClearPublishedFSObjectBlockReferenceRepair(database *db.DB, orgID, repoID, commitID, fsID string) error {
	repair := newPublishedBlockReferenceRepair(orgID, repoID, commitID, fsID, nil)
	if strings.TrimSpace(repair.CommitID) == "" || strings.TrimSpace(repair.FSID) == "" {
		return nil
	}
	return deletePublishedBlockReferenceRepairFn(database, repair)
}

// removePublishedBlockReferenceRepairOwnedLiveness drops pub:<repo:commit:fsID>
// rows this repair worker may have renewed. Promote still uses commitID for the
// original v2 attempt identity; this helper must not reuse that shared key.
// Concurrent renewal of this same row can recreate the refs before the repair
// row is deleted (ISSUE-PUBLISH-REPAIR-OWNED-PUB-CLEANUP-RACE-01).
var removePublishedBlockReferenceRepairOwnedLivenessFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
	if strings.TrimSpace(repair.CommitID) == "" || strings.TrimSpace(repair.FSID) == "" {
		return nil
	}
	return removePublishedBlockReferenceRepairOwnedPubFn(database, repair)
}

func publishedBlockReferenceRepairRetryKey(repair publishedBlockReferenceRepair) string {
	return strings.TrimSpace(repair.OrgID) + `:` + publishedBlockReferenceRepairKey(repair.RepoID, repair.CommitID, repair.FSID)
}

func publishedBlockReferenceRepairKey(repoID, commitID, fsID string) string {
	return strings.TrimSpace(repoID) + ":" + strings.TrimSpace(commitID) + ":" + strings.TrimSpace(fsID)
}

// publishedBlockReferenceRepairLivenessAttemptID is the pub:<attempt> identity
// owned by one repair row. It includes repo, commit, and fs_id because
// pub:<commitID> is already the v2 publication attempt and is shared by every
// file of that commit. Settling one repair must not drop a sibling's renewal.
func publishedBlockReferenceRepairLivenessAttemptID(repair publishedBlockReferenceRepair) string {
	return publishedBlockReferenceRepairKey(repair.RepoID, repair.CommitID, repair.FSID)
}

func rollbackQueuedPublishedBlockReferenceRepairs(database *db.DB, inserted []publishedBlockReferenceRepair, stageErr error) error {
	if len(inserted) == 0 {
		return stageErr
	}
	var cleanupErr error
	for _, repair := range inserted {
		if err := deletePublishedBlockReferenceRepairFn(database, repair); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete queued publish repair %s/%s/%s: %w", repair.RepoID, repair.CommitID, repair.FSID, err))
		}
	}
	if cleanupErr != nil {
		return errors.Join(stageErr, cleanupErr)
	}
	return stageErr
}

func queuePendingPublishedFileRepairs(database *db.DB, orgID, repoID, commitID string, pendingFiles []*pendingPublishedFile) error {
	queued := make([]publishedBlockReferenceRepair, 0, len(pendingFiles))
	for _, pending := range pendingFiles {
		if pending == nil || !shouldQueuePublishedBlockReferenceRepair(pending.fsID, pending.internalBlockIDs) {
			continue
		}
		repair := newPublishedBlockReferenceRepair(orgID, repoID, commitID, pending.fsID, pending.internalBlockIDs)
		if err := queuePublishedBlockReferenceRepair(database, repair); err != nil {
			return rollbackQueuedPublishedBlockReferenceRepairs(database, append(append([]publishedBlockReferenceRepair(nil), queued...), repair), fmt.Errorf("queue publish repair for fs_object %s: %w", pending.fsID, err))
		}
		queued = append(queued, repair)
	}
	return nil
}

func clearPendingPublishedFileRepairs(database *db.DB, orgID, repoID, commitID string, pendingFiles []*pendingPublishedFile) error {
	var clearErr error
	for _, pending := range pendingFiles {
		if pending == nil || !shouldQueuePublishedBlockReferenceRepair(pending.fsID, pending.internalBlockIDs) {
			continue
		}
		if err := ClearPublishedFSObjectBlockReferenceRepair(database, orgID, repoID, commitID, pending.fsID); err != nil {
			clearErr = errors.Join(clearErr, fmt.Errorf("clear queued publish repair for fs_object %s: %w", pending.fsID, err))
		}
	}
	return clearErr
}

func SchedulePublishedFSObjectBlockReferenceRepair(database *db.DB, orgID, repoID, commitID, fsID, label string, stagedBlockIDs []string) {
	if !shouldQueuePublishedBlockReferenceRepair(fsID, stagedBlockIDs) || strings.TrimSpace(commitID) == "" {
		return
	}
	SchedulePublishedBlockReferenceRepair(publishedBlockReferenceRepairKey(repoID, commitID, fsID), label, func() error {
		return RepairPublishedFSObjectBlockReferenceRepair(database, orgID, repoID, commitID, fsID, stagedBlockIDs)
	})
}

func schedulePendingPublishedFileRepairs(database *db.DB, orgID, repoID, commitID string, pendingFiles []*pendingPublishedFile, label string) {
	keyParts := []string{strings.TrimSpace(repoID), strings.TrimSpace(commitID)}
	for _, pending := range pendingFiles {
		if pending == nil || !shouldQueuePublishedBlockReferenceRepair(pending.fsID, pending.internalBlockIDs) {
			continue
		}
		keyParts = append(keyParts, strings.TrimSpace(pending.fsID))
	}
	SchedulePublishedBlockReferenceRepair(strings.Join(keyParts, ":"), label, func() error {
		var repairErr error
		for _, pending := range pendingFiles {
			if pending == nil || !shouldQueuePublishedBlockReferenceRepair(pending.fsID, pending.internalBlockIDs) {
				continue
			}
			if err := RepairPublishedFSObjectBlockReferenceRepair(database, orgID, repoID, commitID, pending.fsID, pending.internalBlockIDs); err != nil {
				repairErr = errors.Join(repairErr, fmt.Errorf("repair queued publish refs for fs_object %s: %w", pending.fsID, err))
			}
		}
		return repairErr
	})
}

func repairPublishedBlockReferenceRepair(database *db.DB, repair publishedBlockReferenceRepair) error {
	return repairPublishedBlockReferenceRepairWithClassifier(database, repair, publishedBlockReferenceRepairClassifyFn)
}

// repairPublishedBlockReferenceRepairWithClassifier is one worker visit with an
// explicit classifier. Production passes publishedBlockReferenceRepairClassifyFn;
// evidence can wrap it for one identity without swapping the process-wide
// variable a live worker may read concurrently.
func repairPublishedBlockReferenceRepairWithClassifier(database *db.DB, repair publishedBlockReferenceRepair, classify func(*db.DB, *publishedBlockReferenceRepair) (publishedBlockReferenceRepairCommitOutcome, error)) error {
	if !shouldQueuePublishedBlockReferenceRepair(repair.FSID, repair.StagedBlockIDs) || strings.TrimSpace(repair.CommitID) == "" {
		return nil
	}
	hydrated, err := hydratePublishedBlockReferenceRepair(database, repair)
	if errors.Is(err, errPublishedBlockReferenceRepairGone) {
		return nil
	}
	if err != nil {
		return err
	}
	repair = hydrated
	// Renew repair-owned liveness while the row is still pending and BEFORE
	// the bounded classifier (SERIAL HEAD + up to 30s of EACH_QUORUM parent
	// reads). Renewing after the walk left an interval created by this very
	// visit in which a previously valid pub: could expire while the walk was
	// still running (ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01). Gone
	// before the write is a terminal no-op; gone during the write has already
	// compensated exactly the pub: it wrote. A failed renewal fails closed:
	// there is no point spending the walk budget without the protection this
	// ordering exists to provide. This is one renewal per visit; UNKNOWN,
	// classifier error, and settlement failure retain the row under it and
	// do not write a second time. This closes the classifier-induced gap
	// only: the renewal is itself a sequential per-block fan-out, so a prior
	// pin can still expire during that fan-out, and a visit that starts after
	// the prior pub: already expired is a discovery/TTL problem this order
	// cannot fix (ISSUE-PUBLISH-REPAIR-DISCOVERY-SCALE-01).
	if renewErr := renewPublishedBlockReferenceRepairLivenessIfPending(database, &repair); renewErr != nil {
		if errors.Is(renewErr, errPublishedBlockReferenceRepairGone) {
			return nil
		}
		return fmt.Errorf("renew publish-attempt liveness for fs_object %s before classification: %w", repair.FSID, renewErr)
	}
	commitOutcome, classifyErr := classify(database, &repair)
	settleErr := settlePublishedBlockReferenceRepair(database, repair, commitOutcome, classifyErr)
	if settleErr == nil && classifyErr == nil && commitOutcome == publishedBlockReferenceRepairCommitReachable {
		return nil
	}
	// No positive settlement (UNKNOWN/error retention, classifier Gone, or a
	// REACHABLE settlement that failed before its own pub: removal). A
	// writer's ordinary settlement (ClearPublishedFSObjectBlockReferenceRepair)
	// deletes only the row and may have run while the walk or the settlement
	// was in progress; this visit wrote pub: before that walk, so it must
	// remove that identity when the row is gone instead of leaving it
	// ownerless until its TTL. A row still pending is retained under the pin
	// already written.
	gone, compensateErr := compensatePublishedBlockReferenceRepairLivenessIfGone(database, repair)
	if compensateErr != nil {
		return errors.Join(settleErr, compensateErr)
	}
	if gone {
		return nil
	}
	return settleErr
}

// settlePublishedBlockReferenceRepair applies a previously classified
// publication outcome. Keeping classification separate lets integration
// evidence exercise the settlement contract without replacing a process-wide
// classifier that a live repair worker may call concurrently.
func settlePublishedBlockReferenceRepair(database *db.DB, repair publishedBlockReferenceRepair, commitOutcome publishedBlockReferenceRepairCommitOutcome, classifyErr error) error {
	if errors.Is(classifyErr, errPublishedBlockReferenceRepairGone) || commitOutcome == publishedBlockReferenceRepairCommitNoLongerPending {
		return nil
	}
	if classifyErr != nil {
		return classifyErr
	}
	switch commitOutcome {
	case publishedBlockReferenceRepairCommitReachable:
		pending, err := loadPublishedBlockReferenceRepairPendingFileFn(database, repair.RepoID, repair.FSID)
		if err != nil {
			return err
		}
		pending.internalBlockIDs = append([]string(nil), repair.StagedBlockIDs...)
		helper := NewFSHelper(database)
		if err := publishedBlockReferenceRepairPromoteFn(helper, repair.OrgID, repair.RepoID, repair.CommitID, pending); err != nil {
			return fmt.Errorf("promote published fs_object %s for commit %s: %w", repair.FSID, repair.CommitID, err)
		}
		if err := removePublishedBlockReferenceRepairOwnedLivenessFn(database, repair); err != nil {
			return fmt.Errorf("remove repair-owned publish-attempt liveness for fs_object %s: %w", repair.FSID, err)
		}
		// A visit that renewed holds a producer token and deletes exactly its
		// own witness; a settlement without one (no renewal in this visit)
		// wrote no witness. Witnesses of earlier retained visits of this
		// identity are consumed by the sweep once the row is gone.
		if strings.TrimSpace(repair.LivenessToken) != "" {
			if err := deletePublishedBlockReferenceRepairLivenessCleanupFn(database, repair); err != nil {
				return fmt.Errorf("delete repair-owned liveness cleanup intent for fs_object %s: %w", repair.FSID, err)
			}
		}
		// Delete the row only after the best-effort pub: remove. Concurrent
		// UNKNOWN renewal of this same row can still recreate
		// pub:<repo:commit:fsID> before this delete
		// (ISSUE-PUBLISH-REPAIR-OWNED-PUB-CLEANUP-RACE-01).
	case publishedBlockReferenceRepairCommitUnknown:
		return fmt.Errorf("publication outcome for fs_object %s commit %s is unknown; retain queued repair", repair.FSID, repair.CommitID)
	case publishedBlockReferenceRepairCommitDefinitelyNotReachable:
		return fmt.Errorf("publication outcome for fs_object %s commit %s is definitely not reachable but has no durable cleanup authority; retain queued repair", repair.FSID, repair.CommitID)
	default:
		return fmt.Errorf("publication outcome for fs_object %s commit %s is unsupported; retain queued repair", repair.FSID, repair.CommitID)
	}
	if err := deletePublishedBlockReferenceRepairFn(database, repair); err != nil {
		return fmt.Errorf("delete queued publish repair for fs_object %s: %w", repair.FSID, err)
	}
	return nil
}

func runPendingPublishedFSObjectOwnerSweep(database *db.DB) error {
	if database == nil {
		return nil
	}
	now := pendingPublishedFSObjectOwnerNowFn().UTC()
	cutoff := now.Add(-pendingPublishedFSObjectOwnerStaleAfter)
	startDay := db.GCProjectionUTCDate(now.AddDate(0, 0, -pendingPublishedFSObjectOwnerLookbackDays))
	endDay := db.GCProjectionUTCDate(now)
	var firstErr error
	for day := startDay; !day.After(endDay); day = day.AddDate(0, 0, 1) {
		for bucket := 0; bucket < db.GCDiscoveryBucketCount; bucket++ {
			owners, err := listPendingPublishedFSObjectOwnersByDayFn(database, day, bucket)
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("list pending published fs_object owners for day=%s bucket=%d: %w", db.GCProjectionDateString(day), bucket, err)
				}
				continue
			}
			for _, owner := range owners {
				if owner.CreatedAt.IsZero() || owner.CreatedAt.After(cutoff) {
					continue
				}
				if strings.TrimSpace(owner.AttemptID) == "" {
					loadedOwner, err := loadPendingPublishedFSObjectOwnerFn(database, owner.RepoID, owner.FSID, owner.OwnerID)
					if err != nil {
						if errors.Is(err, gocql.ErrNotFound) {
							if deleteErr := cleanupFailedPublishDeletePendingOwnerFn(database, owner.RepoID, owner.FSID, owner.OwnerID, owner.CreatedAt); deleteErr != nil {
								log.Printf("[publish_repair] failed to delete dangling pending fs_object owner projection repo=%s fs_object=%s owner=%s: %v", owner.RepoID, owner.FSID, owner.OwnerID, deleteErr)
								if firstErr == nil {
									firstErr = deleteErr
								}
							}
							continue
						}
						log.Printf("[publish_repair] failed to hydrate pending fs_object owner repo=%s fs_object=%s owner=%s: %v", owner.RepoID, owner.FSID, owner.OwnerID, err)
						if firstErr == nil {
							firstErr = err
						}
						continue
					}
					if !loadedOwner.CreatedAt.IsZero() {
						owner.CreatedAt = loadedOwner.CreatedAt
					}
					owner.OrgID = loadedOwner.OrgID
					owner.AttemptID = loadedOwner.AttemptID
					owner.BlockIDs = append([]string(nil), loadedOwner.BlockIDs...)
				}
				pending := &pendingPublishedFile{
					fsID:             owner.FSID,
					internalBlockIDs: append([]string(nil), owner.BlockIDs...),
					cleanupOwnerID:   owner.OwnerID,
					cleanupCreatedAt: owner.CreatedAt,
					cleanupOrgID:     owner.OrgID,
					cleanupAttemptID: owner.AttemptID,
				}
				if err := cleanupPendingPublishedFileOwnerAttemptFn(database, owner.RepoID, pending); err != nil {
					log.Printf("[publish_repair] stale pending fs_object owner cleanup failed for repo=%s fs_object=%s owner=%s: %v", owner.RepoID, owner.FSID, owner.OwnerID, err)
					if firstErr == nil {
						firstErr = err
					}
				}
			}
		}
	}
	return firstErr
}

func shouldRunPendingPublishedFSObjectOwnerSweep(lastRun, now time.Time) bool {
	return lastRun.IsZero() || !now.Before(lastRun.Add(pendingPublishedFSObjectOwnerSweepInterval))
}

func RepairPublishedFSObjectBlockReferenceRepair(database *db.DB, orgID, repoID, commitID, fsID string, stagedBlockIDs []string) error {
	return repairPublishedBlockReferenceRepair(database, newPublishedBlockReferenceRepair(orgID, repoID, commitID, fsID, stagedBlockIDs))
}

func runPublishedBlockReferenceRepairSweep(database *db.DB) error {
	if database == nil {
		return nil
	}
	now := publishedBlockReferenceRepairNowFn().UTC()
	prunePublishedBlockReferenceRepairRetryHints(now)
	cutoff := now.Add(-publishedBlockReferenceRepairStaleAfter)
	var firstErr error
	for bucket := 0; bucket < publishedBlockReferenceRepairBuckets; bucket++ {
		repairs, err := listPublishedBlockReferenceRepairsForBucketFn(database, bucket)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("list queued publish repairs for bucket %d: %w", bucket, err)
			}
			continue
		}
		for _, repair := range repairs {
			if publishedBlockReferenceRepairIsProgressOnly(repair) {
				// Residue of a progress LWT that raced the ordinary settlement
				// DELETE. It is listed on every sweep and can never be acted
				// on. Reap only its reachability cells; never the row.
				if _, err := reapPublishedBlockReferenceRepairProgressOnlyRowFn(database, repair); err != nil {
					log.Printf("[publish_repair] failed to reap progress-only repair residue for repo=%s commit=%s fs_object=%s: %v", repair.RepoID, repair.CommitID, repair.FSID, err)
					if firstErr == nil {
						firstErr = fmt.Errorf("reap progress-only repair residue for fs_object %s: %w", repair.FSID, err)
					}
				}
				continue
			}
			retryKey := publishedBlockReferenceRepairRetryKey(repair)
			if nextRetry, ok := publishedBlockReferenceRepairNextRetryAt.Load(retryKey); ok {
				if retryAt, ok := nextRetry.(time.Time); ok && retryAt.After(now) {
					continue
				}
				if _, ok := nextRetry.(time.Time); !ok {
					publishedBlockReferenceRepairNextRetryAt.Delete(retryKey)
				}
			}
			if !repair.CreatedAt.IsZero() && repair.CreatedAt.After(cutoff) {
				continue
			}
			// lease_expires_at is advisory scheduling state only. It is not
			// consulted by the settlement function and never authorizes cleanup.
			if !repair.LeaseExpiresAt.IsZero() && repair.LeaseExpiresAt.After(now) {
				continue
			}
			if err := repairPublishedBlockReferenceRepair(database, repair); err != nil {
				nextRetryAt := now.Add(publishedBlockReferenceRepairRetryDelay(now, repair.CreatedAt))
				if retryErr := schedulePublishedBlockReferenceRepairRetryFn(database, repair, nextRetryAt); retryErr != nil {
					err = errors.Join(err, fmt.Errorf("schedule next publish repair retry at %s: %w", nextRetryAt.Format(time.RFC3339), retryErr))
				}
				log.Printf("[publish_repair] queued repair failed for repo=%s commit=%s fs_object=%s: %v", repair.RepoID, repair.CommitID, repair.FSID, err)
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		if err := sweepPublishedBlockReferenceRepairLivenessCleanups(database, bucket); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// sweepPublishedBlockReferenceRepairLivenessCleanups is the durable retry of
// the in-visit compensation. Per identity it decides absence once
// (publishedBlockReferenceRepairGoneForCleanup: local read retains,
// EACH_QUORUM decides). Row conclusively gone: every consumable intent
// (armed, or preparing past its lease) has its producer's pins removed with a
// tombstone at that producer's lease timestamp and is then deleted; a
// preparing intent under a live lease is kept (its producer may still
// write). Row pending: the pin is owned, and armed intents of that identity
// are compacted to the one with the greatest lease — its eventual tombstone
// timestamp shadows every pin the older producers wrote — so the durable
// state per pending identity stays bounded no matter how many visits
// retained it; preparing intents are never compacted. A consumable intent
// without its block payload is never consumed (a replica may not have the
// payload yet): it is kept and reported. The sweep never touches repair
// rows.
func sweepPublishedBlockReferenceRepairLivenessCleanups(database *db.DB, bucket int) error {
	intents, err := listPublishedBlockReferenceRepairLivenessCleanupsForBucketFn(database, bucket)
	if err != nil {
		return fmt.Errorf("list repair-owned liveness cleanup intents for bucket %d: %w", bucket, err)
	}
	now := publishedBlockReferenceRepairNowFn().UTC()
	groups := map[string][]publishedBlockReferenceRepair{}
	var order []string
	for _, intent := range intents {
		key := publishedBlockReferenceRepairRetryKey(intent)
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], intent)
	}
	var firstErr error
	report := func(intent publishedBlockReferenceRepair, err error) {
		log.Printf("[publish_repair] repair-owned liveness cleanup failed for repo=%s commit=%s fs_object=%s token=%s: %v", intent.RepoID, intent.CommitID, intent.FSID, intent.LivenessToken, err)
		if firstErr == nil {
			firstErr = err
		}
	}
	for _, key := range order {
		group := groups[key]
		gone, err := publishedBlockReferenceRepairGoneForCleanup(database, group[0])
		if err != nil {
			report(group[0], err)
			continue
		}
		if !gone {
			// Compaction of finished producers of a pending identity.
			var keep publishedBlockReferenceRepair
			for _, intent := range group {
				if intent.LivenessArmed && intent.LivenessLeaseExpiresAt.After(keep.LivenessLeaseExpiresAt) {
					keep = intent
				}
			}
			for _, intent := range group {
				if !intent.LivenessArmed || intent.LivenessToken == keep.LivenessToken {
					continue
				}
				if err := deletePublishedBlockReferenceRepairLivenessCleanupFn(database, intent); err != nil {
					report(intent, fmt.Errorf("compact repair-owned liveness cleanup intent: %w", err))
				}
			}
			continue
		}
		for _, intent := range group {
			if !publishedBlockReferenceRepairLivenessCleanupConsumable(intent, now) {
				continue
			}
			if len(db.NormalizeBlockIDs(intent.StagedBlockIDs)) == 0 {
				report(intent, fmt.Errorf("consumable cleanup intent has no block payload; retained (replica may not have it yet)"))
				continue
			}
			if err := removePublishedBlockReferenceRepairOwnedPubFn(database, intent); err != nil {
				report(intent, fmt.Errorf("remove repair-owned publish-attempt liveness after its repair row was gone: %w", err))
				continue
			}
			if err := deletePublishedBlockReferenceRepairLivenessCleanupFn(database, intent); err != nil {
				report(intent, fmt.Errorf("delete repair-owned liveness cleanup intent: %w", err))
			}
		}
	}
	return firstErr
}

func StartPublishedBlockReferenceRepairer(database *db.DB) {
	if database == nil {
		return
	}
	startPublishedBlockReferenceRepairWorkerOnce.Do(func() {
		schedulePublishedBlockReferenceRepairRunFn(func() {
			var lastOwnerSweepAt time.Time
			runOwnerSweep := func() {
				if err := runPendingPublishedFSObjectOwnerSweep(database); err != nil {
					log.Printf("[publish_repair] pending fs_object owner sweep failed: %v", err)
				}
				lastOwnerSweepAt = pendingPublishedFSObjectOwnerNowFn().UTC()
			}
			if err := runPublishedBlockReferenceRepairSweep(database); err != nil {
				log.Printf("[publish_repair] initial sweep failed: %v", err)
			}
			runOwnerSweep()
			ticker := publishedBlockReferenceRepairTickerFn(publishedBlockReferenceRepairSweepInterval)
			defer ticker.Stop()
			for range ticker.C {
				if err := runPublishedBlockReferenceRepairSweep(database); err != nil {
					log.Printf("[publish_repair] periodic sweep failed: %v", err)
				}
				if shouldRunPendingPublishedFSObjectOwnerSweep(lastOwnerSweepAt, pendingPublishedFSObjectOwnerNowFn().UTC()) {
					runOwnerSweep()
				}
			}
		})
	})
}

// SchedulePublishedBlockReferenceRepair runs one additional repair pass for a
// published fs_object's block references outside the request path. repairKey
// deduplicates concurrent schedulers for the same publish attempt.
func SchedulePublishedBlockReferenceRepair(repairKey, label string, repair func() error) {
	repairKey = strings.TrimSpace(repairKey)
	if repairKey == "" || repair == nil {
		return
	}
	if _, loaded := scheduledPublishedBlockReferenceRepairs.LoadOrStore(repairKey, struct{}{}); loaded {
		return
	}
	schedulePublishedBlockReferenceRepairRunFn(func() {
		defer scheduledPublishedBlockReferenceRepairs.Delete(repairKey)
		sleepFor := RetryBackoff(1)
		if sleepFor > 0 {
			schedulePublishedBlockReferenceRepairSleepFn(sleepFor)
		}
		if err := repair(); err != nil {
			log.Printf("[%s] WARNING: background block-reference repair failed for %s: %v", label, repairKey, err)
			return
		}
		log.Printf("[%s] INFO: background block-reference repair completed for %s", label, repairKey)
	})
}
