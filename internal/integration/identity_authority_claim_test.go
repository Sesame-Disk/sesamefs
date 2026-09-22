//go:build integration

package integration

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

// PC-D1B identity-authority claim lifecycle against real Cassandra. This is
// the M16 evidence at the claim layer, with no certifier involved: a claim is
// write-once, an identical retry is idempotent, a different digest is a
// conflict, and the claim survives deletion of the source row it describes so
// a re-created key cannot mint fresh provenance.
//
// The primitive has no production consumer yet; these tests exercise it
// directly. Every row they touch lives in a throwaway library id.

// identityAuthorityEvidenceEnv is the evidence gate for these tests. With it set,
// a SKIP or an unreachable stack is a failure rather than a silent "ok"; TestMain
// carries the other half of the gate so the run cannot exit 0 before reaching
// these tests. TestEveryEvidenceGateIsWiredIntoTestMain enforces the pairing.
const identityAuthorityEvidenceEnv = "SESAMEFS_REQUIRE_IDENTITY_AUTHORITY_EVIDENCE"

func identityTestCommitDigest(libraryID, commitID, parentID, rootFSID string) string {
	digest, err := dbpkg.CommitIdentityDigest(libraryID, commitID, parentID, rootFSID, "22222222-2222-2222-2222-222222222222", "test-description", time.UnixMilli(1_700_000_000_123))
	if err != nil {
		panic(err)
	}
	return digest
}

func identityTestFileDigest(libraryID, fsID string, size int64, logical, canonical []string) string {
	digest, err := dbpkg.FileIdentityDigest(libraryID, fsID, size, logical, canonical)
	if err != nil {
		panic(err)
	}
	return digest
}

func identityTestDirectoryDigest(libraryID, fsID, entries string) string {
	digest, err := dbpkg.DirectoryIdentityDigest(libraryID, fsID, entries)
	if err != nil {
		panic(err)
	}
	return digest
}

func identityAuthorityDB(t *testing.T) *dbpkg.DB {
	t.Helper()
	database := shareProjectionDBForTest(t)
	if database == nil && os.Getenv(identityAuthorityEvidenceEnv) == "1" {
		t.Fatalf("%s=1 but no Cassandra session is available", identityAuthorityEvidenceEnv)
	}
	return database
}

func identityClaimCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func TestIdentityAuthorityClaimIsWriteOnceOnRealCassandra(t *testing.T) {
	database := identityAuthorityDB(t)
	ctx, cancel := identityClaimCtx(t)
	defer cancel()

	library := uuid.NewString()
	commitID := "c-" + uuid.NewString()
	digestA := identityTestCommitDigest(library, commitID, "", "root-a")
	digestB := identityTestCommitDigest(library, commitID, "", "root-b")

	first, err := dbpkg.ClaimIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindCommit, commitID, dbpkg.SupportedIdentityDigestVersion, digestA)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if first.Outcome != dbpkg.IdentityClaimEstablished {
		t.Fatalf("first claim outcome=%v, want established", first.Outcome)
	}

	retry, err := dbpkg.ClaimIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindCommit, commitID, dbpkg.SupportedIdentityDigestVersion, digestA)
	if err != nil {
		t.Fatalf("identical retry: %v", err)
	}
	if retry.Outcome != dbpkg.IdentityClaimIdempotent {
		t.Fatalf("identical retry outcome=%v, want idempotent", retry.Outcome)
	}

	conflict, err := dbpkg.ClaimIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindCommit, commitID, dbpkg.SupportedIdentityDigestVersion, digestB)
	if err != nil {
		t.Fatalf("conflicting claim: %v", err)
	}
	if conflict.Outcome != dbpkg.IdentityClaimConflict {
		t.Fatalf("conflicting claim outcome=%v, want conflict", conflict.Outcome)
	}
	if conflict.Stored == nil || conflict.Stored.Digest != digestA {
		t.Fatalf("conflict did not report the winning digest: %+v", conflict.Stored)
	}

	// The stored claim is still A after the losing attempt.
	stored, found, err := dbpkg.ReadIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindCommit, commitID)
	if err != nil || !found {
		t.Fatalf("read after conflict: found=%v err=%v", found, err)
	}
	if stored.Digest != digestA {
		t.Fatalf("stored digest changed after a conflicting claim: %s", stored.Digest)
	}
	if outcome, err := dbpkg.VerifyIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindCommit, commitID, dbpkg.SupportedIdentityDigestVersion, digestB); err != nil || outcome != dbpkg.IdentityVerificationConflict {
		t.Fatalf("verify with the losing digest: outcome=%v err=%v, want conflict", outcome, err)
	}
}

