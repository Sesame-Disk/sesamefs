#!/usr/bin/env bash
# PC-D1 real-Cassandra proof for the moving-HEAD witness rule.
#
# The table is deliberately test-only and is created/dropped in the ephemeral
# 3-DC fixture. No product migration or runtime code is involved. The script
# proves that a certifier which observed H cannot certify H after another DC
# advances the canonical HEAD to H'.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

THREE_DC=(docker compose --project-name sesamefs-pcd1 --progress quiet -f docker-compose.cassandra-3dc.yaml)
FIXTURE_STARTED=0
TABLE=pcd1_continuity_probe
RUN_ID="pcd1-$(date +%s)-$$"
ORG="org-$RUN_ID"
LIB="library-$RUN_ID"
H=H
H_NEXT=H-prime
VERSION=V1

step() { printf '\033[1m==> %s\033[0m\n' "$*"; }
fail() { printf '\033[31mFAILED: %s\033[0m\n' "$*" >&2; exit 1; }

cql() {
	local node="$1" statement="$2"
	docker exec "sesamefs-cassandra-$node" cqlsh -e "$statement"
}

head_value() {
	local node="$1"
	cql "$node" "CONSISTENCY LOCAL_QUORUM; SELECT head FROM ${CASSANDRA_KEYSPACE:-sesamefs}.${TABLE} WHERE org_id='$ORG' AND library_id='$LIB';" \
		| tr -d '\r' \
		| awk '{ value=$0; gsub(/^[[:space:]]+|[[:space:]]+$/, "", value); if (value == "H" || value == "H-prime") { print value; exit } }' \
		| tr -cd '[:alnum:]-'
}

cleanup() {
	local rc=$?
	local cleanup_rc=0
	set +e
	if [ "$FIXTURE_STARTED" -eq 1 ]; then
		for node in na eu asia; do
			if ! docker exec "sesamefs-cassandra-$node" nodetool enablehandoff >/dev/null 2>&1; then
				printf '\033[31mFAILED: cleanup could not re-enable hinted handoff on %s\033[0m\n' "$node" >&2
				cleanup_rc=1
			fi
			if ! docker exec "sesamefs-cassandra-$node" cqlsh -e "DROP TABLE IF EXISTS ${CASSANDRA_KEYSPACE:-sesamefs}.${TABLE};" >/dev/null 2>&1; then
				printf '\033[31mFAILED: cleanup could not drop %s on %s\033[0m\n' "$TABLE" "$node" >&2
				cleanup_rc=1
			fi
		done
		if ! "${THREE_DC[@]}" down -v >/dev/null 2>&1; then
			printf '\033[31mFAILED: cleanup could not remove the 3-DC fixture\033[0m\n' >&2
			cleanup_rc=1
		fi
	fi
	if [ "$rc" -eq 0 ] && [ "$cleanup_rc" -ne 0 ]; then
		printf '\033[31mFAILED: cleanup failed after an otherwise successful proof\033[0m\n' >&2
		rc=1
	fi
	exit "$rc"
}
trap cleanup EXIT INT TERM

wait_healthy() {
	local node="$1" status
	for _ in $(seq 1 120); do
		status="$(docker inspect -f '{{.State.Health.Status}}' "sesamefs-cassandra-$node" 2>/dev/null || true)"
		[ "$status" = healthy ] && return 0
		sleep 5
	done
	fail "cassandra-$node did not become healthy"
}

wait_bootstrap() {
	local status
	for _ in $(seq 1 120); do
		status="$(docker inspect -f '{{.State.Status}}:{{.State.ExitCode}}' sesamefs-cassandra-3dc-bootstrap 2>/dev/null || true)"
		[ "$status" = exited:0 ] && return 0
		case "$status" in
			exited:*) docker logs sesamefs-cassandra-3dc-bootstrap | tail -40; fail "3-DC bootstrap failed: $status" ;;
		esac
		sleep 5
	done
	fail "3-DC bootstrap did not finish"
}

