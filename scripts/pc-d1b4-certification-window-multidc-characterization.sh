#!/usr/bin/env bash
# Real 3-DC characterization for PC-D1B.4 (race R12 in
# docs/PC-D1B-CERTIFICATION-WINDOW-FENCE.md). It asserts CURRENT behavior on
# main: a witness can settle after a soft-delete that another DC already
# acknowledged, a third-DC LOCAL_QUORUM reader sees it as valid, and restore
# revives it. Every resource belongs to this private fixture; no application
# stack is touched.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

PROJECT=sesamefs-pcd1b4-cassandra
PREFIX=sesamefs-pcd1b4-cassandra
RUNNER=sesamefs-pcd1b4-runner
IMAGE=sesamefs-pcd1b4-gotest
KEEP=0
STOPPED=()

export CASSANDRA_3DC_CONTAINER_PREFIX="$PREFIX"
export CASSANDRA_NA_HOST_PORT=0
export CASSANDRA_EU_HOST_PORT=0
export CASSANDRA_ASIA_HOST_PORT=0
THREE_DC=(docker compose -p "$PROJECT" -f docker-compose.cassandra-3dc.yaml)
HOSTS=dc-na=cassandra-na:9042,dc-eu=cassandra-eu:9042,dc-asia=cassandra-asia:9042

for arg in "$@"; do
    case "$arg" in
        --keep) KEEP=1 ;;
        *) echo "usage: $0 [--keep]" >&2; exit 2 ;;
    esac
done

step() { echo "==> $*"; }
fail() { echo "FAILED: $*" >&2; exit 1; }

cleanup() {
    local rc=$?
    set +e
    docker rm -f "$RUNNER" >/dev/null 2>&1 || true
    if [ "$KEEP" -eq 0 ]; then
        CASSANDRA_3DC_CONTAINER_PREFIX="$PREFIX" "${THREE_DC[@]}" down -v >/dev/null 2>&1 || true
    else
        for node in "${STOPPED[@]}"; do docker start "$PREFIX-$node" >/dev/null 2>&1 || true; done
        echo "PC-D1B.4 3-DC fixture left running (--keep)"
    fi
    exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

wait_healthy() {
    local node="$1" status
    for _ in $(seq 1 120); do
        status="$(docker inspect -f '{{.State.Health.Status}}' "$PREFIX-$node" 2>/dev/null || true)"
        [ "$status" = healthy ] && return 0
        sleep 5
    done
    fail "$PREFIX-$node did not become healthy"
}

wait_bootstrap() {
    local status
    for _ in $(seq 1 120); do
        status="$(docker inspect -f '{{.State.Status}}:{{.State.ExitCode}}' "$PREFIX-3dc-bootstrap" 2>/dev/null || true)"
        [ "$status" = exited:0 ] && return 0
        case "$status" in
            exited:*) docker logs "$PREFIX-3dc-bootstrap" | tail -40; fail "3-DC schema bootstrap failed: $status" ;;
        esac
        sleep 5
    done
    fail "3-DC schema bootstrap did not finish"
}

wait_gossip_stable() {
    local node="$1" status=""
    for _ in $(seq 1 90); do
        status="$(docker exec "$PREFIX-$node" nodetool status 2>/dev/null | grep -c '^UN ' || true)"
        [ "$status" = "3" ] && return 0
        sleep 2
    done
    fail "gossip did not stabilize to 3 UN nodes from dc-$node (last count: $status)"
}

# wait_down OBSERVER DC... waits until OBSERVER's gossip reports every DC as DN.
wait_down() {
    local observer="$1" status
    shift
    for _ in $(seq 1 90); do
        status="$(docker exec "$PREFIX-$observer" nodetool status 2>/dev/null || true)"
        if printf '%s\n' "$status" | awk -v want="$*" '
            BEGIN { n = split(want, dcs, " "); for (i = 1; i <= n; i++) wanted["dc-" dcs[i]] = 1 }
            /^Datacenter: / { dc = $2; next }
            (dc in wanted) && /^DN / { down[dc] = 1 }
            END { for (d in wanted) if (!(d in down)) exit 1; exit 0 }
        '; then
            return 0
        fi
        sleep 2
    done
    printf '%s\n' "$status" | tail -30 >&2
    fail "dc-$observer did not observe $* as DN"
}

