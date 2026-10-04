//go:build integration

package api

import (
	"sync"

	"github.com/Sesame-Disk/sesamefs/internal/db"
)

var seafHTTPSingleBarrierMu sync.Mutex
var seafHTTPSingleMaterializedHook struct {
	repoID string
	fn     func(string, db.BlockPhysicalLocation, string)
}

// SetSeafHTTPSingleAfterMaterializedBarrierForTest observes productive confirmed
// registration before metadata publication, scoped to one repository.
func SetSeafHTTPSingleAfterMaterializedBarrierForTest(repoID string, fn func(string, db.BlockPhysicalLocation, string)) func() {
	seafHTTPSingleBarrierMu.Lock()
	previous := seafHTTPSingleMaterializedHook
	seafHTTPSingleMaterializedHook.repoID, seafHTTPSingleMaterializedHook.fn = repoID, fn
	seafHTTPSingleBarrierMu.Unlock()
	return func() {
		seafHTTPSingleBarrierMu.Lock()
		seafHTTPSingleMaterializedHook = previous
		seafHTTPSingleBarrierMu.Unlock()
	}
}

func seafHTTPSingleAfterMaterializedBarrier(repoID, blockID string, location db.BlockPhysicalLocation, operationID string) {
	seafHTTPSingleBarrierMu.Lock()
	hook := seafHTTPSingleMaterializedHook
	seafHTTPSingleBarrierMu.Unlock()
	if hook.repoID != "" && hook.repoID == repoID && hook.fn != nil {
		hook.fn(blockID, location, operationID)
	}
}

var seafHTTPSingleBeforeHeadHook struct {
	repoID string
	fn     func(string, db.BlockPhysicalLocation)
}

// SetSeafHTTPSingleBeforeHeadBarrierForTest schedules an actual competing writer
// after successful exact-P validation. It does not substitute a HEAD CAS result.
func SetSeafHTTPSingleBeforeHeadBarrierForTest(repoID string, fn func(string, db.BlockPhysicalLocation)) func() {
	seafHTTPSingleBarrierMu.Lock()
	previous := seafHTTPSingleBeforeHeadHook
	seafHTTPSingleBeforeHeadHook.repoID, seafHTTPSingleBeforeHeadHook.fn = repoID, fn
	seafHTTPSingleBarrierMu.Unlock()
	return func() {
		seafHTTPSingleBarrierMu.Lock()
		seafHTTPSingleBeforeHeadHook = previous
		seafHTTPSingleBarrierMu.Unlock()
	}
}
func seafHTTPSingleBeforeHeadBarrier(repoID, blockID string, location db.BlockPhysicalLocation) {
	seafHTTPSingleBarrierMu.Lock()
	hook := seafHTTPSingleBeforeHeadHook
	seafHTTPSingleBarrierMu.Unlock()
	if hook.repoID != "" && hook.repoID == repoID && hook.fn != nil {
		hook.fn(blockID, location)
	}
}
