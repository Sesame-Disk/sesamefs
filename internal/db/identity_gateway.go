package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

var (
	IdentityAuthorityConflict    = errors.New("metadata identity authority conflict")
	IdentityAuthorityUnavailable = errors.New("metadata identity authority unavailable")
)

type FileStorageLayout uint8

const (
	FileStorageLayoutInvalid FileStorageLayout = iota
	FileStorageSHA1Only
	FileStoragePairedCanonical
)

type CommitProjection struct {
	LibraryID, CommitID, ParentID, RootFSID, CreatorID, Description string
	CreatedAt                                                       time.Time
}
type AuthorizedCommit struct{ projection CommitProjection }

type FSObjectProjection struct {
	LibraryID, FSID, ObjectType, DirectoryEntries string
	SizeBytes                                     int64
	FileLayout                                    FileStorageLayout
	LogicalSHA1IDs, CanonicalSHA256IDs            []string
	ObjectName, FullPath                          *string
	MTime                                         int64
}
type AuthorizedFSObject struct{ projection FSObjectProjection }

func cloneIdentityStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string{}, values...)
}
func cloneOptionalIdentityText(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func validateCommitProjection(p CommitProjection) (CommitProjection, error) {
	libraryID, err := canonicalIdentityUUID(p.LibraryID)
	if err != nil {
		return CommitProjection{}, err
	}
	creatorID, err := canonicalIdentityUUID(p.CreatorID)
	if err != nil {
		return CommitProjection{}, err
	}
	if p.CommitID == "" || p.RootFSID == "" {
		return CommitProjection{}, fmt.Errorf("%w: commit id and root fs id are required", ErrInvalidIdentityAuthorityInput)
	}
	p.LibraryID, p.CreatorID = libraryID, creatorID
	// #208 treated SQL NULL and an empty parent id as the same retry identity.
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	p.CreatedAt = canonicalIdentityCreatedAt(p.CreatedAt)
	return p, nil
}

// AuthorizeCommitProjection reads the global SERIAL claim before selecting a
// timestamp. A new identity costs one SERIAL read and one SERIAL LWT. An exact
// retry reuses claim.created_at. A CAS Conflict never grants permission: for
// a same-projection race, a second exact claim must return Idempotent.
func AuthorizeCommitProjection(ctx context.Context, session *gocql.Session, input CommitProjection) (*AuthorizedCommit, error) {
	p, err := validateCommitProjection(input)
	if err != nil {
		return nil, err
	}
	claim, found, err := ReadIdentityAuthority(ctx, session, p.LibraryID, IdentityKindCommit, p.CommitID)
	if err != nil {
		return nil, fmt.Errorf("%w: read commit claim: %v", IdentityAuthorityUnavailable, err)
	}
	if found {
		digest, digestErr := CommitIdentityDigest(p.LibraryID, p.CommitID, p.ParentID, p.RootFSID, p.CreatorID, p.Description, claim.CreatedAt)
		if digestErr != nil || claim.CreatedAt.IsZero() || claim.DigestVersion != SupportedIdentityDigestVersion || claim.Digest != digest {
			return nil, IdentityAuthorityConflict
		}
		p.CreatedAt = commitRetryCreatedAt(p.CreatedAt, claim.CreatedAt)
		if err := verifyCommitSourceProjection(ctx, session, p); err != nil {
			return nil, err
		}
		return &AuthorizedCommit{projection: p}, nil
	}
	digest, err := CommitIdentityDigest(p.LibraryID, p.CommitID, p.ParentID, p.RootFSID, p.CreatorID, p.Description, p.CreatedAt)
	if err != nil {
		return nil, err
	}
	result, err := ClaimIdentityAuthorityAt(ctx, session, p.LibraryID, IdentityKindCommit, p.CommitID, SupportedIdentityDigestVersion, digest, p.CreatedAt)
	if err != nil || result.Outcome == IdentityClaimUnknown {
		if err == nil {
			err = errors.New("claim result is unknown")
		}
		return nil, fmt.Errorf("%w: claim commit: %v", IdentityAuthorityUnavailable, err)
	}
	if result.Outcome != IdentityClaimConflict && !identityClaimOutcomeAuthorizesSource(result.Outcome) {
		return nil, IdentityAuthorityUnavailable
	}
	switch result.Outcome {
	case IdentityClaimEstablished:
		if err := verifyCommitSourceProjection(ctx, session, p); err != nil {
			return nil, err
		}
		return &AuthorizedCommit{projection: p}, nil
	case IdentityClaimIdempotent:
		if result.Stored == nil || result.Stored.CreatedAt.IsZero() {
			return nil, IdentityAuthorityUnavailable
		}
		p.CreatedAt = commitRetryCreatedAt(p.CreatedAt, result.Stored.CreatedAt)
		if err := verifyCommitSourceProjection(ctx, session, p); err != nil {
			return nil, err
		}
		return &AuthorizedCommit{projection: p}, nil
	case IdentityClaimConflict:
		if result.Stored == nil || result.Stored.CreatedAt.IsZero() {
			return nil, IdentityAuthorityConflict
		}
		recovered := canonicalIdentityCreatedAt(result.Stored.CreatedAt)
		recoveredDigest, digestErr := CommitIdentityDigest(p.LibraryID, p.CommitID, p.ParentID, p.RootFSID, p.CreatorID, p.Description, recovered)
		if digestErr != nil || result.Stored.DigestVersion != SupportedIdentityDigestVersion || recoveredDigest != result.Stored.Digest {
			return nil, IdentityAuthorityConflict
		}
		// The initial Conflict did not authorize a write. Only this exact
		// re-claim, which must be Idempotent, returns the capability.
		retry, retryErr := ClaimIdentityAuthorityAt(ctx, session, p.LibraryID, IdentityKindCommit, p.CommitID, SupportedIdentityDigestVersion, recoveredDigest, recovered)
		if retryErr != nil {
			return nil, fmt.Errorf("%w: exact retry claim: %v", IdentityAuthorityUnavailable, retryErr)
		}
		if !identityExactRetryMatches(retry, SupportedIdentityDigestVersion, recoveredDigest) {
			return nil, IdentityAuthorityConflict
		}
		p.CreatedAt = recovered
		if err := verifyCommitSourceProjection(ctx, session, p); err != nil {
			return nil, err
		}
		return &AuthorizedCommit{projection: p}, nil
	default:
		return nil, IdentityAuthorityUnavailable
	}
}

// verifyCommitSourceProjection permits crash recovery when the claim exists but
// the row does not. A present row must already match the complete claimed
// projection; retries never repair divergent semantic data by overwriting it.
func verifyCommitSourceProjection(ctx context.Context, session *gocql.Session, expected CommitProjection) error {
	if ctx == nil {
		ctx = context.Background()
	}
	row := map[string]interface{}{}
	err := session.Query(`
		SELECT parent_id, root_fs_id, creator_id, description, created_at
		FROM commits WHERE library_id = ? AND commit_id = ?
	`, expected.LibraryID, expected.CommitID).
		WithContext(ctx).
		Consistency(gocql.LocalQuorum).
		MapScan(row)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: read commit source projection: %v", IdentityAuthorityUnavailable, err)
	}
	actual, err := commitProjectionFromIdentitySourceRow(expected.LibraryID, expected.CommitID, row)
	if err != nil {
		return err
	}
	actualDigest, actualErr := CommitIdentityDigest(actual.LibraryID, actual.CommitID, actual.ParentID, actual.RootFSID, actual.CreatorID, actual.Description, actual.CreatedAt)
	expectedDigest, expectedErr := CommitIdentityDigest(expected.LibraryID, expected.CommitID, expected.ParentID, expected.RootFSID, expected.CreatorID, expected.Description, expected.CreatedAt)
	if actualErr != nil || expectedErr != nil || !identitySourceDigestMatches(actualDigest, expectedDigest) {
		return IdentityAuthorityConflict
	}
	return nil
}

