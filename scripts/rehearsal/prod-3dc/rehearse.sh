#!/usr/bin/env bash
# Three-DC rehearsal of docker-compose.prod.yml on one Docker host. See README.md.
set -uo pipefail
export MSYS_NO_PATHCONV=1

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../../.." && pwd)"
WORK_REL=".rehearsal-3dc"            # generated env files, relative to the repo root
WORK="$REPO/$WORK_REL"
IMAGE="${REHEARSAL_IMAGE:-sesamefs-rehearsal:local}"
FRONTEND_IMAGE="${REHEARSAL_FRONTEND_IMAGE:-sesamefs-rehearsal-frontend:local}"
REGIONS="na eu asia"
declare -A IP=([na]=10.77.0.11 [eu]=10.77.0.12 [asia]=10.77.0.13)
declare -A SUBNET=([na]=172.30.51.0/24 [eu]=172.30.52.0/24 [asia]=172.30.53.0/24)

cd "$REPO"
say(){ echo "##### $(date -u +%H:%M:%S) $*"; }
hostpath(){ if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else printf '%s' "$1"; fi; }
# docker receives repo-relative paths: MSYS absolute paths (/c/...) break under Docker Desktop.
HERE_REL="scripts/rehearsal/prod-3dc"
pc(){ local r=$1; shift; ENV_FILE="$WORK_REL/.env.$r" docker compose --env-file "$WORK_REL/.env.$r" \
        -f docker-compose.prod.yml -f "$HERE_REL/compose/override.yml" -p "sfs-$r" "$@"; }
