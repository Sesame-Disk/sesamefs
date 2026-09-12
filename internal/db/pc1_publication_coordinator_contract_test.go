package db

import (
	"go/ast"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// PC-1 source contracts replace PC-0's TestPC0PublicationCoordinatorTypeIsNotImplemented.
// PC-0 froze "no PublicationCoordinator exists"; PC-1 freezes the opposite
// boundary: exactly one intended PublicationCoordinator exists, in
// internal/publication, and nothing productive has adopted it. Together with
// the untouched PC-0 inventory guards (pc0_publication_inventory_contract_test.go)
// they prove that PC-1 changed no funnel: zero productive importers means zero
// new call edges, zero new CQL, and no consistency-level or TTL change can
// have entered through the coordinator. They are lexical/AST guards (no type
// checking), like the PC-0 guards they sit next to.

const (
	pc1PublicationImportPath = "github.com/Sesame-Disk/sesamefs/internal/publication"
	pc1PublicationPackageDir = "internal/publication"
	pc1CoordinatorTypeName   = "PublicationCoordinator"
	pc1CoordinatorFile       = "internal/publication/coordinator.go"
)

// pc1PublicationMethodAllowlist inventories every concrete method in the
// package, not only coordinator methods. This closes the gap where a hidden
// I/O-shaped capability could be attached to another protocol type.
var pc1PublicationMethodAllowlist = []string{
	"AttemptIdentity.Validate",
	"HeadOutcome.Valid",
	"PublicationCoordinator.ValidateSettlement",
	"SettlementDecision.AuthorizesAttemptCleanup",
	"SettlementDecision.Validate",
	"SettlementDisposition.Valid",
}

// pc1CoordinatorIdentifiers are the package identifiers whose appearance in a
// productive function body means the funnel has started to adopt the
// coordinator, whether or not the file imports the package yet.
var pc1CoordinatorIdentifiers = []string{"PublicationCoordinator", "NewPublicationCoordinator"}

// pc1AllowedPublicationImports is positive: a new standard-library import is
// rejected just like an external/storage import. PC-1 needs only pure error
// construction and formatting.
var pc1AllowedPublicationImports = map[string]bool{
	"errors": true,
	"fmt":    true,
}

// pc1AllowedPublicationPackageVars are the immutable sentinel errors that are
// permitted at package scope. Every other package-level var is rejected: a
// slice, scalar, pointer, cache, callback, or owner token can all carry
// process-local publication authority just as readily as a map or channel.
var pc1AllowedPublicationPackageVars = map[string]string{
	"ErrInvalidAttemptIdentity":       "internal/publication/attempt.go",
	"ErrInvalidHeadOutcome":           "internal/publication/head_outcome.go",
	"ErrInvalidSettlementDecision":    "internal/publication/settlement.go",
	"ErrInvalidSettlementDisposition": "internal/publication/settlement.go",
}

// pc1PublicationPackageFunctionAllowlist inventories the only package-level
// capabilities PC-1 exposes. Any addition, whatever its name, must be a
// deliberate later-PC change rather than a hidden universal sequence.
var pc1PublicationPackageFunctionAllowlist = []string{
	"NewPublicationCoordinator",
}

// pc1PublicationCallAllowlist snapshots every production call expression.
// It catches I/O slipped into an existing allowed method (for example
// fmt.Println) as well as dynamic/builtin calls. Counts make additions of an
// otherwise allowed callee deliberate too.
var pc1PublicationCallAllowlist = map[string]int{
	"AttemptID":  1,
	".Valid":     2,
	".Validate":  3,
	"errors.New": 4,
	"fmt.Errorf": 10,
}

// pc1WalkProductionFiles visits every non-_test.go source file under the given
// repository-relative roots with its slash-separated relative path.
func pc1WalkProductionFiles(t *testing.T, visit func(relPath string, file *ast.File), roots ...string) {
	t.Helper()
	repoRoot := r3RepositoryRoot(t)
	for _, root := range roots {
		walkErr := filepath.WalkDir(filepath.Join(repoRoot, root), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			relPath, relErr := filepath.Rel(repoRoot, path)
			if relErr != nil {
				return relErr
			}
			visit(filepath.ToSlash(relPath), r3ParseProductionFile(t, path))
			return nil
		})
		if walkErr != nil {
			t.Fatalf("PC1 WALK: %s: %v", root, walkErr)
		}
	}
}