func commitProjectionFromIdentitySourceRow(libraryID, commitID string, row map[string]interface{}) (CommitProjection, error) {
	parentID, parentOK := identitySourceNullableText(row, "parent_id")
	rootFSID, rootOK := identitySourceText(row, "root_fs_id")
	creatorID, creatorOK := identitySourceText(row, "creator_id")
	description, descriptionOK := identitySourceText(row, "description")
	createdAt, timeOK := row["created_at"].(time.Time)
	if !parentOK || !rootOK || !creatorOK || !descriptionOK || !timeOK || createdAt.IsZero() {
		return CommitProjection{}, IdentityAuthorityConflict
	}
	return CommitProjection{
		LibraryID: libraryID, CommitID: commitID, ParentID: parentID,
		RootFSID: rootFSID, CreatorID: creatorID, Description: description,
		CreatedAt: canonicalIdentityCreatedAt(createdAt),
	}, nil
}

func identitySourceText(row map[string]interface{}, key string) (string, bool) {
	value, ok := row[key]
	if !ok || value == nil {
		return "", false
	}
	switch typed := value.(type) {
	case string:
		return typed, true
	case []byte:
		return string(typed), true
	case gocql.UUID:
		parsed, err := uuid.FromBytes(typed[:])
		if err != nil {
			return "", false
		}
		return parsed.String(), true
	case uuid.UUID:
		return typed.String(), true
	default:
		return "", false
	}
}

func identitySourceNullableText(row map[string]interface{}, key string) (string, bool) {
	value, ok := row[key]
	if !ok || value == nil {
		return "", true
	}
	return identitySourceText(row, key)
}

