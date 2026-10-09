//go:build integration

package integration

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
)

const e115dEvidenceEnv = "SESAMEFS_REQUIRE_E115D_ABANDONED_COMMIT_EVIDENCE"

var e115dLegs = []string{"sync-promotes-abandoned-commit", "sync-promote-after-terminal"}
var e115dObserved = map[string]bool{}

func e115dMissing(seen map[string]bool) []string {
	var out []string
	for _, leg := range e115dLegs {
		if !seen[leg] {
			out = append(out, leg)
		}
	}
	return out
}

// E1-15B crash-after-queue without GC: the real writer subprocess is resumed
// at once and SIGKILLed after repair queueing and insertCommit.
func e115dCrashAfterQueueNoGC(t *testing.T, f *e114Fixture) {
	t.Helper()
	fx := f.fx
	data := w2ChildFixture{Org: fx.orgID, Repo: fx.repoID, User: fx.userID, Head: fx.headBefore, Filename: fx.filename, Phase: "crash-after-queue", Marker: filepath.Join(t.TempDir(), "writer"), Class: x1StorageClass(t), Funnel: "Office", Content: fx.content, Block: fx.blockID, SHA1: fx.sha1ID}
	cmd := e115bCommand(t, data)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	finished := false
	t.Cleanup(func() {
		if !finished {
			_ = cmd.Process.Kill()
			<-done
		}
	})
	e112AwaitFile(t, data.Marker+".materialized")
	fx.target = fx.readTarget(t)
	if err := os.WriteFile(data.Marker+".resume", nil, 0600); err != nil {
		t.Fatal(err)
	}
	e112AwaitFile(t, data.Marker+".beforefence")
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	waitErr := <-done
	finished = true
	var exit *exec.ExitError
	if !errors.As(waitErr, &exit) || !w2WasSIGKILL(exit) {
		t.Fatalf("writer was not SIGKILL: %v\n%s", waitErr, output.String())
	}
	fx.assertHeadUnchanged(t)
}

// The abandoned attempt: exactly one durable repair whose commit exists.
func e115dAbandoned(t *testing.T, fx *w2CreateFileFixture) w2Repair {
	t.Helper()
	rows := w2Repairs(t, fx)
	if len(rows) != 1 || len(rows[0].blocks) != 1 || rows[0].blocks[0] != fx.blockID {
		t.Fatalf("abandoned attempt repair missing: %+v", rows)
	}
	e112CommitRoot(t, fx.database, fx.repoID, rows[0].commitID)
	return rows[0]
}

// A real blockless competitor publication moves HEAD off c1's parent.
func e115dAdvanceHead(t *testing.T, f *e114Fixture) string {
	t.Helper()
	fx := f.fx
	name := fx.filename
	fx.filename = "competitor.txt"
	rec := fx.create(t)
	fx.filename = name
	if rec.Code != http.StatusCreated {
		t.Fatalf("competitor: %d %s", rec.Code, rec.Body.String())
	}
	head := borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID)
	if head == fx.headBefore {
		t.Fatal("competitor did not advance HEAD")
	}
	return head
}

// Real Sync UpdateBranch?head=<abandoned commit>, through the existing W2
// closure invocation of the production SyncHandler.
func e115dSyncPromote(t *testing.T, fx *w2CreateFileFixture, commit string) (int, string) {
	t.Helper()
	data := w2ChildFixture{Org: fx.orgID, Repo: fx.repoID, User: fx.userID, Class: x1StorageClass(t), Funnel: "SyncDirect", TargetCommit: commit}
	rec := w2InvokeWriter(t, fx, data, fx.database)
	return rec.Code, rec.Body.String()
}

func e115dParent(t *testing.T, fx *w2CreateFileFixture, commit string) string {
	t.Helper()
	var parent *string
	if err := fx.database.Session().Query(`SELECT parent_id FROM commits WHERE library_id=? AND commit_id=?`, fx.repoID, commit).Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if parent == nil {
		return ""
	}
	return *parent
}

