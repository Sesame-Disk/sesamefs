//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	apipkg "github.com/Sesame-Disk/sesamefs/internal/api"
	"github.com/Sesame-Disk/sesamefs/internal/config"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/middleware"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Characterization of the Paxos work on the upload hot path
// (ISSUE-LIBRARY-HEAD-ORG-PARTITION-CONTENTION-01 and the decision record
// docs/HOTPATH-PAXOS-CHARACTERIZATION.md).
//
// The production handlers run in this process on a session that carries a
// query observer, against the real Cassandra and object store. Every
// conditional statement (an LWT, one Paxos round) and every SERIAL or
// LOCAL_SERIAL read (also a Paxos round) is counted per table. A single node
// is enough: the count of Paxos operations does not depend on topology. What
// each one costs does: under SERIAL in a three-DC cluster every round waits on
// another DC.
//
// The pinned numbers describe the code as it is. A change that moves Paxos off
// the hot path is expected to change them; update the numbers and the decision
// record together.

const hotPathPaxosEvidenceEnv = "SESAMEFS_REQUIRE_HOTPATH_PAXOS_CHARACTERIZATION"

func requireHotPathPaxosCassandra(t *testing.T) {
	t.Helper()
	session := shareProjectionDBForTest(t).Session()
	var version string
	if err := session.Query(`SELECT release_version FROM system.local`).Scan(&version); err != nil {
		if os.Getenv(hotPathPaxosEvidenceEnv) == "1" {
			t.Fatalf("%s=1 but Cassandra is not reachable: %v", hotPathPaxosEvidenceEnv, err)
		}
		t.Skipf("Cassandra not reachable: %v", err)
	}
}

// paxosOp names one kind of Paxos operation on one table.
type paxosOp struct {
	table string
	kind  string // "lwt", "serial-read" or "lease-renew"
}

// hotPathLeaseRenewKind marks the finalize lease's renewal LWT. It runs every
// 10 s while a finalization holds the lease, so how many there are depends on
// how long the finalization takes, not on the request. The pinned shapes leave
// it out and the log reports it.
const hotPathLeaseRenewKind = "lease-renew"

func (op paxosOp) String() string { return op.table + " " + op.kind }

type paxosCounts map[paxosOp]int

func (c paxosCounts) total() int {
	n := 0
	for _, v := range c {
		n += v
	}
	return n
}

func (c paxosCounts) String() string {
	if len(c) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(c))
	for op, n := range c {
		keys = append(keys, fmt.Sprintf("%s=%d", op, n))
	}
	sort.Strings(keys)
	return "{" + strings.Join(keys, ", ") + "}"
}

var (
	// A condition clause: IF NOT EXISTS, IF EXISTS or IF <column> <op>.
	hotPathLWTPattern   = regexp.MustCompile(`\bif\s+(not\s+exists|exists|[a-z_][a-z0-9_]*\s*(=|!=|<|>|\bin\b))`)
	hotPathTablePattern = regexp.MustCompile(`^(?:insert\s+into|update|delete\b.*?\bfrom|select\b.*?\bfrom)\s+([a-z0-9_."]+)`)
)

func hotPathStatementTable(statement string) string {
	m := hotPathTablePattern.FindStringSubmatch(statement)
	if m == nil {
		return "?"
	}
	table := strings.Trim(m[1], `"`)
	if i := strings.LastIndex(table, "."); i >= 0 {
		table = strings.Trim(table[i+1:], `"`)
	}
	return table
}

// classifyHotPathStatement returns the Paxos operation a statement performs,
// or false when it performs none.
func classifyHotPathStatement(statement string, consistency gocql.Consistency) (paxosOp, bool) {
	normalized := strings.ToLower(strings.Join(strings.Fields(statement), " "))
	table := hotPathStatementTable(normalized)
	if strings.HasPrefix(normalized, "select") {
		if consistency == gocql.Serial || consistency == gocql.LocalSerial {
			return paxosOp{table: table, kind: "serial-read"}, true
		}
		return paxosOp{}, false
	}
	if hotPathLWTPattern.MatchString(normalized) {
		if table == "gc_leases" && strings.HasPrefix(normalized, "update") {
			return paxosOp{table: table, kind: hotPathLeaseRenewKind}, true
		}
		return paxosOp{table: table, kind: "lwt"}, true
	}
	return paxosOp{}, false
}

