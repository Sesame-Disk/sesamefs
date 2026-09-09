#!/usr/bin/env bash
# Mutations that prove the PC-0 inventory/consistency guards actually fail closed.
set -uo pipefail
cd "$(dirname "$0")/.."

FILES=internal/api/v2/files.go
REFS=internal/db/block_references.go
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

m_drop_funnel_stage_seam() {
  restore
  mutate "$FILES" 's@if err := fsHelper.stagePendingPublishedFiles\(orgID, repoID, commitID, pendingFiles\)@if err := fsHelper.notAPublicationStage(orgID, repoID, commitID, pendingFiles)@'
  expect_red '^TestPC0BlockPublicationFunnelsHaveMappedSeams$' 'v2/CreateFile missing seam calls' 'CreateFile without stage seam'
}

m_downgrade_sync_provenance_cl() {
  restore
  mutate "$REFS" 's@referrer = \?\n\t`, orgID, blockID, referrer\).Consistency\(gocql.LocalQuorum\)@referrer = ?\n	`, orgID, blockID, referrer).Consistency(gocql.One)@'
  expect_red '^TestPC0CriticalConsistencyPrimitivesArePinned$' 'BlockReferenceExistsLocalQuorum' 'Sync provenance CL downgraded from LOCAL_QUORUM'
}

m_untracked_head_publisher
m_drop_funnel_stage_seam
m_downgrade_sync_provenance_cl
restore
green "PC-0 inventory mutations are red"