// M16 proper: deleting the source row must not touch the claim, and writing
// the same key again with a different projection is a conflict, exactly as if
// the row had never been deleted.
func TestIdentityAuthorityClaimSurvivesSourceRowDeleteOnRealCassandra(t *testing.T) {
	database := identityAuthorityDB(t)
	ctx, cancel := identityClaimCtx(t)
	defer cancel()

	library := uuid.NewString()
	fsID := "f-" + uuid.NewString()
	logical := []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	canonicalA := []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	canonicalB := []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	digestA := identityTestFileDigest(library, fsID, 10, logical, canonicalA)
	digestB := identityTestFileDigest(library, fsID, 10, logical, canonicalB)

	// Materialize a source row and claim it.
	if err := database.Session().Query(`
		INSERT INTO fs_objects (library_id, fs_id, obj_type, size_bytes, block_ids, seafile_block_ids_sha1)
		VALUES (?, ?, ?, ?, ?, ?)
	`, library, fsID, "file", int64(10), canonicalA, logical).WithContext(ctx).Exec(); err != nil {
		t.Fatalf("insert source row: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Session().Query(`DELETE FROM fs_objects WHERE library_id = ? AND fs_id = ?`, library, fsID).Exec()
	})
	if res, err := dbpkg.ClaimIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindFSObject, fsID, dbpkg.SupportedIdentityDigestVersion, digestA); err != nil || res.Outcome != dbpkg.IdentityClaimEstablished {
		t.Fatalf("claim A: outcome=%v err=%v", res.Outcome, err)
	}

	// Delete the source row the way every production deleter does: plain
	// DELETE, no authority consultation.
	if err := database.Session().Query(`DELETE FROM fs_objects WHERE library_id = ? AND fs_id = ?`, library, fsID).WithContext(ctx).Exec(); err != nil {
		t.Fatalf("delete source row: %v", err)
	}

	// The claim is untouched.
	stored, found, err := dbpkg.ReadIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindFSObject, fsID)
	if err != nil || !found {
		t.Fatalf("claim after delete: found=%v err=%v; the claim must survive deletion of its source row", found, err)
	}
	if stored.Digest != digestA {
		t.Fatalf("claim digest changed across the delete: %s", stored.Digest)
	}

	// Re-create the same key with a different canonical dependency: this is
	// the ABA the claim exists to refuse.
	if err := database.Session().Query(`
		INSERT INTO fs_objects (library_id, fs_id, obj_type, size_bytes, block_ids, seafile_block_ids_sha1)
		VALUES (?, ?, ?, ?, ?, ?)
	`, library, fsID, "file", int64(10), canonicalB, logical).WithContext(ctx).Exec(); err != nil {
		t.Fatalf("re-create source row: %v", err)
	}
	res, err := dbpkg.ClaimIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindFSObject, fsID, dbpkg.SupportedIdentityDigestVersion, digestB)
	if err != nil {
		t.Fatalf("claim after re-create: %v", err)
	}
	if res.Outcome != dbpkg.IdentityClaimConflict {
		t.Fatalf("re-created key with a different digest got outcome=%v, want conflict (never a fresh first claim)", res.Outcome)
	}
	if outcome, err := dbpkg.VerifyIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindFSObject, fsID, dbpkg.SupportedIdentityDigestVersion, digestB); err != nil || outcome != dbpkg.IdentityVerificationConflict {
		t.Fatalf("verify re-created row: outcome=%v err=%v, want conflict", outcome, err)
	}
	// And the original projection still verifies.
	if outcome, err := dbpkg.VerifyIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindFSObject, fsID, dbpkg.SupportedIdentityDigestVersion, digestA); err != nil || outcome != dbpkg.IdentityVerificationVerified {
		t.Fatalf("verify original projection: outcome=%v err=%v, want idempotent", outcome, err)
	}
}