func pc1IsPublicationPackageFile(relPath string) bool {
	return strings.HasPrefix(relPath, pc1PublicationPackageDir+"/")
}

func pc1ImportPaths(file *ast.File) []string {
	var paths []string
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

// TestPC1PublicationCoordinatorIsDeclaredExactlyOnce inverts PC-0's
// "not implemented" guard: one top-level type named PublicationCoordinator
// exists under internal/ and cmd/, it lives in internal/publication, and it is
// a struct with zero fields (no mutex, no in-memory ownership, no home DC).
func TestPC1PublicationCoordinatorIsDeclaredExactlyOnce(t *testing.T) {
	type hit struct {
		path string
		spec *ast.TypeSpec
	}
	var hits []hit
	pc1WalkProductionFiles(t, func(relPath string, file *ast.File) {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if ok && typeSpec.Name.Name == pc1CoordinatorTypeName {
					hits = append(hits, hit{path: relPath, spec: typeSpec})
				}
			}
		}
	}, "internal", "cmd")

	if len(hits) != 1 {
		var paths []string
		for _, h := range hits {
			paths = append(paths, h.path)
		}
		sort.Strings(paths)
		t.Fatalf("PC1 COORDINATOR: expected exactly one PublicationCoordinator declaration in %s, found %d: %v", pc1CoordinatorFile, len(hits), paths)
	}
	if hits[0].path != pc1CoordinatorFile {
		t.Fatalf("PC1 COORDINATOR: PublicationCoordinator is declared in %s, expected %s", hits[0].path, pc1CoordinatorFile)
	}
	if hits[0].spec.Assign.IsValid() {
		t.Fatalf("PC1 COORDINATOR: PublicationCoordinator must be a concrete type, not an alias")
	}
	structType, ok := hits[0].spec.Type.(*ast.StructType)
	if !ok {
		t.Fatalf("PC1 COORDINATOR: PublicationCoordinator must be a struct, got %T", hits[0].spec.Type)
	}
	if structType.Fields != nil && len(structType.Fields.List) != 0 {
		var fields []string
		for _, field := range structType.Fields.List {
			for _, name := range field.Names {
				fields = append(fields, name.Name)
			}
			if len(field.Names) == 0 {
				fields = append(fields, "<embedded>")
			}
		}
		t.Fatalf("PC1 COORDINATOR: PublicationCoordinator must have zero fields (stateless, no process-local authority), found %v", fields)
	}
}

// TestPC1PublicationPackageHasZeroProductiveImporters is the adoption gate: no
// production file outside internal/publication imports the package. Without
// an importer there is no call edge from any endpoint into the coordinator,
// hence no new CQL, consistency level, or TTL reachable through it.
func TestPC1PublicationPackageHasZeroProductiveImporters(t *testing.T) {
	var importers []string
	pc1WalkProductionFiles(t, func(relPath string, file *ast.File) {
		if pc1IsPublicationPackageFile(relPath) {
			return
		}
		for _, path := range pc1ImportPaths(file) {
			if path == pc1PublicationImportPath {
				importers = append(importers, relPath)
			}
		}
	}, "internal", "cmd")
	sort.Strings(importers)
	if len(importers) > 0 {
		t.Fatalf("PC1 ADOPTION: productive importers of internal/publication %v; PC-1 migrates zero funnels — a funnel migration is its own PC with its own characterization", importers)
	}
}

// pc1FunctionReferencesCoordinator reports whether fn's body mentions the
// coordinator by identifier or through a publication.X selector.
func pc1FunctionReferencesCoordinator(fn *ast.FuncDecl) []string {
	var refs []string
	if fn == nil || fn.Body == nil {
		return refs
	}
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.SelectorExpr:
			if x, ok := typed.X.(*ast.Ident); ok && x.Name == "publication" {
				refs = append(refs, "publication."+typed.Sel.Name)
			}
		case *ast.Ident:
			for _, name := range pc1CoordinatorIdentifiers {
				if typed.Name == name {
					refs = append(refs, name)
				}
			}
		}
		return true
	})
	return refs
}

