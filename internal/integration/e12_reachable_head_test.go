//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// A real Office CreateFile wins HEAD and stops before permanent promotion.
// Cassandra expires the writer's actual temporary references; the only time
// control is shortening their TTL through the production reference API.
// Neither repair authority nor HEAD/tree/P/D rows are manufactured by CQL.
func TestE12ReachableHEADLateRepair(t *testing.T) {
	requireCassandra(t)
	for _, expire := range []bool{false, true} {
		name := "noGCControl"
		if expire {
			name = "temporaryTTLExpiresGCIsGated"
		}
		t.Run(name, func(t *testing.T) {
			fx := w2RepairFixture(t)
			restore := v2pkg.SetW2PublicationAfterHeadForTest(fx.repoID, func() { panic("w2-process-death") })
			defer restore()
			w2Crash(t, fx)
			restore()
			fx.assertHeadAdvanced(t)
			head := borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID)
			rows := w2Repairs(t, fx)
			if len(rows) != 1 || rows[0].commitID != head || len(rows[0].blocks) != 1 || rows[0].blocks[0] != fx.blockID {
				t.Fatalf("applied HEAD must own the real repair/block: head=%s rows=%+v", head, rows)
			}
			r := rows[0]
			w24AssertHeadReaches(t, fx, head, r.fsID)
			refs, err := fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
			if err != nil {
				t.Fatal(err)
			}
			up, pub := false, false
			for _, ref := range refs {
				up = up || strings.HasPrefix(ref, "up:")
				pub = pub || strings.HasPrefix(ref, "pub:")
				if !strings.HasPrefix(ref, "up:") && !strings.HasPrefix(ref, "pub:") {
					t.Fatalf("before repair only temporary liveness is allowed: %v", refs)
				}
			}
			if !up || !pub {
				t.Fatalf("real writer did not leave both up: and pub: references: %v", refs)
			}
			if expire {
				e12ExpireRealTemporaryTTL(t, fx, refs)
				w2AssertGuardOnly(t, fx)
				// This executes the actual candidate, settled claim and global
				// pre-D proof. The queue wrapper narrows discovery only.
				w2AssertGCBlocked(t, fx)
				e12AssertNoDeleteLifecycle(t, fx)
				if remaining := w2Repairs(t, fx); len(remaining) != 1 || remaining[0].commitID != r.commitID || remaining[0].fsID != r.fsID {
					t.Fatalf("GC changed the durable repair: %+v", remaining)
				}
				w24AssertHeadReaches(t, fx, head, r.fsID)
			}
			// A fresh session runs the real cold classifier and visitor. No
			// injected classification, aged repair row or synthetic promotion.
			recovery := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], nil)
			classification, classifyErr := v2pkg.ClassifyPublishedBlockReferenceRepairResumableForIntegration(recovery, fx.orgID, fx.repoID, r.commitID, r.fsID)
			if classifyErr != nil || classification != "reachable" {
				t.Fatalf("real HEAD/tree classification=%q err=%v; want reachable", classification, classifyErr)
			}
			if err := v2pkg.RepairPublishedFSObjectBlockReferenceRepair(recovery, fx.orgID, fx.repoID, r.commitID, r.fsID, r.blocks); err != nil {
				t.Fatalf("reachable repair: %v", err)
			}
			// A second visit models retry after settlement; it must be a no-op.
			if err := v2pkg.RepairPublishedFSObjectBlockReferenceRepair(recovery, fx.orgID, fx.repoID, r.commitID, r.fsID, r.blocks); err != nil {
				t.Fatalf("settled repair retry: %v", err)
			}
			refs, err = fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
			if err != nil {
				t.Fatal(err)
			}
			expected := dbpkg.BlockReferrerForFSObject(fx.repoID, r.fsID)
			found := false
			for _, ref := range refs {
				found = found || ref == expected
				if ref != expected && !strings.HasPrefix(ref, "up:") {
					t.Fatalf("settlement left unexpected liveness: %v", refs)
				}
			}
			if !found || (expire && len(refs) != 1) || len(w2Repairs(t, fx)) != 0 {
				t.Fatalf("settlement must leave exact fs: and no repair: refs=%v repairs=%+v", refs, w2Repairs(t, fx))
			}
			if current := borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID); current != head {
				t.Fatalf("repair changed HEAD: %s -> %s", head, current)
			}
			w24AssertHeadReaches(t, fx, head, r.fsID)
			if target := fx.readTarget(t); target != fx.target {
				t.Fatalf("repair changed physical life P1 to P2: %+v -> %+v", fx.target, target)
			}
			w2AssertBytes(t, fx)
			e12AssertNoDeleteLifecycle(t, fx)
			t.Logf("E1-2: HEAD=%s fs=%s P1=(%s,%s) realTTLExpired=%t classifier=reachable repair=settled refs=%v; no D1 COMMITTED/TERMINAL", head, r.fsID, fx.target.StorageClass, fx.target.StorageKey, expire, refs)
		})
	}
}

