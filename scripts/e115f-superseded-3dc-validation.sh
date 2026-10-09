#!/usr/bin/env bash
# E1-15F: real 3-DC evidence for the #275 SUPERSEDED repair settlement.
# Each phase of TestE115FSupersededCrossDC3DC runs in its own process from a
# named DC; ids flow through the log. See docs/E1-15F-SUPERSEDED-CROSS-DC.md.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

THREE_DC=(docker compose -f docker-compose.cassandra-3dc.yaml)
DEFAULT_COMPOSE_PROJECT="${COMPOSE_PROJECT_NAME:-sesamefs}"
DEFAULT_COMPOSE=(docker compose -p "$DEFAULT_COMPOSE_PROJECT" -f docker-compose.yaml)
RUNNER=sesamefs-e115f-3dc-runner
IMAGE=sesamefs-e115f-3dc
TEST=TestE115FSupersededCrossDC3DC
KEEP=0
DEFAULT_BACKEND_WAS_RUNNING=0

for arg in "$@"; do
	case "$arg" in
		--keep) KEEP=1 ;;
		*) echo "usage: $0 [--keep]" >&2; exit 2 ;;
	esac
done

step() { printf '\033[1m==> %s\033[0m\n' "$*"; }
fail() { printf '\033[31mFAILED: %s\033[0m\n' "$*" >&2; exit 1; }

cleanup() {
	local rc=$?
	set +e
	for n in na eu asia; do docker start "sesamefs-cassandra-$n" >/dev/null 2>&1 || true; done
	docker rm -f "$RUNNER" >/dev/null 2>&1 || true
	if [ "$KEEP" -eq 0 ]; then
		if [ "$DEFAULT_BACKEND_WAS_RUNNING" -eq 0 ]; then
			"${DEFAULT_COMPOSE[@]}" stop sesamefs >/dev/null 2>&1 || true
		fi
		"${THREE_DC[@]}" down -v >/dev/null 2>&1 || true
	else
		echo "3-DC fixture left running (--keep)"
	fi
	exit "$rc"
}
trap cleanup EXIT INT TERM

wait_healthy() {
	local node="$1" status
	for _ in $(seq 1 120); do
		status="$(docker inspect -f '{{.State.Health.Status}}' "sesamefs-cassandra-$node" 2>/dev/null || true)"
		[ "$status" = "healthy" ] && return 0
		sleep 5
	done
	fail "sesamefs-cassandra-$node did not become healthy"
}

wait_gossip_stable() {
	local node="$1" status
	for _ in $(seq 1 60); do
		status="$(docker exec "sesamefs-cassandra-$node" nodetool status 2>/dev/null | grep -c '^UN ' || true)"
		[ "$status" = "3" ] && return 0
		sleep 2
	done
	fail "gossip did not stabilize to 3 UN nodes from dc-$node (last count: $status)"
}

wait_each_quorum_ready() {
	local node="$1"
	for _ in $(seq 1 30); do
		if docker exec "sesamefs-cassandra-$node" cqlsh -e "CONSISTENCY EACH_QUORUM; SELECT * FROM sesamefs.gc_provisional_block_refs LIMIT 1;" >/dev/null 2>&1; then
			return 0
		fi
		sleep 2
	done
	fail "EACH_QUORUM reads from dc-$node did not become reliable"
}

wait_bootstrap() {
	local status
	for _ in $(seq 1 120); do
		status="$(docker inspect -f '{{.State.Status}}:{{.State.ExitCode}}' sesamefs-cassandra-3dc-bootstrap 2>/dev/null || true)"
		[ "$status" = "exited:0" ] && return 0
		case "$status" in
			exited:*) docker logs sesamefs-cassandra-3dc-bootstrap | tail -40; fail "3-DC schema bootstrap failed: $status" ;;
		esac
		sleep 5
	done
	fail "3-DC schema bootstrap did not finish"
}

all_up() {
	for n in na eu asia; do wait_healthy "$n"; done
	for n in na eu asia; do wait_gossip_stable "$n"; done
	for n in na eu asia; do wait_each_quorum_ready "$n"; done
}

IDS=()
capture() {
	local output="$1" key value
	for key in ORG REPO USER PARENT C1 FS BLOCK H1 C2 FS2 BLOCK2 H2; do
		value="$(sed -n "s/.*E115F_${key}=\([0-9a-f-]*\).*/\1/p" <<<"$output" | tail -1)"
		[ -n "$value" ] && IDS+=("E115F_${key}=$value")
	done
	return 0
}