func TestAbandonedCommitPublishability(t *testing.T) {
	if endpoint := os.Getenv("SESAMEFS_E115D_ISOLATED_URL"); endpoint != "" && os.Getenv("SESAMEFS_E115D_CHILD") != "1" {
		e115dRunIsolated(t, endpoint)
		return
	}
	requireCassandra(t)
	if runtime.GOOS != "linux" || os.Getenv("SESAMEFS_TEST_IN_CONTAINER") != "1" {
		t.Fatal("E1-15D requires Linux Docker SIGKILL")
	}
	for _, endpoint := range []string{superadminClient.baseURL, envOrDefault("SESAMEFS_URL_2", "http://sesamefs-node-2:8080"), envOrDefault("SESAMEFS_URL_3", "http://sesamefs-node-3:8080")} {
		if err := e19CheckGCDisabled(newTestClient(endpoint, superadminClient.token)); err != nil {
			t.Fatalf("E1-15D isolation: %v", err)
		}
	}
	for _, leg := range e115dLegs {
		t.Run(leg, func(t *testing.T) {
			t.Cleanup(func() {
				if !t.Failed() && !t.Skipped() {
					e115dObserved[leg] = true
				}
			})
			f := e115bFixture(t)
			fx := f.fx
			defer e115bFinalizeCommitted(t, fx)
			loser := fx.filename
			switch leg {
			case "sync-promotes-abandoned-commit":
				e115dCrashAfterQueueNoGC(t, f)
				r := e115dAbandoned(t, fx)
				if parent := e115dParent(t, fx, r.commitID); parent != fx.headBefore {
					t.Fatalf("abandoned commit parent %s != original HEAD %s", parent, fx.headBefore)
				}
				advanced := e115dAdvanceHead(t, f)
				code, body := e115dSyncPromote(t, fx, r.commitID)
				head := borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID)
				entries := e115cHeadEntries(t, fx, head)
				t.Logf("E1-15D Sync UpdateBranch?head=%s with HEAD advanced to %s: status=%d body=%s newHEAD=%s parent=%s entries=%v", r.commitID, advanced, code, strings.TrimSpace(body), head, e115dParent(t, fx, head), entries)
				if code != http.StatusOK || head == advanced || entries[loser] != r.fsID {
					t.Fatalf("expected Sync to publish the abandoned commit's content: status=%d head=%s entries=%v", code, head, entries)
				}
				if e115dParent(t, fx, head) != advanced {
					t.Fatalf("merge commit parent must be the advanced HEAD")
				}
				if fx.readTarget(t) != fx.target {
					t.Fatal("exact P1 changed")
				}
				w2AssertBytes(t, fx)
				published := false
				for _, ref := range f.refsExact(t) {
					published = published || ref == dbpkg.BlockReferrerForFSObject(fx.repoID, r.fsID)
				}
				if !published {
					t.Fatalf("Sync published c1's file without its fs: reference")
				}
				t.Logf("E1-15D RESULT: abandoned v2 commit %s (repair UNKNOWN, writer dead) was published by Sync auto-merge into HEAD %s (parent %s, not %s); its file fs=%s is now HEAD-reachable on exact P1. The repair was protecting a still-publishable commit", r.commitID, head, advanced, r.commitID, r.fsID)
			case "sync-promote-after-terminal":
				e115cCrashAfterD(t, f, "crash-after-queue")
				p1 := fx.target
				r := e115dAbandoned(t, fx)
				e115bFinalizeCommitted(t, fx)
				if exists, err := gcpkg.NewCassandraStore(fx.database).BlockExists(fx.orgUUID, fx.blockID); err != nil || exists {
					t.Fatalf("P1 not retired before promotion: %t %v", exists, err)
				}
				advanced := e115dAdvanceHead(t, f)
				refsBefore := f.refsExact(t)
				// Capture (and still emit) the handler's log for this one call, so the
				// rejection is attributed to the exact cause, not any non-200.
				var handlerLog bytes.Buffer
				log.SetOutput(io.MultiWriter(os.Stderr, &handlerLog))
				code, body := e115dSyncPromote(t, fx, r.commitID)
				log.SetOutput(os.Stderr)
				if !strings.Contains(handlerLog.String(), "publication readiness check failed for auto-merged commit") || !strings.Contains(handlerLog.String(), fx.blockID+" is not currently reusable") {
					t.Fatalf("rejection not attributed to the publication readiness/fence gate for block %s:\n%s", fx.blockID, handlerLog.String())
				}
				head := borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID)
				entries := e115cHeadEntries(t, fx, head)
				t.Logf("E1-15D Sync UpdateBranch?head=%s after TERMINAL: status=%d body=%s HEAD=%s entries=%v", r.commitID, code, strings.TrimSpace(body), head, entries)
				if code == http.StatusOK || head != advanced {
					t.Fatalf("Sync must not publish content depending on retired P1: status=%d head=%s advanced=%s", code, head, advanced)
				}
				if _, ok := entries[loser]; ok {
					t.Fatal("abandoned file reachable from HEAD after TERMINAL")
				}
				if exists, err := gcpkg.NewCassandraStore(fx.database).BlockExists(fx.orgUUID, fx.blockID); err != nil || exists {
					t.Fatalf("P1 reinstalled by Sync promotion: %t %v", exists, err)
				}
				if exists, err := newVerificationBlockStore(t, fx.orgID).ObjectExists(t.Context(), p1.StorageKey); err != nil || exists {
					t.Fatalf("K1 reappeared: %t %v", exists, err)
				}
				for _, ref := range f.refsExact(t) {
					if ref == dbpkg.BlockReferrerForFSObject(fx.repoID, r.fsID) {
						t.Fatalf("fs: for abandoned file after rejected promotion; refs before=%v", refsBefore)
					}
				}
				t.Logf("E1-15D RESULT: after TERMINAL of P1, Sync promotion of abandoned commit %s was rejected by the Sync publication readiness gate (block not reusable; status=%d); HEAD stays %s; P1 not reinstalled; K1 absent; no fs:", r.commitID, code, advanced)
			}
		})
	}
}

