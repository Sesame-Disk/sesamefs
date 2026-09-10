package db

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
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

var pc0BlockPublicationStageSeams = []string{
	"stagePendingPublishedFiles",
	"stageSeafHTTPPublishAttemptReferences",
	"stageSyncCommitBlockDelta",
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

// pc0ParseProductionFuncs walks internal/api recursively (this covers
// internal/api/v2 and any future subpackage placed under internal/api/) so a
// new productive HEAD publisher cannot hide from TestPC0AllHeadCallersAreInventoried
// by living in a directory this guard never lists.
func pc0ParseProductionFuncs(t *testing.T) map[string]*ast.FuncDecl {
	t.Helper()
	root := r3RepositoryRoot(t)
	apiRoot := filepath.Join(root, "internal", "api")
	functions := make(map[string]*ast.FuncDecl)
	walkErr := filepath.WalkDir(apiRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		relPath, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		relPath = filepath.ToSlash(relPath)
		file := r3ParseProductionFile(t, path)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			functions[pc0CallerKey(relPath, fn.Name.Name)] = fn
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("PC0 INVENTORY: walk %s: %v", apiRoot, walkErr)
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

func TestPC0TreeMutationsDoNotCallBlockPublicationStageSeams(t *testing.T) {
	functions := pc0ParseProductionFuncs(t)
	var violations []string
	for _, caller := range pc0ExpectedHeadCallers {
		if caller.class != pc0HeadTreeMutation {
			continue
		}
		key := pc0CallerKey(caller.path, caller.function)
		fn := functions[key]
		if fn == nil {
			t.Fatalf("PC0 CLASSIFICATION: listed tree mutation %s not found", key)
		}
		calls := pc0FunctionCallsNamed(fn, pc0BlockPublicationStageSeams...)
		for _, seam := range pc0BlockPublicationStageSeams {
			if calls[seam] {
				violations = append(violations, key+" -> "+seam)
			}
		}
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("PC0 CLASSIFICATION: tree mutation callers must not invoke block-publication stage seams; reclassify and remap them: %v", violations)
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
// order, including that stage precedes repair/readiness. It does not
// authorize a coordinator to pick one universal readiness→repair sequence.
func TestPC0ObservedRepairReadinessPartialOrder(t *testing.T) {
	functions := pc0ParseProductionFuncs(t)

	cffb := pc0FunctionByName(functions, "finalizeStoredUploadMetadataOnce")
	cffbStage := pc0FirstNamedCallPos(cffb, "stagePendingPublishedFiles")
	repairPos := pc0FirstNamedCallPos(cffb, "queuePendingPublishedFileRepairs")
	fencePos := pc0FirstNamedCallPos(cffb, "validateCommitBlockPublicationFences")
	headPos := pc0FirstNamedCallPos(cffb, "UpdateLibraryHeadFromSnapshot")
	if cffbStage == token.NoPos || repairPos == token.NoPos || fencePos == token.NoPos || headPos == token.NoPos {
		t.Fatal("PC0 ORDER: finalizeStoredUploadMetadataOnce lost stage, repair, fence, or HEAD")
	}
	if !(cffbStage < repairPos && repairPos < fencePos && fencePos < headPos) {
		t.Fatalf("PC0 ORDER: CreateFileFromBlocks/shared Once observed order is stage then repair then fence then HEAD, not a universal readiness-before-repair spine")
	}

	syncDirect := pc0FunctionByName(functions, "handleSyncHeadPromotion")
	syncStage := pc0FirstNamedCallPos(syncDirect, "stageSyncCommitBlockDelta")
	readinessPos := pc0FirstNamedCallPos(syncDirect, "ensureSyncCommitBlockPublicationReadiness")
	syncRepairPos := pc0FirstNamedCallPos(syncDirect, "queueSyncCommitBlockReferenceRepairsFn")
	syncHeadPos := pc0FirstNamedCallPos(syncDirect, "updateLibraryHeadWithStats")
	if syncStage == token.NoPos || readinessPos == token.NoPos || syncRepairPos == token.NoPos || syncHeadPos == token.NoPos {
		t.Fatal("PC0 ORDER: handleSyncHeadPromotion lost stage, readiness, repair, or HEAD")
	}
	if !(syncStage < readinessPos && readinessPos < syncRepairPos && syncRepairPos < syncHeadPos) {
		t.Fatalf("PC0 ORDER: Sync direct HEAD observed order is stage then readiness then repair then HEAD")
	}

	autoMerge := pc0FunctionByName(functions, "tryAutoMergeSyncHeadPromotion")
	autoStage := pc0FirstNamedCallPos(autoMerge, "stageSyncCommitBlockDelta")
	autoHelper := pc0FirstNamedCallPos(autoMerge, "ensureAndQueueAutoMergeSyncPublication")
	autoHead := pc0FirstNamedCallPos(autoMerge, "updateLibraryHeadWithStats")
	if autoStage == token.NoPos || autoHelper == token.NoPos || autoHead == token.NoPos {
		t.Fatal("PC0 ORDER: tryAutoMergeSyncHeadPromotion lost stage, readiness/repair helper, or HEAD")
	}
	if !(autoStage < autoHelper && autoHelper < autoHead) {
		t.Fatalf("PC0 ORDER: Sync auto-merge caller observed order is stage then readiness/repair helper then HEAD")
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

// TestPC0PublicationCoordinatorTypeIsNotImplemented walks every production
// (non-_test.go) source file under internal/ and fails if any top-level type
// declaration is *named* PublicationCoordinator, whatever its underlying
// shape (struct, interface, `type PublicationCoordinator = X` alias, or
// generic) and whatever package it lands in. It matches on the declared
// name only and does not resolve aliases, so a coordinator hidden behind
// `type X = PublicationCoordinator` (PublicationCoordinator on the RHS, a
// different name declared) would not be caught; that is out of scope for a
// characterization-only guard. A literal-string match on
// "type PublicationCoordinator struct" over three fixed directories would
// also miss an interface, a generic `PublicationCoordinator[T any]`, and
// any coordinator placed outside internal/api, internal/api/v2, and
// internal/db.
func TestPC0PublicationCoordinatorTypeIsNotImplemented(t *testing.T) {
	root := r3RepositoryRoot(t)
	internalRoot := filepath.Join(root, "internal")
	var hits []string
	walkErr := filepath.WalkDir(internalRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if perr != nil {
			t.Fatalf("PC0 NO COORDINATOR: parse %s: %v", path, perr)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || typeSpec.Name.Name != "PublicationCoordinator" {
					continue
				}
				rel, relErr := filepath.Rel(root, path)
				if relErr != nil {
					rel = path
				}
				hits = append(hits, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("PC0 NO COORDINATOR: walk %s: %v", internalRoot, walkErr)
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
