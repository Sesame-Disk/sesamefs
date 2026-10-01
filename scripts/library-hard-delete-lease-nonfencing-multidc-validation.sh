#!/usr/bin/env bash
# Directed 3-DC evidence for ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01.
#
# The stale owner runs in dc-na and the new owner in dc-eu, with both sessions
# defaulting to LOCAL_SERIAL. The old owner renews, pauses, loses the lease,
# and resumes after the new owner committed; the generation-fenced lifecycle
# LWTs (global SERIAL) must reject it. No SesameFS backend is needed: the legs
# call the production helpers against an isolated Cassandra 3-DC fixture.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

DEFAULT_COMPOSE=(docker compose -f docker-compose.yaml)
# Scope everything to this checkout's compose project so a parallel checkout's
# stack or 3-DC fixture is never touched.
COMPOSE_PROJECT="$("${DEFAULT_COMPOSE[@]}" config 2>/dev/null | sed -n 's/^name: //p' | head -1)"
[ -n "$COMPOSE_PROJECT" ] || { echo "could not resolve this checkout's compose project" >&2; exit 1; }
export CASSANDRA_3DC_CONTAINER_PREFIX="${CASSANDRA_3DC_CONTAINER_PREFIX:-$COMPOSE_PROJECT-nonfencing-cassandra}"
export CASSANDRA_NA_HOST_PORT="${CASSANDRA_NA_HOST_PORT:-127.0.0.1:0}"
export CASSANDRA_EU_HOST_PORT="${CASSANDRA_EU_HOST_PORT:-127.0.0.1:0}"
export CASSANDRA_ASIA_HOST_PORT="${CASSANDRA_ASIA_HOST_PORT:-127.0.0.1:0}"
THREE_DC=(docker compose -p "${CASSANDRA_3DC_PROJECT:-$COMPOSE_PROJECT-nonfencing-3dc}" -f docker-compose.cassandra-3dc.yaml)
RUNNER="$COMPOSE_PROJECT-library-hard-delete-lease-nonfencing-3dc-runner"
IMAGE="$COMPOSE_PROJECT-library-hard-delete-lease-nonfencing-3dc"
KEEP=0

for arg in "$@"; do
	case "$arg" in
		--keep) KEEP=1 ;;
		*) echo "usage: $0 [--keep]" >&2; exit 2 ;;
	esac
done

step() { printf '\033[1m==> %s\033[0m\n' "$*"; }
fail() { printf '\033[31mFAILED: %s\033[0m\n' "$*" >&2; exit 1; }

PAUSED=0

cleanup() {
	local rc=$?
	set +e
	if [ "$PAUSED" -eq 1 ]; then
		docker unpause "$CASSANDRA_3DC_CONTAINER_PREFIX-na" >/dev/null 2>&1 || true
	fi
	docker rm -f "$RUNNER" >/dev/null 2>&1 || true
	if [ "$KEEP" -eq 0 ]; then
		"${THREE_DC[@]}" down -v >/dev/null 2>&1 || true
	else
		echo "3-DC fixture left running (--keep)"
	fi
	exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

wait_healthy() {
	local node="$1" status
	for _ in $(seq 1 120); do
		status="$(docker inspect -f '{{.State.Health.Status}}' "$CASSANDRA_3DC_CONTAINER_PREFIX-$node" 2>/dev/null || true)"
		[ "$status" = "healthy" ] && return 0
		sleep 5
	done
	fail "$CASSANDRA_3DC_CONTAINER_PREFIX-$node did not become healthy"
}

wait_gossip_stable() {
	local node="$1" status
	for _ in $(seq 1 60); do
		status="$(docker exec "$CASSANDRA_3DC_CONTAINER_PREFIX-$node" nodetool status 2>/dev/null | grep -c '^UN ' || true)"
		[ "$status" = "3" ] && return 0
		sleep 2
	done
	fail "gossip did not stabilize to 3 UN nodes from dc-$node (last count: $status)"
}

wait_each_quorum_ready() {
	local node="$1"
	for _ in $(seq 1 30); do
		if docker exec "$CASSANDRA_3DC_CONTAINER_PREFIX-$node" cqlsh -e "CONSISTENCY EACH_QUORUM; SELECT * FROM sesamefs.libraries LIMIT 1;" >/dev/null 2>&1; then
			return 0
		fi
		sleep 2
	done
	fail "EACH_QUORUM reads from dc-$node did not become reliable after gossip stabilized"
}

wait_bootstrap() {
	local status
	for _ in $(seq 1 120); do
		status="$(docker inspect -f '{{.State.Status}}:{{.State.ExitCode}}' "$CASSANDRA_3DC_CONTAINER_PREFIX-3dc-bootstrap" 2>/dev/null || true)"
		[ "$status" = "exited:0" ] && return 0
		case "$status" in
			exited:*) docker logs "$CASSANDRA_3DC_CONTAINER_PREFIX-3dc-bootstrap" | tail -40; fail "3-DC schema bootstrap failed: $status" ;;
		esac
		sleep 5
	done
	fail "3-DC schema bootstrap did not finish"
}