// TestPC1ProductiveFunnelsDoNotReferenceCoordinator closes the gap the import
// guard leaves: an injected call in an inventoried HEAD caller or wrapper is
// caught even before an import exists. Every function PC-0 inventoried (F1–F9,
// tree mutations, R1–R4, the HEAD primitive) and every wrapper alias is
// checked, plus a repo-wide scan for the coordinator identifiers outside the
// package.
func TestPC1ProductiveFunnelsDoNotReferenceCoordinator(t *testing.T) {
	functions := pc0ParseProductionFuncs(t)
	var violations []string

	for _, caller := range pc0ExpectedHeadCallers {
		fn := pc0FunctionByPathAndName(functions, caller.path, caller.function)
		if fn == nil {
			t.Fatalf("PC1 ADOPTION: inventoried HEAD caller %s not found", pc0CallerKey(caller.path, caller.function))
		}
		for _, ref := range pc1FunctionReferencesCoordinator(fn) {
			violations = append(violations, pc0CallerKey(caller.path, caller.function)+" -> "+ref)
		}
	}
	for _, wrapper := range pc0PublicationWrappers {
		fn := pc0FunctionByPathAndName(functions, wrapper.path, wrapper.wrapper)
		if fn == nil {
			t.Fatalf("PC1 ADOPTION: inventoried wrapper %s not found", pc0CallerKey(wrapper.path, wrapper.wrapper))
		}
		for _, ref := range pc1FunctionReferencesCoordinator(fn) {
			violations = append(violations, pc0CallerKey(wrapper.path, wrapper.wrapper)+" -> "+ref)
		}
	}
	for key, fn := range functions {
		path, _, ok := pc0FunctionKeyParts(key)
		if !ok || pc1IsPublicationPackageFile(path) {
			continue
		}
		if fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			for _, name := range pc1CoordinatorIdentifiers {
				if ident.Name == name {
					violations = append(violations, key+" -> "+name)
				}
			}
			return true
		})
	}

	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("PC1 ADOPTION: productive funnel references the coordinator %v; PC-1 migrates zero funnels", violations)
	}
}

// TestPC1PublicationPackageIsStatelessAndStorageFree pins the multi-DC design
// constraints of PC-0 §14 at the source level: internal/publication imports
// only its exact pure allowlist and declares no mutable package-level state.
// The errors.New sentinels are the complete package-variable allowlist.
func TestPC1PublicationPackageIsStatelessAndStorageFree(t *testing.T) {
	var violations []string
	files := 0
	seenAllowedVars := map[string]int{}
	pc1WalkProductionFiles(t, func(relPath string, file *ast.File) {
		files++
		for _, path := range pc1ImportPaths(file) {
			if !pc1AllowedPublicationImports[path] {
				violations = append(violations, relPath+" imports unapproved "+path)
			}
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, name := range value.Names {
					expectedPath, allowed := pc1AllowedPublicationPackageVars[name.Name]
					if !allowed {
						violations = append(violations, relPath+" declares forbidden package-level var "+name.Name)
						continue
					}
					seenAllowedVars[name.Name]++
					if relPath != expectedPath {
						violations = append(violations, relPath+" declares "+name.Name+", expected "+expectedPath)
					}
					if index >= len(value.Values) || !pc1IsErrorsNewCall(value.Values[index]) {
						violations = append(violations, relPath+" declares "+name.Name+" with a non-errors.New value")
					}
				}
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch statement := node.(type) {
			case *ast.AssignStmt:
				for _, target := range statement.Lhs {
					if name := pc1AllowedPackageVarTarget(target); name != "" {
						violations = append(violations, relPath+" reassigns allowed package-level var "+name)
					}
				}
			case *ast.IncDecStmt:
				if name := pc1AllowedPackageVarTarget(statement.X); name != "" {
					violations = append(violations, relPath+" mutates allowed package-level var "+name)
				}
			case *ast.RangeStmt:
				for _, target := range []ast.Expr{statement.Key, statement.Value} {
					if name := pc1AllowedPackageVarTarget(target); name != "" {
						violations = append(violations, relPath+" reassigns allowed package-level var "+name)
					}
				}
			case *ast.UnaryExpr:
				if statement.Op == token.AND {
					if name := pc1AllowedPackageVarTarget(statement.X); name != "" {
						violations = append(violations, relPath+" takes address of allowed package-level var "+name)
					}
				}
			}
			return true
		})
	}, pc1PublicationPackageDir)
	if files == 0 {
		t.Fatalf("PC1 STATELESS: no production files found under %s", pc1PublicationPackageDir)
	}
	for name := range pc1AllowedPublicationPackageVars {
		if seenAllowedVars[name] != 1 {
			violations = append(violations, name+" declaration count is "+strconv.Itoa(seenAllowedVars[name])+", want 1")
		}
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("PC1 STATELESS: internal/publication imports or state violate the multi-DC skeleton contract: %v", violations)
	}
}

