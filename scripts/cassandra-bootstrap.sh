#!/usr/bin/env bash
# One-shot Cassandra bootstrap for production: rotates the built-in superuser password,
# creates the keyspace, replicates system_auth to every DC, and creates the application
# role with its grants. Every statement is idempotent; run it again whenever needed.
#
# Runs inside the cassandra image, from either production path:
#   - docker-compose.prod.yml: the cassandra-bootstrap service (profile "bootstrap")
#   - Swarm: docker run --network host ... with CASSANDRA_BOOTSTRAP_HOST set to the
#     node's private IP (see docs/DEPLOY.md)
#
# Reads from the environment: CASSANDRA_SUPERUSER_PASSWORD, CASSANDRA_USERNAME,
# CASSANDRA_PASSWORD, CASSANDRA_KEYSPACE, STORAGE_MODE, CASSANDRA_DC / CASSANDRA_LOCAL_DC,
# CASSANDRA_REPLICATION_CLASS, CASSANDRA_REPLICATION_FACTOR, CASSANDRA_REPLICATION_DCS,
# CASSANDRA_BOOTSTRAP_ATTEMPTS and CASSANDRA_BOOTSTRAP_HOST (default: cassandra).
set -euo pipefail
host="${CASSANDRA_BOOTSTRAP_HOST:-cassandra}"
admin_pass="${CASSANDRA_SUPERUSER_PASSWORD:?set CASSANDRA_SUPERUSER_PASSWORD in .env}"
app_user="${CASSANDRA_USERNAME:?set CASSANDRA_USERNAME in .env}"
app_pass="${CASSANDRA_PASSWORD:?set CASSANDRA_PASSWORD in .env}"
keyspace="${CASSANDRA_KEYSPACE:-sesamefs}"
storage_mode="$(printf '%s' "${STORAGE_MODE:-multi}" | tr -d '[:space:]' | tr '[:upper:]' '[:lower:]')"
local_dc="${CASSANDRA_DC:-${CASSANDRA_LOCAL_DC:-datacenter1}}"
replication_class="${CASSANDRA_REPLICATION_CLASS:-NetworkTopologyStrategy}"
replication_factor="${CASSANDRA_REPLICATION_FACTOR:-1}"
replication_dcs="${CASSANDRA_REPLICATION_DCS:-${local_dc}:1}"
admin_pass_escaped="${admin_pass//\'/\'\'}"
app_pass_escaped="${app_pass//\'/\'\'}"
attempts=0
admin_rotated=0

if [ "${storage_mode}" = "multi" ] && [ -z "${CASSANDRA_REPLICATION_DCS:-}" ]; then
  echo "STORAGE_MODE=multi requires CASSANDRA_REPLICATION_DCS to be set explicitly with every datacenter." >&2
  exit 1
fi

build_replication_cql() {
  local normalized_class
  normalized_class="$(printf '%s' "${replication_class}" | tr -d '[:space:]' | tr '[:upper:]' '[:lower:]')"

  case "${normalized_class}" in
    simplestrategy)
      echo "Warning: SimpleStrategy is only kept for backward compatibility; prefer NetworkTopologyStrategy even for single-node deployments." >&2
      case "${replication_factor}" in
        ''|*[!0-9]*) echo "Invalid CASSANDRA_REPLICATION_FACTOR: ${replication_factor}" >&2; exit 1 ;;
      esac
      if [ "${replication_factor}" -lt 1 ]; then
        echo "CASSANDRA_REPLICATION_FACTOR must be greater than zero" >&2
        exit 1
      fi
      printf "{'class': 'SimpleStrategy', 'replication_factor': %s}" "${replication_factor}"
      ;;
    networktopologystrategy)
      if [ -z "${replication_dcs//[[:space:]]/}" ]; then
        echo "CASSANDRA_REPLICATION_DCS is required when CASSANDRA_REPLICATION_CLASS=NetworkTopologyStrategy" >&2
        exit 1
      fi
      local map entry dc rf has_local_dc
      has_local_dc=false
      map="{'class': 'NetworkTopologyStrategy'"
      IFS=',' read -r -a replication_entries <<< "${replication_dcs}"
      for entry in "${replication_entries[@]}"; do
        entry="${entry//[[:space:]]/}"
        [ -n "${entry}" ] || continue
        if [[ "${entry}" != *:* ]]; then
          echo "Invalid CASSANDRA_REPLICATION_DCS entry: ${entry}" >&2
          exit 1
        fi
        dc="${entry%%:*}"
        rf="${entry#*:}"
        if [[ ! "${dc}" =~ ^[A-Za-z0-9._-]+$ ]]; then
          echo "Invalid Cassandra datacenter name in CASSANDRA_REPLICATION_DCS: ${dc}" >&2
          exit 1
        fi
        case "${rf}" in
          ''|*[!0-9]*) echo "Invalid replication factor for datacenter ${dc}: ${rf}" >&2; exit 1 ;;
        esac
        if [ "${rf}" -lt 1 ]; then
          echo "Replication factor must be greater than zero for datacenter ${dc}" >&2
          exit 1
        fi
        map="${map}, '${dc}': ${rf}"
        if [ -n "${local_dc}" ] && [ "${dc}" = "${local_dc}" ]; then
          has_local_dc=true
        fi
      done
      if [ "${map}" = "{'class': 'NetworkTopologyStrategy'" ]; then
        echo "CASSANDRA_REPLICATION_DCS must contain at least one dc:rf pair" >&2
        exit 1
      fi
      if [ -n "${local_dc}" ] && [ "${has_local_dc}" = "false" ]; then
        echo "CASSANDRA_REPLICATION_DCS does not include the local datacenter '${local_dc}'. Add it to CASSANDRA_REPLICATION_DCS or fix CASSANDRA_DC/CASSANDRA_LOCAL_DC." >&2
        exit 1
      fi
      printf "%s}" "${map}"
      ;;
    *)
      echo "Invalid CASSANDRA_REPLICATION_CLASS: ${replication_class}" >&2
      exit 1
      ;;
  esac
}