func verifyFSObjectSourceProjection(ctx context.Context, session *gocql.Session, expected FSObjectProjection) error {
	if ctx == nil {
		ctx = context.Background()
	}
	row := map[string]interface{}{}
	err := session.Query(`
		SELECT obj_type, size_bytes, dir_entries, block_ids, seafile_block_ids_sha1
		FROM fs_objects WHERE library_id = ? AND fs_id = ?
	`, expected.LibraryID, expected.FSID).
		WithContext(ctx).
		Consistency(gocql.LocalQuorum).
		MapScan(row)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: read fs_object source projection: %v", IdentityAuthorityUnavailable, err)
	}
	if identitySourceRowIsEmptyFile(row) && expected.ObjectType == "file" && expected.SizeBytes == 0 && len(expected.LogicalSHA1IDs) == 0 && len(expected.CanonicalSHA256IDs) == 0 {
		return nil
	}
	actual, placeholder, err := fsObjectProjectionFromIdentitySourceRow(expected.LibraryID, expected.FSID, row)
	if err != nil {
		return err
	}
	if placeholder {
		return nil
	}
	_, actualDigest, actualErr := validateFSObjectProjection(actual)
	_, expectedDigest, expectedErr := validateFSObjectProjection(expected)
	if actualErr != nil || expectedErr != nil || !identitySourceDigestMatches(actualDigest, expectedDigest) {
		return IdentityAuthorityConflict
	}
	return nil
}

// Only a row with no semantic fields is a placeholder. Empty or partially set
// semantic values are not inferred from their display metadata and fail closed.
func fsObjectProjectionFromIdentitySourceRow(libraryID, fsID string, row map[string]interface{}) (FSObjectProjection, bool, error) {
	if IdentitySourceRowIsMetadataPlaceholder(row) {
		return FSObjectProjection{}, true, nil
	}
	semanticFields := []string{"obj_type", "size_bytes", "dir_entries", "block_ids", "seafile_block_ids_sha1"}
	semanticPresent := false
	for _, field := range semanticFields {
		if identitySourceHasValue(row, field) {
			semanticPresent = true
			break
		}
	}
	if !semanticPresent {
		return FSObjectProjection{}, true, nil
	}
	objectType, ok := identitySourceText(row, "obj_type")
	if !ok {
		return FSObjectProjection{}, false, IdentityAuthorityConflict
	}
	projection := FSObjectProjection{LibraryID: libraryID, FSID: fsID, ObjectType: objectType}
	switch objectType {
	case "dir":
		if identitySourceNonZeroInt64(row, "size_bytes") || identitySourceHasValue(row, "block_ids") || identitySourceHasValue(row, "seafile_block_ids_sha1") {
			return FSObjectProjection{}, false, IdentityAuthorityConflict
		}
		entries, entriesOK := identitySourceText(row, "dir_entries")
		if !entriesOK {
			return FSObjectProjection{}, false, IdentityAuthorityConflict
		}
		projection.DirectoryEntries = entries
	case "file":
		size, sizeOK := identitySourceInt64(row, "size_bytes")
		if !sizeOK {
			return FSObjectProjection{}, false, IdentityAuthorityConflict
		}
		blockIDs, blockIDsOK := identitySourceStringSlice(row, "block_ids")
		if !blockIDsOK {
			return FSObjectProjection{}, false, IdentityAuthorityConflict
		}
		projection.SizeBytes = size
		if identitySourceHasValue(row, "dir_entries") {
			entries, entriesOK := identitySourceText(row, "dir_entries")
			if !entriesOK || entries != "" {
				return FSObjectProjection{}, false, IdentityAuthorityConflict
			}
		}
		if identitySourceHasValue(row, "seafile_block_ids_sha1") {
			logicalIDs, logicalOK := identitySourceStringSlice(row, "seafile_block_ids_sha1")
			if !logicalOK {
				return FSObjectProjection{}, false, IdentityAuthorityConflict
			}
			projection.FileLayout = FileStoragePairedCanonical
			projection.LogicalSHA1IDs = logicalIDs
			projection.CanonicalSHA256IDs = blockIDs
		} else {
			projection.FileLayout = FileStorageSHA1Only
			projection.LogicalSHA1IDs = blockIDs
		}
	default:
		return FSObjectProjection{}, false, IdentityAuthorityConflict
	}
	return projection, false, nil
}

// IdentitySourceRowIsMetadataPlaceholder recognizes the all-NULL row shape that
// gocql MapScan exposes as zero values for nullable scalar columns.
func IdentitySourceRowIsMetadataPlaceholder(row map[string]interface{}) bool {
	rawType, typeExists := row["obj_type"]
	if typeExists && rawType != nil {
		typ, ok := identitySourceText(row, "obj_type")
		if !ok || typ != "" {
			return false
		}
	}
	if identitySourceHasValue(row, "dir_entries") {
		value, ok := identitySourceText(row, "dir_entries")
		if !ok || value != "" {
			return false
		}
	}
	if identitySourceHasValue(row, "block_ids") || identitySourceHasValue(row, "seafile_block_ids_sha1") {
		return false
	}
	if size, ok := identitySourceInt64(row, "size_bytes"); ok && size != 0 {
		return false
	}
	return true
}

