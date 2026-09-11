#!/usr/bin/env bash
# PC-0 §3.4 / ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01 (multi-DC reversion variant).
#
# Reproduces, on the real 3-DC fixture, that the unconditional initial-HEAD writer
# shape (SyncHandler.createInitialCommit, reached from GET /seafhttp/repo/:id/commit/HEAD
# when a session-consistency read observes ""; FSHelper.InitializeLibraryFS has the
# same LoggedBatch shape) can REVERT a HEAD that another datacenter already
# published through the production LWT. The CONTROL leg shows that the same
# initialization expressed as the conditional initializer is rejected from the
# blind datacenter and even reports the real HEAD.
#
# The statements are the exact CQL shapes of the pre-fix createInitialCommit
# (unconditional LoggedBatch) and of FSHelper.InitializeLibraryHeadIfUnset (the
# conditional initializer every initializer now uses), executed through cqlsh so a
# consistency level can be chosen per statement. Divergence is built the way
# scripts/w2-*.sh and scripts/x2-multidc-validation.sh build it: hinted handoff off,
# one DC stopped during the write.
#
# Two fail-closed modes, both after a CAS control leg that must be rejected:
#
#   bug mode (default): step 5b executes the UNCONDITIONAL initializer shape
#     (today's production CQL) from the blind DC and REQUIRES HEAD reverted.
#     Exit 0 only when the recorded bug reproduces.
#   --expect-cas-fix:   step 5b executes the CONDITIONAL initializer shape
#     (IF head_commit_id = null AND created_at != null, the production CQL of
#     FSHelper.InitializeLibraryHeadIfUnset) from the blind DC
#     and REQUIRES it rejected and HEAD survived. Exit 0 only then.
#
# The probe validates CQL shapes through cqlsh, not handler code: the H1
# follow-up must add a handler-level 3-DC leg that drives GetHeadCommit /
# InitializeLibraryFS themselves. --expect-cas-fix is the shape-level acceptance
# of that follow-up, not a substitute for it.
#
# Prerequisites: docker-compose.cassandra-3dc.yaml up and bootstrapped, schema
# applied (go run ./cmd/sesamefs migrate through dc-na; see docs/TESTING.md).
#
# Usage:
#   ./scripts/pc0-initial-head-xdc-probe.sh                  # bug mode: exit 0 iff HEAD reverted
#   ./scripts/pc0-initial-head-xdc-probe.sh --expect-cas-fix # fixed mode: exit 0 iff HEAD survived
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

THREE_DC=(docker compose -f docker-compose.cassandra-3dc.yaml)
ORG=00000000-0000-0000-0000-000000000001
LIB=${LIB:-$(printf '7a1f4c2e-%04x-4000-8000-%012x' "$RANDOM" "$((RANDOM * RANDOM))")}
EXPECT=reverted
for arg in "$@"; do
	case "$arg" in
		--expect-cas-fix) EXPECT=survived ;;
		*) echo "usage: $0 [--expect-cas-fix]" >&2; exit 2 ;;
	esac
done

step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
fail() { printf '\033[31mFAILED: %s\033[0m\n' "$*" >&2; exit 1; }
cql() { local node="$1"; shift; docker exec "sesamefs-cassandra-$node" cqlsh -e "$*" 2>&1 | grep -vE '^\s*$|^-+$|rows\)|^\s*head_commit_id|Consistency level set'; }
wait_healthy() {
	local n="$1"
	for _ in $(seq 1 120); do
		[ "$(docker inspect -f '{{.State.Health.Status}}' "sesamefs-cassandra-$n" 2>/dev/null)" = healthy ] && return 0
		sleep 5
	done
	fail "node $n not healthy"
}
wait_ring() {
	local n="$1"
	for _ in $(seq 1 60); do
		[ "$(docker exec "sesamefs-cassandra-$n" nodetool status 2>/dev/null | grep -c '^UN ')" = 3 ] && return 0
		sleep 5
	done
	fail "ring not UN=3 as seen from $n"
}
read_head() {
	local node="$1" cl="$2"
	docker exec "sesamefs-cassandra-$node" cqlsh -e "CONSISTENCY $cl; SELECT head_commit_id FROM sesamefs.libraries WHERE org_id=$ORG AND library_id=$LIB;" 2>&1 \
		| grep -E "^\s+(C[01]-|null|$)" | sed 's/^ *//; s/^null$//' | head -1
}

