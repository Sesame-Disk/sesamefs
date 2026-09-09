package db

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// PC-0 source contracts freeze selected publication-protocol facts reconstructed
// in docs/PUBLICATION-PROTOCOL-CHARACTERIZATION.md. They are characterization
// guards: a new productive HEAD publisher that lexically calls a named HEAD
// helper, a missing funnel seam, or a silent token change at a named primitive
// must turn red. They do not inventory every callsite shape, do not freeze
// the full consistency map, do not implement PublicationCoordinator, and do
// not change production behavior.

type pc0HeadClass string

const (
	pc0HeadBlockPublication pc0HeadClass = "block-publication"
	pc0HeadTreeMutation     pc0HeadClass = "tree-mutation"
	pc0HeadPrimitive        pc0HeadClass = "head-primitive"
)

type pc0HeadCaller struct {
	path     string
	function string
	class    pc0HeadClass
}

// Every production function that calls UpdateLibraryHeadFromSnapshot,
// updateLibraryHeadWithStats, or UpdateLibraryHead must appear here. Tree
// mutations are included so a new block publisher cannot hide as an unlisted
// directory rename.
var pc0ExpectedHeadCallers = []pc0HeadCaller{
	{path: "internal/api/v2/files.go", function: "CreateFile", class: pc0HeadBlockPublication},
	{path: "internal/api/v2/files.go", function: "finalizeStoredUploadMetadataOnce", class: pc0HeadBlockPublication},
	{path: "internal/api/v2/batch_operations.go", function: "processSingleItem", class: pc0HeadBlockPublication},
	{path: "internal/api/v2/onlyoffice.go", function: "publishEditedDocumentMetadata", class: pc0HeadBlockPublication},
	{path: "internal/api/seafhttp.go", function: "commitUploadedFileOnce", class: pc0HeadBlockPublication},
	{path: "internal/api/seafhttp.go", function: "commitUploadedFileMultiBlockOnce", class: pc0HeadBlockPublication},
	{path: "internal/api/sync.go", function: "handleSyncHeadPromotion", class: pc0HeadBlockPublication},
	{path: "internal/api/sync.go", function: "tryAutoMergeSyncHeadPromotion", class: pc0HeadBlockPublication},
	{path: "internal/api/v2/files.go", function: "CreateDirectory", class: pc0HeadTreeMutation},
	{path: "internal/api/v2/files.go", function: "RenameDirectory", class: pc0HeadTreeMutation},
	{path: "internal/api/v2/files.go", function: "RenameFile", class: pc0HeadTreeMutation},
	{path: "internal/api/v2/files.go", function: "DeleteDirectory", class: pc0HeadTreeMutation},
	{path: "internal/api/v2/files.go", function: "DeleteFile", class: pc0HeadTreeMutation},
	{path: "internal/api/v2/files.go", function: "BatchDeleteItems", class: pc0HeadTreeMutation},
	{path: "internal/api/v2/files.go", function: "copyItemWithinRepoWithRetry", class: pc0HeadTreeMutation},
	{path: "internal/api/v2/batch_operations.go", function: "processSameRepoMove", class: pc0HeadTreeMutation},
	{path: "internal/api/v2/files.go", function: "RevertFile", class: pc0HeadTreeMutation},
	{path: "internal/api/v2/files.go", function: "RevertDirectory", class: pc0HeadTreeMutation},
	{path: "internal/api/v2/trash.go", function: "RestoreTrashItem", class: pc0HeadTreeMutation},
	{path: "internal/api/v2/trash.go", function: "RevertDirents", class: pc0HeadTreeMutation},
	{path: "internal/api/v2/fs_helpers.go", function: "UpdateLibraryHeadFromSnapshot", class: pc0HeadPrimitive},
}

type pc0FunnelSeams struct {
	label      string
	function   string
	prepare    []string
	stage      []string
	head       string
	settlement []string
}

