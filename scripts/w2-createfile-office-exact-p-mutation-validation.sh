#!/usr/bin/env bash
# Run inside the disposable Docker test runner against real Cassandra/MinIO.
# Only the CreateFile body is mutated; the host checkout is never mounted writable.
set -euo pipefail
cd /build
source_file=internal/api/v2/files.go
backup=$(mktemp)
cp "$source_file" "$backup"
trap 'cp "$backup" "$source_file"; rm -f "$backup"' EXIT
export SESAMEFS_REQUIRE_W2_CREATEFILE_EXACT_P_EVIDENCE=1
pattern='^TestW2CreateFileOfficeTemplateExactPlacementBeforeHead$'
run_evidence() {
  go test -tags integration -v -count=1 -timeout 5m -run "$pattern" ./internal/integration
}
expect_red() {
  local name=$1 needle=$2 status=0
  shift 2
  "$@" >/tmp/w2-createfile-mutation.log 2>&1 || status=$?
  if [ "$status" -eq 0 ]; then
    cat /tmp/w2-createfile-mutation.log
    echo "FAILED: $name stayed GREEN" >&2
    exit 1
  fi
  if grep -Eq '\[build failed\]|syntax error|undefined:' /tmp/w2-createfile-mutation.log || ! grep -Eq "$needle" /tmp/w2-createfile-mutation.log; then
    cat /tmp/w2-createfile-mutation.log
    echo "FAILED: $name failed without the required semantic evidence" >&2
    exit 1
  fi
  echo "RED as required: $name"
}
mutate_createfile() {
  cp "$backup" "$source_file"
  W2_MUTATION=$1 perl -0pi -e '
    $start = index($_, "func (h *FileHandler) CreateFile(c *gin.Context) {");
    $end = index($_, "// DeleteDirectory deletes", $start);
    die "CreateFile scope missing" if $start < 0 || $end < 0;
    $body = substr($_, $start, $end - $start);
    if ($ENV{W2_MUTATION} eq "omit-placement") {
      $body =~ s/validateCommitBlockPublicationFences\(orgID, commitBlocks\)/validateCommitBlockPublicationFences(orgID, commitBlocks[:0])/ or die "placement gate anchor missing";
    } elsif ($ENV{W2_MUTATION} eq "wrong-storage-key") {
      $body =~ s/storageKey:\s+templateStorageKey,/storageKey: templateStorageKey + ".wrong",/ or die "storage key anchor missing";
    } elsif ($ENV{W2_MUTATION} eq "external-hash") {
      $body =~ s/blockID:\s+templateBlockData.Hash,/blockID: externalBlockID,/ or die "SHA-256 anchor missing";
    } else { die "unknown mutation"; }
    substr($_, $start, $end - $start) = $body;
  ' "$source_file"
  if cmp -s "$backup" "$source_file"; then echo 'mutation did not apply' >&2; exit 1; fi
}
run_evidence >/tmp/w2-createfile-baseline.log 2>&1 || { cat /tmp/w2-createfile-baseline.log; exit 1; }
echo 'GREEN baseline: all nine Office legs + empty file'
mutate_createfile omit-placement
expect_red 'omit exact placement' 'gcCommittedBeforeStage: create status=201.*want 409' run_evidence
mutate_createfile wrong-storage-key
expect_red 'corrupt storage_key' 'writerFirst: create status=409.*want 201' run_evidence
mutate_createfile external-hash
expect_red 'pass external SHA-1 instead of internal SHA-256' 'writerFirst: create status=500.*want 201' run_evidence
cp "$backup" "$source_file"
expect_red 'filter out required legs' 'requires all named W2-6a legs; missing=' go test -tags integration -v -count=1 -timeout 5m -run '^TestW2CreateFileOfficeTemplateExactPlacementBeforeHead$/docx/writerFirst$' ./internal/integration
expect_red 'unavailable backend' 'Backend not available' env SESAMEFS_URL=http://127.0.0.1:1 go test -tags integration -count=1 -timeout 5m -run "$pattern" ./internal/integration
echo 'PASS: 3 CreateFile production mutations + 2 fail-closed evidence gates'