func e12ExpireRealTemporaryTTL(t *testing.T, fx *w2CreateFileFixture, refs []string) {
	t.Helper()
	for _, ref := range refs {
		var ttl int
		if err := fx.database.Session().Query(`SELECT TTL(created_at) FROM block_references WHERE org_id = ? AND block_id = ? AND referrer = ?`, fx.orgID, fx.blockID, ref).Consistency(gocql.EachQuorum).Scan(&ttl); err != nil || ttl <= 0 {
			t.Fatalf("writer reference %s is not genuinely TTL-bound: ttl=%d err=%v", ref, ttl, err)
		}
		if err := fx.database.AddBlockReference(fx.orgID, fx.blockID, ref, fx.repoID, 2); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		remaining, err := fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
		if err != nil {
			t.Fatal(err)
		}
		live, err := fx.database.BlockHasReferencesGlobal(fx.orgID, fx.blockID)
		if err != nil {
			t.Fatal(err)
		}
		if len(remaining) == 0 && !live {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("actual Cassandra TTL did not remove all temporary liveness")
}

func e12AssertNoDeleteLifecycle(t *testing.T, fx *w2CreateFileFixture) {
	t.Helper()
	store := gcpkg.NewCassandraStore(fx.database)
	x1AssertCanonicalPresent(t, store, fx.orgUUID, fx.blockID, fx.target.StorageKey)
	var state, claim *string
	var handoff *bool
	if err := fx.database.Session().Query(`SELECT gc_state, gc_claim_id, gc_orphan_handoff FROM blocks WHERE org_id = ? AND block_id = ?`, fx.orgID, fx.blockID).Consistency(gocql.Serial).Scan(&state, &claim, &handoff); err != nil {
		t.Fatal(err)
	}
	if (state != nil && *state != "") || (claim != nil && *claim != "") || (handoff != nil && *handoff) {
		t.Fatalf("guard left an active claim/handoff: state=%v claim=%v handoff=%v", state, claim, handoff)
	}
	var phase string
	err := fx.database.Session().Query(`SELECT phase FROM gc_block_delete_lifecycles WHERE org_id = ? AND block_id = ?`, fx.orgID, fx.blockID).Consistency(gocql.EachQuorum).Scan(&phase)
	if err != gocql.ErrNotFound {
		t.Fatalf("unexpected delete lifecycle phase=%s err=%v", phase, err)
	}
	for bucket := 0; bucket < dbpkg.GCDiscoveryBucketCount; bucket++ {
		var pageState []byte
		for {
			page, err := store.ListS3OrphanRecoveryRoots(bucket, pageState, 1000)
			if err != nil {
				t.Fatal(err)
			}
			for _, root := range page.Roots {
				if root.OrgID == fx.orgUUID && root.BlockID == fx.blockID {
					t.Fatalf("guard published destructive recovery root: %+v", root)
				}
			}
			pageState = page.PageState
			if len(pageState) == 0 {
				break
			}
		}
	}
}