# phase <name> <dc>: run one phase from that DC; require its PASS and marker.
phase() {
	local name="$1" dc="$2" node="${2#dc-}" output
	step "Phase $name from $dc"
	if ! output="$(docker exec "$RUNNER" env \
		SESAMEFS_URL=http://sesamefs:8080 \
		CASSANDRA_HOSTS="cassandra-$node:9042" CASSANDRA_LOCAL_DC="$dc" CASSANDRA_KEYSPACE=sesamefs \
		CASSANDRA_REPLICATION_CLASS=NetworkTopologyStrategy CASSANDRA_REPLICATION_DCS=dc-na:1,dc-eu:1,dc-asia:1 \
		W2_POST_HEAD_3DC_HOSTS=dc-na=cassandra-na:9042,dc-eu=cassandra-eu:9042,dc-asia=cassandra-asia:9042 \
		E115F_3DC_PHASE="$name" "${IDS[@]}" \
		go test -tags integration -count=1 ./internal/integration/ -run "^${TEST}\$" -v 2>&1)"; then
		echo "$output"
		fail "phase $name from $dc failed"
	fi
	echo "$output" | grep -E 'E1-15F|E115F_|sweep from|--- ' || true
	grep -q "^--- PASS: $TEST" <<<"$output" && grep -q "E115F_PHASE_DONE=$name" <<<"$output" && ! grep -q -- "--- SKIP" <<<"$output" \
		|| { echo "$output"; fail "phase $name produced no PASS/marker (or skipped)"; }
	capture "$output"
}

step "Start the normal backend and the real three-DC Cassandra fixture"
if [ -n "$("${DEFAULT_COMPOSE[@]}" ps -q --status running sesamefs 2>/dev/null)" ]; then
	DEFAULT_BACKEND_WAS_RUNNING=1
fi
"${DEFAULT_COMPOSE[@]}" up -d sesamefs
"${THREE_DC[@]}" up -d
for n in na eu asia; do wait_healthy "$n"; done
wait_bootstrap
NA_CONTAINER="$("${THREE_DC[@]}" ps -q cassandra-na)"
NETWORK="$(docker inspect -f '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}}{{end}}' "$NA_CONTAINER")"
[ -n "$NETWORK" ] || fail "could not resolve the three-DC Docker network"

step "Build an isolated Docker Go runner on both networks"
docker build -f Dockerfile.gotest -t "$IMAGE" .
docker rm -f "$RUNNER" >/dev/null 2>&1 || true
docker run -d --name "$RUNNER" --network "$NETWORK" "$IMAGE" sleep 7200 >/dev/null
if ! docker inspect -f '{{range $name, $_ := .NetworkSettings.Networks}}{{println $name}}{{end}}' "$RUNNER" | grep -qx "${DEFAULT_COMPOSE_PROJECT}_default"; then
	docker network connect "${DEFAULT_COMPOSE_PROJECT}_default" "$RUNNER"
fi

step "Apply schema through dc-na"
docker exec "$RUNNER" env CASSANDRA_HOSTS=cassandra-na:9042 CASSANDRA_LOCAL_DC=dc-na CASSANDRA_KEYSPACE=sesamefs \
	CASSANDRA_REPLICATION_CLASS=NetworkTopologyStrategy CASSANDRA_REPLICATION_DCS=dc-na:1,dc-eu:1,dc-asia:1 \
	go run ./cmd/sesamefs migrate
all_up

step "Gate negative controls"
neg="$(docker exec "$RUNNER" env W2_POST_HEAD_3DC_HOSTS=dc-na=cassandra-na:9042,dc-eu=cassandra-eu:9042,dc-asia=cassandra-asia:9042 \
	SESAMEFS_URL=http://sesamefs:8080 CASSANDRA_HOSTS=cassandra-na:9042 CASSANDRA_LOCAL_DC=dc-na CASSANDRA_KEYSPACE=sesamefs \
	E115F_3DC_PHASE=m1-verify go test -tags integration -count=1 ./internal/integration/ -run "^${TEST}\$" -v 2>&1 || true)"
grep -q "E115F_ORG is required" <<<"$neg" && ! grep -q "^--- PASS: $TEST" <<<"$neg" || { echo "$neg"; fail "missing-id negative control did not fail"; }
echo "PASS E1-15F gate negative control: missing ids fail the phase"
neg="$(docker exec "$RUNNER" env SESAMEFS_URL=http://sesamefs:8080 CASSANDRA_HOSTS=cassandra-na:9042 CASSANDRA_KEYSPACE=sesamefs \
	go test -tags integration -count=1 ./internal/integration/ -run "^${TEST}\$" -v 2>&1 || true)"
grep -q -- "--- SKIP: $TEST" <<<"$neg" || { echo "$neg"; fail "unset-phase control did not skip"; }
echo "PASS E1-15F gate negative control: an unset phase SKIPs, which phase() rejects"

phase seed dc-na
phase m2 dc-asia
phase advance dc-eu

step "Stop dc-eu: SERIAL keeps 2/3, EACH_QUORUM loses its quorum"
"${THREE_DC[@]}" stop cassandra-eu
phase m4a dc-na

"${THREE_DC[@]}" start cassandra-eu
all_up
phase m4a-recovery dc-asia
phase m1-verify dc-na
phase m1-verify dc-eu

phase m3-setup dc-na
phase m3-publish dc-eu
phase m3 dc-na

echo
echo "E1-15F 3-DC evidence passed: M2 (HEAD = parent) retained; M4a (one DC down) stayed UNKNOWN with an availability error and retained R; after recovery a sweep from another DC settled R as SUPERSEDED; M1 verified from dc-na and dc-eu at EACH_QUORUM; M3 classified REACHABLE and promoted. GC stays off; E1/X1 stay OPEN."
