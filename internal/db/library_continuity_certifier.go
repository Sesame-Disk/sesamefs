package db

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/metrics"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

type LibraryBaselineCertificationOutcome string

const (
	LibraryBaselineCertificationCertified    LibraryBaselineCertificationOutcome = "CERTIFIED"
	LibraryBaselineCertificationNotCertified LibraryBaselineCertificationOutcome = "NOT_CERTIFIED"
	LibraryBaselineCertificationUnknown      LibraryBaselineCertificationOutcome = "UNKNOWN"
)

type LibraryBaselineCertificationReason string

const (
	LibraryBaselineReasonApplied                      LibraryBaselineCertificationReason = "witness_applied"
	LibraryBaselineReasonInvalidInput                 LibraryBaselineCertificationReason = "invalid_input"
	LibraryBaselineReasonLibraryNotFound              LibraryBaselineCertificationReason = "library_not_found"
	LibraryBaselineReasonLibraryDeleted               LibraryBaselineCertificationReason = "library_deleted"
	LibraryBaselineReasonHeadChanged                  LibraryBaselineCertificationReason = "head_changed"
	LibraryBaselineReasonMissingCommit                LibraryBaselineCertificationReason = "missing_commit"
	LibraryBaselineReasonMissingFSObject              LibraryBaselineCertificationReason = "missing_fs_object"
	LibraryBaselineReasonIncompleteFSObject           LibraryBaselineCertificationReason = "incomplete_fs_object"
	LibraryBaselineReasonIdentityUnproven             LibraryBaselineCertificationReason = "identity_unproven"
	LibraryBaselineReasonIdentityConflict             LibraryBaselineCertificationReason = "identity_conflict"
	LibraryBaselineReasonIdentityAuthorityUnavailable LibraryBaselineCertificationReason = "identity_authority_unavailable"
	LibraryBaselineReasonMissingBlock                 LibraryBaselineCertificationReason = "missing_block"
	LibraryBaselineReasonMissingBlockMapping          LibraryBaselineCertificationReason = "missing_block_mapping"
	LibraryBaselineReasonMalformedTree                LibraryBaselineCertificationReason = "malformed_tree"
	LibraryBaselineReasonTraversalLimit               LibraryBaselineCertificationReason = "traversal_limit"
	LibraryBaselineReasonLegacyLocator                LibraryBaselineCertificationReason = "legacy_locator"
	LibraryBaselineReasonMalformedLocator             LibraryBaselineCertificationReason = "malformed_locator"
	LibraryBaselineReasonPhysicalIncarnationChanged   LibraryBaselineCertificationReason = "physical_incarnation_changed"
	LibraryBaselineReasonGCAuthorityConflict          LibraryBaselineCertificationReason = "gc_authority_conflict"
	LibraryBaselineReasonLivenessWriteFailed          LibraryBaselineCertificationReason = "liveness_write_failed"
	LibraryBaselineReasonLivenessReadFailed           LibraryBaselineCertificationReason = "liveness_read_failed"
	LibraryBaselineReasonLivenessNotVisible           LibraryBaselineCertificationReason = "liveness_not_visible"
	LibraryBaselineReasonPhysicalBytesMissing         LibraryBaselineCertificationReason = "physical_bytes_missing"
	LibraryBaselineReasonPhysicalStorageUnavailable   LibraryBaselineCertificationReason = "physical_storage_unavailable"

	LibraryBaselineReasonDependencyReadFailed LibraryBaselineCertificationReason = "dependency_read_failed"
	LibraryBaselineReasonWitnessNotApplied    LibraryBaselineCertificationReason = "witness_not_applied"
	LibraryBaselineReasonWitnessSettled       LibraryBaselineCertificationReason = "witness_settled"
	LibraryBaselineReasonWitnessUnknown       LibraryBaselineCertificationReason = "witness_unknown"
)

type LibraryBaselineCertificationLimits struct {
	MaxDepth           int
	MaxFSObjects       int
	MaxTreeEdges       int
	MaxBlockReferences int
	MaxUniqueBlocks    int
}

var DefaultLibraryBaselineCertificationLimits = LibraryBaselineCertificationLimits{
	MaxDepth:           64,
	MaxFSObjects:       100000,
	MaxTreeEdges:       200000,
	MaxBlockReferences: 1000000,
	MaxUniqueBlocks:    1000000,
}

type LibraryBaselineCertificationResult struct {
	Outcome                 LibraryBaselineCertificationOutcome
	Reason                  LibraryBaselineCertificationReason
	Diagnostic              error
	ObservedHead            string
	CommitsWalked           int
	FSObjectsWalked         int
	UniqueBlocks            int
	PermanentLivenessWrites int
	PhysicalRevalidations   int
}

var (
	errContinuityMissingCommit       = errors.New("continuity commit is missing")
	errContinuityMissingFSObject     = errors.New("continuity fs object is missing")
	errContinuityIncompleteFSObject  = errors.New("continuity fs object is incomplete")
	errContinuityMissingBlockMapping = errors.New("continuity block mapping is missing")
	errContinuityIdentityUnproven    = errors.New("continuity metadata identity is unproven")
	errContinuityIdentityConflict    = errors.New("continuity metadata identity conflicts with authority")
	errContinuityIdentityUnavailable = errors.New("continuity metadata identity authority is unavailable")
	errContinuityMalformedTree       = errors.New("continuity tree is malformed")
	errContinuityTraversalLimit      = errors.New("continuity traversal limit exceeded")
	errContinuityMalformedLocator    = errors.New("continuity locator is malformed")
)

func (l LibraryBaselineCertificationLimits) validate() error {
	if l.MaxDepth <= 0 || l.MaxFSObjects <= 0 || l.MaxTreeEdges <= 0 ||
		l.MaxBlockReferences <= 0 || l.MaxUniqueBlocks <= 0 {
		return fmt.Errorf("library baseline certification limits must be finite and positive")
	}
	return nil
}

func certificationResult(outcome LibraryBaselineCertificationOutcome, reason LibraryBaselineCertificationReason, diagnostic error, observedHead string) LibraryBaselineCertificationResult {
	return LibraryBaselineCertificationResult{
		Outcome:      outcome,
		Reason:       reason,
		Diagnostic:   diagnostic,
		ObservedHead: observedHead,
	}
}

func (r *LibraryBaselineCertificationResult) finish(outcome LibraryBaselineCertificationOutcome, reason LibraryBaselineCertificationReason, diagnostic error) {
	r.Outcome = outcome
	r.Reason = reason
	r.Diagnostic = diagnostic
}

type continuityDependencies struct {
	fsByBlock   map[string]map[string]struct{}
	projections map[string]FSObjectProjection
	fsObjects   int
	// sha1Mappings records each SHA-1-only dependency's durable mapping
	// authority so the pre-witness pass can recheck the mutable row against it.
	sha1Mappings map[string]string
}

