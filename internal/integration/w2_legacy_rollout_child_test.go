//go:build integration

package integration

// This helper is copied verbatim into pinned pre-#239 source by the rollout
// runner. It calls that source's productive writer/GC implementation. Only
// TestMain isolation and a real-session adapter are added to the old test build.
import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	apipkg "github.com/Sesame-Disk/sesamefs/internal/api"
	"github.com/Sesame-Disk/sesamefs/internal/config"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type w2LegacyFixture struct{ Org, Repo, Block, Class, Mode, TargetCommit, User, Proxy string }

func TestW2LegacyRolloutChild(t *testing.T) {
	if os.Getenv("SESAMEFS_W2_LEGACY_CHILD") != "1" {
		t.Skip("pinned legacy subprocess helper")
	}
	var d w2LegacyFixture
	if err := json.Unmarshal([]byte(os.Getenv("SESAMEFS_W2_LEGACY_FIXTURE")), &d); err != nil {
		t.Fatal(err)
	}
	database := shareProjectionDBForTest(t)
	if d.Proxy != "" {
		cluster := gocql.NewCluster(d.Proxy)
		cluster.Keyspace = envOrDefault("CASSANDRA_KEYSPACE", "sesamefs")
		cluster.DisableInitialHostLookup = true
		cluster.NumConns = 1
		cluster.ProtoVersion = 4
		cluster.Consistency = gocql.LocalQuorum
		cluster.SerialConsistency = gocql.Serial
		cluster.Timeout = 60 * time.Second
		if u, p := os.Getenv("CASSANDRA_USERNAME"), os.Getenv("CASSANDRA_PASSWORD"); u != "" && p != "" {
			cluster.Authenticator = gocql.PasswordAuthenticator{Username: u, Password: p}
		}
		session, err := cluster.CreateSession()
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		database = dbpkg.NewPublicationEvidenceDB(session, config.DatabaseConfig{Hosts: []string{d.Proxy}, Keyspace: cluster.Keyspace, Consistency: "LOCAL_QUORUM", SerialConsistency: "SERIAL"})
	}
	manager := storage.NewManager()
	s3 := newVerificationS3Store(t)
	manager.SetDefaultClass(d.Class)
	manager.RegisterBackend(d.Class, s3, "")
	if d.Mode == "gc" {
		store := gcpkg.NewCassandraStore(database)
		worker := gcpkg.NewWorker(store, gcpkg.NewStorageManagerAdapter(manager), gcpkg.NewQueue(store), 100, 0, false, &gcpkg.Stats{})
		org, err := uuid.Parse(d.Org)
		if err != nil {
			t.Fatal(err)
		}
		n, err := worker.ProcessOrgOnce(t.Context(), org)
		if err != nil || n != 1 {
			t.Fatalf("legacy GC did not retire owned P: n=%d err=%v", n, err)
		}
		if present, err := store.BlockExists(org, d.Block); err != nil || present {
			t.Fatalf("legacy GC canonical retirement: exists=%v err=%v", present, err)
		}
		t.Log("LEGACY GC COMMITTED AND RETIRED EXACT P")
		return
	}
	if d.Mode != "sync" {
		t.Fatal("unknown legacy child mode")
	}
	handler := apipkg.NewSyncHandler(database, s3, manager, &config.Config{Storage: config.StorageConfig{DefaultClass: d.Class}}, nil)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/seafhttp/repo/"+d.Repo+"/update-branch?head="+d.TargetCommit, nil)
	c.Params = gin.Params{{Key: "repo_id", Value: d.Repo}}
	c.Set("org_id", d.Org)
	c.Set("user_id", d.User)
	handler.UpdateBranch(c)
	t.Logf("legacy Sync completed: status=%d body=%s", rec.Code, rec.Body.String())
	var head string
	if err := database.Session().Query(`SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?`, d.Org, d.Repo).Consistency(gocql.Serial).Scan(&head); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(head) != d.TargetCommit {
		t.Fatalf("expected legacy Sync to publish its real target: HEAD=%s want=%s", head, d.TargetCommit)
	}
	t.Log("LEGACY WRITER PUBLISHED TARGET HEAD")
}
