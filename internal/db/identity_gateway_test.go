package db

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFSObjectStorageLayoutsMapSemanticListsToColumns(t *testing.T) {
	logical := []string{"logical-a", "logical-b"}
	canonical := []string{"canonical-a", "canonical-b"}
	tests := []struct {
		name                         string
		layout                       FileStorageLayout
		wantBlockIDs, wantSeafileIDs []string
	}{
		{name: "sync sha1 only", layout: FileStorageSHA1Only, wantBlockIDs: logical},
		{name: "paired canonical", layout: FileStoragePairedCanonical, wantBlockIDs: canonical, wantSeafileIDs: logical},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			blockIDs, seafileIDs := fsObjectStorageBlockColumns(FSObjectProjection{
				FileLayout: test.layout, LogicalSHA1IDs: logical, CanonicalSHA256IDs: canonical,
			})
			if !reflect.DeepEqual(blockIDs, test.wantBlockIDs) || !reflect.DeepEqual(seafileIDs, test.wantSeafileIDs) {
				t.Fatalf("storage columns = block_ids %v, seafile_block_ids_sha1 %v; want %v and %v",
					blockIDs, seafileIDs, test.wantBlockIDs, test.wantSeafileIDs)
			}
		})
	}
}

func TestSHA1OnlyAuthorityCanReusePairedProjection(t *testing.T) {
	libraryID := "00000000-0000-4000-8000-000000000001"
	input := FSObjectProjection{
		LibraryID: libraryID, FSID: "fs", ObjectType: "file", SizeBytes: 12,
		FileLayout:         FileStoragePairedCanonical,
		LogicalSHA1IDs:     []string{"sha1-a", "sha1-b"},
		CanonicalSHA256IDs: []string{"sha256-a", "sha256-b"},
	}
	_, pairedDigest, err := validateFSObjectProjection(input)
	if err != nil {
		t.Fatal(err)
	}
	legacyDigest, err := FileIdentityDigest(libraryID, "fs", 12, input.LogicalSHA1IDs, nil)
	if err != nil {
		t.Fatal(err)
	}
	compatible, ok, err := compatibleSHA1OnlyProjection(input, &IdentityAuthorityClaim{
		DigestVersion: SupportedIdentityDigestVersion, Digest: legacyDigest,
	})
	if err != nil || !ok {
		t.Fatalf("paired projection was not accepted for an existing SHA1-only claim: ok=%v err=%v", ok, err)
	}
	if compatible.FileLayout != FileStorageSHA1Only || len(compatible.CanonicalSHA256IDs) != 0 {
		t.Fatalf("compatibility changed the authoritative layout: %+v", compatible)
	}
	if _, adaptedDigest, err := validateFSObjectProjection(compatible); err != nil || adaptedDigest != legacyDigest {
		t.Fatalf("adapted projection digest=%s err=%v, want legacy %s", adaptedDigest, err, legacyDigest)
	}
	if pairedDigest == legacyDigest {
		t.Fatal("paired and SHA1-only projections unexpectedly share a digest")
	}
	if _, ok, err := compatibleSHA1OnlyProjection(input, &IdentityAuthorityClaim{
		DigestVersion: SupportedIdentityDigestVersion, Digest: pairedDigest,
	}); err != nil || ok {
		t.Fatalf("paired authority was incorrectly downgraded: ok=%v err=%v", ok, err)
	}
}

func TestFileProjectionRejectsDirectoryEntries(t *testing.T) {
	_, _, err := validateFSObjectProjection(FSObjectProjection{
		LibraryID: "00000000-0000-4000-8000-000000000001", FSID: "fs", ObjectType: "file",
		SizeBytes: 1, DirectoryEntries: "[]", FileLayout: FileStorageSHA1Only,
		LogicalSHA1IDs: []string{"sha1"},
	})
	if err == nil {
		t.Fatal("file projection with directory entries was accepted")
	}
}

func TestIdentityCreatedAtUsesCassandraMilliseconds(t *testing.T) {
	input := time.Date(2026, time.September, 21, 12, 30, 45, 123456789, time.FixedZone("test", -5*60*60))
	got := canonicalIdentityCreatedAt(input)
	want := time.UnixMilli(input.UnixMilli()).UTC()
	if !got.Equal(want) || got.Nanosecond()%int(time.Millisecond) != 0 {
		t.Fatalf("canonical timestamp = %s, want millisecond UTC %s", got, want)
	}
}