cleanup() {
	set +e
	docker start sesamefs-cassandra-eu >/dev/null 2>&1
	for n in na eu asia; do docker exec "sesamefs-cassandra-$n" nodetool enablehandoff >/dev/null 2>&1; done
}
trap cleanup EXIT INT TERM

step "0. all DCs up and schema present; disable hinted handoff so a stopped DC stays divergent"
for n in na eu asia; do wait_healthy "$n"; done
wait_ring na
docker exec sesamefs-cassandra-na cqlsh -e "DESCRIBE TABLE sesamefs.libraries;" >/dev/null 2>&1 || fail "sesamefs.libraries missing; apply the schema first (see docs/TESTING.md)"
for n in na eu asia; do docker exec "sesamefs-cassandra-$n" nodetool disablehandoff >/dev/null; done

step "1. create library $LIB with a null HEAD, visible in EVERY DC (EACH_QUORUM write)"
cql na "CONSISTENCY EACH_QUORUM; INSERT INTO sesamefs.libraries (org_id, library_id, owner_id, name, size_bytes, file_count, created_at, updated_at) VALUES ($ORG, $LIB, $ORG, 'pc0-initial-head-probe', 0, 0, toTimestamp(now()), toTimestamp(now()));"
echo "dc-eu LOCAL_QUORUM sees head='$(read_head eu LOCAL_QUORUM)' (expected empty: null head, as every creation path leaves it before initialization)"

step "2. stop dc-eu (it misses everything from here until restarted; hints are off)"
"${THREE_DC[@]}" stop cassandra-eu >/dev/null

step "3. dc-na publishes HEAD=C1-with-files through the PRODUCTION conditional-initializer shape (IF head_commit_id = null AND created_at != null, SERIAL)"
docker exec sesamefs-cassandra-na cqlsh -e "CONSISTENCY LOCAL_QUORUM; SERIAL CONSISTENCY SERIAL; UPDATE sesamefs.libraries SET head_commit_id='C1-with-files', root_commit_id='C1-with-files', size_bytes=0, file_count=0, updated_at=toTimestamp(now()) WHERE org_id=$ORG AND library_id=$LIB IF head_commit_id = null AND created_at != null;" 2>&1 | grep -E 'applied|True|False|rror' | head -2
[ "$(read_head na LOCAL_QUORUM)" = "C1-with-files" ] || fail "dc-na did not publish C1"
echo "dc-na LOCAL_QUORUM sees head='C1-with-files'"

step "4. restart dc-eu; it rejoins the ring but never receives the C1 update"
"${THREE_DC[@]}" start cassandra-eu >/dev/null
wait_healthy eu
wait_ring eu
blind="$(read_head eu LOCAL_QUORUM)"
echo "dc-eu LOCAL_QUORUM sees head='$blind' (blind: expected empty)"
[ -z "$blind" ] || fail "dc-eu is not blind (head='$blind'); the divergence precondition did not hold, rerun"

