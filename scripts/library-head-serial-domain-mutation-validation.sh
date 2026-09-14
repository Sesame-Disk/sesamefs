#!/usr/bin/env bash
# Mutations that prove the canonical library HEAD SERIAL-domain pin
# (ISSUE-LIBRARY-HEAD-SERIAL-DOMAIN-01) fails closed.
#
#   ./scripts/library-head-serial-domain-mutation-validation.sh
#   ./scripts/library-head-serial-domain-mutation-validation.sh <name>
#   ./scripts/library-head-serial-domain-mutation-validation.sh --list
#
# Unit-level only: no Cassandra, MinIO, or application stack is required.
# Mutations stay compilable; the evidence is a protocol-incorrect pin that
# the PC-0 HEAD serial-domain tests detect.
set -uo pipefail
cd "$(dirname "$0")/.."

FSH=internal/api/v2/fs_helpers.go
SYNC=internal/api/sync.go
WH=internal/api/v2/write_helpers.go
FILES=internal/api/v2/files.go
CONST=internal/db/library_head_serial.go
BACKUPS=()

green() { printf '\033[32m%s\033[0m\n' "$*"; }
red() { printf '\033[31m%s\033[0m\n' "$*" >&2; }
restore() {
  local f
  for f in "${BACKUPS[@]:-}"; do
    if [ -n "$f" ] && [ -f "$f.headserialbak" ]; then mv -f "$f.headserialbak" "$f"; fi
  done
  BACKUPS=()
}
fail() { red "FAILED: $*"; restore; exit 1; }
trap restore EXIT INT TERM