type continuityDirectoryEntry struct {
	ID string
}

type continuityFSObject struct {
	ObjectType             string
	ObjectTypePresent      bool
	SizeBytes              int64
	SizeBytesPresent       bool
	DirectoryEntry         string
	DirectoryEntryPresent  bool
	BlockIDs               []string
	BlockIDsPresent        bool
	SeafileBlockIDs        []string
	SeafileBlockIDsPresent bool
}

type continuityTreeWalker struct {
	db             *DB
	ctx            context.Context
	orgID          string
	libraryID      string
	representation string
	limits         LibraryBaselineCertificationLimits
	visited        map[string]struct{}
	active         map[string]struct{}
	dependencies   continuityDependencies
	treeEdges      int
	blockRefs      int
	// mappingAuthority resolves SHA-1-only dependencies. Nil means no mapping
	// authority is reachable, so every SHA-1-only dependency stays unproven.
	mappingAuthority continuityMappingAuthority
	mappingResolved  map[string]string
}

// continuityMappingAuthority is the certifier's view of the PC-D1B.3 Mapping
// Authority: cold-path acquisition of the durable claim, plus a read of the
// mutable mapping, which must agree with the claim or be absent and is never
// consumed as the resolution.
type continuityMappingAuthority interface {
	promote(ctx context.Context, orgID, representationID, externalID string) (BlockMappingPromotionResult, error)
	readMutable(ctx context.Context, orgID, representationID, externalID string) (string, bool, error)
}

type dbContinuityMappingAuthority struct {
	db             *DB
	storageManager *storage.Manager
}

func (a dbContinuityMappingAuthority) promote(ctx context.Context, orgID, representationID, externalID string) (BlockMappingPromotionResult, error) {
	return a.db.PromoteBlockMappingAuthority(ctx, a.storageManager, orgID, representationID, externalID)
}

func (a dbContinuityMappingAuthority) readMutable(ctx context.Context, orgID, representationID, externalID string) (string, bool, error) {
	return a.db.GetBlockIDMappingContext(ctx, orgID, representationID, externalID)
}

type libraryBaselineCertifierTestHooksContextKey struct{}

type libraryBaselineCertifierTestHooks struct {
	afterLiveness    func(context.Context, string, string, BlockPhysicalLocation)
	beforeWitnessCAS func(context.Context, string, string, string)
	afterWitnessCAS  func(LibraryContinuityCASResult, error) (LibraryContinuityCASResult, error)
}

