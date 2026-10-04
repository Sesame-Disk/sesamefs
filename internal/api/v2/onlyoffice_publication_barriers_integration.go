//go:build integration

package v2

import "sync"

var onlyOfficeBarrierMu sync.Mutex
var onlyOfficeMaterializedHook struct {
	repoID string
	fn     func()
}

// SetOnlyOfficeAfterMaterializedBarrierForTest observes the real callback after
// confirmed materialization and before metadata publication. It changes no
// query results and affects only the matching in-process repository.
func SetOnlyOfficeAfterMaterializedBarrierForTest(repoID string, fn func()) func() {
	onlyOfficeBarrierMu.Lock()
	previous := onlyOfficeMaterializedHook
	onlyOfficeMaterializedHook.repoID, onlyOfficeMaterializedHook.fn = repoID, fn
	onlyOfficeBarrierMu.Unlock()
	return func() {
		onlyOfficeBarrierMu.Lock()
		onlyOfficeMaterializedHook = previous
		onlyOfficeBarrierMu.Unlock()
	}
}

func onlyOfficeAfterMaterializedBarrier(repoID string) {
	onlyOfficeBarrierMu.Lock()
	hook := onlyOfficeMaterializedHook
	onlyOfficeBarrierMu.Unlock()
	if hook.repoID != "" && hook.repoID == repoID && hook.fn != nil {
		hook.fn()
	}
}