// hotPathPaxosObserver records the Paxos operations a handler issues. Retries
// of one operation (Attempt > 0) are counted apart so that a timeout retried
// by the driver does not inflate the per-route shape.
type hotPathPaxosObserver struct {
	mu      sync.Mutex
	counts  paxosCounts
	retries int
	errs    []string
}

func newHotPathPaxosObserver() *hotPathPaxosObserver {
	return &hotPathPaxosObserver{counts: paxosCounts{}}
}

func (o *hotPathPaxosObserver) ObserveQuery(_ context.Context, q gocql.ObservedQuery) {
	consistency := gocql.Any
	if q.Query != nil {
		consistency = q.Query.GetConsistency()
	}
	op, ok := classifyHotPathStatement(q.Statement, consistency)
	if !ok {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if q.Attempt > 0 {
		o.retries++
	} else {
		o.counts[op]++
	}
	if q.Err != nil {
		o.errs = append(o.errs, fmt.Sprintf("%s: %v", op, q.Err))
	}
}

// take returns what was recorded since the last call and starts a new phase.
func (o *hotPathPaxosObserver) take() (paxosCounts, int, []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	counts, retries, errs := o.counts, o.retries, o.errs
	o.counts, o.retries, o.errs = paxosCounts{}, 0, nil
	return counts, retries, errs
}

func TestHotPathPaxosClassifier(t *testing.T) {
	cases := []struct {
		statement   string
		consistency gocql.Consistency
		want        string
	}{
		{"UPDATE libraries SET head_commit_id = ? WHERE org_id = ? AND library_id = ? IF head_commit_id = ?", gocql.LocalQuorum, "libraries lwt"},
		{"INSERT INTO blocks (org_id, block_id) VALUES (?, ?) IF NOT EXISTS", gocql.LocalQuorum, "blocks lwt"},
		{"\n\t\tDELETE FROM libraries WHERE org_id = ? AND library_id = ? IF head_commit_id = null", gocql.LocalQuorum, "libraries lwt"},
		{"UPDATE locked_files SET locked_at = ? WHERE repo_id = ? AND path = ? IF locked_by IN (?)", gocql.LocalQuorum, "locked_files lwt"},
		{"UPDATE blocks SET sha1 = ? WHERE org_id = ? AND block_id = ? IF sha1 = ? AND representation_id = ?", gocql.LocalQuorum, "blocks lwt"},
		{"INSERT INTO gc_leases (role, instance_id, heartbeat) VALUES (?, ?, ?) IF NOT EXISTS USING TTL ?", gocql.LocalQuorum, "gc_leases lwt"},
		{"UPDATE gc_leases USING TTL ? SET instance_id = ?, heartbeat = ? WHERE role = ? IF instance_id = ?", gocql.LocalQuorum, "gc_leases lease-renew"},
		{"DELETE FROM gc_leases WHERE role = ? IF instance_id = ?", gocql.LocalQuorum, "gc_leases lwt"},
		{"SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?", gocql.Serial, "libraries serial-read"},
		{"SELECT digest FROM sesamefs.identity_authority_claims WHERE library_id = ?", gocql.LocalSerial, "identity_authority_claims serial-read"},
		{"SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?", gocql.LocalQuorum, ""},
		{"INSERT INTO fs_objects (library_id, fs_id) VALUES (?, ?)", gocql.LocalQuorum, ""},
		{"UPDATE libraries SET name = ? WHERE org_id = ? AND library_id = ?", gocql.LocalQuorum, ""},
	}
	for _, tc := range cases {
		op, ok := classifyHotPathStatement(tc.statement, tc.consistency)
		got := ""
		if ok {
			got = op.String()
		}
		if got != tc.want {
			t.Errorf("classify %q at %v = %q, want %q", tc.statement, tc.consistency, got, tc.want)
		}
	}
}

// hotPathLibrary is a fresh library in the admin's organization.
type hotPathLibrary struct {
	repoID, orgID, userID string
}

func newHotPathLibrary(t *testing.T, database *dbpkg.DB) hotPathLibrary {
	t.Helper()
	repoID := createTestLibrary(t, adminClient, fmt.Sprintf("inttest-paxos-%d", time.Now().UnixNano()))
	lib := hotPathLibrary{repoID: repoID}
	if err := database.Session().Query(`SELECT org_id, owner_id FROM libraries_by_id WHERE library_id = ?`, repoID).Scan(&lib.orgID, &lib.userID); err != nil {
		t.Fatalf("resolve org/owner of %s: %v", repoID, err)
	}
	return lib
}

func hotPathRandomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// cleanupHotPathBlocks removes what an upload of content left for each of its
// 8 MiB blocks: every block reference, the mapping, the canonical row and the
// stored object. It runs before the library cleanup registered earlier.
func cleanupHotPathBlocks(t *testing.T, database *dbpkg.DB, orgID, repoID string, blocks ...[]byte) {
	t.Helper()
	for _, block := range blocks {
		blockID, sha1ID := sha256hex(block), sha1hex(block)
		refs, err := database.ListBlockReferrers(orgID, blockID)
		if err != nil {
			t.Errorf("list referrers of %s: %v", blockID, err)
		}
		cleanupUploadedBlockArtifactsForTest(t, orgID, repoID, blockID, sha1ID, refs...)
	}
}

func splitHotPathBlocks(content []byte, size int) [][]byte {
	var blocks [][]byte
	for start := 0; start < len(content); start += size {
		end := start + size
		if end > len(content) {
			end = len(content)
		}
		blocks = append(blocks, content[start:end])
	}
	return blocks
}

// seafHTTPUploadMode is how a client sends a file to /seafhttp/upload-api.
type seafHTTPUploadMode int

const (
	// seafHTTPSingleShot is one request without Content-Range: the server
	// stores the whole file, up to 1 GiB, as one block.
	seafHTTPSingleShot seafHTTPUploadMode = iota
	// seafHTTPChunked is the resumable.js upload the web UI uses for large
	// files. Here the file is sent as its only chunk; finalization splits it
	// into 8 MiB blocks either way.
	seafHTTPChunked
)

const seafHTTPChunkedBlockSize = 8 * 1024 * 1024

// hotPathSeafHTTPUpload uploads one file through the production
// /seafhttp/upload-api route and returns the Paxos operations it issued.
// A non-empty relativeDir sends the file as a folder upload does
// (relative_path "<relativeDir>/<name>"), creating the missing directories.
func hotPathSeafHTTPUpload(t *testing.T, database *dbpkg.DB, lib hotPathLibrary, content []byte, mode seafHTTPUploadMode, relativeDir string) (paxosCounts, int, []string) {
	t.Helper()
	class := x1StorageClass(t)
	observer := newHotPathPaxosObserver()
	writerDB := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], observer)
	s3 := newVerificationS3Store(t)
	manager := storage.NewManager()
	manager.SetDefaultClass(class)
	manager.RegisterBackend(class, s3, "")
	tokens := dbpkg.NewTokenStore(writerDB, time.Hour)
	token, err := tokens.CreateUploadToken(lib.orgID, lib.repoID, "/", lib.userID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tokens.DeleteToken(token) })
	cfg := &config.Config{Storage: config.StorageConfig{DefaultClass: class}}
	handler := apipkg.NewSeafHTTPHandler(s3, manager, writerDB, apipkg.NewCassandraTokenAdapter(tokens), cfg, middleware.NewPermissionMiddleware(writerDB))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler.RegisterSeafHTTPRoutes(router)

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	name := "paxos-" + uuid.NewString()[:8] + ".bin"
	fields := map[string]string{"parent_dir": "/", "replace": "0"}
	if mode == seafHTTPChunked {
		fields["resumableIdentifier"] = "paxos-" + uuid.NewString()
	}
	if relativeDir != "" {
		fields["relative_path"] = relativeDir + "/" + name
	}
	for field, value := range fields {
		if err := form.WriteField(field, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/seafhttp/upload-api/"+token, &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	blocks := [][]byte{content}
	if mode == seafHTTPChunked {
		req.Header.Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(content)-1, len(content)))
		blocks = splitHotPathBlocks(content, seafHTTPChunkedBlockSize)
	}
	rec := httptest.NewRecorder()

	observer.take() // exclude the fixture's own queries
	router.ServeHTTP(rec, req)
	counts, retries, errs := observer.take()
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	cleanupHotPathBlocks(t, database, lib.orgID, lib.repoID, blocks...)
	return counts, retries, errs
}

