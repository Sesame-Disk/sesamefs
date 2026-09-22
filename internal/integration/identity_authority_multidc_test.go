//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/config"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

// PC-D1B identity-authority 3-DC evidence. The property under test is the one
// the decision exists for: a claim pins the canonical global SERIAL domain
// explicitly, so concurrent first claims for one identity issued from three
// datacenters, over sessions whose configured default is LOCAL_SERIAL, still
// have exactly one winner, and every datacenter reads that same winner.
//
// If the primitive inherited the session default, each DC could win its own
// local Paxos round and multiple "authoritative" digests would coexist. A final
// SERIAL read, rather than a successful client acknowledgement, identifies the
// one stored winner when a winner's CAS response is ambiguous.

func identityAuthority3DCReady(t *testing.T) map[string]string {
	t.Helper()
	if os.Getenv(identityAuthorityEvidenceEnv) != "1" {
		t.Skipf("%s is not set", identityAuthorityEvidenceEnv)
	}
	if strings.TrimSpace(os.Getenv(w2PostHeadMultidcEndpoints)) == "" {
		t.Skipf("%s is not set; 3-DC leg needs the isolated fixture", w2PostHeadMultidcEndpoints)
	}
	return w2PostHead3DCEndpoints(t)
}

func identityAuthorityReadEventually(ctx context.Context, session *gocql.Session, libraryID string, kind dbpkg.IdentityKind, identityID string) (dbpkg.IdentityAuthorityClaim, bool, error) {
	var lastErr error
	for attempt := 0; attempt < 40; attempt++ {
		claim, found, err := dbpkg.ReadIdentityAuthority(ctx, session, libraryID, kind, identityID)
		if err == nil && found {
			return claim, true, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("identity claim %s/%s is not visible yet", kind, identityID)
		}
		select {
		case <-ctx.Done():
			return dbpkg.IdentityAuthorityClaim{}, false, lastErr
		case <-time.After(500 * time.Millisecond):
		}
	}
	return dbpkg.IdentityAuthorityClaim{}, false, lastErr
}
func TestIdentityAuthorityConcurrentCrossDCClaimsHaveOneWinner3DC(t *testing.T) {
	endpoints := identityAuthority3DCReady(t)
	na := w2PostHead3DCConnectSerial(t, "dc-na", endpoints, "LOCAL_SERIAL")
	eu := w2PostHead3DCConnectSerial(t, "dc-eu", endpoints, "LOCAL_SERIAL")
	asia := w2PostHead3DCConnectSerial(t, "dc-asia", endpoints, "LOCAL_SERIAL")
	sessions := map[string]*dbpkg.DB{"dc-na": na, "dc-eu": eu, "dc-asia": asia}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const rounds = 5
	for round := 0; round < rounds; round++ {
		library := uuid.NewString()
		fsID := "f-" + uuid.NewString()
		logical := []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}

		// Each DC tries to claim a DIFFERENT canonical dependency for the same
		// SHA-1-only-looking identity. Only one may become authoritative.
		digests := map[string]string{}
		for dc := range sessions {
			canonical := []string{strings.Repeat(string(dc[3]), 64)}
			digests[dc] = identityTestFileDigest(library, fsID, 10, logical, canonical)
		}

		type attempt struct {
			dc     string
			result dbpkg.IdentityClaimResult
			err    error
		}
		attempts := make([]attempt, 0, len(sessions))
		var mu sync.Mutex
		var wg sync.WaitGroup
		start := make(chan struct{})
		for dc, database := range sessions {
			wg.Add(1)
			go func(dc string, database *dbpkg.DB) {
				defer wg.Done()
				<-start
				res, err := dbpkg.ClaimIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindFSObject, fsID, dbpkg.SupportedIdentityDigestVersion, digests[dc])
				mu.Lock()
				attempts = append(attempts, attempt{dc: dc, result: res, err: err})
				mu.Unlock()
			}(dc, database)
		}
		close(start)
		wg.Wait()

		established := 0
		establishedDigest := ""
		for _, a := range attempts {
			if a.err != nil {
				if a.result.Outcome != dbpkg.IdentityClaimUnknown {
					t.Fatalf("round %d %s: err=%v with outcome=%v, want unknown", round, a.dc, a.err, a.result.Outcome)
				}
				t.Logf("round %d %s: ambiguous claim (%v); tolerated, never counted as a win", round, a.dc, a.err)
				continue
			}
			switch a.result.Outcome {
			case dbpkg.IdentityClaimEstablished:
				established++
				establishedDigest = digests[a.dc]
			case dbpkg.IdentityClaimConflict:
				if a.result.Stored == nil {
					t.Fatalf("round %d %s: lost without observing the winner", round, a.dc)
				}
			default:
				t.Fatalf("round %d %s: outcome=%v, want established or conflict", round, a.dc, a.result.Outcome)
			}
		}
		if established > 1 {
			t.Fatalf("round %d: observed Established results=%d across three DCs, want at most 1", round, established)
		}

		// Every DC must read the same winner, even though each session's
		// default serial consistency is LOCAL_SERIAL.
		var winner string
		for dc, database := range sessions {
			stored, found, err := identityAuthorityReadEventually(ctx, database.Session(), library, dbpkg.IdentityKindFSObject, fsID)
			if err != nil || !found {
				t.Fatalf("round %d: read from %s: found=%v err=%v", round, dc, found, err)
			}
			if winner == "" {
				winner = stored.Digest
			} else if stored.Digest != winner {
				t.Fatalf("round %d: %s reads digest %s but another DC reads %s; two authoritative claims coexist", round, dc, stored.Digest, winner)
			}
		}
		matches := 0
		for _, d := range digests {
			if d == winner {
				matches++
			}
		}
		if matches != 1 {
			t.Fatalf("round %d: stored digest matches %d distinct contenders, want exactly 1", round, matches)
		}
		if established == 1 && establishedDigest != winner {
			t.Fatalf("round %d: observed Established digest %s differs from final stored winner %s", round, establishedDigest, winner)
		}

		// A later claim from a losing DC with the winner's digest is idempotent,
		// and with its own digest is still a conflict: the outcome does not
		// depend on which DC asks.
		for dc, database := range sessions {
			if digests[dc] == winner {
				continue
			}
			res, err := dbpkg.ClaimIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindFSObject, fsID, dbpkg.SupportedIdentityDigestVersion, winner)
			if err != nil || res.Outcome != dbpkg.IdentityClaimIdempotent {
				t.Fatalf("round %d: %s re-claiming the winner: outcome=%v err=%v, want idempotent", round, dc, res.Outcome, err)
			}
			res, err = dbpkg.ClaimIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindFSObject, fsID, dbpkg.SupportedIdentityDigestVersion, digests[dc])
			if err != nil || res.Outcome != dbpkg.IdentityClaimConflict {
				t.Fatalf("round %d: %s re-claiming its own digest: outcome=%v err=%v, want conflict", round, dc, res.Outcome, err)
			}
		}
	}
}

