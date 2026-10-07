//go:build !integration

package v2

import "github.com/Sesame-Disk/sesamefs/internal/db"

func repairAfterClassifyBarrier(*db.DB, string, publishedBlockReferenceRepairCommitOutcome, error) {}