ext(){ KMS_B64="${KMS_B64:-unused}" docker compose -f "$HERE_REL/compose/ext.yml" -p r3-ext "$@"; }
set_kv(){ local f=$1 k=$2 v=$3; if grep -qE "^#? ?$k=" "$f"; then sed -i -E "s|^#? ?$k=.*|$k=$v|" "$f"; else echo "$k=$v" >> "$f"; fi; }
ready(){ docker exec "sfs-$1-sesamefs-1" wget -qO- http://127.0.0.1:8080/ready 2>/dev/null; }
wait_ready(){ local r=$1 s=""; for _ in $(seq 1 120); do s=$(ready "$r") && break; sleep 3; done
  echo "[$r] ready=$(echo "$s" | grep -o '"status":"[a-z]*"') backend restarts=$(docker inspect --format '{{.RestartCount}}' "sfs-$r-sesamefs-1" 2>/dev/null)"; }
driver(){ docker exec r3-driver sh "/tmp/$1.sh" "${@:2}"; }

cmd_up(){
  say "networks, stand-ins, images"
  docker network inspect sfs-net >/dev/null 2>&1 || docker network create sfs-net >/dev/null
  docker network create --subnet 10.77.0.0/24 r3-vrack >/dev/null
  docker network create --subnet 203.0.113.0/24 r3-idpnet >/dev/null
  docker network create r3-s3net >/dev/null
  KMS_B64=$(head -c 32 /dev/urandom | base64 | tr -d '\n'); export KMS_B64
  ext up -d --wait minio && ext up minio-init >/dev/null && ext up -d idp central-nginx || { echo "stand-ins failed to start" >&2; exit 1; }
  if [ "${REHEARSAL_SKIP_BUILD:-0}" != 1 ]; then
    docker build -q -t "$IMAGE" -f Dockerfile . >/dev/null && docker build -q -t "$FRONTEND_IMAGE" frontend >/dev/null || { echo "image build failed" >&2; exit 1; }
  fi

  say "DEPLOY Step M1: one env per region from the shipped template, secrets generated once"
  mkdir -p "$WORK"
  local tmp; tmp=$(mktemp -d); cp .env.prod.example "$tmp/"
  ( cd "$tmp" && bash "$REPO/scripts/prod-preflight.sh" --init-env >/dev/null )
  cp "$tmp/.env" "$WORK/.env.base"; rm -rf "$tmp"
  for r in $REGIONS; do
    local f="$WORK/.env.$r" ip=${IP[$r]}; cp "$WORK/.env.base" "$f"
    set_kv "$f" SESAMEFS_IMAGE "$IMAGE"; set_kv "$f" FRONTEND_IMAGE "$FRONTEND_IMAGE"
    set_kv "$f" S3_ACCESS_KEY_ID rehearsalkey; set_kv "$f" S3_SECRET_ACCESS_KEY rehearsalsecret123
    for c in NA EU ASIA; do
      set_kv "$f" "S3_CLASS_HOT_S3_${c}_BUCKET" "rehearsal-$(echo "$c" | tr 'A-Z' 'a-z')"
      set_kv "$f" "S3_CLASS_HOT_S3_${c}_ENDPOINT" http://s3.rehearsal.test:9000
    done
    set_kv "$f" SERVER_REGION "$r"; set_kv "$f" DEPLOY_ID "$r"
    set_kv "$f" CASSANDRA_DC "dc-$r"; set_kv "$f" CASSANDRA_LOCAL_DC "dc-$r"; set_kv "$f" CASSANDRA_RACK rack1
    set_kv "$f" CASSANDRA_SEEDS "${IP[na]},${IP[eu]}"
    set_kv "$f" CASSANDRA_BIND_IP "$ip"; set_kv "$f" CASSANDRA_RPC_ADDRESS 0.0.0.0
    set_kv "$f" CASSANDRA_BROADCAST_ADDRESS "$ip"; set_kv "$f" CASSANDRA_BROADCAST_RPC_ADDRESS "$ip"
    set_kv "$f" CASSANDRA_REPLICATION_DCS dc-na:1,dc-eu:1,dc-asia:1
    set_kv "$f" CASSANDRA_DATA_DIR cassdata
    set_kv "$f" CASSANDRA_MAX_HEAP_SIZE 1G; set_kv "$f" CASSANDRA_HEAP_NEWSIZE 200M
    set_kv "$f" CORS_ALLOWED_ORIGINS "http://$r.files.rehearsal.test"
    set_kv "$f" FIRST_SUPERADMIN_EMAIL admin@rehearsal.test
    set_kv "$f" OIDC_ISSUER http://idp.rehearsal.test:8000
    set_kv "$f" OIDC_CLIENT_ID rehearsal-client; set_kv "$f" OIDC_CLIENT_SECRET rehearsal-client-secret
    set_kv "$f" OIDC_REDIRECT_URIS "http://$r.files.rehearsal.test/sso/,http://$r.files.rehearsal.test/oauth/callback/"
    set_kv "$f" BILLING_URL https://accounts.rehearsal.test/billing/
    set_kv "$f" ACCOUNTS_DELETE_ACCOUNT_URL https://accounts.rehearsal.test/delete/
    set_kv "$f" ACCOUNTS_ORG_USER_MANAGEMENT_URL "https://accounts.rehearsal.test/orgs/{org_id}/members/"
    set_kv "$f" ONLYOFFICE_API_JS_URL https://office.rehearsal.test/web-apps/apps/api/documents/api.js
    set_kv "$f" SERVER_TRUSTED_PROXIES "${SUBNET[$r]}"; set_kv "$f" INTERNAL_SUBNET "${SUBNET[$r]}"
    # Expected preflight refusals here: the stand-in IdP is plain http and the data dir is a volume.
    ( set -a; . "$f"; set +a; bash scripts/prod-preflight.sh 2>&1 | grep -E '\[FAIL\]' | grep -vE 'OIDC_ISSUER|CASSANDRA_DATA_DIR' | sed "s/^/[$r] /" )
  done

  say "DEPLOY Step M4.2-4: Cassandra, one node at a time"
  for r in $REGIONS; do
    pc "$r" up -d --wait cassandra >/dev/null 2>&1 || { echo "[$r] cassandra did not become healthy; see: docker logs sfs-$r-cassandra-1" >&2; exit 1; }
    echo "[$r] cassandra healthy"
  done
  docker exec sfs-na-cassandra-1 nodetool status | grep -E '^(UN|DN|UJ)' | cut -c1-30

  say "DEPLOY Step M4.5: bootstrap once, from NA"
  pc na --profile bootstrap up --exit-code-from cassandra-bootstrap cassandra-bootstrap >/dev/null 2>&1
  local rc=$?; echo "bootstrap exit=$rc"
  docker logs sfs-na-cassandra-bootstrap-1 2>&1 | grep -E 'attempt|converged' | tail -3
  [ "$rc" = 0 ] || { echo "bootstrap failed; not starting backends" >&2; exit 1; }

  say "DEPLOY Step M4.8: first backend alone, then the others"
  for r in $REGIONS; do pc "$r" up -d sesamefs frontend >/dev/null 2>&1; wait_ready "$r"; done
  for r in $REGIONS; do
    echo "[$r] $(docker exec "sfs-$r-cassandra-1" sh -c "grep -E '^(cas_contention_timeout|write_request_timeout):' /etc/cassandra/cassandra.yaml | tr '\n' ' '")"
  done

  say "driver container and login"
  docker rm -f r3-driver >/dev/null 2>&1
  docker run --name r3-driver --network sfs-net -v "$(hostpath "$HERE/driver"):/h:ro" -d alpine:3.21 sleep infinity >/dev/null
  docker network connect r3-idpnet r3-driver
  docker exec r3-driver sh -c 'apk add --no-cache curl jq coreutils findutils >/dev/null 2>&1; for f in /h/*.sh; do sed "s/\r$//" "$f" > /tmp/$(basename "$f"); done'
  cmd_smoke
}

cmd_smoke(){ say "cross-region matrix"; driver func; }
cmd_usage(){ say "usage shapes ${1:-run}"; driver usage "${1:-run}"; }
cmd_contention(){ say "same-library contention"; docker exec -e LISTWAIT="${LISTWAIT:-3}" r3-driver sh /tmp/conc.sh "${1:-run}" "${2:-15}" "${3:-6}"; }
cmd_ops(){ driver ops "$@"; }

cmd_latency(){ # latency add <one-way ms> | latency del
  local action=${1:?add|del} ms=${2:-40}
  for r in $REGIONS; do
    local ip=${IP[$r]} peers=""
    for p in $REGIONS; do [ "$p" = "$r" ] || peers="$peers ${IP[$p]}"; done
    docker run --rm --net "container:sfs-$r-cassandra-1" --cap-add NET_ADMIN nicolaka/netshoot sh -c "
      dev=\$(ip -o -4 addr | awk '/10\.77\./{print \$2}'); tc qdisc del dev \$dev root 2>/dev/null
      if [ '$action' = add ]; then
        tc qdisc add dev \$dev root handle 1: prio
        tc qdisc add dev \$dev parent 1:3 handle 30: netem delay ${ms}ms 5ms
        for p in $peers; do tc filter add dev \$dev protocol ip parent 1: prio 1 u32 match ip dst \$p/32 flowid 1:3; done
      fi" >/dev/null 2>&1 || echo "[$r] netem failed" >&2
  done
  echo "latency $action ${ms}ms one way between Cassandra nodes only"
}

cmd_timeouts(){ # rehearsal-only rolling change; production uses the compose variables
  local cas=${1:?cas_contention_timeout, e.g. 5000ms} write=${2:?write_request_timeout, e.g. 5000ms}
  for r in $REGIONS; do
    docker exec "sfs-$r-cassandra-1" sh -c "sed -ri 's/^cas_contention_timeout:.*/cas_contention_timeout: $cas/; s/^write_request_timeout:.*/write_request_timeout: $write/' /etc/cassandra/cassandra.yaml"
    docker restart "sfs-$r-cassandra-1" >/dev/null
    for _ in $(seq 1 60); do docker exec sfs-na-cassandra-1 nodetool status 2>/dev/null | grep -c '^UN' | grep -q 3 && break; sleep 5; done
    echo "[$r] restarted with cas=$cas write=$write"
  done
  echo "note: recreating the containers reapplies the compose values"
}

cmd_dc(){ # dc down|up <region>
  local action=${1:?down|up} r=${2:?region}
  if [ "$action" = down ]; then docker stop "sfs-$r-cassandra-1" >/dev/null
  else docker start "sfs-$r-cassandra-1" >/dev/null; for _ in $(seq 1 60); do docker exec sfs-na-cassandra-1 nodetool status 2>/dev/null | grep -c '^UN' | grep -q 3 && break; sleep 5; done; fi
  docker exec sfs-na-cassandra-1 nodetool status 2>/dev/null | grep -E '^(UN|DN)' | cut -c1-30
}

probe_run(){ local pkg=$1; shift
  local pw; pw=$(grep '^CASSANDRA_PASSWORD=' "$WORK/.env.base" | cut -d= -f2)
  docker run --rm --network r3-vrack -v "$(hostpath "$REPO"):/src" -v r3-gomod:/go/pkg/mod -w /src \
    -e APP_USER=sesamefs_app -e APP_PW="$pw" -e HOSTS="dc-na=${IP[na]},dc-eu=${IP[eu]},dc-asia=${IP[asia]}" \
    -e GOFLAGS=-buildvcs=false "$@" golang:1.26.10-trixie go run -tags rehearsal "./scripts/rehearsal/prod-3dc/probe/$pkg"
}
cmd_probe(){ say "raw Cassandra latency, no SesameFS"; probe_run lwt -e N="${N:-40}"; }
cmd_contention_probe(){ say "raw same-partition CAS, no SesameFS"; probe_run contention -e SECS="${SECS:-20}"; }

cmd_status(){
  for r in $REGIONS; do echo "[$r] $(ready "$r" | grep -o '"status":"[a-z]*"') restarts=$(docker inspect --format '{{.RestartCount}}' "sfs-$r-sesamefs-1" 2>/dev/null)"; done
  docker exec sfs-na-cassandra-1 nodetool status 2>/dev/null | grep -E '^(UN|DN)' | cut -c1-30
}

cmd_down(){
  say "teardown"
  docker rm -f r3-driver >/dev/null 2>&1
  for r in $REGIONS; do [ -f "$WORK/.env.$r" ] && pc "$r" --profile bootstrap down -v --remove-orphans >/dev/null 2>&1; done
  ext down -v --remove-orphans >/dev/null 2>&1
  docker network rm r3-vrack r3-idpnet r3-s3net >/dev/null 2>&1
  docker network rm sfs-net >/dev/null 2>&1
  docker volume rm r3-gomod >/dev/null 2>&1
  rm -f "$WORK"/.env.na "$WORK"/.env.eu "$WORK"/.env.asia "$WORK"/.env.base
  rmdir "$WORK" 2>/dev/null
  echo "done; images kept: $IMAGE $FRONTEND_IMAGE"
}

case "${1:-}" in
  up) cmd_up ;;
  smoke) cmd_smoke ;;
  usage) cmd_usage "${@:2}" ;;
  contention) cmd_contention "${@:2}" ;;
  ops) cmd_ops "${@:2}" ;;
  latency) cmd_latency "${@:2}" ;;
  timeouts) cmd_timeouts "${@:2}" ;;
  dc) cmd_dc "${@:2}" ;;
  probe) cmd_probe ;;
  contention-probe) cmd_contention_probe ;;
  status) cmd_status ;;
  down) cmd_down ;;
  *) sed -n '/^## Commands/,/^## Results/p' "$HERE/README.md" | sed '$d'; exit 2 ;;
esac