func (db *DB) CertifyLibraryBaseline(ctx context.Context, storageManager *storage.Manager, orgID, libraryID, observedHead string) (result LibraryBaselineCertificationResult) {
	started := time.Now()
	result = certificationResult(LibraryBaselineCertificationUnknown, LibraryBaselineReasonDependencyReadFailed, nil, observedHead)
	defer func() {
		metrics.LibraryContinuityCertificationRunsTotal.WithLabelValues(string(result.Outcome), string(result.Reason)).Inc()
		metrics.LibraryContinuityCertificationDuration.Observe(time.Since(started).Seconds())
		metrics.LibraryContinuityCertificationCommitsWalkedTotal.Add(float64(result.CommitsWalked))
		metrics.LibraryContinuityCertificationFSObjectsWalkedTotal.Add(float64(result.FSObjectsWalked))
		metrics.LibraryContinuityCertificationUniqueBlocksTotal.Add(float64(result.UniqueBlocks))
		metrics.LibraryContinuityCertificationPermanentLivenessWritesTotal.Add(float64(result.PermanentLivenessWrites))
		metrics.LibraryContinuityCertificationPhysicalRevalidationsTotal.Add(float64(result.PhysicalRevalidations))
	}()

	if ctx == nil {
		ctx = context.Background()
	}
	testHooks, _ := ctx.Value(libraryBaselineCertifierTestHooksContextKey{}).(libraryBaselineCertifierTestHooks)
	if err := validateLibraryContinuityInput(orgID, libraryID, observedHead, SupportedContinuityContractVersion); err != nil {
		result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonInvalidInput, err)
		return result
	}
	if db == nil || db.Session() == nil {
		result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonDependencyReadFailed, fmt.Errorf("database session unavailable"))
		return result
	}
	if err := DefaultLibraryBaselineCertificationLimits.validate(); err != nil {
		result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonInvalidInput, err)
		return result
	}

	state, err := ReadLibraryStateContext(ctx, db.Session(), orgID, libraryID)
	if err != nil {
		if errors.Is(err, gocql.ErrNotFound) {
			result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonLibraryNotFound, err)
		} else {
			result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonDependencyReadFailed, err)
		}
		return result
	}
	if state.DeletedAt != nil {
		result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonLibraryDeleted, ErrLibraryDeleted)
		return result
	}
	if state.HeadCommitID != observedHead {
		result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonHeadChanged, fmt.Errorf("current HEAD is %q", state.HeadCommitID))
		return result
	}

	representationID, err := CanonicalBlockRepresentationIDForLibrary(state.LibraryID, state.Encrypted, state.BlockRepresentationID)
	if err != nil {
		result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonMalformedTree, err)
		return result
	}
	commitProjection, err := readContinuityCommitProjectionContext(ctx, db, libraryID, observedHead)
	if err != nil {
		outcome, reason := classifyContinuityDependencyError(err)
		result.finish(outcome, reason, err)
		return result
	}
	rootFSID := commitProjection.RootFSID
	result.CommitsWalked = 1

	mappingAuthority := dbContinuityMappingAuthority{db: db, storageManager: storageManager}
	dependencies, err := db.walkContinuityTree(ctx, orgID, libraryID, representationID, rootFSID, DefaultLibraryBaselineCertificationLimits, mappingAuthority)
	result.FSObjectsWalked = dependencies.fsObjects
	result.UniqueBlocks = len(dependencies.fsByBlock)
	if err != nil {
		outcome, reason := classifyContinuityDependencyError(err)
		result.finish(outcome, reason, err)
		return result
	}

	blockIDs := make([]string, 0, len(dependencies.fsByBlock))
	for blockID := range dependencies.fsByBlock {
		blockIDs = append(blockIDs, blockID)
	}
	sort.Strings(blockIDs)

	physicalLocations := make(map[string]BlockPhysicalLocation, len(blockIDs))
	for _, blockID := range blockIDs {
		expected, found, err := readContinuityPhysicalLocationContext(ctx, db, orgID, blockID)
		if err != nil {
			if errors.Is(err, errContinuityMalformedLocator) {
				result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonMalformedLocator, err)
			} else {
				result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonDependencyReadFailed, err)
			}
			return result
		}
		if !found {
			result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonMissingBlock, fmt.Errorf("block %s is missing", blockID))
			return result
		}
		physicalLocations[blockID] = expected
		if err := validateContinuityPhysicalAuthorityInput(blockID, expected); err != nil {
			result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonMalformedLocator, err)
			return result
		}
		if storageManager == nil {
			result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonPhysicalStorageUnavailable, fmt.Errorf("storage manager unavailable for physical class %s", expected.StorageClass))
			return result
		}
		blockStore, err := storageManager.GetBlockStoreForOrg(orgID, expected.StorageClass)
		if err != nil {
			result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonPhysicalStorageUnavailable, fmt.Errorf("resolve physical storage class %s: %w", expected.StorageClass, err))
			return result
		}
		if mintedErr := blockStore.ValidateMintedPhysicalLocator(blockID, expected.StorageKey); mintedErr != nil {
			reason := LibraryBaselineReasonMalformedLocator
			// ValidatePhysicalLocator accepts the legacy base only after the stricter
			// minted check fails, without deriving a physical key as authority here.
			if blockStore.ValidatePhysicalLocator(blockID, expected.StorageKey) == nil {
				reason = LibraryBaselineReasonLegacyLocator
			}
			result.finish(LibraryBaselineCertificationNotCertified, reason, mintedErr)
			return result
		}

		fsIDs := make([]string, 0, len(dependencies.fsByBlock[blockID]))
		for fsID := range dependencies.fsByBlock[blockID] {
			fsIDs = append(fsIDs, fsID)
		}
		sort.Strings(fsIDs)
		for _, fsID := range fsIDs {
			referrer := BlockReferrerForFSObject(libraryID, fsID)
			permanent, err := db.BlockReferencePermanentExistsEachQuorumContext(ctx, orgID, blockID, referrer, libraryID)
			if err != nil {
				result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonLivenessReadFailed, err)
				return result
			}
			if permanent {
				continue
			}
			if err := db.AddBlockReferenceContext(ctx, orgID, blockID, referrer, libraryID, 0); err != nil {
				result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonLivenessWriteFailed, err)
				return result
			}
			result.PermanentLivenessWrites++
			permanent, err = db.BlockReferencePermanentExistsEachQuorumContext(ctx, orgID, blockID, referrer, libraryID)
			if err != nil {
				result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonLivenessReadFailed, err)
				return result
			}
			if !permanent {
				result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonLivenessNotVisible, fmt.Errorf("reference %s for block %s is not visible at EACH_QUORUM after permanent write", referrer, blockID))
				return result
			}
		}

		if testHooks.afterLiveness != nil {
			testHooks.afterLiveness(ctx, orgID, blockID, expected)
		}

		result.PhysicalRevalidations++
		authority, authorityErr := db.ValidateLibraryContinuityPhysicalAuthorityContext(ctx, orgID, blockID, expected)
		switch authority {
		case BlockRepairAuthorityAuthorized:
		case BlockRepairAuthorityChanged:
			result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonPhysicalIncarnationChanged, authorityErr)
			return result
		case BlockRepairAuthorityBlocked:
			result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonGCAuthorityConflict, authorityErr)
			return result
		case BlockRepairAuthorityPermanent:
			result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonMalformedLocator, authorityErr)
			return result
		default:
			if authorityErr == nil {
				authorityErr = fmt.Errorf("physical authority for block %s is unknown", blockID)
			}
			result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonDependencyReadFailed, authorityErr)
			return result
		}
		physicalExists, err := blockStore.ObjectExists(ctx, expected.StorageKey)
		if err != nil {
			result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonPhysicalStorageUnavailable, fmt.Errorf("check physical object %s/%s: %w", expected.StorageClass, expected.StorageKey, err))
			return result
		}
		if !physicalExists {
			result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonPhysicalBytesMissing, fmt.Errorf("physical object %s/%s is missing", expected.StorageClass, expected.StorageKey))
			return result
		}

		for _, fsID := range fsIDs {
			referrer := BlockReferrerForFSObject(libraryID, fsID)
			permanent, err := db.BlockReferencePermanentExistsEachQuorumContext(ctx, orgID, blockID, referrer, libraryID)
			if err != nil {
				result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonLivenessReadFailed, err)
				return result
			}
			if !permanent {
				result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonLivenessNotVisible, fmt.Errorf("reference %s for block %s is not visible at EACH_QUORUM", referrer, blockID))
				return result
			}
		}
	}

	// Recheck the HEAD before the final authority pass. The witness CAS below
	// still fences a concurrent advance after this read.
	finalState, err := ReadLibraryStateContext(ctx, db.Session(), orgID, libraryID)
	if err != nil {
		if errors.Is(err, gocql.ErrNotFound) {
			result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonLibraryNotFound, err)
		} else {
			result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonDependencyReadFailed, err)
		}
		return result
	}
	if finalState.DeletedAt != nil {
		result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonLibraryDeleted, ErrLibraryDeleted)
		return result
	}
	if finalState.HeadCommitID != observedHead {
		result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonHeadChanged, fmt.Errorf("current HEAD is %q", finalState.HeadCommitID))
		return result
	}

	for _, blockID := range blockIDs {
		expected := physicalLocations[blockID]
		current, found, readErr := readContinuityPhysicalLocationContext(ctx, db, orgID, blockID)
		if readErr != nil {
			if errors.Is(readErr, errContinuityMalformedLocator) {
				result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonMalformedLocator, readErr)
			} else {
				result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonDependencyReadFailed, readErr)
			}
			return result
		}
		if !found || current != expected {
			result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonPhysicalIncarnationChanged, fmt.Errorf("physical placement for block %s changed before witness", blockID))
			return result
		}
		result.PhysicalRevalidations++
		authority, authorityErr := db.ValidateLibraryContinuityPhysicalAuthorityContext(ctx, orgID, blockID, expected)
		switch authority {
		case BlockRepairAuthorityAuthorized:
		case BlockRepairAuthorityChanged:
			result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonPhysicalIncarnationChanged, authorityErr)
			return result
		case BlockRepairAuthorityBlocked:
			result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonGCAuthorityConflict, authorityErr)
			return result
		case BlockRepairAuthorityPermanent:
			result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonMalformedLocator, authorityErr)
			return result
		default:
			if authorityErr == nil {
				authorityErr = fmt.Errorf("physical authority for block %s is unknown before witness", blockID)
			}
			result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonDependencyReadFailed, authorityErr)
			return result
		}
		store, storeErr := storageManager.GetBlockStoreForOrg(orgID, expected.StorageClass)
		if storeErr != nil {
			result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonPhysicalStorageUnavailable, fmt.Errorf("resolve final physical storage class %s: %w", expected.StorageClass, storeErr))
			return result
		}
		physicalExists, existsErr := store.ObjectExists(ctx, expected.StorageKey)
		if existsErr != nil {
			result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonPhysicalStorageUnavailable, fmt.Errorf("recheck physical object %s/%s: %w", expected.StorageClass, expected.StorageKey, existsErr))
			return result
		}
		if !physicalExists {
			result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonPhysicalBytesMissing, fmt.Errorf("physical object %s/%s disappeared before witness", expected.StorageClass, expected.StorageKey))
			return result
		}
		for fsID := range dependencies.fsByBlock[blockID] {
			referrer := BlockReferrerForFSObject(libraryID, fsID)
			permanent, livenessErr := db.BlockReferencePermanentExistsEachQuorumContext(ctx, orgID, blockID, referrer, libraryID)
			if livenessErr != nil {
				result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonLivenessReadFailed, livenessErr)
				return result
			}
			if !permanent {
				result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonLivenessNotVisible, fmt.Errorf("reference %s for block %s is not visible at EACH_QUORUM before witness", referrer, blockID))
				return result
			}
		}
	}

	currentCommit, err := readContinuityCommitProjectionContext(ctx, db, libraryID, observedHead)
	if err != nil {
		outcome, reason := classifyContinuityDependencyError(err)
		result.finish(outcome, reason, err)
		return result
	}
	if !sameContinuityCommitProjection(commitProjection, currentCommit) {
		result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonIdentityConflict, errContinuityIdentityConflict)
		return result
	}
	fsObjectIDs := make([]string, 0, len(dependencies.projections))
	for fsID := range dependencies.projections {
		fsObjectIDs = append(fsObjectIDs, fsID)
	}
	sort.Strings(fsObjectIDs)
	for _, fsID := range fsObjectIDs {
		currentProjection, verifyErr := readContinuityFSObjectProjectionContext(ctx, db, libraryID, fsID)
		if verifyErr != nil {
			outcome, reason := classifyContinuityDependencyError(verifyErr)
			result.finish(outcome, reason, verifyErr)
			return result
		}
		if !sameContinuityFSObjectProjection(dependencies.projections[fsID], currentProjection) {
			result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonIdentityConflict, fmt.Errorf("fs_object %s changed before witness: %w", fsID, errContinuityIdentityConflict))
			return result
		}
	}
	if err := revalidateContinuityMappingAuthority(ctx, mappingAuthority, orgID, representationID, dependencies.sha1Mappings); err != nil {
		outcome, reason := classifyContinuityDependencyError(err)
		result.finish(outcome, reason, err)
		return result
	}

	if testHooks.beforeWitnessCAS != nil {
		testHooks.beforeWitnessCAS(ctx, orgID, libraryID, observedHead)
	}
	cas, casErr := CommitLibraryContinuityWitnessContext(ctx, db.Session(), orgID, libraryID, observedHead, SupportedContinuityContractVersion)
	if testHooks.afterWitnessCAS != nil {
		cas, casErr = testHooks.afterWitnessCAS(cas, casErr)
	}
	if casErr != nil || cas.Outcome == LibraryContinuityCASUnknown {
		settlement, settleErr := settleLibraryContinuityWitnessContext(ctx, db, orgID, libraryID, observedHead)
		if settleErr != nil {
			result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonWitnessUnknown, errors.Join(casErr, settleErr))
			return result
		}
		if settlement.Certified {
			result.finish(LibraryBaselineCertificationCertified, LibraryBaselineReasonWitnessSettled, casErr)
			return result
		}
		if !settlement.Found {
			result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonLibraryNotFound, casErr)
			return result
		}
		if settlement.Deleted {
			result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonLibraryDeleted, casErr)
			return result
		}
		if settlement.Head != observedHead {
			result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonHeadChanged, casErr)
			return result
		}
		result.finish(LibraryBaselineCertificationNotCertified, LibraryBaselineReasonWitnessNotApplied, casErr)
		return result
	}
	if cas.Outcome == LibraryContinuityCASNotApplied {
		reason := LibraryBaselineReasonWitnessNotApplied
		if cas.CurrentHeadCommitID != "" && cas.CurrentHeadCommitID != observedHead {
			reason = LibraryBaselineReasonHeadChanged
		}
		result.finish(LibraryBaselineCertificationNotCertified, reason, nil)
		return result
	}
	if cas.Outcome != LibraryContinuityCASApplied {
		result.finish(LibraryBaselineCertificationUnknown, LibraryBaselineReasonWitnessUnknown, fmt.Errorf("unexpected witness CAS outcome %d", cas.Outcome))
		return result
	}

	result.finish(LibraryBaselineCertificationCertified, LibraryBaselineReasonApplied, nil)
	return result
}

