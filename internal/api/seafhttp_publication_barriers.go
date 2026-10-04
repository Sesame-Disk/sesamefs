//go:build !integration

package api

import "github.com/Sesame-Disk/sesamefs/internal/db"

func seafHTTPSingleAfterMaterializedBarrier(repoID, blockID string, location db.BlockPhysicalLocation, operationID string) {
}

func seafHTTPSingleBeforeHeadBarrier(repoID, blockID string, location db.BlockPhysicalLocation) {}