wait_each_quorum_ready() {
    local node="$1"
    for _ in $(seq 1 30); do
        if docker exec "$PREFIX-$node" cqlsh -e "CONSISTENCY EACH_QUORUM; SELECT * FROM sesamefs.libraries LIMIT 1;" >/dev/null 2>&1; then
            return 0
        fi
        sleep 2
    done
    fail "EACH_QUORUM reads from dc-$node did not become reliable"
}

stop_nodes() {
    for node in "$@"; do
        docker stop "$PREFIX-$node" >/dev/null
        STOPPED+=("$node")
    done
}

start_nodes() {
    local remaining=()
    for node in "$@"; do docker start "$PREFIX-$node" >/dev/null; done
    for node in "${STOPPED[@]}"; do
        case " $* " in *" $node "*) ;; *) remaining+=("$node") ;; esac
    done
    STOPPED=("${remaining[@]}")
    for node in "$@"; do wait_healthy "$node"; done
}

run_phase() {
    local phase="$1" test="${2:-TestPCD1B4CertificationWindow3DC}" log
    log="$(mktemp)"
    if ! docker exec "$RUNNER" env \
        SESAMEFS_PCD1B4_3DC_PHASE="$phase" \
        SESAMEFS_PCD1B4_3DC_RUN_ID="$RUN_ID" \
        SESAMEFS_PCD1B4_3DC_HOSTS="$HOSTS" \
        go test -tags integration -count=1 ./internal/integration/pcd1b4multidc/ \
            -run "^${test}\$" -v 2>&1 | tee "$log"; then
        rm -f "$log"
        fail "phase $phase failed"
    fi
    # A phase that skipped proves nothing; require the test to have passed.
    if ! grep -q -- "--- PASS: ${test}" "$log"; then
        rm -f "$log"
        fail "phase $phase did not run to PASS"
    fi
    rm -f "$log"
}

step "Start the isolated Cassandra 3-DC fixture"
CASSANDRA_3DC_CONTAINER_PREFIX="$PREFIX" "${THREE_DC[@]}" up -d
for node in na eu asia; do wait_healthy "$node"; done
wait_bootstrap
for node in na eu asia; do wait_gossip_stable "$node"; done

NETWORK="$(docker inspect -f '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}}{{end}}' "$PREFIX-na")"
[ -n "$NETWORK" ] || fail "could not resolve the isolated Cassandra network"

step "Build the branch-local test image and start the runner"
docker build -f Dockerfile.gotest -t "$IMAGE" .
docker run -d --name "$RUNNER" --network "$NETWORK" "$IMAGE" sleep 3600 >/dev/null

step "Apply this branch's migrations to the isolated keyspace"
docker exec "$RUNNER" env \
    CASSANDRA_HOSTS=cassandra-na:9042 CASSANDRA_LOCAL_DC=dc-na \
    CASSANDRA_KEYSPACE=sesamefs CASSANDRA_REPLICATION_CLASS=NetworkTopologyStrategy \
    CASSANDRA_REPLICATION_DCS=dc-na:1,dc-eu:1,dc-asia:1 \
    go run ./cmd/sesamefs migrate
for node in na eu asia; do wait_each_quorum_ready "$node"; done

RUN_ID="$(docker exec "$RUNNER" sh -c 'cat /proc/sys/kernel/random/uuid')"

step "prepare: seed a certifiable library with durable identity claims in all DCs"
run_phase prepare

