package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLibraryBaselineCertificationLimitsValidate(t *testing.T) {
	valid := LibraryBaselineCertificationLimits{
		MaxDepth:           1,
		MaxFSObjects:       1,
		MaxTreeEdges:       1,
		MaxBlockReferences: 1,
		MaxUniqueBlocks:    1,
	}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid limits rejected: %v", err)
	}
	cases := []LibraryBaselineCertificationLimits{
		{},
		{MaxDepth: 1, MaxFSObjects: 1, MaxTreeEdges: 1, MaxBlockReferences: 1},
		{MaxDepth: -1, MaxFSObjects: 1, MaxTreeEdges: 1, MaxBlockReferences: 1},
	}
	for _, limits := range cases {
		if err := limits.validate(); err == nil {
			t.Fatalf("invalid limits %v accepted", limits)
		}
	}
}

func TestParseContinuityDirectoryEntriesRejectsMalformedAndAcceptsEmpty(t *testing.T) {
	if entries, err := parseContinuityDirectoryEntries("[]"); err != nil || len(entries) != 0 {
		t.Fatalf("empty directory = %#v, %v", entries, err)
	}
	valid := "[{\"name\":\"nested\",\"id\":\"child-1\",\"mode\":16384},{\"id\":\"child-2\"}]"
	entries, err := parseContinuityDirectoryEntries(valid)
	if err != nil {
		t.Fatalf("valid directory rejected: %v", err)
	}
	if len(entries) != 2 || entries[0].ID != "child-1" || entries[1].ID != "child-2" {
		t.Fatalf("parsed entries = %#v", entries)
	}
	for _, raw := range []string{"", "null", "{", "[{\"name\":\"missing-id\"}]", "[{\"id\":7}]", "[\"child-1\"]"} {
		if _, err := parseContinuityDirectoryEntries(raw); err == nil {
			t.Fatalf("malformed directory %q accepted", raw)
		}
	}
}

func TestContinuityStoredBlockIDsEnforcesCanonicalPairing(t *testing.T) {
	sha256ID := strings.Repeat("a", 64)
	sha1ID := strings.Repeat("b", 40)
	got, err := continuityStoredBlockIDs([]string{strings.ToUpper(sha256ID)}, []string{strings.ToUpper(sha1ID)})
	if err != nil || len(got) != 1 || got[0] != sha256ID {
		t.Fatalf("canonical pair = %#v, %v", got, err)
	}
	got, err = continuityStoredBlockIDs([]string{strings.ToUpper(sha1ID)}, nil)
	if err != nil || len(got) != 1 || got[0] != sha1ID {
		t.Fatalf("legacy external id = %#v, %v", got, err)
	}
	got, err = continuityStoredBlockIDs(nil, []string{strings.ToUpper(sha1ID)})
	if err != nil || len(got) != 1 || got[0] != sha1ID {
		t.Fatalf("external-only logical identity = %#v, %v", got, err)
	}
	for name, internalIDs := range map[string][]string{
		"canonical without external identity": {sha256ID},
		"mixed canonical and legacy ids":      {sha1ID, sha256ID},
		"invalid id":                          {"not-a-block"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := continuityStoredBlockIDs(internalIDs, nil); !errors.Is(err, errContinuityMalformedTree) {
				t.Fatalf("continuityStoredBlockIDs(%v) error = %v, want malformed tree", internalIDs, err)
			}
		})
	}
	if _, err := continuityStoredBlockIDs([]string{sha256ID}, []string{sha1ID, sha1ID}); !errors.Is(err, errContinuityMalformedTree) {
		t.Fatalf("mismatched canonical lists error = %v, want malformed tree", err)
	}
	if _, err := continuityStoredBlockIDs([]string{sha1ID}, []string{"bad"}); !errors.Is(err, errContinuityMalformedTree) {
		t.Fatalf("malformed external list error = %v, want malformed tree", err)
	}
}

