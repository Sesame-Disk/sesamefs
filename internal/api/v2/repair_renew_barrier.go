//go:build !integration

package v2

import "github.com/Sesame-Disk/sesamefs/internal/db"

func repairBeforeRenewBarrier(*db.DB, string, string, string) {}