func pc1AllowedPackageVarTarget(expr ast.Expr) string {
	switch target := expr.(type) {
	case *ast.ParenExpr:
		return pc1AllowedPackageVarTarget(target.X)
	case *ast.StarExpr:
		return pc1AllowedPackageVarTarget(target.X)
	case *ast.Ident:
		if _, allowed := pc1AllowedPublicationPackageVars[target.Name]; allowed {
			return target.Name
		}
		return ""
	default:
		return ""
	}
}

func pc1IsErrorsNewCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "New" {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	return ok && identifier.Name == "errors"
}

// TestPC1PublicationPackageMethodAndFunctionSetsAreInventoried freezes every
// concrete method and package-level function. There is deliberately no
// Publish/Stage/Repair/Head/Settle capability: PC-0 demonstrated only a
// partial order, and a universal sequence would be an invented protocol.
func TestPC1PublicationPackageMethodAndFunctionSetsAreInventoried(t *testing.T) {
	var methods []string
	var packageFunctions []string
	pc1WalkProductionFiles(t, func(relPath string, file *ast.File) {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if receiver := pc0ReceiverTypeName(fn); receiver != "" {
				methods = append(methods, receiver+"."+fn.Name.Name)
			}
			if fn.Recv == nil {
				packageFunctions = append(packageFunctions, fn.Name.Name)
			}
		}
	}, pc1PublicationPackageDir)
	sort.Strings(methods)
	want := append([]string{}, pc1PublicationMethodAllowlist...)
	sort.Strings(want)
	if strings.Join(methods, ",") != strings.Join(want, ",") {
		t.Fatalf("PC1 METHOD SET: publication methods %v, inventoried %v; every new method must be explicitly reviewed and characterized", methods, want)
	}
	sort.Strings(packageFunctions)
	wantPackageFunctions := append([]string{}, pc1PublicationPackageFunctionAllowlist...)
	sort.Strings(wantPackageFunctions)
	if strings.Join(packageFunctions, ",") != strings.Join(wantPackageFunctions, ",") {
		t.Fatalf("PC1 PACKAGE FUNCTION SET: functions %v, inventoried %v; PC-1 must not expose an uninventoried orchestration entry point", packageFunctions, wantPackageFunctions)
	}
}

func pc1PublicationCallName(call *ast.CallExpr) string {
	switch function := call.Fun.(type) {
	case *ast.Ident:
		return function.Name
	case *ast.SelectorExpr:
		if receiver, ok := function.X.(*ast.Ident); ok {
			if receiver.Name == "errors" || receiver.Name == "fmt" {
				return receiver.Name + "." + function.Sel.Name
			}
		}
		return "." + function.Sel.Name
	default:
		return "<dynamic>"
	}
}

// TestPC1PublicationPackageCallSetIsInventoried closes the body-level gap in
// the state/import/method guards. Every production call expression is counted;
// adding fmt.Println to an existing allowed method, a builtin, a dynamic call,
// or another pure-looking call all require explicit review.
func TestPC1PublicationPackageCallSetIsInventoried(t *testing.T) {
	got := map[string]int{}
	pc1WalkProductionFiles(t, func(relPath string, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok {
				got[pc1PublicationCallName(call)]++
			}
			return true
		})
	}, pc1PublicationPackageDir)

	var violations []string
	for call := range got {
		if _, ok := pc1PublicationCallAllowlist[call]; !ok {
			violations = append(violations, call+" is not allowed")
		}
	}
	for call, want := range pc1PublicationCallAllowlist {
		if got[call] != want {
			violations = append(violations, call+" count is "+strconv.Itoa(got[call])+", want "+strconv.Itoa(want))
		}
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("PC1 CALL SET: internal/publication production calls differ from the pure allowlist: %v", violations)
	}
}