func TestContinuityFSObjectFromScannedFieldsPreservesColumnPresence(t *testing.T) {
	objectType := "file"
	zero := int64(0)
	emptyIDs := []string{}
	row := continuityFSObjectFromScannedFields(&objectType, &zero, nil, &emptyIDs, &emptyIDs)
	if !row.ObjectTypePresent || !row.SizeBytesPresent || !row.BlockIDsPresent || !row.SeafileBlockIDsPresent {
		t.Fatalf("complete empty row lost field presence: %+v", row)
	}
	if row.SizeBytes != 0 || len(row.BlockIDs) != 0 {
		t.Fatalf("complete empty identity = %+v, want explicit zero size and empty block list", row)
	}
	if err := validateContinuityFileCompleteness(row, "empty-file"); err != nil {
		t.Fatalf("explicit zero size and empty block list were not treated as complete: %v", err)
	}

	partial := continuityFSObjectFromScannedFields(&objectType, nil, nil, &emptyIDs, nil)
	if !partial.ObjectTypePresent || partial.SizeBytesPresent || !partial.BlockIDsPresent || partial.SeafileBlockIDsPresent {
		t.Fatalf("partial row presence = %+v", partial)
	}
	if err := validateContinuityFileCompleteness(partial, "partial-file"); !errors.Is(err, errContinuityIncompleteFSObject) {
		t.Fatalf("NULL size_bytes was confused with a present zero: %v", err)
	}
}

func TestContinuityFileCompleteness(t *testing.T) {
	empty := continuityFSObject{
		ObjectType:        "file",
		ObjectTypePresent: true,
		SizeBytesPresent:  true,
		BlockIDs:          []string{},
		BlockIDsPresent:   true,
	}
	if err := validateContinuityFileCompleteness(empty, "empty-file"); err != nil {
		t.Fatalf("complete empty file rejected: %v", err)
	}
	blockIDs, err := continuityStoredBlockIDs(empty.BlockIDs, empty.SeafileBlockIDs)
	if err != nil || len(blockIDs) != 0 {
		t.Fatalf("complete empty file dependencies = %v, %v; want zero dependencies", blockIDs, err)
	}

	legacy := continuityFSObject{
		ObjectType:        "file",
		ObjectTypePresent: true,
		SizeBytes:         1,
		SizeBytesPresent:  true,
		BlockIDs:          []string{strings.Repeat("a", 40)},
		BlockIDsPresent:   true,
	}
	if err := validateContinuityFileCompleteness(legacy, "legacy-file"); err != nil {
		t.Fatalf("legacy file without optional SHA-1 column rejected: %v", err)
	}
	externalOnly := continuityFSObject{
		ObjectType:             "file",
		ObjectTypePresent:      true,
		SizeBytes:              1,
		SizeBytesPresent:       true,
		SeafileBlockIDs:        []string{strings.Repeat("b", 40)},
		SeafileBlockIDsPresent: true,
	}
	if err := validateContinuityFileCompleteness(externalOnly, "external-only-file"); err != nil {
		t.Fatalf("complete non-empty SHA-1 identity without physical block_ids rejected: %v", err)
	}

	for _, test := range []struct {
		name string
		row  continuityFSObject
	}{
		{
			name: "missing size_bytes",
			row: continuityFSObject{
				ObjectType: "file", ObjectTypePresent: true,
				BlockIDs: []string{}, BlockIDsPresent: true,
			},
		},
		{
			name: "missing logical block identity",
			row: continuityFSObject{
				ObjectType: "file", ObjectTypePresent: true,
				SizeBytes: 0, SizeBytesPresent: true,
			},
		},
		{
			name: "missing both file identity fields",
			row:  continuityFSObject{ObjectType: "file", ObjectTypePresent: true},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateContinuityFileCompleteness(test.row, "partial-file")
			if !errors.Is(err, errContinuityIncompleteFSObject) {
				t.Fatalf("incomplete file %s accepted: %v", test.name, err)
			}
			outcome, reason := classifyContinuityDependencyError(err)
			if outcome != LibraryBaselineCertificationNotCertified || reason != LibraryBaselineReasonIncompleteFSObject {
				t.Fatalf("incomplete file classification = %s/%s, want NOT_CERTIFIED/incomplete_fs_object", outcome, reason)
			}
		})
	}
}