// A directory and a file with the same fs_id address one fs_objects row and
// therefore one authority key. Delete/re-create in either direction must keep
// the original claim and reject the replacement projection.
func TestIdentityAuthorityFSObjectSubtypeRecreateConflictsOnRealCassandra(t *testing.T) {
	database := identityAuthorityDB(t)
	ctx, cancel := identityClaimCtx(t)
	defer cancel()

	library := uuid.NewString()
	logical := []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	canonical := []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	for _, tc := range []struct {
		name       string
		firstIsDir bool
	}{
		{"directory to file", true},
		{"file to directory", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsID := "f-" + uuid.NewString()
			directoryDigest := identityTestDirectoryDigest(library, fsID, "[]")
			fileDigest := identityTestFileDigest(library, fsID, 10, logical, canonical)
			insert := func(isDir bool) error {
				if isDir {
					return database.Session().Query(`
						INSERT INTO fs_objects (library_id, fs_id, obj_type, dir_entries)
						VALUES (?, ?, ?, ?)
					`, library, fsID, "dir", "[]").WithContext(ctx).Exec()
				}
				return database.Session().Query(`
					INSERT INTO fs_objects (library_id, fs_id, obj_type, size_bytes, block_ids, seafile_block_ids_sha1)
					VALUES (?, ?, ?, ?, ?, ?)
				`, library, fsID, "file", int64(10), canonical, logical).WithContext(ctx).Exec()
			}
			firstDigest, replacementDigest := directoryDigest, fileDigest
			if !tc.firstIsDir {
				firstDigest, replacementDigest = fileDigest, directoryDigest
			}
			if err := insert(tc.firstIsDir); err != nil {
				t.Fatalf("insert original source row: %v", err)
			}
			t.Cleanup(func() {
				_ = database.Session().Query(`DELETE FROM fs_objects WHERE library_id = ? AND fs_id = ?`, library, fsID).Exec()
			})
			first, err := dbpkg.ClaimIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindFSObject, fsID, dbpkg.SupportedIdentityDigestVersion, firstDigest)
			if err != nil || first.Outcome != dbpkg.IdentityClaimEstablished {
				t.Fatalf("claim original subtype: outcome=%v err=%v", first.Outcome, err)
			}
			if err := database.Session().Query(`DELETE FROM fs_objects WHERE library_id = ? AND fs_id = ?`, library, fsID).WithContext(ctx).Exec(); err != nil {
				t.Fatalf("delete original source row: %v", err)
			}
			if err := insert(!tc.firstIsDir); err != nil {
				t.Fatalf("re-create source row with other subtype: %v", err)
			}
			recreated, err := dbpkg.ClaimIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindFSObject, fsID, dbpkg.SupportedIdentityDigestVersion, replacementDigest)
			if err != nil || recreated.Outcome != dbpkg.IdentityClaimConflict {
				t.Fatalf("re-created identity with other subtype: outcome=%v err=%v, want conflict", recreated.Outcome, err)
			}
			if got, err := dbpkg.VerifyIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindFSObject, fsID, dbpkg.SupportedIdentityDigestVersion, firstDigest); err != nil || got != dbpkg.IdentityVerificationVerified {
				t.Fatalf("verify original projection: outcome=%v err=%v, want verified", got, err)
			}
			if got, err := dbpkg.VerifyIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindFSObject, fsID, dbpkg.SupportedIdentityDigestVersion, replacementDigest); err != nil || got != dbpkg.IdentityVerificationConflict {
				t.Fatalf("verify replacement projection: outcome=%v err=%v, want conflict", got, err)
			}
		})
	}
}

