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
LIBS=internal/api/v2/libraries.go
CONST=internal/db/library_head_serial.go
GC=internal/gc/store_cassandra.go
BACKUPS=()
CREATED_FILES=()

green() { printf '\033[32m%s\033[0m\n' "$*"; }
red() { printf '\033[31m%s\033[0m\n' "$*" >&2; }
restore() {
  local f
  for f in "${BACKUPS[@]:-}"; do
    if [ -n "$f" ] && [ -f "$f.headserialbak" ]; then mv -f "$f.headserialbak" "$f"; fi
  done
  BACKUPS=()
  for f in "${CREATED_FILES[@]:-}"; do
    if [ -n "$f" ] && [ -e "$f" ]; then rm -f "$f"; fi
  done
  CREATED_FILES=()
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

create_mutation_file() {
  local f="$1"
  [ ! -e "$f" ] || fail "mutation file already exists: $f"
  cat > "$f"
  CREATED_FILES+=("$f")
  [ -s "$f" ] || fail "mutation file empty: $f"
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
  expect_red '^TestPC0HeadAuthorityDeleteGuardsAreInventoried$' 'unlisted competing HEAD mutation' \
    'M7 hidden DELETE IF created_at then head_commit_id'
}

m_hidden_delete_qualified_table() {
  restore
  mutate "$FILES" 's@(func \(h \*FileHandler\) CreateFile\(c \*gin.Context\) \{)@func (h *FileHandler) pc0HiddenQualifiedHeadDeleteGuard(orgID, repoID string) error {\n	_, err := h.db.Session().Query(`DELETE FROM sesamefs.libraries WHERE org_id = ? AND library_id = ? IF head_commit_id = null`, orgID, repoID).MapScanCAS(map[string]interface{}{})\n	return err\n}\n\n$1@'
  expect_red '^TestPC0HeadAuthorityDeleteGuardsAreInventoried$' 'unlisted competing HEAD mutation' \
    'M8 hidden DELETE FROM sesamefs.libraries IF head_commit_id'
}

m_unresolvable_delete_query() {
  restore
  # A Query whose first argument is not a source-resolvable string (const,
  # ident, or concatenation of those) used to vanish from a BasicLit walk.
  # Fail-closed: constructed CQL must be allowlisted, not silently omitted.
  mutate "$FILES" 's@(func \(h \*FileHandler\) CreateFile\(c \*gin.Context\) \{)@func (h *FileHandler) pc0HiddenUnresolvableHeadDeleteGuard(orgID, repoID string) error {\n	_, err := h.db.Session().Query(fmt.Sprintf(`DELETE FROM libraries WHERE org_id = ? AND library_id = ? IF head_commit_id = null`), orgID, repoID).MapScanCAS(map[string]interface{}{})\n	return err\n}\n\n$1@'
  expect_red '^TestPC0HeadAuthorityDeleteGuardsAreInventoried$' 'unresolvable Query/Bind CQL' \
    'M9 hidden Query(fmt.Sprintf(DELETE IF head_commit_id))'
}

m_hidden_package_func_lit_delete() {
  restore
  # A package-level var FuncLit is not a FuncDecl; walking only functions
  # would leave this DELETE IF invisible.
  mutate "$FILES" 's@(func \(h \*FileHandler\) CreateFile\(c \*gin.Context\) \{)@var pc0HiddenHeadGuardFn = func(h *FileHandler, orgID, repoID string) error {\n	_, err := h.db.Session().Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ? IF head_commit_id = null`, orgID, repoID).MapScanCAS(map[string]interface{}{})\n	return err\n}\n\n$1@'
  expect_red '^TestPC0HeadAuthorityDeleteGuardsAreInventoried$' 'unlisted competing HEAD mutation' \
    'M10 hidden package-level var FuncLit DELETE IF head_commit_id'
}

m_allowlisted_update_library_becomes_head_lwt() {
  restore
  # UpdateLibrary is already allowlisted for one unresolved Query. Changing
  # that Query into a HEAD LWT must not stay green on count alone.
  mutate "$LIBS" 's@query \+= " WHERE org_id = \? AND library_id = \?"@query += " WHERE org_id = ? AND library_id = ? IF head_commit_id = null"@'
  expect_red '^TestPC0UnresolvedHeadQueriesStayOutOfHeadDomain$' 'allowlisted UpdateLibrary unresolved Query shape' \
    'M11 allowlisted UpdateLibrary suffix becomes IF head_commit_id'
}

m_allowlisted_update_library_dynamic_set_fragment() {
  restore
  # A SET fragment that is not an inline literal used to be skipped. The
  # shape pin must fail closed rather than ignore it.
  mutate "$LIBS" 's@updates = append\(updates, "name = \?"\)@fragment := "head_commit_id = ?"\n		updates = append(updates, fragment)@'
  expect_red '^TestPC0UnresolvedHeadQueriesStayOutOfHeadDomain$' 'SET fragment "head_commit_id = ?" is not in the non-HEAD column list' \
    'M12 allowlisted UpdateLibrary dynamic SET fragment becomes head_commit_id'
}

m_allowlisted_lock_sprintf_format_unresolvable() {
  restore
  # acquireHardDeleteLock has two fmt.Sprintf formats. Making the takeover
  # format dynamic while leaving the INSERT literal must not stay green.
  mutate "$GC" 's@fmt.Sprintf\(`(\n		UPDATE %s USING TTL %d\n		SET started_at = \?, heartbeat = \?, lease_token = \?\n		WHERE %s = \? IF lease_token = \?\n	)`, tableName, hardDeleteLockTTLSeconds, keyColumn\), now, now@fmt.Sprintf(string([]byte(`$1`)), tableName, hardDeleteLockTTLSeconds, keyColumn), now, now@'
  expect_red '^TestPC0UnresolvedHeadQueriesStayOutOfHeadDomain$' 'fmt.Sprintf format is not a source-resolvable string' \
    'M13 allowlisted acquireHardDeleteLock second fmt.Sprintf format is dynamic'
}

m_embedded_migration_head_lwt() {
  restore
  # Migrator.apply is allowlisted because it ranges over checked-in CQL.
  # A new embedded migration that competes for HEAD must fail closed.
  create_mutation_file internal/db/migrations/099_pc0_hidden_head_lwt.cql <<'EOF'
UPDATE libraries SET head_commit_id = ? WHERE org_id = ? AND library_id = ? IF head_commit_id = ?;
EOF
  expect_red '^TestPC0UnresolvedHeadQueriesStayOutOfHeadDomain$' 'embedded migration 099_pc0_hidden_head_lwt.cql competes for libraries.head_commit_id' \
    'M14 embedded migration UPDATE libraries IF head_commit_id'
}

m_embedded_migration_set_head_if_exists() {
  restore
  # An UPDATE that writes head_commit_id under IF EXISTS used to miss the
  # classifier because IF does not name the column.
  create_mutation_file internal/db/migrations/098_pc0_hidden_head_set_if_exists.cql <<'EOF'
UPDATE libraries SET head_commit_id = 'H2' WHERE org_id = ? AND library_id = ? IF EXISTS;
EOF
  expect_red '^TestPC0UnresolvedHeadQueriesStayOutOfHeadDomain$' 'embedded migration 098_pc0_hidden_head_set_if_exists.cql competes for libraries.head_commit_id' \
    'M15 embedded migration UPDATE libraries SET head_commit_id IF EXISTS'
}

m_embedded_migration_whole_row_delete_if_exists() {
  restore
  # A whole-row DELETE IF EXISTS of libraries removes head_commit_id even
  # though IF does not name the column.
  create_mutation_file internal/db/migrations/097_pc0_hidden_whole_row_delete_if_exists.cql <<'EOF'
DELETE FROM libraries WHERE org_id = ? AND library_id = ? IF EXISTS;
EOF
  expect_red '^TestPC0UnresolvedHeadQueriesStayOutOfHeadDomain$' 'embedded migration 097_pc0_hidden_whole_row_delete_if_exists.cql competes for libraries.head_commit_id' \
    'M16 embedded migration DELETE FROM libraries IF EXISTS'
}

m_hidden_concat_head_update() {
  restore
  # A concat UPDATE that writes head_commit_id is resolvable, so it never
  # hits the unresolved allowlist, and neither half is a raw BasicLit
  # `UPDATE libraries ... head_commit_id` writer. Query/Bind must still
  # classify it as a competing HEAD mutation.
  mutate "$FILES" 's@(func \(h \*FileHandler\) CreateFile\(c \*gin.Context\) \{)@func (h *FileHandler) pc0HiddenConcatHeadUpdate(orgID, repoID, head string) error {\n	stmt := "UPDATE libraries SET " + "head_commit_id = ? WHERE org_id = ? AND library_id = ? IF EXISTS"\n	_, err := h.db.Session().Query(stmt, head, orgID, repoID).MapScanCAS(map[string]interface{}{})\n	return err\n}\n\n$1@'
  expect_red '^TestPC0HeadAuthorityDeleteGuardsAreInventoried$' 'unlisted competing HEAD mutation' \
    'M17 hidden concat UPDATE libraries SET + head_commit_id IF EXISTS'
}

m_allowlisted_update_library_initializer_preload() {
  restore
  # The shape pin used to inspect append(updates, ...) but not the
  # initializer. Preloading a HEAD SET fragment into updates := []string{...}
  # must not stay green on the remaining allowed appends.
  mutate "$LIBS" 's@updates := \[\]string\{\}\n\tvalues := \[\]interface\{\}\{\}@updates := []string{"head_commit_id = ?"}\n	values := []interface{}{"evil"}@'
  expect_red '^TestPC0UnresolvedHeadQueriesStayOutOfHeadDomain$' 'updates initializer is not empty' \
    'M18 allowlisted UpdateLibrary updates initializer preloads head_commit_id'
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
  m_unresolvable_delete_query
  m_hidden_package_func_lit_delete
  m_allowlisted_update_library_becomes_head_lwt
  m_allowlisted_update_library_dynamic_set_fragment
  m_allowlisted_lock_sprintf_format_unresolvable
  m_embedded_migration_head_lwt
  m_embedded_migration_set_head_if_exists
  m_embedded_migration_whole_row_delete_if_exists
  m_hidden_concat_head_update
  m_allowlisted_update_library_initializer_preload
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
green "library HEAD SERIAL-domain mutations are red (18/18)"