type hotPathSyncPhase struct {
	name    string
	counts  paxosCounts
	retries int
	errs    []string
}

// hotPathSyncPush runs one desktop-client push of files new files, one block
// each, through the production Sync handlers: the commit object, recv-fs
// with every fs object, one put-block per block and the HEAD update. It returns
// the Paxos operations of each phase.
func hotPathSyncPush(t *testing.T, database *dbpkg.DB, lib hotPathLibrary, files int) []hotPathSyncPhase {
	t.Helper()
	class := x1StorageClass(t)
	observer := newHotPathPaxosObserver()
	writerDB := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], observer)
	handler := w24Handler(t, writerDB, class)
	request := func(method, suffix, param, value string, body []byte, serve func(*gin.Context)) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(method, "/seafhttp/repo/"+lib.repoID+suffix, bytes.NewReader(body))
		if strings.HasPrefix(suffix, "/commit/") {
			c.Request.Header.Set("Content-Type", "application/json")
		} else {
			c.Request.Header.Set("Content-Type", "application/octet-stream")
		}
		c.Params = gin.Params{{Key: "repo_id", Value: lib.repoID}}
		if param != "" {
			c.Params = append(c.Params, gin.Param{Key: param, Value: value})
		}
		c.Set("org_id", lib.orgID)
		c.Set("user_id", lib.userID)
		serve(c)
		return rec
	}
	ok := func(phase string, rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", phase, rec.Code, rec.Body.String())
		}
	}

	head := borrowedFSReadHead(t, database, lib.orgID, lib.repoID)
	contents := make([][]byte, files)
	objects := make([]syncPackedFSObject, 0, files+1)
	dirents := make([]apipkg.FSEntry, 0, files)
	for i := range contents {
		contents[i] = hotPathRandomBytes(t, 1024)
		fileJSON := mustMarshalSyncObjectForTest(t, map[string]interface{}{"block_ids": []string{sha1hex(contents[i])}, "size": len(contents[i]), "type": 1, "version": 1})
		fileID := syncSHA1HexForTest(fileJSON)
		objects = append(objects, syncPackedFSObject{fsID: fileID, jsonData: fileJSON})
		dirents = append(dirents, apipkg.FSEntry{ID: fileID, Mode: 33188, Mtime: time.Now().Unix(), Name: fmt.Sprintf("paxos-%d-%s.bin", i, uuid.NewString()[:8]), Size: int64(len(contents[i]))})
	}
	sort.Slice(dirents, func(i, j int) bool { return dirents[i].Name > dirents[j].Name })
	rootJSON := mustMarshalSyncObjectForTest(t, map[string]interface{}{"dirents": dirents, "type": 3, "version": 1})
	root := syncSHA1HexForTest(rootJSON)
	objects = append(objects, syncPackedFSObject{fsID: root, jsonData: rootJSON})
	commit := syncSHA1HexForTest([]byte("paxos-" + uuid.NewString()))
	payload := mustMarshalSyncObjectForTest(t, map[string]interface{}{"commit_id": commit, "repo_id": lib.repoID, "root_id": root, "parent_id": head, "description": "hot-path Paxos characterization", "ctime": time.Now().Unix(), "version": 1})

	var phases []hotPathSyncPhase
	record := func(name string) {
		counts, retries, errs := observer.take()
		phases = append(phases, hotPathSyncPhase{name: name, counts: counts, retries: retries, errs: errs})
	}
	observer.take()
	ok("put commit", request(http.MethodPut, "/commit/"+commit, "commit_id", commit, payload, handler.PutCommit))
	record("put-commit")
	ok("recv-fs", request(http.MethodPost, "/recv-fs", "", "", packSyncFSObjectsForTest(t, objects...), handler.RecvFS))
	record("recv-fs")
	for _, content := range contents {
		sha := sha1hex(content)
		ok("put block", request(http.MethodPut, "/block/"+sha, "block_id", sha, content, handler.PutBlock))
	}
	record("put-block")
	ok("update HEAD", request(http.MethodPut, "/commit/HEAD?head="+commit, "commit_id", "HEAD", nil, handler.PutCommit))
	record("head")
	if got := borrowedFSReadHead(t, database, lib.orgID, lib.repoID); got != commit {
		t.Fatalf("HEAD=%s, want %s", got, commit)
	}
	cleanupHotPathBlocks(t, database, lib.orgID, lib.repoID, contents...)
	// The Sync path stores a published-block repair per push; settle any left.
	t.Cleanup(func() {
		for bucket := 0; bucket < dbpkg.PublishedBlockReferenceRepairBuckets; bucket++ {
			_ = database.Session().Query(`DELETE FROM published_block_reference_repairs WHERE bucket = ? AND org_id = ? AND repo_id = ?`, bucket, lib.orgID, lib.repoID).Exec()
		}
	})
	return phases
}