// Concurrent first claims have at most one observed Established result.
// The final SERIAL read is authoritative even if the winner's successful CAS
// acknowledgement was lost and every client observed Unknown.
func TestIdentityAuthorityConcurrentClaimsHaveOneWinnerOnRealCassandra(t *testing.T) {
	database := identityAuthorityDB(t)
	ctx, cancel := identityClaimCtx(t)
	defer cancel()

	library := uuid.NewString()
	commitID := "c-" + uuid.NewString()
	const contenders = 8
	digests := make([]string, contenders)
	for i := range digests {
		digests[i] = identityTestCommitDigest(library, commitID, "", "root-"+string(rune('a'+i)))
	}

	outcomes := make([]dbpkg.IdentityClaimResult, contenders)
	errs := make([]error, contenders)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			outcomes[i], errs[i] = dbpkg.ClaimIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindCommit, commitID, dbpkg.SupportedIdentityDigestVersion, digests[i])
		}(i)
	}
	close(start)
	wg.Wait()

	established := 0
	establishedDigest := ""
	for i := 0; i < contenders; i++ {
		if errs[i] != nil {
			// An ambiguous LWT is Unknown, which is not a positive outcome; it
			// is tolerated here but must never be counted as a win.
			if outcomes[i].Outcome != dbpkg.IdentityClaimUnknown {
				t.Fatalf("contender %d returned err=%v with outcome=%v, want unknown", i, errs[i], outcomes[i].Outcome)
			}
			continue
		}
		switch outcomes[i].Outcome {
		case dbpkg.IdentityClaimEstablished:
			established++
			establishedDigest = digests[i]
		case dbpkg.IdentityClaimConflict:
			if outcomes[i].Stored == nil {
				t.Fatalf("contender %d lost without seeing the winner", i)
			}
		default:
			t.Fatalf("contender %d outcome=%v, want established or conflict", i, outcomes[i].Outcome)
		}
	}
	if established > 1 {
		t.Fatalf("observed Established results=%d, want at most 1", established)
	}

	stored, found, err := dbpkg.ReadIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindCommit, commitID)
	if err != nil || !found {
		t.Fatalf("read winner: found=%v err=%v", found, err)
	}
	winners := 0
	for _, d := range digests {
		if d == stored.Digest {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("stored digest matches %d contenders, want exactly 1", winners)
	}
	if established == 1 && establishedDigest != stored.Digest {
		t.Fatalf("observed Established digest %s differs from final stored winner %s", establishedDigest, stored.Digest)
	}
}