func TestContinuityWalkerRequiresMappingForLegacyID(t *testing.T) {
	sha1ID := strings.Repeat("c", 40)
	walker := &continuityTreeWalker{
		ctx:            context.Background(),
		representation: PlainBlockRepresentationID,
	}
	_, err := walker.resolveBlockIDs([]string{sha1ID}, nil)
	if !errors.Is(err, errContinuityMissingBlockMapping) {
		t.Fatalf("legacy resolution error = %v, want missing mapping", err)
	}
}

func TestContinuityWalkerRejectsCycleBeforeDatabaseRead(t *testing.T) {
	walker := &continuityTreeWalker{
		ctx:     context.Background(),
		limits:  DefaultLibraryBaselineCertificationLimits,
		active:  map[string]struct{}{"root": {}},
		visited: map[string]struct{}{},
		db:      nil,
	}
	err := walker.visit("root", 0)
	if !errors.Is(err, errContinuityMalformedTree) {
		t.Fatalf("cycle error = %v, want malformed tree", err)
	}
}

func TestContinuityWalkerRejectsTraversalLimitBeforeDatabaseRead(t *testing.T) {
	walker := &continuityTreeWalker{
		ctx:     context.Background(),
		limits:  LibraryBaselineCertificationLimits{MaxDepth: 1, MaxFSObjects: 1, MaxTreeEdges: 1, MaxBlockReferences: 1, MaxUniqueBlocks: 1},
		active:  map[string]struct{}{},
		visited: map[string]struct{}{},
		db:      nil,
	}
	err := walker.visit("root", 2)
	if !errors.Is(err, errContinuityTraversalLimit) {
		t.Fatalf("depth error = %v, want traversal limit", err)
	}
}

func TestClassifyContinuityDependencyErrorFailsClosed(t *testing.T) {
	tests := []struct {
		err     error
		outcome LibraryBaselineCertificationOutcome
		reason  LibraryBaselineCertificationReason
	}{
		{errContinuityMissingFSObject, LibraryBaselineCertificationNotCertified, LibraryBaselineReasonMissingFSObject},
		{errContinuityMissingBlockMapping, LibraryBaselineCertificationNotCertified, LibraryBaselineReasonMissingBlockMapping},
		{errContinuityMalformedTree, LibraryBaselineCertificationNotCertified, LibraryBaselineReasonMalformedTree},
		{errContinuityTraversalLimit, LibraryBaselineCertificationNotCertified, LibraryBaselineReasonTraversalLimit},
		{context.DeadlineExceeded, LibraryBaselineCertificationUnknown, LibraryBaselineReasonDependencyReadFailed},
		{errors.New("cassandra timeout"), LibraryBaselineCertificationUnknown, LibraryBaselineReasonDependencyReadFailed},
	}
	for _, test := range tests {
		outcome, reason := classifyContinuityDependencyError(test.err)
		if outcome != test.outcome || reason != test.reason {
			t.Fatalf("classify(%v) = %s/%s, want %s/%s", test.err, outcome, reason, test.outcome, test.reason)
		}
	}
}

func TestValidateContinuityPhysicalAuthorityInputRejectsMalformedLocator(t *testing.T) {
	blockID := strings.Repeat("d", 64)
	if err := validateContinuityPhysicalAuthorityInput(blockID, BlockPhysicalLocation{StorageClass: "hot-v1", StorageKey: "blocks/org/key"}); err != nil {
		t.Fatalf("valid physical authority rejected: %v", err)
	}
	cases := []struct {
		blockID string
		locator BlockPhysicalLocation
	}{
		{"not-a-sha256", BlockPhysicalLocation{StorageClass: "hot-v1", StorageKey: "key"}},
		{blockID, BlockPhysicalLocation{StorageClass: " Hot", StorageKey: "key"}},
		{blockID, BlockPhysicalLocation{StorageClass: "hot-v1", StorageKey: " key"}},
	}
	for _, test := range cases {
		if err := validateContinuityPhysicalAuthorityInput(test.blockID, test.locator); err == nil {
			t.Fatalf("invalid authority accepted: %#v", test)
		}
	}
}