mutate() {
  local f="$1" expr="$2"
  cp "$f" "$f.headserialbak"
  BACKUPS+=("$f")
  perl -0pi -e "$expr" "$f"
  cmp -s "$f" "$f.headserialbak" && fail "mutation did not apply to $f"
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

m_v2_update_local_serial() {
  restore
  mutate "$FSH" 's{(UPDATE libraries SET head_commit_id = \?, size_bytes = \?, file_count = \?, updated_at = \?.*?SerialConsistency\()db\.LibraryHeadSerialConsistency}{$1gocql.LocalSerial}s'
  expect_red '^TestPC0HeadSerialDomainPinsGlobalSerial$|^TestPC0CriticalConsistencyPrimitivesArePinned$' 'SerialConsistency(gocql.LocalSerial)' \
    'M1 v2 UpdateLibraryHead SERIAL -> LOCAL_SERIAL'
}

m_sync_update_local_serial() {
  restore
  mutate "$SYNC" 's{(UPDATE libraries SET head_commit_id = \?, updated_at = \?, size_bytes = \?, file_count = \?.*?SerialConsistency\()db\.LibraryHeadSerialConsistency}{$1gocql.LocalSerial}s'
  expect_red '^TestPC0HeadSerialDomainPinsGlobalSerial$|^TestPC0CriticalConsistencyPrimitivesArePinned$' 'SerialConsistency(gocql.LocalSerial)' \
    'M2 Sync updateLibraryHeadWithStats SERIAL -> LOCAL_SERIAL'
}

m_initializer_local_serial() {
  restore
  mutate "$FSH" 's{(UPDATE libraries SET head_commit_id = \?, root_commit_id = \?.*?SerialConsistency\()db\.LibraryHeadSerialConsistency}{$1gocql.LocalSerial}s'
  expect_red '^TestPC0HeadSerialDomainPinsGlobalSerial$|^TestPC0CriticalConsistencyPrimitivesArePinned$' 'SerialConsistency(gocql.LocalSerial)' \
    'M3 InitializeLibraryHeadIfUnset SERIAL -> LOCAL_SERIAL'
}

m_rollback_local_serial() {
  restore
  mutate "$WH" 's{(DELETE FROM libraries WHERE org_id = \? AND library_id = \? IF head_commit_id = null.*?SerialConsistency\()dbpkg\.LibraryHeadSerialConsistency}{$1gocql.LocalSerial}s'
  expect_red '^TestPC0HeadSerialDomainPinsGlobalSerial$|^TestPC0CriticalConsistencyPrimitivesArePinned$' 'SerialConsistency(gocql.LocalSerial)' \
    'M4 deleteUnpublishedLibraryRow SERIAL -> LOCAL_SERIAL'
}

m_remove_explicit_pin() {
  restore
  mutate "$FSH" 's{(UPDATE libraries SET head_commit_id = \?, size_bytes = \?, file_count = \?, updated_at = \?[\s\S]*?\)\.\s*)SerialConsistency\(db\.LibraryHeadSerialConsistency\)\.\s*}{$1}'
  expect_red '^TestPC0HeadSerialDomainPinsGlobalSerial$' 'must call SerialConsistency(LibraryHeadSerialConsistency)' \
    'M5 remove explicit SerialConsistency from UpdateLibraryHead'
}

m_constant_local_serial() {
  restore
  mutate "$CONST" 's{const LibraryHeadSerialConsistency = gocql\.Serial}{const LibraryHeadSerialConsistency = gocql.LocalSerial}'
  expect_red '^TestLibraryHeadSerialConsistencyIsGlobalSerial$' 'must be gocql.Serial' \
    'M6 LibraryHeadSerialConsistency = gocql.LocalSerial'
}

m_hidden_delete_if_other_column_first() {
  restore
  # A DELETE IF whose first predicate is not head_commit_id used to miss the
  # name-literal inventory. The R12-style scanner must still list it.
  mutate "$FILES" 's@(func \(h \*FileHandler\) CreateFile\(c \*gin.Context\) \{)@func (h *FileHandler) pc0HiddenHeadDeleteGuard(orgID, repoID string) error {\n	_, err := h.db.Session().Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ? IF created_at = ? AND head_commit_id = null`, orgID, repoID, nil).MapScanCAS(map[string]interface{}{})\n	return err\n}\n\n$1@'
  expect_red '^TestPC0HeadAuthorityDeleteGuardsAreInventoried$' 'unlisted DELETE FROM libraries IF head_commit_id' \
    'M7 hidden DELETE IF created_at then head_commit_id'
}

m_hidden_delete_qualified_table() {
  restore
  mutate "$FILES" 's@(func \(h \*FileHandler\) CreateFile\(c \*gin.Context\) \{)@func (h *FileHandler) pc0HiddenQualifiedHeadDeleteGuard(orgID, repoID string) error {\n	_, err := h.db.Session().Query(`DELETE FROM sesamefs.libraries WHERE org_id = ? AND library_id = ? IF head_commit_id = null`, orgID, repoID).MapScanCAS(map[string]interface{}{})\n	return err\n}\n\n$1@'
  expect_red '^TestPC0HeadAuthorityDeleteGuardsAreInventoried$' 'unlisted DELETE FROM libraries IF head_commit_id' \
    'M8 hidden DELETE FROM sesamefs.libraries IF head_commit_id'
}

ALL_MUTATIONS=(
  m_v2_update_local_serial
  m_sync_update_local_serial
  m_initializer_local_serial
  m_rollback_local_serial
  m_remove_explicit_pin
  m_constant_local_serial
  m_hidden_delete_if_other_column_first
  m_hidden_delete_qualified_table
)

if [ "${1:-}" = "--list" ]; then
  printf '%s\n' "${ALL_MUTATIONS[@]}"
  exit 0
fi

if [ -n "${1:-}" ]; then
  found=0
  for m in "${ALL_MUTATIONS[@]}"; do
    if [ "$m" = "$1" ]; then
      found=1
      "$m"
      restore
      green "library HEAD SERIAL-domain mutation $1 is red (1/1)"
      exit 0
    fi
  done
  fail "unknown mutation $1"
fi

for m in "${ALL_MUTATIONS[@]}"; do
  "$m"
done
restore
green "library HEAD SERIAL-domain mutations are red (8/8)"