func readContinuityCommitProjectionContext(ctx context.Context, database *DB, libraryID, commitID string) (CommitProjection, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(commitID) == "" {
		return CommitProjection{}, fmt.Errorf("%w: empty commit id", errContinuityMalformedTree)
	}
	row, err := ReadCommitIdentitySourceRow(ctx, database.Session(), libraryID, commitID)
	if err != nil {
		if errors.Is(err, gocql.ErrNotFound) {
			return CommitProjection{}, fmt.Errorf("%w: %s", errContinuityMissingCommit, commitID)
		}
		return CommitProjection{}, fmt.Errorf("%w: read strict commit projection %s: %v", errContinuityIdentityUnavailable, commitID, err)
	}
	projection, err := commitProjectionFromIdentitySourceRow(libraryID, commitID, row)
	if err != nil {
		return CommitProjection{}, fmt.Errorf("%w: commit %s has an incomplete source projection: %v", errContinuityIdentityConflict, commitID, err)
	}
	if projection.RootFSID == "" {
		return CommitProjection{}, fmt.Errorf("%w: commit %s has an empty root_fs_id", errContinuityMalformedTree, commitID)
	}
	outcome, err := VerifyCommitProjection(ctx, database.Session(), projection)
	if err := requireContinuityIdentityVerification("commit "+commitID, outcome, err); err != nil {
		return CommitProjection{}, err
	}
	if err := validateContinuityFSID(projection.RootFSID); err != nil {
		return CommitProjection{}, err
	}
	return projection, nil
}

// continuityEmptyFSID is Seafile's EMPTY_SHA1: an empty library root, empty
// directory, or empty file.
const continuityEmptyFSID = "0000000000000000000000000000000000000000"