func TestCertificationResultFinishPreservesProofCounters(t *testing.T) {
	result := certificationResult(LibraryBaselineCertificationUnknown, LibraryBaselineReasonDependencyReadFailed, nil, "H")
	result.CommitsWalked = 1
	result.FSObjectsWalked = 3
	result.UniqueBlocks = 2
	result.PermanentLivenessWrites = 2
	result.PhysicalRevalidations = 2
	result.finish(LibraryBaselineCertificationCertified, LibraryBaselineReasonApplied, nil)
	if result.Outcome != LibraryBaselineCertificationCertified || result.Reason != LibraryBaselineReasonApplied {
		t.Fatalf("result outcome = %s/%s", result.Outcome, result.Reason)
	}
	if result.CommitsWalked != 1 || result.FSObjectsWalked != 3 || result.UniqueBlocks != 2 || result.PermanentLivenessWrites != 2 || result.PhysicalRevalidations != 2 {
		t.Fatalf("proof counters were discarded: %#v", result)
	}
}

func TestCertifierOrdersLivenessRevalidationAndWitness(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	sourceBytes, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "library_continuity_certifier.go"))
	if err != nil {
		t.Fatalf("read certifier source: %v", err)
	}
	source := string(sourceBytes)
	start := strings.Index(source, "func (db *DB) CertifyLibraryBaseline(")
	if start < 0 {
		t.Fatal("CertifyLibraryBaseline source not found")
	}
	signature := "func (db *DB) CertifyLibraryBaseline("
	body := source[start:]
	if nextFunction := strings.Index(body[len(signature):], "\nfunc "); nextFunction >= 0 {
		body = body[:len(signature)+nextFunction]
	}
	if !strings.HasPrefix(body, signature) {
		t.Fatal("CertifyLibraryBaseline function extraction is malformed")
	}
	liveness := strings.Index(body, "AddBlockReferenceContext")
	revalidation := strings.Index(body, "ValidateLibraryContinuityPhysicalAuthorityContext")
	physical := strings.Index(body, "blockStore.ObjectExists(ctx, expected.StorageKey)")
	witness := strings.Index(body, "cas, casErr := CommitLibraryContinuityWitness")
	permanent := strings.Count(body, "BlockReferencePermanentExistsEachQuorumContext")
	if witness < 0 {
		t.Fatal("certifier must execute the final witness CAS; a synthetic APPLIED result cannot authorize certification")
	}
	if !strings.Contains(body, "CommitLibraryContinuityWitnessContext(ctx, db.Session(),") {
		t.Fatal("certifier must use the context-aware witness CAS")
	}
	if physical < 0 {
		t.Fatal("certifier must prove bytes exist at the exact captured physical storage key")
	}
	if strings.Count(body, "if !permanent") != 2 {
		t.Fatalf("certifier must fail closed for both post-write and pre-witness liveness checks")
	}
	if liveness < 0 || revalidation < 0 {
		t.Fatalf("certification sequence is incomplete: liveness=%d revalidation=%d physical=%d witness=%d", liveness, revalidation, physical, witness)
	}
	if !(liveness < revalidation && revalidation < physical && physical < witness) {
		t.Fatalf("certification order is unsafe: liveness=%d revalidation=%d physical=%d witness=%d", liveness, revalidation, physical, witness)
	}
	if !strings.Contains(body, "GetBlockStoreForOrg(orgID, expected.StorageClass)") {
		t.Fatal("certifier must resolve the store from the captured physical storage class")
	}
	if !strings.Contains(body, "LibraryBaselineReasonPhysicalBytesMissing") || !strings.Contains(body, "LibraryBaselineReasonPhysicalStorageUnavailable") {
		t.Fatal("certifier must distinguish missing physical bytes from unavailable storage")
	}
	appliedGuard := strings.Index(body, "if cas.Outcome != LibraryContinuityCASApplied")
	certified := strings.Index(body, "result.finish(LibraryBaselineCertificationCertified, LibraryBaselineReasonApplied, nil)")
	if appliedGuard < 0 || certified < appliedGuard {
		t.Fatal("certifier must require the real final CAS outcome to be APPLIED before certification")
	}
	if permanent != 3 {
		t.Fatalf("certifier must prove permanent EACH_QUORUM liveness before write, after write, and before witness: occurrences=%d", permanent)
	}
	if !strings.Contains(body, "AddBlockReferenceContext(ctx, orgID, blockID, referrer, libraryID, 0)") {
		t.Fatal("certifier must establish non-expiring liveness, not a TTL pin")
	}
	if !strings.Contains(body, "LibraryBaselineReasonLivenessNotVisible") {
		t.Fatal("certifier must fail closed when permanent liveness is not EACH_QUORUM-visible")
	}
	if !strings.Contains(body, "walkContinuityTree(ctx, orgID, libraryID, representationID, rootFSID, DefaultLibraryBaselineCertificationLimits)") {
		t.Fatal("certifier must walk the complete reachable tree from the observed commit root")
	}
	if !strings.Contains(body, "ValidateMintedPhysicalLocator") || !strings.Contains(body, "ValidatePhysicalLocator") {
		t.Fatal("certifier does not explicitly reject deterministic legacy locators")
	}
	if !strings.Contains(body, "settleLibraryContinuityWitnessContext") {
		t.Fatal("certifier has no ambiguous-witness settlement path")
	}
	if !strings.Contains(body, "casErr != nil || cas.Outcome == LibraryContinuityCASUnknown") {
		t.Fatal("certifier must settle both transport errors and explicit UNKNOWN CAS outcomes")
	}
	if !strings.Contains(body, "LibraryBaselineCertificationCertified, LibraryBaselineReasonWitnessSettled") {
		t.Fatal("settled authoritative witness must be reported as certified")
	}
	if !strings.Contains(body, "case BlockRepairAuthorityAuthorized:") {
		t.Fatal("certifier must accept only an explicitly authorized physical revalidation")
	}
}