func TestAuthorizedCommitBindingUsesCompleteProjectionAndClaimTimestamp(t *testing.T) {
	createdAt := time.UnixMilli(1_800_000_000_123)
	projection := CommitProjection{
		LibraryID: "library", CommitID: "commit", ParentID: "", RootFSID: "root",
		CreatorID: "creator", Description: "description", CreatedAt: createdAt,
	}
	values := commitProjectionSourceValues(projection)
	want := []interface{}{"library", "commit", "", "root", "creator", "description", canonicalIdentityCreatedAt(createdAt)}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("commit source bindings = %#v, want %#v", values, want)
	}
}

func TestIdentitySourceRowsAreVerifiedBeforeMaterialization(t *testing.T) {
	libraryID := "00000000-0000-4000-8000-000000000001"
	creatorID := "00000000-0000-4000-8000-000000000002"
	createdAt := time.UnixMilli(1_800_000_000_123).UTC()
	commitRow := map[string]interface{}{
		"parent_id": nil, "root_fs_id": "root", "creator_id": creatorID,
		"description": "description", "created_at": createdAt,
	}
	commit, err := commitProjectionFromIdentitySourceRow(libraryID, "commit", commitRow)
	if err != nil {
		t.Fatalf("parse commit source projection: %v", err)
	}
	if commit.ParentID != "" || !commit.CreatedAt.Equal(createdAt) {
		t.Fatalf("parsed commit projection = %+v; NULL parent must canonicalize to empty and keep created_at", commit)
	}
	wantCommitDigest, err := CommitIdentityDigest(libraryID, "commit", "", "root", creatorID, "description", createdAt)
	if err != nil {
		t.Fatal(err)
	}
	gotCommitDigest, err := CommitIdentityDigest(commit.LibraryID, commit.CommitID, commit.ParentID, commit.RootFSID, commit.CreatorID, commit.Description, commit.CreatedAt)
	if err != nil || gotCommitDigest != wantCommitDigest {
		t.Fatalf("NULL parent digest = %q, want %q (err=%v)", gotCommitDigest, wantCommitDigest, err)
	}

	logical := []string{"sha1-a", "sha1-b"}
	canonical := []string{"sha256-a", "sha256-b"}
	fileRows := []struct {
		name string
		row  map[string]interface{}
		want FSObjectProjection
	}{
		{
			name: "sync sha1-only",
			row:  map[string]interface{}{"obj_type": "file", "size_bytes": int64(12), "dir_entries": "", "block_ids": logical, "seafile_block_ids_sha1": nil},
			want: FSObjectProjection{LibraryID: libraryID, FSID: "fs", ObjectType: "file", SizeBytes: 12, FileLayout: FileStorageSHA1Only, LogicalSHA1IDs: logical},
		},
		{
			name: "sync sha1-only typed null collection",
			row:  map[string]interface{}{"obj_type": "file", "size_bytes": int64(12), "dir_entries": "", "block_ids": logical, "seafile_block_ids_sha1": []string(nil)},
			want: FSObjectProjection{LibraryID: libraryID, FSID: "fs", ObjectType: "file", SizeBytes: 12, FileLayout: FileStorageSHA1Only, LogicalSHA1IDs: logical},
		},
		{
			name: "non-nil empty logical collection is explicit paired layout",
			row:  map[string]interface{}{"obj_type": "file", "size_bytes": int64(12), "block_ids": []string{}, "seafile_block_ids_sha1": []string{}},
			want: FSObjectProjection{LibraryID: libraryID, FSID: "fs", ObjectType: "file", SizeBytes: 12, FileLayout: FileStoragePairedCanonical, LogicalSHA1IDs: []string{}, CanonicalSHA256IDs: []string{}},
		},
		{
			name: "paired canonical",
			row:  map[string]interface{}{"obj_type": "file", "size_bytes": int64(12), "block_ids": canonical, "seafile_block_ids_sha1": logical},
			want: FSObjectProjection{LibraryID: libraryID, FSID: "fs", ObjectType: "file", SizeBytes: 12, FileLayout: FileStoragePairedCanonical, LogicalSHA1IDs: logical, CanonicalSHA256IDs: canonical},
		},
	}
	for _, test := range fileRows {
		t.Run(test.name, func(t *testing.T) {
			got, placeholder, err := fsObjectProjectionFromIdentitySourceRow(libraryID, "fs", test.row)
			if err != nil || placeholder {
				t.Fatalf("source projection = %+v, placeholder=%v, err=%v", got, placeholder, err)
			}
			_, gotDigest, err := validateFSObjectProjection(got)
			if err != nil {
				t.Fatalf("validate parsed source projection: %v", err)
			}
			_, wantDigest, err := validateFSObjectProjection(test.want)
			if err != nil || gotDigest != wantDigest {
				t.Fatalf("source digest=%s, want=%s (err=%v)", gotDigest, wantDigest, err)
			}
		})
	}

	placeholder, isPlaceholder, err := fsObjectProjectionFromIdentitySourceRow(libraryID, "fs", map[string]interface{}{
		"obj_type": nil, "size_bytes": nil, "dir_entries": nil, "block_ids": nil, "seafile_block_ids_sha1": nil,
	})
	if err != nil || !isPlaceholder {
		t.Fatalf("metadata-only row = %+v placeholder=%v err=%v, want placeholder", placeholder, isPlaceholder, err)
	}
	for name, row := range map[string]map[string]interface{}{
		"semantic value without type": {"size_bytes": int64(12)},
		"file missing size":           {"obj_type": "file", "block_ids": logical},
		"unknown type":                {"obj_type": "other"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, isPlaceholder, err := fsObjectProjectionFromIdentitySourceRow(libraryID, "fs", row); err == nil || isPlaceholder {
				t.Fatalf("partial source row accepted as placeholder=%v with err=%v", isPlaceholder, err)
			}
		})
	}
}