// A file and a directory with one fs_id share one fs_objects key. Concurrent
// contenders from three datacenters must therefore establish exactly one
// stored authority digest, even when that digest represents a different subtype
// from the source row a losing contender wanted to create.
func TestIdentityAuthorityConcurrentFileDirectoryClaimsShareOneKey3DC(t *testing.T) {
	endpoints := identityAuthority3DCReady(t)
	sessions := map[string]*dbpkg.DB{
		"dc-na":   w2PostHead3DCConnectSerial(t, "dc-na", endpoints, "LOCAL_SERIAL"),
		"dc-eu":   w2PostHead3DCConnectSerial(t, "dc-eu", endpoints, "LOCAL_SERIAL"),
		"dc-asia": w2PostHead3DCConnectSerial(t, "dc-asia", endpoints, "LOCAL_SERIAL"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	library := uuid.NewString()
	fsID := "f-" + uuid.NewString()
	logical := []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	candidates := map[string]string{
		"dc-na":   identityTestFileDigest(library, fsID, 10, logical, []string{strings.Repeat("a", 64)}),
		"dc-eu":   identityTestDirectoryDigest(library, fsID, "[]"),
		"dc-asia": identityTestFileDigest(library, fsID, 10, logical, []string{strings.Repeat("c", 64)}),
	}
	seen := map[string]bool{}
	for _, digest := range candidates {
		if seen[digest] {
			t.Fatalf("test contenders unexpectedly share digest %s", digest)
		}
		seen[digest] = true
	}

	type attempt struct {
		dc     string
		result dbpkg.IdentityClaimResult
		err    error
	}
	attempts := make([]attempt, 0, len(sessions))
	var mu sync.Mutex
	var wg sync.WaitGroup
	start := make(chan struct{})
	for dc, database := range sessions {
		wg.Add(1)
		go func(dc string, database *dbpkg.DB) {
			defer wg.Done()
			<-start
			result, err := dbpkg.ClaimIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindFSObject, fsID, dbpkg.SupportedIdentityDigestVersion, candidates[dc])
			mu.Lock()
			attempts = append(attempts, attempt{dc: dc, result: result, err: err})
			mu.Unlock()
		}(dc, database)
	}
	close(start)
	wg.Wait()

	established := 0
	establishedDigest := ""
	for _, a := range attempts {
		if a.err != nil {
			if a.result.Outcome != dbpkg.IdentityClaimUnknown {
				t.Fatalf("%s: err=%v with outcome=%v, want unknown", a.dc, a.err, a.result.Outcome)
			}
			continue
		}
		switch a.result.Outcome {
		case dbpkg.IdentityClaimEstablished:
			established++
			establishedDigest = candidates[a.dc]
		case dbpkg.IdentityClaimConflict:
			if a.result.Stored == nil {
				t.Fatalf("%s: conflicting contender did not observe the stored claim", a.dc)
			}
		default:
			t.Fatalf("%s: outcome=%v, want established, conflict or ambiguous unknown", a.dc, a.result.Outcome)
		}
	}
	if established > 1 {
		t.Fatalf("observed Established results=%d, want at most 1", established)
	}

	var winner string
	for dc, database := range sessions {
		stored, found, err := identityAuthorityReadEventually(ctx, database.Session(), library, dbpkg.IdentityKindFSObject, fsID)
		if err != nil || !found {
			t.Fatalf("read from %s: found=%v err=%v", dc, found, err)
		}
		if winner == "" {
			winner = stored.Digest
		} else if stored.Digest != winner {
			t.Fatalf("%s reads %s but another DC reads %s", dc, stored.Digest, winner)
		}
	}
	if !seen[winner] {
		t.Fatalf("stored digest %s belongs to no file/directory contender", winner)
	}
	if established == 1 && establishedDigest != winner {
		t.Fatalf("observed Established digest %s differs from final stored winner %s", establishedDigest, winner)
	}
}

// The claim's survival across a source-row delete must hold when the delete is
// issued from one DC and the re-create from another.
func TestIdentityAuthorityClaimSurvivesCrossDCDeleteAndRecreate3DC(t *testing.T) {
	endpoints := identityAuthority3DCReady(t)
	na := w2PostHead3DCConnectSerial(t, "dc-na", endpoints, "LOCAL_SERIAL")
	asia := w2PostHead3DCConnectSerial(t, "dc-asia", endpoints, "LOCAL_SERIAL")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	library := uuid.NewString()
	fsID := "f-" + uuid.NewString()
	logical := []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	digestA := identityTestFileDigest(library, fsID, 10, logical, []string{strings.Repeat("a", 64)})
	digestB := identityTestFileDigest(library, fsID, 10, logical, []string{strings.Repeat("b", 64)})

	if res, err := dbpkg.ClaimIdentityAuthority(ctx, na.Session(), library, dbpkg.IdentityKindFSObject, fsID, dbpkg.SupportedIdentityDigestVersion, digestA); err != nil || res.Outcome != dbpkg.IdentityClaimEstablished {
		t.Fatalf("claim from dc-na: outcome=%v err=%v", res.Outcome, err)
	}
	if err := na.Session().Query(`
		INSERT INTO fs_objects (library_id, fs_id, obj_type, size_bytes, block_ids, seafile_block_ids_sha1)
		VALUES (?, ?, ?, ?, ?, ?)
	`, library, fsID, "file", int64(10), []string{strings.Repeat("a", 64)}, logical).WithContext(ctx).Exec(); err != nil {
		t.Fatalf("insert from dc-na: %v", err)
	}
	t.Cleanup(func() {
		_ = na.Session().Query(`DELETE FROM fs_objects WHERE library_id = ? AND fs_id = ?`, library, fsID).Exec()
	})

	// Delete from a different DC, then re-create with a different dependency.
	if err := asia.Session().Query(`DELETE FROM fs_objects WHERE library_id = ? AND fs_id = ?`, library, fsID).WithContext(ctx).Exec(); err != nil {
		t.Fatalf("delete from dc-asia: %v", err)
	}
	if err := asia.Session().Query(`
		INSERT INTO fs_objects (library_id, fs_id, obj_type, size_bytes, block_ids, seafile_block_ids_sha1)
		VALUES (?, ?, ?, ?, ?, ?)
	`, library, fsID, "file", int64(10), []string{strings.Repeat("b", 64)}, logical).WithContext(ctx).Exec(); err != nil {
		t.Fatalf("re-create from dc-asia: %v", err)
	}

	for dc, database := range map[string]*dbpkg.DB{"dc-na": na, "dc-asia": asia} {
		res, err := dbpkg.ClaimIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindFSObject, fsID, dbpkg.SupportedIdentityDigestVersion, digestB)
		if err != nil || res.Outcome != dbpkg.IdentityClaimConflict {
			t.Fatalf("%s re-claims after cross-DC delete/re-create: outcome=%v err=%v, want conflict", dc, res.Outcome, err)
		}
		outcome, err := dbpkg.VerifyIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindFSObject, fsID, dbpkg.SupportedIdentityDigestVersion, digestA)
		if err != nil || outcome != dbpkg.IdentityVerificationVerified {
			t.Fatalf("%s still verifies the original claim: outcome=%v err=%v, want verified", dc, outcome, err)
		}
	}
}