// IdentitySourceValuePresent reports whether a MapScan value carries semantic data.
// Cassandra NULL collections can arrive as typed nil slices inside an interface;
// those are absent, while non-nil empty collections remain explicit values.
func IdentitySourceValuePresent(row map[string]interface{}, key string) bool {
	return identitySourceHasValue(row, key)
}

func identitySourceHasValue(row map[string]interface{}, key string) bool {
	value, ok := row[key]
	if !ok || value == nil {
		return false
	}
	// MapScan can represent a Cassandra NULL collection as a typed nil
	// slice inside the interface. Treat that as absent so a SHA-1-only row
	// is not misclassified as a paired projection with an empty SHA-1 list.
	switch typed := value.(type) {
	case []string:
		return typed != nil
	case []interface{}:
		return typed != nil
	case []byte:
		return typed != nil
	default:
		return true
	}
}

func identitySourceInt64(row map[string]interface{}, key string) (int64, bool) {
	value, ok := row[key]
	if !ok || value == nil {
		return 0, false
	}
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int32:
		return int64(typed), true
	case int:
		return int64(typed), true
	case uint64:
		return int64(typed), true
	case uint:
		return int64(typed), true
	default:
		return 0, false
	}
}

func identitySourceNonZeroInt64(row map[string]interface{}, key string) bool {
	value, ok := identitySourceInt64(row, key)
	return ok && value != 0
}

func identitySourceStringSlice(row map[string]interface{}, key string) ([]string, bool) {
	if !identitySourceHasValue(row, key) {
		return nil, false
	}
	value := row[key]
	switch typed := value.(type) {
	case []string:
		return cloneIdentityStrings(typed), true
	case []interface{}:
		values := make([]string, len(typed))
		for i, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			values[i] = text
		}
		return values, true
	default:
		return nil, false
	}
}

func commitRetryCreatedAt(candidate, stored time.Time) time.Time {
	if stored.IsZero() {
		return canonicalIdentityCreatedAt(candidate)
	}
	return canonicalIdentityCreatedAt(stored)
}

func identityExactRetryMatches(result IdentityClaimResult, digestVersion, digest string) bool {
	return result.Outcome == IdentityClaimIdempotent && result.Stored != nil &&
		result.Stored.DigestVersion == digestVersion && result.Stored.Digest == digest
}

func identityClaimOutcomeAuthorizesSource(outcome IdentityClaimOutcome) bool {
	return outcome == IdentityClaimEstablished || outcome == IdentityClaimIdempotent
}

func identitySourceDigestMatches(actual, expected string) bool {
	return actual != "" && expected != "" && actual == expected
}

func commitProjectionSourceValues(p CommitProjection) []interface{} {
	return []interface{}{p.LibraryID, p.CommitID, p.ParentID, p.RootFSID, p.CreatorID, p.Description, canonicalIdentityCreatedAt(p.CreatedAt)}
}

