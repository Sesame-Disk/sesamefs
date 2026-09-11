#!/usr/bin/env bash
# Real 3-DC, handler-level evidence for ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01
# (multi-DC HEAD-reversion variant, PC-0 §3.4).
#
# Drives the PRODUCTION initializers (FSHelper.InitializeLibraryFS and the Sync
# createInitialCommit path behind GET /seafhttp/repo/:id/commit/HEAD) through a
# *db.DB connected to a specific datacenter, the way scripts/w2-*.sh drive their
# production functions. scripts/pc0-initial-head-xdc-probe.sh validates the CQL
# shapes with cqlsh; this script validates the code that issues them.
#
#   1. seed a library row with a null HEAD, visible in every DC
#   2. stop dc-eu; dc-na initializes HEAD = C1 through InitializeLibraryFS
#   3. restart dc-eu blind (hinted handoff off); run both initializers from dc-eu:
#      they must keep C1, return C1, and leave no dangling commit
#
# Before the conditional initializer, step 3's Sync path overwrote C1.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

THREE_DC=(docker compose -f docker-compose.cassandra-3dc.yaml)
DEFAULT_COMPOSE=(docker compose -p sesamefs -f docker-compose.yaml)
RUNNER=sesamefs-h1-initial-head-3dc-runner
IMAGE=sesamefs-h1-initial-head-3dc
NETWORK=
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
	docker start sesamefs-cassandra-eu >/dev/null 2>&1 || true
	for n in na eu asia; do
		docker exec "sesamefs-cassandra-$n" nodetool enablehandoff >/dev/null 2>&1 || true
	done
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
		if docker exec "sesamefs-cassandra-$node" cqlsh -e "CONSISTENCY EACH_QUORUM; SELECT * FROM sesamefs.libraries LIMIT 1;" >/dev/null 2>&1; then
			return 0
		fi
		sleep 2
	done
	fail "EACH_QUORUM reads from dc-$node did not become reliable after gossip stabilized"
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

runner_env() {
	local dc="$1"
	local node="${dc#dc-}"
	shift
	docker exec "$RUNNER" env \
		SESAMEFS_URL=http://sesamefs:8080 \
		CASSANDRA_HOSTS="cassandra-$node:9042" \
		CASSANDRA_LOCAL_DC="$dc" \
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

step "Start the normal backend and the real three-DC Cassandra fixture"
if [ -n "$("${DEFAULT_COMPOSE[@]}" ps -q --status running sesamefs 2>/dev/null)" ]; then
	DEFAULT_BACKEND_WAS_RUNNING=1
fi
"${DEFAULT_COMPOSE[@]}" up -d sesamefs
"${THREE_DC[@]}" up -d
for n in na eu asia; do wait_healthy "$n"; done
wait_bootstrap
NA_CONTAINER="$("${THREE_DC[@]}" ps -q cassandra-na)"
[ -n "$NA_CONTAINER" ] || fail "could not resolve cassandra-na container id"
NETWORK="$(docker inspect -f '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}}{{end}}' "$NA_CONTAINER")"
[ -n "$NETWORK" ] || fail "could not resolve the real three-DC Docker network"

step "Build an isolated Docker Go runner on both networks"
docker build -f Dockerfile.gotest -t "$IMAGE" .
docker rm -f "$RUNNER" >/dev/null 2>&1 || true
docker run -d --name "$RUNNER" --network "$NETWORK" "$IMAGE" sleep 3600 >/dev/null
docker network connect sesamefs_default "$RUNNER"

step "Apply schema through dc-na"
runner_env dc-na env CASSANDRA_HOSTS=cassandra-na:9042 go run ./cmd/sesamefs migrate
for n in na eu asia; do wait_gossip_stable "$n"; done
for n in na eu asia; do wait_each_quorum_ready "$n"; done

step "Seed a library row with a null HEAD, visible in every DC"
if ! seed_output="$(runner_env dc-na env H1_SEED=1 go test -tags integration -count=1 ./internal/integration/ -run '^TestH1InitialHeadSeedFor3DC$' -v 2>&1)"; then
	echo "$seed_output"
	fail "3-DC seed test failed"
fi
echo "$seed_output"
require_pass "$seed_output" TestH1InitialHeadSeedFor3DC
ORG="$(sed -n 's/.*H1_ORG=\([0-9a-f-]*\).*/\1/p' <<<"$seed_output" | tail -1)"
REPO="$(sed -n 's/.*H1_REPO=\([0-9a-f-]*\).*/\1/p' <<<"$seed_output" | tail -1)"
OWNER="$(sed -n 's/.*H1_OWNER=\([0-9a-f-]*\).*/\1/p' <<<"$seed_output" | tail -1)"
[ -n "$ORG" ] && [ -n "$REPO" ] && [ -n "$OWNER" ] || fail "could not capture seeded H1 3-DC ids"

step "Stop dc-eu (hinted handoff off everywhere) and initialize HEAD from dc-na through the production v2 initializer"
for n in na eu asia; do docker exec "sesamefs-cassandra-$n" nodetool disablehandoff >/dev/null; done
"${THREE_DC[@]}" stop cassandra-eu
if ! publish_output="$(runner_env dc-na env \
	H1_PUBLISH_NA=1 H1_ORG="$ORG" H1_REPO="$REPO" H1_OWNER="$OWNER" \
	go test -tags integration -count=1 ./internal/integration/ -run '^TestH1InitialHeadPublishInNA3DC$' -v 2>&1)"; then
	echo "$publish_output"
	fail "dc-na initialization failed"
fi
echo "$publish_output"
require_pass "$publish_output" TestH1InitialHeadPublishInNA3DC
C1="$(sed -n 's/.*H1_C1=\([0-9a-f]*\).*/\1/p' <<<"$publish_output" | tail -1)"
[ -n "$C1" ] || fail "could not capture the HEAD dc-na published"

step "Restart dc-eu blind and run both production initializers from it"
"${THREE_DC[@]}" start cassandra-eu
wait_healthy eu
wait_gossip_stable eu
wait_gossip_stable na
if ! blind_output="$(runner_env dc-eu env \
	SESAMEFS_REQUIRE_H1_INITIAL_HEAD_MULTIDC_EVIDENCE=1 \
	H1_BLIND_EU=1 H1_ORG="$ORG" H1_REPO="$REPO" H1_OWNER="$OWNER" H1_C1="$C1" \
	go test -tags integration -count=1 ./internal/integration/ -run '^TestH1InitialHeadBlindDCDoesNotRevert3DC$|^TestEveryEvidenceGateIsWiredIntoTestMain$' -v 2>&1)"; then
	echo "$blind_output"
	fail "blind-datacenter initializer leg failed (HEAD reverted or initializer misbehaved)"
fi
echo "$blind_output"
require_pass "$blind_output" TestH1InitialHeadBlindDCDoesNotRevert3DC

echo
echo "H1 3-DC evidence passed: both production initializers driven from a blind datacenter kept and returned the HEAD another datacenter had published (${C1})."