func TestIdentityAuthorityGatewayCommitCrashRetryAndRejectsDivergenceOnRealCassandra(t *testing.T) {
	database := identityAuthorityDB(t)
	ctx, cancel := identityClaimCtx(t)
	defer cancel()
	libraryID := uuid.NewString()
	commitID := "gateway-" + uuid.NewString()
	creatorID := uuid.NewString()
	initialTime := time.Date(2026, time.September, 21, 18, 0, 0, 987654321, time.UTC)
	projection := dbpkg.CommitProjection{
		LibraryID: libraryID, CommitID: commitID, ParentID: "", RootFSID: "root-original",
		CreatorID: creatorID, Description: "same complete projection", CreatedAt: initialTime,
	}

	// Process one establishes provenance and disappears before source materialization.
	first, err := dbpkg.AuthorizeCommitProjection(ctx, database.Session(), projection)
	if err != nil || first == nil {
		t.Fatalf("first authorization = %v, err=%v", first, err)
	}
	claim, found, err := dbpkg.ReadIdentityAuthority(ctx, database.Session(), libraryID, dbpkg.IdentityKindCommit, commitID)
	if err != nil || !found {
		t.Fatalf("read durable claim: found=%v err=%v", found, err)
	}
	wantCreatedAt := time.UnixMilli(initialTime.UnixMilli()).UTC()
	if !claim.CreatedAt.Equal(wantCreatedAt) {
		t.Fatalf("claim.created_at=%s, want %s", claim.CreatedAt, wantCreatedAt)
	}

	// A fresh process proposes a different server time. It must recover T from
	// the SERIAL claim and materialize exactly the row whose V1 digest was claimed.
	projection.CreatedAt = initialTime.Add(24 * time.Hour)
	retry, err := dbpkg.AuthorizeCommitProjection(ctx, database.Session(), projection)
	if err != nil || retry == nil {
		t.Fatalf("crash retry authorization = %v, err=%v", retry, err)
	}
	if err := dbpkg.MaterializeAuthorizedCommit(database.Session(), retry); err != nil {
		t.Fatalf("materialize recovered commit: %v", err)
	}
	var storedCreator, storedDescription string
	var storedCreatedAt time.Time
	if err := database.Session().Query("SELECT creator_id, description, created_at FROM commits WHERE library_id = ? AND commit_id = ?", libraryID, commitID).
		WithContext(ctx).Scan(&storedCreator, &storedDescription, &storedCreatedAt); err != nil {
		t.Fatalf("read materialized commit: %v", err)
	}
	if storedCreator != creatorID || storedDescription != projection.Description || !storedCreatedAt.Equal(claim.CreatedAt) {
		t.Fatalf("source projection creator=%q description=%q created_at=%s, claim=%+v", storedCreator, storedDescription, storedCreatedAt, claim)
	}

	conflictingDescription := projection
	conflictingDescription.Description = "different description"
	if _, err := dbpkg.AuthorizeCommitProjection(ctx, database.Session(), conflictingDescription); !errors.Is(err, dbpkg.IdentityAuthorityConflict) {
		t.Fatalf("description conflict error=%v, want IdentityAuthorityConflict", err)
	}
	conflictingCreator := projection
	conflictingCreator.CreatorID = uuid.NewString()
	if _, err := dbpkg.AuthorizeCommitProjection(ctx, database.Session(), conflictingCreator); !errors.Is(err, dbpkg.IdentityAuthorityConflict) {
		t.Fatalf("creator conflict error=%v, want IdentityAuthorityConflict", err)
	}
	if err := database.Session().Query("SELECT creator_id, description, created_at FROM commits WHERE library_id = ? AND commit_id = ?", libraryID, commitID).
		WithContext(ctx).Scan(&storedCreator, &storedDescription, &storedCreatedAt); err != nil {
		t.Fatalf("re-read source after conflicts: %v", err)
	}
	if storedCreator != creatorID || storedDescription != "same complete projection" || !storedCreatedAt.Equal(claim.CreatedAt) {
		t.Fatalf("conflict changed the source row: creator=%q description=%q created_at=%s", storedCreator, storedDescription, storedCreatedAt)
	}

	// Simulate corruption outside the gateway. Exact claim retry must reject
	// the divergent source instead of silently overwriting it with the claim.
	if err := database.Session().Query("UPDATE commits SET description = ? WHERE library_id = ? AND commit_id = ?", "rogue source divergence", libraryID, commitID).WithContext(ctx).Exec(); err != nil {
		t.Fatalf("inject divergent source row: %v", err)
	}
	if _, err := dbpkg.AuthorizeCommitProjection(ctx, database.Session(), projection); !errors.Is(err, dbpkg.IdentityAuthorityConflict) {
		t.Fatalf("divergent source authorization error=%v, want IdentityAuthorityConflict", err)
	}
	if err := database.Session().Query("SELECT description FROM commits WHERE library_id = ? AND commit_id = ?", libraryID, commitID).WithContext(ctx).Scan(&storedDescription); err != nil {
		t.Fatalf("read divergent source after rejected authorization: %v", err)
	}
	if storedDescription != "rogue source divergence" {
		t.Fatalf("rejected authorization modified divergent source: description=%q", storedDescription)
	}
	_ = first
}

func TestIdentityAuthorityGatewayDeleteVerifiesAndPreservesClaimOnRealCassandra(t *testing.T) {
	database := identityAuthorityDB(t)
	ctx, cancel := identityClaimCtx(t)
	defer cancel()
	libraryID := uuid.NewString()
	commitID := "gateway-delete-" + uuid.NewString()
	projection := dbpkg.CommitProjection{
		LibraryID: libraryID, CommitID: commitID, RootFSID: "root-delete",
		CreatorID: uuid.NewString(), Description: "delete boundary", CreatedAt: time.Now().UTC(),
	}
	authorized, err := dbpkg.AuthorizeCommitProjection(ctx, database.Session(), projection)
	if err != nil {
		t.Fatalf("authorize commit: %v", err)
	}
	if err := dbpkg.MaterializeAuthorizedCommit(database.Session(), authorized); err != nil {
		t.Fatalf("materialize commit: %v", err)
	}
	if err := dbpkg.DeleteCommitIdentity(database.Session(), libraryID, commitID); err != nil {
		t.Fatalf("delete authorized commit: %v", err)
	}
	var stored string
	if err := database.Session().Query("SELECT commit_id FROM commits WHERE library_id = ? AND commit_id = ?", libraryID, commitID).WithContext(ctx).Scan(&stored); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("source row after delete err=%v value=%q, want absent", err, stored)
	}
	claim, found, err := dbpkg.ReadIdentityAuthority(ctx, database.Session(), libraryID, dbpkg.IdentityKindCommit, commitID)
	if err != nil || !found {
		t.Fatalf("claim after source delete found=%v err=%v, want preserved", found, err)
	}
	retry, err := dbpkg.AuthorizeCommitProjection(ctx, database.Session(), projection)
	if err != nil {
		t.Fatalf("authorize exact re-create after delete: %v", err)
	}
	if err := dbpkg.MaterializeAuthorizedCommit(database.Session(), retry); err != nil {
		t.Fatalf("materialize exact re-create after delete: %v", err)
	}
	projection.RootFSID = "different-root"
	if _, err := dbpkg.AuthorizeCommitProjection(ctx, database.Session(), projection); !errors.Is(err, dbpkg.IdentityAuthorityConflict) {
		t.Fatalf("conflicting re-create error=%v, want IdentityAuthorityConflict", err)
	}
	if claim.Digest == "" {
		t.Fatal("source deletion erased or invalidated the authority claim")
	}
}