var pc0BlockPublicationFunnels = []pc0FunnelSeams{
	{
		label:      "v2/CreateFile",
		function:   "CreateFile",
		prepare:    []string{"RegisterUploadedBlockTargetAndMapping", "prepareFileFSObjectForPublish"},
		stage:      []string{"stagePendingPublishedFiles"},
		head:       "UpdateLibraryHeadFromSnapshot",
		settlement: []string{"promotePendingPublishedFiles", "CleanupFailedPublishAttempt"},
	},
	{
		label:      "v2/finalizeStoredUploadMetadataOnce",
		function:   "finalizeStoredUploadMetadataOnce",
		prepare:    []string{"newPendingPublishedFile"},
		stage:      []string{"stagePendingPublishedFiles"},
		head:       "UpdateLibraryHeadFromSnapshot",
		settlement: []string{"promotePendingPublishedFiles", "CleanupFailedPublishAttempt"},
	},
	{
		label:      "v2/processSingleItem",
		function:   "processSingleItem",
		prepare:    []string{"copyFSObjectToLibraryForPublish"},
		stage:      []string{"stagePendingPublishedFiles"},
		head:       "UpdateLibraryHeadFromSnapshot",
		settlement: []string{"promotePendingPublishedFiles", "CleanupFailedPublishAttempt"},
	},
	{
		label:      "v2/publishEditedDocumentMetadata",
		function:   "publishEditedDocumentMetadata",
		prepare:    []string{"prepareFileFSObjectForPublish"},
		stage:      []string{"stagePendingPublishedFiles"},
		head:       "UpdateLibraryHeadFromSnapshot",
		settlement: []string{"promotePendingPublishedFiles"},
	},
	{
		label:      "seafhttp/commitUploadedFileOnce",
		function:   "commitUploadedFileOnce",
		prepare:    []string{"stageSeafHTTPPublishAttemptReferences"},
		stage:      []string{"stageSeafHTTPPublishAttemptReferences"},
		head:       "UpdateLibraryHeadFromSnapshot",
		settlement: []string{"finalizeSeafHTTPPublishedBlockReferences"},
	},
	{
		label:      "seafhttp/commitUploadedFileMultiBlockOnce",
		function:   "commitUploadedFileMultiBlockOnce",
		prepare:    []string{"stageSeafHTTPPublishAttemptReferences"},
		stage:      []string{"stageSeafHTTPPublishAttemptReferences"},
		head:       "UpdateLibraryHeadFromSnapshot",
		settlement: []string{"finalizeSeafHTTPPublishedBlockReferences"},
	},
	{
		label:      "sync/handleSyncHeadPromotion",
		function:   "handleSyncHeadPromotion",
		prepare:    []string{"ensureSyncCommitBlockPublicationReadiness"},
		stage:      []string{"stageSyncCommitBlockDelta"},
		head:       "updateLibraryHeadWithStats",
		settlement: []string{"finalizeSyncCommitBlockDeltaAndSettleRepairIntent"},
	},
	{
		label:      "sync/tryAutoMergeSyncHeadPromotion",
		function:   "tryAutoMergeSyncHeadPromotion",
		prepare:    []string{"ensureAndQueueAutoMergeSyncPublication"},
		stage:      []string{"stageSyncCommitBlockDelta"},
		head:       "updateLibraryHeadWithStats",
		settlement: []string{"finalizeSyncCommitBlockDeltaAndSettleRepairIntent"},
	},
}

type pc0WrapperAlias struct {
	wrapper string
	callee  string
	path    string
}

var pc0PublicationWrappers = []pc0WrapperAlias{
	{wrapper: "CreateFileFromBlocks", callee: "finalizeStoredUploadMetadata", path: "internal/api/v2/file_from_blocks.go"},
	{wrapper: "finalizeStoredUploadMetadata", callee: "finalizeStoredUploadMetadataOnce", path: "internal/api/v2/files.go"},
	{wrapper: "UploadFile", callee: "finalizeStoredUploadMetadata", path: "internal/api/v2/files.go"},
	{wrapper: "commitUploadedFile", callee: "commitUploadedFileOnce", path: "internal/api/seafhttp.go"},
	{wrapper: "commitUploadedFileMultiBlock", callee: "commitUploadedFileMultiBlockOnce", path: "internal/api/seafhttp.go"},
}

type pc0ConsistencyPin struct {
	path       string
	function   string
	needle     string
	observed   string
	notNeedle  string
	notMessage string
}

