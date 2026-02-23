#!/bin/bash
# =============================================================================
# make-superadmin.sh — Promote a user to superadmin in the SesameFS platform org
# =============================================================================
#
# Creates or updates a user record directly in Cassandra, placing them in
# the platform org (00000000-0000-0000-0000-000000000000) with the superadmin
# role. This allows the user to access organization management endpoints.
#
# After running this script the user must log out and log back in via OIDC
# so that a new session is issued with the updated org_id and role.
#
# IMPORTANT — OIDC re-login behavior:
#   If OIDC is configured with an org_claim, re-login may re-assign the user
#   to their OIDC-claimed org. To persist superadmin status across OIDC logins,
#   configure OIDC_PLATFORM_ORG_CLAIM_VALUE in your .env so that the OIDC
#   provider sends the matching org claim for this user (see docs/OIDC.md).
#   Alternatively, disable OIDC and use the dev-token-superadmin token.
#
# Usage:
#   ./scripts/make-superadmin.sh <email> [name]
#
# Examples:
#   # Dev/test (uses docker-compose.yaml by default):
#   ./scripts/make-superadmin.sh admin@example.com
#   ./scripts/make-superadmin.sh admin@example.com "Alice Admin"
#
#   # Production (explicit compose file):
#   ./scripts/make-superadmin.sh -f docker-compose.prod.yml admin@example.com
#
# Options:
#   -f/--file    Docker Compose file (default: docker-compose.yaml)
#   --keyspace   Cassandra keyspace (default: sesamefs from env or default)
#   --container  Cassandra container name (default: cassandra)
#   --host       Direct Cassandra host:port instead of docker (e.g. localhost:9042)
#   --username   Cassandra username (or set CASSANDRA_USERNAME env var)
#   --password   Cassandra password (or set CASSANDRA_PASSWORD env var)
#   --help       Show this help
#
# Requirements:
#   - Docker + docker compose OR direct cqlsh access
#   - Cassandra running with sesamefs keyspace already created
#
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Colors
# ---------------------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# ---------------------------------------------------------------------------
# Defaults — all can be overridden by env vars or flags
# ---------------------------------------------------------------------------
PLATFORM_ORG_ID="00000000-0000-0000-0000-000000000000"
KEYSPACE="${CASSANDRA_KEYSPACE:-sesamefs}"
CONTAINER_NAME="cassandra"
COMPOSE_FILE=""                        # empty = use docker compose default (docker-compose.yaml)
DIRECT_HOST=""
CASS_USER="${CASSANDRA_USERNAME:-}"   # empty = no auth (dev default)
CASS_PASS="${CASSANDRA_PASSWORD:-}"
EMAIL=""
DISPLAY_NAME=""

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
info()    { echo -e "${CYAN}→${NC} $*"; }
success() { echo -e "${GREEN}✓${NC} $*"; }
warn()    { echo -e "${YELLOW}⚠${NC} $*"; }
error()   { echo -e "${RED}✗${NC} $*" >&2; }
fatal()   { error "$*"; exit 1; }
header()  { echo -e "\n${BOLD}${BLUE}$*${NC}"; }

usage() {
  sed -n '/^# Usage:/,/^# ==/p' "$0" | sed 's/^# \?//'
  exit 0
}

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --help|-h)    usage ;;
    -f|--file)    COMPOSE_FILE="$2";   shift 2 ;;
    --keyspace)   KEYSPACE="$2";       shift 2 ;;
    --container)  CONTAINER_NAME="$2"; shift 2 ;;
    --host)       DIRECT_HOST="$2";    shift 2 ;;
    --username|-u) CASS_USER="$2";     shift 2 ;;
    --password|-p) CASS_PASS="$2";     shift 2 ;;
    -*)           fatal "Unknown option: $1" ;;
    *)
      if [[ -z "$EMAIL" ]]; then
        EMAIL="$1"
      elif [[ -z "$DISPLAY_NAME" ]]; then
        DISPLAY_NAME="$1"
      else
        fatal "Unexpected argument: $1"
      fi
      shift
      ;;
  esac
done

[[ -z "$EMAIL" ]] && fatal "Usage: $0 <email> [name]\nRun '$0 --help' for details."

# Default display name = part before @
if [[ -z "$DISPLAY_NAME" ]]; then
  DISPLAY_NAME="${EMAIL%%@*}"
fi

# ---------------------------------------------------------------------------
# Cassandra runner: execute a CQL statement and return output
# ---------------------------------------------------------------------------
cql_exec() {
  local cql="$1"

  # Build optional auth flags
  local auth_flags=()
  [[ -n "$CASS_USER" ]] && auth_flags+=("-u" "$CASS_USER")
  [[ -n "$CASS_PASS" ]] && auth_flags+=("-p" "$CASS_PASS")

  if [[ -n "$DIRECT_HOST" ]]; then
    cqlsh "$DIRECT_HOST" "${auth_flags[@]}" -e "$cql" --keyspace="$KEYSPACE" 2>/dev/null
  else
    # Build compose file flags (-f only when explicitly set)
    local compose_flags=()
    [[ -n "$COMPOSE_FILE" ]] && compose_flags+=("-f" "$COMPOSE_FILE")

    docker compose "${compose_flags[@]}" exec -T "$CONTAINER_NAME" \
      cqlsh "${auth_flags[@]}" -e "$cql" --keyspace="$KEYSPACE" 2>/dev/null
  fi
}