func TestIdentityAuthorityGatewayFSObjectDeletePreservesClaimsOnRealCassandra(t *testing.T) {
	database := identityAuthorityDB(t)
	ctx, cancel := identityClaimCtx(t)
	defer cancel()
	libraryID := uuid.NewString()
	logicalBlockID := "1111111111111111111111111111111111111111"

	projections := []struct {
		name       string
		projection dbpkg.FSObjectProjection
		conflict   func(dbpkg.FSObjectProjection) dbpkg.FSObjectProjection
	}{
		{
			name: "directory",
			projection: dbpkg.FSObjectProjection{
				LibraryID: libraryID, FSID: "dir-" + uuid.NewString(), ObjectType: "dir", DirectoryEntries: "[]",
			},
			conflict: func(p dbpkg.FSObjectProjection) dbpkg.FSObjectProjection {
				p.DirectoryEntries = "{}"
				return p
			},
		},
		{
			name: "sha1-only-file",
			projection: dbpkg.FSObjectProjection{
				LibraryID: libraryID, FSID: "file-" + uuid.NewString(), ObjectType: "file", SizeBytes: 7,
				FileLayout: dbpkg.FileStorageSHA1Only, LogicalSHA1IDs: []string{logicalBlockID},
			},
			conflict: func(p dbpkg.FSObjectProjection) dbpkg.FSObjectProjection {
				p.LogicalSHA1IDs = []string{"2222222222222222222222222222222222222222"}
				return p
			},
		},
		{
			name: "zero-block-file",
			projection: dbpkg.FSObjectProjection{
				LibraryID: libraryID, FSID: "empty-" + uuid.NewString(), ObjectType: "file", FileLayout: dbpkg.FileStorageSHA1Only,
			},
			conflict: func(p dbpkg.FSObjectProjection) dbpkg.FSObjectProjection {
				p.SizeBytes = 1
				return p
			},
		},
	}

	for _, test := range projections {
		t.Run(test.name, func(t *testing.T) {
			projection := test.projection
			authorized, err := dbpkg.AuthorizeFSObjectProjection(ctx, database.Session(), projection)
			if err != nil {
				t.Fatalf("authorize source projection: %v", err)
			}
			if err := dbpkg.MaterializeAuthorizedFSObject(database.Session(), authorized); err != nil {
				t.Fatalf("materialize source projection: %v", err)
			}

			if err := dbpkg.DeleteFSObjectIdentity(database.Session(), libraryID, projection.FSID); err != nil {
				t.Fatalf("delete authorized source row: %v", err)
			}
			var stored string
			if err := database.Session().Query("SELECT fs_id FROM fs_objects WHERE library_id = ? AND fs_id = ?", libraryID, projection.FSID).
				WithContext(ctx).Scan(&stored); !errors.Is(err, gocql.ErrNotFound) {
				t.Fatalf("source row after delete err=%v value=%q, want absent", err, stored)
			}

			claim, found, err := dbpkg.ReadIdentityAuthority(ctx, database.Session(), libraryID, dbpkg.IdentityKindFSObject, projection.FSID)
			if err != nil || !found || claim.DigestVersion != dbpkg.SupportedIdentityDigestVersion || claim.Digest == "" {
				t.Fatalf("claim after source delete found=%v claim=%+v err=%v, want preserved", found, claim, err)
			}
			outcome, err := dbpkg.VerifyFSObjectProjection(ctx, database.Session(), projection)
			if err != nil || outcome != dbpkg.IdentityVerificationVerified {
				t.Fatalf("verify exact claim after source delete outcome=%v err=%v, want verified", outcome, err)
			}
			if _, err := dbpkg.AuthorizeFSObjectProjection(ctx, database.Session(), projection); err != nil {
				t.Fatalf("authorize exact recreation after source delete: %v", err)
			}
			if _, err := dbpkg.AuthorizeFSObjectProjection(ctx, database.Session(), test.conflict(projection)); !errors.Is(err, dbpkg.IdentityAuthorityConflict) {
				t.Fatalf("conflicting recreation error=%v, want IdentityAuthorityConflict", err)
			}
		})
	}
}