func AddAuthorizedCommitToBatch(batch *gocql.Batch, authorized *AuthorizedCommit) error {
	if batch == nil || authorized == nil {
		return fmt.Errorf("%w: nil commit batch capability", ErrInvalidIdentityAuthorityInput)
	}
	p := authorized.projection
	batch.Query("INSERT INTO commits (library_id, commit_id, parent_id, root_fs_id, creator_id, description, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		commitProjectionSourceValues(p)...)
	return nil
}
func MaterializeAuthorizedCommit(session *gocql.Session, authorized *AuthorizedCommit) error {
	if session == nil || authorized == nil {
		return fmt.Errorf("%w: nil commit session capability", ErrInvalidIdentityAuthorityInput)
	}
	batch := session.Batch(gocql.LoggedBatch)
	if err := AddAuthorizedCommitToBatch(batch, authorized); err != nil {
		return err
	}
	if err := batch.Exec(); err != nil {
		return fmt.Errorf("materialize authorized commit %s: %w", authorized.projection.CommitID, err)
	}
	return nil
}

func validateFSObjectProjection(p FSObjectProjection) (FSObjectProjection, string, error) {
	libraryID, err := canonicalIdentityUUID(p.LibraryID)
	if err != nil {
		return FSObjectProjection{}, "", err
	}
	if p.FSID == "" {
		return FSObjectProjection{}, "", fmt.Errorf("%w: fs id is required", ErrInvalidIdentityAuthorityInput)
	}
	p.LibraryID = libraryID
	p.LogicalSHA1IDs = cloneIdentityStrings(p.LogicalSHA1IDs)
	p.CanonicalSHA256IDs = cloneIdentityStrings(p.CanonicalSHA256IDs)
	p.ObjectName = cloneOptionalIdentityText(p.ObjectName)
	p.FullPath = cloneOptionalIdentityText(p.FullPath)
	switch p.ObjectType {
	case "dir":
		if p.DirectoryEntries == "" || p.FileLayout != FileStorageLayoutInvalid || len(p.LogicalSHA1IDs) != 0 || len(p.CanonicalSHA256IDs) != 0 {
			return FSObjectProjection{}, "", fmt.Errorf("%w: incomplete or mixed directory projection", ErrInvalidIdentityAuthorityInput)
		}
		digest, err := DirectoryIdentityDigest(p.LibraryID, p.FSID, p.DirectoryEntries)
		return p, digest, err
	case "file":
		if p.SizeBytes < 0 {
			return FSObjectProjection{}, "", fmt.Errorf("%w: negative file size", ErrInvalidIdentityAuthorityInput)
		}
		if p.DirectoryEntries != "" {
			return FSObjectProjection{}, "", fmt.Errorf("%w: file has directory entries", ErrInvalidIdentityAuthorityInput)
		}
		if p.FileLayout == FileStorageSHA1Only {
			if len(p.CanonicalSHA256IDs) != 0 {
				return FSObjectProjection{}, "", fmt.Errorf("%w: SHA1-only layout has canonical ids", ErrInvalidIdentityAuthorityInput)
			}
		} else if p.FileLayout == FileStoragePairedCanonical {
			if len(p.LogicalSHA1IDs) != len(p.CanonicalSHA256IDs) {
				return FSObjectProjection{}, "", fmt.Errorf("%w: paired block lists differ in length", ErrInvalidIdentityAuthorityInput)
			}
		} else {
			return FSObjectProjection{}, "", fmt.Errorf("%w: file storage layout is required", ErrInvalidIdentityAuthorityInput)
		}
		digest, err := FileIdentityDigest(p.LibraryID, p.FSID, p.SizeBytes, p.LogicalSHA1IDs, p.CanonicalSHA256IDs)
		return p, digest, err
	default:
		return FSObjectProjection{}, "", fmt.Errorf("%w: unknown object type %q", ErrInvalidIdentityAuthorityInput, p.ObjectType)
	}
}

func compatibleSHA1OnlyProjection(input FSObjectProjection, stored *IdentityAuthorityClaim) (FSObjectProjection, bool, error) {
	if stored == nil || input.ObjectType != "file" || input.FileLayout != FileStoragePairedCanonical {
		return FSObjectProjection{}, false, nil
	}
	sha1OnlyDigest, err := FileIdentityDigest(input.LibraryID, input.FSID, input.SizeBytes, input.LogicalSHA1IDs, nil)
	if err != nil {
		return FSObjectProjection{}, false, err
	}
	if stored.DigestVersion != SupportedIdentityDigestVersion || stored.Digest != sha1OnlyDigest {
		return FSObjectProjection{}, false, nil
	}
	// A paired writer may reuse an authoritative SHA-1-only identity, but it
	// cannot change the write-once claim or source projection into a paired one.
	input.FileLayout = FileStorageSHA1Only
	input.CanonicalSHA256IDs = nil
	input.DirectoryEntries = ""
	return input, true, nil
}

func AuthorizeFSObjectProjection(ctx context.Context, session *gocql.Session, input FSObjectProjection) (*AuthorizedFSObject, error) {
	p, digest, err := validateFSObjectProjection(input)
	if err != nil {
		return nil, err
	}
	result, err := ClaimIdentityAuthorityAt(ctx, session, p.LibraryID, IdentityKindFSObject, p.FSID, SupportedIdentityDigestVersion, digest, canonicalIdentityCreatedAt(time.Now()))
	if err != nil || result.Outcome == IdentityClaimUnknown {
		if err == nil {
			err = errors.New("claim result is unknown")
		}
		return nil, fmt.Errorf("%w: claim fs object: %v", IdentityAuthorityUnavailable, err)
	}
	if result.Outcome != IdentityClaimConflict && !identityClaimOutcomeAuthorizesSource(result.Outcome) {
		return nil, IdentityAuthorityUnavailable
	}
	switch result.Outcome {
	case IdentityClaimEstablished, IdentityClaimIdempotent:
	case IdentityClaimConflict:
		if result.Stored == nil {
			return nil, IdentityAuthorityConflict
		}
		if compatible, ok, compatibilityErr := compatibleSHA1OnlyProjection(p, result.Stored); compatibilityErr != nil {
			return nil, compatibilityErr
		} else if ok {
			_, sha1OnlyDigest, digestErr := validateFSObjectProjection(compatible)
			if digestErr != nil {
				return nil, digestErr
			}
			// The paired claim conflicted. Compatibility can only reuse the
			// existing SHA-1-only authority after a second exact claim settles
			// that original projection as Idempotent.
			retry, retryErr := ClaimIdentityAuthorityAt(ctx, session, compatible.LibraryID, IdentityKindFSObject, compatible.FSID, SupportedIdentityDigestVersion, sha1OnlyDigest, canonicalIdentityCreatedAt(time.Now()))
			if retryErr != nil {
				return nil, fmt.Errorf("%w: exact SHA1-only compatibility claim: %v", IdentityAuthorityUnavailable, retryErr)
			}
			if !identityExactRetryMatches(retry, SupportedIdentityDigestVersion, sha1OnlyDigest) {
				return nil, IdentityAuthorityConflict
			}
			p = compatible
			if err := verifyFSObjectSourceProjection(ctx, session, p); err != nil {
				return nil, err
			}
			return &AuthorizedFSObject{projection: p}, nil
		}
		if result.Stored.DigestVersion != SupportedIdentityDigestVersion || result.Stored.Digest != digest {
			return nil, IdentityAuthorityConflict
		}

		retry, retryErr := ClaimIdentityAuthorityAt(ctx, session, p.LibraryID, IdentityKindFSObject, p.FSID, SupportedIdentityDigestVersion, digest, canonicalIdentityCreatedAt(time.Now()))
		if retryErr != nil {
			return nil, fmt.Errorf("%w: exact fs object retry claim: %v", IdentityAuthorityUnavailable, retryErr)
		}
		if !identityExactRetryMatches(retry, SupportedIdentityDigestVersion, digest) {
			return nil, IdentityAuthorityConflict
		}
	default:
		return nil, IdentityAuthorityUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := verifyFSObjectSourceProjection(ctx, session, p); err != nil {
		return nil, err
	}
	return &AuthorizedFSObject{projection: p}, nil
}
func wrapIdentityAuthorityReadError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrInvalidIdentityAuthorityInput) {
		return err
	}
	return fmt.Errorf("%w: %s: %v", IdentityAuthorityUnavailable, operation, err)
}

