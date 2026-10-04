//go:build !integration

package v2

func revertFileAfterHistoricalEntryBarrier(repoID, fsID string) {}
func revertFileBeforeHeadBarrier(repoID string)                 {}