func TestIdentityAuthorityGatewayCommitWinnerGlobalSerial3DC(t *testing.T) {
	endpoints := identityAuthority3DCReady(t)
	na := w2PostHead3DCConnectSerial(t, "dc-na", endpoints, "LOCAL_SERIAL")
	eu := w2PostHead3DCConnectSerial(t, "dc-eu", endpoints, "LOCAL_SERIAL")
	asia := w2PostHead3DCConnectSerial(t, "dc-asia", endpoints, "LOCAL_SERIAL")
	sessions := map[string]*dbpkg.DB{"dc-na": na, "dc-eu": eu, "dc-asia": asia}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	libraryID := uuid.NewString()
	commitID := "gateway-race-" + uuid.NewString()
	dcs := []string{"dc-na", "dc-eu", "dc-asia"}
	start := make(chan struct{})
	errs := make([]error, len(dcs))
	var wg sync.WaitGroup
	for i, dc := range dcs {
		wg.Add(1)
		go func(i int, dc string) {
			defer wg.Done()
			<-start
			projection := dbpkg.CommitProjection{
				LibraryID: libraryID, CommitID: commitID, RootFSID: "root-" + dc,
				CreatorID: uuid.NewString(), Description: "candidate-" + dc, CreatedAt: time.Now().UTC(),
			}
			authorized, err := dbpkg.AuthorizeCommitProjection(ctx, sessions[dc].Session(), projection)
			if err == nil {
				err = dbpkg.MaterializeAuthorizedCommit(sessions[dc].Session(), authorized)
			}
			errs[i] = err
		}(i, dc)
	}
	close(start)
	wg.Wait()

	winners, conflicts := 0, 0
	for i, err := range errs {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, dbpkg.IdentityAuthorityConflict):
			conflicts++
		case errors.Is(err, dbpkg.IdentityAuthorityUnavailable):
			// An ambiguous or temporarily unavailable CAS never materializes a source row.
			continue
		default:
			t.Fatalf("%s gateway outcome: %v", dcs[i], err)
		}
	}
	if winners > 1 {
		t.Fatalf("gateway outcomes winners=%d conflicts=%d errors=%v, want one winner and two conflicts", winners, conflicts, errs)
	}

	var winningClaim dbpkg.IdentityAuthorityClaim
	for _, dc := range dcs {
		claim, found, err := identityAuthorityReadEventually(ctx, sessions[dc].Session(), libraryID, dbpkg.IdentityKindCommit, commitID)
		if err != nil || !found {
			t.Fatalf("%s SERIAL claim read found=%v err=%v", dc, found, err)
		}
		if winningClaim.Digest == "" {
			winningClaim = claim
		} else if claim.Digest != winningClaim.Digest || !claim.CreatedAt.Equal(winningClaim.CreatedAt) {
			t.Fatalf("%s observed a different authority winner: %+v vs %+v", dc, claim, winningClaim)
		}
	}
	for _, dc := range dcs {
		var rootFSID, creatorID, description string
		var createdAt time.Time
		err := sessions[dc].Session().Query("SELECT root_fs_id, creator_id, description, created_at FROM commits WHERE library_id = ? AND commit_id = ?", libraryID, commitID).
			Consistency(gocql.EachQuorum).WithContext(ctx).Scan(&rootFSID, &creatorID, &description, &createdAt)
		if err != nil {
			t.Fatalf("%s source read: %v", dc, err)
		}
		digest, err := dbpkg.CommitIdentityDigest(libraryID, commitID, "", rootFSID, creatorID, description, createdAt)
		if err != nil || digest != winningClaim.Digest || !createdAt.Equal(winningClaim.CreatedAt) {
			t.Fatalf("%s source row does not match the one global claim: digest=%s err=%v created_at=%s claim=%+v", dc, digest, err, createdAt, winningClaim)
		}
	}
}

