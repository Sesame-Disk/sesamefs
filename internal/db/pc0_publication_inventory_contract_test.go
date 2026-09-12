package db

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// PC-0 source contracts freeze selected publication-protocol facts reconstructed
// in docs/PUBLICATION-PROTOCOL-CHARACTERIZATION.md. They are characterization
// guards: a new productive HEAD publisher that lexically calls a named HEAD
// helper, including a package-level function-valued variable, a missing funnel
// seam, a silent token change at a named primitive, or a new raw-CQL writer of
// libraries.head_commit_id outside the inventoried allowlist must turn red. They
// do not inventory method values or aliased callees, do not freeze the full
// consistency map, and do not change production behavior. Whether exactly one
// PublicationCoordinator exists and that nothing productive adopts it is frozen
// by the PC-1 contracts in pc1_publication_coordinator_contract_test.go.

type pc0HeadClass string

const (
	pc0HeadBlockPublication pc0HeadClass = "block-publication"
	pc0HeadTreeMutation     pc0HeadClass = "tree-mutation"
	// pc0HeadContentResurrection paths publish a HEAD that newly depends on a
	// historical fs_object (trash or version history). They add a positive
	// block-dependency delta with BORROWED provenance but today stage no pub:,
	// queue no repair, and run no fence. They are not tree mutations
	// (ISSUE-PC0-CONTENT-RESURRECTION-PUBLICATION-01).
	pc0HeadContentResurrection pc0HeadClass = "content-resurrection"
	pc0HeadPrimitive           pc0HeadClass = "head-primitive"
)

type pc0HeadCaller struct {
	path     string
	function string
	class    pc0HeadClass
}

// Every production function that calls UpdateLibraryHeadFromSnapshot,
// updateLibraryHeadWithStats, or UpdateLibraryHead must appear here. Tree
// mutations are included so a new block publisher cannot hide as an unlisted
// directory rename. Content-resurrection paths are listed separately: they
// are HEAD publications with a positive borrowed block-dependency delta and
// no publication seams today. The two HEAD initializers (InitializeLibraryFS,
// createInitialCommit) call no named HEAD helper — both publish through the
// conditional FSHelper.InitializeLibraryHeadIfUnset
// (ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01) — and are inventoried by
// pc0ExpectedHeadColumnWriters instead.
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
	{path: "internal/api/v2/files.go", function: "RevertFile", class: pc0HeadContentResurrection},
	{path: "internal/api/v2/files.go", function: "RevertDirectory", class: pc0HeadContentResurrection},
	{path: "internal/api/v2/trash.go", function: "RestoreTrashItem", class: pc0HeadContentResurrection},
	{path: "internal/api/v2/trash.go", function: "RevertDirents", class: pc0HeadContentResurrection},
	{path: "internal/api/v2/fs_helpers.go", function: "UpdateLibraryHeadFromSnapshot", class: pc0HeadPrimitive},
}

type pc0FunnelSeams struct {
	label               string
	function            string
	characteristicSeams []string
	stage               []string
	repair              []string
	head                string
	settlement          []string
}