// fs_id values are SHA-1 object identities. Once the commit or directory
// projection has been verified, traversal must use that exact primary key;
// normalizing it could redirect the walk to a different fs_objects row.
func validateContinuityFSID(fsID string) error {
	if len(fsID) != 40 || strings.ToLower(fsID) != fsID {
		return fmt.Errorf("%w: fs object id %q is not a canonical 40-character SHA-1 id", errContinuityMalformedTree, fsID)
	}
	if _, err := hex.DecodeString(fsID); err != nil {
		return fmt.Errorf("%w: fs object id %q is not a canonical 40-character SHA-1 id", errContinuityMalformedTree, fsID)
	}
	return nil
}
func requireContinuityIdentityVerification(identity string, outcome IdentityVerificationOutcome, err error) error {
	if err != nil {
		if errors.Is(err, IdentityAuthorityConflict) {
			return fmt.Errorf("%w: %s: %v", errContinuityIdentityConflict, identity, err)
		}
		return fmt.Errorf("%w: verify %s: %v", errContinuityIdentityUnavailable, identity, err)
	}
	switch outcome {
	case IdentityVerificationVerified:
		return nil
	case IdentityVerificationUnproven:
		return fmt.Errorf("%w: no durable identity claim for %s", errContinuityIdentityUnproven, identity)
	case IdentityVerificationConflict:
		return fmt.Errorf("%w: durable identity claim disagrees for %s", errContinuityIdentityConflict, identity)
	default:
		return fmt.Errorf("%w: verification outcome %s for %s", errContinuityIdentityUnavailable, outcome, identity)
	}
}

func readContinuityFSObjectProjectionContext(ctx context.Context, database *DB, libraryID, fsID string) (FSObjectProjection, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateContinuityFSID(fsID); err != nil {
		return FSObjectProjection{}, err
	}
	row, err := ReadFSObjectIdentitySourceRow(ctx, database.Session(), libraryID, fsID)
	if err != nil {
		if errors.Is(err, gocql.ErrNotFound) {
			return FSObjectProjection{}, fmt.Errorf("%w: %s", errContinuityMissingFSObject, fsID)
		}
		return FSObjectProjection{}, fmt.Errorf("%w: read strict fs_object projection %s: %v", errContinuityIdentityUnavailable, fsID, err)
	}
	projection, placeholder, err := continuityFSObjectProjectionFromSourceRow(libraryID, fsID, row)
	if err != nil {
		if typ, ok := identitySourceText(row, "obj_type"); ok && typ == "file" {
			if completenessErr := continuityFileSourceCompleteness(row, fsID); completenessErr != nil {
				return FSObjectProjection{}, completenessErr
			}
		}
		return FSObjectProjection{}, fmt.Errorf("%w: fs_object %s has an invalid source projection: %v", errContinuityIdentityConflict, fsID, err)
	}
	if placeholder {
		return FSObjectProjection{}, fmt.Errorf("%w: reachable fs_object %s has no semantic source fields", errContinuityIncompleteFSObject, fsID)
	}
	outcome, err := VerifyFSObjectProjection(ctx, database.Session(), projection)
	if err := requireContinuityIdentityVerification("fs_object "+fsID, outcome, err); err != nil {
		return FSObjectProjection{}, err
	}
	return projection, nil
}

func continuityFSObjectProjectionFromSourceRow(libraryID, fsID string, row map[string]interface{}) (FSObjectProjection, bool, error) {
	if identitySourceRowIsEmptyFile(row) {
		return FSObjectProjection{
			LibraryID: libraryID, FSID: fsID, ObjectType: "file", SizeBytes: 0,
			FileLayout: FileStorageSHA1Only, LogicalSHA1IDs: []string{},
		}, false, nil
	}
	return fsObjectProjectionFromIdentitySourceRow(libraryID, fsID, row)
}
func continuityFileSourceCompleteness(row map[string]interface{}, fsID string) error {
	_, sizePresent := identitySourceInt64(row, "size_bytes")
	blockIDs, blockIDsPresent := identitySourceStringSlice(row, "block_ids")
	externalIDs, externalPresent := identitySourceStringSlice(row, "seafile_block_ids_sha1")
	hasLogicalIDs := blockIDsPresent || externalPresent && len(externalIDs) > 0
	if sizePresent && hasLogicalIDs {
		return nil
	}
	missing := make([]string, 0, 2)
	if !sizePresent {
		missing = append(missing, "size_bytes")
	}
	if !hasLogicalIDs {
		missing = append(missing, "block_ids or non-empty seafile_block_ids_sha1")
	}
	_ = blockIDs
	return fmt.Errorf("%w: file %s is missing %s", errContinuityIncompleteFSObject, fsID, strings.Join(missing, " and "))
}

func sameContinuityCommitProjection(left, right CommitProjection) bool {
	leftDigest, leftErr := CommitIdentityDigest(left.LibraryID, left.CommitID, left.ParentID, left.RootFSID, left.CreatorID, left.Description, left.CreatedAt)
	rightDigest, rightErr := CommitIdentityDigest(right.LibraryID, right.CommitID, right.ParentID, right.RootFSID, right.CreatorID, right.Description, right.CreatedAt)
	return leftErr == nil && rightErr == nil && leftDigest == rightDigest
}

func sameContinuityFSObjectProjection(left, right FSObjectProjection) bool {
	_, leftDigest, leftErr := validateFSObjectProjection(left)
	_, rightDigest, rightErr := validateFSObjectProjection(right)
	return leftErr == nil && rightErr == nil && leftDigest == rightDigest
}

func (database *DB) walkContinuityTree(ctx context.Context, orgID, libraryID, representationID, rootFSID string, limits LibraryBaselineCertificationLimits, mappingAuthority continuityMappingAuthority) (continuityDependencies, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	dependencies := continuityDependencies{
		fsByBlock:   make(map[string]map[string]struct{}),
		projections: make(map[string]FSObjectProjection),
	}
	if err := limits.validate(); err != nil {
		return dependencies, err
	}
	walker := &continuityTreeWalker{
		db:             database,
		ctx:            ctx,
		orgID:          orgID,
		libraryID:      libraryID,
		representation: representationID,
		limits:         limits,
		visited:        make(map[string]struct{}),
		active:         make(map[string]struct{}),
		dependencies:   dependencies,

		mappingAuthority: mappingAuthority,
		mappingResolved:  make(map[string]string),
	}
	if err := walker.visit(rootFSID, 0); err != nil {
		return walker.dependencies, err
	}
	walker.dependencies.sha1Mappings = walker.mappingResolved
	return walker.dependencies, nil
}