func TestIdentityAuthorityGatewayFSObjectCanonicalListWinnerGlobalSerial3DC(t *testing.T) {
	endpoints := identityAuthority3DCReady(t)
	sessions := map[string]*dbpkg.DB{
		"dc-na":   w2PostHead3DCConnectSerial(t, "dc-na", endpoints, "LOCAL_SERIAL"),
		"dc-eu":   w2PostHead3DCConnectSerial(t, "dc-eu", endpoints, "LOCAL_SERIAL"),
		"dc-asia": w2PostHead3DCConnectSerial(t, "dc-asia", endpoints, "LOCAL_SERIAL"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	libraryID, fsID := uuid.NewString(), "gateway-file-"+uuid.NewString()
	logical := []string{"sha1-logical"}
	canonical := map[string][]string{
		"dc-na":   {strings.Repeat("a", 64)},
		"dc-eu":   {strings.Repeat("b", 64)},
		"dc-asia": {strings.Repeat("c", 64)},
	}
	type attempt struct {
		dc  string
		err error
	}
	attempts := make([]attempt, 0, len(sessions))
	var mu sync.Mutex
	var wg sync.WaitGroup
	start := make(chan struct{})
	for dc, database := range sessions {
		wg.Add(1)
		go func(dc string, database *dbpkg.DB) {
			defer wg.Done()
			<-start
			authorized, err := dbpkg.AuthorizeFSObjectProjection(ctx, database.Session(), dbpkg.FSObjectProjection{
				LibraryID: libraryID, FSID: fsID, ObjectType: "file", SizeBytes: 10,
				FileLayout: dbpkg.FileStoragePairedCanonical, LogicalSHA1IDs: logical,
				CanonicalSHA256IDs: canonical[dc],
			})
			if err == nil {
				err = dbpkg.MaterializeAuthorizedFSObject(database.Session(), authorized)
			}
			mu.Lock()
			attempts = append(attempts, attempt{dc: dc, err: err})
			mu.Unlock()
		}(dc, database)
	}
	close(start)
	wg.Wait()
	winners := 0
	for _, a := range attempts {
		if a.err == nil {
			winners++
			continue
		}
		if !errors.Is(a.err, dbpkg.IdentityAuthorityConflict) && !errors.Is(a.err, dbpkg.IdentityAuthorityUnavailable) {
			t.Fatalf("%s gateway FS outcome: %v", a.dc, a.err)
		}
	}
	if winners > 1 {
		t.Fatalf("gateway paired-file winners=%d, want at most one", winners)
	}
	claim, found, err := identityAuthorityReadEventually(ctx, sessions["dc-na"].Session(), libraryID, dbpkg.IdentityKindFSObject, fsID)
	if err != nil || !found {
		t.Fatalf("read paired-file claim found=%v err=%v", found, err)
	}
	row := map[string]interface{}{}
	if err := sessions["dc-na"].Session().Query("SELECT obj_type, size_bytes, block_ids, seafile_block_ids_sha1 FROM fs_objects WHERE library_id = ? AND fs_id = ?", libraryID, fsID).Consistency(gocql.EachQuorum).WithContext(ctx).MapScan(row); err != nil {
		t.Fatalf("read paired-file source: %v", err)
	}
	objType, _ := row["obj_type"].(string)
	size, _ := row["size_bytes"].(int64)
	blocks, _ := row["block_ids"].([]string)
	logicalStored, _ := row["seafile_block_ids_sha1"].([]string)
	digest, digestErr := dbpkg.FileIdentityDigest(libraryID, fsID, size, logicalStored, blocks)
	if digestErr != nil || objType != "file" || digest != claim.Digest {
		t.Fatalf("source does not match global gateway claim: type=%s digest=%s err=%v claim=%s", objType, digest, digestErr, claim.Digest)
	}
}

func TestIdentityAuthorityGatewayFSObjectFileDirectoryWinnerGlobalSerial3DC(t *testing.T) {
	endpoints := identityAuthority3DCReady(t)
	sessions := map[string]*dbpkg.DB{
		"dc-na":   w2PostHead3DCConnectSerial(t, "dc-na", endpoints, "LOCAL_SERIAL"),
		"dc-eu":   w2PostHead3DCConnectSerial(t, "dc-eu", endpoints, "LOCAL_SERIAL"),
		"dc-asia": w2PostHead3DCConnectSerial(t, "dc-asia", endpoints, "LOCAL_SERIAL"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	libraryID, fsID := uuid.NewString(), "gateway-subtype-"+uuid.NewString()
	type contender struct {
		dc         string
		projection dbpkg.FSObjectProjection
	}
	contenders := []contender{
		{"dc-na", dbpkg.FSObjectProjection{LibraryID: libraryID, FSID: fsID, ObjectType: "dir", DirectoryEntries: "[]"}},
		{"dc-eu", dbpkg.FSObjectProjection{LibraryID: libraryID, FSID: fsID, ObjectType: "file", SizeBytes: 4, FileLayout: dbpkg.FileStoragePairedCanonical, LogicalSHA1IDs: []string{"sha1"}, CanonicalSHA256IDs: []string{strings.Repeat("d", 64)}}},
		{"dc-asia", dbpkg.FSObjectProjection{LibraryID: libraryID, FSID: fsID, ObjectType: "file", SizeBytes: 4, FileLayout: dbpkg.FileStoragePairedCanonical, LogicalSHA1IDs: []string{"sha1"}, CanonicalSHA256IDs: []string{strings.Repeat("e", 64)}}},
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	start := make(chan struct{})
	var attempts []error
	for _, candidate := range contenders {
		wg.Add(1)
		go func(c contender) {
			defer wg.Done()
			<-start
			authorized, err := dbpkg.AuthorizeFSObjectProjection(ctx, sessions[c.dc].Session(), c.projection)
			if err == nil {
				err = dbpkg.MaterializeAuthorizedFSObject(sessions[c.dc].Session(), authorized)
			}
			mu.Lock()
			attempts = append(attempts, err)
			mu.Unlock()
		}(candidate)
	}
	close(start)
	wg.Wait()
	winners := 0
	for _, err := range attempts {
		if err == nil {
			winners++
		} else if !errors.Is(err, dbpkg.IdentityAuthorityConflict) && !errors.Is(err, dbpkg.IdentityAuthorityUnavailable) {
			t.Fatalf("subtype gateway outcome: %v", err)
		}
	}
	if winners > 1 {
		t.Fatalf("gateway subtype winners=%d, want at most one", winners)
	}
	claim, found, err := identityAuthorityReadEventually(ctx, sessions["dc-na"].Session(), libraryID, dbpkg.IdentityKindFSObject, fsID)
	if err != nil || !found {
		t.Fatalf("read subtype claim found=%v err=%v", found, err)
	}
	row := map[string]interface{}{}
	if err := sessions["dc-na"].Session().Query("SELECT obj_type, size_bytes, dir_entries, block_ids, seafile_block_ids_sha1 FROM fs_objects WHERE library_id = ? AND fs_id = ?", libraryID, fsID).Consistency(gocql.EachQuorum).WithContext(ctx).MapScan(row); err != nil {
		t.Fatalf("read subtype source: %v", err)
	}
	objType, _ := row["obj_type"].(string)
	var digest string
	if objType == "dir" {
		entries, _ := row["dir_entries"].(string)
		digest, err = dbpkg.DirectoryIdentityDigest(libraryID, fsID, entries)
	} else {
		size, _ := row["size_bytes"].(int64)
		blocks, _ := row["block_ids"].([]string)
		logical, _ := row["seafile_block_ids_sha1"].([]string)
		digest, err = dbpkg.FileIdentityDigest(libraryID, fsID, size, logical, blocks)
	}
	if err != nil || digest != claim.Digest {
		t.Fatalf("subtype source does not match claim: type=%s digest=%s err=%v claim=%s", objType, digest, err, claim.Digest)
	}
}

func TestIdentityAuthorityGatewayBlindDCDeleteEmitsTombstone3DC(t *testing.T) {
	endpoints := identityAuthority3DCReady(t)
	na := w2PostHead3DCConnectSerial(t, "dc-na", endpoints, "LOCAL_SERIAL")
	eu := w2PostHead3DCConnectSerial(t, "dc-eu", endpoints, "LOCAL_SERIAL")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	libraryID, fsID := uuid.NewString(), "blind-delete-"+uuid.NewString()
	logical := []string{"sha1-blind"}
	canonical := []string{strings.Repeat("f", 64)}
	authorized, err := dbpkg.AuthorizeFSObjectProjection(ctx, na.Session(), dbpkg.FSObjectProjection{LibraryID: libraryID, FSID: fsID, ObjectType: "file", SizeBytes: 5, FileLayout: dbpkg.FileStoragePairedCanonical, LogicalSHA1IDs: logical, CanonicalSHA256IDs: canonical})
	if err != nil {
		t.Fatalf("authorize blind-delete source: %v", err)
	}
	if err := dbpkg.MaterializeAuthorizedFSObject(na.Session(), authorized); err != nil {
		t.Fatalf("materialize blind-delete source: %v", err)
	}
	// Remove only dc-eu's local replica, leaving the source live in dc-na. The
	// following DeleteFSObjectIdentity must treat its LOCAL_QUORUM miss as blind,
	// read the durable claim, and issue an EACH_QUORUM tombstone.
	if err := eu.Session().Query("DELETE FROM fs_objects WHERE library_id = ? AND fs_id = ?", libraryID, fsID).Consistency(gocql.LocalOne).WithContext(ctx).Exec(); err != nil {
		t.Fatalf("make dc-eu locally blind: %v", err)
	}
	w2PostHeadRetryEachQuorum(t, "blind gateway delete", func() error { return dbpkg.DeleteFSObjectIdentity(eu.Session(), libraryID, fsID) })
	row := map[string]interface{}{}
	if err := na.Session().Query("SELECT obj_type, size_bytes, block_ids, seafile_block_ids_sha1 FROM fs_objects WHERE library_id = ? AND fs_id = ?", libraryID, fsID).Consistency(gocql.EachQuorum).WithContext(ctx).MapScan(row); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("source survived EACH_QUORUM tombstone: err=%v row=%v", err, row)
	}
	claim, found, err := identityAuthorityReadEventually(ctx, eu.Session(), libraryID, dbpkg.IdentityKindFSObject, fsID)
	if err != nil || !found || claim.Digest == "" {
		t.Fatalf("claim after blind delete found=%v err=%v claim=%+v", found, err, claim)
	}
}

type identityAuthorityCostObserver struct {
	mu      sync.Mutex
	queries []gocql.ObservedQuery
	batches []gocql.ObservedBatch
}

func (o *identityAuthorityCostObserver) ObserveQuery(_ context.Context, observed gocql.ObservedQuery) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.queries = append(o.queries, observed)
}

func (o *identityAuthorityCostObserver) ObserveBatch(_ context.Context, observed gocql.ObservedBatch) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.batches = append(o.batches, observed)
}

func (o *identityAuthorityCostObserver) reset() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.queries = nil
	o.batches = nil
}

func (o *identityAuthorityCostObserver) snapshot() ([]gocql.ObservedQuery, []gocql.ObservedBatch) {
	o.mu.Lock()
	defer o.mu.Unlock()
	queries := append([]gocql.ObservedQuery(nil), o.queries...)
	batches := append([]gocql.ObservedBatch(nil), o.batches...)
	return queries, batches
}

func identityAuthorityCostCounts(queries []gocql.ObservedQuery, batches []gocql.ObservedBatch) (claimReads, claimLWTs, sourceReads, sourceWrites int) {
	for _, query := range queries {
		statement := strings.ToLower(query.Statement)
		switch {
		case strings.Contains(statement, "identity_authority_claims") && strings.Contains(statement, "select"):
			claimReads++
		case strings.Contains(statement, "identity_authority_claims") && strings.Contains(statement, "if not exists"):
			claimLWTs++
		case strings.Contains(statement, "from commits") || strings.Contains(statement, "from fs_objects"):
			sourceReads++
		}
	}
	for _, batch := range batches {
		for _, statement := range batch.Statements {
			lower := strings.ToLower(statement)
			if strings.Contains(lower, "into commits") || strings.Contains(lower, "into fs_objects") {
				sourceWrites++
			}
		}
	}
	return claimReads, claimLWTs, sourceReads, sourceWrites
}

// This is a measured gateway characterization, rather than a static count of
// call sites. The observer is attached only to the keyspace session and the
// test records the claim read, global SERIAL LWT, source verification read and
// LoggedBatch materialization for a new commit and an exact retry. It also
// proves that an already conflicting claim does not mint a batch. The table is
// intentionally small; route-specific wrappers add ordinary work around this
// same gateway sequence and none adds a per-block authority LWT.
func TestIdentityAuthorityGatewayClaimCostCharacterization3DC(t *testing.T) {
	endpoints := identityAuthority3DCReady(t)
	observer := &identityAuthorityCostObserver{}
	database, err := dbpkg.NewWithObservers(config.DatabaseConfig{
		Hosts:             []string{endpoints["dc-na"]},
		Keyspace:          envOrDefault("CASSANDRA_KEYSPACE", "sesamefs"),
		Consistency:       "LOCAL_QUORUM",
		SerialConsistency: "LOCAL_SERIAL",
		LocalDC:           "dc-na",
		ReplicationClass:  "NetworkTopologyStrategy",
		ReplicationDCs: map[string]int{
			"dc-na": 1, "dc-eu": 1, "dc-asia": 1,
		},
		Username: os.Getenv("CASSANDRA_USERNAME"),
		Password: os.Getenv("CASSANDRA_PASSWORD"),
	}, observer, observer)
	if err != nil {
		t.Fatalf("connect observer session: %v", err)
	}
	defer database.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	library := uuid.NewString()
	commitID := "cost-" + uuid.NewString()
	createdAt := time.UnixMilli(1_700_000_123_456).UTC()
	projection := dbpkg.CommitProjection{
		LibraryID: library, CommitID: commitID, RootFSID: "root-cost",
		CreatorID: "22222222-2222-2222-2222-222222222222", Description: "cost",
		CreatedAt: createdAt,
	}
	t.Cleanup(func() {
		_ = database.Session().Query("DELETE FROM commits WHERE library_id = ? AND commit_id = ?", library, commitID).Exec()
		_ = database.Session().Query("DELETE FROM identity_authority_claims WHERE library_id = ? AND identity_kind = ? AND identity_id = ?", library, string(dbpkg.IdentityKindCommit), commitID).Exec()
	})

	observer.reset()
	authorized, err := dbpkg.AuthorizeCommitProjection(ctx, database.Session(), projection)
	if err != nil {
		t.Fatalf("new commit authorization: %v", err)
	}
	if err := dbpkg.MaterializeAuthorizedCommit(database.Session(), authorized); err != nil {
		t.Fatalf("new commit materialization: %v", err)
	}
	queries, batches := observer.snapshot()
	claimReads, claimLWTs, sourceReads, sourceWrites := identityAuthorityCostCounts(queries, batches)
	t.Logf("new commit gateway cost: SERIAL reads=%d global SERIAL LWTs=%d ordinary source reads=%d ordinary source writes=%d batches=%d", claimReads, claimLWTs, sourceReads, sourceWrites, len(batches))
	if claimReads != 1 || claimLWTs != 1 || sourceReads != 1 || sourceWrites != 1 || len(batches) != 1 {
		t.Fatalf("new commit cost=%d claim reads, %d claim LWTs, %d source reads, %d source writes, %d batches; want 1,1,1,1,1", claimReads, claimLWTs, sourceReads, sourceWrites, len(batches))
	}

	observer.reset()
	authorized, err = dbpkg.AuthorizeCommitProjection(ctx, database.Session(), projection)
	if err != nil {
		t.Fatalf("retry authorization: %v", err)
	}
	if err := dbpkg.MaterializeAuthorizedCommit(database.Session(), authorized); err != nil {
		t.Fatalf("retry materialization: %v", err)
	}
	queries, batches = observer.snapshot()
	claimReads, claimLWTs, sourceReads, sourceWrites = identityAuthorityCostCounts(queries, batches)
	t.Logf("retry commit gateway cost: SERIAL reads=%d global SERIAL LWTs=%d ordinary source reads=%d ordinary source writes=%d batches=%d", claimReads, claimLWTs, sourceReads, sourceWrites, len(batches))
	if claimReads != 1 || claimLWTs != 0 || sourceReads != 1 || sourceWrites != 1 || len(batches) != 1 {
		t.Fatalf("retry cost=%d claim reads, %d claim LWTs, %d source reads, %d source writes, %d batches; want 1,0,1,1,1", claimReads, claimLWTs, sourceReads, sourceWrites, len(batches))
	}

	observer.reset()
	conflicting := projection
	conflicting.RootFSID = "root-conflict"
	if _, err := dbpkg.AuthorizeCommitProjection(ctx, database.Session(), conflicting); !errors.Is(err, dbpkg.IdentityAuthorityConflict) {
		t.Fatalf("conflicting commit authorization error=%v, want identity conflict", err)
	}
	queries, batches = observer.snapshot()
	claimReads, claimLWTs, sourceReads, sourceWrites = identityAuthorityCostCounts(queries, batches)
	t.Logf("conflicting commit gateway cost: SERIAL reads=%d global SERIAL LWTs=%d ordinary source reads=%d ordinary source writes=%d batches=%d", claimReads, claimLWTs, sourceReads, sourceWrites, len(batches))
	if claimReads != 1 || claimLWTs != 0 || sourceReads != 0 || sourceWrites != 0 || len(batches) != 0 {
		t.Fatalf("conflict cost=%d claim reads, %d claim LWTs, %d source reads, %d source writes, %d batches; want 1,0,0,0,0", claimReads, claimLWTs, sourceReads, sourceWrites, len(batches))
	}
	fsID := "cost-fs-" + uuid.NewString()
	fsProjection := dbpkg.FSObjectProjection{
		LibraryID: library, FSID: fsID, ObjectType: "file", SizeBytes: 5,
		FileLayout:         dbpkg.FileStoragePairedCanonical,
		LogicalSHA1IDs:     []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		CanonicalSHA256IDs: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	t.Cleanup(func() {
		_ = database.Session().Query("DELETE FROM fs_objects WHERE library_id = ? AND fs_id = ?", library, fsID).Exec()
		_ = database.Session().Query("DELETE FROM identity_authority_claims WHERE library_id = ? AND identity_kind = ? AND identity_id = ?", library, string(dbpkg.IdentityKindFSObject), fsID).Exec()
	})
	observer.reset()
	fsAuthorized, err := dbpkg.AuthorizeFSObjectProjection(ctx, database.Session(), fsProjection)
	if err != nil {
		t.Fatalf("new fs object authorization: %v", err)
	}
	if err := dbpkg.MaterializeAuthorizedFSObject(database.Session(), fsAuthorized); err != nil {
		t.Fatalf("new fs object materialization: %v", err)
	}
	queries, batches = observer.snapshot()
	claimReads, claimLWTs, sourceReads, sourceWrites = identityAuthorityCostCounts(queries, batches)
	t.Logf("new fs object gateway cost: SERIAL reads=%d global SERIAL LWTs=%d ordinary source reads=%d ordinary source writes=%d batches=%d", claimReads, claimLWTs, sourceReads, sourceWrites, len(batches))
	if claimReads != 0 || claimLWTs != 1 || sourceReads != 1 || sourceWrites != 1 || len(batches) != 1 {
		t.Fatalf("new fs object cost=%d claim reads, %d claim LWTs, %d source reads, %d source writes, %d batches; want 0,1,1,1,1", claimReads, claimLWTs, sourceReads, sourceWrites, len(batches))
	}
}
