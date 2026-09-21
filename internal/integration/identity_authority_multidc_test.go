//go:build integration

package integration

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
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
			stored, found, err := dbpkg.ReadIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindFSObject, fsID)
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
		stored, found, err := dbpkg.ReadIdentityAuthority(ctx, database.Session(), library, dbpkg.IdentityKindFSObject, fsID)
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
