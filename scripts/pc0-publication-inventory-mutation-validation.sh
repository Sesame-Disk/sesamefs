#!/usr/bin/env bash
# Mutations that prove the PC-0 inventory/consistency guards actually fail closed.
set -uo pipefail
cd "$(dirname "$0")/.."

FILES=internal/api/v2/files.go
REFS=internal/db/block_references.go
FSH=internal/api/v2/fs_helpers.go
BACKUPS=()
green() { printf '\033[32m%s\033[0m\n' "$*"; }
red() { printf '\033[31m%s\033[0m\n' "$*" >&2; }
restore() {
  local f
  for f in "${BACKUPS[@]:-}"; do
    if [ -n "$f" ] && [ -f "$f.pc0bak" ]; then mv -f "$f.pc0bak" "$f"; fi
  done
  BACKUPS=()
}
fail() { red "FAILED: $*"; restore; exit 1; }
trap restore EXIT INT TERM
mutate() {
  local f="$1" expr="$2"
  cp "$f" "$f.pc0bak"
  BACKUPS+=("$f")
  perl -0pi -e "$expr" "$f"
  cmp -s "$f" "$f.pc0bak" && fail "mutation did not apply to $f"
}
expect_red() {
  local pattern="$1" needle="$2" what="$3" out status
  out="$(go test ./internal/db -count=1 -run "$pattern" 2>&1)"
  status=$?
  if [ "$status" -eq 0 ]; then
    printf '%s\n' "$out"
    fail "$what stayed green"
  fi
  printf '%s\n' "$out" | grep -q "$needle" || {
    printf '%s\n' "$out"
    fail "$what went red without $needle"
  }
  green "RED as required: $what"
}

m_untracked_head_publisher() {
  restore
  mutate "$FILES" 's@(func \(h \*FileHandler\) CreateFile\(c \*gin.Context\) \{)@func (h *FileHandler) pc0UntrackedPublisher(fsHelper *FSHelper) {\n\t_ = fsHelper.UpdateLibraryHeadFromSnapshot(nil, "", "", "")\n}\n\n$1@'
  expect_red '^TestPC0AllHeadCallersAreInventoried$' 'unlisted lexical HEAD callers' 'untracked HEAD publisher'
}

m_untracked_function_value_head_publisher() {
  restore
  mutate "$FILES" 's@(func \(h \*FileHandler\) CreateFile\(c \*gin.Context\) \{)@var pc0MutationHiddenPublisher = func(fsHelper *FSHelper) {\n\t_ = fsHelper.UpdateLibraryHeadFromSnapshot(nil, "", "", "")
}\n\n$1@'
  expect_red '^TestPC0AllHeadCallersAreInventoried$' 'unlisted lexical HEAD callers' 'untracked function-valued HEAD publisher'
}
m_untracked_parenthesized_function_value_head_publisher() {
  restore
  mutate "$FILES" 's@(func \(h \*FileHandler\) CreateFile\(c \*gin.Context\) \{)@var pc0MutationParenthesizedPublisher = (func(fsHelper *FSHelper) {\n\t_ = fsHelper.UpdateLibraryHeadFromSnapshot(nil, "", "", "")\n})\n\n$1@'
  expect_red '^TestPC0AllHeadCallersAreInventoried$' 'unlisted lexical HEAD callers' 'untracked parenthesized function-valued HEAD publisher'
}
m_drop_funnel_stage_seam() {
  restore
  mutate "$FILES" 's@if err := fsHelper.stagePendingPublishedFiles\(orgID, repoID, commitID, pendingFiles\)@if err := fsHelper.notAPublicationStage(orgID, repoID, commitID, pendingFiles)@'
  expect_red '^TestPC0BlockPublicationFunnelsHaveMappedSeams$' 'v2/CreateFile missing seam calls' 'CreateFile without stage seam'
}

m_tree_mutation_stages_block_publication() {
  restore
  mutate "$FILES" 's@(func \(h \*FileHandler\) RenameFile\(c \*gin.Context\) \{)@$1\n\t_ = stagePendingPublishedFiles(nil, "", "", nil)@'
  expect_red '^TestPC0TreeMutationsDoNotCallBlockPublicationStageSeams$' 'tree mutation callers must not invoke block-publication stage seams' 'tree mutation classified as block publisher'
}

