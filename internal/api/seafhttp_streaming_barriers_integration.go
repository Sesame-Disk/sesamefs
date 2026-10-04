//go:build integration

package api

import (
	"github.com/Sesame-Disk/sesamefs/internal/db"
	"sync"
)

var seafHTTPStreamingBarrierMu sync.Mutex
var seafHTTPStreamingHooks struct {
	repo         string
	block        func(int, string, db.BlockPhysicalLocation, string)
	materialized func(*ChunkUpload)
	head         func()
}

// SetSeafHTTPStreamingBarriersForTest observes confirmed materialization and
// schedules productive writers. Callbacks never replace authority/CAS results.
func SetSeafHTTPStreamingBarriersForTest(repo string, block func(int, string, db.BlockPhysicalLocation, string), materialized func(*ChunkUpload), head func()) func() {
	seafHTTPStreamingBarrierMu.Lock()
	previous := seafHTTPStreamingHooks
	seafHTTPStreamingHooks.repo, seafHTTPStreamingHooks.block, seafHTTPStreamingHooks.materialized, seafHTTPStreamingHooks.head = repo, block, materialized, head
	seafHTTPStreamingBarrierMu.Unlock()
	return func() {
		seafHTTPStreamingBarrierMu.Lock()
		seafHTTPStreamingHooks = previous
		seafHTTPStreamingBarrierMu.Unlock()
	}
}
func seafHTTPStreamingBlockMaterializedBarrier(repo string, index int, block string, p db.BlockPhysicalLocation, operation string) {
	seafHTTPStreamingBarrierMu.Lock()
	h := seafHTTPStreamingHooks
	seafHTTPStreamingBarrierMu.Unlock()
	if h.repo == repo && h.block != nil {
		h.block(index, block, p, operation)
	}
}
func seafHTTPStreamingAfterMaterializedBarrier(repo string, upload *ChunkUpload) {
	seafHTTPStreamingBarrierMu.Lock()
	h := seafHTTPStreamingHooks
	seafHTTPStreamingBarrierMu.Unlock()
	if h.repo == repo && h.materialized != nil {
		h.materialized(upload)
	}
}
func seafHTTPStreamingBeforeHeadBarrier(repo string) {
	seafHTTPStreamingBarrierMu.Lock()
	h := seafHTTPStreamingHooks
	seafHTTPStreamingBarrierMu.Unlock()
	if h.repo == repo && h.head != nil {
		h.head()
	}
}