# Variant that ignores errors (for optional checks)
cql_exec_noerr() {
  cql_exec "$1" 2>/dev/null || true
}

# ---------------------------------------------------------------------------
# Check connectivity
# ---------------------------------------------------------------------------
header "SesameFS — Make Superadmin"
echo ""
CASS_TARGET="${DIRECT_HOST:-docker container '$CONTAINER_NAME' (${COMPOSE_FILE:-docker-compose.yaml})}"
info "Target email    : ${BOLD}$EMAIL${NC}"
info "Display name    : ${BOLD}$DISPLAY_NAME${NC}"
info "Platform org ID : ${BOLD}$PLATFORM_ORG_ID${NC}"
info "Cassandra       : ${BOLD}${CASS_TARGET}${NC}"
info "Keyspace        : ${BOLD}$KEYSPACE${NC}"
echo ""

# Quick connectivity test
if ! cql_exec_noerr "SELECT now() FROM system.local;" | grep -q "-"; then
  compose_hint="docker compose${COMPOSE_FILE:+ -f $COMPOSE_FILE}"
  warn "Could not connect to Cassandra via docker compose."
  warn "If you are not using Docker, pass --host <host:port>."
  warn "Make sure the cassandra container is running: ${compose_hint} ps cassandra"
  fatal "Cassandra connection failed."
fi
success "Cassandra reachable"

# ---------------------------------------------------------------------------
# Step 1: Look up existing user by email
# ---------------------------------------------------------------------------
header "Step 1: Looking up user by email..."

LOOKUP_RAW=$(cql_exec "SELECT user_id, org_id FROM users_by_email WHERE email = '$EMAIL';" 2>/dev/null || true)

EXISTING_USER_ID=""
EXISTING_ORG_ID=""

# Parse cqlsh tabular output (skip header rows, grab first data row)
while IFS= read -r line; do
  # Skip blank lines and separator lines
  [[ -z "$line" ]] && continue
  [[ "$line" =~ ^[-\|\ ]+$ ]] && continue
  [[ "$line" =~ ^\ *user_id ]] && continue
  [[ "$line" =~ ^"(0 rows)" ]] && continue
  [[ "$line" =~ ^"(1 rows)" ]] && continue

  # Extract UUID-shaped values (format: " uuid | uuid ")
  if [[ "$line" =~ ([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})[[:space:]]*\|[[:space:]]*([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}) ]]; then
    EXISTING_USER_ID="${BASH_REMATCH[1]}"
    EXISTING_ORG_ID="${BASH_REMATCH[2]}"
    break
  fi
done <<< "$LOOKUP_RAW"

if [[ -n "$EXISTING_USER_ID" ]]; then
  success "Found existing user: user_id=$EXISTING_USER_ID (currently in org $EXISTING_ORG_ID)"
  USER_ID="$EXISTING_USER_ID"

  if [[ "$EXISTING_ORG_ID" == "$PLATFORM_ORG_ID" ]]; then
    warn "User is already in the platform org. Updating role to superadmin anyway..."
  fi
else
  info "User not found — will create a new user record."
  # Generate UUID using python (available in cassandra image) or /proc
  if command -v python3 &>/dev/null; then
    USER_ID=$(python3 -c "import uuid; print(uuid.uuid4())")
  elif command -v python &>/dev/null; then
    USER_ID=$(python -c "import uuid; print(uuid.uuid4())")
  else
    # Fallback: use /proc/sys/kernel/random/uuid (Linux only)
    USER_ID=$(cat /proc/sys/kernel/random/uuid 2>/dev/null || \
      fatal "Cannot generate UUID — install python3 or run on Linux")
  fi
  info "Generated new user_id: $USER_ID"
fi

# ---------------------------------------------------------------------------
# Step 2: Upsert user in platform org with superadmin role
# ---------------------------------------------------------------------------
header "Step 2: Writing user to platform org..."

NOW_CQL="toTimestamp(now())"

# Insert into users table (INSERT uses IF NOT EXISTS semantics in Cassandra,
# but we want to upsert so we use plain INSERT which is always an upsert)
cql_exec "INSERT INTO users (org_id, user_id, email, name, role, quota_bytes, used_bytes, created_at) VALUES ($PLATFORM_ORG_ID, $USER_ID, '$EMAIL', '$DISPLAY_NAME', 'superadmin', -2, 0, $NOW_CQL);"
success "Upserted user in platform org with superadmin role"

# ---------------------------------------------------------------------------
# Step 3: Update users_by_email lookup table
# ---------------------------------------------------------------------------
header "Step 3: Updating email lookup table..."

cql_exec "INSERT INTO users_by_email (email, user_id, org_id) VALUES ('$EMAIL', $USER_ID, $PLATFORM_ORG_ID);"
success "Updated users_by_email → platform org"