func TestFSObjectAuthorityReadErrorsMapToUnavailable(t *testing.T) {
	operational := wrapIdentityAuthorityReadError("existing fs object authority", errors.New("serial timeout"))
	if !errors.Is(operational, IdentityAuthorityUnavailable) {
		t.Fatalf("operational authority error=%v, want unavailable", operational)
	}
	invalid := wrapIdentityAuthorityReadError("existing fs object authority", ErrInvalidIdentityAuthorityInput)
	if !errors.Is(invalid, ErrInvalidIdentityAuthorityInput) || errors.Is(invalid, IdentityAuthorityUnavailable) {
		t.Fatalf("invalid input was reclassified as unavailable: %v", invalid)
	}
}
func TestIdentityGatewayFailureOutcomesNeverAuthorizeSource(t *testing.T) {
	for _, outcome := range []IdentityClaimOutcome{
		IdentityClaimUnknown,
		IdentityClaimConflict,
		0xff,
	} {
		if identityClaimOutcomeAuthorizesSource(outcome) {
			t.Errorf("claim outcome %v authorized a source write", outcome)
		}
	}
	for _, outcome := range []IdentityClaimOutcome{IdentityClaimEstablished, IdentityClaimIdempotent} {
		if !identityClaimOutcomeAuthorizesSource(outcome) {
			t.Errorf("claim outcome %v did not authorize an exact source projection", outcome)
		}
	}
	if identityExactRetryMatches(IdentityClaimResult{Outcome: IdentityClaimConflict, Stored: &IdentityAuthorityClaim{DigestVersion: SupportedIdentityDigestVersion, Digest: "same"}}, SupportedIdentityDigestVersion, "same") {
		t.Fatal("Conflict authorized a source retry")
	}
	if identityExactRetryMatches(IdentityClaimResult{Outcome: IdentityClaimIdempotent, Stored: &IdentityAuthorityClaim{DigestVersion: SupportedIdentityDigestVersion, Digest: "other"}}, SupportedIdentityDigestVersion, "same") {
		t.Fatal("different digest authorized an exact retry")
	}
	if !identityExactRetryMatches(IdentityClaimResult{Outcome: IdentityClaimIdempotent, Stored: &IdentityAuthorityClaim{DigestVersion: SupportedIdentityDigestVersion, Digest: "same"}}, SupportedIdentityDigestVersion, "same") {
		t.Fatal("exact idempotent retry was rejected")
	}
}

