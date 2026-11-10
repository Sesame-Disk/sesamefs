#!/usr/bin/env bash
# Checks of scripts/prod-preflight.sh that need a Linux filesystem with real ownership.
# Needs Docker; runs one throwaway container and touches nothing on the host.
#  1. --swarm accepts a CASSANDRA_DATA_DIR owned by the Cassandra image user (uid 999)
#     that the deploy user cannot write: the cassandra entrypoint chowns the directory to
#     that user on every start, so write access for the deploy user cannot be required.
#  2. --swarm still fails when CASSANDRA_DATA_DIR does not exist.
#  3. Without --swarm (docker-compose.prod.yml) the writability check is unchanged.
# Only the CASSANDRA_DATA_DIR lines of the preflight are checked; the rest of its output
# fails on purpose, because the container has no production environment.
set -uo pipefail
export MSYS_NO_PATHCONV=1
cd "$(dirname "$0")/.."
src=$(pwd)
if command -v cygpath >/dev/null 2>&1; then src=$(cygpath -m "$src"); fi

fail=0
ok(){ echo "PASS  $1"; }
ko(){ echo "FAIL  $1"; fail=1; }

out=$(docker run --rm -v "$src/scripts:/s:ro" bash:5.2 bash -c '
  mkdir -p /data/cassandra && chown 999:999 /data/cassandra && chmod 755 /data/cassandra
  adduser -D -u 1001 deploy >/dev/null 2>&1
  run(){ su deploy -s /usr/local/bin/bash -c "cd /tmp && CASSANDRA_DATA_DIR=$1 bash /s/prod-preflight.sh $2" 2>&1 \
           | grep "CASSANDRA_DATA_DIR" | grep -v "must be an absolute" | sed "s/^/$3 /"; }
  run /data/cassandra --swarm       owned999-swarm
  run /data/missing   --swarm       missing-swarm
  run /data/cassandra ""            owned999-compose
' 2>&1)

line(){ printf '%s\n' "$out" | grep "^$1 " | head -1; }
case "$(line owned999-swarm)" in
  *"[ok]"*) ok "--swarm accepts a data dir owned by uid 999 that the deploy user cannot write" ;;
  *) ko "--swarm data dir owned by uid 999: $(line owned999-swarm)" ;;
esac
case "$(line missing-swarm)" in
  *"[FAIL]"*"does not exist"*) ok "--swarm fails when the data dir does not exist" ;;
  *) ko "--swarm missing data dir: $(line missing-swarm)" ;;
esac
case "$(line owned999-compose)" in
  *"[FAIL]"*"not writable"*) ok "compose mode still requires a writable data dir" ;;
  *) ko "compose mode data dir: $(line owned999-compose)" ;;
esac

[ "$fail" = 0 ] && echo "RESULT PASS" || { echo "RESULT FAIL"; printf '%s\n' "$out" | sed 's/^/  | /'; exit 1; }