runner_env() {
	docker exec "$RUNNER" env \
		CASSANDRA_HOSTS=cassandra-na:9042 \
		CASSANDRA_LOCAL_DC=dc-na \
		CASSANDRA_KEYSPACE=sesamefs \
		CASSANDRA_REPLICATION_CLASS=NetworkTopologyStrategy \
		CASSANDRA_REPLICATION_DCS=dc-na:1,dc-eu:1,dc-asia:1 \
		W2_POST_HEAD_3DC_HOSTS=dc-na=cassandra-na:9042,dc-eu=cassandra-eu:9042,dc-asia=cassandra-asia:9042 \
		"$@"
}

require_pass() {
	local output="$1" test_name="$2"
	grep -q "^--- PASS: $test_name" <<<"$output" || {
		echo "$output"
		fail "$test_name did not produce a PASS line"
	}
}

step "Start the isolated three-DC Cassandra fixture ($CASSANDRA_3DC_CONTAINER_PREFIX)"
"${THREE_DC[@]}" up -d
for n in na eu asia; do wait_healthy "$n"; done
wait_bootstrap
NA_CONTAINER="$("${THREE_DC[@]}" ps -q cassandra-na)"
[ -n "$NA_CONTAINER" ] || fail "could not resolve cassandra-na container id"
NETWORK="$(docker inspect -f '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}}{{end}}' "$NA_CONTAINER")"
[ -n "$NETWORK" ] || fail "could not resolve the three-DC Docker network"

step "Build an isolated Docker Go runner on the fixture network"
docker build -f Dockerfile.gotest -t "$IMAGE" .
docker rm -f "$RUNNER" >/dev/null 2>&1 || true
docker run -d --name "$RUNNER" --network "$NETWORK" "$IMAGE" sleep 3600 >/dev/null

step "Apply schema through dc-na"
runner_env go run ./cmd/sesamefs migrate
for n in na eu asia; do wait_gossip_stable "$n"; done
for n in na eu asia; do wait_each_quorum_ready "$n"; done

step "Stale owner in dc-na vs new owner in dc-eu, session default LOCAL_SERIAL"
if ! output="$(runner_env env \
	SESAMEFS_REQUIRE_LIBRARY_HARD_DELETE_NONFENCING_3DC_EVIDENCE=1 \
	go test -tags integration -count=1 ./internal/api/v2/ -run '^TestNonfencing3DC' -v 2>&1)"; then
	echo "$output"
	fail "library hard-delete lease non-fencing 3-DC evidence failed"
fi
echo "$output"
require_pass "$output" TestNonfencing3DCStaleDeleteAfterRestore
require_pass "$output" TestNonfencing3DCStaleRestoreAfterDelete

run_phase() {
	local phase="$1" output
	if ! output="$(runner_env env NONFENCING_3DC_PHASE="$phase" \
		go test -tags integration -count=1 ./internal/api/v2/ -run '^TestNonfencing3DCRecoveryFailsClosed$' -v 2>&1)"; then
		echo "$output"
		fail "3-DC recovery phase $phase failed"
	fi
	echo "$output"
	require_pass "$output" TestNonfencing3DCRecoveryFailsClosed
}

step "Recovery across DCs: transitions commit in dc-na with a failed completion"
run_phase prepare
step "dc-na unreachable: recovery from dc-eu must fail closed, never report success"
docker pause "$CASSANDRA_3DC_CONTAINER_PREFIX-na" >/dev/null
PAUSED=1
run_phase na-down
docker unpause "$CASSANDRA_3DC_CONTAINER_PREFIX-na" >/dev/null
PAUSED=0
for n in na eu asia; do wait_gossip_stable "$n"; done
for n in na eu asia; do wait_each_quorum_ready "$n"; done
step "dc-na back: the same recovery from dc-eu converges"
run_phase after

echo
echo "ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01 3-DC evidence passed: a stale owner in dc-na cannot delete a library restored in dc-eu or resurrect a library permanently deleted in dc-eu; recovery from dc-eu fails closed while dc-na is unreachable and converges once it is back."
