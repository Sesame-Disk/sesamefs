//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
)

const w2RolloutEvidenceEnv = "SESAMEFS_REQUIRE_W2_ROLLOUT_EVIDENCE"

var w2RolloutObserved = map[string]bool{}
var w2RolloutRequired = []string{"TestW2MixedRollout/newWriterOldGC", "TestW2MixedRollout/oldWriterNewGC"}

func w2RolloutMissing() []string {
	var missing []string
	for _, name := range w2RolloutRequired {
		if !w2RolloutObserved[name] {
			missing = append(missing, name)
		}
	}
	return missing
}
func w2LegacyCommand(t *testing.T, d w2LegacyFixture) *exec.Cmd {
	t.Helper()
	binary := os.Getenv("SESAMEFS_W2_LEGACY_BINARY")
	if binary == "" {
		t.Fatal("legacy binary must be built from pinned pre-#239 source")
	}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestW2LegacyRolloutChild$", "-test.v")
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "SESAMEFS_W2_LEGACY_CHILD=") && !strings.HasPrefix(env, "SESAMEFS_W2_LEGACY_FIXTURE=") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "SESAMEFS_W2_LEGACY_CHILD=1", "SESAMEFS_W2_LEGACY_FIXTURE="+string(data))
	return cmd
}
func TestW2MixedRollout(t *testing.T) {
	if os.Getenv("SESAMEFS_W2_LEGACY_BINARY") == "" {
		if os.Getenv(w2RolloutEvidenceEnv) == "1" {
			t.Fatal("required pinned legacy binary missing")
		}
		t.Skip("run scripts/w2-closure-rollout-validation.sh in Docker")
	}
	requireCassandra(t)
	t.Run("newWriterOldGC", func(t *testing.T) {
		t.Cleanup(func() {
			if !t.Failed() && !t.Skipped() {
				w2RolloutObserved[t.Name()] = true
			}
		})
		fx, data := w2ClosureFixture(t, "Office")
		store := gcpkg.NewCassandraStore(fx.database)
		t.Cleanup(v2pkg.SetW2PublicationAfterAuthorityForTest(fx.repoID, func() {
			fx.target = fx.readTarget(t)
			w2ExpireTemporaryRefs(t, fx)
			w2AssertGuardOnly(t, fx)
			w2Candidate(t, store, fx.orgUUID, fx.blockID, fx.target.StorageClass)
			cmd := w2LegacyCommand(t, w2LegacyFixture{Org: fx.orgID, Repo: fx.repoID, Block: fx.blockID, Class: data.Class, Mode: "gc"})
			out, err := cmd.CombinedOutput()
			if err != nil || !bytes.Contains(out, []byte("LEGACY GC COMMITTED AND RETIRED EXACT P")) {
				t.Fatalf("pinned old GC: %v\n%s", err, out)
			}
			t.Logf("%s", out)
			w2AssertCommittedContinuation(t, store, fx.orgUUID, fx.blockID, fx.target.StorageClass, fx.target.StorageKey, newVerificationBlockStore(t, fx.orgID))
		}))
		rec := w2InvokeWriter(t, fx, data, fx.database)
		if rec.Code < 200 || rec.Code >= 300 {
			t.Fatalf("new writer did not publish after legacy D: %d %s", rec.Code, rec.Body.String())
		}
		fx.assertHeadAdvanced(t)
		t.Log("UNSUPPORTED MIXED ROLLOUT: new writer published HEAD after old GC committed D(P); GC must stay disabled until every destructive reader is upgraded")
	})
	t.Run("oldWriterNewGC", func(t *testing.T) {
		t.Cleanup(func() {
			if !t.Failed() && !t.Skipped() {
				w2RolloutObserved[t.Name()] = true
			}
		})
		fx, data := w2ClosureFixture(t, "SyncDirect")
		proxy := newW2WireProxy(t)
		// This is a real old INSERT paused after old Sync readiness and before
		// repair acquisition. The new reader must legitimately see zero here.
		proxy.armHold("insert into published_block_reference_repairs")
		cmd := w2LegacyCommand(t, w2LegacyFixture{Org: fx.orgID, Repo: fx.repoID, Block: fx.blockID, Class: data.Class, User: fx.userID, TargetCommit: data.TargetCommit, Mode: "sync", Proxy: proxy.listener.Addr().String()})
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		finished := false
		t.Cleanup(func() {
			proxy.resume()
			if !finished {
				cmd.Process.Kill()
				<-done
			}
		})
		select {
		case <-proxy.reached:
		case err := <-done:
			finished = true
			t.Fatalf("old writer exited before real repair INSERT: %v\n%s", err, out.String())
		case <-time.After(45 * time.Second):
			t.Fatal("old writer missed native repair barrier")
		}
		if len(w2Repairs(t, fx)) != 0 {
			t.Fatal("old writer acquired repair before paused INSERT")
		}
		w2ExpireTemporaryRefs(t, fx)
		store := gcpkg.NewCassandraStore(fx.database)
		candidate := w2Candidate(t, store, fx.orgUUID, fx.blockID, fx.target.StorageClass)
		scope := &w2ClosureOwnedQueue{GCStore: store, org: fx.orgUUID, block: fx.blockID, identity: candidate.Identity()}
		fx.assertHeadUnchanged(t)
		if n, err := w2Worker(t, scope, fx.target.StorageClass).ProcessOrgOnce(t.Context(), fx.orgUUID); err != nil || n != 1 {
			t.Fatalf("current productive GC did not retire the owned candidate: n=%d err=%v", n, err)
		}
		if !scope.visited {
			t.Fatal("current productive GC did not execute its pre-D publication liveness probe")
		}
		w2AssertCommittedContinuation(t, store, fx.orgUUID, fx.blockID, fx.target.StorageClass, fx.target.StorageKey, newVerificationBlockStore(t, fx.orgID))
		fx.assertHeadUnchanged(t)
		t.Log("CURRENT PRODUCTIVE GC COMMITTED AND RETIRED EXACT P BEFORE LEGACY HEAD")
		proxy.resume()
		select {
		case err := <-done:
			finished = true
			if err != nil {
				t.Fatalf("old writer did not reach its productive HEAD: %v\n%s", err, out.String())
			}
		case <-time.After(45 * time.Second):
			t.Fatal("old writer did not complete after repair resumed")
		}
		if !bytes.Contains(out.Bytes(), []byte("LEGACY WRITER PUBLISHED TARGET HEAD")) {
			t.Fatalf("missing old writer proof: %s", out.String())
		}
		fx.assertHeadAdvanced(t)
		w2AssertCommittedContinuation(t, store, fx.orgUUID, fx.blockID, fx.target.StorageClass, fx.target.StorageKey, newVerificationBlockStore(t, fx.orgID))
		t.Log("UNSUPPORTED MIXED ROLLOUT: old Sync published HEAD after new GC committed D(P) before late repair; GC stays disabled until all adopting writers are upgraded")
	})
}