var pc0BlockPublicationFunnels = []pc0FunnelSeams{
	{
		label:               "v2/CreateFile",
		function:            "CreateFile",
		characteristicSeams: []string{"RegisterUploadedBlockTargetAndMapping", "prepareFileFSObjectForPublish"},
		stage:               []string{"stagePendingPublishedFiles"},
		repair:              []string{"queuePendingPublishedFileRepairs"},
		head:                "UpdateLibraryHeadFromSnapshot",
		settlement:          []string{"promotePendingPublishedFiles", "CleanupFailedPublishAttempt"},
	},
	{
		label:               "v2/finalizeStoredUploadMetadataOnce",
		function:            "finalizeStoredUploadMetadataOnce",
		characteristicSeams: []string{"newPendingPublishedFile"},
		stage:               []string{"stagePendingPublishedFiles"},
		repair:              []string{"queuePendingPublishedFileRepairs"},
		head:                "UpdateLibraryHeadFromSnapshot",
		settlement:          []string{"promotePendingPublishedFiles", "CleanupFailedPublishAttempt"},
	},
	{
		label:               "v2/processSingleItem",
		function:            "processSingleItem",
		characteristicSeams: []string{"copyFSObjectToLibraryForPublish"},
		stage:               []string{"stagePendingPublishedFiles"},
		repair:              []string{"queuePendingPublishedFileRepairs"},
		head:                "UpdateLibraryHeadFromSnapshot",
		settlement:          []string{"promotePendingPublishedFiles", "CleanupFailedPublishAttempt"},
	},
	{
		label:               "v2/publishEditedDocumentMetadata",
		function:            "publishEditedDocumentMetadata",
		characteristicSeams: []string{"prepareFileFSObjectForPublish"},
		stage:               []string{"stagePendingPublishedFiles"},
		repair:              []string{"queuePendingPublishedFileRepairs"},
		head:                "UpdateLibraryHeadFromSnapshot",
		settlement:          []string{"promotePendingPublishedFiles"},
	},
	{
		label:               "seafhttp/commitUploadedFileOnce",
		function:            "commitUploadedFileOnce",
		characteristicSeams: []string{"stageSeafHTTPPublishAttemptReferences"},
		stage:               []string{"stageSeafHTTPPublishAttemptReferences"},
		repair:              []string{"queuePublishedFSObjectBlockReferenceRepairFn"},
		head:                "UpdateLibraryHeadFromSnapshot",
		settlement:          []string{"finalizeSeafHTTPPublishedBlockReferences"},
	},
	{
		label:               "seafhttp/commitUploadedFileMultiBlockOnce",
		function:            "commitUploadedFileMultiBlockOnce",
		characteristicSeams: []string{"stageSeafHTTPPublishAttemptReferences"},
		stage:               []string{"stageSeafHTTPPublishAttemptReferences"},
		repair:              []string{"queuePublishedFSObjectBlockReferenceRepairFn"},
		head:                "UpdateLibraryHeadFromSnapshot",
		settlement:          []string{"finalizeSeafHTTPPublishedBlockReferences"},
	},
	{
		label:               "sync/handleSyncHeadPromotion",
		function:            "handleSyncHeadPromotion",
		characteristicSeams: []string{"ensureSyncCommitBlockPublicationReadiness"},
		stage:               []string{"stageSyncCommitBlockDelta"},
		repair:              []string{"queueSyncCommitBlockReferenceRepairsFn"},
		head:                "updateLibraryHeadWithStats",
		settlement:          []string{"finalizeSyncCommitBlockDeltaAndSettleRepairIntent"},
	},
	{
		label:               "sync/tryAutoMergeSyncHeadPromotion",
		function:            "tryAutoMergeSyncHeadPromotion",
		characteristicSeams: []string{"ensureAndQueueAutoMergeSyncPublication"},
		stage:               []string{"stageSyncCommitBlockDelta"},
		repair:              []string{"ensureAndQueueAutoMergeSyncPublication"},
		head:                "updateLibraryHeadWithStats",
		settlement:          []string{"finalizeSyncCommitBlockDeltaAndSettleRepairIntent"},
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
		path:       "internal/db/block_references.go",
		function:   "ValidateBorrowedFSPublicationAuthority",
		needle:     "BlockAuthorityAdvisory",
		notNeedle:  "BlockAuthorityStrong",
		observed:   "BorrowedFS publication authority stays advisory/LQ",
		notMessage: "BorrowedFS publication authority must not switch to SERIAL",
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
		path:     "internal/api/v2/fs_helpers.go",
		function: "InitializeLibraryHeadIfUnset",
		needle:   "IF head_commit_id = null",
		observed: "the initial-HEAD publish only applies when no HEAD exists (ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01)",
	},
	{
		path:     "internal/api/v2/fs_helpers.go",
		function: "InitializeLibraryHeadIfUnset",
		needle:   "AND created_at != null",
		observed: "the initial-HEAD publish is anchored to an existing row; without it IF head_commit_id = null upserts a phantom library on a missing partition",
	},
	{
		path:     "internal/api/v2/write_helpers.go",
		function: "deleteUnpublishedLibraryRow",
		needle:   "IF head_commit_id = null",
		observed: "a creation rollback takes authority in the HEAD Paxos domain: the canonical row is deleted only while no HEAD is published, so a creator's own failure can never destroy a HEAD another initializer published (ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01, review round 4)",
	},
	{
		path:     "internal/api/v2/library_rollback.go",
		function: "rollbackNewLibrary",
		needle:   "persistLibraryRollbackPendingFn",
		observed: "a durable library_rollback_pending marker is written before the authority LWT so crash recovery is discoverable; the marker is not itself cleanup authority",
	},
	{
		path:     "internal/api/v2/library_rollback.go",
		function: "runAuthorizedLibraryRollbackCleanup",
		needle:   "deleteUnpublishedLibraryRow",
		observed: "request-path rollback and the pending-marker reaper share the same HEAD LWT; finding a marker never authorizes derived cleanup",
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

// pc0HeadColumnWriter inventories every production string literal that writes
// libraries.head_commit_id (UPDATE libraries ... head_commit_id or INSERT INTO
// libraries ... head_commit_id). libraries_by_id projections are excluded by
// the word boundary. HEAD advances and HEAD initialization must both stay in
// the CAS domain: TestPC0NoUnconditionalHeadUpdateRemains fails if any
// inventoried or discovered writer has the update-unconditional shape. decl
// keeps receiver identity (Receiver.Method for
// methods, the bare name for functions and package-level var/const) so two
// same-named methods on different receivers in one file cannot share an
// allowlist entry. shape is derived from the literal:
//
//   - pc0HeadWriteCAS: the LWT shape (IF head_commit_id = ?), the only
//     publication authority PC-0 characterizes;
//   - pc0HeadWriteInsertCreate: INSERT of a brand-new library partition at
//     creation time (fresh UUID, no other writer can address the row yet);
//   - pc0HeadWriteUpdateUnconditional: UPDATE of an EXISTING row without IF.
//     This was the multi-DC HEAD-reversion shape recorded as
//     ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01 and PC-0 §3.4 (a blind DC's
//     session-consistency read of "" could overwrite a HEAD another DC had
//     already published by CAS). No production writer has this shape any
//     more; the class stays so a reintroduction is named, not merely
//     "unlisted".
type pc0HeadWriteShape string

const (
	pc0HeadWriteCAS                 pc0HeadWriteShape = "cas"
	pc0HeadWriteInsertCreate        pc0HeadWriteShape = "insert-create"
	pc0HeadWriteUpdateUnconditional pc0HeadWriteShape = "update-unconditional"
)

type pc0HeadColumnWriter struct {
	path  string
	decl  string
	shape pc0HeadWriteShape
}

var pc0ExpectedHeadColumnWriters = []pc0HeadColumnWriter{
	{path: "internal/api/v2/fs_helpers.go", decl: "FSHelper.UpdateLibraryHead", shape: pc0HeadWriteCAS},
	{path: "internal/api/sync.go", decl: "SyncHandler.updateLibraryHeadWithStats", shape: pc0HeadWriteCAS},
	// The only initializer: IF head_commit_id = null AND created_at != null
	// (both clauses pinned separately in pc0ConsistencyPins).
	// InitializeLibraryFS and Sync createInitialCommit publish through it and
	// write no head_commit_id literal of their own
	// (ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01, resolved).
	{path: "internal/api/v2/fs_helpers.go", decl: "FSHelper.InitializeLibraryHeadIfUnset", shape: pc0HeadWriteCAS},
	{path: "internal/api/v2/libraries.go", decl: "LibraryHandler.CreateLibrary", shape: pc0HeadWriteInsertCreate},
	{path: "internal/api/v2/admin_libraries.go", decl: "AdminHandler.AdminCreateLibrary", shape: pc0HeadWriteInsertCreate},
}

// pc0ReceiverTypeName returns the receiver's base type name (pointer and
// generic instantiation stripped) or "" for plain functions.
func pc0ReceiverTypeName(fn *ast.FuncDecl) string {
	if fn == nil || fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	for {
		switch typed := expr.(type) {
		case *ast.StarExpr:
			expr = typed.X
		case *ast.ParenExpr:
			expr = typed.X
		case *ast.IndexExpr:
			expr = typed.X
		case *ast.IndexListExpr:
			expr = typed.X
		case *ast.Ident:
			return typed.Name
		default:
			return "<unknown>"
		}
	}
}

// pc0HeadColumnDeclName is the receiver-aware declaration name used by the
// raw head_commit_id inventory: Receiver.Method for methods, otherwise the
// function / first var or const name.
func pc0HeadColumnDeclName(decl ast.Decl) string {
	switch decl := decl.(type) {
	case *ast.FuncDecl:
		if recv := pc0ReceiverTypeName(decl); recv != "" {
			return recv + "." + decl.Name.Name
		}
		return decl.Name.Name
	case *ast.GenDecl:
		for _, spec := range decl.Specs {
			if value, ok := spec.(*ast.ValueSpec); ok && len(value.Names) > 0 {
				return value.Names[0].Name
			}
		}
	}
	return ""
}

var pc0HeadColumnWritePattern = regexp.MustCompile(`(?is)\b(update|insert\s+into)\s+libraries\b[^;]*?\bhead_commit_id\b`)
var pc0HeadColumnConditionalPattern = regexp.MustCompile(`(?is)\bif\s+head_commit_id\b`)
var pc0HeadColumnInsertPattern = regexp.MustCompile(`(?is)\binsert\s+into\s+libraries\b`)

func pc0HeadWriteShapeOf(literal string) pc0HeadWriteShape {
	switch {
	case pc0HeadColumnConditionalPattern.MatchString(literal):
		return pc0HeadWriteCAS
	case pc0HeadColumnInsertPattern.MatchString(literal):
		return pc0HeadWriteInsertCreate
	default:
		return pc0HeadWriteUpdateUnconditional
	}
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

// pc0FunctionKey preserves receiver identity for methods. The receiver type
// position is stable for this parsed file and distinguishes same-named methods
// without requiring type checking (which this lexical inventory deliberately
// does not perform).
func pc0FunctionKey(path string, fn *ast.FuncDecl) string {
	if fn == nil || fn.Recv == nil || len(fn.Recv.List) == 0 {
		return pc0CallerKey(path, fn.Name.Name)
	}
	receiverPos := strconv.FormatInt(int64(fn.Recv.List[0].Type.Pos()), 10)
	return pc0CallerKey(path, "recv@"+receiverPos+":"+fn.Name.Name)
}

func pc0FunctionKeyParts(key string) (path, function string, ok bool) {
	separator := strings.IndexByte(key, ':')
	if separator <= 0 || separator == len(key)-1 {
		return "", "", false
	}
	path = key[:separator]
	member := key[separator+1:]
	if strings.HasPrefix(member, "recv@") {
		receiverSeparator := strings.LastIndexByte(member, ':')
		if receiverSeparator < 0 || receiverSeparator == len(member)-1 {
			return "", "", false
		}
		member = member[receiverSeparator+1:]
	}
	if member == "" {
		return "", "", false
	}
	return path, member, true
}

func pc0UnwrapFuncLit(expr ast.Expr) *ast.FuncLit {
	for {
		switch expression := expr.(type) {
		case *ast.FuncLit:
			return expression
		case *ast.ParenExpr:
			expr = expression.X
		default:
			return nil
		}
	}
}

// pc0ParseProductionFuncs walks internal/ recursively so a new productive HEAD
// publisher cannot hide from TestPC0AllHeadCallersAreInventoried by living in
// a package outside the API tree. It also indexes package-level var declarations
// whose value is a function literal, while deliberately ignoring local closures.
func pc0ParseProductionFuncs(t *testing.T) map[string]*ast.FuncDecl {
	t.Helper()
	root := r3RepositoryRoot(t)
	internalRoot := filepath.Join(root, "internal")
	functions := make(map[string]*ast.FuncDecl)
	walkErr := filepath.WalkDir(internalRoot, func(path string, entry fs.DirEntry, err error) error {
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
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				key := pc0FunctionKey(relPath, decl)
				if _, exists := functions[key]; exists {
					t.Fatalf("PC0 INVENTORY: duplicate production function key %s", key)
				}
				functions[key] = decl
			case *ast.GenDecl:
				if decl.Tok != token.VAR {
					continue
				}
				for _, spec := range decl.Specs {
					value, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for index, expression := range value.Values {
						literal := pc0UnwrapFuncLit(expression)
						if literal == nil || index >= len(value.Names) {
							continue
						}
						fn := &ast.FuncDecl{
							Name: value.Names[index],
							Type: literal.Type,
							Body: literal.Body,
						}
						key := pc0FunctionKey(relPath, fn)
						if _, exists := functions[key]; exists {
							t.Fatalf("PC0 INVENTORY: duplicate production function key %s", key)
						}
						functions[key] = fn
					}
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("PC0 INVENTORY: walk %s: %v", internalRoot, walkErr)
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
		_, name, ok := pc0FunctionKeyParts(key)
		if ok && name == function {
			if found != nil {
				return nil
			}
			found = fn
		}
	}
	return found
}

func pc0FunctionByPathAndName(functions map[string]*ast.FuncDecl, path, function string) *ast.FuncDecl {
	var found *ast.FuncDecl
	for key, fn := range functions {
		keyPath, keyFunction, ok := pc0FunctionKeyParts(key)
		if !ok || keyPath != path || keyFunction != function {
			continue
		}
		if found != nil {
			return nil
		}
		found = fn
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
		path, function, ok := pc0FunctionKeyParts(key)
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
		fn := pc0FunctionByPathAndName(functions, expected.path, expected.function)
		if fn == nil {
			missing = append(missing, pc0CallerKey(expected.path, expected.function))
			continue
		}
		if expected.class != pc0HeadPrimitive {
			foundExpected := false
			for key, candidate := range functions {
				if candidate != fn {
					continue
				}
				foundExpected = found[key]
				break
			}
			if !foundExpected {
				missing = append(missing, pc0CallerKey(expected.path, expected.function)+" (listed but no HEAD call)")
			}
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
		need := append([]string{}, funnel.characteristicSeams...)
		need = append(need, funnel.stage...)
		need = append(need, funnel.repair...)
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

func TestPC0BlockBearingFunnelsKeepDurableRepairBeforeHEAD(t *testing.T) {
	functions := pc0ParseProductionFuncs(t)
	for _, funnel := range pc0BlockPublicationFunnels {
		if len(funnel.repair) == 0 {
			t.Fatalf("PC0 ORDER: %s has no explicit durable repair seam", funnel.label)
		}
		fn := pc0FunctionByName(functions, funnel.function)
		if fn == nil {
			t.Fatalf("PC0 ORDER: %s function %s not found uniquely", funnel.label, funnel.function)
		}
		stagePos := pc0FirstNamedCallPos(fn, funnel.stage[0])
		headPos := pc0FirstNamedCallPos(fn, funnel.head)
		if stagePos == token.NoPos || headPos == token.NoPos {
			t.Fatalf("PC0 ORDER: %s lost stage or HEAD", funnel.label)
		}
		for _, repair := range funnel.repair {
			repairPos := pc0FirstNamedCallPos(fn, repair)
			if repairPos == token.NoPos {
				t.Fatalf("PC0 ORDER: %s lost durable repair seam %s", funnel.label, repair)
			}
			if !(stagePos < repairPos && repairPos < headPos) {
				t.Fatalf("PC0 ORDER: %s must keep stage < durable repair < HEAD", funnel.label)
			}
		}
	}
}

func TestPC0TreeMutationsDoNotCallBlockPublicationStageSeams(t *testing.T) {
	functions := pc0ParseProductionFuncs(t)
	var violations []string
	for _, caller := range pc0ExpectedHeadCallers {
		if caller.class != pc0HeadTreeMutation {
			continue
		}
		fn := pc0FunctionByPathAndName(functions, caller.path, caller.function)
		if fn == nil {
			t.Fatalf("PC0 CLASSIFICATION: listed tree mutation %s not found", pc0CallerKey(caller.path, caller.function))
		}
		calls := pc0FunctionCallsNamed(fn, pc0BlockPublicationStageSeams...)
		for _, seam := range pc0BlockPublicationStageSeams {
			if calls[seam] {
				violations = append(violations, pc0CallerKey(caller.path, caller.function)+" -> "+seam)
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

// TestPC0ObservedRepairReadinessPartialOrder freezes today's selected
// readiness/repair orders. It does not authorize a coordinator to pick one
// universal readiness→repair sequence.
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

// pc0HeadColumnWriteLiterals returns every production string literal under
// the given roots that writes libraries.head_commit_id, keyed by path and the
// receiver-aware enclosing top-level declaration (Receiver.Method, function,
// var, or const).
func pc0HeadColumnWriteLiterals(t *testing.T, roots ...string) map[string][]string {
	t.Helper()
	repoRoot := r3RepositoryRoot(t)
	hits := map[string][]string{}
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
			relPath = filepath.ToSlash(relPath)
			file := r3ParseProductionFile(t, path)
			for _, decl := range file.Decls {
				name := pc0HeadColumnDeclName(decl)
				ast.Inspect(decl, func(node ast.Node) bool {
					lit, ok := node.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						return true
					}
					if pc0HeadColumnWritePattern.MatchString(lit.Value) {
						key := pc0CallerKey(relPath, name)
						hits[key] = append(hits[key], lit.Value)
					}
					return true
				})
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("PC0 HEAD COLUMN: walk %s: %v", root, walkErr)
		}
	}
	return hits
}

// TestPC0RawHeadColumnWritersAreInventoried closes the raw-CQL blind spot of
// TestPC0AllHeadCallersAreInventoried: a writer of libraries.head_commit_id
// that never calls a named HEAD helper (today: the conditional
// FSHelper.InitializeLibraryHeadIfUnset initializer — called by both
// InitializeLibraryFS and Sync createInitialCommit, but itself the only
// literal writer — and two creation-time INSERTs) must still be inventoried,
// and its write shape must match the record. It scans string literals in
// internal/ and cmd/ and keys writers with receiver identity, so a same-named
// method on another receiver cannot hide under an allowlisted entry.
func TestPC0RawHeadColumnWritersAreInventoried(t *testing.T) {
	hits := pc0HeadColumnWriteLiterals(t, "internal", "cmd")

	expected := map[string]pc0HeadColumnWriter{}
	for _, writer := range pc0ExpectedHeadColumnWriters {
		expected[pc0CallerKey(writer.path, writer.decl)] = writer
	}

	var unlisted, mismatched []string
	for key, literals := range hits {
		writer, listed := expected[key]
		if !listed {
			unlisted = append(unlisted, key)
			continue
		}
		for _, literal := range literals {
			if shape := pc0HeadWriteShapeOf(literal); shape != writer.shape {
				mismatched = append(mismatched, key+" is "+string(shape)+", inventoried as "+string(writer.shape))
			}
		}
	}
	sort.Strings(unlisted)
	sort.Strings(mismatched)
	if len(unlisted) > 0 {
		t.Fatalf("PC0 HEAD COLUMN: unlisted raw head_commit_id writer %v; every libraries.head_commit_id write must be inventoried in pc0ExpectedHeadColumnWriters and docs/PUBLICATION-PROTOCOL-CHARACTERIZATION.md §3.4", unlisted)
	}
	if len(mismatched) > 0 {
		t.Fatalf("PC0 HEAD COLUMN: head_commit_id write shape changed for %v; update pc0ExpectedHeadColumnWriters, §3.4, and ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01", mismatched)
	}

	var missing []string
	for key := range expected {
		if _, found := hits[key]; !found {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("PC0 HEAD COLUMN: inventoried head_commit_id writers no longer found: %v", missing)
	}
}

// TestPC0NoUnconditionalHeadUpdateRemains pins the resolution of
// ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01: every UPDATE of
// libraries.head_commit_id in production is conditional. An unconditional
// UPDATE anywhere (inventoried or not) is a regression to the multi-DC
// HEAD-reversion shape.
func TestPC0NoUnconditionalHeadUpdateRemains(t *testing.T) {
	for _, writer := range pc0ExpectedHeadColumnWriters {
		if writer.shape == pc0HeadWriteUpdateUnconditional {
			t.Fatalf("PC0 HEAD COLUMN: %s:%s is inventoried as update-unconditional; HEAD initialization must go through FSHelper.InitializeLibraryHeadIfUnset (IF head_commit_id = null AND created_at != null)", writer.path, writer.decl)
		}
	}
	hits := pc0HeadColumnWriteLiterals(t, "internal", "cmd")
	var unconditional []string
	for key, literals := range hits {
		for _, literal := range literals {
			if pc0HeadWriteShapeOf(literal) == pc0HeadWriteUpdateUnconditional {
				unconditional = append(unconditional, key)
			}
		}
	}
	sort.Strings(unconditional)
	if len(unconditional) > 0 {
		t.Fatalf("PC0 HEAD COLUMN: unconditional UPDATE of libraries.head_commit_id reintroduced at %v; a blind datacenter could revert a CAS-published HEAD (ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01)", unconditional)
	}
}

// TestPC0ContentResurrectionPathsObservedWithoutPublicationSeams freezes the
// observed gap: RevertFile, RevertDirectory, RestoreTrashItem, and
// RevertDirents publish a HEAD that newly depends on historical fs_objects
// without staging pub:, queueing durable repair, or fencing exact P. When a
// later PC migrates one of them, it must be reclassified as block-publication
// and mapped in pc0BlockPublicationFunnels rather than silently keeping this
// observed-gap classification.
func TestPC0ContentResurrectionPathsObservedWithoutPublicationSeams(t *testing.T) {
	functions := pc0ParseProductionFuncs(t)
	seams := append([]string{}, pc0BlockPublicationStageSeams...)
	seams = append(seams, "queuePendingPublishedFileRepairs", "queuePublishedFSObjectBlockReferenceRepairFn", "queueSyncCommitBlockReferenceRepairsFn", "validateCommitBlockPublicationFences")
	listed := 0
	var violations []string
	for _, caller := range pc0ExpectedHeadCallers {
		if caller.class != pc0HeadContentResurrection {
			continue
		}
		listed++
		fn := pc0FunctionByPathAndName(functions, caller.path, caller.function)
		if fn == nil {
			t.Fatalf("PC0 RESURRECTION: listed path %s not found", pc0CallerKey(caller.path, caller.function))
		}
		calls := pc0FunctionCallsNamed(fn, seams...)
		for _, seam := range seams {
			if calls[seam] {
				violations = append(violations, pc0CallerKey(caller.path, caller.function)+" -> "+seam)
			}
		}
	}
	if listed != 4 {
		t.Fatalf("PC0 RESURRECTION: expected the 4 inventoried content-resurrection paths, found %d", listed)
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("PC0 RESURRECTION: a content-resurrection path now invokes publication seams; reclassify it as block-publication and map its seams: %v", violations)
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
