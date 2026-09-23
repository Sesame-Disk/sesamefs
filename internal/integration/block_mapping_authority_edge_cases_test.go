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

// requireFrozenProjection asserts the ordinary row readers use resolves to
// internalID at the frozen timestamp in every datacenter.
func requireFrozenProjection(t *testing.T, ctx context.Context, database *dbpkg.DB, orgID, externalID, internalID string) {
	t.Helper()
	mapped, frozen, found, err := dbpkg.ReadBlockMappingProjection(ctx, database.Session(), orgID, dbpkg.PlainBlockRepresentationID, externalID)
	if err != nil || !found || !frozen || mapped != internalID {
		t.Fatalf("projection for %s = %q frozen=%t found=%t err=%v; want frozen %s", externalID, mapped, frozen, found, err, internalID)
	}
}

// seedMappingBlockInRepresentation installs the canonical block row for
// content in an explicit representation and writes its exact bytes.
func seedMappingBlockInRepresentation(t *testing.T, ctx context.Context, database *dbpkg.DB, blockStore *storage.BlockStore, orgID, representationID string, content mappingAuthorityContent) {
	t.Helper()
	storageKey, err := blockStore.MintStorageKey(content.internal)
	if err != nil {
		t.Fatalf("mint representation block key: %v", err)
	}
	if _, err := blockStore.PutObjectAutoDirect(ctx, storageKey, content.bytes); err != nil {
		t.Fatalf("store representation block bytes: %v", err)
	}
	now := time.Now().UTC()
	w2PostHeadRetryEachQuorum(t, "seed representation block", func() error {
		return database.Session().Query(`
			INSERT INTO blocks (org_id, block_id, representation_id, sha1, size_bytes, storage_class, storage_key, gc_state, gc_claim_id, created_at, last_accessed)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, orgID, content.internal, representationID, content.external, int64(len(content.bytes)), "evidence", storageKey, "", "", now, now).Consistency(gocql.EachQuorum).Exec()
	})
}

func updateMutableBlockMapping(t *testing.T, ctx context.Context, database *dbpkg.DB, orgID, externalID, internalID string) {
	t.Helper()
	if err := database.Session().Query(`UPDATE block_id_mappings SET internal_id = ? WHERE org_id = ? AND representation_id = ? AND external_id = ?`,
		internalID, orgID, dbpkg.PlainBlockRepresentationID, externalID).WithContext(ctx).Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("ordinary mapping write %s -> %s: %v", externalID, internalID, err)
	}
}

// TestBlockMappingAuthorityCertifierRealCassandra proves the Mapping Authority
// end to end against real Cassandra and MinIO:
//   - Convergence without provenance (M19) and cross-representation evidence
//     are not promoted.
//   - A provable mapping is claimed, its ordinary projection is frozen, and the
//     library certifies.
//   - Every later, racing, delayed or deleting ordinary write is inert (M18),
//     including one landing after the final recheck and before the witness CAS.
//   - A claim whose projection diverged before it could be frozen fails closed
//     without being repaired.
//   - Unsupported claim evidence is unusable.
//   - A consumed claim whose canonical block moved to another representation is
//     refused.
func TestBlockMappingAuthorityCertifierRealCassandra(t *testing.T) {
	database := shareProjectionDBForTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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

	t.Run("cross-representation evidence is not promoted", func(t *testing.T) {
		foreign := newMappingAuthorityContent("foreign representation " + orgID)
		seedMappingBlockInRepresentation(t, ctx, database, blockStore, orgID, dbpkg.EncryptedLibraryBlockRepresentationID(uuid.NewString()), foreign)
		writeMutableBlockMapping(t, database, orgID, foreign.external, foreign.internal)
		promotion, err := database.PromoteBlockMappingAuthority(ctx, storageManager, orgID, dbpkg.PlainBlockRepresentationID, foreign.external)
		if promotion.Outcome != dbpkg.BlockMappingAuthorityUnproven {
			t.Fatalf("hash-valid bytes from another representation were promoted: %+v, %v", promotion, err)
		}
		requireNoMappingAuthority(t, ctx, database, orgID, foreign.external)
	})

	writeMutableBlockMapping(t, database, orgID, legacy.external, legacy.internal)
	firstLibrary, firstHead, firstFile := seedSHA1OnlyMappingLibrary(t, database, orgID, "proven", legacy)
	t.Run("provable mapping is promoted, frozen and certifies", func(t *testing.T) {
		result := database.CertifyLibraryBaseline(ctx, storageManager, orgID, firstLibrary, firstHead)
		if result.Outcome != dbpkg.LibraryBaselineCertificationCertified || result.Reason != dbpkg.LibraryBaselineReasonApplied ||
			result.UniqueBlocks != 1 || result.PermanentLivenessWrites != 1 || result.PhysicalRevalidations != 2 {
			t.Fatalf("SHA1-only with proven mapping = %+v; want CERTIFIED with one canonical dependency", result)
		}
		requireMappingAuthority(t, ctx, database, orgID, legacy.external, legacy.internal)
		requireFrozenProjection(t, ctx, database, orgID, legacy.external, legacy.internal)
		requireBaselineWitness(t, database, orgID, firstLibrary, firstHead)
		if !libraryReferenceExists(t, database, orgID, legacy.internal, firstLibrary, firstFile) {
			t.Fatal("certified SHA1-only dependency has no permanent liveness on its authoritative canonical block")
		}
	})

	t.Run("ordinary writes after promotion are inert", func(t *testing.T) {
		writeMutableBlockMapping(t, database, orgID, legacy.external, other.internal)
		requireFrozenProjection(t, ctx, database, orgID, legacy.external, legacy.internal)
		// A pre-fence mutation delivered late keeps its original, older timestamp.
		stale := time.Now().Add(-time.Second).UnixMicro()
		if err := database.Session().Query(`UPDATE block_id_mappings USING TIMESTAMP ? SET internal_id = ? WHERE org_id = ? AND representation_id = ? AND external_id = ?`,
			stale, other.internal, orgID, dbpkg.PlainBlockRepresentationID, legacy.external).WithContext(ctx).Consistency(gocql.EachQuorum).Exec(); err != nil {
			t.Fatalf("deliver late pre-fence mutation: %v", err)
		}
		requireFrozenProjection(t, ctx, database, orgID, legacy.external, legacy.internal)
		if err := database.Session().Query(`DELETE FROM block_id_mappings WHERE org_id = ? AND representation_id = ? AND external_id = ?`,
			orgID, dbpkg.PlainBlockRepresentationID, legacy.external).WithContext(ctx).Consistency(gocql.EachQuorum).Exec(); err != nil {
			t.Fatalf("ordinary mapping delete: %v", err)
		}
		requireFrozenProjection(t, ctx, database, orgID, legacy.external, legacy.internal)

		libraryID, head, fileFSID := seedSHA1OnlyMappingLibrary(t, database, orgID, "after-freeze", legacy)
		result := database.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
		if result.Outcome != dbpkg.LibraryBaselineCertificationCertified || !libraryReferenceExists(t, database, orgID, legacy.internal, libraryID, fileFSID) ||
			libraryReferenceExists(t, database, orgID, other.internal, libraryID, fileFSID) {
			t.Fatalf("certification after inert ordinary writes = %+v; want CERTIFIED resolving only A", result)
		}
		requireMappingAuthority(t, ctx, database, orgID, legacy.external, legacy.internal)
	})

	t.Run("ordinary write racing certification is inert", func(t *testing.T) {
		libraryID, head, _ := seedSHA1OnlyMappingLibrary(t, database, orgID, "race", legacy)
		raced := false
		result := database.CertifyLibraryBaselineWithIntegrationHooks(ctx, storageManager, orgID, libraryID, head, dbpkg.LibraryBaselineCertifierIntegrationHooks{
			AfterLiveness: func(hookCtx context.Context, _, blockID string, _ dbpkg.BlockPhysicalLocation) {
				if blockID != legacy.internal {
					t.Fatalf("race hook saw block %s, want authority %s", blockID, legacy.internal)
				}
				updateMutableBlockMapping(t, hookCtx, database, orgID, legacy.external, other.internal)
				raced = true
			},
		})
		if !raced || result.Outcome != dbpkg.LibraryBaselineCertificationCertified {
			t.Fatalf("ordinary write during certification = raced:%t %+v; want CERTIFIED with the write inert", raced, result)
		}
		requireFrozenProjection(t, ctx, database, orgID, legacy.external, legacy.internal)
		requireBaselineWitness(t, database, orgID, libraryID, head)
	})

	t.Run("ordinary write between final recheck and witness CAS is inert", func(t *testing.T) {
		libraryID, head, _ := seedSHA1OnlyMappingLibrary(t, database, orgID, "before-cas", legacy)
		raced := false
		result := database.CertifyLibraryBaselineWithIntegrationHooks(ctx, storageManager, orgID, libraryID, head, dbpkg.LibraryBaselineCertifierIntegrationHooks{
			BeforeWitnessCAS: func(hookCtx context.Context, _, _, _ string) {
				updateMutableBlockMapping(t, hookCtx, database, orgID, legacy.external, other.internal)
				raced = true
			},
		})
		if !raced || result.Outcome != dbpkg.LibraryBaselineCertificationCertified {
			t.Fatalf("ordinary write before witness CAS = raced:%t %+v", raced, result)
		}
		// The witness exists, so the invariant is that readers still resolve A.
		requireBaselineWitness(t, database, orgID, libraryID, head)
		requireFrozenProjection(t, ctx, database, orgID, legacy.external, legacy.internal)
	})

	t.Run("projection diverged before freeze fails closed without repair", func(t *testing.T) {
		stranded := newMappingAuthorityContent("stranded " + orgID)
		storeMappingAuthorityBlock(t, ctx, database, blockStore, orgID, stranded)
		// The claim committed, then the promotion stopped before freezing and a
		// stale ordinary writer landed B.
		if outcome, _, err := dbpkg.ClaimBlockMappingAuthorityForIntegration(ctx, database.Session(), orgID, dbpkg.PlainBlockRepresentationID, stranded.external, stranded.internal, dbpkg.BlockMappingEvidencePhysicalBytesV1); err != nil || outcome != dbpkg.IdentityClaimEstablished {
			t.Fatalf("establish unfrozen claim: %s, %v", outcome, err)
		}
		writeMutableBlockMapping(t, database, orgID, stranded.external, other.internal)
		libraryID, head, fileFSID := seedSHA1OnlyMappingLibrary(t, database, orgID, "stranded", stranded)
		result := database.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
		if result.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || result.Reason != dbpkg.LibraryBaselineReasonIdentityConflict ||
			result.PermanentLivenessWrites != 0 || result.PhysicalRevalidations != 0 {
			t.Fatalf("claim A with unfrozen projection B = %+v; want NOT_CERTIFIED/identity_conflict before physical or liveness work", result)
		}
		if libraryReferenceExists(t, database, orgID, other.internal, libraryID, fileFSID) {
			t.Fatal("certifier consumed the diverged mutable mapping B")
		}
		mapped, frozen, found, err := dbpkg.ReadBlockMappingProjection(ctx, database.Session(), orgID, dbpkg.PlainBlockRepresentationID, stranded.external)
		if err != nil || !found || frozen || mapped != other.internal {
			t.Fatalf("diverged projection was repaired or frozen: %q frozen=%t found=%t err=%v", mapped, frozen, found, err)
		}
		requireMappingAuthority(t, ctx, database, orgID, stranded.external, stranded.internal)
		requireNoBaselineWitness(t, database, orgID, libraryID)
	})

	t.Run("unsupported claim evidence is unusable", func(t *testing.T) {
		unsupported := newMappingAuthorityContent("unsupported evidence " + orgID)
		storeMappingAuthorityBlock(t, ctx, database, blockStore, orgID, unsupported)
		writeMutableBlockMapping(t, database, orgID, unsupported.external, unsupported.internal)
		if outcome, _, err := dbpkg.ClaimBlockMappingAuthorityForIntegration(ctx, database.Session(), orgID, dbpkg.PlainBlockRepresentationID, unsupported.external, unsupported.internal, dbpkg.BlockMappingEvidenceIntegrationInjected); err != nil || outcome != dbpkg.IdentityClaimEstablished {
			t.Fatalf("establish unsupported-evidence claim: %s, %v", outcome, err)
		}
		libraryID, head, _ := seedSHA1OnlyMappingLibrary(t, database, orgID, "unsupported", unsupported)
		result := database.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
		if result.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || result.Reason != dbpkg.LibraryBaselineReasonIdentityConflict || result.PermanentLivenessWrites != 0 {
			t.Fatalf("claim with unsupported evidence = %+v; want NOT_CERTIFIED/identity_conflict", result)
		}
		if _, frozen, _, err := dbpkg.ReadBlockMappingProjection(ctx, database.Session(), orgID, dbpkg.PlainBlockRepresentationID, unsupported.external); err != nil || frozen {
			t.Fatalf("unusable claim froze the projection: frozen=%t err=%v", frozen, err)
		}
		requireNoBaselineWitness(t, database, orgID, libraryID)
	})

	t.Run("consumed claim whose block moved representation is refused", func(t *testing.T) {
		moved := dbpkg.EncryptedLibraryBlockRepresentationID(uuid.NewString())
		setRepresentation := func(representationID string) {
			if err := database.Session().Query(`UPDATE blocks SET representation_id = ? WHERE org_id = ? AND block_id = ?`,
				representationID, orgID, legacy.internal).WithContext(ctx).Consistency(gocql.EachQuorum).Exec(); err != nil {
				t.Fatalf("set canonical block representation: %v", err)
			}
		}
		setRepresentation(moved)
		defer setRepresentation(dbpkg.PlainBlockRepresentationID)
		libraryID, head, _ := seedSHA1OnlyMappingLibrary(t, database, orgID, "moved", legacy)
		result := database.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
		if result.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || result.Reason != dbpkg.LibraryBaselineReasonIdentityConflict ||
			result.PermanentLivenessWrites != 0 || result.PhysicalRevalidations != 0 {
			t.Fatalf("claim over a block now in %s = %+v; want NOT_CERTIFIED/identity_conflict", moved, result)
		}
		requireNoBaselineWitness(t, database, orgID, libraryID)
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