# cas_init runs the conditional initializer shape from the blind DC and prints
# the [applied] result row. Fail-closed: the caller asserts the row.
cas_init() {
	local value="$1"
	docker exec sesamefs-cassandra-eu cqlsh -e "CONSISTENCY LOCAL_QUORUM; SERIAL CONSISTENCY SERIAL; UPDATE sesamefs.libraries SET head_commit_id='$value', root_commit_id='$value', size_bytes=0, file_count=0, updated_at=toTimestamp(now()) WHERE org_id=$ORG AND library_id=$LIB IF head_commit_id = null AND created_at != null;" 2>&1 \
		| grep -E '^\s*(True|False)' | head -1
}
assert_cas_rejected_with_real_head() {
	local row="$1" what="$2"
	echo "  $what -> $row"
	case "$row" in
		*False*C1-with-files*) return 0 ;;
		*True*) fail "$what: the conditional initializer APPLIED from the blind DC (HEAD would be overwritten)" ;;
		*) fail "$what: could not read [applied]/current HEAD from the CAS result: '$row'" ;;
	esac
}

step "5a. CONTROL (both modes): the conditional initializer shape (IF head_commit_id = null AND created_at != null) from blind dc-eu must be rejected and report the real HEAD"
assert_cas_rejected_with_real_head "$(cas_init C0-cas-control)" "CAS control"
echo "  -> a SERIAL CAS from the blind DC is rejected and reports C1 (safe shape)"

if [ "$EXPECT" = reverted ]; then
	step "5b. BUG MODE — today's production shape: GetHeadCommit -> createInitialCommit from blind dc-eu (session-CL read saw '', unconditional LoggedBatch)"
	if out="$(docker exec sesamefs-cassandra-eu cqlsh -e "CONSISTENCY LOCAL_QUORUM; BEGIN BATCH UPDATE sesamefs.libraries SET head_commit_id='C0-initial-empty', root_commit_id='C0-initial-empty', size_bytes=0, file_count=0, updated_at=toTimestamp(now()) WHERE org_id=$ORG AND library_id=$LIB; APPLY BATCH;" 2>&1)" && ! grep -qi 'rror' <<<"$out"; then
		echo "  unconditional batch accepted at LOCAL_QUORUM in dc-eu"
	else
		fail "unconditional batch was not accepted: $out"
	fi
else
	step "5b. FIXED MODE — the follow-up's production shape: conditional initializer (IF head_commit_id = null AND created_at != null) from blind dc-eu"
	assert_cas_rejected_with_real_head "$(cas_init C0-initial-empty)" "conditional initializer"
fi

step "6. re-enable hints, let the cluster converge, then read the canonical HEAD"
for n in na eu asia; do docker exec "sesamefs-cassandra-$n" nodetool enablehandoff >/dev/null; done
docker exec sesamefs-cassandra-na nodetool repair sesamefs libraries >/dev/null 2>&1 || true
sleep 5
echo "dc-na   SERIAL      head='$(read_head na SERIAL)'"
echo "dc-na   EACH_QUORUM head='$(read_head na EACH_QUORUM)'"
echo "dc-eu   EACH_QUORUM head='$(read_head eu EACH_QUORUM)'"
echo "dc-asia EACH_QUORUM head='$(read_head asia EACH_QUORUM)'"
final="$(read_head na SERIAL)"

step "7. cleanup probe row"
cql na "CONSISTENCY EACH_QUORUM; DELETE FROM sesamefs.libraries WHERE org_id=$ORG AND library_id=$LIB;"

if [ "$final" = "C1-with-files" ]; then
	observed=survived
	printf '\033[32mRESULT: HEAD survived (C1-with-files).\033[0m\n'
elif [ -n "$final" ]; then
	observed=reverted
	printf '\033[31mRESULT: HEAD REVERTED to %s -- the LWT-published C1 was overwritten by the non-CAS initial-HEAD writer from a blind DC.\033[0m\n' "$final"
else
	fail "could not read the canonical HEAD after convergence"
fi
if [ "$observed" = "$EXPECT" ]; then
	echo "observed=$observed matches expectation ($EXPECT): $([ "$EXPECT" = reverted ] && echo 'recorded bug reproduced (bug mode)' || echo 'conditional initializer shape is safe (fixed mode)')"
	exit 0
fi
fail "observed=$observed but expected $EXPECT"