func diffHotPathCounts(more, less paxosCounts) paxosCounts {
	out := paxosCounts{}
	for op, n := range more {
		if d := n - less[op]; d != 0 {
			out[op] = d
		}
	}
	for op, n := range less {
		if _, seen := more[op]; !seen {
			out[op] = -n
		}
	}
	return out
}

func hotPathLWT(table string) paxosOp        { return paxosOp{table: table, kind: "lwt"} }
func hotPathSerialRead(table string) paxosOp { return paxosOp{table: table, kind: "serial-read"} }

// hotPathWebUploadShape is the Paxos shape of one /seafhttp/upload-api upload
// of a new file: the install of each new block, the per-library finalize lease
// (acquire and release), an identity claim per new fs object (the file, every
// directory between it and the root, and the root, all rewritten) plus the
// commit's claim and its SERIAL read, and the HEAD CAS. depth counts the
// directories between the root and the file.
func hotPathWebUploadShape(blocks, depth int) paxosCounts {
	return paxosCounts{
		hotPathLWT("blocks"):                           blocks,
		hotPathLWT("gc_leases"):                        2,
		hotPathLWT("identity_authority_claims"):        1 + depth + 1 + 1,
		hotPathSerialRead("identity_authority_claims"): 1,
		hotPathLWT("libraries"):                        1,
	}
}

