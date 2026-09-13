#!/usr/bin/env bash
# PC-D1 source-contract mutations. Every mutation must make the targeted
# decision test RED; a green mutation means the architecture can drift without
# review.
set -uo pipefail

cd "$(dirname "$0")/.."

DOC=docs/PC-D1-INHERITED-DEPENDENCY-CONTINUITY.md
BACKUP=

green() { printf '\033[32m%s\033[0m\n' "$*"; }
red() { printf '\033[31m%s\033[0m\n' "$*" >&2; }

restore() {
	if [ -n "$BACKUP" ] && [ -f "$BACKUP" ]; then
		mv -f "$BACKUP" "$DOC"
	fi
	BACKUP=
}
fail() { red "FAILED: $*"; restore; exit 1; }
trap restore EXIT INT TERM

mutate() {
	local expression="$1"
	BACKUP="$DOC.pcd1bak"
	cp "$DOC" "$BACKUP"
	perl -0pi -e "$expression" "$DOC"
	cmp -s "$DOC" "$BACKUP" && fail "mutation did not apply to $DOC"
}

expect_publication_red() {
	local needle="$1" what="$2" output status
	output="$(go test ./internal/publication -count=1 -run '^TestPCD1(DecisionDocumentPinsSingleOwnerAndBoundaries|BaselineHandshakeOrderIsFrozen)$' 2>&1)"
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
mutate 's/(resolve\/capture exact P \+ incarnation)(.*?)(establish durable library-owned liveness)/$3$2$1/s'
expect_publication_red 'baseline handshake order' 'baseline liveness/authority order changed'

restore
go test ./internal/publication ./internal/db -count=1 -run '^TestPCD1' >/dev/null 2>&1 || fail "restored PC-D1 tests are red"
green "PC-D1 decision mutations are red (4/4)"
