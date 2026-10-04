//go:build !integration

package api

import "github.com/Sesame-Disk/sesamefs/internal/db"

func seafHTTPStreamingBlockMaterializedBarrier(string, int, string, db.BlockPhysicalLocation, string) {
}
func seafHTTPStreamingAfterMaterializedBarrier(string, *ChunkUpload) {}
func seafHTTPStreamingBeforeHeadBarrier(string)                      {}
