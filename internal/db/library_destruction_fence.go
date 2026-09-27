package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

var ErrInvalidDestructionFence = errors.New("invalid library destruction fence")
var ErrGlobalCanonicalAbsenceUnproven = errors.New("global canonical absence is unproven")
var ErrDestructionFenceBackpressure = errors.New("library has reached the unresolved destruction intent limit")

// MaxOutstandingDestructionIntents bounds each libraries-row Paxos payload.
// A saturated library may still take over or finish an existing token, so a
// crash-recovery path can always make progress without adding a new entry.
const MaxOutstandingDestructionIntents = 256

type DestructionFenceCaptureStatus uint8

const (
	DestructionFenceUnknown DestructionFenceCaptureStatus = iota
	DestructionFenceIdle
	DestructionFencePending
	DestructionFenceCanonicalAbsent
)

// DestructionFenceSnapshot is one authoritative observation of the canonical
// lifecycle fence. Epoch and Superseded are nil when Cassandra returned NULL.
// CreatedAt is the immutable, non-null row sentinel used to prevent an intent
// LWT from materializing a ghost libraries row.
type DestructionFenceSnapshot struct {
	Status     DestructionFenceCaptureStatus
	Head       string
	DeletedAt  *time.Time
	Epoch      *gocql.UUID
	Pending    map[uuid.UUID]gocql.UUID
	Superseded *gocql.UUID
	CreatedAt  time.Time
}

// DestructionIntentCapability is minted only by an APPLIED global-SERIAL
// BeginDestructionIntent for the exact library, durable token and generation.
// Its fields stay private so callers cannot manufacture destruction authority.
type DestructionIntentCapability struct {
	orgID      string
	libraryID  string
	token      uuid.UUID
	generation gocql.UUID
}

// GlobalCanonicalAbsenceProof is minted only when a global-SERIAL HEAD read
// itself observes no canonical row and a subsequent EACH_QUORUM read also
// observes absence. It is bound to exactly one organization/library pair.
type GlobalCanonicalAbsenceProof struct {
	orgID     string
	libraryID string
	minted    bool
}

type DestructionIntentOutcome uint8

const (
	DestructionIntentUnknown DestructionIntentOutcome = iota
	DestructionIntentNotApplied
	DestructionIntentApplied
)

type DestructionIntentResult struct {
	Outcome    DestructionIntentOutcome
	Capability *DestructionIntentCapability
}

func (c DestructionIntentCapability) Matches(orgID, libraryID string, token uuid.UUID, generation gocql.UUID) bool {
	return c.orgID == orgID && c.libraryID == libraryID && c.token == token && c.generation == generation
}

func maxSupersededGeneration(current, replaced *gocql.UUID) *gocql.UUID {
	if replaced == nil || current != nil && current.Timestamp() >= replaced.Timestamp() {
		return current
	}
	copy := *replaced
	return &copy
}

func validateDestructionFenceSnapshot(snapshot DestructionFenceSnapshot) error {
	if snapshot.CreatedAt.IsZero() {
		return fmt.Errorf("%w: canonical created_at sentinel is missing", ErrInvalidDestructionFence)
	}
	if snapshot.Status != DestructionFenceIdle && snapshot.Status != DestructionFencePending {
		return fmt.Errorf("%w: snapshot status %d is not a known canonical state", ErrInvalidDestructionFence, snapshot.Status)
	}
	if snapshot.Epoch != nil && snapshot.Epoch.Version() != 1 || snapshot.Superseded != nil && snapshot.Superseded.Version() != 1 {
		return fmt.Errorf("%w: epoch and superseded values must be timeuuid", ErrInvalidDestructionFence)
	}
	if snapshot.Epoch == nil && (len(snapshot.Pending) != 0 || snapshot.Superseded != nil) {
		return fmt.Errorf("%w: pending or superseded state exists without an epoch", ErrInvalidDestructionFence)
	}
	if snapshot.Superseded != nil && snapshot.Epoch != nil && snapshot.Superseded.Timestamp() >= snapshot.Epoch.Timestamp() {
		return fmt.Errorf("%w: superseded generation must be older than the current epoch", ErrInvalidDestructionFence)
	}
	if len(snapshot.Pending) == 0 && snapshot.Status != DestructionFenceIdle || len(snapshot.Pending) != 0 && snapshot.Status != DestructionFencePending {
		return fmt.Errorf("%w: status does not match pending map cardinality", ErrInvalidDestructionFence)
	}
	for token, generation := range snapshot.Pending {
		if token == uuid.Nil || generation.Version() != 1 || snapshot.Epoch == nil || generation.Timestamp() > snapshot.Epoch.Timestamp() {
			return fmt.Errorf("%w: malformed pending token or generation newer than epoch", ErrInvalidDestructionFence)
		}
	}
	return nil
}