replication_cql="$(build_replication_cql)"

case "${keyspace}" in
  [A-Za-z][A-Za-z0-9_]*) ;;
  *) echo "Invalid Cassandra keyspace identifier: ${keyspace}" >&2; exit 1 ;;
esac
case "${app_user}" in
  [A-Za-z][A-Za-z0-9_]*) ;;
  *) echo "Invalid Cassandra role identifier: ${app_user}" >&2; exit 1 ;;
esac

until cqlsh "${host}" -u cassandra -p "${admin_pass}" -e "DESCRIBE KEYSPACES" >/dev/null 2>&1; do
  if cqlsh "${host}" -u cassandra -p cassandra -e "DESCRIBE KEYSPACES" >/dev/null 2>&1; then
    if [ "${admin_rotated}" -eq 0 ]; then
      cqlsh "${host}" -u cassandra -p cassandra -e "ALTER ROLE cassandra WITH PASSWORD = '${admin_pass_escaped}' AND LOGIN = true;"
      admin_rotated=1
    fi

    echo "Waiting for Cassandra superuser password rotation to settle..." >&2
    sleep 5
    continue
  fi

  attempts=$((attempts + 1))
  if [ "${attempts}" -ge 30 ]; then
    echo "Timed out waiting for Cassandra superuser bootstrap" >&2
    exit 1
  fi

  echo "Waiting for Cassandra auth tables to finish initializing..." >&2
  sleep 2
done

apply_bootstrap_cql() {
  cqlsh "${host}" -u cassandra -p "${admin_pass}" <<EOF
CREATE KEYSPACE IF NOT EXISTS ${keyspace} WITH replication = ${replication_cql};
ALTER KEYSPACE ${keyspace} WITH replication = ${replication_cql};
ALTER KEYSPACE system_auth WITH replication = ${replication_cql};
CREATE ROLE IF NOT EXISTS ${app_user} WITH PASSWORD = '${app_pass_escaped}' AND LOGIN = true;
GRANT ALL PERMISSIONS ON KEYSPACE ${keyspace} TO ${app_user};
EOF
}

app_role_has_grants() {
  # grep reads the whole listing on purpose: grep -q exits at the first match,
  # which sends SIGPIPE to cqlsh and fails the pipeline under pipefail.
  cqlsh "${host}" -u cassandra -p "${admin_pass}" -e "LIST ALL PERMISSIONS OF ${app_user};" 2>/dev/null | grep "<keyspace ${keyspace}>" >/dev/null
}

# On a fresh multi-DC cluster the first run can fail right after the system_auth
# replication change, before the role exists (ISSUE-CASSANDRA-BOOTSTRAP-3DC-ROLE-01).
# Every statement is idempotent, so retry until the role holds its grants.
max_attempts="${CASSANDRA_BOOTSTRAP_ATTEMPTS:-12}"
case "${max_attempts}" in
  ''|*[!0-9]*|0) echo "CASSANDRA_BOOTSTRAP_ATTEMPTS must be a positive integer, got '${max_attempts}'" >&2; exit 1 ;;
esac
attempt=1
until apply_bootstrap_cql && app_role_has_grants; do
  if [ "${attempt}" -ge "${max_attempts}" ]; then
    echo "Cassandra bootstrap did not converge after ${attempt} attempt(s)" >&2
    exit 1
  fi
  echo "Cassandra bootstrap attempt ${attempt} did not converge; retrying in 15s" >&2
  attempt=$((attempt + 1))
  sleep 15
done
echo "Cassandra bootstrap converged after ${attempt} attempt(s)"

sleep 5
cqlsh "${host}" -u cassandra -p "${admin_pass}" -e "ALTER ROLE ${app_user} WITH PASSWORD = '${app_pass_escaped}' AND LOGIN = true;"
