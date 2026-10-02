#!/usr/bin/env bash
# Directed G4/G5 continuation on a fresh, isolated Cassandra 5.0.9 three-DC fixture.
# A current-source local backend shares this isolated DB; MinIO supplies storage.
set -euo pipefail
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"
image=${G5_TEST_IMAGE:-sesamefs-go-integration-test:latest}
project="sesamefs-g5-3dc-$(date -u +%Y%m%d%H%M%S)-$RANDOM"
export CASSANDRA_3DC_CONTAINER_PREFIX=$project
snapshot=$(mktemp -d "${TMPDIR:-/tmp}/sesamefs-g5-3dc-XXXXXXXX")
logs="$repo_root/tmp/$project"
mkdir -p "$logs"
git ls-files -z --cached --others --exclude-standard > "$logs/source-files.bin"
# Filter removed paths with shell builtins; tar copies the snapshot in two processes.
while IFS= read -r -d '' source; do
 [[ -f "$source" ]] && printf '%s\0' "$source"
done < "$logs/source-files.bin" > "$logs/existing-source-files.bin"
tar --null -T "$logs/existing-source-files.bin" -cf - | tar -xf - -C "$snapshot"
cat > "$logs/no-ports.yaml" <<'YAML'
services:
  cassandra-na:
    ports: !reset []
  cassandra-eu:
    ports: !reset []
  cassandra-asia:
    ports: !reset []
YAML
compose=(docker compose -p "$project" -f docker-compose.cassandra-3dc.yaml -f "tmp/$project/no-ports.yaml")
runner="$project-runner"
cleanup() { "${compose[@]}" stop; }
trap cleanup EXIT
snapshot_mount=$snapshot
if command -v cygpath >/dev/null 2>&1; then snapshot_mount=$(cygpath -w "$snapshot"); fi
export MSYS_NO_PATHCONV=1
"${compose[@]}" up -d
# Compose forms the nodes in order. Require all three CQL/UN health checks.
deadline=$((SECONDS+720))
while :; do
 healthy=1
 for node in na eu asia; do
  [[ $(docker inspect --format '{{.State.Health.Status}}' "$project-$node") == healthy ]] || healthy=0
 done
 ((healthy)) && break
 ((SECONDS<deadline)) || { echo 'Three-DC health timeout' >&2; exit 1; }
 sleep 5
done
environment=(
  -e SESAMEFS_URL=http://localhost:8080 -e CONFIG_PATH=configs/config.docker.yaml
  -e GC_ENABLED=false -e SESAMEFS_REQUIRE_G4_EVIDENCE=1 -e SESAMEFS_REQUIRE_G5_EVIDENCE=1
  -e CASSANDRA_HOSTS=cassandra-na:9042 -e CASSANDRA_LOCAL_DC=dc-na
  -e CASSANDRA_USERNAME= -e CASSANDRA_PASSWORD= -e CASSANDRA_TIMEOUT=60s
  -e CASSANDRA_REPLICATION_DCS=dc-na:1,dc-eu:1,dc-asia:1
  -e X2_DC_HOSTS=dc-na=cassandra-na:9042,dc-eu=cassandra-eu:9042,dc-asia=cassandra-asia:9042
)
command=$(cat <<'BASH'
set -euo pipefail
go build -o /tmp/g5-server ./cmd/sesamefs
/tmp/g5-server migrate
/tmp/g5-server serve >/tmp/g5-server.log 2>&1 &
server=$!
trap 'kill "$server" 2>/dev/null || true' EXIT
ready=0
for attempt in {1..120}; do
  if curl -fsS http://localhost:8080/health >/dev/null; then ready=1; break; fi
  kill -0 "$server" || { cat /tmp/g5-server.log; exit 1; }
  sleep 1
done
((ready)) || { cat /tmp/g5-server.log; exit 1; }
sed 's/\r$//' scripts/go-test-stream.sh >/tmp/g5-stream.sh
bash /tmp/g5-stream.sh -tags integration ./internal/integration \
  -run '^TestG4Cassandra|^TestG5Cassandra|^TestP3_WriterInAnotherDatacenterObservesTheFence$' \
  -count=1 -v -timeout 5m
BASH
)
docker create --name "$runner" --network sesamefs_default --env-file .env \
  "${environment[@]}" -v "$snapshot_mount:/build" \
  -v sesamefs-g4-gocache:/root/.cache/go-build "$image" bash -c "$command"
docker network connect "$project"_default "$runner"
docker start -a "$runner" 2>&1 | tee "$logs/evidence.log"
[[ $(docker inspect --format '{{.State.ExitCode}}' "$runner") == 0 ]]
printf 'G5 directed healthy three-DC evidence PASS. Logs: %s\n' "$logs"
# Named fixture/runner and volumes are retained for inspection; only this project stops.
