//go:build integration

package v2

import "sync"

var w2AfterAuthority struct {
	sync.Mutex
	repoID string
	fn     func()
}

// SetW2PublicationAfterAuthorityForTest pauses only the matching in-process
// writer after its final authority read and before HEAD. Restore via t.Cleanup.
func SetW2PublicationAfterAuthorityForTest(repoID string, fn func()) func() {
	w2AfterAuthority.Lock()
	oldRepo, oldFn := w2AfterAuthority.repoID, w2AfterAuthority.fn
	w2AfterAuthority.repoID, w2AfterAuthority.fn = repoID, fn
	w2AfterAuthority.Unlock()
	return func() {
		w2AfterAuthority.Lock()
		w2AfterAuthority.repoID, w2AfterAuthority.fn = oldRepo, oldFn
		w2AfterAuthority.Unlock()
	}
}

func W2PublicationAfterAuthorityBarrier(repoID string) {
	w2AfterAuthority.Lock()
	repo, fn := w2AfterAuthority.repoID, w2AfterAuthority.fn
	w2AfterAuthority.Unlock()
	if repo == repoID && fn != nil {
		fn()
	}
}

var w2AfterHead struct {
	sync.Mutex
	repoID string
	fn     func()
}

// SetW2PublicationAfterHeadForTest models process death after applied HEAD but
// before permanent-reference promotion. Only the matching library is affected.
func SetW2PublicationAfterHeadForTest(repoID string, fn func()) func() {
	w2AfterHead.Lock()
	oldRepo, oldFn := w2AfterHead.repoID, w2AfterHead.fn
	w2AfterHead.repoID, w2AfterHead.fn = repoID, fn
	w2AfterHead.Unlock()
	return func() { w2AfterHead.Lock(); w2AfterHead.repoID, w2AfterHead.fn = oldRepo, oldFn; w2AfterHead.Unlock() }
}
func w2PublicationAfterHeadBarrier(repoID string) {
	w2AfterHead.Lock()
	repo, fn := w2AfterHead.repoID, w2AfterHead.fn
	w2AfterHead.Unlock()
	if repo == repoID && fn != nil {
		fn()
	}
}

var w2BeforeRepair struct {
	sync.Mutex
	repoID string
	fn     func()
}

func SetW2PublicationBeforeRepairForTest(repoID string, fn func()) func() {
	w2BeforeRepair.Lock()
	oldRepo, oldFn := w2BeforeRepair.repoID, w2BeforeRepair.fn
	w2BeforeRepair.repoID, w2BeforeRepair.fn = repoID, fn
	w2BeforeRepair.Unlock()
	return func() {
		w2BeforeRepair.Lock()
		w2BeforeRepair.repoID, w2BeforeRepair.fn = oldRepo, oldFn
		w2BeforeRepair.Unlock()
	}
}
func W2PublicationBeforeRepairBarrier(repoID string) {
	w2BeforeRepair.Lock()
	repo, fn := w2BeforeRepair.repoID, w2BeforeRepair.fn
	w2BeforeRepair.Unlock()
	if repo == repoID && fn != nil {
		fn()
	}
}