# ---------------------------------------------------------------------------
# Step 4: Update users_by_oidc (so OIDC login resolves to platform org)
# ---------------------------------------------------------------------------
header "Step 4: Updating OIDC lookup table..."

# Scan users_by_oidc for entries that point to this user (by user_id or by email match)
OIDC_ALL_RAW=$(cql_exec_noerr "SELECT oidc_issuer, oidc_sub, user_id, org_id FROM users_by_oidc;")

OIDC_UPDATED=0
while IFS='|' read -r col_issuer col_sub col_uid col_oid <&3; do
  # Trim whitespace
  o_issuer=$(echo "$col_issuer" | tr -d ' ')
  o_sub=$(echo "$col_sub" | tr -d ' ')
  o_uid=$(echo "$col_uid" | tr -d ' ')
  o_oid=$(echo "$col_oid" | tr -d ' ')

  # Skip header/separator/empty/count lines
  [[ -z "$o_issuer" ]] && continue
  [[ "$o_issuer" == "oidc_issuer" ]] && continue
  [[ "$o_issuer" =~ ^-+$ ]] && continue
  [[ "$o_issuer" =~ rows ]] && continue

  # Match by user_id (current or previous)
  MATCH=false
  if [[ "$o_uid" == "$USER_ID" || "$o_uid" == "$EXISTING_USER_ID" ]]; then
    MATCH=true
  else
    # Check if this OIDC user_id has the same email in the users table
    EMAIL_CHECK=$(cql_exec_noerr "SELECT email FROM users WHERE org_id = $o_oid AND user_id = $o_uid;")
    if echo "$EMAIL_CHECK" | grep -qi "$EMAIL"; then
      MATCH=true
    fi
  fi

  if [[ "$MATCH" == "true" ]]; then
    info "Found OIDC mapping: issuer=$o_issuer sub=$o_sub → updating to platform org"
    cql_exec "INSERT INTO users_by_oidc (oidc_issuer, oidc_sub, user_id, org_id) VALUES ('$o_issuer', '$o_sub', $USER_ID, $PLATFORM_ORG_ID);"
    OIDC_UPDATED=$((OIDC_UPDATED + 1))
  fi
done 3<<< "$OIDC_ALL_RAW"

if [[ $OIDC_UPDATED -gt 0 ]]; then
  success "Updated $OIDC_UPDATED OIDC mapping(s) → platform org"
else
  warn "No OIDC mappings found for this user (user may not have logged in via OIDC yet)"
  info "On first OIDC login, the user will be recognized by email and assigned to platform org"
fi

# ---------------------------------------------------------------------------
# Step 5: Invalidate existing sessions (so the new role takes effect)
# ---------------------------------------------------------------------------
header "Step 5: Invalidating existing sessions for this user..."

SESSION_COUNT=0
SESSION_RAW=$(cql_exec "SELECT token_hash FROM user_sessions WHERE user_id = $USER_ID ALLOW FILTERING;" 2>/dev/null || true)

while IFS= read -r line; do
  [[ -z "$line" ]] && continue
  [[ "$line" =~ ^[-\|\ ]+$ ]] && continue
  [[ "$line" =~ ^\ *token_hash ]] && continue
  [[ "$line" =~ rows\) ]] && continue

  TOKEN_HASH=$(echo "$line" | tr -d ' |')
  if [[ -n "$TOKEN_HASH" && ${#TOKEN_HASH} -gt 10 ]]; then
    cql_exec_noerr "DELETE FROM user_sessions WHERE token_hash = '$TOKEN_HASH';"
    SESSION_COUNT=$((SESSION_COUNT + 1))
  fi
done <<< "$SESSION_RAW"

if [[ $SESSION_COUNT -gt 0 ]]; then
  success "Invalidated $SESSION_COUNT existing session(s)"
else
  info "No existing sessions found (user was not logged in)"
fi

# ---------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------
echo ""
echo -e "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}${BOLD}  Superadmin created successfully!${NC}"
echo -e "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo -e "  Email    : ${BOLD}$EMAIL${NC}"
echo -e "  User ID  : ${BOLD}$USER_ID${NC}"
echo -e "  Org ID   : ${BOLD}$PLATFORM_ORG_ID (platform)${NC}"
echo -e "  Role     : ${BOLD}superadmin${NC}"
echo ""

echo -e "${YELLOW}${BOLD}Next steps:${NC}"
echo -e "  1. Log out of the web UI (if currently logged in)"
echo -e "  2. Log back in via OIDC — a new session will be issued"
echo -e "     with org_id=${PLATFORM_ORG_ID} and role=superadmin"
echo -e "  3. Navigate to ${BOLD}/sys/organizations/${NC} to manage tenants"
echo ""

echo -e "${YELLOW}${BOLD}OIDC note:${NC}"
echo -e "  If OIDC re-assigns this user to a different org on login,"
echo -e "  configure OIDC_PLATFORM_ORG_CLAIM_VALUE in .env so the provider"
echo -e "  sends the matching org claim for this user. See docs/OIDC.md."
echo ""