func TestCommitRetryUsesDurableClaimTimestamp(t *testing.T) {
	candidate := time.UnixMilli(1_800_000_000_123)
	stored := time.UnixMilli(1_700_000_000_456)
	if got := commitRetryCreatedAt(candidate, stored); !got.Equal(stored.UTC()) {
		t.Fatalf("retry timestamp = %s, want stored claim timestamp %s", got, stored.UTC())
	}
	if got := commitRetryCreatedAt(candidate, time.Time{}); !got.Equal(canonicalIdentityCreatedAt(candidate)) {
		t.Fatalf("first-attempt timestamp = %s, want candidate %s", got, canonicalIdentityCreatedAt(candidate))
	}
}

func TestIdentitySourceDigestComparisonRejectsDivergence(t *testing.T) {
	if !identitySourceDigestMatches("same", "same") {
		t.Fatal("matching source digest was rejected")
	}
	if identitySourceDigestMatches("divergent", "claimed") || identitySourceDigestMatches("", "claimed") || identitySourceDigestMatches("claimed", "") {
		t.Fatal("divergent or missing source digest was accepted")
	}
}

func TestIdentityGatewayAuthorizationOrderingAndRecoveryContracts(t *testing.T) {
	sourceBytes, err := os.ReadFile("identity_gateway.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	functionBody := func(name string) string {
		start := strings.Index(source, "func "+name+"(")
		if start < 0 {
			t.Fatalf("gateway function %s is missing", name)
		}
		end := strings.Index(source[start+len("func "+name+"("):], "\nfunc ")
		if end < 0 {
			end = len(source) - (start + len("func "+name+"("))
		}
		return source[start : start+len("func "+name+"(")+end]
	}
	commit := functionBody("AuthorizeCommitProjection")
	fsObject := functionBody("AuthorizeFSObjectProjection")
	for _, test := range []struct {
		name   string
		body   string
		before string
		after  string
	}{
		{name: "commit claim precedes source verification", body: commit, before: "ClaimIdentityAuthorityAt", after: "verifyCommitSourceProjection"},
		{name: "fs claim precedes source verification", body: fsObject, before: "ClaimIdentityAuthorityAt", after: "verifyFSObjectSourceProjection"},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := strings.LastIndex(test.body, test.before)
			after := strings.LastIndex(test.body, test.after)
			if before < 0 || after < 0 || before > after {
				t.Fatalf("gateway ordering %s=%d %s=%d is invalid", test.before, before, test.after, after)
			}
		})
	}
	for _, required := range []string{
		"commitRetryCreatedAt",
		"identityExactRetryMatches",
		"identityClaimOutcomeAuthorizesSource",
		"verifyCommitSourceProjection",
		"verifyFSObjectSourceProjection",
	} {
		if !strings.Contains(commit+fsObject, required) {
			t.Fatalf("gateway recovery contract no longer references %s", required)
		}
	}
	if got := strings.Count(commit, "verifyCommitSourceProjection(ctx, session, p)"); got != 4 {
		t.Fatalf("commit gateway source verification calls=%d, want one for pre-read retry, established, idempotent and conflict paths", got)
	}
	if got := strings.Count(fsObject, "verifyFSObjectSourceProjection(ctx, session, p)"); got != 2 {
		t.Fatalf("fs gateway source verification calls=%d, want compatibility and normal paths", got)
	}
	if got := strings.Count(fsObject, "identityExactRetryMatches"); got != 2 {
		t.Fatalf("fs gateway exact re-claim checks=%d, want compatibility and ordinary conflict paths", got)
	}
	compatibilityStart := strings.Index(fsObject, "compatibleSHA1OnlyProjection")
	if compatibilityStart < 0 || !strings.Contains(fsObject[compatibilityStart:], "exact SHA1-only compatibility claim") || !strings.Contains(fsObject[compatibilityStart:], "identityExactRetryMatches") {
		t.Fatal("SHA1-only compatibility path no longer settles an exact Idempotent claim")
	}
	fsVerification := functionBody("VerifyFSObjectProjection")
	if strings.Contains(fsVerification, "sha1OnlyDigest") || strings.Contains(fsVerification, "FileIdentityDigest") || strings.Contains(fsVerification, "IdentityVerificationVerified") {
		t.Fatal("fs-object verification introduced a non-exact SHA1-only fallback")
	}
	if !strings.Contains(commit, "result.Outcome == IdentityClaimUnknown") || !strings.Contains(fsObject, "result.Outcome == IdentityClaimUnknown") {
		t.Fatal("unknown claim outcomes no longer fail closed before source authorization")
	}
	conflictStart := strings.Index(commit, "case IdentityClaimConflict:")
	if conflictStart < 0 || !strings.Contains(commit[conflictStart:], "retry, retryErr := ClaimIdentityAuthorityAt") || !strings.Contains(commit[conflictStart:], "identityExactRetryMatches") {
		t.Fatal("commit conflict path no longer requires an exact idempotent re-claim")
	}
	verification := functionBody("verifyCommitSourceProjection")
	if !strings.Contains(verification, "IdentityAuthorityConflict") || !strings.Contains(verification, "identitySourceDigestMatches(actualDigest, expectedDigest)") {
		t.Fatal("commit source divergence no longer fails closed")
	}
}