func e115dRunIsolated(t *testing.T, endpoint string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	run := "^TestAbandonedCommitPublishability$"
	if _, sub, ok := strings.Cut(flag.Lookup("test.run").Value.String(), "/"); ok {
		run += "/" + sub
	}
	cmd := exec.CommandContext(ctx, binary, "-test.run="+run, "-test.v", "-test.count=1", "-test.timeout=5m")
	for _, entry := range os.Environ() {
		name := strings.SplitN(entry, "=", 2)[0]
		if strings.HasPrefix(name, "SESAMEFS_REQUIRE_") || strings.HasSuffix(name, "_CHILD") || name == "SESAMEFS_URL" || name == "SESAMEFS_URL_2" || name == "SESAMEFS_URL_3" || name == "CASSANDRA_KEYSPACE" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, e115dEvidenceEnv+"=1", "SESAMEFS_E115D_CHILD=1", "CASSANDRA_KEYSPACE=sesamefs_e19", "SESAMEFS_URL="+endpoint, "SESAMEFS_URL_2="+endpoint, "SESAMEFS_URL_3="+endpoint)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("isolated E1-15D: %v", err)
	}
	for _, leg := range e115dLegs {
		e115dObserved[leg] = true
	}
}

func TestAbandonedCommitPublishabilityCompleteness(t *testing.T) {
	if len(e115dMissing(nil)) != 2 {
		t.Fatal("two required legs")
	}
	all := map[string]bool{}
	for _, leg := range e115dLegs {
		all[leg] = true
	}
	if len(e115dMissing(all)) != 0 {
		t.Fatal("complete rejected")
	}
	for _, leg := range e115dLegs {
		delete(all, leg)
		if missing := e115dMissing(all); len(missing) != 1 || missing[0] != leg {
			t.Fatalf("omission %s: %v", leg, missing)
		}
		all[leg] = true
	}
}