func VerifyFSObjectProjection(ctx context.Context, session *gocql.Session, input FSObjectProjection) (IdentityVerificationOutcome, error) {
	p, digest, err := validateFSObjectProjection(input)
	if err != nil {
		return IdentityVerificationUnknown, err
	}
	outcome, err := VerifyIdentityAuthority(ctx, session, p.LibraryID, IdentityKindFSObject, p.FSID, SupportedIdentityDigestVersion, digest)
	if err != nil {
		if errors.Is(err, ErrInvalidIdentityAuthorityInput) {
			return IdentityVerificationUnknown, err
		}
		return IdentityVerificationUnknown, wrapIdentityAuthorityReadError("verify fs object authority", err)
	}
	return outcome, nil
}

func fsObjectStorageBlockColumns(p FSObjectProjection) (blockIDs, seafileBlockIDsSHA1 []string) {
	emptyList := func(values []string) []string {
		if values == nil {
			return []string{}
		}
		return cloneIdentityStrings(values)
	}
	if p.FileLayout == FileStorageSHA1Only {
		return emptyList(p.LogicalSHA1IDs), nil
	}
	if p.FileLayout == FileStoragePairedCanonical {
		return emptyList(p.CanonicalSHA256IDs), emptyList(p.LogicalSHA1IDs)
	}
	return nil, nil
}

func AddAuthorizedFSObjectToBatch(batch *gocql.Batch, authorized *AuthorizedFSObject) error {
	if batch == nil || authorized == nil {
		return fmt.Errorf("%w: nil fs object batch capability", ErrInvalidIdentityAuthorityInput)
	}
	p := authorized.projection
	blockIDs, seafileBlockIDsSHA1 := fsObjectStorageBlockColumns(p)
	switch p.ObjectType {
	case "dir":
		if p.ObjectName != nil {
			batch.Query("INSERT INTO fs_objects (library_id, fs_id, obj_type, obj_name, dir_entries, mtime) VALUES (?, ?, ?, ?, ?, ?)", p.LibraryID, p.FSID, p.ObjectType, *p.ObjectName, p.DirectoryEntries, p.MTime)
		} else {
			batch.Query("INSERT INTO fs_objects (library_id, fs_id, obj_type, dir_entries, mtime) VALUES (?, ?, ?, ?, ?)", p.LibraryID, p.FSID, p.ObjectType, p.DirectoryEntries, p.MTime)
		}
	case "file":
		if p.FileLayout == FileStorageSHA1Only {
			batch.Query("INSERT INTO fs_objects (library_id, fs_id, obj_type, size_bytes, mtime, block_ids) VALUES (?, ?, ?, ?, ?, ?)", p.LibraryID, p.FSID, p.ObjectType, p.SizeBytes, p.MTime, blockIDs)
		} else if p.FullPath != nil {
			batch.Query("INSERT INTO fs_objects (library_id, fs_id, obj_type, obj_name, full_path, size_bytes, mtime, block_ids, seafile_block_ids_sha1) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", p.LibraryID, p.FSID, p.ObjectType, optionalIdentityText(p.ObjectName), *p.FullPath, p.SizeBytes, p.MTime, blockIDs, seafileBlockIDsSHA1)
		} else if p.ObjectName != nil {
			batch.Query("INSERT INTO fs_objects (library_id, fs_id, obj_type, obj_name, size_bytes, mtime, block_ids, seafile_block_ids_sha1) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", p.LibraryID, p.FSID, p.ObjectType, *p.ObjectName, p.SizeBytes, p.MTime, blockIDs, seafileBlockIDsSHA1)
		} else {
			batch.Query("INSERT INTO fs_objects (library_id, fs_id, obj_type, size_bytes, mtime, block_ids, seafile_block_ids_sha1) VALUES (?, ?, ?, ?, ?, ?, ?)", p.LibraryID, p.FSID, p.ObjectType, p.SizeBytes, p.MTime, blockIDs, seafileBlockIDsSHA1)
		}
	default:
		return fmt.Errorf("%w: unknown authorized fs object type", ErrInvalidIdentityAuthorityInput)
	}
	return nil
}
func optionalIdentityText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func MaterializeAuthorizedFSObject(session *gocql.Session, authorized *AuthorizedFSObject) error {
	if session == nil || authorized == nil {
		return fmt.Errorf("%w: nil fs object session capability", ErrInvalidIdentityAuthorityInput)
	}
	batch := session.Batch(gocql.LoggedBatch)
	if err := AddAuthorizedFSObjectToBatch(batch, authorized); err != nil {
		return err
	}
	if err := batch.Exec(); err != nil {
		return fmt.Errorf("materialize authorized fs object %s: %w", authorized.projection.FSID, err)
	}
	return nil
}