var pc0ConsistencyPins = []pc0ConsistencyPin{
	{
		path:     "internal/db/block_references.go",
		function: "BlockReferenceExistsLocalQuorum",
		needle:   "Consistency(gocql.LocalQuorum)",
		observed: "LOCAL_QUORUM presence read; miss is not global absence",
	},
	{
		path:     "internal/db/block_references.go",
		function: "ValidateBorrowedFSPublicationAuthority",
		needle:   "BlockAuthorityAdvisory",
		observed: "exact-P fence stays advisory/LQ on the publish path",
	},
	{
		path:      "internal/db/block_references.go",
		function:  "ValidateBorrowedFSPublicationAuthority",
		needle:    "BlockAuthorityAdvisory",
		notNeedle: "BlockAuthorityStrong",
		observed:  "BorrowedFS publication authority must not switch to SERIAL",
	},
	{
		path:     "internal/api/v2/fs_helpers.go",
		function: "UpdateLibraryHead",
		needle:   "IF head_commit_id = ?",
		observed: "HEAD remains a conditional LWT; this pin does not freeze SERIAL vs LOCAL_SERIAL",
	},
	{
		path:     "internal/api/v2/fs_helpers.go",
		function: "confirmLibraryHeadCommitVisible",
		needle:   "Consistency(gocql.Serial)",
		observed: "v2 ambiguous-CAS confirm is a SERIAL read",
	},
	{
		path:     "internal/api/sync.go",
		function: "updateLibraryHeadWithStats",
		needle:   "IF head_commit_id = ?",
		observed: "Sync HEAD is the same LWT shape",
	},
	{
		path:     "internal/api/v2/publish_repair.go",
		function: "publishedBlockReferenceRepairHeadCommitFn",
		needle:   "Consistency(gocql.Serial)",
		observed: "repair HEAD lookup is SERIAL",
	},
	{
		path:     "internal/api/v2/publish_repair.go",
		function: "publishedBlockReferenceRepairCommitParentFn",
		needle:   "Consistency(gocql.EachQuorum)",
		observed: "repair parent walk is EACH_QUORUM",
	},
}

func pc0HeadCallName(name string) bool {
	switch name {
	case "UpdateLibraryHeadFromSnapshot", "updateLibraryHeadWithStats", "UpdateLibraryHead":
		return true
	default:
		return false
	}
}

func pc0CallerKey(path, function string) string {
	return filepath.ToSlash(path) + ":" + function
}

func pc0ParseProductionFuncs(t *testing.T) map[string]*ast.FuncDecl {
	t.Helper()
	root := r3RepositoryRoot(t)
	functions := make(map[string]*ast.FuncDecl)
	for _, rel := range []string{
		filepath.Join("internal", "api"),
		filepath.Join("internal", "api", "v2"),
	} {
		dir := filepath.Join(root, rel)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("PC0 INVENTORY: read %s: %v", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			relPath := filepath.ToSlash(filepath.Join(rel, entry.Name()))
			file := r3ParseProductionFile(t, filepath.Join(root, filepath.FromSlash(relPath)))
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				functions[pc0CallerKey(relPath, fn.Name.Name)] = fn
			}
		}
	}
	return functions
}

func pc0ExpectedCaller(path, function string) (pc0HeadCaller, bool) {
	for _, caller := range pc0ExpectedHeadCallers {
		if caller.path == path && caller.function == function {
			return caller, true
		}
	}
	return pc0HeadCaller{}, false
}

func pc0FunctionByName(functions map[string]*ast.FuncDecl, function string) *ast.FuncDecl {
	var found *ast.FuncDecl
	for key, fn := range functions {
		if strings.HasSuffix(key, ":"+function) {
			if found != nil {
				return nil
			}
			found = fn
		}
	}
	return found
}

func pc0FunctionCallsNamed(fn *ast.FuncDecl, names ...string) map[string]bool {
	want := make(map[string]bool, len(names))
	for _, name := range names {
		want[name] = false
	}
	if fn == nil || fn.Body == nil {
		return want
	}
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := r3PublicationCallName(call)
		if _, exists := want[name]; exists {
			want[name] = true
		}
		return true
	})
	return want
}

