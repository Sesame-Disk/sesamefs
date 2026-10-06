//go:build !integration

package v2

func revertDirentsAfterHistoricalEntryBarrier(repoID, itemPath, fsID string) {}
func revertDirentsBeforeHeadBarrier(repoID, itemPath string)                 {}