func TestIdentityAuthorityGatewayRejectsDivergentZeroBlockSourceDeleteOnRealCassandra(t *testing.T) {
	database := identityAuthorityDB(t)
	ctx, cancel := identityClaimCtx(t)
	defer cancel()
	libraryID := uuid.NewString()
	fsID := "divergent-empty-" + uuid.NewString()
	logicalBlockID := "1111111111111111111111111111111111111111"
	projection := dbpkg.FSObjectProjection{
		LibraryID: libraryID, FSID: fsID, ObjectType: "file", SizeBytes: 7,
		FileLayout: dbpkg.FileStorageSHA1Only, LogicalSHA1IDs: []string{logicalBlockID},
	}
	authorized, err := dbpkg.AuthorizeFSObjectProjection(ctx, database.Session(), projection)
	if err != nil {
		t.Fatalf("authorize non-empty source projection: %v", err)
	}
	if err := dbpkg.MaterializeAuthorizedFSObject(database.Session(), authorized); err != nil {
		t.Fatalf("materialize non-empty source projection: %v", err)
	}

	if err := database.Session().Query("UPDATE fs_objects SET size_bytes = ? WHERE library_id = ? AND fs_id = ?", int64(0), libraryID, fsID).
		WithContext(ctx).Exec(); err != nil {
		t.Fatalf("change source size to zero: %v", err)
	}
	if err := database.Session().Query("DELETE block_ids, seafile_block_ids_sha1, dir_entries FROM fs_objects WHERE library_id = ? AND fs_id = ?", libraryID, fsID).
		WithContext(ctx).Exec(); err != nil {
		t.Fatalf("clear source collections: %v", err)
	}

	row, err := dbpkg.ReadFSObjectIdentitySourceRow(ctx, database.Session(), libraryID, fsID)
	if err != nil {
		t.Fatalf("read divergent zero-block source: %v", err)
	}
	if row["obj_type"] != "file" || row["size_bytes"] != int64(0) ||
		dbpkg.IdentitySourceValuePresent(row, "block_ids") ||
		dbpkg.IdentitySourceValuePresent(row, "seafile_block_ids_sha1") {
		t.Fatalf("source row=%#v, want file size=0 with both block lists NULL", row)
	}
	if err := dbpkg.DeleteFSObjectIdentity(database.Session(), libraryID, fsID); !errors.Is(err, dbpkg.IdentityAuthorityConflict) {
		t.Fatalf("delete divergent zero-block source error=%v, want IdentityAuthorityConflict", err)
	}
	var survivingFSID string
	if err := database.Session().Query("SELECT fs_id FROM fs_objects WHERE library_id = ? AND fs_id = ?", libraryID, fsID).
		WithContext(ctx).Scan(&survivingFSID); err != nil || survivingFSID != fsID {
		t.Fatalf("divergent source after rejected delete=%q err=%v, want source preserved", survivingFSID, err)
	}
	claim, found, err := dbpkg.ReadIdentityAuthority(ctx, database.Session(), libraryID, dbpkg.IdentityKindFSObject, fsID)
	wantDigest := identityTestFileDigest(libraryID, fsID, 7, []string{logicalBlockID}, nil)
	if err != nil || !found || claim.Digest != wantDigest {
		t.Fatalf("claim after rejected delete found=%v claim=%+v err=%v, want original non-empty claim", found, claim, err)
	}
}