func pc0FunctionSource(t *testing.T, path, function string) string {
	t.Helper()
	root := r3RepositoryRoot(t)
	full := filepath.Join(root, filepath.FromSlash(path))
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, full, nil, 0)
	if err != nil {
		t.Fatalf("PC0 CONSISTENCY: parse %s: %v", path, err)
	}
	src, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("PC0 CONSISTENCY: read %s: %v", path, err)
	}
	var target ast.Node
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == function {
			target = fn
			break
		}
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for position, name := range value.Names {
				if name.Name != function || position >= len(value.Values) {
					continue
				}
				target = value.Values[position]
			}
		}
	}
	if target == nil {
		t.Fatalf("PC0 CONSISTENCY: function or var %s not found in %s", function, path)
	}
	start := int(target.Pos()) - int(file.Pos())
	end := int(target.End()) - int(file.Pos())
	if start < 0 || end > len(src) || start >= end {
		t.Fatalf("PC0 CONSISTENCY: %s source range out of bounds in %s", function, path)
	}
	return string(src[start:end])
}

// TestPC0AllHeadCallersAreInventoried matches production functions that
// lexically invoke UpdateLibraryHeadFromSnapshot, updateLibraryHeadWithStats,
// or UpdateLibraryHead by those names. It does not walk method values, aliased
// callees, or a second HEAD branch inside an already-listed function. A new
// wrapper that hides the call still needs an explicit inventory row.
func TestPC0AllHeadCallersAreInventoried(t *testing.T) {
	functions := pc0ParseProductionFuncs(t)
	found := map[string]bool{}
	for key, fn := range functions {
		if fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if pc0HeadCallName(r3PublicationCallName(call)) {
				found[key] = true
			}
			return true
		})
	}

	var unexpected []string
	for key := range found {
		path, function, ok := strings.Cut(key, ":")
		if !ok {
			unexpected = append(unexpected, key)
			continue
		}
		if _, listed := pc0ExpectedCaller(path, function); !listed {
			unexpected = append(unexpected, key)
		}
	}
	sort.Strings(unexpected)
	if len(unexpected) > 0 {
		t.Fatalf("PC0 INVENTORY: unlisted lexical HEAD callers %v; classify them in pc0ExpectedHeadCallers and docs/PUBLICATION-PROTOCOL-CHARACTERIZATION.md (this guard does not see method values or aliased callees)", unexpected)
	}

	var missing []string
	for _, expected := range pc0ExpectedHeadCallers {
		key := pc0CallerKey(expected.path, expected.function)
		fn := functions[key]
		if fn == nil {
			missing = append(missing, key)
			continue
		}
		if expected.class != pc0HeadPrimitive && !found[key] {
			missing = append(missing, key+" (listed but no HEAD call)")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("PC0 INVENTORY: listed HEAD callers missing from production: %v", missing)
	}
}

