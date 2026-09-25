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
CASSANDRA_TEST_IMAGE=sesamefs-cassandra-cw-m33:5.0.9
LATCH_VOLUME=""
KEEP=0
ONLY_CW_M33=0
STOPPED=()
ABSENCE_TARGET=""
ABSENCE_BACKUP=""

export CASSANDRA_3DC_CONTAINER_PREFIX="$PREFIX"
export CASSANDRA_NA_HOST_PORT=0
export CASSANDRA_EU_HOST_PORT=0
export CASSANDRA_ASIA_HOST_PORT=0
export CASSANDRA_3DC_IMAGE_TAG=5.0.9
THREE_DC=(docker compose -p "$PROJECT" -f docker-compose.cassandra-3dc.yaml)
HOSTS=dc-na=cassandra-na:9042,dc-eu=cassandra-eu:9042,dc-asia=cassandra-asia:9042

for arg in "$@"; do
    case "$arg" in
        --keep) KEEP=1 ;;
        --only-cw-m33) ONLY_CW_M33=1 ;;
        *) echo "usage: $0 [--keep] [--only-cw-m33]" >&2; exit 2 ;;
    esac
done

step() { echo "==> $*"; }
fail() { echo "FAILED: $*" >&2; exit 1; }

restore_absence_mutation() {
    if [ -n "$ABSENCE_BACKUP" ] && [ -f "$ABSENCE_BACKUP" ]; then
        mv -f "$ABSENCE_BACKUP" "$ABSENCE_TARGET"
        ABSENCE_BACKUP=""
        ABSENCE_TARGET=""
    fi
}

