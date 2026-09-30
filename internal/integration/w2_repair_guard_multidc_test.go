//go:build integration

package integration

import (
	"os"
	"testing"

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

const w2Repair3DCEvidenceEnv = "SESAMEFS_REQUIRE_W2_REPAIR_3DC_EVIDENCE"

var w2Repair3DCEvidence bool

// The runner stops NA/Asia with hints disabled for seed, restarts them blind,
// then stops EU for the unavailable leg. A local miss is mandatory evidence:
// ordinary replication to every live replica cannot accidentally pass this.
func TestW2RepairGuard3DC(t *testing.T) {
	phase := os.Getenv("W2_REPAIR_3DC_PHASE")
	if phase == "" {
		if os.Getenv(w2Repair3DCEvidenceEnv) == "1" {
			t.Fatal("required 3-DC phase missing")
		}
		t.Skip("isolated 3-DC runner only")
	}
	t.Cleanup(func() {
		if !t.Failed() && !t.Skipped() {
			w2Repair3DCEvidence = true
		}
	})
	org, repo, commit, fs, block := os.Getenv("W2_REPAIR_3DC_ORG"), "11111111-1111-4111-8111-111111111111", "w2-guard-commit", "w2-guard-fs", "w2-guard-block"
	if org == "" {
		t.Fatal("unique organization fixture required")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	dc := "dc-na"
	if phase == "seed" || phase == "promote" || phase == "cleanup" {
		dc = "dc-eu"
	}
	database := w2PostHead3DCConnect(t, dc, endpoints)
	switch phase {
	case "seed":
		if err := v2pkg.QueuePublishedFSObjectBlockReferenceRepair(database, org, repo, commit, fs, []string{block}); err != nil {
			t.Fatal(err)
		}
		found := false
		for bucket := 0; bucket < dbpkg.PublishedBlockReferenceRepairBuckets; bucket++ {
			iter := database.Session().Query(`SELECT staged_block_ids FROM published_block_reference_repairs WHERE bucket = ? AND org_id = ?`, bucket, org).Consistency(gocql.LocalQuorum).Iter()
			var ids []string
			for iter.Scan(&ids) {
				for _, id := range ids {
					found = found || id == block
				}
			}
			if err := iter.Close(); err != nil {
				t.Fatal(err)
			}
		}
		if !found {
			t.Fatal("EU did not durably acknowledge its own repair")
		}
		t.Log("repair acknowledged at LOCAL_QUORUM while NA/Asia are down, with hints disabled")
	case "readGuard":
		for bucket := 0; bucket < dbpkg.PublishedBlockReferenceRepairBuckets; bucket++ {
			iter := database.Session().Query(`SELECT staged_block_ids FROM published_block_reference_repairs WHERE bucket = ? AND org_id = ?`, bucket, org).Consistency(gocql.LocalQuorum).Iter()
			var ids []string
			for iter.Scan(&ids) {
				for _, id := range ids {
					if id == block {
						t.Fatal("NA was not blind: repair already replicated; drill is invalid")
					}
				}
			}
			if err := iter.Close(); err != nil {
				t.Fatal(err)
			}
		}
		live, err := database.BlockPublicationLivenessGlobal(org, block)
		if err != nil || live != dbpkg.BlockPublicationRepairGuardOnly {
			t.Fatalf("blind NA failed to see EU guard: live=%v err=%v", live, err)
		}
		t.Log("blind LOCAL_QUORUM misses; productive EACH_QUORUM finds remote durable repair")
	case "promote":
		if err := database.AddBlockReference(org, block, "fs:"+repo+":"+fs, repo, 0); err != nil {
			t.Fatal(err)
		}
		if err := v2pkg.ClearPublishedFSObjectBlockReferenceRepair(database, org, repo, commit, fs); err != nil {
			t.Fatal(err)
		}
		t.Log("productive permanent fs reference acknowledged before repair removal")
	case "readPermanent":
		live, err := database.BlockPublicationLivenessGlobal(org, block)
		if err != nil || live != dbpkg.BlockPublicationRealReference {
			t.Fatalf("repair-to-fs handoff lost continuity: live=%v err=%v", live, err)
		}
	case "unavailable":
		live, err := database.BlockPublicationLivenessGlobal(org, block)
		if err == nil || live != dbpkg.BlockPublicationUnknown {
			t.Fatalf("unavailable EU must not become destructive zero: live=%v err=%v", live, err)
		}
		t.Logf("unavailable DC fails closed: %T %v", err, err)
	case "cleanup":
		if err := database.RemoveBlockReference(org, block, "fs:"+repo+":"+fs); err != nil {
			t.Fatal(err)
		}
		if err := v2pkg.ClearPublishedFSObjectBlockReferenceRepair(database, org, repo, commit, fs); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown 3-DC phase %q", phase)
	}
}