func TestFSObjectStorageLayoutsMaterializeExplicitEmptyCollections(t *testing.T) {
	blockIDs, seafileIDs := fsObjectStorageBlockColumns(FSObjectProjection{FileLayout: FileStorageSHA1Only})
	if blockIDs == nil || len(blockIDs) != 0 || seafileIDs != nil {
		t.Fatalf("SHA1-only empty columns = %#v/%#v, want non-nil empty block_ids and nil paired column", blockIDs, seafileIDs)
	}
	blockIDs, seafileIDs = fsObjectStorageBlockColumns(FSObjectProjection{FileLayout: FileStoragePairedCanonical})
	if blockIDs == nil || seafileIDs == nil || len(blockIDs) != 0 || len(seafileIDs) != 0 {
		t.Fatalf("paired empty columns = %#v/%#v, want two non-nil empty collections", blockIDs, seafileIDs)
	}
}

func TestFSObjectSourceRowPreservesNullableSizeSemantics(t *testing.T) {
	libraryID := "00000000-0000-4000-8000-000000000001"

	t.Run("file with null size and blocks is partial", func(t *testing.T) {
		row := map[string]interface{}{"obj_type": "file", "block_ids": []string{"block-a"}}
		if _, placeholder, err := fsObjectProjectionFromIdentitySourceRow(libraryID, "fs", row); !errors.Is(err, IdentityAuthorityConflict) || placeholder {
			t.Fatalf("NULL size file row placeholder=%v err=%v, want a conflict", placeholder, err)
		}
	})

	t.Run("explicit zero size remains a complete empty file", func(t *testing.T) {
		row := map[string]interface{}{
			"obj_type": "file", "size_bytes": int64(0), "block_ids": []string{},
			"seafile_block_ids_sha1": []string(nil),
		}
		got, placeholder, err := fsObjectProjectionFromIdentitySourceRow(libraryID, "fs", row)
		if err != nil || placeholder || got.ObjectType != "file" || got.SizeBytes != 0 || got.FileLayout != FileStorageSHA1Only {
			t.Fatalf("explicit zero file = %+v placeholder=%v err=%v, want complete zero-size file", got, placeholder, err)
		}
	})

	t.Run("directory accepts null size", func(t *testing.T) {
		row := map[string]interface{}{
			"obj_type": "dir", "dir_entries": "[]",
			"block_ids": []string(nil), "seafile_block_ids_sha1": []string(nil),
		}
		got, placeholder, err := fsObjectProjectionFromIdentitySourceRow(libraryID, "fs", row)
		if err != nil || placeholder || got.ObjectType != "dir" || got.DirectoryEntries != "[]" {
			t.Fatalf("NULL size directory = %+v placeholder=%v err=%v, want valid directory projection", got, placeholder, err)
		}
	})
}