func (w *continuityTreeWalker) visit(fsID string, depth int) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	if err := validateContinuityFSID(fsID); err != nil {
		return err
	}
	// EMPTY_SHA1 is Seafile's canonical empty directory/file identity. Clients
	// never upload it, so it has no fs_objects row or fs_object claim: the
	// verified parent (commit root or directory entry) is the only authority
	// and the reachable content below it is empty.
	if fsID == continuityEmptyFSID {
		return nil
	}
	if depth > w.limits.MaxDepth {
		return fmt.Errorf("%w: depth %d exceeds %d", errContinuityTraversalLimit, depth, w.limits.MaxDepth)
	}
	if _, active := w.active[fsID]; active {
		return fmt.Errorf("%w: cycle at fs object %s", errContinuityMalformedTree, fsID)
	}
	if _, visited := w.visited[fsID]; visited {
		return nil
	}
	if len(w.visited) >= w.limits.MaxFSObjects {
		return fmt.Errorf("%w: fs object limit %d", errContinuityTraversalLimit, w.limits.MaxFSObjects)
	}

	w.active[fsID] = struct{}{}
	defer delete(w.active, fsID)

	projection, err := readContinuityFSObjectProjectionContext(w.ctx, w.db, w.libraryID, fsID)
	if err != nil {
		return err
	}
	row := continuityFSObjectFromProjection(projection)
	w.visited[fsID] = struct{}{}
	w.dependencies.fsObjects++
	if w.dependencies.projections == nil {
		w.dependencies.projections = make(map[string]FSObjectProjection)
	}
	w.dependencies.projections[fsID] = projection

	if !row.ObjectTypePresent || strings.TrimSpace(row.ObjectType) == "" {
		return fmt.Errorf("%w: fs object %s has no object type", errContinuityMalformedTree, fsID)
	}
	switch strings.ToLower(strings.TrimSpace(row.ObjectType)) {
	case "dir":
		if len(row.BlockIDs) != 0 || len(row.SeafileBlockIDs) != 0 {
			return fmt.Errorf("%w: directory %s contains block ids", errContinuityMalformedTree, fsID)
		}
		entries, err := parseContinuityDirectoryEntries(row.DirectoryEntry)
		if err != nil {
			return fmt.Errorf("%w: directory %s: %v", errContinuityMalformedTree, fsID, err)
		}
		for _, entry := range entries {
			w.treeEdges++
			if w.treeEdges > w.limits.MaxTreeEdges {
				return fmt.Errorf("%w: tree edge limit %d", errContinuityTraversalLimit, w.limits.MaxTreeEdges)
			}
			if err := w.visit(entry.ID, depth+1); err != nil {
				return err
			}
		}
	case "file":
		if strings.TrimSpace(row.DirectoryEntry) != "" && strings.TrimSpace(row.DirectoryEntry) != "null" {
			return fmt.Errorf("%w: file %s contains directory entries", errContinuityMalformedTree, fsID)
		}
		if err := validateContinuityFileCompleteness(row, fsID); err != nil {
			return err
		}
		blockIDs, err := w.resolveBlockIDs(row.BlockIDs, row.SeafileBlockIDs)
		if err != nil {
			return err
		}
		for _, blockID := range blockIDs {
			w.blockRefs++
			if w.blockRefs > w.limits.MaxBlockReferences {
				return fmt.Errorf("%w: block reference limit %d", errContinuityTraversalLimit, w.limits.MaxBlockReferences)
			}
			fsIDs := w.dependencies.fsByBlock[blockID]
			if fsIDs == nil {
				if len(w.dependencies.fsByBlock) >= w.limits.MaxUniqueBlocks {
					return fmt.Errorf("%w: unique block limit %d", errContinuityTraversalLimit, w.limits.MaxUniqueBlocks)
				}
				fsIDs = make(map[string]struct{})
				w.dependencies.fsByBlock[blockID] = fsIDs
			}
			fsIDs[fsID] = struct{}{}
		}
	default:
		return fmt.Errorf("%w: fs object %s has type %q", errContinuityMalformedTree, fsID, row.ObjectType)
	}
	return nil
}

func continuityFSObjectFromScannedFields(objectType *string, sizeBytes *int64, directoryEntry *string, blockIDs, seafileBlockIDs *[]string) continuityFSObject {
	var row continuityFSObject
	if objectType != nil {
		row.ObjectType = *objectType
		row.ObjectTypePresent = true
	}
	if sizeBytes != nil {
		row.SizeBytes = *sizeBytes
		row.SizeBytesPresent = true
	}
	if directoryEntry != nil {
		row.DirectoryEntry = *directoryEntry
		row.DirectoryEntryPresent = true
	}
	if blockIDs != nil {
		row.BlockIDs = *blockIDs
		row.BlockIDsPresent = true
	}
	if seafileBlockIDs != nil {
		row.SeafileBlockIDs = *seafileBlockIDs
		row.SeafileBlockIDsPresent = true
	}
	return row
}

func continuityFSObjectFromProjection(projection FSObjectProjection) continuityFSObject {
	row := continuityFSObject{ObjectType: projection.ObjectType, ObjectTypePresent: true}
	if projection.ObjectType == "dir" {
		row.DirectoryEntry = projection.DirectoryEntries
		row.DirectoryEntryPresent = true
		return row
	}
	row.SizeBytes = projection.SizeBytes
	row.SizeBytesPresent = true
	row.BlockIDsPresent = true
	if projection.FileLayout == FileStoragePairedCanonical {
		row.BlockIDs = cloneIdentityStrings(projection.CanonicalSHA256IDs)
		row.SeafileBlockIDs = cloneIdentityStrings(projection.LogicalSHA1IDs)
		row.SeafileBlockIDsPresent = true
	} else {
		row.BlockIDs = cloneIdentityStrings(projection.LogicalSHA1IDs)
	}
	return row
}

func validateContinuityFileCompleteness(row continuityFSObject, fsID string) error {
	missing := make([]string, 0, 2)
	if !row.SizeBytesPresent {
		missing = append(missing, "size_bytes")
	}
	hasLogicalBlockIDs := row.BlockIDsPresent
	if row.SeafileBlockIDsPresent && len(row.SeafileBlockIDs) > 0 {
		hasLogicalBlockIDs = true
	}
	if !hasLogicalBlockIDs {
		missing = append(missing, "block_ids or non-empty seafile_block_ids_sha1")
	}
	if len(missing) != 0 {
		return fmt.Errorf("%w: file %s is missing %s", errContinuityIncompleteFSObject, fsID, strings.Join(missing, " and "))
	}
	return nil
}

