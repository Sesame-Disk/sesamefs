//go:build integration

package integration

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/config"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

// mappingAuthorityContent is one stored canonical block: its SHA-1 external
// id, SHA-256 internal id and the exact bytes at its minted physical key.
type mappingAuthorityContent struct {
	bytes    []byte
	external string
	internal string
}

func newMappingAuthorityContent(label string) mappingAuthorityContent {
	content := []byte("pc-d1b3 mapping authority " + label)
	sha1Sum, sha256Sum := sha1.Sum(content), sha256.Sum256(content)
	return mappingAuthorityContent{bytes: content, external: hex.EncodeToString(sha1Sum[:]), internal: hex.EncodeToString(sha256Sum[:])}
}

// storeMappingAuthorityBlock installs the canonical block row with a minted
// key and writes the exact bytes to MinIO.
func storeMappingAuthorityBlock(t *testing.T, ctx context.Context, database *dbpkg.DB, blockStore *storage.BlockStore, orgID string, content mappingAuthorityContent) {
	t.Helper()
	storageKey, err := blockStore.MintStorageKey(content.internal)
	if err != nil {
		t.Fatalf("mint mapping-authority block key: %v", err)
	}
	if _, err := blockStore.PutObjectAutoDirect(ctx, storageKey, content.bytes); err != nil {
		t.Fatalf("store mapping-authority block bytes: %v", err)
	}
	seedLibraryBaselineCertifierBlock(t, database, orgID, content.internal, content.external, "evidence", storageKey, int64(len(content.bytes)))
}