// Deletes remove source rows only. Claims are permanent provenance and are
// never changed here; delete-vs-retry lifecycle fencing remains a follow-up.
func DeleteCommitIdentity(session *gocql.Session, libraryID, commitID string) error {
	if session == nil {
		return fmt.Errorf("%w: nil commit delete session", ErrInvalidIdentityAuthorityInput)
	}
	canonicalLibraryID, err := canonicalIdentityUUID(libraryID)
	if err != nil {
		return err
	}
	row := map[string]interface{}{}
	err = session.Query(`
		SELECT parent_id, root_fs_id, creator_id, description, created_at
		FROM commits WHERE library_id = ? AND commit_id = ?
	`, canonicalLibraryID, commitID).Consistency(gocql.LocalQuorum).MapScan(row)
	blind := errors.Is(err, gocql.ErrNotFound)
	if !blind && err != nil {
		return fmt.Errorf("%w: read commit before delete: %v", IdentityAuthorityUnavailable, err)
	}
	if blind {
		if err := requireIdentityClaimForBlindDelete(session, canonicalLibraryID, IdentityKindCommit, commitID); err != nil {
			return err
		}
	} else {
		projection, projectionErr := commitProjectionFromIdentitySourceRow(canonicalLibraryID, commitID, row)
		if projectionErr != nil {
			return projectionErr
		}
		digest, digestErr := CommitIdentityDigest(projection.LibraryID, projection.CommitID, projection.ParentID, projection.RootFSID, projection.CreatorID, projection.Description, projection.CreatedAt)
		if digestErr != nil {
			return IdentityAuthorityConflict
		}
		if err := verifyIdentityBeforeDelete(session, canonicalLibraryID, IdentityKindCommit, commitID, digest); err != nil {
			return err
		}
	}
	if err := session.Query("DELETE FROM commits WHERE library_id = ? AND commit_id = ?", canonicalLibraryID, commitID).Consistency(gocql.EachQuorum).Exec(); err != nil {
		return fmt.Errorf("delete commit source row %s: %w", commitID, err)
	}
	return nil
}
func DeleteFSObjectIdentity(session *gocql.Session, libraryID, fsID string) error {
	if session == nil {
		return fmt.Errorf("%w: nil fs object delete session", ErrInvalidIdentityAuthorityInput)
	}
	canonicalLibraryID, err := canonicalIdentityUUID(libraryID)
	if err != nil {
		return err
	}
	row := map[string]interface{}{}
	err = session.Query(`
		SELECT obj_type, size_bytes, dir_entries, block_ids, seafile_block_ids_sha1
		FROM fs_objects WHERE library_id = ? AND fs_id = ?
	`, canonicalLibraryID, fsID).Consistency(gocql.LocalQuorum).MapScan(row)
	blind := errors.Is(err, gocql.ErrNotFound)
	if !blind && err != nil {
		return fmt.Errorf("%w: read fs_object before delete: %v", IdentityAuthorityUnavailable, err)
	}
	if blind {
		if err := requireIdentityClaimForBlindDelete(session, canonicalLibraryID, IdentityKindFSObject, fsID); err != nil {
			return err
		}
	} else {
		if identitySourceRowIsEmptyFile(row) {
			claim, found, claimErr := ReadIdentityAuthority(context.Background(), session, canonicalLibraryID, IdentityKindFSObject, fsID)
			if claimErr != nil {
				return fmt.Errorf("%w: read claim for zero-block fs object delete: %v", IdentityAuthorityUnavailable, claimErr)
			}
			if !found || claim.DigestVersion != SupportedIdentityDigestVersion || claim.Digest == "" {
				return IdentityAuthorityConflict
			}
			if err := verifyFSObjectAuthorityBeforeDelete(session, canonicalLibraryID, fsID, claim.Digest); err != nil {
				return err
			}
		} else {
			projection, placeholder, projectionErr := fsObjectProjectionFromIdentitySourceRow(canonicalLibraryID, fsID, row)
			if projectionErr != nil {
				return projectionErr
			}
			if placeholder {
				return IdentityAuthorityConflict
			}
			_, digest, digestErr := validateFSObjectProjection(projection)
			if digestErr != nil {
				return IdentityAuthorityConflict
			}
			if err := verifyFSObjectAuthorityBeforeDelete(session, canonicalLibraryID, fsID, digest); err != nil {
				return err
			}
		}
	}
	if err := session.Query("DELETE FROM fs_objects WHERE library_id = ? AND fs_id = ?", canonicalLibraryID, fsID).Consistency(gocql.EachQuorum).Exec(); err != nil {
		return fmt.Errorf("delete fs object source row %s: %w", fsID, err)
	}
	return nil
}

