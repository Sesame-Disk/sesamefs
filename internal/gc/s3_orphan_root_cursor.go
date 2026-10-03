package gc

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// A seek checkpoint is scheduling only. Until bounds event-time keys, not an
// insertion snapshot: later-published roots can still enter its range. Eventual
// wrap assumes advancing, bounded-skew application clocks and finite production
// of claims before the cutoff; see ISSUE-GC-ROOT-CYCLE-CLOCK-ASSUMPTION-01.
// The per-tick page bound does not depend on clocks. Keys survive row deletion.
type s3OrphanRootKey struct {
	ClaimedAt    time.Time
	OrgID        uuid.UUID
	BlockID      string
	StorageClass string
	StorageKey   string
	ClaimID      string
}
type s3OrphanRootCursor struct {
	Bucket int
	After  *s3OrphanRootKey
	Until  s3OrphanRootKey
}

func s3RootKey(root S3OrphanRecoveryRootInfo) s3OrphanRootKey {
	return s3OrphanRootKey{normalizeS3OrphanRecoveryTime(root.Authority.ClaimedAt), root.OrgID, root.BlockID, root.Authority.Target.StorageClass, root.Authority.Target.StorageKey, root.Authority.ClaimID}
}
func (key s3OrphanRootKey) root() S3OrphanRecoveryRootInfo {
	return S3OrphanRecoveryRootInfo{OrgID: key.OrgID, BlockID: key.BlockID, Authority: BlockDeleteAuthority{Target: BlockDeleteTarget{StorageClass: key.StorageClass, StorageKey: key.StorageKey}, ClaimID: key.ClaimID, ClaimedAt: key.ClaimedAt}}
}
func (key s3OrphanRootKey) values() []interface{} {
	return []interface{}{key.ClaimedAt, key.OrgID.String(), key.BlockID, key.StorageClass, key.StorageKey, key.ClaimID}
}
func (key s3OrphanRootKey) valid() bool {
	return !key.ClaimedAt.IsZero() && key.OrgID != uuid.Nil && strings.TrimSpace(key.BlockID) != "" && !key.root().Authority.IsZero()
}
func decodeS3OrphanRootCursor(state []byte, bucket int) (s3OrphanRootCursor, error) {
	var cursor s3OrphanRootCursor
	if err := json.Unmarshal(state, &cursor); err != nil {
		return cursor, fmt.Errorf("invalid recovery-root checkpoint: %w", err)
	}
	if cursor.Bucket != bucket || cursor.After == nil || !cursor.After.valid() || !cursor.Until.valid() {
		return cursor, fmt.Errorf("invalid recovery-root checkpoint identity/bucket")
	}
	return cursor, nil
}
