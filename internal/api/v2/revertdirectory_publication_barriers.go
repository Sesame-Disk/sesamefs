//go:build !integration

package v2

func revertDirectoryAfterHistoricalEntryBarrier(repoID, fsID string) {}
func revertDirectoryBeforeHeadBarrier(repoID string)                 {}
