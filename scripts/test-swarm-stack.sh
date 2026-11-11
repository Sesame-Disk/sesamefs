#!/usr/bin/env bash
# Static checks of docker-compose.swarm.yml against docker-compose.prod.yml. Needs Docker
# with the compose plugin; starts nothing.
#  1. Both files render with a complete example env, the stack also with docker stack config.
#  2. Cassandra's start-up script (auth and timeouts) is identical in both files.
#  3. The values both files force for production are identical.
#  4. The Swarm stack refuses to render without each variable it requires.
#  5. The Swarm Cassandra service does not read the region env file: changing an
#     application variable leaves its spec unchanged (no restart on deploy), and
#     application secrets stay out of its environment.
set -uo pipefail
export MSYS_NO_PATHCONV=1
cd "$(dirname "$0")/.."

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
# Docker on Windows needs the host form of the path; elsewhere it is the same path.
if command -v cygpath >/dev/null 2>&1; then tmp=$(cygpath -m "$tmp"); fi
fail=0
ok(){ echo "PASS  $1"; }
ko(){ echo "FAIL  $1"; fail=1; }

cat > "$tmp/region.env" <<'EOF'
SESAMEFS_IMAGE=registry.example/sesamefs:backend-test
FRONTEND_IMAGE=registry.example/sesamefs:frontend-test
DEPLOY_ID=na
SERVER_REGION=na
CASSANDRA_CLUSTER_NAME=sesamefs-prod
CASSANDRA_DC=dc-na
CASSANDRA_LOCAL_DC=dc-na
CASSANDRA_MAX_HEAP_SIZE=2G
CASSANDRA_HEAP_NEWSIZE=400M
CASSANDRA_DATA_DIR=/srv/sesamefs/cassandra
CASSANDRA_BIND_IP=10.0.1.10
CASSANDRA_SEEDS=10.0.1.10,10.0.2.20
SESAMEFS_INTERNAL_SUBNET=10.231.0.0/24
SESAMEFS_MEMORY_LIMIT=4G
CASSANDRA_MEMORY_LIMIT=6G
CASSANDRA_SUPERUSER_PASSWORD=test-superuser-password
# Application-only values: Cassandra must neither see them nor restart when they change.
OIDC_CLIENT_SECRET=canary-app-secret
OIDC_REDIRECT_URIS=https://files.example/sso/
# Hostile values: the files must force these back.
GC_ENABLED=true
AUTH_DEV_MODE=true
SESAMEFS_SYNC_W2_CRASH_AFTER_HEAD_CAS_ONCE=1
EOF
echo "ENV_FILE=$tmp/region.env" >> "$tmp/region.env"

render(){ # file -> json on stdout
  docker compose --env-file "$tmp/region.env" -f "$1" config --format json 2>"$tmp/err"
}
render docker-compose.swarm.yml > "$tmp/swarm.json" && ok "swarm stack renders" || { ko "swarm stack renders: $(head -c 300 "$tmp/err")"; exit 1; }
render docker-compose.prod.yml > "$tmp/prod.json" && ok "prod compose renders" || { ko "prod compose renders: $(head -c 300 "$tmp/err")"; exit 1; }
# docker stack deploy uses its own, stricter loader (a bare "$" that docker compose accepts
# fails there), and interpolates only from the shell environment.
if ( set -a; . "$tmp/region.env"; set +a; docker stack config -c docker-compose.swarm.yml ) >/dev/null 2>"$tmp/err"; then
  ok "swarm stack renders with docker stack config"
else
  ko "swarm stack renders with docker stack config: $(head -c 300 "$tmp/err")"
fi

# Extracts with the python in the cassandra image would be overkill; jq is not assumed,
# so a tiny python3 reads the JSON when available, else docker runs one.
py(){ if command -v python3 >/dev/null 2>&1 && python3 -c 'import json' 2>/dev/null; then python3 "$@"; else
  docker run --rm -i -v "$tmp:$tmp" python:3.12-alpine python3 "$@"; fi; }

cat > "$tmp/q.py" <<'EOF'
import json, sys
what, path = sys.argv[1], sys.argv[2]
svc = json.load(open(path))["services"]
if what == "cassandra-command":
    print("\n".join(svc["cassandra"]["command"]))