func TestCertifierUsesPresenceAwareFSObjectScan(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	sourceBytes, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "library_continuity_certifier.go"))
	if err != nil {
		t.Fatalf("read certifier source: %v", err)
	}
	source := string(sourceBytes)
	if !strings.Contains(source, "SELECT obj_type, size_bytes, dir_entries, block_ids, seafile_block_ids_sha1 FROM fs_objects") {
		t.Fatal("certifier fs_objects read must include file size and both block identity columns")
	}
	if !strings.Contains(source, ".Scan(&objectType, &sizeBytes, &directoryEntry, &blockIDs, &seafileBlockIDs)") ||
		!strings.Contains(source, "continuityFSObjectFromScannedFields(objectType, sizeBytes, directoryEntry, blockIDs, seafileBlockIDs)") {
		t.Fatal("certifier must use pointer-to-pointer scan destinations to preserve CQL NULL presence when reading fs_objects")
	}
	visitStart := strings.Index(source, "func (w *continuityTreeWalker) visit(")
	if visitStart < 0 {
		t.Fatal("continuity tree walker visit function not found")
	}
	visit := source[visitStart:]
	if nextFunction := strings.Index(visit, "\nfunc "); nextFunction >= 0 {
		visit = visit[:nextFunction]
	}
	if !strings.Contains(visit, "validateContinuityFileCompleteness(row, fsID)") {
		t.Fatal("reachable file rows must pass the completeness guard before block resolution")
	}
}