// TestPC1PC0InventoryIsUnchanged pins the PC-0 inventories by content so the
// PC-1 diff can be audited: the F1–F9 funnel map, the tree-mutation and
// content-resurrection classifications, the wrapper aliases, and the raw HEAD
// column writers are exactly what PC-0 (after H1) recorded. It is a snapshot
// of the PC-0 tables, not of production code; the PC-0 guards still bind the
// tables to production.
func TestPC1PC0InventoryIsUnchanged(t *testing.T) {
	wantCallers := []string{
		"internal/api/v2/files.go:CreateFile:block-publication",
		"internal/api/v2/files.go:finalizeStoredUploadMetadataOnce:block-publication",
		"internal/api/v2/batch_operations.go:processSingleItem:block-publication",
		"internal/api/v2/onlyoffice.go:publishEditedDocumentMetadata:block-publication",
		"internal/api/seafhttp.go:commitUploadedFileOnce:block-publication",
		"internal/api/seafhttp.go:commitUploadedFileMultiBlockOnce:block-publication",
		"internal/api/sync.go:handleSyncHeadPromotion:block-publication",
		"internal/api/sync.go:tryAutoMergeSyncHeadPromotion:block-publication",
		"internal/api/v2/files.go:CreateDirectory:tree-mutation",
		"internal/api/v2/files.go:RenameDirectory:tree-mutation",
		"internal/api/v2/files.go:RenameFile:tree-mutation",
		"internal/api/v2/files.go:DeleteDirectory:tree-mutation",
		"internal/api/v2/files.go:DeleteFile:tree-mutation",
		"internal/api/v2/files.go:BatchDeleteItems:tree-mutation",
		"internal/api/v2/files.go:copyItemWithinRepoWithRetry:tree-mutation",
		"internal/api/v2/batch_operations.go:processSameRepoMove:tree-mutation",
		"internal/api/v2/files.go:RevertFile:content-resurrection",
		"internal/api/v2/files.go:RevertDirectory:content-resurrection",
		"internal/api/v2/trash.go:RestoreTrashItem:content-resurrection",
		"internal/api/v2/trash.go:RevertDirents:content-resurrection",
		"internal/api/v2/fs_helpers.go:UpdateLibraryHeadFromSnapshot:head-primitive",
	}
	var gotCallers []string
	for _, caller := range pc0ExpectedHeadCallers {
		gotCallers = append(gotCallers, caller.path+":"+caller.function+":"+string(caller.class))
	}
	pc1AssertSameLines(t, "pc0ExpectedHeadCallers", gotCallers, wantCallers)

	wantFunnels := []string{
		"v2/CreateFile|CreateFile|RegisterUploadedBlockTargetAndMapping,prepareFileFSObjectForPublish|stagePendingPublishedFiles|queuePendingPublishedFileRepairs|UpdateLibraryHeadFromSnapshot|promotePendingPublishedFiles,CleanupFailedPublishAttempt",
		"v2/finalizeStoredUploadMetadataOnce|finalizeStoredUploadMetadataOnce|newPendingPublishedFile|stagePendingPublishedFiles|queuePendingPublishedFileRepairs|UpdateLibraryHeadFromSnapshot|promotePendingPublishedFiles,CleanupFailedPublishAttempt",
		"v2/processSingleItem|processSingleItem|copyFSObjectToLibraryForPublish|stagePendingPublishedFiles|queuePendingPublishedFileRepairs|UpdateLibraryHeadFromSnapshot|promotePendingPublishedFiles,CleanupFailedPublishAttempt",
		"v2/publishEditedDocumentMetadata|publishEditedDocumentMetadata|prepareFileFSObjectForPublish|stagePendingPublishedFiles|queuePendingPublishedFileRepairs|UpdateLibraryHeadFromSnapshot|promotePendingPublishedFiles",
		"seafhttp/commitUploadedFileOnce|commitUploadedFileOnce|stageSeafHTTPPublishAttemptReferences|stageSeafHTTPPublishAttemptReferences|queuePublishedFSObjectBlockReferenceRepairFn|UpdateLibraryHeadFromSnapshot|finalizeSeafHTTPPublishedBlockReferences",
		"seafhttp/commitUploadedFileMultiBlockOnce|commitUploadedFileMultiBlockOnce|stageSeafHTTPPublishAttemptReferences|stageSeafHTTPPublishAttemptReferences|queuePublishedFSObjectBlockReferenceRepairFn|UpdateLibraryHeadFromSnapshot|finalizeSeafHTTPPublishedBlockReferences",
		"sync/handleSyncHeadPromotion|handleSyncHeadPromotion|ensureSyncCommitBlockPublicationReadiness|stageSyncCommitBlockDelta|queueSyncCommitBlockReferenceRepairsFn|updateLibraryHeadWithStats|finalizeSyncCommitBlockDeltaAndSettleRepairIntent",
		"sync/tryAutoMergeSyncHeadPromotion|tryAutoMergeSyncHeadPromotion|ensureAndQueueAutoMergeSyncPublication|stageSyncCommitBlockDelta|ensureAndQueueAutoMergeSyncPublication|updateLibraryHeadWithStats|finalizeSyncCommitBlockDeltaAndSettleRepairIntent",
	}
	var gotFunnels []string
	for _, funnel := range pc0BlockPublicationFunnels {
		gotFunnels = append(gotFunnels, strings.Join([]string{
			funnel.label,
			funnel.function,
			strings.Join(funnel.characteristicSeams, ","),
			strings.Join(funnel.stage, ","),
			strings.Join(funnel.repair, ","),
			funnel.head,
			strings.Join(funnel.settlement, ","),
		}, "|"))
	}
	pc1AssertSameLines(t, "pc0BlockPublicationFunnels", gotFunnels, wantFunnels)

	wantWrappers := []string{
		"internal/api/v2/file_from_blocks.go:CreateFileFromBlocks->finalizeStoredUploadMetadata",
		"internal/api/v2/files.go:finalizeStoredUploadMetadata->finalizeStoredUploadMetadataOnce",
		"internal/api/v2/files.go:UploadFile->finalizeStoredUploadMetadata",
		"internal/api/seafhttp.go:commitUploadedFile->commitUploadedFileOnce",
		"internal/api/seafhttp.go:commitUploadedFileMultiBlock->commitUploadedFileMultiBlockOnce",
	}
	var gotWrappers []string
	for _, wrapper := range pc0PublicationWrappers {
		gotWrappers = append(gotWrappers, wrapper.path+":"+wrapper.wrapper+"->"+wrapper.callee)
	}
	pc1AssertSameLines(t, "pc0PublicationWrappers", gotWrappers, wantWrappers)

	wantWriters := []string{
		"internal/api/v2/fs_helpers.go:FSHelper.UpdateLibraryHead:cas",
		"internal/api/sync.go:SyncHandler.updateLibraryHeadWithStats:cas",
		"internal/api/v2/fs_helpers.go:FSHelper.InitializeLibraryHeadIfUnset:cas",
		"internal/api/v2/libraries.go:LibraryHandler.CreateLibrary:insert-create",
		"internal/api/v2/admin_libraries.go:AdminHandler.AdminCreateLibrary:insert-create",
	}
	var gotWriters []string
	for _, writer := range pc0ExpectedHeadColumnWriters {
		gotWriters = append(gotWriters, writer.path+":"+writer.decl+":"+string(writer.shape))
	}
	pc1AssertSameLines(t, "pc0ExpectedHeadColumnWriters", gotWriters, wantWriters)

	wantStageSeams := []string{"stagePendingPublishedFiles", "stageSeafHTTPPublishAttemptReferences", "stageSyncCommitBlockDelta"}
	pc1AssertSameLines(t, "pc0BlockPublicationStageSeams", pc0BlockPublicationStageSeams, wantStageSeams)
}

func pc1AssertSameLines(t *testing.T, label string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("PC1 INVENTORY: %s changed since PC-0/H1; PC-1 must not reclassify, remap, or migrate any funnel.\n got: %q\nwant: %q", label, got, want)
	}
}