m_downgrade_sync_provenance_cl() {
  restore
  # Single-line, CRLF-agnostic: BlockReferenceExistsLocalQuorum's
  # Consistency(gocql.LocalQuorum) call is the only occurrence of that exact
  # token in this file, so this does not need to span the multi-line query
  # literal (a \n-based pattern silently no-ops on a CRLF checkout).
  mutate "$REFS" 's@\.Consistency\(gocql.LocalQuorum\)\.Scan\(&existing\)@.Consistency(gocql.One).Scan(&existing)@'
  expect_red '^TestPC0CriticalConsistencyPrimitivesArePinned$' 'BlockReferenceExistsLocalQuorum' 'Sync provenance CL downgraded from LOCAL_QUORUM'
}

m_raw_cql_head_writer() {
  restore
  # A writer of libraries.head_commit_id that calls no named HEAD helper is
  # invisible to TestPC0AllHeadCallersAreInventoried (the shape of the two
  # production initializers); the raw-CQL guard must catch it instead.
  mutate "$FILES" 's@(func \(h \*FileHandler\) CreateFile\(c \*gin.Context\) \{)@func (h *FileHandler) pc0RawCQLPublisher(orgID, repoID, commitID string) error {\n\treturn h.db.Session().Query(`UPDATE libraries SET head_commit_id = ? WHERE org_id = ? AND library_id = ?`, commitID, orgID, repoID).Exec()\n}\n\n$1@'
  expect_red '^TestPC0RawHeadColumnWritersAreInventoried$' 'unlisted raw head_commit_id writer' 'raw-CQL head_commit_id writer outside the allowlist'
}

m_resurrection_path_starts_staging() {
  restore
  # A content-resurrection path that starts staging pub: must be reclassified
  # as a block-publication funnel, not silently keep the observed-gap class.
  mutate "$FILES" 's@(func \(h \*FileHandler\) RevertFile\(c \*gin.Context\) \{)@$1\n\t_ = stagePendingPublishedFiles(nil, "", "", nil)@'
  expect_red '^TestPC0ContentResurrectionPathsObservedWithoutPublicationSeams$' 'reclassify it as block-publication' 'resurrection path invoking a publication stage seam'
}

m_fence_before_stage() {
  restore
  # Moving the final exact-P revalidation before stage reopens the W1 TOCTOU;
  # the observed CFFB order stage < repair < fence < HEAD must stay frozen.
  mutate "$FILES" 's@(\tif err := fsHelper.stagePendingPublishedFiles\(orgID, repoID, newCommitID, pendingFiles\); err != nil \{)@\t_ = h.validateCommitBlockPublicationFences(orgID, commitBlocks)\n$1@'
  expect_red '^TestPC0ObservedRepairReadinessPartialOrder$' 'stage then repair then fence then HEAD' 'exact-P fence moved before stage'
}

m_initializer_loses_its_condition() {
  restore
  # Reintroducing the unconditional initializer (ISSUE-LIBRARY-INITIAL-HEAD-
  # CONCURRENCY-01) must be named as a regression, not just an inventory drift.
  # CRLF-agnostic: only the CQL line starts with two tabs; the doc comment
  # mentioning the same condition is on a '//' line and must stay.
  mutate "$FSH" 's@		IF head_commit_id = null AND created_at != null@		@'
  expect_red '^TestPC0NoUnconditionalHeadUpdateRemains$' 'unconditional UPDATE of libraries.head_commit_id reintroduced' 'initializer stripped of its IF condition'
}

m_initializer_loses_null_head_clause() {
  restore
  # Keeping only IF created_at != null would let the initializer overwrite an
  # existing HEAD; both load-bearing clauses are pinned independently.
  # Two-tab prefix targets the CQL line, not the doc comment above the function.
  mutate "$FSH" 's@		IF head_commit_id = null AND created_at != null@		IF created_at != null@'
  expect_red '^TestPC0CriticalConsistencyPrimitivesArePinned$' 'InitializeLibraryHeadIfUnset' 'initializer lost head_commit_id = null'
}

m_initializer_loses_created_at_clause() {
  restore
  # Keeping only IF head_commit_id = null reopens the phantom-row upsert on a
  # missing partition.
  mutate "$FSH" 's@		IF head_commit_id = null AND created_at != null@		IF head_commit_id = null@'
  expect_red '^TestPC0CriticalConsistencyPrimitivesArePinned$' 'InitializeLibraryHeadIfUnset' 'initializer lost created_at != null'
}

m_untracked_head_publisher
m_untracked_function_value_head_publisher
m_untracked_parenthesized_function_value_head_publisher
m_drop_funnel_stage_seam
m_tree_mutation_stages_block_publication
m_downgrade_sync_provenance_cl
m_raw_cql_head_writer
m_resurrection_path_starts_staging
m_fence_before_stage
m_initializer_loses_its_condition
m_initializer_loses_null_head_clause
m_initializer_loses_created_at_clause
restore
green "PC-0 inventory mutations are red (12/12)"