// hotPathSyncPushShape is the Paxos shape of each phase of a desktop push of
// files new files with one block each, all in the library root.
func hotPathSyncPushShape(files int) map[string]paxosCounts {
	return map[string]paxosCounts{
		"put-commit": {hotPathLWT("identity_authority_claims"): 1, hotPathSerialRead("identity_authority_claims"): 1},
		"recv-fs":    {hotPathLWT("identity_authority_claims"): files + 1},
		"put-block":  {hotPathLWT("blocks"): files},
		"head":       {hotPathLWT("libraries"): 1},
	}
}

// TestHotPathPaxosUploadCharacterization pins the Paxos operations of the two
// upload routes clients use. A change that moves Paxos off the hot path
// changes these shapes on purpose: update them together with
// docs/HOTPATH-PAXOS-CHARACTERIZATION.md.
func TestHotPathPaxosUploadCharacterization(t *testing.T) {
	requireHotPathPaxosCassandra(t)
	database := shareProjectionDBForTest(t)
	lib := newHotPathLibrary(t, database)
	const blockSize = seafHTTPChunkedBlockSize

	check := func(route string, got paxosCounts, retries int, errs []string, want paxosCounts) {
		t.Helper()
		t.Logf("PAXOS %-40s total=%-3d driver-retries=%d %s", route, got.total(), retries, got)
		if len(errs) != 0 {
			t.Logf("PAXOS %-40s errors=%v", route, errs)
		}
		pinned := paxosCounts{}
		for op, n := range got {
			if op.kind != hotPathLeaseRenewKind {
				pinned[op] = n
			}
		}
		if diff := diffHotPathCounts(pinned, want); len(diff) != 0 {
			t.Errorf("%s: Paxos shape changed by %s (got %s, want %s); if intended, update this test and docs/HOTPATH-PAXOS-CHARACTERIZATION.md", route, diff, got, want)
		}
	}

	got, retries, errs := hotPathSeafHTTPUpload(t, database, lib, hotPathRandomBytes(t, 1024), seafHTTPSingleShot, "")
	check("web single-shot, 1 block, root", got, retries, errs, hotPathWebUploadShape(1, 0))
	got, retries, errs = hotPathSeafHTTPUpload(t, database, lib, hotPathRandomBytes(t, 1024), seafHTTPSingleShot, "paxos-"+uuid.NewString()[:8]+"/b")
	check("web single-shot, 1 block, depth 2", got, retries, errs, hotPathWebUploadShape(1, 2))
	got, retries, errs = hotPathSeafHTTPUpload(t, database, lib, hotPathRandomBytes(t, blockSize+1024), seafHTTPChunked, "")
	check("web chunked, 2 blocks, root", got, retries, errs, hotPathWebUploadShape(2, 0))
	got, retries, errs = hotPathSeafHTTPUpload(t, database, lib, hotPathRandomBytes(t, 3*blockSize+1024), seafHTTPChunked, "")
	check("web chunked, 4 blocks, root", got, retries, errs, hotPathWebUploadShape(4, 0))

	for _, files := range []int{1, 3} {
		want := hotPathSyncPushShape(files)
		for _, phase := range hotPathSyncPush(t, database, lib, files) {
			check(fmt.Sprintf("sync push %d files: %s", files, phase.name), phase.counts, phase.retries, phase.errs, want[phase.name])
		}
	}
}

