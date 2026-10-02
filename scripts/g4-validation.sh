#!/usr/bin/env bash
# Current sources, complete Go suite and required real-service integration evidence.
# Usage: scripts/g4-validation.sh [--stage all|unit|integration] [--scope full|g4]
set -euo pipefail

stage=all
scope=full
image=sesamefs-go-integration-test:latest
network=sesamefs_default
env_file=.env
while (($#)); do
  case "$1" in
    --stage) stage=$2; shift 2 ;;
    --scope) scope=$2; shift 2 ;;
    --image) image=$2; shift 2 ;;
    --network) network=$2; shift 2 ;;
    --env-file) env_file=$2; shift 2 ;;
    *) printf 'Unknown argument: %s\n' "$1" >&2; exit 2 ;;
  esac
done
case "$stage" in all|unit|integration) ;; *) echo 'Invalid --stage' >&2; exit 2 ;; esac
case "$scope" in full|g4) ;; *) echo 'Invalid --scope' >&2; exit 2 ;; esac

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"
snapshot=$(mktemp -d "${TMPDIR:-/tmp}/sesamefs-g4-XXXXXXXX")
run=${snapshot##*/}
logs="$repo_root/tmp/${run#sesamefs-}"
mkdir -p "$logs"
git ls-files -z --cached --others --exclude-standard > "$logs/source-files.bin"
while IFS= read -r -d '' source; do
  [[ -f "$source" ]] || continue
  mkdir -p "$snapshot/$(dirname "$source")"
  cp -p -- "$source" "$snapshot/$source"
done < "$logs/source-files.bin"
snapshot_mount=$snapshot
# Git Bash invokes the Windows Docker client; its bind source must be native.
if command -v cygpath >/dev/null 2>&1; then
  snapshot_mount=$(cygpath -w "$snapshot")
fi
mounts=(-v "$snapshot_mount:/build" -v sesamefs-g4-gocache:/root/.cache/go-build)
run_docker() {
  local log=$1
  shift
  MSYS_NO_PATHCONV=1 docker "$@" 2>&1 | tee "$logs/$log"
}
if [[ "$stage" == all || "$stage" == unit ]]; then
  run_docker unit.log run --rm "${mounts[@]}" "$image" go test ./... -count=1 -timeout 10m
fi
if [[ "$stage" == all || "$stage" == integration ]]; then
  environment=(-e SESAMEFS_URL=http://sesamefs:8080
    -e SESAMEFS_URL_2=http://sesamefs-node-2:8080
    -e SESAMEFS_URL_3=http://sesamefs-node-3:8080
    -e SESAMEFS_REQUIRE_G4_EVIDENCE=1)
  go_args=(-tags integration -count=1 -v -timeout 15m ./internal/integration/...)
  if [[ "$scope" == g4 ]]; then
    go_args+=(-run '^TestG4CassandraMinIOPhysicalLifeCoexistence$|^TestEveryEvidenceGateIsWiredIntoTestMain$')
  else
    gates=(W24_CHARACTERIZATION P2_EVIDENCE P3_EVIDENCE P4A_EVIDENCE P4B_EVIDENCE
      G1_ORPHAN_EVIDENCE R26_EVIDENCE R3_CHARACTERIZATION X1_NONOVERLAP_CHARACTERIZATION
      BORROWEDFS_OWN_LIVENESS_EVIDENCE SESSIONUPLOAD_OWN_LIVENESS_EVIDENCE W2_POST_HEAD_EVIDENCE
      W2_SYNC_PUTBLOCK_HEAD_EVIDENCE W2_UPLOADFILE_EXACT_P_EVIDENCE W2_CREATEFILE_EXACT_P_EVIDENCE
      W2_PUBLICATION_CONTINUITY_EVIDENCE W2_CLOSURE_EVIDENCE IDENTITY_AUTHORITY_EVIDENCE)
    for gate in "${gates[@]}"; do environment+=(-e "SESAMEFS_REQUIRE_${gate}=1"); done
  fi
  # Use the repository's streaming runner. Normalize only its checkout CRLF in
  # the disposable container copy, as Dockerfile.gotest does on image builds.
  run_docker integration.log run --rm --network "$network" --env-file "$env_file" \
    "${environment[@]}" "${mounts[@]}" "$image" bash -c \
    'set -euo pipefail; sed "s/\r$//" scripts/go-test-stream.sh > /tmp/g4-go-test-stream.sh; bash /tmp/g4-go-test-stream.sh "$@"' \
    -- "${go_args[@]}"
fi
printf 'G4 validation passed. Logs: %s; source snapshot: %s\n' "$logs" "$snapshot"
