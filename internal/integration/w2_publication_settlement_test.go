//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"net/http"
	"testing"
	"time"
)

type w2Repair struct {
	bucket         int
	commitID, fsID string
	blocks         []string
}

func w2Repairs(t *testing.T, fx *w2CreateFileFixture) []w2Repair {
	t.Helper()
	var out []w2Repair
	for b := 0; b < dbpkg.PublishedBlockReferenceRepairBuckets; b++ {
		iter := fx.database.Session().Query(`SELECT commit_id,fs_id,staged_block_ids FROM published_block_reference_repairs WHERE bucket = ? AND org_id = ? AND repo_id = ?`, b, fx.orgID, fx.repoID).Consistency(gocql.EachQuorum).Iter()
		var r w2Repair
		for iter.Scan(&r.commitID, &r.fsID, &r.blocks) {
			r.bucket = b
			out = append(out, r)
			r = w2Repair{}
		}
		if err := iter.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return out
}
func w2RepairFixture(t *testing.T) *w2CreateFileFixture {
	t.Helper()
	database := shareProjectionDBForTest(t)
	fx := newW2CreateFileFixture(t, database, newBorrowedFSHeadHandler(t, database, x1StorageClass(t)), ".docx")
	t.Cleanup(func() {
		for b := 0; b < dbpkg.PublishedBlockReferenceRepairBuckets; b++ {
			if err := database.Session().Query(`DELETE FROM published_block_reference_repairs WHERE bucket = ? AND org_id = ? AND repo_id = ?`, b, fx.orgID, fx.repoID).Consistency(gocql.LocalQuorum).Exec(); err != nil {
				t.Errorf("owned guard cleanup: %v", err)
			}
		}
	})
	t.Cleanup(v2pkg.SetCreateFileAfterMaterializedBarrierForTest(fx.repoID, func() {
		fx.target = fx.readTarget(t)
		refs, err := database.ListBlockReferrers(fx.orgID, fx.blockID)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range refs {
			if len(r) > 3 && r[:3] == "up:" {
				fx.uploadRefs = append(fx.uploadRefs, r)
			}
		}
	}))
	return fx
}
func w2ExpireTemporaryRefs(t *testing.T, fx *w2CreateFileFixture) {
	t.Helper()
	refs, err := fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range refs {
		if len(r) > 3 && (r[:3] == "up:" || r[:4] == "pub:") {
			if err := fx.database.RemoveBlockReference(fx.orgID, fx.blockID, r); err != nil {
				t.Fatal(err)
			}
		}
	}
}
func w2AssertGuardOnly(t *testing.T, fx *w2CreateFileFixture) {
	t.Helper()
	refs, err := fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
	if err != nil || len(refs) != 0 {
		t.Fatalf("must have zero actual refs, got %v err=%v", refs, err)
	}
	start := time.Now()
	live, err := fx.database.BlockPublicationLivenessGlobal(fx.orgID, fx.blockID)
	if err != nil || live != dbpkg.BlockPublicationRepairGuardOnly {
		t.Fatalf("durable repair must block destructive absence: liveness=%v err=%v", live, err)
	}
	t.Logf("guard probe with zero refs took %s", time.Since(start))
}
func w2Crash(t *testing.T, fx *w2CreateFileFixture) {
	t.Helper()
	defer func() {
		if got := recover(); got != "w2-process-death" {
			t.Fatalf("expected abrupt process-stop barrier, got %v", got)
		}
	}()
	fx.create(t)
	t.Fatal("writer did not reach crash barrier")
}

func TestW2PublicationSettlementAndRecovery(t *testing.T) {
	requireCassandra(t)
	t.Run("normalWriter", func(t *testing.T) {
		w2ObservePublicationEvidence(t)
		fx := w2RepairFixture(t)
		if rec := fx.create(t); rec.Code != http.StatusCreated {
			t.Fatalf("status=%d %s", rec.Code, rec.Body.String())
		}
		fx.assertHeadAdvanced(t)
		if !fx.hasOwnFSReferrer(t) || len(w2Repairs(t, fx)) != 0 {
			t.Fatal("normal success must promote fs: and settle repair")
		}
		fx.assertPubCount(t, 0, "normal success settles temporary pub:")
	})
	t.Run("knownLoserRetry", func(t *testing.T) {
		w2ObservePublicationEvidence(t)
		fx := w2RepairFixture(t)
		inside, competed := false, false
		var loser []w2Repair
		t.Cleanup(v2pkg.SetW2PublicationAfterAuthorityForTest(fx.repoID, func() {
			if inside || competed {
				return
			}
			inside = true
			loser = w2Repairs(t, fx)
			if len(loser) != 1 {
				t.Fatalf("expected original attempt repair, got %v", loser)
			}
			name := fx.filename
			fx.filename = "competitor.txt"
			if rec := fx.create(t); rec.Code != http.StatusCreated {
				t.Fatalf("competing empty HEAD: %d %s", rec.Code, rec.Body.String())
			}
			fx.filename = name
			inside = false
			competed = true
		}))
		if rec := fx.create(t); rec.Code != http.StatusCreated {
			t.Fatalf("retry must succeed: %d %s", rec.Code, rec.Body.String())
		}
		if !competed || len(w2Repairs(t, fx)) != 0 {
			t.Fatal("known loser and successful retry must settle all attempt repairs")
		}
		for _, r := range loser {
			present, err := fx.database.BlockReferenceExists(fx.orgID, fx.blockID, dbpkg.BlockReferrerForPublishAttempt(r.commitID))
			if err != nil || present {
				t.Fatalf("loser pub leak: %v %v", present, err)
			}
		}
		if !fx.hasOwnFSReferrer(t) {
			t.Fatal("retry must establish fs:")
		}
	})
	t.Run("ambiguousSettlementRetains", func(t *testing.T) {
		w2ObservePublicationEvidence(t)
		fx := w2RepairFixture(t)
		t.Cleanup(v2pkg.SetW2PublicationAfterAuthorityForTest(fx.repoID, func() { panic("w2-process-death") }))
		w2Crash(t, fx)
		fx.assertHeadUnchanged(t)
		rows := w2Repairs(t, fx)
		if len(rows) != 1 {
			t.Fatalf("missing repair: %v", rows)
		}
		r := rows[0]
		// Model the classifier's UNKNOWN result, not a wire-level timeout. Settlement
		// is productive and all retained authority/GC reads are real Cassandra.
		if err := v2pkg.SettlePublishedBlockReferenceRepairForIntegration(fx.database, fx.orgID, fx.repoID, r.commitID, r.fsID, r.blocks, "unknown", nil); err == nil {
			t.Fatal("UNKNOWN must retain rather than settle")
		}
		w2ExpireTemporaryRefs(t, fx)
		w2AssertGuardOnly(t, fx)
		if len(w2Repairs(t, fx)) != 1 {
			t.Fatal("UNKNOWN cleaned durable repair")
		}
	})
	for _, afterHEAD := range []bool{false, true} {
		name := "crashBeforeHEAD"
		if afterHEAD {
			name = "crashAfterHEADRecovery"
		}
		t.Run(name, func(t *testing.T) {
			w2ObservePublicationEvidence(t)
			fx := w2RepairFixture(t)
			if afterHEAD {
				t.Cleanup(v2pkg.SetW2PublicationAfterHeadForTest(fx.repoID, func() { panic("w2-process-death") }))
			} else {
				t.Cleanup(v2pkg.SetW2PublicationAfterAuthorityForTest(fx.repoID, func() { panic("w2-process-death") }))
			}
			w2Crash(t, fx)
			if afterHEAD {
				fx.assertHeadAdvanced(t)
			} else {
				fx.assertHeadUnchanged(t)
			}
			rows := w2Repairs(t, fx)
			if len(rows) != 1 {
				t.Fatalf("crash lost discovery row: %v", rows)
			}
			w2ExpireTemporaryRefs(t, fx)
			w2AssertGuardOnly(t, fx)
			for _, r := range rows {
				if err := fx.database.Session().Query(`UPDATE published_block_reference_repairs SET created_at = ?,lease_expires_at = ? WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?`, time.Now().Add(-40*24*time.Hour), time.Now().Add(-40*24*time.Hour), r.bucket, fx.orgID, fx.repoID, r.commitID, r.fsID).Consistency(gocql.LocalQuorum).Exec(); err != nil {
					t.Fatal(err)
				}
			}
			// Run the real bucket discovery/classification/promotion worker, not a mock.
			if err := v2pkg.RunPublishedBlockReferenceRepairSweepForIntegration(fx.database); err != nil {
				t.Logf("sweep retained unresolved rows: %v", err)
			}
			if !afterHEAD {
				if len(w2Repairs(t, fx)) != 1 {
					t.Fatal("pre-HEAD crash must retain unresolved guard")
				}
				// The UNKNOWN worker can renew its own TTL-bound pub:. Expire it too
				// so this assertion proves repair-gate protection independently.
				w2ExpireTemporaryRefs(t, fx)
				w2AssertGuardOnly(t, fx)
				return
			}
			if len(w2Repairs(t, fx)) != 0 || !fx.hasOwnFSReferrer(t) {
				t.Fatal("worker must discover applied attempt, promote fs: and remove repair")
			}
			data, err := newVerificationBlockStore(t, fx.orgID).GetBlockByStorageKey(context.Background(), fx.target.StorageKey)
			if err != nil || !bytes.Equal(data, fx.content) {
				t.Fatalf("recovered bytes err=%v", err)
			}
		})
	}
}

func TestW2PublicationGuardPagingAndOrganizationIsolation(t *testing.T) {
	w2ObservePublicationEvidence(t)
	requireCassandra(t)
	fx := w2RepairFixture(t)
	for start := 0; start < 270; start += 30 {
		batch := fx.database.Session().Batch(gocql.LoggedBatch).Consistency(gocql.LocalQuorum)
		for i := start; i < start+30; i++ {
			ids := []string{fmt.Sprintf("unrelated-%03d", i)}
			if i == 269 {
				ids = []string{fx.blockID}
			}
			batch.Query(`INSERT INTO published_block_reference_repairs (bucket,org_id,repo_id,commit_id,fs_id,staged_block_ids,created_at) VALUES (?,?,?,?,?,?,?)`, 0, fx.orgID, fx.repoID, "paging", fmt.Sprintf("fs-%03d", i), ids, time.Now().UTC())
		}
		if err := batch.Exec(); err != nil {
			t.Fatal(err)
		}
	}
	w2AssertGuardOnly(t, fx)
	other := w2RepairFixture(t)
	start := time.Now()
	live, err := other.database.BlockPublicationLivenessGlobal(other.orgID, fx.blockID)
	if err != nil || live != dbpkg.BlockPublicationZero {
		t.Fatalf("other organization cannot supply authority: %v %v", live, err)
	}
	t.Logf("32 empty organization ranges + final zero probe took %s", time.Since(start))
}