step "Refuse fixed-container collisions with another Docker environment"
for container in sesamefs-cassandra-na sesamefs-cassandra-eu sesamefs-cassandra-asia sesamefs-cassandra-3dc-bootstrap; do
	if docker inspect "$container" >/dev/null 2>&1; then
		fail "fixture container $container already exists; refusing to touch another environment"
	fi
done

step "Start the real three-DC Cassandra fixture"
FIXTURE_STARTED=1
"${THREE_DC[@]}" up -d
for node in na eu asia; do wait_healthy "$node"; done
wait_bootstrap

step "Create an ephemeral witness table and seed HEAD=H globally"
cql na "CREATE TABLE IF NOT EXISTS ${CASSANDRA_KEYSPACE:-sesamefs}.${TABLE} (org_id text, library_id text, head text, certified_head text, contract text, PRIMARY KEY ((org_id), library_id));"
cql na "CONSISTENCY LOCAL_QUORUM; INSERT INTO ${CASSANDRA_KEYSPACE:-sesamefs}.${TABLE} (org_id, library_id, head) VALUES ('$ORG', '$LIB', '$H');"
cql na "CONSISTENCY EACH_QUORUM; SELECT head FROM ${CASSANDRA_KEYSPACE:-sesamefs}.${TABLE} WHERE org_id='$ORG' AND library_id='$LIB';" >/dev/null

step "Observe H from dc-eu, then advance the canonical HEAD to H' while dc-eu is down"
observed="$(head_value eu)"
[ "$observed" = "$H" ] || fail "dc-eu did not observe H before the move (observed: $observed)"
for node in na eu asia; do docker exec "sesamefs-cassandra-$node" nodetool disablehandoff >/dev/null; done
"${THREE_DC[@]}" stop cassandra-eu
cql na "CONSISTENCY LOCAL_QUORUM; SERIAL CONSISTENCY SERIAL; UPDATE ${CASSANDRA_KEYSPACE:-sesamefs}.${TABLE} SET head='$H_NEXT' WHERE org_id='$ORG' AND library_id='$LIB' IF head='$H';" | grep -q 'True' || fail "canonical H -> H' CAS did not apply"

step "Restart dc-eu without hinted handoff and prove its local view is stale"
"${THREE_DC[@]}" start cassandra-eu
wait_healthy eu
blind="$(head_value eu)"
[ "$blind" = "$H" ] || fail "dc-eu was not deliberately blind to H' (local view: $blind)"

step "Reject stale certification of H through the global serial witness condition"
stale_result="$(cql eu "CONSISTENCY LOCAL_QUORUM; SERIAL CONSISTENCY SERIAL; UPDATE ${CASSANDRA_KEYSPACE:-sesamefs}.${TABLE} SET certified_head='$H', contract='$VERSION' WHERE org_id='$ORG' AND library_id='$LIB' IF head='$H';")"
echo "$stale_result" | grep -q 'False' || fail "stale certification unexpectedly applied: $stale_result"

canonical="$(cql na "CONSISTENCY SERIAL; SELECT head, certified_head, contract FROM ${CASSANDRA_KEYSPACE:-sesamefs}.${TABLE} WHERE org_id='$ORG' AND library_id='$LIB';")"
echo "$canonical" | grep -q "$H_NEXT" || fail "canonical HEAD is not H': $canonical"
echo "$canonical" | grep -Eq "H-prime[[:space:]]*\\|[[:space:]]*(null|None)?[[:space:]]*\\|" || fail "stale attempt left a witness behind: $canonical"

step "Accept only a fresh certification of H'"
cql asia "CONSISTENCY LOCAL_QUORUM; SERIAL CONSISTENCY SERIAL; UPDATE ${CASSANDRA_KEYSPACE:-sesamefs}.${TABLE} SET certified_head='$H_NEXT', contract='$VERSION' WHERE org_id='$ORG' AND library_id='$LIB' IF head='$H_NEXT';" | grep -q 'True' || fail "fresh H' certification did not apply"

step "PC-D1 moving-HEAD witness proof passed"