cleanup() {
    local rc=$?
    set +e
    restore_absence_mutation
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

expect_red_phase() {
    local phase="$1" test="$2" diagnostic="$3" log
    log="$(mktemp)"
    if docker exec "$RUNNER" env \
        SESAMEFS_PCD1B4_3DC_PHASE="$phase" \
        SESAMEFS_PCD1B4_3DC_RUN_ID="$RUN_ID" \
        SESAMEFS_PCD1B4_3DC_HOSTS="$HOSTS" \
        go test -tags integration -count=1 ./internal/integration/pcd1b4multidc/ \
            -run "^${test}\$" -v >"$log" 2>&1; then
        cat "$log"
        rm -f "$log"
        fail "$phase mutation stayed green"
    fi
    if ! grep -q -- "$diagnostic" "$log"; then
        cat "$log"
        rm -f "$log"
        fail "$phase did not trip its targeted assertion: $diagnostic"
    fi
    echo "RED as required: $phase ($diagnostic)"
    rm -f "$log"
}

step "Build the isolated test-only Cassandra 5.0.9 image with a disabled-by-default Paxos latch"
docker build -f scripts/cassandra-cw-m33/Dockerfile -t "$CASSANDRA_TEST_IMAGE" .
export CASSANDRA_3DC_IMAGE="$CASSANDRA_TEST_IMAGE"

step "Start the isolated Cassandra 3-DC fixture"
CASSANDRA_3DC_CONTAINER_PREFIX="$PREFIX" "${THREE_DC[@]}" up -d
for node in na eu asia; do wait_healthy "$node"; done
wait_bootstrap
for node in na eu asia; do wait_gossip_stable "$node"; done

NETWORK="$(docker inspect -f '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}}{{end}}' "$PREFIX-na")"
[ -n "$NETWORK" ] || fail "could not resolve the isolated Cassandra network"
LATCH_VOLUME="$(docker inspect -f '{{range .Mounts}}{{if eq .Destination "/tmp/cw-m33"}}{{.Name}}{{end}}{{end}}' "$PREFIX-na")"
[ -n "$LATCH_VOLUME" ] || fail "could not resolve the isolated CW-M33 latch volume"

step "Build the branch-local test image and start the runner"
docker build -f Dockerfile.gotest -t "$IMAGE" .
docker run -d --name "$RUNNER" --network "$NETWORK" -e GC_ENABLED=false -e SESAMEFS_PCD1B4_PAXOS_LATCH_DIR=/test-latches -v "$LATCH_VOLUME":/test-latches -v "$PWD":/build -w /build "$IMAGE" sleep 3600 >/dev/null

step "Apply this branch's migrations to the isolated keyspace"
docker exec "$RUNNER" env \
    CASSANDRA_HOSTS=cassandra-na:9042 CASSANDRA_LOCAL_DC=dc-na \
    CASSANDRA_KEYSPACE=sesamefs CASSANDRA_REPLICATION_CLASS=NetworkTopologyStrategy \
    CASSANDRA_REPLICATION_DCS=dc-na:1,dc-eu:1,dc-asia:1 \
    go run ./cmd/sesamefs migrate
for node in na eu asia; do wait_each_quorum_ready "$node"; done

RUN_ID="$(docker exec "$RUNNER" sh -c 'cat /proc/sys/kernel/random/uuid')"

if [ "$ONLY_CW_M33" -eq 1 ]; then
    step "cw-m33-only: accepted HEAD Paxos proposal must be settled before global absence proof"
    run_phase m33-barrier TestPCD1B4StableAbsencePaxosRace3DC
    expect_red_phase m33-no-barrier TestPCD1B4StableAbsencePaxosRace3DC "CW-M33: EACH_QUORUM-only proof coexisted with a resurrected HEAD"
    run_phase m33-serial-presence TestPCD1B4SerialProofReadPresenceRace3DC
    ABSENCE_TARGET=internal/integration/pcd1b4multidc/stable_absence_paxos_race_test.go
    ABSENCE_BACKUP="$ABSENCE_TARGET.pcd1b4bak.$$"
    cp "$ABSENCE_TARGET" "$ABSENCE_BACKUP"
    perl -0pi -e 's/(func pcd1b4RequireSerialAbsenceForProof\(\) bool \{ )return true( \})/$1return false$2/' "$ABSENCE_TARGET"
    cmp -s "$ABSENCE_TARGET" "$ABSENCE_BACKUP" && fail "CW-M33 SERIAL-absence mutation did not apply"
    expect_red_phase m33-serial-presence-mutation TestPCD1B4SerialProofReadPresenceMutation3DC "CW-M33: proof minted from EACH_QUORUM absence despite SERIAL read observing H0"
    restore_absence_mutation
    echo "CW-M33 isolated 5.0.9 Paxos latch characterization passed (pre-existing proposal barrier GREEN; G21 no-barrier RED; post-barrier SERIAL-present proof refusal GREEN; G22 SERIAL-presence mutation RED)."
    exit 0
fi

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

step "m33-barrier: pause an accepted HEAD Paxos after reading H0; SERIAL settlement must refuse absence proof"
run_phase m33-barrier TestPCD1B4StableAbsencePaxosRace3DC

step "G21/CW-M33: omit Paxos settlement; EACH_QUORUM absence then old CAS resume must turn RED"
expect_red_phase m33-no-barrier TestPCD1B4StableAbsencePaxosRace3DC "CW-M33: EACH_QUORUM-only proof coexisted with a resurrected HEAD"

step "m33-serial-presence: a SERIAL read of H0 before a new accepted CAS must prohibit later absence proof"
run_phase m33-serial-presence TestPCD1B4SerialProofReadPresenceRace3DC

step "G22/CW-M33: omit the SERIAL-result absence predicate and require RED"
ABSENCE_TARGET=internal/integration/pcd1b4multidc/stable_absence_paxos_race_test.go
ABSENCE_BACKUP="$ABSENCE_TARGET.pcd1b4bak.$$"
cp "$ABSENCE_TARGET" "$ABSENCE_BACKUP"
perl -0pi -e 's/(func pcd1b4RequireSerialAbsenceForProof\(\) bool \{ )return true( \})/$1return false$2/' "$ABSENCE_TARGET"
cmp -s "$ABSENCE_TARGET" "$ABSENCE_BACKUP" && fail "CW-M33 SERIAL-absence mutation did not apply"
expect_red_phase m33-serial-presence-mutation TestPCD1B4SerialProofReadPresenceMutation3DC "CW-M33: proof minted from EACH_QUORUM absence despite SERIAL read observing H0"
restore_absence_mutation

step "m34-inflight: an admitted, timestamped materialization resumes after an EACH_QUORUM GC tombstone"
run_phase m34-inflight TestPCD1B4InFlightMaterialization3DC

step "Disable hinted handoff, then leave only dc-eu running"
for node in na eu asia; do docker exec "$PREFIX-$node" nodetool disablehandoff >/dev/null; done
stop_nodes na asia
wait_down eu na asia

step "a31-seed: insert a canonical library in dc-eu only"
run_phase a31-seed TestPCD1B4CanonicalAbsenceProof3DC

step "a31-verify: restore dc-na/dc-asia without hints; local absence must not mint proof"
start_nodes na asia
for node in na eu asia; do wait_gossip_stable "$node"; done

step "G16/CW-M31: weakening the GlobalCanonicalAbsenceProof source to LOCAL_QUORUM must turn the divergence test RED"
ABSENCE_TARGET=internal/integration/pcd1b4multidc/certification_window_3dc_test.go
ABSENCE_BACKUP="$ABSENCE_TARGET.pcd1b4bak.$$"
cp "$ABSENCE_TARGET" "$ABSENCE_BACKUP"
perl -0pi -e 's/(func pcd1b4CanonicalAbsenceProofConsistency\(\) gocql\.Consistency \{\r?\n\treturn gocql\.)EachQuorum/$1LocalQuorum/' "$ABSENCE_TARGET"
cmp -s "$ABSENCE_TARGET" "$ABSENCE_BACKUP" && fail "CW-M31 local-consistency mutation did not apply"
if ! grep -A1 'func pcd1b4CanonicalAbsenceProofConsistency' "$ABSENCE_TARGET" | grep -q 'return gocql.LocalQuorum'; then
    fail "CW-M31 mutation did not replace the proof read's consistency"
fi
expect_red_phase a31-verify TestPCD1B4CanonicalAbsenceProof3DC "CW-M31: global absence proof consistency is not EACH_QUORUM"
restore_absence_mutation

step "a31-verify: the global EACH_QUORUM read sees the remote library and refuses absence proof (CW-M31)"
run_phase a31-verify TestPCD1B4CanonicalAbsenceProof3DC
for node in na eu asia; do
    docker exec "$PREFIX-$node" nodetool enablehandoff >/dev/null
done

echo "PC-D1B.4 3-DC characterization passed: R12, CW-M23 EACH_QUORUM visibility, CW-M27 post-UNKNOWN local-only retry, CW-M31 local-absent/remote-present global-EACH_QUORUM proof, CW-M33 pre-existing and post-barrier accepted-Paxos races with G21/G22 mutations RED, CW-M34 admitted in-flight materialization hidden by a later tombstone, and G16 (EACH_QUORUM-to-LOCAL_QUORUM mutation RED). CW-M29/M32 clock safety and CW-M30 UUIDv5 vectors are model-characterized; runtime enforcement remains PC-D1B.5."