func writeMutableBlockMapping(t *testing.T, database *dbpkg.DB, orgID, externalID, internalID string) {
	t.Helper()
	w2PostHeadRetryEachQuorum(t, "write mutable block_id_mappings row", func() error {
		return database.Session().Query(`
			INSERT INTO block_id_mappings (org_id, representation_id, external_id, internal_id, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, orgID, dbpkg.PlainBlockRepresentationID, externalID, internalID, time.Now().UTC()).Consistency(gocql.EachQuorum).Exec()
	})
}

// seedSHA1OnlyMappingLibrary creates a library whose HEAD reaches one
// SHA1-only file (block_ids holds the SHA-1, no paired SHA-256 list) with
// authority-verified commit and fs_object claims. Mapping state is left to
// the caller.
func seedSHA1OnlyMappingLibrary(t *testing.T, database *dbpkg.DB, orgID, label string, content mappingAuthorityContent) (libraryID, head, fileFSID string) {
	t.Helper()
	libraryID, ownerID := uuid.NewString(), uuid.NewString()
	head = "pc-d1b3-" + label + "-" + uuid.NewString()
	rootFSID := baselineCertifierTestFSID("pc-d1b3-root-" + head)
	fileFSID = baselineCertifierTestFSID("pc-d1b3-file-" + head)
	sizeBytes := int64(len(content.bytes))
	seedLibraryBaselineCertifierLibrary(t, database, orgID, libraryID, ownerID, head, sizeBytes)
	seedLibraryBaselineCertifierCommit(t, database, libraryID, head, rootFSID)
	entries, err := json.Marshal([]map[string]interface{}{{"id": fileFSID, "mode": 33188, "mtime": time.Now().Unix(), "name": "legacy.txt"}})
	if err != nil {
		t.Fatalf("marshal SHA1-only root: %v", err)
	}
	w2PostHeadRetryEachQuorum(t, "seed SHA1-only mapping root", func() error {
		return database.Session().Query(`INSERT INTO fs_objects (library_id, fs_id, obj_type, dir_entries, mtime) VALUES (?, ?, ?, ?, ?)`,
			libraryID, rootFSID, "dir", string(entries), time.Now().Unix()).Consistency(gocql.EachQuorum).Exec()
	})
	if _, err := dbpkg.AuthorizeFSObjectProjection(context.Background(), database.Session(), dbpkg.FSObjectProjection{
		LibraryID: libraryID, FSID: rootFSID, ObjectType: "dir", DirectoryEntries: string(entries),
	}); err != nil {
		t.Fatalf("authorize SHA1-only mapping root: %v", err)
	}
	w2PostHeadRetryEachQuorum(t, "seed SHA1-only mapping file", func() error {
		return database.Session().Query(`
			INSERT INTO fs_objects (library_id, fs_id, obj_type, size_bytes, mtime, block_ids)
			VALUES (?, ?, ?, ?, ?, ?)
		`, libraryID, fileFSID, "file", sizeBytes, time.Now().Unix(), []string{content.external}).Consistency(gocql.EachQuorum).Exec()
	})
	if _, err := dbpkg.AuthorizeFSObjectProjection(context.Background(), database.Session(), dbpkg.FSObjectProjection{
		LibraryID: libraryID, FSID: fileFSID, ObjectType: "file", SizeBytes: sizeBytes,
		FileLayout: dbpkg.FileStorageSHA1Only, LogicalSHA1IDs: []string{content.external},
	}); err != nil {
		t.Fatalf("authorize SHA1-only mapping file: %v", err)
	}
	return libraryID, head, fileFSID
}

func libraryReferenceExists(t *testing.T, database *dbpkg.DB, orgID, blockID, libraryID, fileFSID string) bool {
	t.Helper()
	var storedLibraryID string
	err := database.Session().Query(`SELECT library_id FROM block_references WHERE org_id = ? AND block_id = ? AND referrer = ?`,
		orgID, blockID, dbpkg.BlockReferrerForFSObject(libraryID, fileFSID)).Consistency(gocql.EachQuorum).Scan(&storedLibraryID)
	if err == gocql.ErrNotFound {
		return false
	}
	if err != nil {
		t.Fatalf("read fs_object liveness for %s: %v", blockID, err)
	}
	return storedLibraryID == libraryID
}

func requireNoMappingAuthority(t *testing.T, ctx context.Context, database *dbpkg.DB, orgID, externalID string) {
	t.Helper()
	claim, found, err := dbpkg.ReadBlockMappingAuthority(ctx, database.Session(), orgID, dbpkg.PlainBlockRepresentationID, externalID)
	if err != nil || found {
		t.Fatalf("mapping authority for %s = %+v found=%t err=%v; want no claim", externalID, claim, found, err)
	}
}

func requireMappingAuthority(t *testing.T, ctx context.Context, database *dbpkg.DB, orgID, externalID, internalID string) {
	t.Helper()
	claim, found, err := dbpkg.ReadBlockMappingAuthority(ctx, database.Session(), orgID, dbpkg.PlainBlockRepresentationID, externalID)
	if err != nil || !found || claim.InternalID != internalID || claim.ContractVersion != dbpkg.SupportedBlockMappingAuthorityContract {
		t.Fatalf("mapping authority for %s = %+v found=%t err=%v; want %s", externalID, claim, found, err, internalID)
	}
}

func requireNoBaselineWitness(t *testing.T, database *dbpkg.DB, orgID, libraryID string) {
	t.Helper()
	if _, witness := readLibraryBaselineCertifierWitness(t, database, orgID, libraryID); witness != nil {
		t.Fatalf("library %s gained witness %q", libraryID, *witness)
	}
}

func requireBaselineWitness(t *testing.T, database *dbpkg.DB, orgID, libraryID, head string) {
	t.Helper()
	if current, witness := readLibraryBaselineCertifierWitness(t, database, orgID, libraryID); current != head || witness == nil || *witness != head {
		t.Fatalf("library %s witness head=%q certified=%v, want %q", libraryID, current, witness, head)
	}
}

// TestBlockMappingAuthorityCertifierRealCassandra proves the Mapping Authority
// end to end against real Cassandra and MinIO: a converged but unprovable
// mapping stays unproven (M19), a provable one is promoted and certifies, a
// later or racing ordinary mapping write can neither change the resolution nor
// be witnessed (M18), and deleting the mutable row does not retire the
// authority.
func TestBlockMappingAuthorityCertifierRealCassandra(t *testing.T) {
	database := shareProjectionDBForTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	orgID := uuid.NewString()
	storageManager, blockStore := newLibraryBaselineCertifierStorage(t, ctx, orgID, libraryBaselineCertifierMinIOEndpoint, true)
	legacy := newMappingAuthorityContent("legacy " + orgID)
	other := newMappingAuthorityContent("other " + orgID)
	storeMappingAuthorityBlock(t, ctx, database, blockStore, orgID, legacy)
	storeMappingAuthorityBlock(t, ctx, database, blockStore, orgID, other)

	t.Run("missing mutable mapping stays unproven", func(t *testing.T) {
		unmapped := newMappingAuthorityContent("unmapped " + orgID)
		libraryID, head, _ := seedSHA1OnlyMappingLibrary(t, database, orgID, "unmapped", unmapped)
		result := database.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
		if result.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || result.Reason != dbpkg.LibraryBaselineReasonIdentityUnproven ||
			result.UniqueBlocks != 0 || result.PermanentLivenessWrites != 0 || result.PhysicalRevalidations != 0 {
			t.Fatalf("SHA1-only without any mapping = %+v; want NOT_CERTIFIED/identity_unproven before physical or liveness work", result)
		}
		requireNoMappingAuthority(t, ctx, database, orgID, unmapped.external)
		requireNoBaselineWitness(t, database, orgID, libraryID)
	})

	t.Run("converged mapping without provenance stays unproven", func(t *testing.T) {
		forged := newMappingAuthorityContent("forged " + orgID)
		// Every replica agrees on forged.external -> other.internal, and those
		// bytes exist, but they do not hash to forged.external.
		writeMutableBlockMapping(t, database, orgID, forged.external, other.internal)
		libraryID, head, _ := seedSHA1OnlyMappingLibrary(t, database, orgID, "forged", forged)
		result := database.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
		if result.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || result.Reason != dbpkg.LibraryBaselineReasonIdentityUnproven ||
			result.UniqueBlocks != 0 || result.PermanentLivenessWrites != 0 || result.PhysicalRevalidations != 0 {
			t.Fatalf("converged but unprovable mapping = %+v; want NOT_CERTIFIED/identity_unproven", result)
		}
		requireNoMappingAuthority(t, ctx, database, orgID, forged.external)
		requireNoBaselineWitness(t, database, orgID, libraryID)
	})

	writeMutableBlockMapping(t, database, orgID, legacy.external, legacy.internal)
	firstLibrary, firstHead, firstFile := seedSHA1OnlyMappingLibrary(t, database, orgID, "proven", legacy)
	t.Run("provable mapping is promoted and certifies", func(t *testing.T) {
		result := database.CertifyLibraryBaseline(ctx, storageManager, orgID, firstLibrary, firstHead)
		if result.Outcome != dbpkg.LibraryBaselineCertificationCertified || result.Reason != dbpkg.LibraryBaselineReasonApplied ||
			result.UniqueBlocks != 1 || result.PermanentLivenessWrites != 1 || result.PhysicalRevalidations != 2 {
			t.Fatalf("SHA1-only with proven mapping = %+v; want CERTIFIED with one canonical dependency", result)
		}
		requireMappingAuthority(t, ctx, database, orgID, legacy.external, legacy.internal)
		requireBaselineWitness(t, database, orgID, firstLibrary, firstHead)
		if !libraryReferenceExists(t, database, orgID, legacy.internal, firstLibrary, firstFile) {
			t.Fatal("certified SHA1-only dependency has no permanent liveness on its authoritative canonical block")
		}
	})

	t.Run("later ordinary mapping write cannot change resolution or be witnessed", func(t *testing.T) {
		writeMutableBlockMapping(t, database, orgID, legacy.external, other.internal)
		libraryID, head, fileFSID := seedSHA1OnlyMappingLibrary(t, database, orgID, "diverged", legacy)
		result := database.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
		if result.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || result.Reason != dbpkg.LibraryBaselineReasonIdentityConflict ||
			result.PermanentLivenessWrites != 0 || result.PhysicalRevalidations != 0 {
			t.Fatalf("authority A with mutable B = %+v; want NOT_CERTIFIED/identity_conflict before physical or liveness work", result)
		}
		if libraryReferenceExists(t, database, orgID, other.internal, libraryID, fileFSID) {
			t.Fatal("certifier consumed the mutable mapping B")
		}
		requireMappingAuthority(t, ctx, database, orgID, legacy.external, legacy.internal)
		requireNoBaselineWitness(t, database, orgID, libraryID)

		writeMutableBlockMapping(t, database, orgID, legacy.external, legacy.internal)
		restored := database.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
		if restored.Outcome != dbpkg.LibraryBaselineCertificationCertified || !libraryReferenceExists(t, database, orgID, legacy.internal, libraryID, fileFSID) {
			t.Fatalf("certification after the mutable row agrees again = %+v; want CERTIFIED resolving A", restored)
		}
	})

	t.Run("mutable write racing certification is refused before witness", func(t *testing.T) {
		libraryID, head, fileFSID := seedSHA1OnlyMappingLibrary(t, database, orgID, "race", legacy)
		raced := false
		result := database.CertifyLibraryBaselineWithIntegrationHooks(ctx, storageManager, orgID, libraryID, head, dbpkg.LibraryBaselineCertifierIntegrationHooks{
			AfterLiveness: func(hookCtx context.Context, _, blockID string, _ dbpkg.BlockPhysicalLocation) {
				if blockID != legacy.internal {
					t.Fatalf("race hook saw block %s, want authority %s", blockID, legacy.internal)
				}
				if err := database.Session().Query(`UPDATE block_id_mappings SET internal_id = ? WHERE org_id = ? AND representation_id = ? AND external_id = ?`,
					other.internal, orgID, dbpkg.PlainBlockRepresentationID, legacy.external).WithContext(hookCtx).Consistency(gocql.EachQuorum).Exec(); err != nil {
					t.Fatalf("race mutable mapping to B: %v", err)
				}
				raced = true
			},
		})
		if !raced || result.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || result.Reason != dbpkg.LibraryBaselineReasonIdentityConflict {
			t.Fatalf("mutable write landing during certification = raced:%t %+v; want NOT_CERTIFIED/identity_conflict", raced, result)
		}
		if !libraryReferenceExists(t, database, orgID, legacy.internal, libraryID, fileFSID) || libraryReferenceExists(t, database, orgID, other.internal, libraryID, fileFSID) {
			t.Fatal("race leg must have protected only the authoritative block A")
		}
		requireNoBaselineWitness(t, database, orgID, libraryID)
		writeMutableBlockMapping(t, database, orgID, legacy.external, legacy.internal)
	})

	t.Run("mutable row deletion does not retire authority", func(t *testing.T) {
		if err := database.Session().Query(`DELETE FROM block_id_mappings WHERE org_id = ? AND representation_id = ? AND external_id = ?`,
			orgID, dbpkg.PlainBlockRepresentationID, legacy.external).Consistency(gocql.EachQuorum).Exec(); err != nil {
			t.Fatalf("delete mutable mapping: %v", err)
		}
		libraryID, head, fileFSID := seedSHA1OnlyMappingLibrary(t, database, orgID, "deleted-row", legacy)
		result := database.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
		if result.Outcome != dbpkg.LibraryBaselineCertificationCertified || !libraryReferenceExists(t, database, orgID, legacy.internal, libraryID, fileFSID) {
			t.Fatalf("authority after mutable-row deletion = %+v; want CERTIFIED resolving A", result)
		}
		requireMappingAuthority(t, ctx, database, orgID, legacy.external, legacy.internal)
	})
}

type mappingAuthorityStatementObserver struct {
	mu         sync.Mutex
	statements []string
}

func (o *mappingAuthorityStatementObserver) ObserveQuery(_ context.Context, observed gocql.ObservedQuery) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.statements = append(o.statements, strings.ToLower(observed.Statement))
}

func (o *mappingAuthorityStatementObserver) ObserveBatch(_ context.Context, observed gocql.ObservedBatch) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, statement := range observed.Statements {
		o.statements = append(o.statements, strings.ToLower(statement))
	}
}

// TestUploadMappingWritersIssueNoAuthorityPaxosRealCassandra measures, on a
// real session, every statement the upload-side mapping writers send: plain
// reads and inserts only, never an LWT or a mapping-authority statement.
func TestUploadMappingWritersIssueNoAuthorityPaxosRealCassandra(t *testing.T) {
	observer := &mappingAuthorityStatementObserver{}
	database, err := dbpkg.NewWithObservers(config.DatabaseConfig{
		Hosts:       splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042"),
		Keyspace:    envOrDefault("CASSANDRA_KEYSPACE", "sesamefs"),
		Consistency: envOrDefault("CASSANDRA_CONSISTENCY", "LOCAL_QUORUM"),
		LocalDC:     envOrDefault("CASSANDRA_LOCAL_DC", "datacenter1"),
		Username:    os.Getenv("CASSANDRA_USERNAME"),
		Password:    os.Getenv("CASSANDRA_PASSWORD"),
	}, observer, observer)
	if err != nil {
		t.Fatalf("connect observed session: %v", err)
	}
	defer database.Close()
	orgID := uuid.NewString()
	legacy, web := newMappingAuthorityContent("hot legacy "+orgID), newMappingAuthorityContent("hot web "+orgID)
	if err := database.WriteBlockIDMapping(orgID, dbpkg.PlainBlockRepresentationID, legacy.external, legacy.internal, time.Time{}); err != nil {
		t.Fatalf("legacy upload mapping write: %v", err)
	}
	if err := database.WriteVerifiedWebBlockMapping(orgID, dbpkg.PlainBlockRepresentationID, web.external, web.internal, time.Time{}); err != nil {
		t.Fatalf("web upload mapping write: %v", err)
	}
	if err := database.WriteVerifiedWebBlockMapping(orgID, dbpkg.PlainBlockRepresentationID, web.external, web.internal, time.Time{}); err != nil {
		t.Fatalf("idempotent web upload mapping write: %v", err)
	}
	observer.mu.Lock()
	statements := append([]string(nil), observer.statements...)
	observer.mu.Unlock()
	mappingStatements := 0
	for _, statement := range statements {
		if strings.Contains(statement, "block_mapping_authority_claims") || strings.Contains(statement, "if not exists") || strings.Contains(statement, " if ") {
			t.Fatalf("upload mapping writer issued authority/LWT statement: %q", statement)
		}
		if strings.Contains(statement, "block_id_mappings") {
			mappingStatements++
		}
	}
	if mappingStatements == 0 {
		t.Fatal("observer saw no mapping statements; this gate is vacuous")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	requireNoMappingAuthority(t, ctx, database, orgID, legacy.external)
	requireNoMappingAuthority(t, ctx, database, orgID, web.external)
}