func parseContinuityDirectoryEntries(rawEntries string) ([]continuityDirectoryEntry, error) {
	trimmed := strings.TrimSpace(rawEntries)
	if trimmed == "" || trimmed == "null" {
		return nil, fmt.Errorf("directory listing is blank")
	}
	var rawValues []json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &rawValues); err != nil {
		return nil, fmt.Errorf("malformed directory listing: %w", err)
	}
	entries := make([]continuityDirectoryEntry, 0, len(rawValues))
	for index, raw := range rawValues {
		var entry continuityDirectoryEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, fmt.Errorf("entry %d is malformed: %w", index, err)
		}
		if err := validateContinuityFSID(entry.ID); err != nil {
			return nil, fmt.Errorf("entry %d has an invalid fs object id: %w", index, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func continuityStoredBlockIDs(internalIDs, externalIDs []string) ([]string, error) {
	if len(internalIDs) == 0 && len(externalIDs) == 0 {
		return nil, nil
	}
	if len(internalIDs) == 0 && len(externalIDs) > 0 {
		normalized := make([]string, 0, len(externalIDs))
		for _, rawID := range externalIDs {
			id := NormalizeBlockID(rawID)
			if !IsSHA1BlockID(id) {
				return nil, fmt.Errorf("%w: external block id %q is not SHA-1", errContinuityMalformedTree, rawID)
			}
			normalized = append(normalized, id)
		}
		return normalized, nil
	}
	if len(externalIDs) > 0 && len(internalIDs) != len(externalIDs) {
		return nil, fmt.Errorf("%w: block id lists have different lengths", errContinuityMalformedTree)
	}

	normalized := make([]string, 0, len(internalIDs))
	sawSHA256 := false
	sawSHA1 := false
	for index, rawID := range internalIDs {
		id := NormalizeBlockID(rawID)
		if len(externalIDs) > 0 {
			externalID := NormalizeBlockID(externalIDs[index])
			if !IsSHA256BlockID(id) || !IsSHA1BlockID(externalID) {
				return nil, fmt.Errorf("%w: canonical block id pair at index %d is malformed", errContinuityMalformedTree, index)
			}
			normalized = append(normalized, id)
			continue
		}
		if IsSHA256BlockID(id) {
			sawSHA256 = true
		} else if IsSHA1BlockID(id) {
			sawSHA1 = true
		}
		if IsSHA256BlockID(id) || IsSHA1BlockID(id) {
			if len(externalIDs) == 0 && sawSHA256 && sawSHA1 {
				return nil, fmt.Errorf("%w: mixed canonical and legacy block ids", errContinuityMalformedTree)
			}
			normalized = append(normalized, id)
			continue
		}
		return nil, fmt.Errorf("%w: block id %q is neither SHA-256 nor SHA-1", errContinuityMalformedTree, rawID)
	}
	if len(externalIDs) == 0 && len(internalIDs) > 0 && IsSHA256BlockID(normalized[0]) {
		for _, id := range normalized {
			if !IsSHA256BlockID(id) {
				return nil, fmt.Errorf("%w: mixed canonical and legacy block ids", errContinuityMalformedTree)
			}
		}
		return nil, fmt.Errorf("%w: canonical block ids have no seafile_block_ids_sha1", errContinuityMalformedTree)
	}
	return normalized, nil
}

func (w *continuityTreeWalker) resolveBlockIDs(internalIDs, externalIDs []string) ([]string, error) {
	storedIDs, err := continuityStoredBlockIDs(internalIDs, externalIDs)
	if err != nil {
		return nil, err
	}
	if len(externalIDs) == 0 {
		resolved := make([]string, 0, len(storedIDs))
		for _, blockID := range storedIDs {
			if !IsSHA1BlockID(blockID) {
				return nil, fmt.Errorf("%w: canonical block ids require an authority-bound paired projection", errContinuityMalformedTree)
			}
			canonicalID, err := w.resolveSHA1MappingAuthority(blockID)
			if err != nil {
				return nil, err
			}
			resolved = append(resolved, canonicalID)
		}
		return resolved, nil
	}
	if len(internalIDs) == 0 || len(internalIDs) != len(externalIDs) {
		return nil, fmt.Errorf("%w: canonical ids are not authority-bound to the logical ids", errContinuityIdentityUnproven)
	}
	resolved := make([]string, 0, len(storedIDs))
	for index, canonicalID := range storedIDs {
		externalID := NormalizeBlockID(externalIDs[index])
		mappedID, found, err := w.db.GetBlockIDMappingContext(w.ctx, w.orgID, w.representation, externalID)
		if err != nil {
			return nil, fmt.Errorf("%w: read paired block mapping %s: %v", errContinuityIdentityUnavailable, externalID, err)
		}
		if found {
			if err := validateCanonicalBlockMapping(canonicalID, mappedID, true); err != nil {
				return nil, fmt.Errorf("paired block mapping %s: %w", externalID, err)
			}
		} else if err := validateCanonicalBlockMapping(canonicalID, "", false); err != nil {
			return nil, err
		}
		resolved = append(resolved, canonicalID)
	}
	return resolved, nil
}

// resolveSHA1MappingAuthority resolves one SHA-1-only dependency through the
// durable Mapping Authority, acquiring it on the cold path when absent. The
// authority value is the only value consumed. The mutable block_id_mappings row
// never changes the resolution; it must agree with the authority or be absent,
// exactly like a paired file's compatibility mapping, because ordinary readers
// still resolve through it.
func (w *continuityTreeWalker) resolveSHA1MappingAuthority(externalID string) (string, error) {
	if resolvedID, ok := w.mappingResolved[externalID]; ok {
		return resolvedID, nil
	}
	if w.mappingAuthority == nil {
		return "", fmt.Errorf("%w: SHA-1 block %s has no mapping authority", errContinuityIdentityUnproven, externalID)
	}
	promotion, err := w.mappingAuthority.promote(w.ctx, w.orgID, w.representation, externalID)
	resolvedID, authoritative := promotion.AuthoritativeInternalID()
	if !authoritative {
		switch promotion.Outcome {
		case BlockMappingAuthorityUnproven:
			return "", fmt.Errorf("%w: SHA-1 block %s has no proven mapping authority: %v", errContinuityIdentityUnproven, externalID, err)
		case BlockMappingAuthorityConflict:
			return "", fmt.Errorf("%w: SHA-1 block %s mapping authority is unusable: %v", errContinuityIdentityConflict, externalID, err)
		default:
			return "", fmt.Errorf("%w: SHA-1 block %s mapping authority is %s: %v", errContinuityIdentityUnavailable, externalID, promotion.Outcome, err)
		}
	}
	if err := checkMutableMappingAgainstAuthority(w.ctx, w.mappingAuthority, w.orgID, w.representation, externalID, resolvedID); err != nil {
		return "", err
	}
	if w.mappingResolved == nil {
		w.mappingResolved = make(map[string]string)
	}
	w.mappingResolved[externalID] = resolvedID
	return resolvedID, nil
}

// checkMutableMappingAgainstAuthority fails closed when the mutable row that
// ordinary readers use resolves anywhere other than the durable authority. A
// witness over A while readers resolve B would protect the wrong bytes.
func checkMutableMappingAgainstAuthority(ctx context.Context, authority continuityMappingAuthority, orgID, representationID, externalID, authoritativeID string) error {
	mutableID, mutableFound, err := authority.readMutable(ctx, orgID, representationID, externalID)
	if err != nil {
		return fmt.Errorf("%w: read mutable mapping %s: %v", errContinuityIdentityUnavailable, externalID, err)
	}
	if err := validateCanonicalBlockMapping(authoritativeID, mutableID, mutableFound); err != nil {
		metrics.LibraryContinuityMappingAuthorityDivergenceTotal.Inc()
		return fmt.Errorf("SHA-1 block %s mutable mapping diverges from its authority: %w", externalID, err)
	}
	return nil
}

// revalidateContinuityMappingAuthority repeats the mutable-row agreement check
// immediately before the witness, so an ordinary mapping write that lands
// during certification cannot be witnessed. The claims themselves are
// write-once and have no update or delete path, so they are not re-read.
func revalidateContinuityMappingAuthority(ctx context.Context, authority continuityMappingAuthority, orgID, representationID string, mappings map[string]string) error {
	if len(mappings) == 0 {
		return nil
	}
	if authority == nil {
		return fmt.Errorf("%w: mapping authority unavailable for final revalidation", errContinuityIdentityUnavailable)
	}
	externalIDs := make([]string, 0, len(mappings))
	for externalID := range mappings {
		externalIDs = append(externalIDs, externalID)
	}
	sort.Strings(externalIDs)
	for _, externalID := range externalIDs {
		if err := checkMutableMappingAgainstAuthority(ctx, authority, orgID, representationID, externalID, mappings[externalID]); err != nil {
			return fmt.Errorf("before witness: %w", err)
		}
	}
	return nil
}

func validateCanonicalBlockMapping(authoritativeID, mappedID string, found bool) error {
	authoritativeID = NormalizeBlockID(authoritativeID)
	if !IsSHA256BlockID(authoritativeID) {
		return fmt.Errorf("%w: authoritative canonical block id %q is malformed", errContinuityIdentityConflict, authoritativeID)
	}
	if !found {
		return nil
	}
	mappedID = NormalizeBlockID(mappedID)
	if !IsSHA256BlockID(mappedID) || mappedID != authoritativeID {
		return fmt.Errorf("%w: mapping resolves to %q while identity authority binds %q", errContinuityIdentityConflict, mappedID, authoritativeID)
	}
	return nil
}

func classifyContinuityDependencyError(err error) (LibraryBaselineCertificationOutcome, LibraryBaselineCertificationReason) {
	switch {
	case errors.Is(err, errContinuityMissingCommit):
		return LibraryBaselineCertificationNotCertified, LibraryBaselineReasonMissingCommit
	case errors.Is(err, errContinuityMissingFSObject):
		return LibraryBaselineCertificationNotCertified, LibraryBaselineReasonMissingFSObject
	case errors.Is(err, errContinuityIncompleteFSObject):
		return LibraryBaselineCertificationNotCertified, LibraryBaselineReasonIncompleteFSObject
	case errors.Is(err, errContinuityMissingBlockMapping):
		return LibraryBaselineCertificationNotCertified, LibraryBaselineReasonMissingBlockMapping
	case errors.Is(err, errContinuityIdentityUnproven):
		return LibraryBaselineCertificationNotCertified, LibraryBaselineReasonIdentityUnproven
	case errors.Is(err, errContinuityIdentityConflict):
		return LibraryBaselineCertificationNotCertified, LibraryBaselineReasonIdentityConflict
	case errors.Is(err, errContinuityIdentityUnavailable):
		return LibraryBaselineCertificationUnknown, LibraryBaselineReasonIdentityAuthorityUnavailable
	case errors.Is(err, errContinuityMalformedTree):
		return LibraryBaselineCertificationNotCertified, LibraryBaselineReasonMalformedTree
	case errors.Is(err, errContinuityTraversalLimit):
		return LibraryBaselineCertificationNotCertified, LibraryBaselineReasonTraversalLimit
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return LibraryBaselineCertificationUnknown, LibraryBaselineReasonDependencyReadFailed
	default:
		return LibraryBaselineCertificationUnknown, LibraryBaselineReasonDependencyReadFailed
	}
}

func readContinuityPhysicalLocationContext(ctx context.Context, database *DB, orgID, blockID string) (BlockPhysicalLocation, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	row, found, err := readBlockRepairAuthorityContextFn(ctx, database, orgID, blockID, BlockAuthorityStrong)
	if err != nil {
		return BlockPhysicalLocation{}, false, fmt.Errorf("read physical location for block %s: %w", blockID, err)
	}
	if !found {
		return BlockPhysicalLocation{}, false, nil
	}
	if row.CreatedAt == nil || !row.StorageClassPresent || !row.StorageKeyPresent {
		return BlockPhysicalLocation{}, true, fmt.Errorf("%w: block %s has incomplete canonical metadata", errContinuityMalformedLocator, blockID)
	}
	return BlockPhysicalLocation{StorageClass: row.StorageClass, StorageKey: row.StorageKey}, true, nil
}

type libraryContinuityWitnessSettlement struct {
	Found     bool
	Certified bool
	Head      string
	Deleted   bool
}

func settleLibraryContinuityWitnessContext(ctx context.Context, database *DB, orgID, libraryID, observedHead string) (libraryContinuityWitnessSettlement, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var state LibraryState
	var certifiedHead *string
	var contractVersion *string
	var deletedAt time.Time
	err := database.Session().Query("SELECT head_commit_id, continuity_certified_head_commit_id, continuity_contract_version, deleted_at FROM libraries WHERE org_id = ? AND library_id = ?", orgID, libraryID).WithContext(ctx).Consistency(gocql.Serial).Scan(&state.HeadCommitID, &certifiedHead, &contractVersion, &deletedAt)
	if err != nil {
		if errors.Is(err, gocql.ErrNotFound) {
			return libraryContinuityWitnessSettlement{}, nil
		}
		return libraryContinuityWitnessSettlement{}, err
	}
	state.OrgID = orgID
	state.LibraryID = libraryID
	state.ContinuityCertifiedHeadCommitID = certifiedHead
	state.ContinuityContractVersion = contractVersion
	if !deletedAt.IsZero() {
		deletedCopy := deletedAt
		state.DeletedAt = &deletedCopy
	}
	settlement := libraryContinuityWitnessSettlement{
		Found:   true,
		Head:    state.HeadCommitID,
		Deleted: !deletedAt.IsZero(),
	}
	settlement.Certified = state.ContinuityWitnessValidFor(SupportedContinuityContractVersion) && state.HeadCommitID == observedHead
	return settlement, nil
}