// hotPathPaxosStateSession opens a session that may read system.paxos. The
// application role does not hold that permission, so the superuser is tried
// next. .env.example sets only CASSANDRA_SUPERUSER_PASSWORD: the stack keeps
// the built-in superuser name, cassandra (scripts/prod-preflight.sh requires
// it), so that name is the default.
func hotPathPaxosStateSession(t *testing.T) *gocql.Session {
	t.Helper()
	open := func(user, pass string) (*gocql.Session, error) {
		cluster := gocql.NewCluster(splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0])
		cluster.ProtoVersion = 4
		cluster.DisableInitialHostLookup = true
		cluster.Consistency = gocql.One
		cluster.Timeout = 5 * time.Second
		cluster.ConnectTimeout = 10 * time.Second
		if user != "" && pass != "" {
			cluster.Authenticator = gocql.PasswordAuthenticator{Username: user, Password: pass}
		}
		return cluster.CreateSession()
	}
	probe := func(session *gocql.Session) error {
		return session.Query(`SELECT row_key FROM system.paxos LIMIT 1`).Exec()
	}
	var failures []string
	for _, creds := range [][2]string{
		{os.Getenv("CASSANDRA_USERNAME"), os.Getenv("CASSANDRA_PASSWORD")},
		{envOrDefault("CASSANDRA_SUPERUSER_USERNAME", "cassandra"), os.Getenv("CASSANDRA_SUPERUSER_PASSWORD")},
	} {
		session, err := open(creds[0], creds[1])
		if err == nil {
			if err = probe(session); err == nil {
				t.Cleanup(session.Close)
				return session
			}
			session.Close()
		}
		failures = append(failures, fmt.Sprintf("%q: %v", creds[0], err))
	}
	t.Fatalf("no session can read system.paxos: %s", strings.Join(failures, "; "))
	return nil
}