step "Disable hinted handoff on this fixture, then leave only dc-eu running"
for node in na eu asia; do docker exec "$PREFIX-$node" nodetool disablehandoff >/dev/null; done
stop_nodes na asia
wait_down eu na asia

step "degrade: acknowledge the production soft-delete in dc-eu only"
run_phase degrade

step "Bring dc-na and dc-asia back (no hints), then stop dc-eu"
start_nodes na asia
for node in na eu asia; do wait_gossip_stable "$node"; done
stop_nodes eu
wait_down na eu

step "certify: dc-na certifies under global SERIAL while dc-eu holds the soft-delete"
run_phase certify

step "Restore dc-eu, re-enable hinted handoff and wait for EACH_QUORUM health"
start_nodes eu
for node in na eu asia; do wait_gossip_stable "$node"; done
for node in na eu asia; do
    docker exec "$PREFIX-$node" nodetool enablehandoff >/dev/null
    wait_each_quorum_ready "$node"
done

step "merge: the converged row is deleted with witness H; restore revives it"
run_phase merge

step "rprepare: seed two identical covered rows at T0 in all DCs"
run_phase rprepare TestPCD1B4ReaffirmationConsistency3DC

step "Disable hinted handoff, then leave only dc-na running"
for node in na eu asia; do docker exec "$PREFIX-$node" nodetool disablehandoff >/dev/null; done
stop_nodes eu asia
wait_down na eu asia

step "rdegrade: LOCAL_QUORUM reaffirmation is acknowledged; EACH_QUORUM reaffirmation fails closed"
run_phase rdegrade TestPCD1B4ReaffirmationConsistency3DC

step "Bring dc-eu and dc-asia back (no hints) and re-enable hinted handoff"
start_nodes eu asia
for node in na eu asia; do wait_gossip_stable "$node"; done
for node in na eu asia; do
    docker exec "$PREFIX-$node" nodetool enablehandoff >/dev/null
    wait_each_quorum_ready "$node"
done

step "rverify: a stale tombstone removes the LOCAL_QUORUM-reaffirmed row outside dc-na, never the EACH_QUORUM one (CW-M23)"
run_phase rverify TestPCD1B4ReaffirmationConsistency3DC

step "m27-prepare: seed a fresh covered row at T0 in all DCs"
run_phase m27-prepare TestPCD1B4UnknownReaffirmationRetry3DC

step "Disable hinted handoff, then leave only dc-na running"
for node in na eu asia; do docker exec "$PREFIX-$node" nodetool disablehandoff >/dev/null; done
stop_nodes eu asia
wait_down na eu asia

step "m27-unknown: EACH_QUORUM refuses with remote DCs down; construct its local-only partial state (WRITETIME>S)"
run_phase m27-unknown TestPCD1B4UnknownReaffirmationRetry3DC

step "Restore dc-eu and dc-asia without hints, then re-enable hinted handoff"
start_nodes eu asia
for node in na eu asia; do wait_gossip_stable "$node"; done
for node in na eu asia; do
    docker exec "$PREFIX-$node" nodetool enablehandoff >/dev/null
    wait_each_quorum_ready "$node"
done

step "m27-retry: retry still performs EACH_QUORUM despite local WRITETIME>S"
run_phase m27-retry TestPCD1B4UnknownReaffirmationRetry3DC

step "m27-verify: the stale tombstone leaves the certified row present in every DC"
run_phase m27-verify TestPCD1B4UnknownReaffirmationRetry3DC

echo "PC-D1B.4 3-DC characterization passed: R12 witness settled after a remote acknowledged soft-delete, stale dc-asia LOCAL_QUORUM reader saw it valid, blind-DC gateway delete refused, converged row invalid while deleted, restore revived the witness; CW-M23 proves EACH_QUORUM is required. CW-M27's model covers partial EACH_QUORUM/UNKNOWN; this fixture reproduces the resulting local-only high-WRITETIME state and proves the retry must reaffirm globally."