elif what == "forced":
    env = svc["sesamefs"]["environment"]
    for k in ("GC_ENABLED", "AUTH_DEV_MODE", "SESAMEFS_SYNC_W2_CRASH_AFTER_HEAD_CAS_ONCE", "METRICS_ENABLED"):
        print("%s=%s" % (k, env.get(k)))
elif what == "cassandra-spec":
    print(json.dumps(svc["cassandra"], sort_keys=True))
elif what == "cassandra-env":
    print("\n".join(sorted(svc["cassandra"].get("environment", {}))))
EOF
for what in cassandra-command forced; do
  py "$tmp/q.py" "$what" "$tmp/swarm.json" > "$tmp/s.$what"
  py "$tmp/q.py" "$what" "$tmp/prod.json" > "$tmp/p.$what"
  if [ -s "$tmp/s.$what" ] && cmp -s "$tmp/s.$what" "$tmp/p.$what"; then ok "$what identical"
  else ko "$what differs:"; diff "$tmp/p.$what" "$tmp/s.$what" | head -20; fi
done
for kv in GC_ENABLED=false AUTH_DEV_MODE=false SESAMEFS_SYNC_W2_CRASH_AFTER_HEAD_CAS_ONCE=0; do
  grep -qx "$kv" "$tmp/s.forced" && ok "$kv forced despite the env file" || ko "$kv not forced"
done

# Cassandra reads only its own variables. With the whole env file, every application
# change (OIDC_REDIRECT_URIS, say) changed its spec: Swarm restarted it on deploy and the
# new backend failed against a node still starting.
sed -e 's|^OIDC_REDIRECT_URIS=.*|OIDC_REDIRECT_URIS=https://files.example/sso/,https://other.example/sso/|' \
    -e "s|^ENV_FILE=.*|ENV_FILE=$tmp/region2.env|" "$tmp/region.env" > "$tmp/region2.env"
docker compose --env-file "$tmp/region2.env" -f docker-compose.swarm.yml config --format json > "$tmp/swarm2.json" 2>/dev/null
py "$tmp/q.py" cassandra-spec "$tmp/swarm.json" > "$tmp/c1"
py "$tmp/q.py" cassandra-spec "$tmp/swarm2.json" > "$tmp/c2"
if [ -s "$tmp/c1" ] && cmp -s "$tmp/c1" "$tmp/c2"; then ok "cassandra spec unchanged by an application variable"
else ko "cassandra spec changes with OIDC_REDIRECT_URIS (Swarm would restart Cassandra)"; fi
py "$tmp/q.py" cassandra-env "$tmp/swarm.json" > "$tmp/cenv"
grep -qx OIDC_CLIENT_SECRET "$tmp/cenv" && ko "cassandra environment carries application secrets" \
  || ok "cassandra environment carries no application secrets"
grep -qx CASSANDRA_SUPERUSER_PASSWORD "$tmp/cenv" && ok "cassandra gets CASSANDRA_SUPERUSER_PASSWORD (healthcheck)" \
  || ko "cassandra lacks CASSANDRA_SUPERUSER_PASSWORD"

for v in SESAMEFS_IMAGE FRONTEND_IMAGE DEPLOY_ID SERVER_REGION CASSANDRA_BIND_IP CASSANDRA_SEEDS \
         CASSANDRA_DC CASSANDRA_LOCAL_DC CASSANDRA_DATA_DIR SESAMEFS_INTERNAL_SUBNET \
         SESAMEFS_MEMORY_LIMIT CASSANDRA_MEMORY_LIMIT CASSANDRA_MAX_HEAP_SIZE CASSANDRA_HEAP_NEWSIZE \
         CASSANDRA_SUPERUSER_PASSWORD ENV_FILE; do
  grep -v "^$v=" "$tmp/region.env" > "$tmp/missing.env"
  if docker compose --env-file "$tmp/missing.env" -f docker-compose.swarm.yml config >/dev/null 2>&1; then
    ko "renders without $v"
  else
    ok "requires $v"
  fi
done

[ "$fail" = 0 ] && echo "RESULT PASS" || { echo "RESULT FAIL"; exit 1; }
