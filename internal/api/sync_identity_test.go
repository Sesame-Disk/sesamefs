package api

import "testing"

func TestSyncCommitStoredIdentityMatchesSnapshot(t *testing.T) {
	parent := "parent-1"
	cases := []struct {
		name     string
		existing map[string]interface{}
		want     *string
		root     string
		matches  bool
	}{
		{name: "same parent and root", existing: map[string]interface{}{"parent_id": parent, "root_fs_id": "root-1"}, want: &parent, root: "root-1", matches: true},
		{name: "nil parent matches empty stored parent", existing: map[string]interface{}{"parent_id": nil, "root_fs_id": "root-1"}, root: "root-1", matches: true},
		{name: "byte values match", existing: map[string]interface{}{"parent_id": []byte(parent), "root_fs_id": []byte("root-1")}, want: &parent, root: "root-1", matches: true},
		{name: "different root conflicts", existing: map[string]interface{}{"parent_id": parent, "root_fs_id": "root-2"}, want: &parent, root: "root-1", matches: false},
		{name: "different parent conflicts", existing: map[string]interface{}{"parent_id": "parent-2", "root_fs_id": "root-1"}, want: &parent, root: "root-1", matches: false},
		{name: "missing identity conflicts", existing: map[string]interface{}{"root_fs_id": "root-1"}, want: &parent, root: "root-1", matches: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := syncCommitStoredIdentityMatches(tc.existing, tc.want, tc.root); got != tc.matches {
				t.Fatalf("syncCommitStoredIdentityMatches = %v, want %v", got, tc.matches)
			}
		})
	}
}

func TestClassifySyncFSObjectRow(t *testing.T) {
	expected := syncFSObjectIdentity{
		objType:    "file",
		sizeBytes:  7,
		dirEntries: "[]",
		blockIDs:   []string{"block-a"},
	}
	cases := []struct {
		name string
		row  map[string]interface{}
		want syncFSObjectRowState
	}{
		{
			name: "metadata-only placeholder",
			row:  map[string]interface{}{"obj_name": "child.txt", "full_path": "/child.txt"},
			want: syncFSObjectRowPlaceholder,
		},
		{
			name: "complete identical file",
			row:  map[string]interface{}{"obj_type": "file", "size_bytes": int64(7), "dir_entries": "[]", "block_ids": []string{"block-a"}},
			want: syncFSObjectRowComplete,
		},
		{
			name: "partial matching row needs completion",
			row:  map[string]interface{}{"obj_type": "file", "size_bytes": int64(7)},
			want: syncFSObjectRowNeedsCompletion,
		},
		{
			name: "conflicting complete row",
			row:  map[string]interface{}{"obj_type": "file", "size_bytes": int64(7), "dir_entries": "[]", "block_ids": []string{"block-b"}},
			want: syncFSObjectRowConflict,
		},
		{
			name: "conflicting partial row",
			row:  map[string]interface{}{"obj_type": "file", "size_bytes": int64(8)},
			want: syncFSObjectRowConflict,
		},
	}
	dirExpected := syncFSObjectIdentity{objType: "dir", sizeBytes: 0, dirEntries: "[]"}
	if got := classifySyncFSObjectRow(map[string]interface{}{"obj_type": "dir", "size_bytes": int64(0), "dir_entries": "[]"}, dirExpected); got != syncFSObjectRowComplete {
		t.Fatalf("classifySyncFSObjectRow directory = %v, want %v", got, syncFSObjectRowComplete)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifySyncFSObjectRow(tc.row, expected); got != tc.want {
				t.Fatalf("classifySyncFSObjectRow = %v, want %v", got, tc.want)
			}
		})
	}
}
