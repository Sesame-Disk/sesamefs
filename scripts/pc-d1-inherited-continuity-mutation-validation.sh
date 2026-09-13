#!/usr/bin/env bash
# PC-D1 source-contract mutations. Every mutation must make the targeted
# decision test RED; a green mutation means the architecture can drift without
# review.
set -uo pipefail

cd "$(dirname "$0")/.."

DOC=docs/PC-D1-INHERITED-DEPENDENCY-CONTINUITY.md
TEST=internal/publication/pc_d1_inherited_continuity_decision_test.go
BACKUP=
TEST_BACKUP=

green() { printf '\033[32m%s\033[0m\n' "$*"; }
red() { printf '\033[31m%s\033[0m\n' "$*" >&2; }

restore() {
	if [ -n "$BACKUP" ] && [ -f "$BACKUP" ]; then
		mv -f "$BACKUP" "$DOC"
	fi
	BACKUP=
	if [ -n "$TEST_BACKUP" ] && [ -f "$TEST_BACKUP" ]; then
		mv -f "$TEST_BACKUP" "$TEST"
	fi
	TEST_BACKUP=
}
fail() { red "FAILED: $*"; restore; exit 1; }
trap restore EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

mutate() {
	local expression="$1"
	BACKUP="$DOC.pcd1bak"
	cp "$DOC" "$BACKUP"
	perl -0pi -e "$expression" "$DOC"
	cmp -s "$DOC" "$BACKUP" && fail "mutation did not apply to $DOC"
}

mutate_test() {
	local expression="$1"
	TEST_BACKUP="$TEST.pcd1bak"
	cp "$TEST" "$TEST_BACKUP"
	perl -0pi -e "$expression" "$TEST"
	cmp -s "$TEST" "$TEST_BACKUP" && fail "mutation did not apply to $TEST"
}

expect_publication_red() {
	local needle="$1" what="$2" output status
	[ -n "$needle" ] || fail "$what has no required failure signature"
	output="$(go test ./internal/publication -count=1 -run '^TestPCD1' 2>&1)"
	status=$?
	if [ "$status" -eq 0 ]; then
		printf '%s\n' "$output"
		fail "$what stayed green"
	fi
	printf '%s\n' "$output" | grep -q "$needle" || {
		printf '%s\n' "$output"
		fail "$what went red without $needle"
	}
	green "RED as required: $what"
}

restore
go test ./internal/publication -count=1 -run '^TestPCD1' >/dev/null 2>&1 || fail "unmutated publication tests are red"
go test ./internal/db -count=1 -run '^TestPCD1' >/dev/null 2>&1 || fail "unmutated DB counterexample is red"

restore
mutate 's/This gives an induction proof:/This treats newly-live as the complete work set:/'
expect_publication_red 'induction proof' 'newly-live scope completeness claim'

restore
mutate 's/IF head_commit_id = H/IF head_commit_id = observed HEAD/g'
expect_publication_red 'IF head_commit_id = H' 'moving-HEAD CAS condition removed'

restore
mutate 's/INHERITED CONTINUITY OWNER = CERTIFIED BASELINE FRONTIER/INHERITED CONTINUITY OWNER = GC/'
expect_publication_red 'owner declaration count' 'inherited-continuity owner changed to GC'

restore
mutate 's/(resolve\/capture exact physical incarnation P)(.*?)(establish durable library-owned liveness)/$3$2$1/s'
expect_publication_red 'baseline handshake order' 'baseline liveness/authority order changed'

restore
mutate 's/non-expiring current-library/non-expiring library/g'
expect_publication_red 'non-expiring current-library' 'TTL-only liveness allowed to justify the witness'

restore
mutate 's/PR merge baseline/PR baseline/'
expect_publication_red 'PR merge baseline' 'current PR merge baseline removed'

restore
mutate 's/compatible global `SERIAL` Paxos domain/compatible local serial domain/g'
expect_publication_red 'global `SERIAL` Paxos domain' 'global SERIAL prerequisite weakened'

restore
mutate_test 's/if state\.head != observedHead \|\|/if false ||/'
expect_publication_red 'mismatched predecessor HEAD unexpectedly advanced' 'HEAD predecessor predicate removed'

restore
mutate_test 's/state\.certifiedHead != observedHead/false/'
expect_publication_red 'missing predecessor certificate unexpectedly advanced' 'certified predecessor predicate removed'

restore
mutate_test 's/state\.contract != contract/false/'
expect_publication_red 'wrong contract version unexpectedly advanced' 'contract-version predecessor predicate removed'

restore
mutate_test 's/state\.head = nextHead/state.head = observedHead/'
expect_publication_red 'certified frontier advance =' 'atomic HEAD update weakened'

restore
mutate_test 's/state\.certifiedHead = nextHead/state.certifiedHead = observedHead/'
expect_publication_red 'certified frontier advance =' 'atomic certified-head update weakened'

restore
go test ./internal/publication ./internal/db -count=1 -run '^TestPCD1' >/dev/null 2>&1 || fail "restored PC-D1 tests are red"
green "PC-D1 decision mutations are red (12/12)"
