//go:build !integration

package v2

func restoreTrashAfterHistoricalEntryBarrier(repoID, fsID string) {}
func restoreTrashBeforeHeadBarrier(repoID string)                 {}