func (p GlobalCanonicalAbsenceProof) Matches(orgID, libraryID string) bool {
	return p.minted && p.orgID == orgID && p.libraryID == libraryID
}

// ProveGlobalCanonicalAbsence requires both Paxos settlement and cross-DC
// absence. A SERIAL result that is present or unknown permanently vetoes this
// proof attempt, even if a later EACH_QUORUM read reports a miss.
func ProveGlobalCanonicalAbsence(ctx context.Context, session *gocql.Session, orgID, libraryID string) (GlobalCanonicalAbsenceProof, error) {
	if session == nil || orgID == "" || libraryID == "" {
		return GlobalCanonicalAbsenceProof{}, fmt.Errorf("%w: session, organization and library are required", ErrInvalidDestructionFence)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var head *string
	var createdAt *time.Time
	serialErr := session.Query(`
		SELECT head_commit_id, created_at FROM libraries
		WHERE org_id = ? AND library_id = ?
	`, orgID, libraryID).WithContext(ctx).Consistency(gocql.Serial).Scan(&head, &createdAt)
	if serialErr == nil {
		return GlobalCanonicalAbsenceProof{}, fmt.Errorf("%w: global SERIAL HEAD settlement observed a canonical row", ErrGlobalCanonicalAbsenceUnproven)
	}
	if !errors.Is(serialErr, gocql.ErrNotFound) {
		return GlobalCanonicalAbsenceProof{}, fmt.Errorf("%w: global SERIAL HEAD settlement failed: %v", ErrGlobalCanonicalAbsenceUnproven, serialErr)
	}
	var eachQuorumCreatedAt *time.Time
	eachErr := session.Query(`
		SELECT created_at FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, libraryID).WithContext(ctx).Consistency(gocql.EachQuorum).Scan(&eachQuorumCreatedAt)
	if eachErr == nil {
		return GlobalCanonicalAbsenceProof{}, fmt.Errorf("%w: EACH_QUORUM observed a canonical row", ErrGlobalCanonicalAbsenceUnproven)
	}
	if !errors.Is(eachErr, gocql.ErrNotFound) {
		return GlobalCanonicalAbsenceProof{}, fmt.Errorf("%w: EACH_QUORUM absence read failed: %v", ErrGlobalCanonicalAbsenceUnproven, eachErr)
	}
	return GlobalCanonicalAbsenceProof{orgID: orgID, libraryID: libraryID, minted: true}, nil
}

// CaptureDestructionFence reads E/P/S and the canonical-row sentinel in the
// same global SERIAL HEAD Paxos domain used by HEAD and witness LWTs.
func CaptureDestructionFence(ctx context.Context, session *gocql.Session, orgID, libraryID string) (DestructionFenceSnapshot, error) {
	if session == nil || orgID == "" || libraryID == "" {
		return DestructionFenceSnapshot{Status: DestructionFenceUnknown}, fmt.Errorf("%w: session, organization and library are required", ErrInvalidDestructionFence)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var head *string
	var deletedAt *time.Time
	var epoch *gocql.UUID
	var pending map[gocql.UUID]gocql.UUID
	var superseded *gocql.UUID
	var createdAt *time.Time
	err := session.Query(`
		SELECT head_commit_id, deleted_at, continuity_destruction_epoch,
			continuity_destruction_pending, continuity_destruction_superseded, created_at
		FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, libraryID).WithContext(ctx).Consistency(gocql.Serial).Scan(
		&head, &deletedAt, &epoch, &pending, &superseded, &createdAt,
	)
	if err != nil {
		return DestructionFenceSnapshot{Status: DestructionFenceUnknown}, err
	}
	if createdAt == nil || createdAt.IsZero() {
		return DestructionFenceSnapshot{Status: DestructionFenceUnknown}, fmt.Errorf("%w: canonical created_at sentinel is missing", ErrInvalidDestructionFence)
	}
	snapshot := DestructionFenceSnapshot{
		Status:     DestructionFenceIdle,
		Epoch:      epoch,
		Pending:    make(map[uuid.UUID]gocql.UUID, len(pending)),
		Superseded: superseded,
		CreatedAt:  createdAt.UTC(),
	}
	if head != nil {
		snapshot.Head = *head
	}
	if deletedAt != nil && !deletedAt.IsZero() {
		deletedCopy := deletedAt.UTC()
		snapshot.DeletedAt = &deletedCopy
	}
	for token, generation := range pending {
		googleToken, parseErr := uuid.FromBytes(token[:])
		if parseErr != nil || token == (gocql.UUID{}) || generation.Version() != 1 {
			return DestructionFenceSnapshot{Status: DestructionFenceUnknown}, fmt.Errorf("%w: malformed pending token or generation", ErrInvalidDestructionFence)
		}
		snapshot.Pending[googleToken] = generation
	}
	if len(snapshot.Pending) != 0 {
		snapshot.Status = DestructionFencePending
	}
	if err := validateDestructionFenceSnapshot(snapshot); err != nil {
		return DestructionFenceSnapshot{Status: DestructionFenceUnknown}, err
	}
	return snapshot, nil
}

// BeginDestructionIntent installs/replaces P[t], advances E, clears the
// witness, and records a replaced owner in S in one global-SERIAL LWT. The
// supplied snapshot must come from CaptureDestructionFence. Generation time
// validation is repeated here so callers cannot pass an unbounded timestamp.
func BeginDestructionIntent(ctx context.Context, session *gocql.Session, orgID, libraryID string, token uuid.UUID, generation gocql.UUID, observed DestructionFenceSnapshot) (DestructionIntentResult, error) {
	if session == nil || orgID == "" || libraryID == "" || token == uuid.Nil {
		return DestructionIntentResult{Outcome: DestructionIntentUnknown}, fmt.Errorf("%w: incomplete or non-canonical intent input", ErrInvalidDestructionFence)
	}
	if err := validateDestructionFenceSnapshot(observed); err != nil {
		return DestructionIntentResult{Outcome: DestructionIntentUnknown}, err
	}
	if _, ownsToken := observed.Pending[token]; !ownsToken && len(observed.Pending) >= MaxOutstandingDestructionIntents {
		return DestructionIntentResult{Outcome: DestructionIntentNotApplied}, ErrDestructionFenceBackpressure
	}
	if generation.Version() != 1 || observed.Epoch != nil && (observed.Epoch.Version() != 1 || generation.Timestamp() <= observed.Epoch.Timestamp()) {
		return DestructionIntentResult{Outcome: DestructionIntentUnknown}, fmt.Errorf("%w: generation must be a timeuuid newer than the observed epoch", ErrInvalidDestructionFence)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var oldOwner *gocql.UUID
	for existingToken, existingGeneration := range observed.Pending {
		if existingToken == token {
			owner := existingGeneration
			oldOwner = &owner
			break
		}
	}
	superseded := maxSupersededGeneration(observed.Superseded, oldOwner)
	var oldEpoch any
	if observed.Epoch != nil {
		oldEpoch = *observed.Epoch
	}
	var oldSuperseded any
	if observed.Superseded != nil {
		oldSuperseded = *observed.Superseded
	}
	var oldOwnerValue any
	if oldOwner != nil {
		oldOwnerValue = *oldOwner
	}
	var nextSuperseded any
	if superseded != nil {
		nextSuperseded = *superseded
	}
	state := map[string]interface{}{}
	applied, err := session.Query(`
		UPDATE libraries
		SET continuity_destruction_epoch = ?,
			continuity_destruction_pending[?] = ?,
			continuity_destruction_superseded = ?,
			continuity_certified_head_commit_id = null,
			continuity_contract_version = null
		WHERE org_id = ? AND library_id = ?
		IF continuity_destruction_epoch = ?
		AND continuity_destruction_pending[?] = ?
		AND continuity_destruction_superseded = ?
		AND created_at = ?
	`, generation, gocql.UUID(token), generation, nextSuperseded, orgID, libraryID,
		oldEpoch, gocql.UUID(token), oldOwnerValue, oldSuperseded, observed.CreatedAt).
		WithContext(ctx).SerialConsistency(LibraryHeadSerialConsistency).MapScanCAS(state)
	if err != nil {
		return DestructionIntentResult{Outcome: DestructionIntentUnknown}, fmt.Errorf("begin destruction intent: %w", err)
	}
	if !applied {
		return DestructionIntentResult{Outcome: DestructionIntentNotApplied}, nil
	}
	return DestructionIntentResult{
		Outcome: DestructionIntentApplied,
		Capability: &DestructionIntentCapability{
			orgID: orgID, libraryID: libraryID, token: token, generation: generation,
		},
	}, nil
}

// CompleteDestructionIntent removes only the map entry still owned by this
// generation. A timeout or conditional miss never clears another generation.
func CompleteDestructionIntent(ctx context.Context, session *gocql.Session, capability DestructionIntentCapability) (bool, error) {
	if session == nil || capability.orgID == "" || capability.libraryID == "" || capability.token == uuid.Nil || capability.generation.Version() != 1 {
		return false, fmt.Errorf("%w: invalid completion capability", ErrInvalidDestructionFence)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	state := map[string]interface{}{}
	applied, err := session.Query(`
		DELETE continuity_destruction_pending[?] FROM libraries
		WHERE org_id = ? AND library_id = ?
		IF continuity_destruction_pending[?] = ?
	`, gocql.UUID(capability.token), capability.orgID, capability.libraryID,
		gocql.UUID(capability.token), capability.generation).
		WithContext(ctx).SerialConsistency(LibraryHeadSerialConsistency).MapScanCAS(state)
	if err != nil {
		return false, fmt.Errorf("complete destruction intent: %w", err)
	}
	return applied, nil
}