func TestIdentityAuthorityGatewayPreservesNullableFSObjectSizeOnRealCassandra(t *testing.T) {
	database := identityAuthorityDB(t)
	ctx, cancel := identityClaimCtx(t)
	defer cancel()
	libraryID := uuid.NewString()
	session := database.Session()
	logicalBlockID := "2222222222222222222222222222222222222222"
	fileProjection := dbpkg.FSObjectProjection{
		LibraryID: libraryID, FSID: "zero-sized-with-block-" + uuid.NewString(),
		ObjectType: "file", SizeBytes: 0,
		FileLayout: dbpkg.FileStorageSHA1Only, LogicalSHA1IDs: []string{logicalBlockID},
	}
	authorized, err := dbpkg.AuthorizeFSObjectProjection(ctx, session, fileProjection)
	if err != nil {
		t.Fatalf("authorize explicit zero-size file: %v", err)
	}
	if err := dbpkg.MaterializeAuthorizedFSObject(session, authorized); err != nil {
		t.Fatalf("materialize explicit zero-size file: %v", err)
	}
	row, err := dbpkg.ReadFSObjectIdentitySourceRow(ctx, session, libraryID, fileProjection.FSID)
	if err != nil {
		t.Fatalf("read explicit zero-size file: %v", err)
	}
	if size, ok := row["size_bytes"].(int64); !ok || size != 0 {
		t.Fatalf("explicit zero size was not preserved: %#v", row["size_bytes"])
	}
	if _, err := dbpkg.AuthorizeFSObjectProjection(ctx, session, fileProjection); err != nil {
		t.Fatalf("matching explicit zero-size file retry: %v", err)
	}

	if err := session.Query("DELETE size_bytes FROM fs_objects WHERE library_id = ? AND fs_id = ?", libraryID, fileProjection.FSID).
		WithContext(ctx).Exec(); err != nil {
		t.Fatalf("set file size to NULL: %v", err)
	}
	row, err = dbpkg.ReadFSObjectIdentitySourceRow(ctx, session, libraryID, fileProjection.FSID)
	if err != nil {
		t.Fatalf("read file with NULL size: %v", err)
	}
	if _, exists := row["size_bytes"]; exists {
		t.Fatalf("NULL size was materialized as present zero: %#v", row["size_bytes"])
	}
	if _, err := dbpkg.AuthorizeFSObjectProjection(ctx, session, fileProjection); !errors.Is(err, dbpkg.IdentityAuthorityConflict) {
		t.Fatalf("retry with NULL size and non-empty blocks error=%v, want IdentityAuthorityConflict", err)
	}
	if err := dbpkg.DeleteFSObjectIdentity(session, libraryID, fileProjection.FSID); !errors.Is(err, dbpkg.IdentityAuthorityConflict) {
		t.Fatalf("delete with NULL size and non-empty blocks error=%v, want IdentityAuthorityConflict", err)
	}
	var survivingFSID string
	if err := session.Query("SELECT fs_id FROM fs_objects WHERE library_id = ? AND fs_id = ?", libraryID, fileProjection.FSID).
		WithContext(ctx).Scan(&survivingFSID); err != nil || survivingFSID != fileProjection.FSID {
		t.Fatalf("partial file after rejected delete=%q err=%v, want source preserved", survivingFSID, err)
	}

	directoryProjection := dbpkg.FSObjectProjection{
		LibraryID: libraryID, FSID: "directory-null-size-" + uuid.NewString(),
		ObjectType: "dir", DirectoryEntries: "[]",
	}
	directory, err := dbpkg.AuthorizeFSObjectProjection(ctx, session, directoryProjection)
	if err != nil {
		t.Fatalf("authorize directory with NULL size: %v", err)
	}
	if err := dbpkg.MaterializeAuthorizedFSObject(session, directory); err != nil {
		t.Fatalf("materialize directory with NULL size: %v", err)
	}
	row, err = dbpkg.ReadFSObjectIdentitySourceRow(ctx, session, libraryID, directoryProjection.FSID)
	if err != nil {
		t.Fatalf("read directory with NULL size: %v", err)
	}
	if _, exists := row["size_bytes"]; exists {
		t.Fatalf("directory NULL size was materialized as present: %#v", row["size_bytes"])
	}
	if _, err := dbpkg.AuthorizeFSObjectProjection(ctx, session, directoryProjection); err != nil {
		t.Fatalf("matching directory retry with NULL size: %v", err)
	}
	if err := dbpkg.DeleteFSObjectIdentity(session, libraryID, directoryProjection.FSID); err != nil {
		t.Fatalf("delete directory with NULL size: %v", err)
	}
}
