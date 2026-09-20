#!/usr/bin/env bash
# Real 3-DC evidence for PC-D1B.1. Every resource belongs to this private fixture.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

PROJECT=sesamefs-pcd1b1-cassandra
PREFIX=sesamefs-pcd1b1-cassandra
RUNNER=sesamefs-pcd1b1-integration-runner
BACKEND=sesamefs-pcd1b1-backend
MINIO=sesamefs-pcd1b1-minio
IMAGE=sesamefs-pcd1b1-gotest
BACKEND_IMAGE=sesamefs-pcd1b1-backend-image
KEEP=0

export CASSANDRA_3DC_CONTAINER_PREFIX="$PREFIX"
export CASSANDRA_NA_HOST_PORT=0
export CASSANDRA_EU_HOST_PORT=0
export CASSANDRA_ASIA_HOST_PORT=0
THREE_DC=(docker compose -p "$PROJECT" -f docker-compose.cassandra-3dc.yaml)

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
    docker rm -f "$RUNNER" "$BACKEND" "$MINIO" >/dev/null 2>&1 || true
    if [ "$KEEP" -eq 0 ]; then
        CASSANDRA_3DC_CONTAINER_PREFIX="$PREFIX" "${THREE_DC[@]}" down -v >/dev/null 2>&1 || true
    else
        echo "PC-D1B.1 3-DC fixture left running (--keep)"
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
    local node="$1" status
    for _ in $(seq 1 60); do
        status="$(docker exec "$PREFIX-$node" nodetool status 2>/dev/null | grep -c '^UN ' || true)"
        [ "$status" = "3" ] && return 0
        sleep 2
    done
    fail "gossip did not stabilize to 3 UN nodes from dc-$node (last count: $status)"
}

wait_each_quorum_ready() {
    local node="$1"
    for _ in $(seq 1 30); do
        if docker exec "$PREFIX-$node" cqlsh -e "CONSISTENCY EACH_QUORUM; SELECT * FROM sesamefs.gc_provisional_block_refs LIMIT 1;" >/dev/null 2>&1; then
            return 0
        fi
        sleep 2
    done
    fail "EACH_QUORUM reads from dc-$node did not become reliable after gossip stabilized"
}

step "Start the isolated Cassandra 3-DC fixture"
CASSANDRA_3DC_CONTAINER_PREFIX="$PREFIX" "${THREE_DC[@]}" up -d
for node in na eu asia; do wait_healthy "$node"; done
wait_bootstrap
for node in na eu asia; do wait_gossip_stable "$node"; done

NA_CONTAINER="$PREFIX-na"
NETWORK="$(docker inspect -f '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}}{{end}}' "$NA_CONTAINER")"
[ -n "$NETWORK" ] || fail "could not resolve isolated Cassandra network"

step "Build branch-local test and backend images"
docker build -f Dockerfile.gotest -t "$IMAGE" .
docker build -f Dockerfile -t "$BACKEND_IMAGE" .

step "Start isolated MinIO and integration runner"
docker run -d --name "$MINIO" --network "$NETWORK" --network-alias minio \
    -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin \
    minio/minio:latest server /data --console-address :9001 >/dev/null
docker run -d --name "$RUNNER" --network "$NETWORK" "$IMAGE" sleep 3600 >/dev/null

step "Apply the current branch migrations to the isolated keyspace"
docker exec "$RUNNER" env \
    CASSANDRA_HOSTS=cassandra-na:9042 CASSANDRA_LOCAL_DC=dc-na \
    CASSANDRA_KEYSPACE=sesamefs CASSANDRA_REPLICATION_CLASS=NetworkTopologyStrategy \
    CASSANDRA_REPLICATION_DCS=dc-na:1,dc-eu:1,dc-asia:1 \
    go run ./cmd/sesamefs migrate
for node in na eu asia; do wait_each_quorum_ready "$node"; done

step "Start the isolated SesameFS backend with GC disabled"
docker run -d --name "$BACKEND" --network "$NETWORK" \
    -e PORT=8080 -e SERVER_URL="http://$BACKEND:8080" \
    -e AUTH_DEV_MODE=true -e GC_ENABLED=false \
    -e ONLYOFFICE_ENABLED=false -e ONLYOFFICE_JWT_SECRET=pc-d1b1-test-jwt \
    -e SHARE_LINK_HMAC_KEY=pc-d1b1-test-share-key -e ACCOUNTS_DISABLE_ORG_USER_WRITES=true \
    -e CASSANDRA_HOSTS=cassandra-na:9042 -e CASSANDRA_LOCAL_DC=dc-na \
    -e CASSANDRA_KEYSPACE=sesamefs \
    -e CASSANDRA_REPLICATION_CLASS=NetworkTopologyStrategy \
    -e CASSANDRA_REPLICATION_DCS=dc-na:1,dc-eu:1,dc-asia:1 \
    -e CASSANDRA_SERIAL_CONSISTENCY=LOCAL_SERIAL \
    -e S3_ENDPOINT=http://minio:9000 -e S3_ACCESS_KEY_ID=minioadmin -e S3_SECRET_ACCESS_KEY=minioadmin \
    "$BACKEND_IMAGE" serve >/dev/null

for _ in $(seq 1 60); do
    if docker exec "$RUNNER" curl -fsS "http://$BACKEND:8080/health" >/dev/null 2>&1; then
        break
    fi
    sleep 2
done
docker exec "$RUNNER" curl -fsS "http://$BACKEND:8080/health" >/dev/null || fail "isolated SesameFS backend did not become healthy"

step "Run the PC-D1B.1 certifier evidence under LOCAL_SERIAL client sessions"
docker exec "$RUNNER" env \
    SESAMEFS_URL="http://$BACKEND:8080" \
    CASSANDRA_HOSTS=cassandra-na:9042 CASSANDRA_LOCAL_DC=dc-na \
    CASSANDRA_KEYSPACE=sesamefs CASSANDRA_SERIAL_CONSISTENCY=LOCAL_SERIAL \
    CASSANDRA_REPLICATION_CLASS=NetworkTopologyStrategy \
    CASSANDRA_REPLICATION_DCS=dc-na:1,dc-eu:1,dc-asia:1 \
    W2_POST_HEAD_3DC_HOSTS=dc-na=cassandra-na:9042,dc-eu=cassandra-eu:9042,dc-asia=cassandra-asia:9042 \
    SESAMEFS_REQUIRE_LIBRARY_BASELINE_CERTIFIER_EVIDENCE=1 \
    go test -tags integration -count=1 ./internal/integration/ \
        -run '^TestLibraryBaselineCertifier3DC$|^TestEveryEvidenceGateIsWiredIntoTestMain$' -v

echo "PC-D1B.1 3-DC evidence passed: complete-tree certification, EACH_QUORUM permanent liveness, exact-P/GC revalidation, idempotent retry, stale-HEAD rejection, fail-closed negatives, and serial-CAS settlement gate.";
