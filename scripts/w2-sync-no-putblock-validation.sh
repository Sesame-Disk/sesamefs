#!/usr/bin/env bash
# Reproducible, fully containerized W2-4 characterization. Every Compose
# project, named volume, runner, and published port is scoped to this run.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

project="sesamefs-w24-$(date +%s)-$$"
port_base=$((20000 + $$ % 20000))
override="$(mktemp /tmp/w2-4-compose.XXXXXX.yaml)"
image="${project}-runner"

cat >"$override" <<'YAML'
services:
  sesamefs:
    environment:
      GC_ENABLED: "false"
      SERVER_URL: http://sesamefs:8080
      AUTH_DEV_MODE: "true"
      OIDC_ENABLED: "false"
YAML

compose() {
	SESAMEFS_HOST_PORT="$port_base" \
	CASSANDRA_HOST_PORT="$((port_base + 1))" \
	MINIO_API_HOST_PORT="$((port_base + 2))" \
	MINIO_CONSOLE_HOST_PORT="$((port_base + 3))" \
	docker compose -p "$project" -f docker-compose.yaml -f "$override" "$@"
}

cleanup() {
	local status=$?
	trap - EXIT INT TERM
	compose down --volumes --remove-orphans >/dev/null 2>&1 || true
	docker image rm "$image" >/dev/null 2>&1 || true
	rm -f "$override"
	exit "$status"
}
trap cleanup EXIT INT TERM

compose build sesamefs
compose up -d sesamefs
docker build -t "$image" -f Dockerfile.gotest .

docker run --rm \
	--network "${project}_default" \
	--env-file .env \
	-e SESAMEFS_URL=http://sesamefs:8080 \
	-e SESAMEFS_REQUIRE_W24_CHARACTERIZATION=1 \
	"$image" /bin/sh -ec '
	./scripts/wait-for-http.sh http://sesamefs:8080/health sesamefs
	go test -tags integration -run "^TestW2SyncNoPutBlock$" -count=1 -v -timeout 10m ./internal/integration
	set +e
	SESAMEFS_W24_ASSERT_SAFETY=1 go test -tags integration -run "^TestW2SyncNoPutBlock/direct/gcBeforeStage$" -count=1 -v -timeout 3m ./internal/integration > /tmp/w2-4-safety-red.log 2>&1
	status=$?
	set -e
	if [ "$status" -eq 0 ] || ! grep -q "W2-4 VIOLATION: D(P) committed AND HEAD advanced" /tmp/w2-4-safety-red.log; then
		cat /tmp/w2-4-safety-red.log
		echo "W2-4 safety assertion did not produce its expected RED" >&2
		exit 1
	fi
	cat /tmp/w2-4-safety-red.log
	echo "W2-4 counterexample reproduced; safety assertion is RED as expected"
'