// TestHotPathPaxosLibraryHeadSharesOrgPartition shows where the HEAD CAS runs
// its Paxos round. libraries is keyed ((org_id), library_id) and Cassandra
// keeps one Paxos state per partition, so a HEAD publication in one library
// advances the same state as a publication in any other library of the
// organization: concurrent publications in different libraries contend. Each
// step goes through the production upload route.
func TestHotPathPaxosLibraryHeadSharesOrgPartition(t *testing.T) {
	requireHotPathPaxosCassandra(t)
	database := shareProjectionDBForTest(t)
	paxos := hotPathPaxosStateSession(t)
	keyspace := envOrDefault("CASSANDRA_KEYSPACE", "sesamefs")
	var tableID gocql.UUID
	if err := paxos.Query(`SELECT id FROM system_schema.tables WHERE keyspace_name = ? AND table_name = ?`, keyspace, "libraries").Scan(&tableID); err != nil {
		t.Fatalf("libraries table id: %v", err)
	}
	var partitionColumns []string
	iter := paxos.Query(`SELECT column_name, kind FROM system_schema.columns WHERE keyspace_name = ? AND table_name = ?`, keyspace, "libraries").Iter()
	var column, kind string
	for iter.Scan(&column, &kind) {
		if kind == "partition_key" {
			partitionColumns = append(partitionColumns, column)
		}
	}
	if err := iter.Close(); err != nil {
		t.Fatalf("libraries columns: %v", err)
	}
	if len(partitionColumns) != 1 || partitionColumns[0] != "org_id" {
		t.Fatalf("libraries partition key = %v; this characterization covers the ((org_id), library_id) layout", partitionColumns)
	}
	// Paxos state of one libraries partition: the time of its last committed
	// round. The row key of a single-UUID partition key is the 16 UUID bytes.
	lastCommit := func(id string) (time.Time, bool) {
		t.Helper()
		key := uuid.MustParse(id)
		var at gocql.UUID
		err := paxos.Query(`SELECT most_recent_commit_at FROM system.paxos WHERE row_key = ? AND cf_id = ?`, key[:], tableID).Scan(&at)
		if err == gocql.ErrNotFound {
			return time.Time{}, false
		}
		if err != nil {
			t.Fatalf("read Paxos state of %s: %v", id, err)
		}
		return at.Time(), true
	}

	first := newHotPathLibrary(t, database)
	second := newHotPathLibrary(t, database)
	if first.orgID != second.orgID {
		t.Fatalf("libraries in different organizations: %s, %s", first.orgID, second.orgID)
	}
	hotPathSeafHTTPUpload(t, database, first, hotPathRandomBytes(t, 1024), seafHTTPSingleShot, "")
	afterFirst, found := lastCommit(first.orgID)
	if !found {
		t.Fatalf("no Paxos state for organization partition %s after a HEAD publication", first.orgID)
	}
	hotPathSeafHTTPUpload(t, database, second, hotPathRandomBytes(t, 1024), seafHTTPSingleShot, "")
	afterSecond, found := lastCommit(second.orgID)
	if !found || !afterSecond.After(afterFirst) {
		t.Fatalf("publication in library %s did not advance the organization's Paxos state: before=%v after=%v found=%v", second.repoID, afterFirst, afterSecond, found)
	}
	for _, lib := range []hotPathLibrary{first, second} {
		if at, found := lastCommit(lib.repoID); found {
			t.Fatalf("library %s has Paxos state of its own in libraries (at %v); the partition is no longer the organization", lib.repoID, at)
		}
	}
	t.Logf("PAXOS libraries: HEAD publications in libraries %s and %s advanced one Paxos state, partition org_id=%s (%v -> %v)", first.repoID, second.repoID, first.orgID, afterFirst, afterSecond)
}