func requireIdentityClaimForBlindDelete(session *gocql.Session, libraryID string, kind IdentityKind, identityID string) error {
	claim, found, err := ReadIdentityAuthority(context.Background(), session, libraryID, kind, identityID)
	if err != nil {
		return fmt.Errorf("%w: read claim for blind delete: %v", IdentityAuthorityUnavailable, err)
	}
	if !found || claim.DigestVersion != SupportedIdentityDigestVersion || claim.Digest == "" {
		return IdentityAuthorityConflict
	}
	return nil
}

func verifyFSObjectAuthorityBeforeDelete(session *gocql.Session, libraryID, fsID string, digest string) error {
	outcome, err := VerifyIdentityAuthority(context.Background(), session, libraryID, IdentityKindFSObject, fsID, SupportedIdentityDigestVersion, digest)
	if err != nil || outcome == IdentityVerificationUnknown {
		if err == nil {
			err = errors.New("identity verification is unknown")
		}
		return fmt.Errorf("%w: verify fs object before delete: %v", IdentityAuthorityUnavailable, err)
	}
	if outcome != IdentityVerificationVerified {
		return IdentityAuthorityConflict
	}
	return nil
}

// Individual source-row deletion verifies existing provenance with a global
// SERIAL read. It does not issue a claim LWT and never mutates the claim row.
func verifyIdentityBeforeDelete(session *gocql.Session, libraryID string, kind IdentityKind, identityID, digest string) error {
	outcome, err := VerifyIdentityAuthority(context.Background(), session, libraryID, kind, identityID, SupportedIdentityDigestVersion, digest)
	if err != nil || outcome == IdentityVerificationUnknown {
		if err == nil {
			err = errors.New("identity verification is unknown")
		}
		return fmt.Errorf("%w: verify identity before delete: %v", IdentityAuthorityUnavailable, err)
	}
	if outcome != IdentityVerificationVerified {
		return IdentityAuthorityConflict
	}
	return nil
}

// AddUnpublishedLibraryIdentityPartitionDeletesToBatch is called only after
// the existing unpublished-library HEAD rollback protocol authorizes teardown.
func AddUnpublishedLibraryIdentityPartitionDeletesToBatch(batch *gocql.Batch, libraryID string) error {
	if batch == nil || libraryID == "" {
		return fmt.Errorf("%w: rollback batch and library id are required", ErrInvalidIdentityAuthorityInput)
	}
	batch.Query("DELETE FROM fs_objects WHERE library_id = ?", libraryID)
	batch.Query("DELETE FROM commits WHERE library_id = ?", libraryID)
	return nil
}

// identitySourceRowIsEmptyFile reports the valid zero-block file shape that Cassandra
// can expose with both nullable LIST columns as typed nil slices. The authority
// claim remains the source of truth for whether the layout was SHA-1-only or paired.
func identitySourceRowIsEmptyFile(row map[string]interface{}) bool {
	typ, ok := identitySourceText(row, "obj_type")
	if !ok || typ != "file" {
		return false
	}
	size, ok := identitySourceInt64(row, "size_bytes")
	if !ok || size != 0 {
		return false
	}
	if identitySourceHasValue(row, "dir_entries") {
		entries, ok := identitySourceText(row, "dir_entries")
		if !ok || entries != "" {
			return false
		}
	}
	return !identitySourceHasValue(row, "block_ids") && !identitySourceHasValue(row, "seafile_block_ids_sha1")
}
