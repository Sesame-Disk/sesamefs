package v2

import (
	"os"
	"strings"
	"testing"
)

const lifecycleRecoveryReadReason = "lifecycle recovery no longer reads at a strength that sees other datacenters"

// The permanent-delete resume reads its marker and the lookup at EACH_QUORUM
// (a row acknowledged in another DC must not read as "nothing to resume"), and
// the handler's owner read likewise. The 3-DC harness proves the behavior; this
// pins the source so a downgrade fails without a 3-DC fixture.
func TestLibraryLifecycleRecoveryReadsAreStrong(t *testing.T) {
	raw, err := os.ReadFile("library_delete_helpers.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	body := func(name string) string {
		i := strings.Index(src, "func "+name+"(")
		if i < 0 {
			t.Fatalf("%s: %s not found", lifecycleRecoveryReadReason, name)
		}
		j := strings.Index(src[i+1:], "\nfunc ")
		if j < 0 {
			return src[i:]
		}
		return src[i : i+1+j]
	}
	if n := strings.Count(body("resumeCommittedPermanentDelete"), ".Consistency(gocql.EachQuorum)"); n != 2 {
		t.Errorf("%s: resumeCommittedPermanentDelete has %d EACH_QUORUM reads, want 2 (marker, lookup)", lifecycleRecoveryReadReason, n)
	}
	if !strings.Contains(body("readPermanentDeleteResumeOwner"), ".Consistency(gocql.EachQuorum)") {
		t.Errorf("%s: readPermanentDeleteResumeOwner is not EACH_QUORUM", lifecycleRecoveryReadReason)
	}
	// Bulk discovery: the permanent-delete continuations (global QUORUM, see
	// dbpkg.LibraryLifecyclePendingConsistency) are the discovery source; the
	// session-consistency marker scan is only a second one.
	if !strings.Contains(body("resumeCommittedPermanentDeletes"), "listPendingPermanentDeletesFn(database, wanted)") {
		t.Errorf("%s: resumeCommittedPermanentDeletes does not discover through the continuations", lifecycleRecoveryReadReason)
	}
	i := strings.Index(src, "listPendingPermanentDeletesFn = func(")
	if i < 0 || !strings.Contains(src[i:i+600], "dbpkg.ListLibraryLifecyclePending(") {
		t.Errorf("%s: listPendingPermanentDeletesFn does not read library_lifecycle_pending", lifecycleRecoveryReadReason)
	}
}