func TestPC0BlockPublicationFunnelsHaveMappedSeams(t *testing.T) {
	functions := pc0ParseProductionFuncs(t)
	listedPublication := map[string]bool{}
	for _, caller := range pc0ExpectedHeadCallers {
		if caller.class == pc0HeadBlockPublication {
			listedPublication[caller.function] = true
		}
	}
	for _, funnel := range pc0BlockPublicationFunnels {
		fn := pc0FunctionByName(functions, funnel.function)
		if fn == nil {
			t.Fatalf("PC0 FUNNEL MAP: %s function %s not found uniquely", funnel.label, funnel.function)
		}
		if !listedPublication[funnel.function] {
			t.Fatalf("PC0 FUNNEL MAP: %s is mapped as a publication funnel but not inventoried as block-publication", funnel.label)
		}
		need := append([]string{}, funnel.prepare...)
		need = append(need, funnel.stage...)
		need = append(need, funnel.head)
		need = append(need, funnel.settlement...)
		got := pc0FunctionCallsNamed(fn, need...)
		var missing []string
		for _, name := range need {
			if !got[name] {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			t.Fatalf("PC0 FUNNEL MAP: %s missing seam calls %v", funnel.label, missing)
		}
	}

	var unmapped []string
	for _, caller := range pc0ExpectedHeadCallers {
		if caller.class != pc0HeadBlockPublication {
			continue
		}
		mapped := false
		for _, funnel := range pc0BlockPublicationFunnels {
			if funnel.function == caller.function {
				mapped = true
				break
			}
		}
		if !mapped {
			unmapped = append(unmapped, caller.function)
		}
	}
	sort.Strings(unmapped)
	if len(unmapped) > 0 {
		t.Fatalf("PC0 FUNNEL MAP: block-publication callers have no mapping: %v", unmapped)
	}
}

func TestPC0R3StageToHeadInventoryIsSubset(t *testing.T) {
	mapped := make(map[string]bool, len(pc0BlockPublicationFunnels))
	for _, funnel := range pc0BlockPublicationFunnels {
		mapped[funnel.label] = true
	}
	if len(r3PublicationStageToHeadBoundaries) == 0 {
		t.Fatal("PC0 FUNNEL MAP: live R3 stage-to-HEAD inventory is empty")
	}
	for _, boundary := range r3PublicationStageToHeadBoundaries {
		if !mapped[boundary.label] {
			t.Fatalf("PC0 FUNNEL MAP: R3 stage-to-HEAD label %s is not in the PC-0 mapping", boundary.label)
		}
	}
}

func pc0FirstNamedCallPos(fn *ast.FuncDecl, name string) token.Pos {
	calls := r3NamedCalls(fn, name)
	if len(calls) == 0 {
		return token.NoPos
	}
	earliest := calls[0].Pos()
	for _, call := range calls[1:] {
		if call.Pos() < earliest {
			earliest = call.Pos()
		}
	}
	return earliest
}

// TestPC0ObservedRepairReadinessPartialOrder freezes today's per-funnel
// order. It does not authorize a coordinator to pick one universal
// readiness→repair sequence. Both repair and readiness (when present) occur
// after stage and before HEAD; their relative order still differs.
func TestPC0ObservedRepairReadinessPartialOrder(t *testing.T) {
	functions := pc0ParseProductionFuncs(t)

	cffb := pc0FunctionByName(functions, "finalizeStoredUploadMetadataOnce")
	repairPos := pc0FirstNamedCallPos(cffb, "queuePendingPublishedFileRepairs")
	fencePos := pc0FirstNamedCallPos(cffb, "validateCommitBlockPublicationFences")
	headPos := pc0FirstNamedCallPos(cffb, "UpdateLibraryHeadFromSnapshot")
	if repairPos == token.NoPos || fencePos == token.NoPos || headPos == token.NoPos {
		t.Fatal("PC0 ORDER: finalizeStoredUploadMetadataOnce lost repair, fence, or HEAD")
	}
	if !(repairPos < fencePos && fencePos < headPos) {
		t.Fatalf("PC0 ORDER: CreateFileFromBlocks/shared Once observed order is stage/repair then fence then HEAD, not a universal readiness-before-repair spine")
	}

	syncDirect := pc0FunctionByName(functions, "handleSyncHeadPromotion")
	readinessPos := pc0FirstNamedCallPos(syncDirect, "ensureSyncCommitBlockPublicationReadiness")
	syncRepairPos := pc0FirstNamedCallPos(syncDirect, "queueSyncCommitBlockReferenceRepairsFn")
	syncHeadPos := pc0FirstNamedCallPos(syncDirect, "updateLibraryHeadWithStats")
	if readinessPos == token.NoPos || syncRepairPos == token.NoPos || syncHeadPos == token.NoPos {
		t.Fatal("PC0 ORDER: handleSyncHeadPromotion lost readiness, repair, or HEAD")
	}
	if !(readinessPos < syncRepairPos && syncRepairPos < syncHeadPos) {
		t.Fatalf("PC0 ORDER: Sync direct HEAD observed order is stage then readiness then repair then HEAD")
	}

	autoMergeHelper := pc0FunctionByName(functions, "ensureAndQueueAutoMergeSyncPublication")
	autoReady := pc0FirstNamedCallPos(autoMergeHelper, "ensureSyncCommitBlockPublicationReadiness")
	autoRepair := pc0FirstNamedCallPos(autoMergeHelper, "queueSyncCommitBlockReferenceRepairsFn")
	if autoReady == token.NoPos || autoRepair == token.NoPos || !(autoReady < autoRepair) {
		t.Fatalf("PC0 ORDER: auto-merge helper must keep readiness before repair queue")
	}
}

func TestPC0PublicationWrappersRemainAliases(t *testing.T) {
	root := r3RepositoryRoot(t)
	for _, wrapper := range pc0PublicationWrappers {
		file := r3ParseProductionFile(t, filepath.Join(root, filepath.FromSlash(wrapper.path)))
		fn := r3FindProductionFunction(t, file, wrapper.wrapper)
		if len(r3NamedCalls(fn, wrapper.callee)) == 0 {
			t.Fatalf("PC0 WRAPPER: %s no longer calls %s; update the PC-0 inventory", wrapper.wrapper, wrapper.callee)
		}
		if wrapper.wrapper == "CreateFileFromBlocks" {
			if len(r3NamedCalls(fn, "UpdateLibraryHeadFromSnapshot")) != 0 {
				t.Fatalf("PC0 WRAPPER: CreateFileFromBlocks must keep delegating HEAD through finalizeStoredUploadMetadata")
			}
		}
	}
}

// TestPC0CriticalConsistencyPrimitivesArePinned pins selected source tokens
// at named primitives. It does not freeze the full multi-DC consistency map.
// In particular it does not pin libraries HEAD serial_consistency
// (ISSUE-LIBRARY-HEAD-SERIAL-DOMAIN-01).
func TestPC0CriticalConsistencyPrimitivesArePinned(t *testing.T) {
	for _, pin := range pc0ConsistencyPins {
		src := pc0FunctionSource(t, pin.path, pin.function)
		if !strings.Contains(src, pin.needle) {
			t.Fatalf("PC0 CONSISTENCY: %s in %s lost %q (%s)", pin.function, pin.path, pin.needle, pin.observed)
		}
		if pin.notNeedle != "" && strings.Contains(src, pin.notNeedle) {
			t.Fatalf("PC0 CONSISTENCY: %s in %s contains %q (%s)", pin.function, pin.path, pin.notNeedle, pin.notMessage)
		}
	}
}

func TestPC0PublicationCoordinatorTypeIsNotImplemented(t *testing.T) {
	root := r3RepositoryRoot(t)
	var hits []string
	for _, rel := range []string{
		filepath.Join("internal", "api"),
		filepath.Join("internal", "api", "v2"),
		filepath.Join("internal", "db"),
	} {
		dir := filepath.Join(root, rel)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("PC0 NO COORDINATOR: read %s: %v", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				t.Fatalf("PC0 NO COORDINATOR: read %s: %v", entry.Name(), err)
			}
			if strings.Contains(string(raw), "type PublicationCoordinator struct") {
				hits = append(hits, filepath.ToSlash(filepath.Join(rel, entry.Name())))
			}
		}
	}
	if len(hits) > 0 {
		t.Fatalf("PC0 NO COORDINATOR: productive PublicationCoordinator type found in %v; PC-0 is characterization only", hits)
	}
}

func TestPC0StoredUploadExactPFenceIsNoOpWhenCommitBlocksNil(t *testing.T) {
	root := r3RepositoryRoot(t)
	file := r3ParseProductionFile(t, filepath.Join(root, "internal", "api", "v2", "files.go"))
	upload := r3FindProductionFunction(t, file, "UploadFile")
	calls := r3NamedCalls(upload, "finalizeStoredUploadMetadata")
	if len(calls) != 1 {
		t.Fatalf("PC0 FINDING: UploadFile has %d finalizeStoredUploadMetadata calls, want 1", len(calls))
	}
	call := calls[0]
	if len(call.Args) < 9 {
		t.Fatalf("PC0 FINDING: UploadFile finalizeStoredUploadMetadata has %d args, want at least 9 so commitBlocks can be inspected", len(call.Args))
	}
	ident, ok := call.Args[8].(*ast.Ident)
	if !ok || ident.Name != "nil" {
		t.Fatalf("PC0 FINDING: UploadFile no longer passes nil commitBlocks; update ISSUE-PC0-EXACT-P-FUNNEL-GAP-01 and the characterization matrix")
	}
}
